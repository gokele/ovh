// Package secret 给落盘的敏感字段做加密。
//
// 为什么需要:sniper.db 的 ovh_accounts 表存着 OVH AppKey / AppSecret / ConsumerKey,
// kv 表存着 Telegram Bot Token 与 webhook secret —— 全是明文。拿到这个文件
// 就等于拿到用户 OVH 账户的完全控制权(能下单、能重装、能删机器)。
// .gitignore 只挡住"提交到仓库"这一种泄漏方式,挡不住:备份被同步到网盘、
// 拷贝整个目录换机器、把 data/ 打包发给别人排查问题。
//
// 威胁模型要说实话:密钥默认放在数据库旁边的 .dbkey 文件里,能同时拿到两个文件的人
// 依然能解开。它防的是"只拿到 db 文件"这一类泄漏 —— 而这恰恰是最常见的一类。
// 想要真正的隔离就设环境变量 OVH_DB_KEY(密钥不落盘,只在进程内存里)。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// prefix 密文标记。没有这个前缀的值一律当明文处理 ——
// 老库升级上来时表里全是明文,不能一律当密文去解(那会把所有账户解成乱码)。
const prefix = "enc:v1:"

// EncPrefix 密文标记,给需要在 SQL/JSON 里直接找密文的地方用
const EncPrefix = prefix

// KeyEnv 环境变量名:设了它,密钥就不落盘
const KeyEnv = "OVH_DB_KEY"

// keyFile 旧版密钥文件名(放在 dataDir 下,0600)。
// 新装机器不再往这里写 —— 密钥现在落在 .env 里,和其它配置放一起。
// 但**必须**继续读它:老用户的密钥就在这个文件里,读不到就等于把他们的凭据全废了。
const keyFile = ".dbkey"

var (
	mu     sync.RWMutex
	aead   cipher.AEAD
	loaded bool
	// keyGenerated 本次启动是否新生成了密钥(而不是读到已有的)。
	// 这个标志有个要命的用途:如果密钥是新生成的,而数据库里已经躺着密文,
	// 说明原来那把钥匙丢了 —— 那批凭据永远解不开了,必须让用户知道,
	// 不能装作没事继续跑(表现会是"账户都在但全连不上 OVH",没人猜得到是密钥问题)。
	keyGenerated bool
	// keySource 密钥是从哪儿来的,只用于日志
	keySource string
)

// failures 解密失败次数。密钥丢了/换了的时候,凭据会全部解成空串 ——
// 如果只是静默返回空,用户看到的是"账户还在但连不上 OVH",完全猜不到是密钥问题。
// 用原子计数而不是复用上面那把读写锁:Decrypt 持的是读锁,
// 在读锁里升级成写锁要先放锁再抢,中间那个窗口容易出问题,不值得为一个计数器冒险。
var failures atomic.Int64

// DecryptFailures 至今为止的解密失败次数
func DecryptFailures() int { return int(failures.Load()) }

// KeyWasGenerated 本次启动是否新生成了密钥
func KeyWasGenerated() bool {
	mu.RLock()
	defer mu.RUnlock()
	return keyGenerated
}

// KeySource 密钥来自哪里,给启动日志用
func KeySource() string {
	mu.RLock()
	defer mu.RUnlock()
	return keySource
}

// Init 解析密钥并初始化。
// dataDir 是数据库所在目录(读旧版 .dbkey 用),envPath 是配置文件路径(新密钥写这儿)。
func Init(dataDir, envPath string) error {
	mu.Lock()
	defer mu.Unlock()

	key, err := resolveKey(dataDir, envPath)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("初始化加密失败: %w", err)
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("初始化加密失败: %w", err)
	}
	aead = g
	loaded = true
	return nil
}

// Enabled 加密是否可用
func Enabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return loaded
}

// FromEnv 密钥是否来自环境变量(不落盘)
func FromEnv() bool { return strings.TrimSpace(os.Getenv(KeyEnv)) != "" }

// resolveKey 按优先级找密钥,一个都没有就生成并写进配置文件。
//
// 顺序不能变:
//  1. 环境变量 OVH_DB_KEY —— 也包括 .env 里那一行(godotenv 在 Init 之前已经加载过了)
//  2. 旧版的 data/.dbkey —— 老用户的密钥在这儿,漏读一次他们所有凭据就废了
//  3. 都没有 → 生成一把新的,写进 .env
func resolveKey(dataDir, envPath string) ([]byte, error) {
	if v := strings.TrimSpace(os.Getenv(KeyEnv)); v != "" {
		keySource = "环境变量 " + KeyEnv
		// 允许 hex / base64 / 任意口令:统一 SHA-256 成 32 字节,
		// 免得用户被"必须正好 32 字节"卡住而干脆不开加密
		if b, err := hex.DecodeString(v); err == nil && len(b) == 32 {
			return b, nil
		}
		if b, err := base64.StdEncoding.DecodeString(v); err == nil && len(b) == 32 {
			return b, nil
		}
		sum := sha256.Sum256([]byte(v))
		return sum[:], nil
	}

	legacy := filepath.Join(dataDir, keyFile)
	if b, err := os.ReadFile(legacy); err == nil {
		raw, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if derr == nil && len(raw) == 32 {
			keySource = "密钥文件 " + legacy
			return raw, nil
		}
		return nil, fmt.Errorf("密钥文件 %s 内容不合法(应为 base64 的 32 字节);"+
			"如果它被改坏了,已加密的账户凭据将无法解开,需要重新录入", legacy)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("生成密钥失败: %w", err)
	}
	enc := base64.StdEncoding.EncodeToString(key)

	// 写进配置文件。写不进去就退回旧的 .dbkey —— 只读挂载的 .env、
	// 容器里没有配置文件的部署方式都真实存在,不能因为这个起不来。
	if err := appendEnvKey(envPath, enc); err != nil {
		if werr := os.WriteFile(legacy, []byte(enc+"\n"), 0o600); werr != nil {
			return nil, fmt.Errorf("密钥既写不进配置文件(%v),也写不进 %s: %w", err, legacy, werr)
		}
		keySource = "密钥文件 " + legacy + "(配置文件写不进去: " + err.Error() + ")"
	} else {
		keySource = "配置文件 " + envPath
	}

	keyGenerated = true
	return key, nil
}

// appendEnvKey 把密钥追加到 .env。
//
// 只追加不重写:这个文件是用户自己在维护的(端口、API 密钥、监听地址都在里面),
// 解析后重新序列化会丢掉注释和排版,是拿别人的配置文件冒险。
func appendEnvKey(envPath, encodedKey string) error {
	if strings.TrimSpace(envPath) == "" {
		return errors.New("没有配置文件路径")
	}
	line := "\n# 数据库加密密钥(自动生成)。加密的是 OVH API 凭据和 Telegram Token。\n" +
		"# 丢了这一行,已保存的账户就再也解不开了,只能重新录入 —— 备份配置文件时别漏了它。\n" +
		KeyEnv + "=" + encodedKey + "\n"

	// O_RDWR 而不是 O_WRONLY:下面要把最后一个字节读回来判断有没有换行,
	// 只写的 fd 读不了(而且失败是静默的,那段检查会白写)
	f, err := os.OpenFile(envPath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	// 上一行没换行就先补一个,否则会拼到人家最后一行配置的尾巴上
	if st, serr := f.Stat(); serr == nil && st.Size() > 0 {
		buf := make([]byte, 1)
		if _, rerr := f.ReadAt(buf, st.Size()-1); rerr == nil && buf[0] != '\n' {
			line = "\n" + line
		}
	}
	if _, err := f.WriteString(line); err != nil {
		return err
	}
	// 里面现在有密钥了,别让同机器上的其它用户读到。
	// 已存在的文件也收紧 —— 它本来就存着 API_SECRET_KEY,一直是该 0600 的。
	_ = os.Chmod(envPath, 0o600)
	return nil
}

// Encrypt 加密一个字段。空串原样返回(空就是空,没必要产生密文)。
// 加密不可用时原样返回明文 —— 宁可退化成升级前的行为,也不能让用户存不进账户。
func Encrypt(plain string) string {
	if plain == "" {
		return ""
	}
	mu.RLock()
	defer mu.RUnlock()
	if !loaded {
		return plain
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return plain
	}
	sealed := aead.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.StdEncoding.EncodeToString(sealed)
}

// Decrypt 解密。没有密文前缀的值当明文原样返回(老库 / 加密未启用时写入的)。
func Decrypt(stored string) (string, error) {
	if !strings.HasPrefix(stored, prefix) {
		return stored, nil
	}
	mu.RLock()
	defer mu.RUnlock()
	if !loaded {
		return "", errors.New("数据库里是加密的凭据,但当前没有可用的密钥(检查 " + KeyEnv + " 或 data/.dbkey)")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil {
		return "", fmt.Errorf("密文格式损坏: %w", err)
	}
	ns := aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("密文长度异常")
	}
	out, err := aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		failures.Add(1)
		return "", errors.New("解密失败:密钥不对(换过 " + KeyEnv + " 或 .dbkey 被替换过?)")
	}
	return string(out), nil
}

// IsEncrypted 判断存储值是否已经是密文
func IsEncrypted(stored string) bool { return strings.HasPrefix(stored, prefix) }
