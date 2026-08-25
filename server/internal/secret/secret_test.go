package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 每个用例都要一套干净的全局状态
func fresh(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mu.Lock()
	aead, loaded, keyGenerated = nil, false, false
	mu.Unlock()
	failures.Store(0)
	t.Setenv(KeyEnv, "")
	os.Unsetenv(KeyEnv)
	if err := Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return dir
}

func TestEncryptDecrypt往返(t *testing.T) {
	fresh(t)
	for _, plain := range []string{"abc", "带中文的密钥", strings.Repeat("x", 4096), "a b\tc\n"} {
		enc := Encrypt(plain)
		if !IsEncrypted(enc) {
			t.Fatalf("%q 加密后没有密文前缀: %q", plain, enc)
		}
		if enc == plain {
			t.Fatalf("%q 根本没被加密", plain)
		}
		got, err := Decrypt(enc)
		if err != nil || got != plain {
			t.Fatalf("往返失败: %q → %q (err=%v)", plain, got, err)
		}
	}
}

// 同一明文两次加密必须产生不同密文(nonce 随机),否则能从密文看出"这两个账户用了同一个 key"
func TestEncrypt每次不同(t *testing.T) {
	fresh(t)
	if Encrypt("same") == Encrypt("same") {
		t.Error("两次加密结果相同 —— nonce 没起作用")
	}
}

// 老库升级:表里全是明文,读的时候必须原样返回,不能当密文去解
func TestDecrypt兼容老库明文(t *testing.T) {
	fresh(t)
	for _, plain := range []string{"PLAINKEY123", "", "看起来像密文但不是:enc"} {
		got, err := Decrypt(plain)
		if err != nil || got != plain {
			t.Errorf("明文 %q 应原样返回, 得到 %q err=%v", plain, got, err)
		}
	}
}

// 空串不该产生密文 —— 否则"没填"和"填了空"在库里长得不一样
func TestEncrypt空串(t *testing.T) {
	fresh(t)
	if got := Encrypt(""); got != "" {
		t.Errorf("空串应原样返回, 得到 %q", got)
	}
}

// 换了密钥 → 解不开,而且要计数(启动时据此大声告警)
func TestDecrypt密钥不对(t *testing.T) {
	dir := fresh(t)
	enc := Encrypt("我的密钥")

	// 换一把钥匙
	os.Remove(filepath.Join(dir, keyFile))
	mu.Lock()
	aead, loaded = nil, false
	mu.Unlock()
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}

	before := DecryptFailures()
	if _, err := Decrypt(enc); err == nil {
		t.Fatal("密钥换过之后必须解不开")
	}
	if DecryptFailures() != before+1 {
		t.Error("解密失败没有被计数,启动时就无法告警")
	}
}

// 环境变量密钥:不落盘
func TestKeyFromEnv不落盘(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(KeyEnv, "这是一句口令")
	mu.Lock()
	aead, loaded, keyGenerated = nil, false, false
	mu.Unlock()
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if !FromEnv() {
		t.Error("FromEnv 应为 true")
	}
	if _, err := os.Stat(filepath.Join(dir, keyFile)); !os.IsNotExist(err) {
		t.Error("用环境变量时不该写密钥文件")
	}
	enc := Encrypt("x")
	if got, err := Decrypt(enc); err != nil || got != "x" {
		t.Errorf("环境变量密钥往返失败: %v", err)
	}
}

// 密钥文件权限必须是 0600 —— 同机器其它用户不能读
func TestKeyFile权限(t *testing.T) {
	dir := fresh(t)
	fi, err := os.Stat(filepath.Join(dir, keyFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("密钥文件权限 = %o, 期望 600", perm)
	}
}

// 加密没初始化时必须退化成明文,而不是让用户存不进账户
func TestEncrypt未初始化时退化为明文(t *testing.T) {
	mu.Lock()
	aead, loaded = nil, false
	mu.Unlock()
	if got := Encrypt("plain"); got != "plain" {
		t.Errorf("未初始化时应原样返回, 得到 %q", got)
	}
}
