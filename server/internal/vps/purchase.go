package vps

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/notify"
	"github.com/ovh-buy/server/internal/numconv"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

// VPS 自动下单。
//
// 在此之前这个功能是**只有界面没有实现**的:前端摆着"有货时自动下单"的勾选框,
// 后端的请求体结构里连 autoOrder / quantity 字段都没有,勾了直接被丢掉。
// 用户以为自己挂了自动抢购,实际上只会收到一条通知 —— 而 VPS 补货往往几分钟就抢光。
//
// 下单链路和独服同形但不同路:POST /order/cart/{id}/vps(不是 /eco),
// 必需配置是 vps_datacenter(独服是 dedicated_datacenter),系统在下单时就要定。
//
// 三个站点的差异(实测公开目录,2026-08):
//
//	region 取值   EU/CA 站点: canada | europe      US 站点: 只有 united_states
//	机房取值      EU/CA 站点: 11 个(含 BHS/SGP/SYD/YNM)
//	              US 站点: vps-xxx 只有 US-EAST-VA/US-WEST-OR,
//	                       欧洲和加拿大机房要买 -eu / -ca 后缀的**另一个商品**
//
// 所以 region 不能凭机房硬猜,必须问购物车自己(requiredConfiguration),
// 只有在它给了多个取值时才用机房去挑 —— 这也是独服那边踩过的坑。

// Outcome 一次下单的结果
type Outcome struct {
	Success bool
	// Fatal 确定性失败,重试多少次都一样(账户不存在、机型停售、配置非法)
	Fatal    bool
	Reason   string
	OrderID  string
	OrderURL string
}

// dcRegion 机房 → region。取值来自 OVH 自己的目录:
// US 站点的 -ca 变体列的机房正好是 BHS/SGP/SYD/YNM,-eu 变体是其余欧洲机房,
// 也就是说这张表不是猜的,是 OVH 自己划的线。
// 只在购物车给出多个 region 取值时才用得上。
var dcRegion = map[string]string{
	"BHS": "canada", "SGP": "canada", "SYD": "canada", "YNM": "canada",
	"DE": "europe", "EU-SOUTH-MIL": "europe", "EU-WEST-RBX": "europe",
	"GRA": "europe", "SBG": "europe", "UK": "europe", "WAW": "europe",
	"US-EAST-VA": "united_states", "US-WEST-OR": "united_states",
}

// PurchaseVPS 在指定机房下单一台 VPS。
// dcCode 是 OVH 的机房代码(GRA / BHS / US-EAST-VA ...),来自可用性接口。
func PurchaseVPS(state *app.State, sub types.VPSSubscription, dcCode string) Outcome {
	if strings.TrimSpace(sub.AutoOrderAccountID) == "" {
		return Outcome{Fatal: true, Reason: "没有指定下单账户"}
	}
	client, err := state.OVH.IsolatedClientFor(sub.AutoOrderAccountID)
	if err != nil {
		// 账户没了 / 凭据缺失 —— 重试一万次也一样
		return Outcome{Fatal: true, Reason: fmt.Sprintf("账户 %s 不可用: %s", sub.AutoOrderAccountID, err)}
	}
	acc, ok := state.FindAccount(sub.AutoOrderAccountID)
	if !ok {
		return Outcome{Fatal: true, Reason: "下单账户不存在: " + sub.AutoOrderAccountID}
	}

	subsidiary := NormalizeSubsidiary(sub.OvhSubsidiary)
	if subsidiary == "" {
		subsidiary = DefaultSubsidiary(state, sub.AutoOrderAccountID)
	}
	// 账户和子公司必须同站点。三个站点的购物车互不相通,
	// 拿美区账户去欧区建车,第一步就 400。
	if ar, sr := ovh.EndpointRegion(acc.Endpoint), ovh.SubsidiaryRegion(subsidiary); ar != sr {
		return Outcome{Fatal: true, Reason: fmt.Sprintf(
			"下单账户在 %s，而订阅子公司 %s 属于 %s；两个站点的购物车不互通",
			RegionLabel(ar), subsidiary, RegionLabel(sr))}
	}

	qty := sub.Quantity
	if qty < 1 {
		qty = 1
	}

	state.Logger.Info(fmt.Sprintf("[VPS下单] 开始: %s @ %s (子公司 %s, 账户 %s, %d 台)",
		sub.PlanCode, dcCode, subsidiary, acc.Name, qty), "vps_purchase")

	// 1) 建购物车
	var cartResult map[string]interface{}
	if err := client.Post("/order/cart", map[string]interface{}{"ovhSubsidiary": subsidiary}, &cartResult); err != nil {
		return Outcome{Reason: "创建购物车失败: " + ovh.Explain(err)}
	}
	cartID, _ := cartResult["cartId"].(string)
	if cartID == "" {
		return Outcome{Reason: fmt.Sprintf("购物车响应里没有 cartId: %v", cartResult)}
	}

	// 失败就把车删掉。高频抢购攒下的僵尸 cart 会把账户顶到 OVH 限流,
	// 而限流的表现是"下一次真有货的时候下不了单"。
	success := false
	defer func() {
		if success || cartID == "" {
			return
		}
		if err := client.Delete("/order/cart/"+cartID, nil); err != nil {
			state.Logger.Debug("[VPS下单] 清理失败 cart "+cartID+": "+err.Error(), "vps_purchase")
		}
	}()

	// 2) 绑定购物车。和独服一致:在加商品之前 assign,
	// 免得 OVH 后端出现"cart 未绑定就 checkout"的边界错误。
	// 这个端点只有 path 参数、没有 body,传 {} 会被算进签名导致 400。
	if err := client.Post("/order/cart/"+cartID+"/assign", nil, nil); err != nil {
		return Outcome{Reason: "绑定购物车失败: " + ovh.Explain(err)}
	}

	// 3) 加商品。duration / pricingMode 是必填,合法组合来自 GET /order/cart/{id}/vps
	duration, pricingMode := lookupVPSPricing(state, client, cartID, sub.PlanCode)
	if duration == "" {
		// 拉不到就用最常见的月付。硬编码只作为兜底 ——
		// 真正的取值永远优先问 OVH,因为它会变。
		duration, pricingMode = "P1M", "default"
		state.Logger.Warn("[VPS下单] 拉不到计价组合，退回月付 P1M/default", "vps_purchase")
	}

	var itemResult map[string]interface{}
	if err := client.Post("/order/cart/"+cartID+"/vps", map[string]interface{}{
		"planCode":    sub.PlanCode,
		"duration":    duration,
		"pricingMode": pricingMode,
		"quantity":    qty,
	}, &itemResult); err != nil {
		msg := ovh.Explain(err)
		// 停售机型在这一步会被拒,而且重试没有意义
		if strings.Contains(msg, "not found") || strings.Contains(msg, "invalid planCode") {
			return Outcome{Fatal: true, Reason: fmt.Sprintf(
				"加购 %s 失败(%s)：这个型号在 %s 可能已经停售", sub.PlanCode, msg, subsidiary)}
		}
		return Outcome{Reason: fmt.Sprintf("加购 %s 失败: %s", sub.PlanCode, msg)}
	}
	itemID, _ := numconv.ToInt64(itemResult["itemId"])
	if itemID == 0 {
		return Outcome{Reason: fmt.Sprintf("加购响应里没有 itemId: %v", itemResult)}
	}

	// 4) 必需配置。问购物车要合法取值,不自己猜 ——
	// 三个站点的 region / 机房取值完全不同,猜的代价是整单失败。
	required, err := fetchRequiredConfig(client, cartID, itemID)
	if err != nil {
		state.Logger.Warn("[VPS下单] 拉必需配置失败(按默认继续): "+err.Error(), "vps_purchase")
	}

	configs := buildVPSConfig(required, dcCode, sub.OS)
	for _, cfg := range configs {
		if err := client.Post(fmt.Sprintf("/order/cart/%s/item/%d/configuration", cartID, itemID),
			map[string]interface{}{"label": cfg.label, "value": cfg.value}, nil); err != nil {
			// 配置项被拒 = 这套组合买不到,重试无用
			return Outcome{Fatal: true, Reason: fmt.Sprintf(
				"设置 %s=%s 失败: %s", cfg.label, cfg.value, ovh.Explain(err))}
		}
		state.Logger.Info(fmt.Sprintf("[VPS下单] 配置 %s = %s", cfg.label, cfg.value), "vps_purchase")
	}

	// 4.5) 必选附加项。2027 系列的 automatedBackup / storage 是 mandatory 家族,
	// 不加 checkout 就 400 "Addon of type automatedBackup is missing"。
	// 拉不到选项就硬着头皮结账 —— 老型号可能根本没有必选家族。
	var opts []vpsOption
	if err := client.Get(fmt.Sprintf("/order/cart/%s/vps/options?planCode=%s", cartID, url.QueryEscape(sub.PlanCode)), &opts); err != nil {
		state.Logger.Warn("[VPS下单] 拉附加项失败(按无必选项继续): "+ovh.Explain(err), "vps_purchase")
	}
	addons, missing := pickMandatoryAddons(opts, duration, pricingMode)
	if len(missing) > 0 {
		return Outcome{Fatal: true, Reason: fmt.Sprintf(
			"必选附加项 %v 没有 %s/%s 计价的可选值,这个型号按当前计价方式买不了", missing, duration, pricingMode)}
	}
	for _, a := range addons {
		if err := client.Post(fmt.Sprintf("/order/cart/%s/vps/options", cartID), map[string]interface{}{
			"itemId": itemID, "planCode": a.planCode, "duration": duration,
			"pricingMode": pricingMode, "quantity": qty,
		}, nil); err != nil {
			return Outcome{Reason: fmt.Sprintf("添加附加项 %s 失败: %s", a.planCode, ovh.Explain(err))}
		}
		state.Logger.Info(fmt.Sprintf("[VPS下单] 附加项 %s = %s", a.family, a.planCode), "vps_purchase")
	}

	// 5) 结账
	var checkoutResult map[string]interface{}
	// 同独服:不发 waiveRetractationPeriod(放弃 14 天撤回权)。
	// schema 里它是 required:false,不传即不主动弃权;真要弃权可以事后调
	// POST /me/order/{id}/waiveRetraction,而结账时传 true 则不可逆。
	if err := client.Post("/order/cart/"+cartID+"/checkout", map[string]interface{}{
		// 订阅上显式打开"自动付款"才为 true;默认不替用户扣钱
		"autoPayWithPreferredPaymentMethod": sub.AutoPay,
	}, &checkoutResult); err != nil {
		// 配置接口对取值几乎不校验,真正的"这个机房没货"往往到 checkout 才报出来
		return Outcome{Reason: "结账失败: " + ovh.Explain(err)}
	}

	orderID := numconv.ToString(checkoutResult["orderId"])
	orderURL, _ := checkoutResult["url"].(string)
	success = true
	state.Logger.Info(fmt.Sprintf("[VPS下单] 成功: %s @ %s 订单 %s", sub.PlanCode, dcCode, orderID), "vps_purchase")
	return Outcome{Success: true, OrderID: orderID, OrderURL: orderURL}
}

type kv struct{ label, value string }

// vpsOption GET /order/cart/{id}/vps/options 的一项
type vpsOption struct {
	Family    string `json:"family"`
	Mandatory bool   `json:"mandatory"`
	PlanCode  string `json:"planCode"`
	Prices    []struct {
		Duration    string   `json:"duration"`
		PricingMode string   `json:"pricingMode"`
		Capacities  []string `json:"capacities"`
		Price       struct {
			Value float64 `json:"value"`
		} `json:"price"`
	} `json:"prices"`
}

type addonPick struct{ family, planCode string }

// pickMandatoryAddons 给每个必选家族挑一个附加项:只看和主商品同 duration/pricingMode
// 的续费价,取最便宜的(备份就是 1 天保留那档,而不是 7 天)。
// os 家族跳过 —— 它由 vps_os 配置项决定,再手动加一个 option-linux 会和 Windows 冲突。
// missing 是有必选家族却挑不出可用项的情况,交给调用方判定失败。
func pickMandatoryAddons(opts []vpsOption, duration, pricingMode string) (picks []addonPick, missing []string) {
	type best struct {
		planCode string
		price    float64
	}
	chosen := map[string]*best{}
	var families []string
	for _, o := range opts {
		if !o.Mandatory || o.Family == "" || strings.EqualFold(o.Family, "os") {
			continue
		}
		if _, seen := chosen[o.Family]; !seen {
			chosen[o.Family] = nil
			families = append(families, o.Family)
		}
		for _, p := range o.Prices {
			if p.Duration != duration || p.PricingMode != pricingMode || !containsFold(p.Capacities, "renew") {
				continue
			}
			if b := chosen[o.Family]; b == nil || p.Price.Value < b.price {
				chosen[o.Family] = &best{o.PlanCode, p.Price.Value}
			}
		}
	}
	for _, f := range families {
		if b := chosen[f]; b != nil {
			picks = append(picks, addonPick{f, b.planCode})
		} else {
			missing = append(missing, f)
		}
	}
	return picks, missing
}

// requiredItem 购物车给出的一个必需配置项
type requiredItem struct {
	Label         string   `json:"label"`
	Required      bool     `json:"required"`
	AllowedValues []string `json:"allowedValues"`
}

func fetchRequiredConfig(client *ovhsdk.Client, cartID string, itemID int64) ([]requiredItem, error) {
	var out []requiredItem
	err := client.Get(fmt.Sprintf("/order/cart/%s/item/%d/requiredConfiguration", cartID, itemID), &out)
	return out, err
}

// buildVPSConfig 决定要提交哪些配置项。
//
// 只提交购物车明确列出来的项:多提交一个它不认识的 label 会 400,
// 而少提交一个必需项要到 checkout 才报错 —— 那时候货可能已经被别人抢走了。
func buildVPSConfig(required []requiredItem, dcCode, os string) []kv {
	byLabel := map[string]requiredItem{}
	for _, r := range required {
		byLabel[r.Label] = r
	}

	out := make([]kv, 0, 3)

	// 机房:唯一一个必填项
	if r, ok := byLabel["vps_datacenter"]; ok {
		v := dcCode
		if len(r.AllowedValues) > 0 && !containsFold(r.AllowedValues, dcCode) {
			// 购物车不认这个机房 —— 照样提交,让 OVH 给出准确的报错,
			// 比我们自己编一句"机房不合法"更有用
			v = dcCode
		}
		out = append(out, kv{"vps_datacenter", v})
	} else if len(required) == 0 {
		// 完全拉不到必需配置时的兜底:机房总是要给的
		out = append(out, kv{"vps_datacenter", dcCode})
	}

	// region:US 站点只有 united_states,EU/CA 站点有 canada 和 europe 两个。
	// 给了一个就用它,给了多个才按机房挑 —— 挑错的代价是整单失败。
	if r, ok := byLabel["region"]; ok {
		switch {
		case len(r.AllowedValues) == 1:
			out = append(out, kv{"region", r.AllowedValues[0]})
		case len(r.AllowedValues) > 1:
			if want := dcRegion[strings.ToUpper(dcCode)]; want != "" && containsFold(r.AllowedValues, want) {
				out = append(out, kv{"region", want})
			}
			// 挑不出来就不提交:region 不是必填,让 OVH 用默认值,
			// 总比提交一个错的把整单打掉强
		}
	}

	// 系统:requiredConfiguration 里 vps_os 标的是 required:false,但 checkout 不给它
	// 就 400 "Missing parameter(s): vps_os or vps_backupid is required" ——
	// 根本没有"OVH 默认镜像"这回事。用户没选时从购物车允许的列表里挑一个。
	os = strings.TrimSpace(os)
	r, hasOS := byLabel["vps_os"]
	if os == "" && hasOS {
		os = defaultVPSOS(r.AllowedValues)
	}
	if os != "" {
		if !hasOS || len(r.AllowedValues) == 0 || containsFold(r.AllowedValues, os) {
			out = append(out, kv{"vps_os", os})
		}
	}
	return out
}

// defaultVPSOS 用户没选系统时的兜底:按偏好顺序挑购物车里有的,都没有就取第一个
func defaultVPSOS(allowed []string) string {
	for _, want := range []string{"Debian 13", "Debian 12", "Ubuntu 24.04"} {
		if containsFold(allowed, want) {
			return want
		}
	}
	if len(allowed) > 0 {
		return allowed[0]
	}
	return ""
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

// lookupVPSPricing 从 GET /order/cart/{id}/vps 里找这个 planCode 的月付组合。
// 优先月付(mode=default):抢购要的是尽快拿到机器,不是替用户决定包年。
func lookupVPSPricing(state *app.State, client *ovhsdk.Client, cartID, planCode string) (string, string) {
	var defs []struct {
		PlanCode string `json:"planCode"`
		Prices   []struct {
			Duration    string   `json:"duration"`
			PricingMode string   `json:"pricingMode"`
			Capacities  []string `json:"capacities"`
		} `json:"prices"`
	}
	if err := client.Get("/order/cart/"+cartID+"/vps", &defs); err != nil {
		state.Logger.Debug("[VPS下单] 拉计价失败: "+err.Error(), "vps_purchase")
		return "", ""
	}
	for _, d := range defs {
		if !strings.EqualFold(d.PlanCode, planCode) {
			continue
		}
		// 先找月付
		for _, p := range d.Prices {
			if p.PricingMode == "default" && p.Duration == "P1M" {
				return p.Duration, p.PricingMode
			}
		}
		// 没有就用第一个能续费的
		for _, p := range d.Prices {
			for _, c := range p.Capacities {
				if c == "renew" {
					return p.Duration, p.PricingMode
				}
			}
		}
		if len(d.Prices) > 0 {
			return d.Prices[0].Duration, d.Prices[0].PricingMode
		}
	}
	return "", ""
}

// recordVPSPurchase 把下单结果写进抢购历史,和独服共用一张表 ——
// 用户不关心"这是独服还是 VPS 的历史",只关心"我到底买到了什么"。
func recordVPSPurchase(state *app.State, sub types.VPSSubscription, dcCode string, out Outcome) {
	state.HistoryMu.Lock()
	defer state.HistoryMu.Unlock()
	entry := types.PurchaseHistoryEntry{
		ID:           uuid.NewString(),
		TaskID:       "vps:" + sub.ID,
		AccountID:    sub.AutoOrderAccountID,
		PlanCode:     sub.PlanCode,
		Datacenter:   dcCode,
		Options:      []string{},
		PurchaseTime: types.NowISO(),
		AttemptCount: 1,
	}
	if out.Success {
		entry.Status = "success"
		entry.OrderID = out.OrderID
		entry.OrderURL = out.OrderURL
	} else {
		entry.Status = "failed"
		reason := out.Reason
		entry.ErrorMessage = &reason
	}
	state.History = append(state.History, entry)
	// 注意:defer 的 Unlock 先执行,SaveHistory 在锁外(它自己拿 HistoryMu 快照,
	// 持锁同步调会死锁 —— 见独服 recordSuccess 的同款修复)。这里保持异步:
	// VPS 购买路径一次只一条,上面的 120s 闸门在崩溃重放时兜底
}

// autoOrderOnRestock 补货时自动下单。
// 只对"从无货变成有货"的机房下单 —— 首次检查发现的存量不算补货,
// 那可能是一台挂了几个月的订阅刚启动,用户并没打算现在就买。
func autoOrderOnRestock(state *app.State, sub types.VPSSubscription, dcs []map[string]interface{}) {
	if !sub.AutoOrder || strings.TrimSpace(sub.AutoOrderAccountID) == "" {
		return
	}
	// 同独服 quick-order 的 120 秒近期成功闸门:VPS 库存自然抖动
	// (有货→抢光→再放货)每个回摆都会重新触发无货→有货跳变,
	// 不挡的话同一窗口反复买 quantity 台直到用户肉眼发现。
	// 状态机表达不了"这个窗口已经买过",唯一能表达的就是近期成功史
	state.HistoryMu.Lock()
	nowTS := time.Now().Unix()
	for i := len(state.History) - 1; i >= 0; i-- {
		h := &state.History[i]
		if h.PlanCode == sub.PlanCode && h.Status == "success" && h.AccountID == sub.AutoOrderAccountID {
			if t, ok := types.ParseTS(h.PurchaseTime); ok && nowTS-t.Unix() < 120 {
				state.HistoryMu.Unlock()
				state.Logger.Info("[VPS下单] 同配置 120 秒内已成功下过单,跳过本次补货(防抖动重复下单): "+sub.PlanCode, "vps_purchase")
				return
			}
		}
	}
	state.HistoryMu.Unlock()
	for _, dc := range dcs {
		// 可用性接口每个机房给两个名字:code 是 eu-west-lim 这种新长码,
		// datacenter(这里存成 name)是 DE / GRA / EU-WEST-LZ-AMS 这种短名。
		// 购物车的 vps_datacenter 只认短名 —— 交长码 checkout 直接 400
		// "Invalid VPS datacenter 'EU-WEST-LIM'"。LIM→DE、ERI→UK、MUM→YNM
		// 这几个连大写都对不上,只能用 OVH 自己给的那个字段。
		code, _ := dc["name"].(string)
		if code == "" {
			code, _ = dc["code"].(string)
		}
		if code == "" {
			continue
		}
		out := PurchaseVPS(state, sub, code)
		recordVPSPurchase(state, sub, code, out)

		if out.Success {
			// 同独服:checkout 是 autoPayWithPreferredPaymentMethod:false,
			// "成功"= 订单已创建、未付款、逾期作废。通知里必须说清楚。
			payNote := "⚠️ 订单尚未付款：请尽快打开订单链接完成付款,逾期未付订单会自动作废。\n" +
				"(订单未付款前处于 14 天撤销期内,可在 OVH 订单页撤回)"
			if sub.AutoPay {
				payNote = "💳 已请求用账户默认支付方式自动付款,请打开订单链接核对扣款是否成功。\n" +
					"(订单未付款前处于 14 天撤销期内,可在 OVH 订单页撤回)"
			}
			// 同独服:通知里发需要登录的控制面板深链,不发 checkout 那个带凭证的 url。
			// 带凭证那份存在本地历史里,界面上照样一键可付。
			linkURL := out.OrderURL
			if acc, ok := state.FindAccount(sub.AutoOrderAccountID); ok {
				if u := ovh.ManagerOrderURL(acc.Endpoint, out.OrderID); u != "" {
					linkURL = u
				}
			}
			msg := fmt.Sprintf("🎉 VPS 下单成功\n\n型号: %s\n机房: %s\n订单: %s\n%s\n\n%s",
				sub.PlanCode, code, out.OrderID, linkURL, payNote)
			notify.Broadcast(state, msg, nil)
			// 抢到就停:订阅是"盯着补货",不是"把所有机房都买一遍"
			return
		}
		state.Logger.Error(fmt.Sprintf("[VPS下单] %s @ %s 失败: %s", sub.PlanCode, code, out.Reason), "vps_purchase")
		notify.Broadcast(state, fmt.Sprintf("⚠️ VPS 自动下单失败\n\n型号: %s\n机房: %s\n原因: %s",
			sub.PlanCode, code, out.Reason), nil)
		if out.Fatal {
			// 确定性失败:换个机房也是同样结果,别再刷了
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
