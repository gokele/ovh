package purchase

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/numconv"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// Outcome 一次抢购尝试的结果。
//
// 以前 PurchaseServer 只返回 bool,队列处理器分不出"这轮没货,下轮再来"和
// "这台机器这个账户永远买不到"。后者(跨区 planCode / 非 Eco 机型 / 账户被删)
// 会按 retryInterval 无限重试,而且每一轮都往 history 写一条失败记录 ——
// 一个手滑选错区的任务能刷满整个抢购历史。Fatal 就是用来终止这类任务的。
type Outcome struct {
	Success bool
	// Fatal:确定性失败,重试多少次结果都一样。队列处理器据此把任务直接置 failed。
	// 只有"换个时间点重试也必然是同一个结果"才允许置 true ——
	// 网络 / 429 / OVH 5xx / 无货 一律不是 Fatal。
	Fatal bool
	// Reason:Fatal 时给用户看的原因(已写进 history,这里带一份给日志)
	Reason string
	// Attempted:这一轮是否真的走到了"向 OVH 提交订单"这一步。
	// 无货的轮次是 false —— 抢购的常态就是绝大多数轮次都无货,
	// 把它们计进失败次数会让任务在还没真正尝试过几次时就被判死。
	Attempted bool
}

// PurchaseServer 对应 Python: purchase_server
// 多账户:用 item.AccountID 取对应 OVH client 和 subsidiary。
func PurchaseServer(state *app.State, item *types.QueueItem) Outcome {
	client, err := state.OVH.ClientFor(item.AccountID)
	if err != nil {
		// 账户不存在 / 凭据缺失 / endpoint 非法 —— 都是重试一万次也不会变的错。
		// 以前这里连 history 都不写,任务就在后台每 retryInterval 秒静默失败一次。
		errMsg := fmt.Sprintf("账户 %s 不可用: %s（重试无用，请检查账户是否还在、API 密钥是否填全）",
			item.AccountID, err.Error())
		state.Logger.Error("PurchaseServer: 取 OVH client 失败 ("+item.AccountID+"): "+err.Error(), "purchase")
		recordFailure(state, item, errMsg)
		return Outcome{Fatal: true, Reason: errMsg}
	}

	cartID := ""
	var itemID int64

	state.Logger.Info(fmt.Sprintf("开始为 %s 在 %s 的购买流程，选项: %v",
		item.PlanCode, item.Datacenter, item.Options), "purchase")

	// 检查可用性
	var availabilities []map[string]interface{}
	q := url.Values{}
	q.Set("planCode", item.PlanCode)
	if err := client.Get("/dedicated/server/datacenter/availabilities?"+q.Encode(), &availabilities); err != nil {
		errMsg := err.Error()
		state.Logger.Error(fmt.Sprintf("购买 %s 时发生 OVH API 错误: %s", item.PlanCode, errMsg), "purchase")
		recordFailure(state, item, errMsg)
		return Outcome{Attempted: true}
	}

	// 空数组 ≠ 暂时没货。OVH 三个站点的机型目录彼此独立,拿别的站点的 planCode 查
	// /dedicated/server/datacenter/availabilities 同样是 HTTP 200 + [](实测:
	// 24rise01-v1 在 US 站点 n=0、24rise01-v1-us 在 EU/CA 站点 n=0),
	// 和"这台机器暂时没货"长得一模一样。不区分的话跨区任务会按重试间隔永远刷下去,
	// 日志里只有一句"当前无货",用户永远看不出是自己选错了区的机型。
	// classifyPlan 给出确定性结论时直接判 Fatal:这类任务重试到天荒地老也不会变。
	if len(availabilities) == 0 {
		if verdict, msg := classifyPlan(state, item.AccountID, item.PlanCode); msg != "" {
			state.Logger.Error(fmt.Sprintf("%s（判定 %d）", msg, verdict), "purchase")
			recordFailure(state, item, msg)
			return Outcome{Fatal: true, Reason: msg}
		}
	}

	apiDC := ovh.ConvertDisplayDCToAPIDC(item.Datacenter)

	// dedicated.DatacenterAvailability 的粒度是 fqn（一整套硬件组合），不是 planCode：
	// "32G+HDD 有货" 不等于 "用户要的 64G+NVMe 有货"。用户指定了硬件选项时，
	// 只在 FQN 段集合覆盖了这些选项的条目里判有货。
	wantedHW := fqnRelevantOptions(availabilities, item.Options)
	candidates := availabilities
	if len(wantedHW) > 0 {
		matchedAvs := []map[string]interface{}{}
		for _, av := range availabilities {
			if fqn, _ := av["fqn"].(string); fqnCoversOptions(fqn, wantedHW) {
				matchedAvs = append(matchedAvs, av)
			}
		}
		if len(matchedAvs) > 0 {
			candidates = matchedAvs
		} else {
			// wantedHW 里每一项都在本次 availabilities 的 FQN 段里出现过（否则会被
			// fqnRelevantOptions 剔掉），却没有任何一条 FQN 同时覆盖它们 ——
			// 说明 OVH 根本不供这套组合，直接判无货。
			// 这里退回全量判定就会重演"32G+HDD 有货 → 把 64G+NVMe 的单也下出去"。
			state.Logger.Info(fmt.Sprintf("服务器 %s 没有任何配置组合同时包含所选硬件 %v，视为该配置无货",
				item.PlanCode, wantedHW), "purchase")
			return Outcome{}
		}
	}

	foundAvailable := false
	// 记下"实际可用的那条 FQN"。FQN 格式：<planCode>.<addon1>.<addon2>...
	// 用户没显式指定 options 时，会从这个 FQN 推断 addon，让订单走"有货的那套配置"，
	// 不再退化到 OVH 默认 addon（多半是 HDD / 最小内存）。
	var availableFQN string
	for _, av := range candidates {
		if dcsRaw, ok := av["datacenters"].([]interface{}); ok {
			for _, dcRaw := range dcsRaw {
				dc, ok := dcRaw.(map[string]interface{})
				if !ok {
					continue
				}
				dcName, _ := dc["datacenter"].(string)
				availStr, _ := dc["availability"].(string)
				// 白名单判定：只有 \d+H 家族算有货。comingSoon 是"即将上线尚未开售"，
				// 以前被当成有货，会为永远下不了单的机型跑完整个下单流程、白刷 OVH 限流额度。
				if dcName == apiDC && catalog.IsAvailableForOrder(availStr) {
					foundAvailable = true
					if fqn, ok := av["fqn"].(string); ok {
						availableFQN = fqn
					}
					break
				}
			}
		}
		if foundAvailable {
			break
		}
	}
	if !foundAvailable {
		state.Logger.Info(fmt.Sprintf("服务器 %s 在数据中心 %s 当前无货", item.PlanCode, item.Datacenter), "purchase")
		return Outcome{}
	}

	// 决定本次下单使用的硬件 options：
	// - 用户显式指定了 options → 直接用（fail-fast 由后面的 eco/options 处理）
	// - 用户没指定 → 从可用 FQN 推断 addon planCode，确保订单走"实际有货的那套配置"
	effectiveOptions := item.Options
	if len(effectiveOptions) == 0 && availableFQN != "" {
		parts := strings.Split(availableFQN, ".")
		if len(parts) > 1 {
			// 第一段是 base planCode，其余是 addon 段。注意这些段是"短前缀"
			// （ram-128g-noecc-2933），不是 eco/options 里带机型后缀的完整 planCode
			// （ram-128g-noecc-2933-rise），下面匹配 addon 时要允许前缀命中。
			effectiveOptions = parts[1:]
			state.Logger.Info(fmt.Sprintf("用户未指定硬件选项，从可用 FQN %s 推断 addon: %v",
				availableFQN, effectiveOptions), "purchase")
		}
	}

	// 多账户:购物车 subsidiary 跟着账户走,不再读全局 cfg
	acc, _ := state.FindAccount(item.AccountID)
	subsidiary := orderSubsidiary(state, acc, "purchase")

	// 创建购物车
	state.Logger.Info(fmt.Sprintf("为区域 %s 创建购物车 (账户 %s)", subsidiary, acc.Name), "purchase")
	var cartResult map[string]interface{}
	if err := client.Post("/order/cart", map[string]interface{}{
		"ovhSubsidiary": subsidiary,
	}, &cartResult); err != nil {
		state.Logger.Error(fmt.Sprintf("购买 %s 时发生 OVH API 错误: %s", item.PlanCode, err.Error()), "purchase")
		recordFailure(state, item, err.Error())
		return Outcome{Attempted: true}
	}
	cartID, _ = cartResult["cartId"].(string)
	state.Logger.Info("购物车创建成功，ID: "+cartID, "purchase")

	// 抢购失败时清理 OVH 购物车,避免 OVH 侧堆积僵尸 cart(高频抢购累计能上千个,
	// 进而触发 OVH 限流)。checkout 成功时 cart 自动转 order,Delete 会 404,
	// 所以只在 !success 时尝试,且失败不影响主流程。
	success := false
	defer func() {
		if success || cartID == "" {
			return
		}
		if err := client.Delete("/order/cart/"+cartID, nil); err != nil {
			state.Logger.Debug(fmt.Sprintf("清理失败 cart %s: %s", cartID, err.Error()), "purchase")
		} else {
			state.Logger.Debug("已清理失败 cart "+cartID, "purchase")
		}
	}()

	// 立即绑定购物车到账户 —— 对齐 OVH 官方 PHP / Python 示例的推荐顺序：
	// cart → assign → eco → configuration → options → summary → checkout。
	// 在 add item 之前 assign，OVH 后端不会出现"cart 未绑定就 checkout"的边界错误。
	// schema 里 POST /order/cart/{cartId}/assign 只有 path 参数、没有 body（对比同一个
	// 命名空间下的 /eco 与 /checkout 都明确列了 body），传 {} 会被算进请求签名，
	// OVH 一旦收紧参数校验就会在绑定这一步整单失败
	state.Logger.Info("绑定购物车 "+cartID, "purchase")
	if err := client.Post("/order/cart/"+cartID+"/assign", nil, nil); err != nil {
		errMsg := err.Error()
		state.Logger.Error(fmt.Sprintf("购买 %s 时发生 OVH API 错误: %s", item.PlanCode, errMsg), "purchase")
		state.Logger.Error("错误发生时的购物车ID: "+cartID, "purchase")
		recordFailure(state, item, errMsg)
		return Outcome{Attempted: true}
	}
	state.Logger.Info("购物车绑定成功", "purchase")

	// 添加基础商品 /eco。
	// duration / pricingMode 在 order.cart.GenericProductCreation 里是必填，合法组合来自
	// GET /order/cart/{cartId}/eco 的 prices[]（order.cart.GenericProductPricing）。
	// Eco 系列的标准计价就是 P1M/default，所以先直接按它发 —— 抢购主链路上成功路径
	// 一次多余请求都不发；只有被 OVH 拒了才去查真实计价重试一次，
	// 免得不按月付计价的机型永远下不了单。
	state.Logger.Info(fmt.Sprintf("添加基础商品 %s 到购物车 (使用 /eco)", item.PlanCode), "purchase")
	baseDuration, basePricingMode := "P1M", "default"
	var itemResult map[string]interface{}
	postBase := func() error {
		itemResult = nil
		return client.Post("/order/cart/"+cartID+"/eco", map[string]interface{}{
			"planCode":    item.PlanCode,
			"pricingMode": basePricingMode,
			"duration":    baseDuration,
			"quantity":    1,
		}, &itemResult)
	}
	if err := postBase(); err != nil {
		if d, pm, found := lookupEcoPricing(state, client, cartID, item.PlanCode); found &&
			(d != baseDuration || pm != basePricingMode) {
			state.Logger.Warn(fmt.Sprintf("以 %s/%s 加购 %s 失败(%s)，改用目录计价 %s/%s 重试",
				baseDuration, basePricingMode, item.PlanCode, err.Error(), d, pm), "purchase")
			baseDuration, basePricingMode = d, pm
			err = postBase()
		}
		if err != nil {
			state.Logger.Error(fmt.Sprintf("购买 %s 时发生 OVH API 错误: %s", item.PlanCode, err.Error()), "purchase")
			state.Logger.Error(fmt.Sprintf("错误发生时的购物车ID: %s", cartID), "purchase")
			recordFailure(state, item, err.Error())
			return Outcome{Attempted: true}
		}
	}
	if n, ok := numconv.ToInt64(itemResult["itemId"]); ok {
		itemID = n
	}
	if itemID == 0 {
		errMsg := fmt.Sprintf("无法从购物车响应中解析 itemId（响应: %v）", itemResult)
		state.Logger.Error(fmt.Sprintf("购买 %s 时发生未知错误: %s", item.PlanCode, errMsg), "purchase")
		state.Logger.Error("错误发生时的购物车ID: "+cartID, "purchase")
		recordFailure(state, item, errMsg)
		return Outcome{Attempted: true}
	}
	state.Logger.Info(fmt.Sprintf("基础商品添加成功，项目 ID: %d", itemID), "purchase")

	// 设置必需配置
	state.Logger.Info(fmt.Sprintf("为项目 %d 设置必需配置", itemID), "purchase")
	// region 必须按 (子公司, planCode) 从目录取:US 子公司所有机型都是 united_states,
	// 欧区只有 canada/europe(亚太机型归 canada)。老的静态机房映射会发出目录里
	// 根本不存在的 "usa"/"apac",美区每一单都卡在这里。
	region, regionSrc := catalog.ResolveRegion(state, item.AccountID, item.PlanCode, apiDC)
	if regionSrc == "fallback" {
		// catalog.ResolveRegion 的兜底分支拿的是账户原始 zone,zone 为空时(美区账户很常见,
		// 建号时不填就是空)会算成 europe/canada,而美区目录里 143 个 plan 的 region
		// 只有 united_states 一个取值 —— 目录一拉不动,美区每一单都会提交本区不存在的 region。
		// 这里用已经归一化过的 subsidiary 重算一次。
		// 必须走 catalog.FallbackRegion 而不是 ovh.RegionForDCInSubsidiary:
		// 后者只认 gra/bhs 这种短机房码,而可用性接口会返回 ca-east-tor-a / eu-west-par-a
		// 这类长码,直接问它会拿到空串,反而把已经算对的 region 覆盖掉。
		if r := catalog.FallbackRegion(apiDC, subsidiary); r != region {
			state.Logger.Warn(fmt.Sprintf("目录兜底给出的 region %q 与子公司 %s 不符，改用 %q",
				region, subsidiary, r), "purchase")
			region, regionSrc = r, "fallback(subsidiary-corrected)"
		}
	}
	if region == "" {
		// 目录拉不动 + 静态兜底也认不出机房时的最后一招:问购物车自己。
		// requiredConfiguration 的 schema 只声明 label/required/type,但三区实测都会返回
		// allowedValues,而且这份值是这辆车(= 这个子公司 + 这个 planCode)算出来的,
		// 天然是本区正确值:US 车返回 ["united_states"],EU/CA 车返回 ["canada","europe"]。
		// 老实现只用它判"required 吗"然后整单取消,等于守着答案不看。
		state.Logger.Warn(fmt.Sprintf("无法为数据中心 %s 推断区域，改问购物车的 requiredConfiguration",
			strings.ToLower(apiDC)), "purchase")
		var required []map[string]interface{}
		if err := client.Get(fmt.Sprintf("/order/cart/%s/item/%d/requiredConfiguration", cartID, itemID), &required); err != nil {
			state.Logger.Warn("获取必需配置失败: "+err.Error(), "purchase")
		} else {
			allowed, mandatory := regionAllowedValues(required)
			if v := pickAllowedRegion(allowed, catalog.FallbackRegion(apiDC, subsidiary)); v != "" {
				region, regionSrc = v, "requiredConfiguration"
			} else if mandatory {
				errMsg := fmt.Sprintf("必需的区域配置无法确定：子公司 %s 的目录拉不到，购物车也没给出 region 的合法取值。", subsidiary)
				state.Logger.Error(fmt.Sprintf("购买 %s 时发生未知错误: %s", item.PlanCode, errMsg), "purchase")
				recordFailure(state, item, errMsg)
				return Outcome{Attempted: true}
			}
		}
	}
	if region != "" {
		state.Logger.Info(fmt.Sprintf("区域配置: %s = %s(来源: %s)", apiDC, region, regionSrc), "purchase")
	}

	// 与 Python 一致的顺序：dedicated_datacenter → dedicated_os → (region)
	type kv struct{ label, value string }
	configurations := []kv{
		{"dedicated_datacenter", apiDC},
		{"dedicated_os", "none_64.en"},
	}
	if region != "" {
		configurations = append(configurations, kv{"region", region})
	}
	// 三个 configuration 严格串行：dedicated_datacenter → dedicated_os → region。
	// schema 没声明配置项之间的顺序依赖，但 region 的合法取值本身就依赖机房，
	// price.go 也一直是串行的；抢购主链路上宁可多两次顺序 RTT，
	// 也不赌 OVH 对同一 cart item 并发写的宽容度（失败就是整单取消，货就没了）。
	postConfig := func(label, value string) error {
		state.Logger.Info(fmt.Sprintf("配置项目 %d: 设置必需项 %s = %s", itemID, label, value), "purchase")
		if err := client.Post(fmt.Sprintf("/order/cart/%s/item/%d/configuration", cartID, itemID),
			map[string]interface{}{"label": label, "value": value}, nil); err != nil {
			return err
		}
		state.Logger.Info(fmt.Sprintf("成功设置必需项: %s = %s", label, value), "purchase")
		return nil
	}
	for _, cfg := range configurations {
		if err := postConfig(cfg.label, cfg.value); err != nil {
			errMsg := err.Error()
			state.Logger.Error(fmt.Sprintf("购买 %s 时发生 OVH API 错误(%s): %s", item.PlanCode, cfg.label, errMsg), "purchase")
			state.Logger.Error(fmt.Sprintf("错误发生时的购物车ID: %s", cartID), "purchase")
			state.Logger.Error(fmt.Sprintf("错误发生时的基础商品ID: %d", itemID), "purchase")
			recordFailure(state, item, errMsg)
			return Outcome{Attempted: true}
		}
	}

	// 硬件选项处理。effectiveOptions 已经包含了：
	//   - 用户显式 options（如果有），或
	//   - 从可用 FQN 推断的 addon planCode（用户没指定时）
	if len(effectiveOptions) > 0 {
		state.Logger.Info(fmt.Sprintf("📦 处理硬件选项（%d个）: %v", len(effectiveOptions), effectiveOptions), "purchase")
		filtered := filterHardwareOptions(state, effectiveOptions, true)
		if len(filtered) > 0 {
			state.Logger.Info(fmt.Sprintf("过滤后的硬件选项计划代码: %v", filtered), "purchase")
			var availableEcoOpts []map[string]interface{}
			q := url.Values{}
			q.Set("planCode", item.PlanCode)
			if err := client.Get(fmt.Sprintf("/order/cart/%s/eco/options?%s", cartID, q.Encode()), &availableEcoOpts); err != nil {
				// 拉 eco/options 失败 → 中止订单。否则会用基础 plan 默认存储（多半是 HDD）下到错误配置
				errMsg := fmt.Sprintf("获取 Eco 硬件选项列表失败: %s（用户指定了 %d 个选项，无法验证，已取消下单避免下到错误配置）", err.Error(), len(filtered))
				state.Logger.Error(errMsg, "purchase")
				recordFailure(state, item, errMsg)
				return Outcome{Attempted: true}
			}
			state.Logger.Info(fmt.Sprintf("找到 %d 个可用的 Eco 硬件选项。", len(availableEcoOpts)), "purchase")

			// 先全部匹配,失败直接 fail-fast(避免任何 POST 都发出去之前先卡 missing)
			type addonPayload struct {
				planCode string
				body     map[string]interface{}
			}
			var todo []addonPayload
			var missing []string
			for _, wanted := range filtered {
				// 分档匹配(matchEcoOption,与 price.go 同一口径):用户从前端选来的是完整
				// planCode(第①档直接命中);用户没选配置时 filtered 是从 FQN 推来的短前缀,
				// 精确匹配永远打不中,必须靠前缀/标准化档兜底,否则会被误判成"选项不存在"整单取消。
				matchedOpt, matchedPC, tier := matchEcoOption(availableEcoOpts, wanted)
				if matchedOpt == nil {
					missing = append(missing, wanted)
					continue
				}
				if matchedPC != wanted {
					state.Logger.Info(fmt.Sprintf("硬件选项 %s 按「%s」匹配到 Eco 选项 %s", wanted, tier, matchedPC), "purchase")
				}
				// duration / pricingMode 只存在于 order.cart.GenericProductPricing
				// （prices[] 的元素）里，GenericOptionDefinition 顶层没有这两个字段，
				// 直接读顶层永远读不到、恒回退硬编码值
				duration, pricingMode := pickPricing(matchedOpt["prices"], baseDuration)
				todo = append(todo, addonPayload{
					planCode: matchedPC,
					body: map[string]interface{}{
						"itemId":      itemID,
						"planCode":    matchedPC,
						"duration":    duration,
						"pricingMode": pricingMode,
						"quantity":    1,
					},
				})
			}
			if len(missing) > 0 {
				errMsg := fmt.Sprintf("用户请求的硬件选项 %v 未在 OVH 可用 Eco 选项中找到（已取消下单避免下到错误配置）", missing)
				state.Logger.Error(errMsg, "purchase")
				recordFailure(state, item, errMsg)
				// 用户显式指定的选项对不上该子公司目录 = 确定性失败:eco/options 是按
				// (子公司, planCode) 算出来的固定集合,下一轮重试还是同一份,只会再刷一条失败 history。
				// 但"用户没指定、从 FQN 推来的"那批不算:下一轮有货的可能是另一条 FQN,
				// 推出来的段就不一样了,判死会把本来能抢到的任务掐掉。
				if len(item.Options) > 0 {
					return Outcome{Fatal: true, Reason: errMsg}
				}
				return Outcome{}
			}

			// 串行 POST 各 addon。每次 POST 服务端都要对同一 cart item 做 append +
			// 同 family 互斥(GenericOptionDefinition.exclusive)校验,schema 没有任何
			// 关于并发写同一 cart 的声明;通常只有 1-2 个 addon,串行代价不到 2 秒,
			// 不值得拿"整单取消"去赌 OVH 的并发宽容度。
			state.Logger.Info(fmt.Sprintf("依次添加 %d 个 Eco 选项: %v", len(todo), filtered), "purchase")
			for _, t := range todo {
				if err := client.Post(fmt.Sprintf("/order/cart/%s/eco/options", cartID), t.body, nil); err != nil {
					state.Logger.Error(fmt.Sprintf("添加 Eco 选项 %s 失败: %s", t.planCode, err.Error()), "purchase")
					// 关键选项添加失败 → 整单失败。不能静默继续 checkout,否则会下到错误配置。
					errMsg := fmt.Sprintf("添加 Eco 选项 %s 失败: %s（已取消下单避免下到错误配置）", t.planCode, err.Error())
					recordFailure(state, item, errMsg)
					return Outcome{Attempted: true}
				}
				state.Logger.Info(fmt.Sprintf("成功添加 Eco 选项: %s", t.planCode), "purchase")
			}
			state.Logger.Info(fmt.Sprintf("共成功添加 %d 个硬件选项。", len(filtered)), "purchase")
		}
	} else {
		state.Logger.Info("⚠️ 用户未提供任何硬件选项，将使用默认配置下单", "purchase")
	}

	// 直接结账 —— 跳过 /summary(它只是日志用的价格,2 秒开销),
	// 价格 + 过期时间下面 checkout 成功后用 /me/order 异步补,不阻塞主流程。
	state.Logger.Info("对购物车 "+cartID+" 执行结账", "purchase")
	var checkoutResult map[string]interface{}
	checkoutPayload := map[string]interface{}{
		"autoPayWithPreferredPaymentMethod": false,
		"waiveRetractationPeriod":           true,
	}
	if err := client.Post("/order/cart/"+cartID+"/checkout", checkoutPayload, &checkoutResult); err != nil {
		// POST /item/{id}/configuration 对取值不做任何校验 —— 实测在 EU 车上把
		// dedicated_datacenter 设成 "hil"、region 设成 "usa" 都会 200 返回配置项 id,
		// 直到 summary/checkout 才报 "<fqn> is not available in hil"。
		// 也就是说"配置全设成功"根本不代表值合法,错的机房/区域只能在这里现原形,
		// 必须把它翻成用户看得懂的话,否则历史里只有一句英文 OVH 报错。
		errMsg := err.Error()
		if strings.Contains(errMsg, "is not available in") {
			errMsg = fmt.Sprintf("%s（机房 %s 不在子公司 %s 的 %s 目录里；OVH 三站目录独立，同一机型在不同区可选的机房不同）",
				errMsg, apiDC, subsidiary, item.PlanCode)
		}
		state.Logger.Error(fmt.Sprintf("购买 %s 时发生 OVH API 错误: %s", item.PlanCode, errMsg), "purchase")
		recordFailure(state, item, errMsg)
		return Outcome{Attempted: true}
	}

	orderID := numconv.ToString(checkoutResult["orderId"])
	orderURL, _ := checkoutResult["url"].(string)

	// checkout 已返回订单 ID,cart 已成功转 order,标记成功阻止 defer 删除
	success = true

	// 立刻记成功 —— 价格和过期时间空着,后台异步补
	recordSuccess(state, item, orderID, orderURL, "", nil)

	// 异步补:从 /me/order/{orderID} 读 expirationDate + 价格,写回 history
	if orderID != "" {
		go backfillOrderDetail(state, client, item.ID, orderID)
	}

	state.Logger.Info(fmt.Sprintf("成功购买 %s 在 %s (订单ID: %s, URL: %s)",
		item.PlanCode, item.Datacenter, orderID, orderURL), "purchase")

	// 发送 Telegram 成功通知。TG token / chat id 仍然走全局 state.Config(Telegram 是平台级配置,跨账户共享)
	tgCfg := state.Config.Get()
	if tgCfg.TgToken != "" && tgCfg.TgChatID != "" {
		msg := fmt.Sprintf("🎉 OVH 服务器抢购成功！🎉\n\n服务器型号 (Plan Code): %s\n数据中心: %s\n订单 ID: %s\n订单链接: %s\n",
			item.PlanCode, item.Datacenter, orderID, orderURL)
		if len(item.Options) > 0 {
			msg += "自定义配置: " + strings.Join(item.Options, ", ") + "\n"
		}
		msg += "\n抢购任务ID: " + item.ID
		telegram.SendMessage(state, msg, nil)
		state.Logger.Info("已为订单 "+orderID+" 发送 Telegram 成功通知。", "purchase")
	} else {
		state.Logger.Info("未配置 Telegram Token 或 Chat ID，跳过成功通知发送。", "purchase")
	}
	return Outcome{Success: true}
}

// orderSubsidiary 归一化并校验账户的下单子公司。
//
// OVH 三个站点各自只认自家的 ovhSubsidiary,而且大小写敏感。实测 POST /order/cart:
//
//	eu.api.ovh.com       {"ovhSubsidiary":"IE"} → 200;"ie"/"US"/"CA" → 400 invalid ovhSubsidiary
//	api.us.ovhcloud.com  只有 "US" → 200
//	ca.api.ovh.com       只有 CA 系(CA/QC/SG/AU/...)→ 200
//
// 账户表里的 zone 可能是早期未归一化的小写值,也可能被直接改库改成跨大区的值
// (handlers/accounts.go 只在创建/更新时校验)。建车是抢购链路第一步,这一步 400
// 整单就没了,所以下单前再兜一次:大写化 + 校验归属大区,不合法退回 endpoint 的默认子公司。
// 归属表只有 internal/ovh/helpers.go 一份,这里只调它派生出来的函数,不另写映射。
func orderSubsidiary(state *app.State, acc types.OVHAccount, logTag string) string {
	// 大写化 / zone 为空时按 endpoint 推,统一走 catalog.SubsidiaryOfAccount(全项目唯一口径),
	// 这里只在它之上再加一道"跨大区"校验
	sub := catalog.SubsidiaryOfAccount(acc)
	fallback := ovh.DefaultSubsidiaryForEndpoint(acc.Endpoint)
	if !ovh.KnownSubsidiary(sub) || ovh.SubsidiaryRegion(sub) != ovh.EndpointRegion(acc.Endpoint) {
		state.Logger.Warn(fmt.Sprintf("账户 %s 的子公司 %q 不属于 endpoint %s 所在的 %s 区（OVH 会以 invalid ovhSubsidiary 拒绝建车），本次改用 %s",
			acc.Name, acc.Zone, acc.Endpoint, ovh.EndpointRegion(acc.Endpoint), fallback), logTag)
		return fallback
	}
	return sub
}

// planVerdict 机型归属判定。与 handlers/queue.go 的同名类型必须保持同一口径
// (那边挡入队、这边挡重试,两处口径一分裂就会出现"能入队但每轮都被判死"的任务)。
//
// 上一版把"不在 eco 目录"一律归因成"跨区 planCode",这是错的:eco 目录只回答
// "本工具能不能用 /order/cart/{id}/eco 下单它",不回答"它属于哪个大区"。
// 实测公开接口(2026-08):EU 站点 availabilities 有 244 个 planCode、eco 目录只有 99 个,
// 145 个差集里整条 Scale / HCI / SDS / High-Grade 产品线都在;US 站点是 423 对 143。
type planVerdict int

const (
	planVerdictOK          planVerdict = iota // 属于本区,而且本工具下得了单
	planVerdictUnknown                        // 探测/目录失败 —— 不下结论,保持"无货"语义继续重试
	planVerdictCrossRegion                    // ① 真跨区:别的大区才有它的库存记录
	planVerdictNotEco                         // ② 本区有它,但不属于 Eco 系列,本工具下不了
	planVerdictNoSuchPlan                     // ③ 三区都查不到:planCode 拼错 / 已下架
)

// classifyPlan 判定 planCode 对这个账户属于哪种情况,并给出面向用户的说明。
// 说明为空 = 无需提示(OK / Unknown),调用方必须保持原来的"无货,下轮再来"语义。
//
// 顺序有讲究:先查本子公司的 eco 目录(2 小时内存缓存,不耗账户配额、命中时不发网络请求),
// 有就直接放行 —— 抢购主链路上正常任务一次探测都不会多发。只有"目录里没有"才去探大区归属。
func classifyPlan(state *app.State, accountID, planCode string) (planVerdict, string) {
	acc, _ := state.FindAccount(accountID)
	accRegion := ovh.EndpointRegion(acc.Endpoint)
	subsidiary := catalog.SubsidiaryOfAccount(acc)

	_, catErr := catalog.AddonFamiliesForPlan(state, accountID, planCode)
	if catErr == nil {
		return planVerdictOK, ""
	}
	if !errors.Is(catErr, catalog.ErrPlanNotInCatalog) {
		// 目录拉不动(网络 / 429):不下结论,免得一次瞬断把正常任务判死
		state.Logger.Warn(fmt.Sprintf("判定 %s 归属时目录拉取失败(子公司 %s)，本次不下结论: %s",
			planCode, subsidiary, catErr.Error()), "purchase")
		return planVerdictUnknown, ""
	}

	// 本区排第一:命中就立刻返回,只花一次公开请求
	region, probeErr := catalog.RegionOfPlan(state, planCode, []string{accRegion, "EU", "US", "CA"})
	if probeErr != nil {
		state.Logger.Warn(fmt.Sprintf("探测 %s 的大区归属失败，本次不下结论: %s", planCode, probeErr.Error()), "purchase")
		return planVerdictUnknown, ""
	}

	switch {
	case region == "":
		return planVerdictNoSuchPlan, fmt.Sprintf(
			"机型 %s 在 OVH 的 EU / US / CA 三个站点都查不到任何库存记录（缺货的机型也会有记录）："+
				"planCode 可能拼错了，或者这个机型已经下架。",
			planCode)
	case region != accRegion:
		return planVerdictCrossRegion, fmt.Sprintf(
			"机型 %s 属于 OVH 的 %s 区，而账户 %s 在 %s 区（%s）。三个站点的目录 / 库存 / 购物车互不相通"+
				"（美区机型带 -us / -eu / -ca 后缀，欧区和加区不带），请改用本区的 planCode，或换一个 %s 区的账户下单。",
			planCode, region, acc.Name, accRegion, acc.Endpoint, region)
	}

	return planVerdictNotEco, fmt.Sprintf(
		"机型 %s 在本区（%s 区，子公司 %s）确实有库存记录，但它不在该子公司的 Eco 目录里 —— "+
			"Scale / HCI / SDS / High-Grade 这些产品线都不走 Eco（实测欧区 244 个有库存的机型里 145 个如此）。"+
			"本工具的下单链路只有 /order/cart/eco 一条，买不了这台，请到 OVH 官网下单。",
		planCode, accRegion, subsidiary)
}

// regionAllowedValues 从 requiredConfiguration 响应里取 region 的合法取值 + 是否必填。
//
// schema 里 order.cart.ItemConfiguration 只声明了 label/required/type,但三区实测
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

// pickAllowedRegion 从 allowedValues 里挑一个 region:优先取 prefer(按机房推出来的那个),
// 挑不中就取第一个 —— 美区只有一个候选,取第一个就等于取对。
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

// filterHardwareOptions 剔除非硬件 / 许可证类选项，只留下真正决定机器配置的 addon。
// （注意 "panel" 不在过滤词里：FQN 推断出来的 addon 不会撞这词，
// 留着会误伤；旧版有 "panel" 是因为前端可能塞 cpanel 选项过来）
func filterHardwareOptions(state *app.State, opts []string, logSkipped bool) []string {
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
			if logSkipped {
				state.Logger.Info("跳过非硬件/许可证选项: "+opt, "purchase")
			}
			continue
		}
		filtered = append(filtered, opt)
	}
	return filtered
}

// fqnSegmentMatchesOption FQN 段 vs catalog option planCode 的匹配。
// availabilities 的 FQN 段是"短前缀"（ram-128g-noecc-2933 /
// softraid-4x3840nvme-pcie-gen4），而 catalog 给前端的 option value 是带机型后缀的
// 完整 planCode（ram-128g-noecc-2933-rise / softraid-4x3840nvme-pcie-gen4-24adv01-v2），
// 直接相等永远不命中 —— 这正是"按 FQN 精确匹配"整条修复此前空转的根因。
// 口径与前端 web/src/hooks/use-availability.ts 的 partMatchesValue 保持一致：
// 相等，或任一方是对方的 "x-" 前缀（补 "-" 是为了防 ram-1 错配 ram-128g）。
func fqnSegmentMatchesOption(seg, opt string) bool {
	s := strings.ToLower(strings.TrimSpace(seg))
	o := strings.ToLower(strings.TrimSpace(opt))
	if s == "" || o == "" {
		return false
	}
	return s == o || strings.HasPrefix(o, s+"-") || strings.HasPrefix(s, o+"-")
}

// matchEcoOption 把用户/FQN 给的一个 addon 标识,映射到 /order/cart/{id}/eco/options
// 返回列表里唯一的那条 GenericOptionDefinition。返回 (选项对象, 它的完整 planCode, 命中档位)。
//
// 为什么不能"首个命中即 break":同一个 plan 里存在互为前缀的存储 addon,
// 短 FQN 段会同时前缀命中两条。三区实测(公开 eco 目录 addonFamilies × availabilities
// 的 FQN 段,2026-08):
//
//	EU / CA: 26sk50a-v1 的段 softraid-2x960nvme →
//	         softraid-2x960nvme-26sk50a-v1          月付 €0
//	         softraid-2x960nvme-2x6000sa-26sk50a-v1 月付 €24
//	US     : 26sk50a-v1-ca / 26sk50a-v1-eu 同样各有一处
//
// 命中哪条取决于 OVH 返回数组的顺序,而这条直接决定账单和到手的盘 ——
// 必须取"剩余最短"的那条:多出来的 -2x6000sa 是又编了一套盘,不是机型后缀。
//
// 分档口径与 catalog.matchAddonsForSegment / price.go 的同名函数完全一致,顺序不能换:
//
//	① 原始码相等 ② 原始码互为 "x-" 前缀 ③ 标准化后相等 ④ 标准化后互为前缀
//
// 原始码两档必须排在标准化之前:catalog.StandardizeConfig 会把机型后缀吃成粘连残渣
// (…-26sk50a-v1 → …nvmea),正确项因此丢掉分隔符、反而掉到比错误项更低的档。
// 标准化两档不能删:FQN 段与目录 addon 的内存频率经常对不上 —— 三区实测
// EU/CA 各 25 段、US 49 段原始码匹配不上,其中 EU/CA 各 7 段、US 14 段靠标准化才认得出
// (26sk10b-v1 的段 ram-32g-ecc-2133 → 目录 ram-32g-ecc-2400-26sk10b-v1),
// 这些在旧实现里全是"选项不存在"整单取消。
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

// fqnRelevantOptions 从用户提交的 options 里挑出真正参与 FQN 库存判定的硬件项。
//
// 前端抢购对话框提交的是所有分组的选中值（web/src/routes/servers.tsx 的 selectedValues），
// 里面混着 bandwidth-* / vrack-* / cpu 这类 addon —— 它们跟库存解耦、不出现在任何 FQN 里
// （前端 servers.tsx 自己也是这么判的）。拿它们去要求 FQN 覆盖，结果是每条 FQN 都不匹配，
// 整个"按配置判有货"退化成全量判定，等于没修。
//
// 判定不靠猜命名，而是用"本次 availabilities 里真实出现过的 FQN 段"当白名单：
// 段里查无此项的选项一律不参与覆盖判定。许可证/OS 类选项同样天然出局。
func fqnRelevantOptions(availabilities []map[string]interface{}, opts []string) []string {
	segs := []string{}
	for _, av := range availabilities {
		fqn, _ := av["fqn"].(string)
		parts := strings.Split(fqn, ".")
		if len(parts) < 2 {
			continue
		}
		segs = append(segs, parts[1:]...) // 第一段是 base planCode，不是 addon
	}
	relevant := []string{}
	for _, opt := range opts {
		if strings.TrimSpace(opt) == "" {
			continue
		}
		for _, seg := range segs {
			if fqnSegmentMatchesOption(seg, opt) {
				relevant = append(relevant, opt)
				break
			}
		}
	}
	return relevant
}

// fqnCoversOptions 判断一条 dedicated.DatacenterAvailability 的 fqn
// （格式 <planCode>.<addon1>.<addon2>...）是否覆盖了用户要的全部硬件选项。
func fqnCoversOptions(fqn string, opts []string) bool {
	if fqn == "" {
		return false
	}
	segs := strings.Split(fqn, ".")
	if len(segs) > 1 {
		segs = segs[1:] // 第一段是 base planCode
	}
	for _, want := range opts {
		hit := false
		for _, seg := range segs {
			if fqnSegmentMatchesOption(seg, want) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// pickPricing 从 order.cart.GenericProductPricing[] 里挑一条计价填 duration / pricingMode。
//
// 优先级：duration 必须与主商品一致 > 同 duration 内优先 rental+renew（按周期续费那条）
// > 实在没有同 duration 才退而求其次。
// 为什么 duration 一致排第一：addon 的 prices[] 常见 P1M/P12M/P24M 三条都是 rental+renew，
// 先按"rental+renew"挑等于取数组里碰巧的第一条，跟主商品 duration 无关；
// 一旦挑中不同 duration，OVH 直接拒这个 addon，而两处 addon POST 都是 fail-fast
// （抢购整单取消 / 询价 success:false），代价远大于"少续了一档"。
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

func hasCapacity(raw interface{}, want string) bool {
	list, _ := raw.([]interface{})
	for _, c := range list {
		if s, _ := c.(string); s == want {
			return true
		}
	}
	return false
}

// lookupEcoPricing 查 GET /order/cart/{cartId}/eco（order.cart.GenericProductDefinition[]）
// 里某个 planCode 的真实可用计价。只在按 P1M/default 加购被 OVH 拒之后才调，
// 保证正常月付机型的抢购链路上不会多一次请求。
func lookupEcoPricing(state *app.State, client *ovhsdk.Client, cartID, planCode string) (string, string, bool) {
	var defs []map[string]interface{}
	if err := client.Get("/order/cart/"+cartID+"/eco", &defs); err != nil {
		state.Logger.Warn("查询 Eco 计价失败: "+err.Error(), "purchase")
		return "", "", false
	}
	for _, def := range defs {
		if pc, _ := def["planCode"].(string); pc != planCode {
			continue
		}
		d, pm := pickPricing(def["prices"], "P1M")
		return d, pm, true
	}
	state.Logger.Warn("在 Eco 目录里未找到 planCode: "+planCode, "purchase")
	return "", "", false
}

func extract(v interface{}) *float64 {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]interface{}); ok {
		if f, ok := numconv.ToFloat64(m["value"]); ok {
			return &f
		}
		return nil
	}
	if f, ok := numconv.ToFloat64(v); ok {
		return &f
	}
	return nil
}

func recordSuccess(state *app.State, item *types.QueueItem, orderID, orderURL, expirationTime string, priceInfo *types.PriceInfo) {
	state.HistoryMu.Lock()
	defer state.HistoryMu.Unlock()
	now := types.NowISO()

	for i := range state.History {
		if state.History[i].TaskID == item.ID {
			state.History[i].Status = "success"
			state.History[i].AccountID = item.AccountID
			state.History[i].OrderID = orderID
			state.History[i].OrderURL = orderURL
			state.History[i].ErrorMessage = nil
			state.History[i].PurchaseTime = now
			state.History[i].AttemptCount = item.RetryCount
			state.History[i].Options = item.Options
			if expirationTime != "" {
				state.History[i].ExpirationTime = expirationTime
			}
			if priceInfo != nil {
				state.History[i].Price = priceInfo
			}
			state.Logger.Info("更新抢购历史(成功) 任务ID: "+item.ID, "purchase")
			go state.SaveHistory()
			return
		}
	}

	entry := types.PurchaseHistoryEntry{
		ID:           uuid.NewString(),
		TaskID:       item.ID,
		AccountID:    item.AccountID,
		PlanCode:     item.PlanCode,
		Datacenter:   item.Datacenter,
		Options:      item.Options,
		Status:       "success",
		OrderID:      orderID,
		OrderURL:     orderURL,
		PurchaseTime: now,
		AttemptCount: item.RetryCount,
	}
	if expirationTime != "" {
		entry.ExpirationTime = expirationTime
	}
	if priceInfo != nil {
		entry.Price = priceInfo
	}
	state.History = append(state.History, entry)
	state.Logger.Info("创建抢购历史(成功) 任务ID: "+item.ID, "purchase")
	go state.SaveHistory()
}

func recordFailure(state *app.State, item *types.QueueItem, errMsg string) {
	state.HistoryMu.Lock()
	defer state.HistoryMu.Unlock()
	now := types.NowISO()

	for i := range state.History {
		if state.History[i].TaskID == item.ID {
			state.History[i].Status = "failed"
			state.History[i].AccountID = item.AccountID
			state.History[i].OrderID = ""
			state.History[i].OrderURL = ""
			em := errMsg
			state.History[i].ErrorMessage = &em
			state.History[i].PurchaseTime = now
			state.History[i].AttemptCount = item.RetryCount
			state.History[i].Options = item.Options
			state.Logger.Info("更新抢购历史(失败) 任务ID: "+item.ID, "purchase")
			go state.SaveHistory()
			return
		}
	}
	em := errMsg
	entry := types.PurchaseHistoryEntry{
		ID:           uuid.NewString(),
		TaskID:       item.ID,
		AccountID:    item.AccountID,
		PlanCode:     item.PlanCode,
		Datacenter:   item.Datacenter,
		Options:      item.Options,
		Status:       "failed",
		ErrorMessage: &em,
		PurchaseTime: now,
		AttemptCount: item.RetryCount,
	}
	state.History = append(state.History, entry)
	state.Logger.Info("创建抢购历史(失败) 任务ID: "+item.ID, "purchase")
	go state.SaveHistory()
}

// backfillOrderDetail 下单成功后异步补 history 行的 expirationTime + price。
// 不阻塞 PurchaseServer 主流程,即便这一步失败 history 也已经标 success(只是少了价格 / 过期时间)。
// 在独立 goroutine 跑,持有 OVH client 引用,只 read /me/order/{orderID}。
func backfillOrderDetail(state *app.State, client *ovhsdk.Client, taskID, orderID string) {
	var orderInfo map[string]interface{}
	if err := client.Get("/me/order/"+orderID, &orderInfo); err != nil {
		state.Logger.Warn(fmt.Sprintf("异步查询订单 %s 详情失败: %s", orderID, err.Error()), "purchase")
		return
	}

	// billing.Order 里 expirationDate（订单待付款到期作废时间）与 retractionDate
	// （法定撤销权截止日）是两个语义完全不同的 datetime，历史里展示的"过期时间"
	// 指的是前者；何况 checkout 传了 waiveRetractationPeriod:true 已经放弃撤销期，
	// 拿 retractionDate 当付款截止时间会让用户误判付款窗口。
	expirationTime := ""
	if exp, ok := orderInfo["expirationDate"].(string); ok && exp != "" {
		expirationTime = exp
	}
	// 撤销权截止日单独存 RetractionTime,不混进 ExpirationTime(付款到期),
	// 两者语义不同,混用会让用户误判付款窗口
	retractionTime := ""
	if ret, ok := orderInfo["retractionDate"].(string); ok && ret != "" {
		retractionTime = ret
		state.Logger.Debug(fmt.Sprintf("订单 %s 撤销权截止日: %s", orderID, ret), "purchase")
	}

	// /me/order 返回的价格字段:priceWithTax / priceWithoutTax / tax,
	// 每个典型形式 { value: <number-or-json.Number>, currencyCode: string, text: string }。
	// 复用 extract(),它能容 float64 / json.Number / string / int 各种来法。
	pickCurrency := func(field interface{}) string {
		m, ok := field.(map[string]interface{})
		if !ok {
			return ""
		}
		c, _ := m["currencyCode"].(string)
		return c
	}

	var priceInfo *types.PriceInfo
	withTax := extract(orderInfo["priceWithTax"])
	withoutTax := extract(orderInfo["priceWithoutTax"])
	tax := extract(orderInfo["tax"])
	currency := pickCurrency(orderInfo["priceWithTax"])
	if currency == "" {
		currency = pickCurrency(orderInfo["priceWithoutTax"])
	}
	if currency == "" {
		currency = pickCurrency(orderInfo["tax"])
	}
	// 以前这里兜底写死 "EUR"。币种是按子公司定的,不是按站点:实测公开目录 locale.currencyCode
	// IE=EUR / CA=QC=CAD / US=WE=WS=USD / SG=SGD / AU=AUD ——
	// 给一张美元账单盖上 EUR 会直接误导花了多少钱。宁可留空让上层显示"未知"。
	if currency == "" {
		state.Logger.Warn(fmt.Sprintf("订单 %s 的价格里没有 currencyCode，币种留空（不再默认 EUR：美区/加区/亚太子公司都不是欧元计价）", orderID), "purchase")
	}
	if withTax != nil || withoutTax != nil {
		priceInfo = &types.PriceInfo{
			WithTax:      withTax,
			WithoutTax:   withoutTax,
			Tax:          tax,
			CurrencyCode: currency,
		}
	}

	if expirationTime == "" && priceInfo == nil {
		return
	}

	state.HistoryMu.Lock()
	defer state.HistoryMu.Unlock()
	for i := range state.History {
		if state.History[i].TaskID != taskID {
			continue
		}
		changed := false
		if expirationTime != "" && state.History[i].ExpirationTime != expirationTime {
			state.History[i].ExpirationTime = expirationTime
			changed = true
		}
		if retractionTime != "" && state.History[i].RetractionTime != retractionTime {
			state.History[i].RetractionTime = retractionTime
			changed = true
		}
		if priceInfo != nil && state.History[i].Price == nil {
			state.History[i].Price = priceInfo
			changed = true
		}
		if changed {
			state.Logger.Info(fmt.Sprintf("补全订单 %s 详情: 过期时间=%q 价格=%v",
				orderID, expirationTime, priceInfo != nil), "purchase")
			go state.SaveHistory()
		}
		return
	}
}
