package secret

import (
	"os"
	"path/filepath"
	"testing"
)

// 这是会真丢数据的那一类 bug，必须有测试钉住。
//
// 现场：docker compose 里写 `OVH_DB_KEY: ${OVH_DB_KEY:-}`，宿主机没定义时
// 容器里就是一个**空但已设置**的 OVH_DB_KEY。godotenv 不覆盖已存在的变量，
// 于是 .env 里的真密钥永远读不进来 —— 每次启动都判定"没有密钥"、
// 生成一把新的、再追加进 .env。之前加密的 OVH 凭据和 Telegram Token
// 从此永久解不开，而用户看到的只是"账户怎么没了"。
//
// 实测过：容器重启两次，/data/.env 里就有两行 OVH_DB_KEY。
func TestExistingKeyInEnvFileIsReused(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	// 第一次：没有任何密钥 → 生成并写进 .env
	t.Setenv("OVH_DB_KEY", "")
	os.Unsetenv("OVH_DB_KEY")
	k1, err := resolveKey(dir, envPath)
	if err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}
	raw1, _ := os.ReadFile(envPath)

	// 第二次：模拟容器里那个**空但已设置**的环境变量
	t.Setenv("OVH_DB_KEY", "")
	k2, err := resolveKey(dir, envPath)
	if err != nil {
		t.Fatalf("二次解析失败: %v", err)
	}

	if string(k1) != string(k2) {
		t.Fatal("重启后拿到了不同的密钥 —— 之前加密的凭据将永久解不开")
	}
	raw2, _ := os.ReadFile(envPath)
	if len(raw2) != len(raw1) {
		t.Fatalf("往 .env 里又追加了一把密钥（%d → %d 字节）", len(raw1), len(raw2))
	}
}

// 同一个字符串走环境变量和走文件必须得到同一把密钥 ——
// 两边解析规则分叉的话，换一种提供方式凭据就解不开了。
func TestEnvAndFileKeyAgree(t *testing.T) {
	const passphrase = "my-long-passphrase-not-hex-not-base64"
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte(KeyEnv+"="+passphrase+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("OVH_DB_KEY")
	fromFile, err := resolveKey(dir, envPath)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("OVH_DB_KEY", passphrase)
	fromEnv, err := resolveKey(dir, envPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(fromFile) != string(fromEnv) {
		t.Fatal("同一个口令，从文件读和从环境变量读得到了不同的密钥")
	}
}

// .env 里有多行同名 key（历史上那个 bug 追加出来的）时取最后一行。
func TestReadKeyTakesLastLine(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := "# 注释\n" + KeyEnv + "=firstkey\n\n# 又一段注释\n" + KeyEnv + "=secondkey\n"
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok, err := readKeyFromEnvFile(envPath)
	if !ok || err != nil {
		t.Fatal("应当读到密钥")
	}
	want := normalizeKey("secondkey")
	if string(got) != string(want) {
		t.Fatal("有重复行时应当取最后一行")
	}
}

// 文件不存在 / 里面没有密钥时要如实返回 false，不能瞎编一把
func TestReadKeyMissing(t *testing.T) {
	dir := t.TempDir()
	if _, ok, err := readKeyFromEnvFile(filepath.Join(dir, "nope.env")); ok || err != nil {
		t.Fatal("文件不存在时不该返回密钥")
	}
	p := filepath.Join(dir, ".env")
	os.WriteFile(p, []byte("# 只有注释\nOTHER=1\n"), 0o600)
	if _, ok, err := readKeyFromEnvFile(p); ok || err != nil {
		t.Fatal("没有密钥行时不该返回密钥")
	}
}

func TestUnreadableKeySourceDoesNotGenerateReplacement(t *testing.T) {
	t.Setenv(KeyEnv, "")
	for _, name := range []string{keyFile, ".env"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := resolveKey(dir, filepath.Join(dir, ".env")); err == nil {
				t.Fatal("unreadable key source was silently replaced")
			}
			other := ".env"
			if name == other {
				other = keyFile
			}
			if _, err := os.Stat(filepath.Join(dir, other)); !os.IsNotExist(err) {
				t.Fatal("replacement key was written", err)
			}
		})
	}
}
