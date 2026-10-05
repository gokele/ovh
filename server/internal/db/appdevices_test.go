package db

import (
	"errors"
	"testing"
	"time"
)

// App 配对全链路:生成 → 兑换 → 令牌鉴权 → 吊销 → 吊销后失效。
// 任何一环断掉,手机 App 就会出现"配对成功但连不上"或"吊销了还能用"这两种最难查的状态。
func TestAppPairingLifecycle(t *testing.T) {
	dir := t.TempDir()
	database, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	// ① 生成配对码
	code, expiresAt, err := database.CreatePairingCode()
	if err != nil || len(code) != 8 {
		t.Fatalf("生成配对码失败: %v code=%q", err, code)
	}
	if time.Until(expiresAt) <= 0 || time.Until(expiresAt) > 2*time.Minute+5*time.Second {
		t.Fatalf("有效期不是 2 分钟左右: %v", expiresAt)
	}

	// ② 兑换拿令牌
	token, deviceID, err := database.RedeemPairingCode(code, "kele 的 iPhone")
	if err != nil || token == "" || deviceID == 0 {
		t.Fatalf("兑换失败: %v token=%q id=%d", err, token, deviceID)
	}

	// ③ 令牌能过鉴权
	if _, ok := database.DeviceTokenValid(token); !ok {
		t.Fatal("有效令牌被拒")
	}

	// ④ 同码二次兑换必须被拒(一次性)
	if _, _, err := database.RedeemPairingCode(code, "重放者"); !errors.Is(err, ErrPairingCodeUsed) {
		t.Fatalf("重放未被拦: %v", err)
	}

	// ⑤ 吊销后令牌立即失效
	if err := database.RevokeDevice(deviceID); err != nil {
		t.Fatalf("吊销: %v", err)
	}
	if _, ok := database.DeviceTokenValid(token); ok {
		t.Fatal("已吊销的令牌仍然有效 —— 手机丢了也撤不掉,等于没有吊销功能")
	}

	// ⑥ 设备列表能看到这台(含 revoked 标记)
	devs, err := database.ListDevices()
	if err != nil || len(devs) != 1 || !devs[0].Revoked.Valid {
		t.Fatalf("设备列表不对: %v %v", devs, err)
	}
}

// 不存在的码要给"不存在"而不是笼统失败 —— 三种死法对应三条不同的用户指引
func TestPairingCodeNotFound(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, _, err := database.RedeemPairingCode("ZZZZZZZZ", "x"); !errors.Is(err, ErrPairingCodeNotFound) {
		t.Fatalf("want NotFound, got %v", err)
	}
}

// 过期码:直接改库里的 expires_at 模拟超时
func TestPairingCodeExpired(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	code, _, err := database.CreatePairingCode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE app_pairing_codes SET expires_at = ? WHERE code = ?`,
		time.Now().Add(-time.Second).UTC(), code); err != nil {
		t.Fatal(err)
	}
	if _, _, err := database.RedeemPairingCode(code, "x"); !errors.Is(err, ErrPairingCodeExpired) {
		t.Fatalf("want Expired, got %v", err)
	}
}

// 并发兑换同一码:10 个 goroutine 同时抢,只允许 1 个成功 —— 原子认领是安全底线
func TestPairingCodeConcurrentRedeem(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	code, _, err := database.CreatePairingCode()
	if err != nil {
		t.Fatal(err)
	}
	const n = 10
	ch := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, _, err := database.RedeemPairingCode(code, "并发")
			ch <- err
		}()
	}
	ok := 0
	for i := 0; i < n; i++ {
		if <-ch == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("并发兑换成功 %d 次,必须恰好 1 次", ok)
	}
}

// 并发兑换压力版:50 个 goroutine,恰好 1 成功、其余全部 ErrPairingCodeUsed
func TestPairingCodeConcurrentRedeem50(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	code, _, err := database.CreatePairingCode()
	if err != nil {
		t.Fatal(err)
	}
	const n = 50
	type result struct {
		err error
	}
	ch := make(chan result, n)
	for i := 0; i < n; i++ {
		go func() {
			_, _, e := database.RedeemPairingCode(code, "并发50")
			ch <- result{e}
		}()
	}
	ok, used := 0, 0
	for i := 0; i < n; i++ {
		r := <-ch
		switch {
		case r.err == nil:
			ok++
		case errors.Is(r.err, ErrPairingCodeUsed):
			used++
		default:
			t.Fatalf("预期只有 成功/已用 两种结果, got: %v", r.err)
		}
	}
	if ok != 1 || used != n-1 {
		t.Fatalf("50 并发: 成功 %d 已用 %d,必须 1/%d", ok, used, n-1)
	}
}
