// Package updater 实现「点一下自己换掉自己」的在线更新。
//
// 流程:查最新 Release → 挑本平台的产物 → 下载到同目录临时文件 → 校验 SHA256
// → 原子替换正在运行的二进制 → 优雅关掉服务后重启。
//
// 几个必须讲清楚的前提:
//
//  1. **必须校验 SHA256**。更新是"拿一个远程文件覆盖掉本机正在跑的程序",
//     没有校验就等于给任何能中间人的网络一把远程代码执行的钥匙。
//     校验值来自 Release 里的 checksums.txt(build.sh 发版时一起上传)。
//     没有这个文件就直接拒绝更新,不给"要不要冒险"的选项 —— 那种选项迟早会被点。
//
//  2. **临时文件必须和目标二进制同目录**。os.Rename 跨文件系统会失败,
//     而 /tmp 和程序所在盘经常不是同一个文件系统(容器里尤其常见)。
//
//  3. **替换方式分平台**:
//     Unix 的 rename 是原子的,而且允许覆盖正在执行的文件(内核按 inode 引用,
//     旧进程继续用旧 inode 跑);Windows 不允许覆盖正在运行的 exe,
//     只能先把自己改名成 .old 再把新文件放到原位,下次启动时清理 .old。
package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ovh-buy/server/internal/app"
)

const (
	// Repo 上游仓库,与 handlers 里的检查逻辑保持一致
	Repo = "gokele/ovh"
	// UserAgent GitHub API 要求带 UA
	UserAgent = "OVH-Console-Updater"
	// ChecksumAsset 校验和文件名(build.sh 发版时生成并上传)
	ChecksumAsset = "checksums.txt"
	// 下载 200MB 上限,防止被喂一个超大文件撑爆磁盘
	maxAssetBytes = 200 << 20
)

// Release GitHub Release 里我们用得上的字段
type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	HTMLURL string  `json:"html_url"`
	Draft   bool    `json:"draft"`
	Assets  []Asset `json:"assets"`
}

// Asset Release 附件
type Asset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// AssetName 当前平台对应的产物名,与 build.sh 的命名一致
func AssetName() string {
	name := fmt.Sprintf("ovh-server-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// Progress 更新进度,给前端轮询用
type Progress struct {
	Phase   string `json:"phase"`   // idle / downloading / verifying / installing / restarting / done / failed
	Message string `json:"message"` // 面向用户的中文说明
	Percent int    `json:"percent"` // 0-100,只有下载阶段有意义
	Version string `json:"version"` // 目标版本
	Error   string `json:"error,omitempty"`
}

func httpClient(timeout time.Duration) *http.Client { return &http.Client{Timeout: timeout} }

// LatestReleaseURL 查最新版本的地址。
// 可以用环境变量 OVH_UPDATE_API 覆盖:私有镜像/内网分发用得上,
// 端到端测试也靠它把更新源指到本地假服务器,不必往公开 Release 上传测试文件。
func LatestReleaseURL() string {
	if v := strings.TrimSpace(os.Getenv("OVH_UPDATE_API")); v != "" {
		return v
	}
	return "https://api.github.com/repos/" + Repo + "/releases/latest"
}

// FetchLatest 取最新 Release
func FetchLatest() (*Release, error) {
	req, _ := http.NewRequest(http.MethodGet, LatestReleaseURL(), nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := httpClient(20 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取最新版本失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GitHub 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("解析 Release 失败: %w", err)
	}
	return &rel, nil
}

// findAsset 在 Release 附件里找指定名字的那个
func findAsset(rel *Release, name string) *Asset {
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return &rel.Assets[i]
		}
	}
	return nil
}

// fetchChecksum 下载 checksums.txt 并取出目标文件的 sha256。
// 格式就是 shasum -a 256 的标准输出:"<hex>  <filename>"
func fetchChecksum(rel *Release, target string) (string, error) {
	a := findAsset(rel, ChecksumAsset)
	if a == nil {
		return "", fmt.Errorf("这个版本没有发布 %s,无法校验下载内容的完整性,已中止更新", ChecksumAsset)
	}
	req, _ := http.NewRequest(http.MethodGet, a.BrowserDownloadURL, nil)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := httpClient(30 * time.Second).Do(req)
	if err != nil {
		return "", fmt.Errorf("下载校验和失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载校验和失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("读取校验和失败: %w", err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		// 文件名可能带 * 前缀(shasum 的二进制模式)
		fname := strings.TrimPrefix(fields[len(fields)-1], "*")
		if filepath.Base(fname) == target {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("%s 里没有 %s 的校验和", ChecksumAsset, target)
}

// downloadVerified 下载附件到 dst,边下边算 SHA256,校验不过就删掉临时文件并报错
func downloadVerified(a *Asset, dst, wantSum string, onProgress func(pct int)) error {
	req, _ := http.NewRequest(http.MethodGet, a.BrowserDownloadURL, nil)
	req.Header.Set("User-Agent", UserAgent)
	// 二进制 16MB 左右,给足 10 分钟应付慢网络
	resp, err := httpClient(10 * time.Minute).Do(req)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("创建临时文件失败(目录不可写?): %w", err)
	}
	h := sha256.New()
	var written int64
	buf := make([]byte, 256<<10)
	lastPct := -1
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			written += int64(n)
			if written > maxAssetBytes {
				f.Close()
				os.Remove(dst)
				return fmt.Errorf("下载内容超过 %d MB 上限,已中止", maxAssetBytes>>20)
			}
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(dst)
				return fmt.Errorf("写入临时文件失败: %w", werr)
			}
			h.Write(buf[:n])
			if a.Size > 0 && onProgress != nil {
				if pct := int(written * 100 / a.Size); pct != lastPct {
					lastPct = pct
					onProgress(pct)
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(dst)
			return fmt.Errorf("下载中断: %w", rerr)
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(dst)
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, wantSum) {
		os.Remove(dst)
		return fmt.Errorf("校验和不匹配(期望 %s,实际 %s),文件可能被篡改或下载损坏,已丢弃", wantSum[:12]+"…", got[:12]+"…")
	}
	return nil
}

// selfPathFn 取当前可执行文件路径。做成变量是为了让测试能指到沙箱里的假二进制 ——
// 真去替换测试进程自己的话,go test 会当场爆炸。
var selfPathFn = defaultSelfPath

func selfPath() (string, error) { return selfPathFn() }

// defaultSelfPath 当前可执行文件的真实路径(解掉符号链接)
func defaultSelfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("拿不到当前程序路径: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// Prepare 下载并校验新版本,把它放到最终位置的旁边(还没替换)。
// 返回临时文件路径,调用方校验通过后再调 Install。
func Prepare(rel *Release, onProgress func(pct int)) (tmpPath, exePath string, err error) {
	exePath, err = selfPath()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Dir(exePath)
	// 先探一下目录可写:不可写的话(比如装在 /usr/local/bin 而当前不是 root)
	// 早点报错比下完 16MB 再失败体验好得多
	probe := filepath.Join(dir, ".ovh-server-update-probe")
	if f, perr := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o644); perr != nil {
		return "", "", fmt.Errorf("程序所在目录不可写(%s),无法自更新。请用有写权限的账户运行,或手动替换二进制", dir)
	} else {
		f.Close()
		os.Remove(probe)
	}

	name := AssetName()
	asset := findAsset(rel, name)
	if asset == nil {
		return "", "", fmt.Errorf("这个版本没有提供 %s(当前平台 %s/%s)的产物", name, runtime.GOOS, runtime.GOARCH)
	}
	sum, err := fetchChecksum(rel, name)
	if err != nil {
		return "", "", err
	}
	// 临时文件必须和目标同目录:os.Rename 跨文件系统会失败
	tmpPath = filepath.Join(dir, fmt.Sprintf(".ovh-server-new-%d", os.Getpid()))
	if err := downloadVerified(asset, tmpPath, sum, onProgress); err != nil {
		return "", "", err
	}
	return tmpPath, exePath, nil
}

// CleanupStale 启动时清理上一次更新留下的残骸(Windows 的 .old,以及中断的临时文件)
func CleanupStale() {
	exe, err := selfPath()
	if err != nil {
		return
	}
	// 注意:.old 是回滚用的后路,由 MarkHealthy(启动正常)或 RollbackIfStale(启动失败)
	// 决定去留,这里**不能**无脑删 —— 删了等于取消了回滚能力。
	dir := filepath.Dir(exe)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ovh-server-new-") || e.Name() == ".ovh-server-update-probe" {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// backupSuffix 更新时旧二进制的备份后缀。
// Windows 一直在用它(系统不允许覆盖运行中的 exe,只能先改名);
// Unix 现在也留一份,作为"新版本起不来"时的后路。
const backupSuffix = ".old"

// copyFile 硬链接建不了时的退路
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// MarkHealthy 新版本成功启动后调用:删掉上一版的备份。
//
// 这是"更新后回滚"的另一半 —— Install 留下备份,进程真的起来了才认为这次更新成功。
// 没走到这里(启动就崩、端口占不上、panic)备份就一直在,下次启动时 RollbackIfStale
// 会把它换回去。
//
// 注意时机:必须在**服务真正开始对外提供服务之后**才调,不能一进 main 就调 ——
// 那样等于没有验证。
func MarkHealthy(state *app.State) {
	exe, err := selfPath()
	if err != nil {
		return
	}
	// 先删"启动中"标记 —— RollbackIfStale 的判据就是它。
	// 备份可能因为磁盘/权限问题压根没建成,那种情况下标记要是留着,
	// 下次启动虽然不会误回滚(没备份可回),文件却会一直躺在那儿。
	_ = os.Remove(exe + pendingSuffix)

	backup := exe + backupSuffix
	if _, err := os.Stat(backup); err != nil {
		return // 没有备份 = 这次不是更新后的首次启动
	}
	if err := os.Remove(backup); err == nil {
		state.Logger.Info("[更新] 新版本启动正常,已清理上一版备份", "version")
	}
}

// RollbackIfStale 启动时检查:上一次更新后是不是根本没跑起来。
//
// 判据是一个"启动中"标记文件:Install 之后、重启之前写下它,
// MarkHealthy 时删掉。所以启动时如果同时看到「备份」和「启动中标记」,
// 说明上一次换上去的新版本没能活到对外服务那一刻 —— 把备份换回去。
//
// 返回 true 表示已回滚(调用方应当直接用回滚后的二进制重启)。
func RollbackIfStale(state *app.State) bool {
	exe, err := selfPath()
	if err != nil {
		return false
	}
	backup := exe + backupSuffix
	pending := exe + pendingSuffix
	if _, err := os.Stat(backup); err != nil {
		return false
	}
	raw, perr := os.ReadFile(pending)
	if perr != nil {
		// 有备份但没有"启动中"标记:说明上次是正常起来过的(只是 MarkHealthy 没删掉),
		// 清掉备份即可,不要回滚 —— 那会把用户刚更新好的版本又换回旧的。
		_ = os.Remove(backup)
		return false
	}

	// 标记里记的是"带着这个标记启动过几次"。
	//
	// 这里必须能区分「新版本正在启动」和「新版本上次没启动起来」——
	// 而这个函数跑在启动早期,MarkHealthy 要等端口监听成功才执行,
	// 所以光看"标记还在"永远是真的。之前就是这么写的,后果是每次自更新
	// 都在新版本的第一次启动时被判定为失败、静默回滚成旧版本,
	// 用户看到的是前端一直转"正在重启并加载新版本…"直到超时。
	//
	// 计数 0 = 新版本头一回启动,放行,让它自己跑到 MarkHealthy 把标记删掉;
	// 计数 ≥1 = 上一回带着标记启动过却没能撑到 MarkHealthy,这才是真的起不来。
	if boots := parseBoots(raw); boots < 1 {
		if err := os.WriteFile(pending, []byte(strconv.Itoa(boots+1)), 0o644); err != nil {
			// 写不进去就不能再放行:下次启动还会读到 0,永远滚不动,
			// 等于回滚保护彻底失效。宁可这一次多回滚一遍。
			state.Logger.Warn("[更新] 无法更新待验证标记,按保守策略回滚: "+err.Error(), "version")
		} else {
			state.Logger.Info("[更新] 新版本首次启动,回滚保护已就绪(启动成功后自动解除)", "version")
			return false
		}
	}

	state.Logger.Error("[更新] 检测到上一次更新后未能正常启动,正在回滚到更新前的版本", "version")
	_ = os.Remove(pending)
	if err := os.Rename(backup, exe); err != nil {
		state.Logger.Error("[更新] 回滚失败,请手动用备份文件替换: "+err.Error(), "version")
		return false
	}
	state.Logger.Info("[更新] 已回滚。请检查新版本为什么起不来后再试", "version")
	return true
}

// pendingSuffix "更新后待验证"标记
const pendingSuffix = ".pending"

// MarkPending 替换完二进制、准备重启前写下标记。
// 内容是"带着这个标记启动过几次",初始 0 —— 此刻新版本还一次都没启动过。
func MarkPending() {
	if exe, err := selfPath(); err == nil {
		_ = os.WriteFile(exe+pendingSuffix, []byte("0"), 0o644)
	}
}

// parseBoots 读启动次数。内容不认识就当 1 处理 ——
// 老版本写的是 "1",而且"读不懂"时保守地倾向回滚比倾向放行安全。
func parseBoots(raw []byte) int {
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 1
	}
	if n < 0 {
		return 0
	}
	return n
}
