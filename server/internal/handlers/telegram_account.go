package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// 下单时该用哪个账户 —— 算出来，而不是让用户记着。
//
// 以前是"全局当前账户"那一套：切一次，之后所有操作跟着走。问题在于它是**模态**的：
// 三天前切到美区，今天敲 `24sk602 gra 2`（欧区机型），它就闷头落到美区。
// 而你在敲那条命令的那一刻，界面上根本看不见当前账户是谁。
// 更要命的是这种错不会报错：拿欧区 planCode 打美区接口，OVH 回 200 + 空数组，
// 表现就是"永远抢不到"，日志里也没有异常。
//
// 现在的顺序：
//  1. 用 planCode 反推大区（monitor.PlanAccount，带缓存 + 目录快捷判定 + 可用性探测兜底）
//  2. 当前账户如果正好在对的大区，优先用它 —— 这是给"同区多账户"当决胜局的
//  3. 算不出来（三区都没这个机型 / 没配对应大区的账户）→ 明确说清楚，不猜

// resolvedAccount 一次账户解析的结果。
type resolvedAccount struct {
	Account types.OVHAccount
	Region  string
	// Confident 是不是真的按 planCode 算出来的。
	// false = 退回了当前/默认账户，界面上必须说出来。
	Confident bool
	// Reason 一个账户都没有时的说明。
	Reason string
	// Ambiguous 同一个大区里有多个账户，需要用户挑一次。
	Ambiguous []types.OVHAccount
}

// resolveOrderAccount 决定这个 planCode 该落到哪个账户。
//
// 原则：**能算就算，算不出来也绝不拒绝**。
//
// 判不出来的常见原因是目录还没热，而不是真的没有合适的账户 ——
// 为这个拒绝挂单，等于让一次网络抖动毁掉一次补货。
// 所以判不出来时退回当前账户，但要把"没能确认"这件事写在回复里：
// 用户是唯一知道自己想买哪个区的人。
//
// 另外这里只查缓存目录、不做跨区探测：探测要逐个大区打 OVH，实测好几秒，
// 而补货那一刻每一秒都算数。真正的区域错配由监控每轮检查时坐实
// （LastCheckError → /subs 里能看到）。
func resolveOrderAccount(state *app.State, mon *monitor.Monitor, planCode string) resolvedAccount {
	if len(listAccounts(state)) == 0 {
		return resolvedAccount{Reason: "还没有配置 OVH 账户。请到控制台「设置 → OVH 账户」添加。"}
	}
	cur, _ := telegram.ActiveAccount(state)
	fallback := resolvedAccount{Account: cur, Region: strings.ToUpper(strings.TrimSpace(cur.Zone))}

	if mon == nil {
		return fallback
	}
	accID, region, _ := mon.PlanAccountFast(planCode, cur.ID)
	if accID == "" {
		return fallback
	}
	acc, ok := state.FindAccount(accID)
	if !ok {
		return fallback
	}

	// 同区多账户：光看 planCode 算不出来用哪个，让用户挑一次。
	// 当前账户已经在这个区时不问 —— 上面 prefer 已经选中它了。
	if inRegion := mon.AccountsInRegion(region); len(inRegion) > 1 && cur.ID != acc.ID {
		return resolvedAccount{Account: acc, Region: region, Confident: true, Ambiguous: inRegion}
	}
	return resolvedAccount{Account: acc, Region: region, Confident: true}
}

// explainAccountChoice 一句话说明这单为什么落在这个账户上。
//
// 必须说：账户选错的后果是"永远抢不到"且 OVH 不报错，
// 用户唯一能发现的机会就是下单那一刻看到它落在哪儿了。
//
// 尤其是"没能确认"那一支 —— 那正是最可能出错的情况，更要说。
func explainAccountChoice(r resolvedAccount, planCode string) string {
	if r.Account.ID == "" {
		return ""
	}
	if r.Confident {
		return fmt.Sprintf("账户：%s\n（%s 在这个账户的目录里，已自动选定）",
			telegram.AccountLabel(r.Account), planCode)
	}
	return fmt.Sprintf("账户：%s\n"+
		"⚠️ 没能确认 %s 属于哪个区，用的是当前账户。\n"+
		"   OVH 三个站点互不相通，用错区的账户下单不会报错，只会一直抢不到。\n"+
		"   要换账户发 /accounts。",
		telegram.AccountLabel(r.Account), planCode)
}

// askOrderAccount 同一大区有多个账户时，让用户挑一次。
//
// 挑完会记成当前账户，所以这个问题在同一个区里只会问一次；
// 换到别的区的机型时又会自动解析，不需要再切回来。
func askOrderAccount(state *app.State, chatID interface{}, messageID int64,
	info *telegram.OrderInfo, ra resolvedAccount) {

	f := &watchFlow{
		Step:     stepOrderAccount,
		Accounts: ra.Ambiguous,
		PlanCode: info.PlanCode,
		Order:    info,
	}
	tok := putFlow(f)

	labels := make([]string, 0, len(ra.Ambiguous))
	for _, a := range ra.Ambiguous {
		labels = append(labels, telegram.AccountLabel(a))
	}
	telegram.SendKeyboard(state, chatID, messageID, fmt.Sprintf(
		"👤 %s 属于 %s 区，而你在这个区有 %d 个账户 —— 用哪个下单？\n\n"+
			"（选完会记住，同一个区之后不再问；换别的区的机型会自动切）",
		info.PlanCode, ra.Region, len(ra.Ambiguous)), flowKeyboard(tok, labels))
}

// accountShortName 账户的短名，用在 `@xxx` 里。
//
// 用子公司而不是账户名：名字可能是中文、带空格、或者两个账户叫得很像，
// 打起来都不方便；子公司是两三个字母，而且正好对应"哪个区"——
// 用户真正在意的就是这个。
// 同一个子公司有多个账户时只能用序号区分，/accounts 里会把序号标出来。
func accountShortName(a types.OVHAccount) string {
	return strings.ToLower(strings.TrimSpace(a.Zone))
}

// resolveAccountRef 把命令里的 `@xxx` 解析成具体账户。
//
//	"all"    → 所有能买这个 planCode 的账户（各下一单）
//	"<n>"    → /accounts 列表里的第 n 个
//	"<zone>" → 该子公司的账户；有多个时要求用序号，不猜
//
// 解析不出来一律返回错误，绝不"猜一个最像的" ——
// 猜错的后果是下到另一个账户上，而那不会报错，只会一直抢不到。
func resolveAccountRef(state *app.State, mon *monitor.Monitor, ref, planCode string) ([]types.OVHAccount, string) {
	all := listAccounts(state)
	if len(all) == 0 {
		return nil, "还没有配置 OVH 账户。"
	}
	ref = strings.ToLower(strings.TrimSpace(ref))

	if ref == "all" {
		if mon != nil {
			if buyable := mon.AccountsForPlan(planCode); len(buyable) > 0 {
				return buyable, ""
			}
		}
		// 判不出来哪些能买（目录还没热等）→ 用全部账户，并让调用方说清楚。
		// 这里宁可多投也不少投：@all 的语义就是"能试的都试"，
		// 而拒绝执行等于让一次目录抖动毁掉一次补货。
		return all, ""
	}

	// @1 / @2：按 /accounts 的显示顺序
	if n, err := strconv.Atoi(ref); err == nil {
		if n < 1 || n > len(all) {
			return nil, fmt.Sprintf("没有第 %d 个账户（一共 %d 个）。发 /accounts 看序号。", n, len(all))
		}
		return []types.OVHAccount{all[n-1]}, ""
	}

	// @us / @ie：按子公司
	matched := []types.OVHAccount{}
	for _, a := range all {
		if accountShortName(a) == ref {
			matched = append(matched, a)
		}
	}
	switch len(matched) {
	case 1:
		return matched, ""
	case 0:
		names := make([]string, 0, len(all))
		for i, a := range all {
			names = append(names, fmt.Sprintf("@%d(%s)", i+1, accountShortName(a)))
		}
		return nil, fmt.Sprintf("没有叫 @%s 的账户。可用的：%s", ref, strings.Join(names, " "))
	default:
		idx := []string{}
		for i, a := range all {
			if accountShortName(a) == ref {
				idx = append(idx, fmt.Sprintf("@%d(%s)", i+1, a.Name))
			}
		}
		return nil, fmt.Sprintf("@%s 对应 %d 个账户，用序号指定：%s",
			ref, len(matched), strings.Join(idx, " "))
	}
}

// runOrder 对一组账户依次下单，汇总回复。
//
// 多账户时逐个下：每个账户是独立的一单，一个失败不该拖累其它的 ——
// 抢稀缺机器时"两个账户都投"的全部意义就在于其中一个能成。
func runOrder(state *app.State, o *telegram.OrderInfo, accs []types.OVHAccount) string {
	if len(accs) == 1 {
		res := telegram.ProcessOrder(state, accs[0].ID, o.PlanCode, o.Datacenter, o.Quantity, o.Options)
		if res.Success {
			return fmt.Sprintf("📥 已创建 %d/%d 个抢购任务\n\n型号: %s\n账户: %s\n\n"+
				"系统会每 %d 秒重试一次，直到抢到为止。下单成功≠已付款。\n"+
				"查看 /queue · 取消 /cancel all · 改间隔 /interval",
				res.CreatedOrders, res.TotalOrders, o.PlanCode, telegram.AccountLabel(accs[0]),
				state.Config.RetryInterval())
		}
		return "❌ 下单失败\n\n" + res.Message +
			"\n\n💡 如果只是现在没货，可以挂着等补货：/watch " + o.PlanCode
	}

	var b strings.Builder
	okCount, total := 0, 0
	var fails []string
	for _, a := range accs {
		res := telegram.ProcessOrder(state, a.ID, o.PlanCode, o.Datacenter, o.Quantity, o.Options)
		if res.Success {
			okCount++
			total += res.CreatedOrders
			b.WriteString("  ✅ " + telegram.AccountLabel(a) +
				fmt.Sprintf(" — %d 个任务\n", res.CreatedOrders))
		} else {
			fails = append(fails, "  ❌ "+telegram.AccountLabel(a)+" — "+truncate(res.Message, 80))
		}
	}

	var head strings.Builder
	if okCount == 0 {
		head.WriteString("❌ " + strconv.Itoa(len(accs)) + " 个账户都没能下单\n\n")
	} else {
		head.WriteString(fmt.Sprintf("📥 %d/%d 个账户已开抢，共 %d 个任务\n\n", okCount, len(accs), total))
	}
	head.WriteString(b.String())
	for _, f := range fails {
		head.WriteString(f + "\n")
	}
	if okCount > 1 {
		// 多账户同时抢同一台机器,全中就是多台机器多笔钱。这件事必须说在前面。
		head.WriteString("\n⚠️ 这几个账户在抢同一台机器，都抢到就是几台、几笔钱。\n" +
			"   不想要了发 /cancel all。")
	}
	head.WriteString("\n查看 /queue")
	return head.String()
}

// askOrderConfirm 下单前把账户摆出来让用户确认。
//
// 为什么值得多这一步：账户选错的后果是"永远抢不到"，OVH 不报错、日志里也没异常，
// 用户唯一能发现的机会就是下单前看到它落在哪儿。
//
// 命令里显式写了 @账户 的**不走这一步** —— 那时候用户已经明确表达过了，
// 再拦一下纯属碍事，而抢购最不能忍的就是多一次往返。
func askOrderConfirm(state *app.State, chatID interface{}, messageID int64,
	o *telegram.OrderInfo, ra resolvedAccount, alt []types.OVHAccount) {

	f := &watchFlow{
		Step:     stepOrderConfirm,
		PlanCode: o.PlanCode,
		Order:    o,
		Accounts: alt,
		Confirm:  []types.OVHAccount{ra.Account},
	}
	tok := putFlow(f)

	var b strings.Builder
	b.WriteString("📋 确认一下\n\n")
	b.WriteString("型号：" + o.PlanCode + "\n")
	if o.Datacenter != "" {
		b.WriteString("机房：" + strings.ToUpper(o.Datacenter) + "\n")
	} else {
		b.WriteString("机房：所有有货的\n")
	}
	b.WriteString("数量：" + strconv.Itoa(o.Quantity) + " 台/机房\n")
	if len(o.Options) > 0 {
		b.WriteString("配置：" + strings.Join(o.Options, ", ") + "\n")
	}
	b.WriteString("账户：" + telegram.AccountLabel(ra.Account) + "\n")
	if !ra.Confident {
		b.WriteString("⚠️ 没能确认 " + o.PlanCode + " 属于哪个区，这是当前账户。\n" +
			"   用错区的账户不会报错，只会一直抢不到。\n")
	}
	b.WriteString("\n下一步会真的去下单。")

	labels := []string{"✅ 确认下单"}
	for _, a := range alt {
		if a.ID == ra.Account.ID {
			continue
		}
		labels = append(labels, "🔀 改用 "+telegram.AccountLabel(a))
	}
	if len(alt) > 1 {
		labels = append(labels, "🚀 每个账户都下一单")
	}
	labels = append(labels, "✖️ 取消")

	telegram.SendKeyboard(state, chatID, messageID, b.String(), flowKeyboard(tok, labels))
}
