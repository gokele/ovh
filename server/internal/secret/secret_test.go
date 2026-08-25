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
	if err := Init(dir, filepath.Join(dir, ".env")); err != nil {
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

	// 换一把钥匙:密钥现在落在 .env 里,删掉它再初始化就会生成新的
	os.Remove(filepath.Join(dir, ".env"))
	os.Remove(filepath.Join(dir, keyFile))
	os.Unsetenv(KeyEnv)
	mu.Lock()
	aead, loaded = nil, false
	mu.Unlock()
	if err := Init(dir, filepath.Join(dir, ".env")); err != nil {
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
	if err := Init(dir, filepath.Join(dir, ".env")); err != nil {
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

// 退回 .dbkey 时权限必须是 0600 —— 同机器其它用户不能读。
// (默认路径现在是写进 .env,那条的权限由 TestInit没密钥时写进配置文件 盯着)
func TestKeyFile权限(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(KeyEnv, "")
	os.Unsetenv(KeyEnv)
	if err := reinit(t, dir, ""); err != nil {
		t.Fatal(err)
	}
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

// reinit 换一套路径重新初始化,模拟"重启一次"
func reinit(t *testing.T, dataDir, envPath string) error {
	t.Helper()
	mu.Lock()
	aead, loaded, keyGenerated, keySource = nil, false, false, ""
	mu.Unlock()
	return Init(dataDir, envPath)
}

// 没密钥就生成一把并写进配置文件 —— 这是新装机器的默认路径
func TestInit没密钥时写进配置文件(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	t.Setenv(KeyEnv, "")
	os.Unsetenv(KeyEnv)

	if err := reinit(t, dir, env); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !KeyWasGenerated() {
		t.Error("应该标记为新生成")
	}
	b, err := os.ReadFile(env)
	if err != nil {
		t.Fatalf("配置文件没写出来: %v", err)
	}
	if !strings.Contains(string(b), KeyEnv+"=") {
		t.Errorf("配置文件里应该有 %s 那一行,实际内容:\n%s", KeyEnv, b)
	}
	// 不该再往 data/ 扔 .dbkey
	if _, err := os.Stat(filepath.Join(dir, keyFile)); err == nil {
		t.Error("新装机器不该再生成 data/.dbkey")
	}
	// 0600:同机器上别的用户不该读得到
	if st, _ := os.Stat(env); st != nil && st.Mode().Perm() != 0o600 {
		t.Errorf("配置文件权限应该是 0600,实际 %o", st.Mode().Perm())
	}
}

// 追加不能破坏用户已有的配置,也不能和最后一行黏在一起
func TestInit追加时不破坏已有配置(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	// 故意不以换行结尾
	os.WriteFile(env, []byte("PORT=19998\n# 注释\nAPI_SECRET_KEY=abc"), 0o644)
	t.Setenv(KeyEnv, "")
	os.Unsetenv(KeyEnv)

	if err := reinit(t, dir, env); err != nil {
		t.Fatalf("Init: %v", err)
	}
	got := string(mustRead(t, env))
	for _, want := range []string{"PORT=19998", "# 注释", "API_SECRET_KEY=abc"} {
		if !strings.Contains(got, want) {
			t.Errorf("原有配置 %q 被弄丢了,实际:\n%s", want, got)
		}
	}
	if strings.Contains(got, "API_SECRET_KEY=abc"+KeyEnv) {
		t.Error("密钥黏到了上一行末尾")
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, KeyEnv+"=") && len(line) > len(KeyEnv)+10 {
			return // 找到了独立成行的密钥
		}
	}
	t.Errorf("没找到独立成行的 %s,实际:\n%s", KeyEnv, got)
}

// 最要命的一条:老用户的密钥在 data/.dbkey 里,升级后必须继续读它。
// 漏读一次就是重新生成 —— 那批已加密的凭据全部作废。
func TestInit优先读旧版密钥文件(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	t.Setenv(KeyEnv, "")
	os.Unsetenv(KeyEnv)

	// 先造一个"老用户":用旧路径生成密钥,加密一段数据
	if err := reinit(t, dir, ""); err != nil { // envPath 为空 → 退回 .dbkey
		t.Fatalf("造老库失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, keyFile)); err != nil {
		t.Fatalf("没写出旧版密钥文件: %v", err)
	}
	ciphertext := Encrypt("老用户的 OVH AppSecret")

	// 模拟升级后重启:环境变量没有,.env 也还没有
	os.Unsetenv(KeyEnv)
	if err := reinit(t, dir, env); err != nil {
		t.Fatalf("重启失败: %v", err)
	}
	if KeyWasGenerated() {
		t.Fatal("有旧版密钥文件时绝不能重新生成 —— 那会让所有已存凭据永久解不开")
	}
	got, err := Decrypt(ciphertext)
	if err != nil || got != "老用户的 OVH AppSecret" {
		t.Errorf("升级后应该还能解开老数据,实际: %q err=%v", got, err)
	}
}

// 环境变量优先级最高:用户显式传了密钥,就不该再去碰任何文件
func TestInit环境变量优先(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	t.Setenv(KeyEnv, "我自己的口令")

	if err := reinit(t, dir, env); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if KeyWasGenerated() {
		t.Error("环境变量给了密钥就不该生成新的")
	}
	if _, err := os.Stat(env); err == nil {
		t.Error("环境变量给了密钥就不该去写配置文件")
	}
}

// 配置文件写不进去(只读挂载 / 目录不存在)时要退回 .dbkey,不能起不来
func TestInit配置文件写不进时退回密钥文件(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(KeyEnv, "")
	os.Unsetenv(KeyEnv)

	badEnv := filepath.Join(dir, "不存在的目录", ".env")
	if err := reinit(t, dir, badEnv); err != nil {
		t.Fatalf("配置文件写不进去时不该直接失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, keyFile)); err != nil {
		t.Error("应该退回到 data/.dbkey")
	}
	if !strings.Contains(KeySource(), keyFile) {
		t.Errorf("来源说明应该指向密钥文件,实际: %q", KeySource())
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读 %s: %v", p, err)
	}
	return b
}
