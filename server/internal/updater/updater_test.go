package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 造一个假的 Release 服务:一个二进制附件 + 一份 checksums.txt
func fakeRelease(t *testing.T, payload []byte, sumOverride string) (*httptest.Server, *Release) {
	t.Helper()
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])
	if sumOverride != "" {
		hexSum = sumOverride
	}
	name := AssetName()

	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.Write(payload)
	})
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n%s  ovh-server-other-arch\n", hexSum, name, strings.Repeat("0", 64))
	})
	srv := httptest.NewServer(mux)

	rel := &Release{
		TagName: "v9.9.9",
		Assets: []Asset{
			{Name: name, Size: int64(len(payload)), BrowserDownloadURL: srv.URL + "/asset"},
			{Name: ChecksumAsset, BrowserDownloadURL: srv.URL + "/sums"},
		},
	}
	return srv, rel
}

// 把"当前可执行文件"指到临时目录里的一个假二进制,好让 Prepare/Install 在沙箱里跑
func withFakeExe(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "ovh-server-fake")
	if err := os.WriteFile(exe, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := selfPathFn
	selfPathFn = func() (string, error) { return exe, nil }
	t.Cleanup(func() { selfPathFn = orig })
	return exe
}

func TestPrepareAndInstall(t *testing.T) {
	newBinary := []byte("#!/bin/sh\necho new version\n")
	srv, rel := fakeRelease(t, newBinary, "")
	defer srv.Close()

	exe := withFakeExe(t, "old version")

	var lastPct int
	tmp, gotExe, err := Prepare(rel, func(p int) { lastPct = p })
	if err != nil {
		t.Fatalf("Prepare 失败: %v", err)
	}
	if gotExe != exe {
		t.Errorf("目标路径 = %q, 期望 %q", gotExe, exe)
	}
	if filepath.Dir(tmp) != filepath.Dir(exe) {
		t.Errorf("临时文件必须与目标同目录(否则 rename 跨文件系统会失败): %q vs %q", tmp, exe)
	}
	if lastPct != 100 {
		t.Errorf("下载进度最终应到 100, 实际 %d", lastPct)
	}

	if err := Install(tmp, exe); err != nil {
		t.Fatalf("Install 失败: %v", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != string(newBinary) {
		t.Errorf("替换后内容不对: %q", string(got))
	}
	if fi, err := os.Stat(exe); err == nil && fi.Mode().Perm()&0o111 == 0 {
		t.Error("替换后的文件没有可执行位")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("临时文件应该已被 rename 掉")
	}
}

// 校验和不匹配必须拒绝,而且不能留下临时文件 —— 这是整个更新链路最关键的一道闸:
// 没有它,任何能中间人的网络都能让程序把自己换成任意二进制。
func TestPrepareRejectsBadChecksum(t *testing.T) {
	srv, rel := fakeRelease(t, []byte("malicious payload"), strings.Repeat("a", 64))
	defer srv.Close()
	exe := withFakeExe(t, "old version")

	tmp, _, err := Prepare(rel, nil)
	if err == nil {
		t.Fatal("校验和不匹配却没报错")
	}
	if !strings.Contains(err.Error(), "校验和不匹配") {
		t.Errorf("错误信息应指明校验失败, 实际: %v", err)
	}
	if tmp != "" {
		if _, statErr := os.Stat(tmp); statErr == nil {
			t.Error("校验失败后临时文件必须删掉")
		}
	}
	// 原文件不能被动过
	if got, _ := os.ReadFile(exe); string(got) != "old version" {
		t.Error("校验失败时不能碰原来的二进制")
	}
}

// 没有 checksums.txt 时直接拒绝,不给"要不要冒险"的选项
func TestPrepareRequiresChecksumFile(t *testing.T) {
	payload := []byte("x")
	srv, rel := fakeRelease(t, payload, "")
	defer srv.Close()
	withFakeExe(t, "old")

	// 去掉 checksums.txt 附件
	rel.Assets = rel.Assets[:1]
	if _, _, err := Prepare(rel, nil); err == nil || !strings.Contains(err.Error(), ChecksumAsset) {
		t.Fatalf("缺少校验和文件时应拒绝更新, 实际: %v", err)
	}
}

// 当前平台没有对应产物时要说清楚,而不是随便抓一个附件装上
func TestPrepareRejectsMissingAsset(t *testing.T) {
	srv, rel := fakeRelease(t, []byte("x"), "")
	defer srv.Close()
	withFakeExe(t, "old")

	rel.Assets[0].Name = "ovh-server-someother-arch"
	if _, _, err := Prepare(rel, nil); err == nil || !strings.Contains(err.Error(), AssetName()) {
		t.Fatalf("缺少本平台产物时应拒绝, 实际: %v", err)
	}
}

// 目录不可写时要早报错(而不是下完 16MB 才失败)
func TestPrepareRejectsReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 无视目录权限,跳过")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "ovh-server-fake")
	os.WriteFile(exe, []byte("old"), 0o755)
	os.Chmod(dir, 0o555)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	orig := selfPathFn
	selfPathFn = func() (string, error) { return exe, nil }
	t.Cleanup(func() { selfPathFn = orig })

	srv, rel := fakeRelease(t, []byte("x"), "")
	defer srv.Close()
	if _, _, err := Prepare(rel, nil); err == nil || !strings.Contains(err.Error(), "不可写") {
		t.Fatalf("目录不可写时应提前报错, 实际: %v", err)
	}
}

func TestAssetNameMatchesBuildScript(t *testing.T) {
	// build.sh 产出的名字形如 ovh-server-linux-amd64 / ovh-server-windows-amd64.exe
	n := AssetName()
	if !strings.HasPrefix(n, "ovh-server-") {
		t.Errorf("产物名前缀必须与 build.sh 一致: %s", n)
	}
}

func TestCleanupStaleRemovesLeftovers(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "ovh-server-fake")
	os.WriteFile(exe, []byte("cur"), 0o755)
	orig := selfPathFn
	selfPathFn = func() (string, error) { return exe, nil }
	t.Cleanup(func() { selfPathFn = orig })

	leftovers := []string{exe + ".old", filepath.Join(dir, ".ovh-server-new-123"), filepath.Join(dir, ".ovh-server-update-probe")}
	for _, f := range leftovers {
		os.WriteFile(f, []byte("junk"), 0o644)
	}
	CleanupStale()
	for _, f := range leftovers {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("残骸未清理: %s", f)
		}
	}
	if _, err := os.Stat(exe); err != nil {
		t.Error("清理不能误删当前程序")
	}
}

func TestFetchLatestParsesRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Release{TagName: "v1.2.3", Assets: []Asset{{Name: "a"}}})
	}))
	defer srv.Close()
	// FetchLatest 打的是写死的 GitHub 地址,这里只验证 JSON 结构解析没问题
	var rel Release
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("请求假服务器失败: %v", err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil || rel.TagName != "v1.2.3" {
		t.Fatalf("Release 解析失败: %v %+v", err, rel)
	}
}
