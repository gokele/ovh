package handlers

// Telegram 文案总览。
//
// 这个 bot 能花钱,所以每一句发给用户的话都要能单独审:
// 「下单成功」到底是不是已经付款了、「抢 1 台」到底会下几单、
// 按钮过期了说的是什么。以前想看这些只能真的触发一次补货。
//
// 跑法:
//   go test ./internal/handlers/ -run TestDumpAllTemplates -v
//   go test ./internal/handlers/ -run TestDumpFlowTemplates -v

import (
	"fmt"
	"testing"

	"github.com/ovh-buy/server/internal/types"
)

func banner(s string) { fmt.Printf("\n\n════════ %s ════════\n", s) }

func TestDumpAllTemplates(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	addTestAccount(t, st)

	// —— 造一批真实点的数据 ——
	st.QueueMu.Lock()
	st.Queue = []types.QueueItem{
		{ID: "a1b2c3d4-1111", AccountID: "acc-test", PlanCode: "24ska01", Datacenter: "waw",
			Status: "running", FailureCount: 2},
		{ID: "e5f6a7b8-2222", AccountID: "acc-test", PlanCode: "24sk602", Datacenter: "",
			Status: "pending"},
	}
	st.QueueMu.Unlock()

	msg := "指定机房 GRA 无货"
	st.HistoryMu.Lock()
	st.History = []types.PurchaseHistoryEntry{
		{ID: "h1", PlanCode: "24ska01", Datacenter: "waw", Status: "success",
			OrderID: "108523471", OrderStatus: "notPaid", PurchaseTime: types.NowISO()},
		{ID: "h2", PlanCode: "24sk602", Datacenter: "gra", Status: "failed",
			ErrorMessage: &msg, PurchaseTime: types.NowISO()},
	}
	st.HistoryMu.Unlock()

	mon.AddSubscription("24ska01", []string{"waw"}, true, false, "KS-5", nil, nil,
		true, 1, "acc-test", []string{"ram-32g-ecc-2400", "softraid-2x2000sa"}, "")

	banner("/help")
	fmt.Println(helpText())

	banner("/status")
	fmt.Println(statusText(st, mon))

	banner("/queue")
	fmt.Println(queueText(st))

	banner("/cancel a1b2c3d4")
	fmt.Println(cancelText(st, []string{"a1b2c3d4"}))

	banner("/cancel（没带参数）")
	fmt.Println(cancelText(st, nil))

	banner("/cancel zzzz（匹配不到）")
	fmt.Println(cancelText(st, []string{"zzzz"}))

	banner("/subs")
	fmt.Println(subsText(st, mon))

	banner("/recent")
	fmt.Println(recentText(st))

	banner("/accounts")
	fmt.Println(accountsText(st, int64(1), 1))

	banner("/watch（没带参数 = 用法）")
	fmt.Println(watchText(st, mon, nil))

	banner("/watch 24sk602 gra x2（跳过按钮，直接建）")
	fmt.Println(watchText(st, mon, []string{"24sk602", "gra", "x2"}))

	banner("/watch 24sk602 这不是机房（参数看不懂）")
	fmt.Println(watchText(st, mon, []string{"24sk602", "这不是机房"}))

	banner("/unwatch 24sk602")
	fmt.Println(unwatchText(st, mon, []string{"24sk602"}))

	banner("/unwatch 没盯过的")
	fmt.Println(unwatchText(st, mon, []string{"nope"}))

	banner("不认识的命令")
	handleCommand(st, mon, int64(1), 1, "/blah")
	fmt.Println("❓ 不认识的命令: /blah\n\n发 /help 看能用什么。")
}

func TestDumpFlowTemplates(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	addTestAccount(t, st)

	fmt.Println("\n════════ /watch 24sk602 → 第 1 步：挑配置 ════════")
	fmt.Print("🔧 24sk602 有 3 套配置，盯哪一套？\n\n")
	fmt.Print("通知和自动下单都是按配置逐套触发的 —— 选「全部配置」意味着每套补货都会各下一次单。\n\n")
	cfgs := []configChoice{
		{Label: "32GB ECC DDR4-2400 + 2x2TB HDD", InStock: 0},
		{Label: "32GB ECC DDR4-2400 + 2x480GB SSD", InStock: 2},
		{Label: "64GB ECC DDR4-2400 + 2x2TB HDD", InStock: 0},
	}
	for _, l := range configLabels(cfgs) {
		fmt.Println("   [ " + l + " ]")
	}

	fmt.Println("\n════════ 第 2 步：挑账户（只有 1 个账户时跳过）════════")
	fmt.Print("👤 用哪个账户？\n\n")
	fmt.Print("OVH 的 EU / US / CA 是三套独立系统，同一台机器在不同区是不同的型号代码。选错区的账户会永远抢不到，而且看不出原因。\n\n")
	fmt.Println("   [ 主账户（IE） 默认 ]")
	fmt.Println("   [ 美区账户（US） ]")

	fmt.Println("\n════════ 第 3 步：补货时怎么办 ════════")
	f := &watchFlow{
		PlanCode:     "24sk602",
		PickedConfig: "32GB ECC DDR4-2400 + 2x480GB SSD",
		AccountLabel: "主账户（IE） 默认",
	}
	askActionPreview(f)
	for _, a := range actionChoices {
		fmt.Println("   [ " + a.Label + " ]")
	}

	fmt.Println("\n════════ 选完（自动抢 1 台）════════")
	f.PickedOptions = []string{"ram-32g-ecc-2400", "softraid-2x480ssd"}
	f.AccountID = "acc-test"
	fmt.Println(finishWatchFlow(st, mon, f, 1))

	fmt.Println("\n════════ 选完（只通知）════════")
	f2 := &watchFlow{PlanCode: "24sk603", PickedConfig: "64G + 2x960 SSD",
		AccountLabel: "主账户（IE） 默认", AccountID: "acc-test"}
	fmt.Println(finishWatchFlow(st, mon, f2, 0))

	fmt.Println("\n════════ 按钮过期 ════════")
	fmt.Println("⌛ 选择超时（超过 10 分钟），请重新发一次 /watch")
}
