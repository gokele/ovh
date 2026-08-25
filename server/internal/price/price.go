package price

import (
	"fmt"
	"net/url"
	"strings"

	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/numconv"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

// Result 对应 Python: _get_server_price_internal 返回结构
type Result struct {
	Success    bool     `json:"success"`
	PlanCode   string   `json:"planCode,omitempty"`
	Datacenter string   `json:"datacenter,omitempty"`
	Options    []string `json:"options,omitempty"`
	// Degraded:价格算出来了,但购物车没有完全配成用户要的样子(某个非机房必填配置没设上)。
	// 价格仍可展示,但不能拿它当"可以下单"的闸门 —— purchase.go 对同样的配置是 fail-fast 的。
	Degraded       bool                   `json:"degraded,omitempty"`
	DegradedReason string                 `json:"degradedReason,omitempty"`
	Price          *PriceInfo             `json:"price"`
	Error          string                 `json:"error,omitempty"`
	Raw            map[string]interface{} `json:"-"`
}

type PriceInfo struct {
	PricingMode string                   `json:"pricingMode"`
	Prices      map[string]interface{}   `json:"prices"`
	Items       []map[string]interface{} `json:"items"`
}

// GetInternal 询价。accountID 决定用哪个账户调 OVH(空 = 默认账户),
// 以及购物车走哪个 subsidiary(账户的 zone)。多账户必须区分。
func GetInternal(state *app.State, accountID, planCode, datacenter string, options []string) Result {
	if options == nil {
		options = []string{}
	}
	apiDC := ovh.ConvertDisplayDCToAPIDC(datacenter)

	client, err := state.OVH.ClientFor(accountID)
	if err != nil {
		return Result{Success: false, Error: "未配置OVH API密钥: " + err.Error()}
	}
	acc, _ := state.FindAccount(accountID)
	subsidiary := orderSubsidiary(state, acc)

	state.Logger.Info(fmt.Sprintf("查询 %s 的配置价格，数据中心: %s (原始: %s), 选项: %v",
		planCode, apiDC, datacenter, options), "price")

	cartID := ""

	cleanup := func() {
		if cartID == "" {
			return
		}
		_ = client.Delete("/order/cart/"+cartID, nil)
	}
	// 防止中间步骤 panic（map 断言 / 空指针等）导致 cart 泄漏永不清理；
	// Python `app.py:3961-3989` 在两个 except 块都 best-effort delete
	defer cleanup()

	// 1. 创建购物车
	var cartResult map[string]interface{}
	if err := client.Post("/order/cart", map[string]interface{}{
		"ovhSubsidiary": subsidiary,
	}, &cartResult); err != nil {
		return Result{Success: false, Error: err.Error()}
	}
	cartID, _ = cartResult["cartId"].(string)
	state.Logger.Debug("购物车创建成功，ID: "+cartID, "price")

	// 2. 添加基础商品。
	// duration/pricingMode 在 order.cart.GenericProductCreation 里是必填,合法组合来自
	// GET /order/cart/{cartId}/eco 的 prices[](order.cart.GenericProductPricing)。
	// Eco 系列绝大多数就是 P1M/default,所以先按它发 —— 成功路径零额外 RTT;
	// 只有被 OVH 拒了才去查真实计价重试一次,避免非月付机型永久不可询价。
	baseDuration, basePricingMode := "P1M", "default"
	var itemResult map[string]interface{}
	postBase := func() error {
		itemResult = nil
		return client.Post("/order/cart/"+cartID+"/eco", map[string]interface{}{
			"planCode":    planCode,
			"pricingMode": basePricingMode,
			"duration":    baseDuration,
			"quantity":    1,
		}, &itemResult)
	}
	if err := postBase(); err != nil {
		msg := err.Error()
		if d, pm, found := lookupEcoPricing(state, client, cartID, planCode, baseDuration); found &&
			(d != baseDuration || pm != basePricingMode) {
			state.Logger.Warn(fmt.Sprintf("以 %s/%s 加购 %s 失败(%s)，改用目录计价 %s/%s 重试",
				baseDuration, basePricingMode, planCode, msg, d, pm), "price")
			baseDuration, basePricingMode = d, pm
			err = postBase()
		}
		if err != nil {
			msg = err.Error()
			if strings.Contains(msg, "is not available in") {
				state.Logger.Warn("配置在指定数据中心不可用: "+msg, "price")
				return Result{Success: false, Error: "该配置在指定数据中心不可用"}
			}
			// 跨区 planCode 的典型报错。实测在 EU 车上加购美区机型:
			// POST /order/cart/{id}/eco → 400 {"message":"Plan code not found 24rise01-v1-us"}。
			// OVH 三站目录独立(美区机型带 -us/-eu/-ca 后缀,欧区/加区不带),
			// 原样回吐这句英文,用户根本不知道是自己选错了区。
			if strings.Contains(msg, "Plan code not found") {
				state.Logger.Warn(fmt.Sprintf("planCode %s 不在子公司 %s 的目录里: %s", planCode, subsidiary, msg), "price")
				return Result{Success: false, Error: fmt.Sprintf(
					"机型 %s 不在该账户所属子公司 %s（%s 区）的目录里：OVH 的 EU / US / CA 三个站点目录互不相通，"+
						"美区机型带 -us / -eu / -ca 后缀，欧区和加区不带。请改用本区的 planCode，或换一个同区账户询价。",
					planCode, subsidiary, ovh.SubsidiaryRegion(subsidiary))}
			}
			return Result{Success: false, Error: msg}
		}
	}
	itemID, _ := numconv.ToInt64(itemResult["itemId"])
	if itemID == 0 {
		return Result{Success: false, Error: fmt.Sprintf("无法从购物车响应中解析 itemId（响应: %v）", itemResult)}
	}
	state.Logger.Debug(fmt.Sprintf("基础商品添加成功，项目 ID: %d", itemID), "price")

	// 3. 设置必需配置
	// 1:1 对应 Python app.py:3756-3761：dict 在 Py3.7+ 保持插入序：datacenter → os → region。
	// Go map 遍历顺序随机，若 region 先于 dedicated_datacenter 设置，OVH 可能返回 400
	// region 的合法取值由 (子公司, planCode) 决定:US 子公司一律 united_states,
	// 欧区是 canada/europe。按机房静态推是错的 —— 详见 catalog.ResolveRegion
	region, regionSrc := catalog.ResolveRegion(state, accountID, planCode, apiDC)
	if regionSrc == "fallback" {
		// ResolveRegion 的兜底分支用的是账户原始 zone,zone 为空时(美区账户很常见)
		// 会算成 europe/canada,而美区目录里 143 个 plan 的 region 只有 united_states。
		// 用已归一化的 subsidiary 重算,和 purchase.go 保持同一口径。
		// 走 catalog.FallbackRegion:它会先把 ca-east-tor-a 这类长机房码归一成城市码,
		// 而 ovh.RegionForDCInSubsidiary 只认短码、遇到长码返回空串。
		if r := catalog.FallbackRegion(apiDC, subsidiary); r != region {
			state.Logger.Warn(fmt.Sprintf("目录兜底给出的 region %q 与子公司 %s 不符，改用 %q",
				region, subsidiary, r), "price")
			region, regionSrc = r, "fallback(subsidiary-corrected)"
		}
	}
	degraded := false
	degradedReason := ""
	if region == "" {
		// region 在三区目录里都是 isMandatory:true(IE/CA/US 三份公开目录逐个 plan 核对过),
		// 所以"推不出 region"不是"这个 plan 不需要 region",而是配不全车。
		// 先问购物车自己 —— requiredConfiguration 三区实测都带 allowedValues,
		// 是按本车子公司算出来的本区正确值(US → ["united_states"],EU/CA → ["canada","europe"])。
		var required []map[string]interface{}
		if err := client.Get(fmt.Sprintf("/order/cart/%s/item/%d/requiredConfiguration", cartID, itemID), &required); err != nil {
			state.Logger.Warn("获取必需配置失败: "+err.Error(), "price")
		} else {
			allowed, mandatory := regionAllowedValues(required)
			if v := pickAllowedRegion(allowed, catalog.FallbackRegion(apiDC, subsidiary)); v != "" {
				region, regionSrc = v, "requiredConfiguration"
			} else if mandatory {
				// 标 degraded 而不是硬失败:价格照样算得出来,但 purchase.go 对同样的情况是
				// fail-fast,quick-order 的闸门看 Degraded 就会拒绝入队,两边口径一致。
				degraded = true
				degradedReason = "必填配置 region 无法确定：子公司 " + subsidiary + " 的目录拉不到，购物车也没给出合法取值"
				state.Logger.Warn(degradedReason, "price")
			}
		}
	}
	if region == "" {
		state.Logger.Info(fmt.Sprintf("%s@%s 无需 region 配置(来源: %s)", planCode, apiDC, regionSrc), "price")
	} else {
		state.Logger.Debug(fmt.Sprintf("%s@%s 的 region = %s(来源: %s)", planCode, apiDC, region, regionSrc), "price")
	}
	type kv struct{ label, value string }
	configurations := []kv{
		{"dedicated_datacenter", apiDC},
		{"dedicated_os", "none_64.en"},
	}
	if region != "" {
		configurations = append(configurations, kv{"region", region})
	}
	for _, cfg := range configurations {
		body := map[string]interface{}{"label": cfg.label, "value": cfg.value}
		if err := client.Post(fmt.Sprintf("/order/cart/%s/item/%d/configuration", cartID, itemID), body, nil); err != nil {
			if cfg.label == "dedicated_datacenter" {
				// 机房没设上 → 购物车停留在 OVH 默认机房,summary 出来的是"别的机房的价格"。
				// 把它当成用户请求机房的价格返回会直接误导买不买的决策,必须硬失败。
				state.Logger.Error(fmt.Sprintf("设置机房 %s 失败: %s，本次询价作废", cfg.value, err.Error()), "price")
				return Result{Success: false, Error: "无法把购物车配置到指定机房：" + err.Error()}
			}
			// os / region 是 requiredConfiguration 里的必填项,没设上时价格可能仍然算得出来,
			// 但 purchase.go 对同样的失败是 fail-fast,所以这里标 degraded 让下单闸门拒绝放行。
			state.Logger.Warn(fmt.Sprintf("设置配置 %s 失败: %s", cfg.label, err.Error()), "price")
			degraded = true
			if degradedReason != "" {
				degradedReason += "；"
			}
			degradedReason += "必填配置 " + cfg.label + " 设置失败：" + err.Error()
		} else {
			state.Logger.Debug(fmt.Sprintf("设置配置: %s = %s", cfg.label, cfg.value), "price")
		}
	}

	// 4. 添加用户 addons。
	// 与 purchase.go 对齐:拉不到选项列表 / 选项没匹配上 / 加购失败,一律硬失败。
	// 否则返回的是"少了内存和硬盘的裸机型价格"却标 success:true,
	// 监控的价格校验和 quick-order 的放行闸门都只看 success + withTax,会照单放行。
	// 与 purchase.go 一样先剔掉许可证 / OS / 控制面板类选项:那些不在 eco/options 里,
	// 不过滤就会让"同一份 options 抢购能下单、询价却 400"。UI 路径前端已经滤过一遍
	// (web/src/lib/option-groups.ts 的 isHardwareOption),外部脚本 / Telegram 不一定。
	hwOptions := filterHardwareOptions(state, options)
	// addedPlanCodes 记的是真正发给 OVH 的那批 planCode(带区域后缀的完整值),
	// 调用方拿它才能知道价格里到底含了哪几项;回声请求参数会让短前缀看起来"被计价了"
	addedPlanCodes := []string{}
	if len(hwOptions) > 0 {
		var availableOpts []map[string]interface{}
		q := url.Values{}
		q.Set("planCode", planCode)
		if err := client.Get(fmt.Sprintf("/order/cart/%s/eco/options?%s", cartID, q.Encode()), &availableOpts); err != nil {
			state.Logger.Error("获取 Eco 选项列表失败: "+err.Error(), "price")
			return Result{Success: false, Error: "获取硬件选项列表失败：" + err.Error()}
		}
		state.Logger.Debug(fmt.Sprintf("找到 %d 个可用选项", len(availableOpts)), "price")
		for _, wanted := range hwOptions {
			// 分档匹配(matchEcoOption),与 purchase.go 完全对齐。
			// addon planCode 是带机型后缀的(三区实测:欧区 ram-128g-on-die-ecc-3600-24adv01-v3,
			// 美区 ram-128g-ecc-2933-24rise01-v1-us),而 availabilities 的 FQN 段是短前缀
			// (ram-128g-ecc-2933)。只做精确匹配的话,凡是从 FQN 推选项的调用方
			// (Telegram / 外部脚本 / 监控)在询价这一步就会被判成"该机房不可订购",
			// 抢购却能下单 —— 后缀越长的美区越容易踩。
			matched, matchedPC, tier := matchEcoOption(availableOpts, wanted)
			if matched != nil && matchedPC != wanted {
				state.Logger.Info("硬件选项 "+wanted+" 按「"+tier+"」匹配到 Eco 选项 "+matchedPC, "price")
			}
			if matched == nil {
				state.Logger.Error("硬件选项 "+wanted+" 不在 OVH 可用选项列表中", "price")
				return Result{Success: false, Error: "硬件选项 " + wanted + " 在该机房不可订购，无法定价"}
			}
			// duration / pricingMode 只存在于 order.cart.GenericProductPricing(prices[] 元素),
			// GenericOptionDefinition 顶层没有这两个字段,直接读恒为空
			duration, pricingMode := pickPricing(matched["prices"], baseDuration)
			// 发出去的必须是 OVH 目录里那条带区域后缀的完整 planCode(matchedPC),
			// 不是用户传进来的短前缀 —— 前缀匹配的意义就在这里
			optPayload := map[string]interface{}{
				"itemId":      itemID,
				"planCode":    matchedPC,
				"duration":    duration,
				"pricingMode": pricingMode,
				"quantity":    1,
			}
			if err := client.Post(fmt.Sprintf("/order/cart/%s/eco/options", cartID), optPayload, nil); err != nil {
				state.Logger.Error(fmt.Sprintf("添加选项 %s 失败: %s", matchedPC, err.Error()), "price")
				return Result{Success: false, Error: "添加硬件选项 " + matchedPC + " 失败：" + err.Error()}
			}
			addedPlanCodes = append(addedPlanCodes, matchedPC)
			state.Logger.Debug("成功添加选项: "+matchedPC, "price")
		}
		state.Logger.Info(fmt.Sprintf("共添加 %d 个选项: %v", len(addedPlanCodes), addedPlanCodes), "price")
	}

	// 5. 绑定购物车。schema 里 POST /order/cart/{cartId}/assign 只有 path 参数、没有 body,
	// 传 {} 会被算进请求签名,OVH 收紧校验时会变成 400
	if err := client.Post("/order/cart/"+cartID+"/assign", nil, nil); err != nil {
		state.Logger.Warn("绑定购物车失败（可能不需要）: "+err.Error(), "price")
	}

	// 6. 获取 summary。
	// 这里以前还会先 GET /order/cart/{cartId}：但 order.cart.Cart.items 是 long[](纯 itemId),
	// 按对象数组解析永远拿不到明细，白白多一次能让整次询价硬失败的调用，已删。
	// 逐项明细改从 summary(order.Order)的 details(order.OrderDetail[]) 取。
	// 1:1 对应 Python app.py:3812-3813：OVH 错误直接抛进外层 except 返回 success:false。
	// 之前 Go 静默忽略会导致瞬断时 success:true 但价格全 nil，前端误以为有效价格 0
	var cartSummary map[string]interface{}
	if err := client.Get("/order/cart/"+cartID+"/summary", &cartSummary); err != nil {
		return Result{Success: false, Error: err.Error()}
	}

	priceInfo := &PriceInfo{
		PricingMode: "default",
		Prices: map[string]interface{}{
			"withTax":      nil,
			"withoutTax":   nil,
			"tax":          nil,
			"currencyCode": nil,
		},
		Items: []map[string]interface{}{},
	}

	// 从 summary 提取价格
	if cartSummary != nil {
		if pricesField, ok := cartSummary["prices"].(map[string]interface{}); ok {
			withTaxVal, withTaxCurrency := extractPriceField(pricesField["withTax"])
			withoutTaxVal, _ := extractPriceField(pricesField["withoutTax"])
			taxVal, _ := extractPriceField(pricesField["tax"])

			currency := withTaxCurrency
			if currency == "" {
				if c, ok := pricesField["currencyCode"].(string); ok {
					currency = c
				}
			}
			if currency == "" {
				_, currency = extractPriceField(pricesField["withoutTax"])
			}
			// 以前这里兜底写死 "EUR"。币种是跟子公司走的,不是跟站点走的:
			// 实测公开目录 locale.currencyCode —— IE=EUR / CA=QC=CAD / US=WE=WS=USD /
			// SG=SGD / AU=AUD。给一张美元报价盖上 EUR 会直接误导花多少钱,
			// 宁可留空让上层显示"未知"。
			if currency == "" {
				state.Logger.Warn("summary 未返回 currencyCode，币种留空（不再默认 EUR：子公司 "+
					subsidiary+" 未必是欧元计价）", "price")
			}

			priceInfo.Prices["withTax"] = withTaxVal
			priceInfo.Prices["withoutTax"] = withoutTaxVal
			priceInfo.Prices["tax"] = taxVal
			priceInfo.Prices["currencyCode"] = currency
		}
	}

	// 每个商品的价格：order.Order.details[] 里每条是 order.OrderDetail
	// (description / cartItemID / quantity / totalPrice / unitPrice，后两个是 order.Price)。
	// 注意 order.OrderDetail 没有含税/不含税拆分，税只在订单级 prices 上给，所以这里不再造
	// withTax/withoutTax 字段，避免把不含税单价当含税价展示。
	if cartSummary != nil {
		if detailsRaw, ok := cartSummary["details"].([]interface{}); ok {
			for _, detailRaw := range detailsRaw {
				d, ok := detailRaw.(map[string]interface{})
				if !ok {
					continue
				}
				totalVal, currency := extractPriceField(d["totalPrice"])
				unitVal, unitCurrency := extractPriceField(d["unitPrice"])
				if currency == "" {
					currency = unitCurrency
				}
				// 同上:不再默认 EUR,取不到就留空
				priceInfo.Items = append(priceInfo.Items, map[string]interface{}{
					"itemId":       d["cartItemID"],
					"description":  d["description"],
					"quantity":     d["quantity"],
					"totalPrice":   totalVal,
					"unitPrice":    unitVal,
					"currencyCode": currency,
				})
			}
		}
	}

	withTaxStr := fmt.Sprintf("%v", priceInfo.Prices["withTax"])
	currencyStr := fmt.Sprintf("%v", priceInfo.Prices["currencyCode"])
	state.Logger.Info(fmt.Sprintf("价格查询成功: 总价含税=%s %s", withTaxStr, currencyStr), "price")

	if degraded {
		state.Logger.Warn("询价结果降级（购物车未完全配置成功）: "+degradedReason, "price")
	}
	return Result{
		Success:    true,
		PlanCode:   planCode,
		Datacenter: datacenter,
		// 回报实际参与计价的那批选项(已剔除许可证/OS 类),而不是原样回声请求参数,
		// 否则调用方会以为价格里含了那些没被加进购物车的项
		Options:        addedPlanCodes,
		Degraded:       degraded,
		DegradedReason: degradedReason,
		Price:          priceInfo,
	}
}

// orderSubsidiary 归一化并校验账户的询价子公司。口径与 purchase.go 的同名函数一致 ——
// 询价和抢购必须建在同一个子公司的车上,否则会出现"询价说 X 欧、下单是另一个价"。
//
// OVH 三个站点各自只认自家的 ovhSubsidiary,而且大小写敏感。实测 POST /order/cart:
//
//	eu.api.ovh.com       {"ovhSubsidiary":"IE"} → 200;"ie"/"US"/"CA" → 400 invalid ovhSubsidiary
//	api.us.ovhcloud.com  只有 "US" → 200
//	ca.api.ovh.com       只有 CA 系(CA/QC/SG/AU/...)→ 200
//
// 账户表里的 zone 可能是早期未归一化的小写值,也可能被直接改库改成跨大区的值,
// 所以这里再兜一次;归属表只有 internal/ovh/helpers.go 一份,这里只调它派生的函数。
func orderSubsidiary(state *app.State, acc types.OVHAccount) string {
	// 大写化 / zone 为空时按 endpoint 推,统一走 catalog.SubsidiaryOfAccount(全项目唯一口径),
	// 这里只在它之上再加一道"跨大区"校验
	sub := catalog.SubsidiaryOfAccount(acc)
	fallback := ovh.DefaultSubsidiaryForEndpoint(acc.Endpoint)
	if !ovh.KnownSubsidiary(sub) || ovh.SubsidiaryRegion(sub) != ovh.EndpointRegion(acc.Endpoint) {
		state.Logger.Warn(fmt.Sprintf("账户 %s 的子公司 %q 不属于 endpoint %s 所在的 %s 区（OVH 会以 invalid ovhSubsidiary 拒绝建车），本次改用 %s",
			acc.Name, acc.Zone, acc.Endpoint, ovh.EndpointRegion(acc.Endpoint), fallback), "price")
		return fallback
	}
	return sub
}

// regionAllowedValues / pickAllowedRegion 与 purchase.go 的同名函数保持同一份逻辑。
//
// schema 里 order.cart.ItemConfiguration 只声明 label/required/type,但三区实测
// GET /order/cart/{cartId}/item/{itemId}/requiredConfiguration 都会带 allowedValues,
// 而且这份值是按"这辆车的子公司 + 这个 planCode"算出来的,天然是本区正确值:
// US 车 → ["united_states"],EU/CA 车 → ["canada","europe"]。
func regionAllowedValues(required []map[string]interface{}) ([]string, bool) {
	for _, conf := range required {
		if label, _ := conf["label"].(string); label != "region" {
			continue
		}
		mandatory, _ := conf["required"].(bool)
		raw, _ := conf["allowedValues"].([]interface{})
		vals := make([]string, 0, len(raw))
		for _, v := range raw {
			if s, _ := v.(string); s != "" {
				vals = append(vals, s)
			}
		}
		return vals, mandatory
	}
	return nil, false
}

func pickAllowedRegion(allowed []string, prefer string) string {
	if len(allowed) == 0 {
		return ""
	}
	for _, a := range allowed {
		if strings.EqualFold(a, prefer) {
			return a
		}
	}
	return allowed[0]
}

// matchEcoOption 把用户/FQN 给的一个 addon 标识,映射到 /order/cart/{id}/eco/options
// 返回列表里唯一的那条 GenericOptionDefinition。返回 (选项对象, 它的完整 planCode, 命中档位)。
//
// 为什么不能"首个命中即 break"(这正是上一轮修复留下的尾巴):
// 同一个 plan 里存在互为前缀的存储 addon,短 FQN 段会同时前缀命中两条。三区实测
// (公开 eco 目录 addonFamilies × availabilities 的 FQN 段,2026-08):
//
//	EU / CA: 26sk50a-v1        的段 softraid-2x960nvme →
//	         softraid-2x960nvme-26sk50a-v1          月付 €0
//	         softraid-2x960nvme-2x6000sa-26sk50a-v1 月付 €24
//	US     : 26sk50a-v1-ca 和 26sk50a-v1-eu 同样各有一处
//
// 命中哪条取决于 OVH 返回数组的顺序,而这条直接决定账单 —— 必须取"剩余最短"的那条:
// 多出来的那段(-2x6000sa)是又编了一套盘,不是机型后缀。
//
// 分档口径与 catalog.matchAddonsForSegment 一致,顺序不能换:
//
//	① 原始码相等 ② 原始码互为 "x-" 前缀 ③ 标准化后相等 ④ 标准化后互为前缀
//
// 原始码两档必须排在标准化之前:catalog.StandardizeConfig 会把机型后缀吃成粘连残渣
// (…-26sk50a-v1 → …nvmea),正确项因此丢掉分隔符、反而掉到比错误项更低的档。
// 标准化两档不能删:OVH 的 FQN 段和目录 addon 的内存频率经常对不上 —— 三区实测
// EU/CA 各 25 段、US 49 段原始码完全匹配不上,其中 EU/CA 各 7 段、US 14 段
// 只有标准化才认得出是同一档内存(26sk10b-v1 的段 ram-32g-ecc-2133 → 目录
// ram-32g-ecc-2400-26sk10b-v1;24rise06-v1 的段 ram-1024g-ecc-2933 → ram-1024g-ecc-3200-…)。
// 这些在旧实现里是"选项不存在"整次询价/整单失败,不是能用的功能。
func matchEcoOption(opts []map[string]interface{}, wanted string) (map[string]interface{}, string, string) {
	w := strings.ToLower(strings.TrimSpace(wanted))
	if w == "" {
		return nil, "", ""
	}
	wStd := catalog.StandardizeConfig(w)

	type cand struct {
		opt      map[string]interface{}
		code     string
		strength int // 同档内:命中的公共前缀越长越可信
		size     int // 同档同强度:整体越短越可信(多出来的段=多编了一套配置)
	}
	tierNames := [4]string{"原样相等", "原始码前缀", "标准化相等", "标准化前缀"}
	var tiers [4]*cand

	for _, o := range opts {
		code, _ := o["planCode"].(string)
		c := strings.ToLower(strings.TrimSpace(code))
		if c == "" {
			continue
		}
		cStd := catalog.StandardizeConfig(c)
		tier, strength := -1, 0
		switch {
		case c == w:
			tier, strength = 0, len(c)
		case strings.HasPrefix(c, w+"-"):
			tier, strength = 1, len(w)
		case strings.HasPrefix(w, c+"-"):
			tier, strength = 1, len(c)
		case cStd != "" && cStd == wStd:
			tier, strength = 2, len(cStd)
		case cStd != "" && wStd != "" && strings.HasPrefix(cStd, wStd):
			tier, strength = 3, len(wStd)
		case cStd != "" && wStd != "" && strings.HasPrefix(wStd, cStd):
			tier, strength = 3, len(cStd)
		}
		if tier < 0 {
			continue
		}
		cur := tiers[tier]
		if cur == nil || strength > cur.strength || (strength == cur.strength && len(c) < cur.size) {
			tiers[tier] = &cand{opt: o, code: code, strength: strength, size: len(c)}
		}
	}
	for i, t := range tiers {
		if t != nil {
			return t.opt, t.code, tierNames[i]
		}
	}
	return nil, "", ""
}

// pickPricing 从 order.cart.GenericProductPricing[] 里挑一条计价填 duration / pricingMode。
//
// 优先级:duration 必须与主商品一致 > 同 duration 内优先 rental+renew(按周期续费那条)
// > 实在没有同 duration 才退而求其次;prices 为空或结构异常时回退 P1M/default。
// 为什么 duration 一致排第一:addon 的 prices[] 常见 P1M/P12M/P24M 三条都是 rental+renew,
// 先按 rental+renew 挑等于取数组里碰巧的第一条、跟主商品 duration 无关;
// 一旦挑中与主商品不同的 duration,OVH 直接拒这个 addon,而这里是 fail-fast
// (整次询价 success:false),代价远大于"少续了一档"。
// 与 purchase.go 的 pickPricing 必须保持同一份逻辑。
func pickPricing(pricesRaw interface{}, preferDuration string) (string, string) {
	list, _ := pricesRaw.([]interface{})
	type cand struct{ duration, pricingMode string }
	var sameDurRenew, sameDur, anyRenew, first *cand
	for _, pRaw := range list {
		p, ok := pRaw.(map[string]interface{})
		if !ok {
			continue
		}
		d, _ := p["duration"].(string)
		pm, _ := p["pricingMode"].(string)
		if d == "" || pm == "" {
			continue
		}
		c := &cand{duration: d, pricingMode: pm}
		pt, _ := p["pricingType"].(string)
		isRenew := pt == "rental" && hasCapacity(p["capacities"], "renew")
		if first == nil {
			first = c
		}
		if isRenew && anyRenew == nil {
			anyRenew = c
		}
		if d == preferDuration {
			if sameDur == nil {
				sameDur = c
			}
			if isRenew && sameDurRenew == nil {
				sameDurRenew = c
			}
		}
	}
	switch {
	case sameDurRenew != nil:
		return sameDurRenew.duration, sameDurRenew.pricingMode
	case sameDur != nil:
		return sameDur.duration, sameDur.pricingMode
	case anyRenew != nil:
		return anyRenew.duration, anyRenew.pricingMode
	case first != nil:
		return first.duration, first.pricingMode
	}
	return "P1M", "default"
}

// filterHardwareOptions 剔除许可证 / OS / 控制面板类选项,只留真正决定机器配置的 addon。
// 口径与 purchase.go 的同名函数一致 —— 两处必须同进同退,否则会出现
// "询价说这个组合不可定价、抢购却能下单"(或反之)的分裂。
// 这里复制一份而不是 import purchase:price 是 purchase 的下游(quick_order 先询价再入队),
// 反向依赖会成环。
func filterHardwareOptions(state *app.State, opts []string) []string {
	filtered := []string{}
	for _, opt := range opts {
		if opt == "" {
			continue
		}
		lc := strings.ToLower(opt)
		skip := false
		for _, term := range []string{"windows-server", "sql-server", "cpanel-license", "plesk-",
			"-license-", "os-", "control-panel", "license", "security"} {
			if strings.Contains(lc, term) {
				skip = true
				break
			}
		}
		if skip {
			state.Logger.Debug("询价跳过非硬件/许可证选项: "+opt, "price")
			continue
		}
		filtered = append(filtered, opt)
	}
	return filtered
}

func hasCapacity(raw interface{}, want string) bool {
	list, _ := raw.([]interface{})
	for _, c := range list {
		if s, _ := c.(string); s == want {
			return true
		}
	}
	return false
}

// lookupEcoPricing 查 GET /order/cart/{cartId}/eco(order.cart.GenericProductDefinition[])
// 里某个 planCode 的真实可用计价。只在按 P1M/default 加购被 OVH 拒之后才调,
// 这样正常的月付机型一次多余请求都不会发。
func lookupEcoPricing(state *app.State, client *ovhsdk.Client, cartID, planCode, preferDuration string) (string, string, bool) {
	var defs []map[string]interface{}
	if err := client.Get("/order/cart/"+cartID+"/eco", &defs); err != nil {
		state.Logger.Warn("查询 Eco 计价失败: "+err.Error(), "price")
		return "", "", false
	}
	for _, def := range defs {
		if pc, _ := def["planCode"].(string); pc != planCode {
			continue
		}
		d, pm := pickPricing(def["prices"], preferDuration)
		return d, pm, true
	}
	state.Logger.Warn("在 Eco 目录里未找到 planCode: "+planCode, "price")
	return "", "", false
}

// extractPriceField 兼容字典形式（{value, currencyCode}）与直接值，统一转 float64
// OVH SDK 用 UseNumber，数字是 json.Number，必须经过 numconv 才能拿到正确值
func extractPriceField(v interface{}) (interface{}, string) {
	if v == nil {
		return nil, ""
	}
	if m, ok := v.(map[string]interface{}); ok {
		currency, _ := m["currencyCode"].(string)
		if f, ok := numconv.ToFloat64(m["value"]); ok {
			return f, currency
		}
		return nil, currency
	}
	if f, ok := numconv.ToFloat64(v); ok {
		return f, ""
	}
	return nil, ""
}
