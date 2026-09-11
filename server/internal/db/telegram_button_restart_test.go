package db

import (
	"testing"
	"time"
)

// 一键下单按钮必须跨进程重启可用。
//
// 这是针对「ShortID 纯内存映射导致服务重启后所有旧通知按钮点击即崩溃」这条
// 报告写的复现:第一次 Open 模拟发通知的那个进程,第二次 Open 同一目录模拟
// 重启后的新进程 —— 它的内存缓存是空的,能不能认领全看 SQLite 里有没有。
func TestTelegramButtonSurvivesRestart(t *testing.T) {
	dir := t.TempDir()

	// ── 进程 1:发通知,按钮落库 ──
	d1, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	const id = "0b6d2a1e-restart-test"
	created := float64(time.Now().Unix())
	if err := d1.UpsertTelegramButtonForAccount(id, "acc-eu", "24sk602", "gra",
		[]string{"ram-64g-ecc-2133-24sk60", "softraid-2x480ssd-24sk60"},
		map[string]interface{}{"cpu": "Xeon-D 1520"}, created); err != nil {
		t.Fatal(err)
	}
	if err := d1.Close(); err != nil {
		t.Fatal(err)
	}

	// ── 进程 2:重启。全新句柄,没有任何内存状态 ──
	d2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()

	row, ok, err := d2.ClaimTelegramButton(id)
	if err != nil {
		t.Fatalf("重启后认领按钮报错: %v", err)
	}
	if !ok {
		t.Fatal("重启后按钮认领失败 —— 映射没有持久化,报告成立")
	}
	if row.PlanCode != "24sk602" || row.Datacenter != "gra" || row.AccountID != "acc-eu" {
		t.Fatalf("重启后读回的下单参数不对: %+v", row)
	}
	if opts := ParseTelegramButtonOptions(row.Options); len(opts) != 2 {
		t.Fatalf("options 没完整持久化: %v", opts)
	}

	// 一次性:同一颗按钮再点一次必须被拒(防重放)
	if _, ok2, _ := d2.ClaimTelegramButton(id); ok2 {
		t.Fatal("同一颗按钮被认领了两次 —— 重放防护失效")
	}
	// 但它得还查得到,回调层靠这个区分「已用过」和「不存在」
	if _, exists, _ := d2.GetTelegramButton(id); !exists {
		t.Fatal("已消费的按钮应仍可查到,否则用户会收到「不存在」而不是「已使用过」")
	}
}
