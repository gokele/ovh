package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/telegram"
)

// 从 Telegram 添加「盯着补货就抢」的订阅。
//
// 为什么必须有这个:TG 原来唯一的下单入口是 ProcessOrder,而它**要求机器此刻有货** ——
// 查到全部机房无货就直接拒绝,一个任务都不建。
// 可抢购的常态恰恰是"现在没货,等补货"。也就是说以前从 TG 只能捡漏当下的现货,
// 真正的抢购(盯着补货)只能回网页控制台加订阅。
// 手机上看到别人说某型号要补货,却没法当场挂上,这个工具就少了一半意义。

// watchText 处理 /watch。
//
// 用法:/watch <型号> [机房...] [xN 数量]
//
//	/watch 24sk602                 盯所有机房,补货只通知
//	/watch 24sk602 gra rbx         只盯这两个机房
//	/watch 24sk602 gra x2          补货时自动抢 2 台
//	/watch 24sk602 x1              所有机房,补货自动抢 1 台
//
// watchText 处理 /watch 的文本形式。
//
// 返回空串表示"已经用按钮流程自己回复了",调用方不要再发一条。
func watchText(state *app.State, mon *monitor.Monitor, args []string) string {
	if mon == nil {
		return "监控未初始化，暂时不能添加订阅。"
	}
	if len(args) == 0 {
		return "用法：/watch <型号> [机房...] [x数量]\n\n" +
			"  /watch 24sk602            盯所有机房，补货只通知\n" +
			"  /watch 24sk602 gra rbx    只盯这两个机房\n" +
			"  /watch 24sk602 gra x2     补货时自动抢 2 台\n\n" +
			"带 x<数量> 才会自动下单；不带只发通知，你收到后再点按钮。\n" +
			"取消盯：/unwatch <型号>"
	}

	planCode := strings.TrimSpace(args[0])
	dcs := []string{}
	quantity := 0
	autoOrder := false

	for _, a := range args[1:] {
		a = strings.TrimSpace(strings.ToLower(a))
		if a == "" {
			continue
		}
		// x2 / X2 = 自动下单 2 台
		if strings.HasPrefix(a, "x") {
			if n, err := strconv.Atoi(a[1:]); err == nil && n > 0 {
				quantity = n
				autoOrder = true
				continue
			}
		}
		// 机房代码:3-4 位字母。写死长度是为了不把打错的参数当成机房
		// 静默吞掉 —— 那会变成"盯了一个不存在的机房,永远等不到货"。
		if len(a) >= 3 && len(a) <= 4 && isLowerAlpha(a) {
			dcs = append(dcs, a)
			continue
		}
		return fmt.Sprintf("看不懂参数 %q。机房是 3-4 位字母（gra / rbx / bhs…），数量写成 x2。\n\n发 /watch 看用法。", a)
	}

	// 自动下单必须落到一个具体账户。
	//
	// 不解析成具体 ID 的话，下单那一刻才现取默认账户 —— 中间只要有人改过默认账户，
	// 这一单就会用另一个账户、另一个区的凭据去下，而 planCode 是分区的
	// （EU / US / CA 三套目录基本不重合），结果是永远抢不到且看不出原因。
	accountID := ""
	accLabel := ""
	if autoOrder {
		// 账户按 planCode 反推,不用"上次切到哪个" ——
		// 订阅是要挂着长期跑的,落错区的表现是"永远等不到货",
		// 而那和"这机器一直没补货"在界面上一模一样。
		ra := resolveOrderAccount(state, mon, planCode)
		if ra.Account.ID == "" {
			return "要自动下单得先配置 OVH 账户。请到控制台「设置 → OVH 账户」添加，或者去掉 x<数量> 只接收通知。"
		}
		accountID = ra.Account.ID
		accLabel = telegram.AccountLabel(ra.Account)
		if ra.Confident {
			accLabel += fmt.Sprintf("（%s 在它的目录里，已自动选定）", planCode)
		} else {
			// 判不出来不拒绝,但必须说 —— 订阅是挂着长期跑的,
			// 落错区的表现是"永远等不到货",和"这机器一直没补货"在界面上一模一样。
			accLabel += "\n⚠️ 没能确认 " + planCode + " 属于哪个区，用的是当前账户。" +
				"\n   用错区的账户不会报错，只会一直抢不到。换账户发 /accounts。"
		}
	}

	if quantity > telegram.MaxOrderQuantity {
		quantity = telegram.MaxOrderQuantity
	}

	// 已存在的订阅是"就地改配置"，不会重置库存状态和历史 ——
	// 删了重建会清空 LastStatus，下一轮把"本来就有货"当成补货跳变，
	// 发一条根本没发生的通知外加真下单。
	existed := false
	for _, s := range mon.Snapshot() {
		if s.PlanCode == planCode {
			existed = true
			break
		}
	}

	// autoPay 恒 false：自动付款是个要用户显式打开的开关，
	// 不能因为他在手机上打了句 /watch 就替他决定花钱方式。
	mon.AddSubscription(planCode, dcs, true, false, "", nil, nil, autoOrder, quantity, accountID, false, nil)
	mon.SaveToDB()

	// 监控没在跑的话订阅加了也没用 —— 顺手拉起来，并说清楚
	started := false
	if !mon.Running() {
		started = mon.Start()
	}

	var b strings.Builder
	if existed {
		b.WriteString("✏️ 已更新订阅：" + planCode + "\n")
		b.WriteString("（库存状态和历史保留，不会重复通知）\n\n")
	} else {
		b.WriteString("👀 已开始盯：" + planCode + "\n\n")
	}
	if len(dcs) > 0 {
		b.WriteString("机房：" + strings.ToUpper(strings.Join(dcs, " / ")) + "\n")
	} else {
		b.WriteString("机房：所有\n")
	}
	if autoOrder {
		b.WriteString(fmt.Sprintf("补货时：自动抢 %d 台\n", quantity))
		b.WriteString("账户：" + accLabel + "\n")
		b.WriteString("配置：全部\n")
		// 数量的真实含义必须写出来。通知和自动下单是按配置逐套触发的,
		// 所以 x1 在"三套配置 × 两个机房"同时补货时会下六单 ——
		// 用户以为自己说的是"抢 1 台"。
		b.WriteString(fmt.Sprintf(
			"\n⚠️ 这是真实下单，而且 %d 是**每个机房、每套配置**各 %d 台。\n"+
				"这个型号如果有多套配置同时补货，实际下单数 = 配置数 × 机房数 × %d。\n"+
				"抢到的订单默认不自动付款，需要你去付。\n",
			quantity, quantity, quantity))
	} else {
		b.WriteString("补货时：只发通知，你点按钮再下单\n")
		b.WriteString("\n想补货就自动抢，加个数量：/watch " + planCode + " x1\n")
	}
	if started {
		b.WriteString("\n（监控原本没在跑，已顺手启动）")
	}
	b.WriteString("\n\n取消：/unwatch " + planCode + " · 全部订阅 /subs")
	return b.String()
}

// unwatchText 处理 /unwatch。
func unwatchText(state *app.State, mon *monitor.Monitor, args []string) string {
	if mon == nil {
		return "监控未初始化。"
	}
	if len(args) == 0 {
		return "用法：/unwatch <型号>\n\n发 /subs 看在盯哪些。"
	}
	planCode := strings.TrimSpace(args[0])
	if !mon.RemoveSubscription(planCode) {
		return "没有在盯 " + planCode + "。\n\n发 /subs 看当前订阅。"
	}
	mon.SaveToDB()
	state.Logger.Info("Telegram 取消订阅: "+planCode, "telegram")
	return "🚫 已取消盯 " + planCode + "\n\n注意：已经在队列里的抢购任务不受影响，发 /queue 查看、/cancel 取消。"
}

func isLowerAlpha(s string) bool {
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return len(s) > 0
}
