package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// /watch 的分步选择流程。
//
// 为什么要做成按钮而不是让用户打参数:
// 一个 planCode 底下往往有好几套内存/存储组合，addon 代码长这样
// ram-64g-noecc-2133 / softraid-2x480ssd —— 没人会去手机上打这个，
// 更没人记得住自己那台机器是哪一套。而这件事又不能省略：
// 通知和自动下单都是**按配置逐套触发**的，不挑配置就等于每套都要，
// 「自动抢 1 台」会变成「每套配置在每个机房各抢 1 台」。
// 账户同理：多账户的人以前完全看不见 /watch 落到了哪个账户上，
// 而 planCode 是分区的，落错账户的后果是永远抢不到且看不出原因。

const (
	// flowTTL 一次选择流程的存活时间。这是"人在手机上点几下"的时间尺度，
	// 不需要更久；过期只是要重新 /watch 一次，没有副作用。
	flowTTL = 10 * time.Minute
	// flowMaxButtons 一屏最多列几个配置。Telegram 键盘太长会把消息挤得没法看，
	// 超出的让用户用文本参数精确指定。
	flowMaxButtons = 8
)

// flowStep 当前走到哪一步
type flowStep string

const (
	stepConfig  flowStep = "config"
	stepAccount flowStep = "account"
	stepAction  flowStep = "action"
	// stepSwitchAccount /accounts 里换当前账户。它不属于 /watch 那条流程,
	// 只是复用同一套 token + 按钮机制。
	stepSwitchAccount flowStep = "switch_account"
	// stepOrderAccount 文本下单时同区多账户,挑一次再下。
	stepOrderAccount flowStep = "order_account"
	// stepOrderConfirm 文本下单前的确认(没显式写 @账户 时才走)。
	stepOrderConfirm flowStep = "order_confirm"
	// stepNarrowConfig 快捷式 /watch(带 x<数量>)建完订阅后,一键把它从
	// "盯全部配置"改窄到某一套。addon planCode 动辄二三十字符,让用户打出来不现实,
	// 所以只能用按钮 —— 而这条路以前不存在,用户只能删掉订阅重发一次不带 x 的 /watch。
	stepNarrowConfig flowStep = "narrow_config"
)

type configChoice struct {
	Label   string   // 给用户看的："64G + 2x480 SSD"
	Options []string // 这套配置的 addon planCode
	InStock int      // 当前有货的机房数，只用于展示
}

type watchFlow struct {
	ChatID   interface{}
	PlanCode string
	DCs      []string
	Step     flowStep

	Configs  []configChoice
	Accounts []types.OVHAccount

	// Order 文本下单挂起时保存的原始解析结果,确认/选完账户接着下。
	Order *telegram.OrderInfo
	// Confirm 确认步骤里默认要用的那个账户(第一颗按钮)。
	Confirm []types.OVHAccount

	PickedOptions []string
	PickedConfig  string
	AccountID     string
	AccountLabel  string

	Expires time.Time
}

var (
	flowMu    sync.Mutex
	flowStore = map[string]*watchFlow{}
)

func newFlowToken() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func putFlow(f *watchFlow) string {
	tok := newFlowToken()
	f.Expires = time.Now().Add(flowTTL)
	flowMu.Lock()
	// 顺手清过期的，不引定时器
	for k, v := range flowStore {
		if time.Now().After(v.Expires) {
			delete(flowStore, k)
		}
	}
	flowStore[tok] = f
	flowMu.Unlock()
	return tok
}

func getFlow(tok string) (*watchFlow, bool) {
	flowMu.Lock()
	defer flowMu.Unlock()
	f, ok := flowStore[tok]
	if !ok || time.Now().After(f.Expires) {
		delete(flowStore, tok)
		return nil, false
	}
	return f, true
}

func dropFlow(tok string) {
	flowMu.Lock()
	delete(flowStore, tok)
	flowMu.Unlock()
}

// flowKeyboard 造一行一个的按钮。callback_data 里只放 token + 序号 ——
// Telegram 限制 callback_data 最多 64 字节，而 addon 代码动辄二三十字符，
// 塞进去会直接超限（超限时 Telegram 不报错，按钮就是点了没反应）。
func flowKeyboard(tok string, labels []string) map[string]interface{} {
	rows := make([][]map[string]string, 0, len(labels))
	for i, l := range labels {
		cb, _ := json.Marshal(map[string]interface{}{"a": "wf", "t": tok, "i": i})
		rows = append(rows, []map[string]string{{
			"text":          l,
			"callback_data": string(cb),
		}})
	}
	return map[string]interface{}{"inline_keyboard": rows}
}

// enumerateConfigs 列出这个 planCode 在指定账户下的所有配置。
//
// 注意用的是可用性接口而不是目录：它连"当前无货"的配置也会返回（带 unavailable 状态），
// 这正是我们要的 —— /watch 的使用场景本来就是"现在没货，等补货"。
func enumerateConfigs(state *app.State, planCode, accountID string) []configChoice {
	byConfig := catalog.CheckServerAvailabilityWithConfigs(state, planCode, accountID)
	out := make([]configChoice, 0, len(byConfig))
	for _, d := range byConfig {
		if d == nil {
			continue
		}
		label := strings.TrimSpace(d.Memory + " + " + d.Storage)
		if label == "+" || label == "" {
			continue
		}
		inStock := 0
		for _, status := range d.Datacenters {
			if catalog.IsAvailableForOrder(status) {
				inStock++
			}
		}
		out = append(out, configChoice{
			Label:   label,
			Options: d.Options,
			InStock: inStock,
		})
	}
	// 有货的排前面，其余按名字稳定排序 —— 不排序的话 map 遍历顺序每次都不一样，
	// 用户第二次 /watch 会发现按钮顺序变了，很容易点错。
	sort.Slice(out, func(i, j int) bool {
		if (out[i].InStock > 0) != (out[j].InStock > 0) {
			return out[i].InStock > 0
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// offerNarrowConfig 快捷式 /watch 建完订阅后,挂一排按钮让用户一键改窄配置。
//
// 返回 false = 没什么可挑的(只有一套配置),调用方不用额外说什么。
// 故意放在订阅**已经建好之后**:快捷式的价值就是一条命令立刻开始盯,
// 不能因为多了个选择步骤把它变成又一个向导。
func offerNarrowConfig(state *app.State, chatID interface{}, messageID int64, planCode string) bool {
	configs := enumerateConfigs(state, planCode, "")
	if len(configs) <= 1 {
		return false
	}
	f := &watchFlow{
		ChatID:   chatID,
		PlanCode: planCode,
		Configs:  configs,
		Step:     stepNarrowConfig,
	}
	tok := putFlow(f)
	telegram.SendKeyboard(state, chatID, messageID,
		fmt.Sprintf("🔧 %s 有 %d 套配置，现在盯的是**全部**。\n"+
			"要只盯一套就点一下（随时可以改回来）：", planCode, len(configs)),
		flowKeyboard(tok, configLabels(configs)))
	return true
}

// startWatchFlow 开始分步选择。返回 false 表示不需要走流程（直接建订阅即可）。
func startWatchFlow(state *app.State, mon *monitor.Monitor, chatID interface{}, messageID int64,
	planCode string, dcs []string) bool {

	configs := enumerateConfigs(state, planCode, "")
	accounts := listAccounts(state)

	// 只有一套配置、且只有一个账户 —— 没什么可选的，让调用方走直接路径
	if len(configs) <= 1 && len(accounts) <= 1 {
		return false
	}

	f := &watchFlow{
		ChatID:   chatID,
		PlanCode: planCode,
		DCs:      dcs,
		Configs:  configs,
		Accounts: accounts,
	}

	// 配置只有一套就跳过这一步，直接问账户
	if len(configs) <= 1 {
		if len(configs) == 1 {
			f.PickedOptions = configs[0].Options
			f.PickedConfig = configs[0].Label
		}
		return askAccount(state, f, chatID, messageID)
	}

	f.Step = stepConfig
	tok := putFlow(f)
	labels := configLabels(configs)
	telegram.SendKeyboard(state, chatID, messageID,
		"🔧 "+planCode+" 有 "+fmt.Sprint(len(configs))+" 套配置，盯哪一套？\n\n"+
			"通知和自动下单都是按配置逐套触发的 —— 选「全部配置」意味着每套补货都会各下一次单。",
		flowKeyboard(tok, labels))
	return true
}

func configLabels(configs []configChoice) []string {
	labels := make([]string, 0, len(configs)+1)
	for i, c := range configs {
		if i >= flowMaxButtons {
			break
		}
		l := c.Label
		if c.InStock > 0 {
			l += fmt.Sprintf("  ✅%d个机房有货", c.InStock)
		}
		labels = append(labels, l)
	}
	labels = append(labels, "🌐 全部配置（每套都盯）")
	return labels
}

func listAccounts(state *app.State) []types.OVHAccount {
	state.AccountsMu.RLock()
	defer state.AccountsMu.RUnlock()
	out := make([]types.OVHAccount, len(state.Accounts))
	copy(out, state.Accounts)
	return out
}

func accountLabel(a types.OVHAccount) string {
	zone := strings.ToUpper(strings.TrimSpace(a.Zone))
	if zone == "" {
		zone = "?"
	}
	l := a.Name + "（" + zone + "）"
	if a.IsDefault {
		l += " 默认"
	}
	return l
}

// askAccount 问用哪个账户。只有一个账户时直接跳到下一步。
func askAccount(state *app.State, f *watchFlow, chatID interface{}, messageID int64) bool {
	if len(f.Accounts) <= 1 {
		if len(f.Accounts) == 1 {
			f.AccountID = f.Accounts[0].ID
			f.AccountLabel = accountLabel(f.Accounts[0])
		}
		return askAction(state, f, chatID, messageID)
	}
	f.Step = stepAccount
	tok := putFlow(f)
	cur, _ := telegram.ActiveAccount(state)
	labels := make([]string, 0, len(f.Accounts))
	for _, a := range f.Accounts {
		l := accountLabel(a)
		if a.ID == cur.ID {
			// 标出当前账户 —— 用户多半就想用它,不标的话得自己回想哪个是哪个
			l = "✅ " + l
		}
		labels = append(labels, l)
	}
	telegram.SendKeyboard(state, chatID, messageID,
		"👤 用哪个账户？\n\n"+
			"OVH 的 EU / US / CA 是三套独立系统，同一台机器在不同区是不同的型号代码。"+
			"选错区的账户会永远抢不到，而且看不出原因。",
		flowKeyboard(tok, labels))
	return true
}

var actionChoices = []struct {
	Label    string
	Quantity int
}{
	{"🔔 只通知我，我自己点按钮下单", 0},
	{"⚡ 补货自动抢 1 台", 1},
	{"⚡ 补货自动抢 2 台", 2},
	{"⚡ 补货自动抢 3 台", 3},
}

// askAction 问补货时该做什么。
func askAction(state *app.State, f *watchFlow, chatID interface{}, messageID int64) bool {
	f.Step = stepAction
	tok := putFlow(f)
	labels := make([]string, 0, len(actionChoices))
	for _, a := range actionChoices {
		labels = append(labels, a.Label)
	}

	telegram.SendKeyboard(state, chatID, messageID, actionPrompt(f), flowKeyboard(tok, labels))
	return true
}

// handleFlowCallback 处理分步选择的按钮。返回 false 表示这不是流程回调。
func handleFlowCallback(state *app.State, mon *monitor.Monitor, cb map[string]interface{},
	callbackObj map[string]interface{}, chatID interface{}, messageID int64) bool {

	tok := strOr(callbackObj, "t", "token")
	if tok == "" {
		return false
	}
	idx := -1
	if v, ok := getNumOrFloat(callbackObj["i"]); ok {
		idx = int(v)
	}

	f, ok := getFlow(tok)
	if !ok {
		telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "这个选择已过期，请重新 /watch", true)
		telegram.SendReply(state, chatID, "⌛ 选择超时（超过 10 分钟），请重新发一次 /watch "+"", messageID)
		return true
	}
	dropFlow(tok)

	switch f.Step {
	case stepConfig:
		// 最后一个是「全部配置」
		if idx >= 0 && idx < len(f.Configs) && idx < flowMaxButtons {
			f.PickedOptions = f.Configs[idx].Options
			f.PickedConfig = f.Configs[idx].Label
		} else {
			f.PickedOptions = nil
			f.PickedConfig = ""
		}
		telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "好", false)
		askAccount(state, f, chatID, messageID)
		return true

	case stepNarrowConfig:
		// 只改这条订阅的 Options,其余字段原样保留 ——
		// 不能走"删了重建":那会清空 LastStatus,下一轮把本来就有货当成补货跳变,
		// 发一条根本没发生的通知,还会真下单。
		var opts []string
		label := "全部配置"
		if idx >= 0 && idx < len(f.Configs) && idx < flowMaxButtons {
			opts = f.Configs[idx].Options
			label = f.Configs[idx].Label
		}
		if !mon.SetSubscriptionOptions(f.PlanCode, opts) {
			telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "订阅已不在了", true)
			telegram.SendReply(state, chatID, "这条订阅已经被删掉了，发 /watch "+f.PlanCode+" 重新开始。", messageID)
			return true
		}
		mon.SaveToDB()
		telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "好", false)
		if len(opts) == 0 {
			telegram.SendReply(state, chatID,
				"🌐 "+f.PlanCode+" 改回盯全部配置。\n每套配置补货都会各自通知、各自下单。", messageID)
		} else {
			telegram.SendReply(state, chatID,
				"🎯 "+f.PlanCode+" 现在只盯："+label+"\n其它配置补货不再通知，也不会下单。", messageID)
		}
		return true

	case stepAccount:
		if idx >= 0 && idx < len(f.Accounts) {
			f.AccountID = f.Accounts[idx].ID
			f.AccountLabel = accountLabel(f.Accounts[idx])
		}
		telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "好", false)
		askAction(state, f, chatID, messageID)
		return true

	case stepSwitchAccount:
		if idx < 0 || idx >= len(f.Accounts) {
			telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "选项无效", true)
			return true
		}
		acc := f.Accounts[idx]
		if err := telegram.SetActiveAccount(state, acc.ID); err != nil {
			// 落库失败必须说。不说的话用户以为切了,下一单还是落在旧账户上,
			// 而 planCode 是分区的 —— 后果是永远抢不到且看不出原因。
			state.Logger.Error("保存 Telegram 当前账户失败: "+err.Error(), "telegram")
			telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "保存失败", true)
			telegram.SendReply(state, chatID, "⚠️ 切换失败，没能写进数据库："+err.Error()+"\n当前账户仍是原来那个。", messageID)
			return true
		}
		state.Logger.Info("Telegram 当前账户切换为: "+acc.Name+" ("+acc.ID+")", "telegram")
		telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "已切换", false)
		telegram.SendReply(state, chatID,
			"👤 当前账户已切换为：\n"+telegram.AccountLabel(acc)+
				"\n\n之后的文本下单和 /watch 都会落到这个账户。\n"+
				"注意：已经在队列里的任务和已存在的订阅不受影响，它们各自记着当初选的账户。",
			messageID)
		return true

	case stepOrderConfirm:
		if f.Order == nil || len(f.Confirm) == 0 {
			telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "选项已失效", true)
			return true
		}
		// 按钮顺序:[确认] [改用 A] [改用 B] ... [每个账户都下] [取消]
		alt := []types.OVHAccount{}
		for _, a := range f.Accounts {
			if a.ID != f.Confirm[0].ID {
				alt = append(alt, a)
			}
		}
		iConfirm := 0
		iAll := 1 + len(alt) // 只有 len(f.Accounts) > 1 时才存在
		iCancel := iAll
		if len(f.Accounts) > 1 {
			iCancel = iAll + 1
		}

		switch {
		case idx == iCancel:
			telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "已取消", false)
			telegram.SendReply(state, chatID, "✖️ 已取消，没有创建任何任务。", messageID)
		case len(f.Accounts) > 1 && idx == iAll:
			telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "全部账户开抢", false)
			telegram.SendReply(state, chatID, runOrder(state, f.Order, f.Accounts), messageID)
		case idx == iConfirm:
			telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "开始下单", false)
			telegram.SendReply(state, chatID, runOrder(state, f.Order, f.Confirm), messageID)
		case idx > iConfirm && idx-1 < len(alt):
			picked := alt[idx-1]
			// 换了账户就记成当前账户:同一个区之后不再问
			if err := telegram.SetActiveAccount(state, picked.ID); err != nil {
				state.Logger.Warn("保存 Telegram 当前账户失败(不影响本次下单): "+err.Error(), "telegram")
			}
			telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "换账户下单", false)
			telegram.SendReply(state, chatID,
				runOrder(state, f.Order, []types.OVHAccount{picked}), messageID)
		default:
			telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "选项无效", true)
		}
		return true

	case stepOrderAccount:
		if idx < 0 || idx >= len(f.Accounts) || f.Order == nil {
			telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "选项无效", true)
			return true
		}
		acc := f.Accounts[idx]
		// 记成当前账户:同一个区之后不再问。换别的区的机型时 resolveOrderAccount
		// 会发现它不在对的区,自动按 planCode 重新解析。
		if err := telegram.SetActiveAccount(state, acc.ID); err != nil {
			state.Logger.Warn("保存 Telegram 当前账户失败(不影响本次下单): "+err.Error(), "telegram")
		}
		telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "开始下单", false)
		o := f.Order
		res := telegram.ProcessOrder(state, acc.ID, o.PlanCode, o.Datacenter, o.Quantity, o.Options)
		if res.Success {
			telegram.SendReply(state, chatID, fmt.Sprintf(
				"📥 已创建 %d/%d 个抢购任务\n\n型号: %s\n账户: %s\n\n"+
					"系统将自动尝试下单;下单成功≠已付款。\n查看 /queue · 取消 /cancel all",
				res.CreatedOrders, res.TotalOrders, o.PlanCode, telegram.AccountLabel(acc)), messageID)
		} else {
			telegram.SendReply(state, chatID, "❌ 下单失败\n\n"+res.Message, messageID)
		}
		return true

	case stepAction:
		qty := 0
		if idx >= 0 && idx < len(actionChoices) {
			qty = actionChoices[idx].Quantity
		}
		telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "已保存", false)
		telegram.SendReply(state, chatID, finishWatchFlow(state, mon, f, qty), messageID)
		return true
	}
	return true
}

// finishWatchFlow 落地订阅。
func finishWatchFlow(state *app.State, mon *monitor.Monitor, f *watchFlow, quantity int) string {
	autoOrder := quantity > 0
	accountID := f.AccountID
	if autoOrder && accountID == "" {
		// 走到这里说明一个账户都没有 —— 自动下单没有落点，降级成只通知，
		// 而不是假装挂上了（那样补货时什么都不会发生，用户还以为在抢）。
		autoOrder = false
		quantity = 0
	}
	if quantity > telegram.MaxOrderQuantity {
		quantity = telegram.MaxOrderQuantity
	}

	existed := false
	for _, s := range mon.Snapshot() {
		if s.PlanCode == f.PlanCode {
			existed = true
			break
		}
	}

	// autoPay 恒 false：自动付款是要用户显式打开的开关，不能在几下点击里替他决定。
	mon.AddSubscription(f.PlanCode, f.DCs, true, false, "", nil, nil,
		autoOrder, quantity, accountID, false, f.PickedOptions)
	mon.SaveToDB()

	started := false
	if !mon.Running() {
		started = mon.Start()
	}

	var b strings.Builder
	if existed {
		b.WriteString("✏️ 已更新订阅：" + f.PlanCode + "\n（库存状态和历史保留，不会重复通知）\n\n")
	} else {
		b.WriteString("👀 已开始盯：" + f.PlanCode + "\n\n")
	}
	if f.PickedConfig != "" {
		b.WriteString("配置：" + f.PickedConfig + "\n")
	} else {
		b.WriteString("配置：全部\n")
	}
	if len(f.DCs) > 0 {
		b.WriteString("机房：" + strings.ToUpper(strings.Join(f.DCs, " / ")) + "\n")
	} else {
		b.WriteString("机房：全部\n")
	}
	if autoOrder {
		b.WriteString(fmt.Sprintf("补货时：自动抢 %d 台\n", quantity))
		b.WriteString("账户：" + f.AccountLabel + "\n")
		b.WriteString("\n⚠️ 这是真实下单。数量是每个机房各 " + fmt.Sprint(quantity) + " 台")
		if f.PickedConfig == "" {
			b.WriteString("，且每套配置各算一次")
		}
		b.WriteString("。\n抢到的订单默认不自动付款，需要你去付。\n")
	} else {
		b.WriteString("补货时：只发通知，你点按钮再下单\n")
		if f.AccountLabel != "" {
			b.WriteString("（通知里的按钮会落到：" + f.AccountLabel + "）\n")
		}
	}
	if started {
		b.WriteString("\n（监控原本没在跑，已顺手启动）")
	}
	b.WriteString("\n\n取消：/unwatch " + f.PlanCode + " · 全部订阅 /subs")
	return b.String()
}

// actionPrompt 「补货时怎么办」那一步的正文。
// 拆出来是为了能单独审阅措辞 —— 这一步决定要不要真花钱。
func actionPrompt(f *watchFlow) string {
	var b strings.Builder
	b.WriteString("🎯 补货的时候怎么办？\n\n")
	b.WriteString("型号：" + f.PlanCode + "\n")
	if f.PickedConfig != "" {
		b.WriteString("配置：" + f.PickedConfig + "\n")
	} else {
		b.WriteString("配置：全部\n")
	}
	if len(f.DCs) > 0 {
		b.WriteString("机房：" + strings.ToUpper(strings.Join(f.DCs, " / ")) + "\n")
	} else {
		b.WriteString("机房：全部\n")
	}
	if f.AccountLabel != "" {
		b.WriteString("账户：" + f.AccountLabel + "\n")
	}
	b.WriteString("\n⚠️ 选自动抢 = 真实下单。数量是**每个机房**各抢这么多")
	if f.PickedConfig == "" {
		b.WriteString("，而且**每套配置**都算一次")
	}
	b.WriteString("。")

	return b.String()
}

// askActionPreview 只拼正文不发送，给模板审阅用。
func askActionPreview(f *watchFlow) {
	fmt.Println(actionPrompt(f))
}
