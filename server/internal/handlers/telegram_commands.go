package handlers

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// Telegram 命令。
//
// 以前这个 bot 只认一种输入:恰好符合下单格式的那一行文本。别的一律
// Debug 一行日志然后静默丢掉 —— 用户发 "hi"、发 "?"、发错格式,
// 屏幕上什么都不会发生,他没有任何办法知道自己该发什么。
// 一个能花钱下单的 bot,连 /help 都没有是说不过去的。
//
// 这里的命令都只读或只做"撤销"(取消任务),不新增花钱的动作 ——
// 花钱的入口仍然只有两个:上架通知里的一键下单按钮,和下单格式的文本。

// tgMaxReplyLen Telegram 单条消息上限 4096 字符,留点余量。
const tgMaxReplyLen = 3800

// tgMaxListItems 列表类命令最多列几条,超了给个总数。
const tgMaxListItems = 15

// handleCommand 处理 / 开头的命令。返回 false 表示这不是命令,交给下单解析。
func handleCommand(state *app.State, mon *monitor.Monitor, chatID interface{}, messageID int64, text string) bool {
	if !strings.HasPrefix(text, "/") {
		return false
	}
	// Telegram 群里命令会带 @botname 后缀
	fields := strings.Fields(text)
	cmd := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	if i := strings.Index(cmd, "@"); i >= 0 {
		cmd = cmd[:i]
	}
	args := fields[1:]

	var reply string
	switch cmd {
	case "start", "help", "h", "?":
		reply = helpText()
	case "status", "s":
		reply = statusText(state, mon)
	case "queue", "q":
		reply = queueText(state)
	case "cancel":
		reply = cancelText(state, args)
	case "interval", "iv":
		reply = intervalText(state, args)
	case "watch", "w":
		// 带了 x<数量> = 用户明确知道自己要什么,直接建,不打断他。
		// 否则走按钮流程:让他挑配置和账户 —— 这两件事不挑就等于默默替他决定,
		// 而它们直接决定会不会抢到、以及会下多少单。
		if hasExplicitQuantity(args) || len(args) == 0 {
			reply = watchText(state, mon, args)
		} else if startWatchFlow(state, mon, chatID, messageID, args[0], dcArgs(args[1:])) {
			return true // 流程自己回复了
		} else {
			reply = watchText(state, mon, args)
		}
	case "unwatch", "uw":
		reply = unwatchText(state, mon, args)
	case "accounts", "acc":
		reply = accountsText(state, chatID, messageID)
	case "subs", "sub":
		reply = subsText(state, mon)
	case "recent", "history":
		reply = recentText(state)
	default:
		reply = "❓ 不认识的命令: /" + cmd + "\n\n发 /help 看能用什么。"
	}
	if reply != "" {
		telegram.SendReply(state, chatID, clampReply(reply), messageID)
	}
	return true
}

func clampReply(s string) string {
	if len(s) <= tgMaxReplyLen {
		return s
	}
	return s[:tgMaxReplyLen] + "\n…(内容过长已截断，完整信息请看控制台)"
}

func helpText() string {
	var b strings.Builder
	b.WriteString("🤖 OVH 抢购助手\n\n")
	b.WriteString("【下单】直接发一行文本：\n")
	b.WriteString("  <型号> [机房] [数量] [配置,逗号分隔]\n\n")
	b.WriteString("例子：\n")
	b.WriteString("  24sk602            → 所有有货机房各 1 台\n")
	b.WriteString("  24sk602 gra        → 只买 gra\n")
	b.WriteString("  24sk602 gra 2      → gra 买 2 台\n")
	b.WriteString("  24sk602 gra 2 ram-64g,softraid-2x480ssd\n\n")
	b.WriteString(fmt.Sprintf("数量上限 %d 台/次，一条消息最多创建 %d 个任务。\n",
		telegram.MaxOrderQuantity, telegram.MaxOrderFanout))
	b.WriteString("机房代码是 3-4 位小写字母（gra / rbx / sbg / bhs / waw…）。\n\n")
	b.WriteString("【指定账户】在命令里加 @：\n")
	b.WriteString("  24sk602 gra @us    用美区账户下\n")
	b.WriteString("  24sk602 gra @2     用 /accounts 里第 2 个账户\n")
	b.WriteString("  24sk602 gra @all   每个能买的账户各下一单（抢稀缺机器时翻倍机会）\n")
	b.WriteString("不写 @ 的话我按型号查它在哪个账户的目录里，然后让你确认一次。\n\n")
	b.WriteString("⚠️ 上面这种是「现在就买」，机器当下没货会直接失败。\n")
	b.WriteString("   想等补货请用 /watch。\n\n")
	b.WriteString("【盯补货】机器现在没货时用这个：\n")
	b.WriteString("  /watch 24sk602         我用按钮让你挑配置和账户\n")
	b.WriteString("  /watch 24sk602 gra     只盯 gra，其余照样按钮挑\n")
	b.WriteString("  /watch 24sk602 gra x1  跳过按钮，直接盯全部配置自动抢 1 台\n")
	b.WriteString("  /unwatch 24sk602       不盯了\n\n")
	b.WriteString("⚠️ 一个型号底下常有好几套内存/存储组合，而补货通知和自动下单是\n")
	b.WriteString("   **按配置逐套**触发的。不挑配置就是每套都要，\n")
	b.WriteString("   「抢 1 台」会变成「每套配置在每个机房各抢 1 台」。\n\n")
	b.WriteString("【命令】\n")
	b.WriteString("  /status   监控与队列总览\n")
	b.WriteString("  /queue    正在抢的任务\n")
	b.WriteString("  /cancel <任务号|all>  取消任务\n")
	b.WriteString("  /interval [秒]   看/改新任务的默认重试间隔\n")
	b.WriteString("  /subs     在盯哪些型号\n")
	b.WriteString("  /accounts 看/切当前下单账户\n")
	b.WriteString("  /recent   最近的抢购结果\n\n")
	b.WriteString("💡 上架通知里的按钮可以直接下单，比打字快。\n")
	b.WriteString("💡 多账户的话先 /accounts 确认当前用的是哪个 —— ")
	b.WriteString("三个大区的型号代码不一样，选错区永远抢不到。\n")
	return b.String()
}

func statusText(state *app.State, mon *monitor.Monitor) string {
	var b strings.Builder
	b.WriteString("📊 当前状态\n\n")

	if mon != nil {
		subs := mon.Snapshot()
		running := "已停止"
		if st, ok := mon.Status()["running"].(bool); ok && st {
			running = "运行中"
		}
		b.WriteString(fmt.Sprintf("监控：%s，%d 个订阅\n", running, len(subs)))
		// 查不到库存的订阅要单独说 —— 那是监控已经失效但看上去一切正常的状态
		bad := 0
		for _, s := range subs {
			if s.LastCheckError != "" {
				bad++
			}
		}
		if bad > 0 {
			b.WriteString(fmt.Sprintf("⚠️ 其中 %d 个订阅最近一次检查失败，发 /subs 看详情\n", bad))
		}
	}

	// 当前账户必须在总览里 —— 它决定下单落到哪个区,而选错区的表现只是"抢不到"
	if acc, explicit := telegram.ActiveAccount(state); acc.ID != "" {
		b.WriteString("当前账户：" + telegram.AccountLabel(acc))
		if !explicit {
			b.WriteString("（默认）")
		}
		b.WriteString("\n")
	}

	pending, running, done, failed := queueCounts(state)
	b.WriteString(fmt.Sprintf("队列：%d 进行中，%d 等待，%d 成功，%d 失败\n", running, pending, done, failed))

	ok, fail := state.CountPurchase()
	b.WriteString(fmt.Sprintf("历史：抢到 %d 单，失败 %d 次\n", ok, fail))

	// 未付款的单子最要紧 —— 逾期会自动作废,机器白抢
	if n, earliest := unpaidOrders(state); n > 0 {
		b.WriteString(fmt.Sprintf("\n💳 有 %d 单还没付款", n))
		if earliest != "" {
			b.WriteString("（最早一单下单于 " + earliest + "）")
		}
		b.WriteString("\n逾期未付会自动作废，尽快去 OVH 控制面板付款。\n")
	}

	if fails := state.LoadFailures(); len(fails) > 0 {
		names := make([]string, 0, len(fails))
		for k := range fails {
			names = append(names, k)
		}
		sort.Strings(names)
		b.WriteString("\n🚨 启动时这些数据没读出来，本次运行不会写它们：" + strings.Join(names, "、") + "\n")
	}
	return b.String()
}

func queueCounts(state *app.State) (pending, running, done, failed int) {
	state.QueueMu.Lock()
	defer state.QueueMu.Unlock()
	for _, it := range state.Queue {
		switch it.Status {
		case "running":
			running++
		case "pending", "paused":
			pending++
		case "completed", "success":
			done++
		case "failed":
			failed++
		}
	}
	return
}

// unpaidOrders 数一数成功但还没付款的单。
func unpaidOrders(state *app.State) (int, string) {
	state.HistoryMu.Lock()
	defer state.HistoryMu.Unlock()
	n := 0
	earliest := ""
	for _, h := range state.History {
		if h.Status != "success" || h.OrderID == "" {
			continue
		}
		// delivered / cancelled 是终态,不用再催
		if h.OrderStatus == "delivered" || h.OrderStatus == "cancelled" {
			continue
		}
		n++
		if t, ok := types.ParseTS(h.PurchaseTime); ok {
			s := t.Format("01-02 15:04")
			if earliest == "" || s < earliest {
				earliest = s
			}
		}
	}
	return n, earliest
}

func queueText(state *app.State) string {
	state.QueueMu.Lock()
	items := make([]types.QueueItem, 0, len(state.Queue))
	for _, it := range state.Queue {
		if it.Status == "running" || it.Status == "pending" || it.Status == "paused" {
			items = append(items, it)
		}
	}
	state.QueueMu.Unlock()

	if len(items) == 0 {
		return "📭 当前没有正在抢的任务。\n\n发 /help 看怎么下单。"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🛒 正在抢的任务（%d 个）\n\n", len(items)))
	for i, it := range items {
		if i >= tgMaxListItems {
			b.WriteString(fmt.Sprintf("\n…还有 %d 个，完整列表见控制台。\n", len(items)-tgMaxListItems))
			break
		}
		dc := it.Datacenter
		if dc == "" {
			dc = "任意机房"
		}
		b.WriteString(fmt.Sprintf("%d. %s @ %s\n", i+1, it.PlanCode, strings.ToUpper(dc)))
		b.WriteString(fmt.Sprintf("   状态 %s · 每 %d 秒重试", it.Status,
			types.ClampRetryInterval(it.RetryInterval, state.Config.RetryInterval())))
		if it.FailureCount > 0 {
			b.WriteString(fmt.Sprintf(" · 已失败 %d 次", it.FailureCount))
		}
		if acc, ok := state.FindAccount(it.AccountID); ok {
			b.WriteString(" · " + acc.Name)
		}
		b.WriteString("\n   取消：/cancel " + shortID(it.ID) + "\n")
	}
	b.WriteString("\n全部取消：/cancel all")
	return b.String()
}

// shortID 任务 ID 是 uuid,在手机上让人照着打完整的不现实。
// 取前 8 位做前缀匹配 —— 冲突概率极低,真撞上了会让用户用更长的前缀。
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// intervalText /interval 看或改新建任务的默认重试间隔。
// 只改默认值:已经在跑的任务各自带着自己的间隔,要改单个任务去网页「抢购队列」里点秒数。
func intervalText(state *app.State, args []string) string {
	cur := state.Config.RetryInterval()
	quick := state.Config.QuickOrderRetryInterval()
	if len(args) == 0 {
		return fmt.Sprintf("⏱ 新任务默认重试间隔：%d 秒\n   监控自动下单间隔：%d 秒\n\n"+
			"改默认值：/interval <秒>（%d ~ %d）\n单个任务的间隔到网页「抢购队列」里点秒数改。",
			cur, quick, types.MinRetryInterval, types.MaxRetryInterval)
	}
	n, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || n < types.MinRetryInterval || n > types.MaxRetryInterval {
		return fmt.Sprintf("间隔要是 %d ~ %d 之间的整数秒，例如 /interval 60",
			types.MinRetryInterval, types.MaxRetryInterval)
	}
	cfg := state.Config.Get()
	cfg.DefaultRetryInterval = n
	if err := state.Config.Set(cfg); err != nil {
		return "❌ 保存失败：" + err.Error()
	}
	state.Logger.Info(fmt.Sprintf("Telegram 把默认重试间隔从 %d 改为 %d 秒", cur, n), "telegram")
	return fmt.Sprintf("✅ 默认重试间隔已改为 %d 秒（之前 %d 秒）。\n只影响之后新建的任务。", n, cur)
}

func cancelText(state *app.State, args []string) string {
	if len(args) == 0 {
		return "用法：/cancel <任务号> 或 /cancel all\n\n发 /queue 看任务号。"
	}
	target := strings.ToLower(strings.TrimSpace(args[0]))

	state.QueueMu.Lock()
	var matched []int
	for i := range state.Queue {
		it := state.Queue[i]
		if it.Status != "running" && it.Status != "pending" && it.Status != "paused" {
			continue
		}
		if target == "all" || strings.HasPrefix(strings.ToLower(it.ID), target) {
			matched = append(matched, i)
		}
	}
	if len(matched) == 0 {
		state.QueueMu.Unlock()
		return "没找到匹配的进行中任务：" + target + "\n\n发 /queue 看当前任务号。"
	}
	if target != "all" && len(matched) > 1 {
		state.QueueMu.Unlock()
		return fmt.Sprintf("任务号 %s 匹配到 %d 个任务，太短了。请多打几位。", target, len(matched))
	}
	// 倒着删,避免前面的删除把后面的下标挪掉
	killed := make([]string, 0, len(matched))
	for i := len(matched) - 1; i >= 0; i-- {
		idx := matched[i]
		it := state.Queue[idx]
		killed = append(killed, it.PlanCode+" @ "+strings.ToUpper(orAny(it.Datacenter)))
		state.MarkTaskDeleted(it.ID) // 同时取消它正在进行的下单
		state.Queue = append(state.Queue[:idx], state.Queue[idx+1:]...)
	}
	state.QueueMu.Unlock()

	// 落库失败必须说 —— 不说的话用户以为取消了,重启后任务原地复活继续抢
	if err := state.SaveQueue(); err != nil {
		state.Logger.Error("Telegram 取消任务后保存队列失败: "+err.Error(), "telegram")
		return fmt.Sprintf("⚠️ 已从运行中的队列移除 %d 个任务，但没能写进数据库，重启后会重新出现：\n%s",
			len(killed), err.Error())
	}
	state.Logger.Info(fmt.Sprintf("Telegram 取消了 %d 个抢购任务", len(killed)), "telegram")

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🛑 已取消 %d 个任务\n\n", len(killed)))
	for i, k := range killed {
		if i >= tgMaxListItems {
			b.WriteString(fmt.Sprintf("…还有 %d 个\n", len(killed)-tgMaxListItems))
			break
		}
		b.WriteString("  • " + k + "\n")
	}
	return b.String()
}

func orAny(dc string) string {
	if dc == "" {
		return "任意机房"
	}
	return dc
}

// accountsText 列账户 + 让用户切。
//
// 返回空串表示已经用按钮自己回复了。
//
// 为什么要能切:planCode 是分区的(EU / US / CA 三套目录基本不重合),
// 用错区的账户下单,OVH 返回的是 200 + 空数组而不是报错 ——
// 表现就是"永远抢不到",用户完全看不出是账户选错了。
// 以前 TG 这边连选都不能选,一律落默认账户。
func accountsText(state *app.State, chatID interface{}, messageID int64) string {
	accs := listAccounts(state)
	if len(accs) == 0 {
		return "还没有配置 OVH 账户。请到控制台「设置 → OVH 账户」添加。"
	}
	cur, explicit := telegram.ActiveAccount(state)

	var b strings.Builder
	b.WriteString("👤 当前账户：" + telegram.AccountLabel(cur))
	if !explicit {
		b.WriteString("\n（没单独选过，用的是默认账户）")
	}
	b.WriteString("\n\n这个账户决定文本下单和 /watch 落到哪里。\n")
	b.WriteString("三个大区的机型目录互不相通，同一台机器在不同区的型号代码不一样 ——\n")
	b.WriteString("选错区的后果是永远抢不到，而且看不出原因。\n")

	if len(accs) == 1 {
		// 只有一个账户,没什么可切的
		return b.String()
	}

	b.WriteString("\n下单时可以直接指定：\n")
	for i, a := range accs {
		b.WriteString(fmt.Sprintf("  [@%d] 或 [@%s]  %s\n", i+1, accountShortName(a), a.Name))
	}
	b.WriteString("  [@all]  每个能买的账户各下一单\n")
	b.WriteString("\n要换默认的就点下面：")
	f := &watchFlow{Step: stepSwitchAccount, Accounts: accs}
	tok := putFlow(f)
	labels := make([]string, 0, len(accs))
	for _, a := range accs {
		l := telegram.AccountLabel(a)
		if a.ID == cur.ID {
			l = "✅ " + l
		}
		labels = append(labels, l)
	}
	telegram.SendKeyboard(state, chatID, messageID, b.String(), flowKeyboard(tok, labels))
	return ""
}

func subsText(state *app.State, mon *monitor.Monitor) string {
	if mon == nil {
		return "监控未初始化。"
	}
	subs := mon.Snapshot()
	if len(subs) == 0 {
		return "📭 还没有监控订阅。到控制台「服务器监控」添加。"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🔔 监控订阅（%d 个）\n\n", len(subs)))
	for i, s := range subs {
		if i >= tgMaxListItems {
			b.WriteString(fmt.Sprintf("\n…还有 %d 个。\n", len(subs)-tgMaxListItems))
			break
		}
		b.WriteString("  • " + s.PlanCode)
		if len(s.Datacenters) > 0 {
			b.WriteString(" @ " + strings.ToUpper(strings.Join(s.Datacenters, "/")))
		}
		b.WriteString("\n")
		// 配置和账户必须列出来 —— 这两件事决定了会不会抢到、以及会下多少单,
		// 而用户是在几天前点按钮选的,不会记得。
		if len(s.Options) > 0 {
			b.WriteString("    配置：" + strings.Join(s.Options, " + ") + "\n")
		} else {
			b.WriteString("    配置：全部（每套补货各触发一次）\n")
		}
		if s.AutoOrder && s.AutoOrderAccountID != "" {
			label := s.AutoOrderAccountID
			if acc, ok := state.FindAccount(s.AutoOrderAccountID); ok {
				label = acc.Name + "（" + strings.ToUpper(acc.Zone) + "）"
			}
			b.WriteString(fmt.Sprintf("    补货自动抢 %d 台 · 账户 %s\n", s.Quantity, label))
		}
		// 检查失败要显式说:表现和"一直无货"一模一样,不说用户永远发现不了
		if s.LastCheckError != "" {
			b.WriteString("    ⚠️ 最近一次检查失败：" + truncate(s.LastCheckError, 80) + "\n")
		}
	}
	return b.String()
}

func recentText(state *app.State) string {
	state.HistoryMu.Lock()
	n := len(state.History)
	start := n - tgMaxListItems
	if start < 0 {
		start = 0
	}
	recent := make([]types.PurchaseHistoryEntry, 0, n-start)
	for i := n - 1; i >= start; i-- {
		recent = append(recent, state.History[i])
	}
	state.HistoryMu.Unlock()

	if len(recent) == 0 {
		return "📭 还没有抢购记录。"
	}
	var b strings.Builder
	b.WriteString("📜 最近的抢购结果\n\n")
	for _, h := range recent {
		icon := "❌"
		if h.Status == "success" {
			icon = "✅"
		}
		b.WriteString(icon + " " + h.PlanCode + " @ " + strings.ToUpper(orAny(h.Datacenter)))
		if t, ok := types.ParseTS(h.PurchaseTime); ok {
			b.WriteString("  " + t.Format("01-02 15:04"))
		}
		b.WriteString("\n")
		if h.Status == "success" {
			// 付款状态是这里最该说的一件事:抢到但没付款,逾期就作废了
			switch h.OrderStatus {
			case "delivered":
				b.WriteString("    订单 " + h.OrderID + " · 已交付\n")
			case "cancelled":
				b.WriteString("    订单 " + h.OrderID + " · 已取消\n")
			case "":
				b.WriteString("    订单 " + h.OrderID + " · ⚠️ 付款状态未知，请去面板确认\n")
			default:
				b.WriteString("    订单 " + h.OrderID + " · " + h.OrderStatus + "（未付款会逾期作废）\n")
			}
		} else if h.ErrorMessage != nil && *h.ErrorMessage != "" {
			b.WriteString("    " + truncate(*h.ErrorMessage, 90) + "\n")
		}
	}
	return b.String()
}

// hasExplicitQuantity 参数里有没有 x<数量>。
// 有就说明用户很清楚自己要什么,不该再拿按钮打断他。
func hasExplicitQuantity(args []string) bool {
	for _, a := range args {
		a = strings.ToLower(strings.TrimSpace(a))
		if len(a) > 1 && a[0] == 'x' {
			if _, err := strconv.Atoi(a[1:]); err == nil {
				return true
			}
		}
	}
	return false
}

// dcArgs 从参数里挑出机房代码,忽略其它。
func dcArgs(args []string) []string {
	out := []string{}
	for _, a := range args {
		a = strings.ToLower(strings.TrimSpace(a))
		if len(a) >= 3 && len(a) <= 4 && isLowerAlpha(a) {
			out = append(out, a)
		}
	}
	return out
}
