package db

import (
	"testing"

	"github.com/ovh-buy/server/internal/types"
)

// VPS 订阅的自动下单配置必须真的落库。
//
// 这四个字段(AutoOrder / Quantity / AutoPay / OS)在 types 里早就有,
// 但表里一直没列、行结构也没映射 —— 写入被静默丢弃,读回来全是零值。
// 后果:VPS 自动下单重启即失效(界面开关还亮着),而且删任意账户时
// reloadAfterAccountDelete 会用零值回灌内存,当场清零。
//
// 这个测试锁住往返一致性,防止以后加字段又忘了加列。
func TestVPSSubscriptionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	database, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	want := types.VPSSubscription{
		ID: "sub-1", PlanCode: "vps-2027-model2", OvhSubsidiary: "IE",
		Datacenters:  []string{"gra", "bhs"},
		MonitorLinux: true, MonitorWindows: false,
		NotifyAvailable: true, NotifyUnavailable: true,
		LastStatus: map[string]string{"gra": "available"},
		History:    []map[string]interface{}{},
		CreatedAt:  types.NowISO(),
		// 以下四个是曾经被静默丢弃的
		AutoOrderAccountID: "acc-1",
		AutoOrder:          true,
		Quantity:           3,
		AutoPay:            true,
		OS:                 "debian12_64",
	}
	if err := database.ReplaceVPSSubscriptions([]types.VPSSubscription{want}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err := database.ListVPSSubscriptions()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("期望 1 条,实际 %d", len(got))
	}
	g := got[0]
	if !g.AutoOrder {
		t.Error("AutoOrder 没存住 —— VPS 自动下单会在重启后静默失效")
	}
	if g.Quantity != 3 {
		t.Errorf("Quantity 没存住:期望 3,实际 %d", g.Quantity)
	}
	if !g.AutoPay {
		t.Error("AutoPay 没存住 —— 用户开了自动付款,重启后变成不付款")
	}
	if g.OS != "debian12_64" {
		t.Errorf("OS 没存住:期望 debian12_64,实际 %q —— 自动下单会用 OVH 默认系统", g.OS)
	}
	if g.AutoOrderAccountID != "acc-1" || g.PlanCode != want.PlanCode || !g.MonitorLinux {
		t.Errorf("其它字段也不对: %+v", g)
	}
}
