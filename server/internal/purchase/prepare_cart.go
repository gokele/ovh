package purchase

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/numconv"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// prepareCart 只准备已选配置的购物车,不保留库存、不创建订单或付款。
func prepareCart(ctx context.Context, state *app.State, item *types.QueueItem, client *ovhsdk.Client, effectiveOptions []string, apiDC string, tl *timeline, timingKey string, preparing bool) (*preparedCart, Outcome) {
	fail := func(message string) {
		if !preparing {
			recordFailure(state, item, message)
		}
	}

	cartID := ""
	var itemID int64
	// 多账户:购物车 subsidiary 跟着账户走,不再读全局 cfg
	acc, _ := state.FindAccount(item.AccountID)
	subsidiary := orderSubsidiary(state, acc, "purchase")

	// 创建购物车
	state.Logger.Info(fmt.Sprintf("为区域 %s 创建购物车 (账户 %s)", subsidiary, acc.Name), "purchase")
	var cartResult map[string]interface{}
	if err := client.PostWithContext(ctx, "/order/cart", map[string]interface{}{
		"ovhSubsidiary": subsidiary,
	}, &cartResult); err != nil {
		if ctx.Err() != nil {
			return nil, cancelledOutcome(state, item, "建购物车")
		}
		state.Logger.Error(fmt.Sprintf("购买 %s 时发生 OVH API 错误: %s", item.PlanCode, err.Error()), "purchase")
		fail(ovh.Explain(err))
		return nil, attemptOutcome(err)
	}
	cartID, _ = cartResult["cartId"].(string)
	if cartID == "" {
		return nil, Outcome{Attempted: true, Reason: "OVH did not return a cart ID"}
	}
	tl.mark("建购物车")
	state.Logger.Info("购物车创建成功，ID: "+cartID, "purchase")

	// 抢购失败时清理 OVH 购物车,避免 OVH 侧堆积僵尸 cart(高频抢购累计能上千个,
	// 进而触发 OVH 限流)。checkout 成功时 cart 自动转 order,Delete 会 404,
	// 所以只在 !success 时尝试,且失败不影响主流程。
	success := false
	defer func() {
		if success || cartID == "" {
			return
		}
		// 故意不带 ctx:取消路径也要走到这里清 cart。挂上 ctx 的话,
		// 用户一删任务,清理请求自己先被取消,僵尸 cart 就留在 OVH 那边了。
		if err := client.Delete("/order/cart/"+cartID, nil); err != nil {
			state.Logger.Debug(fmt.Sprintf("清理失败 cart %s: %s", cartID, err.Error()), "purchase")
		} else {
			state.Logger.Debug("已清理失败 cart "+cartID, "purchase")
		}
	}()

	// 立即绑定购物车到账户 —— 对齐 OVH 官方示例的推荐顺序：
	// cart → assign → eco → configuration → options → summary → checkout。
	// 在 add item 之前 assign，OVH 后端不会出现"cart 未绑定就 checkout"的边界错误。
	// schema 里 POST /order/cart/{cartId}/assign 只有 path 参数、没有 body（对比同一个
	// 命名空间下的 /eco 与 /checkout 都明确列了 body），所以这里传 nil 而不是 {}。
	//
	// 注意别把理由记成"{} 会破坏签名"——go-ovh 的签名是对**实际发出的 body** 算的
	// （nil→空串，{}→"{}"），两边自洽，签名不会因此失配。真正的理由只是：
	// schema 没声明 body 就别发 body，少一个 OVH 将来收紧校验时会绊住的东西。
	state.Logger.Info("绑定购物车 "+cartID, "purchase")
	if err := client.PostWithContext(ctx, "/order/cart/"+cartID+"/assign", nil, nil); err != nil {
		if ctx.Err() != nil {
			return nil, cancelledOutcome(state, item, "绑定购物车")
		}
		errMsg := err.Error()
		state.Logger.Error(fmt.Sprintf("购买 %s 时发生 OVH API 错误: %s", item.PlanCode, errMsg), "purchase")
		state.Logger.Error("错误发生时的购物车ID: "+cartID, "purchase")
		fail(ovh.Explain(err))
		return nil, attemptOutcome(err)
	}
	tl.mark("绑定购物车")
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
		return client.PostWithContext(ctx, "/order/cart/"+cartID+"/eco", map[string]interface{}{
			"planCode":    item.PlanCode,
			"pricingMode": basePricingMode,
			"duration":    baseDuration,
			"quantity":    1,
		}, &itemResult)
	}
	if err := postBase(); err != nil {
		// 取消了就别再去查计价重试 —— lookupEcoPricing 还要发一次 OVH 请求
		if ctx.Err() != nil {
			return nil, cancelledOutcome(state, item, "加基础商品")
		}
		if d, pm, found := lookupEcoPricing(state, client, cartID, item.PlanCode); found &&
			(d != baseDuration || pm != basePricingMode) {
			state.Logger.Warn(fmt.Sprintf("以 %s/%s 加购 %s 失败(%s)，改用目录计价 %s/%s 重试",
				baseDuration, basePricingMode, item.PlanCode, err.Error(), d, pm), "purchase")
			baseDuration, basePricingMode = d, pm
			err = postBase()
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, cancelledOutcome(state, item, "加基础商品")
			}
			state.Logger.Error(fmt.Sprintf("购买 %s 时发生 OVH API 错误: %s", item.PlanCode, err.Error()), "purchase")
			state.Logger.Error(fmt.Sprintf("错误发生时的购物车ID: %s", cartID), "purchase")
			fail(ovh.Explain(err))
			return nil, attemptOutcome(err)
		}
	}
	if n, ok := numconv.ToInt64(itemResult["itemId"]); ok {
		itemID = n
	}
	if itemID == 0 {
		errMsg := fmt.Sprintf("无法从购物车响应中解析 itemId（响应: %v）", itemResult)
		state.Logger.Error(fmt.Sprintf("购买 %s 时发生未知错误: %s", item.PlanCode, errMsg), "purchase")
		state.Logger.Error("错误发生时的购物车ID: "+cartID, "purchase")
		fail(errMsg)
		// 这里 OVH 是 200 回来的,只是 body 里没有 itemId —— 不是传输问题,
		// 重试大概率还是同一个响应,该计数。
		return nil, Outcome{Attempted: true}
	}
	tl.mark("加购商品")
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
		if err := client.GetWithContext(ctx, fmt.Sprintf("/order/cart/%s/item/%d/requiredConfiguration", cartID, itemID), &required); err != nil {
			if ctx.Err() != nil {
				return nil, cancelledOutcome(state, item, "查必需配置")
			}
			state.Logger.Warn("获取必需配置失败: "+err.Error(), "purchase")
		} else {
			allowed, mandatory := regionAllowedValues(required)
			if v := pickAllowedRegion(allowed, catalog.FallbackRegion(apiDC, subsidiary)); v != "" {
				region, regionSrc = v, "requiredConfiguration"
			} else if mandatory {
				errMsg := fmt.Sprintf("必需的区域配置无法确定：子公司 %s 的目录拉不到，购物车也没给出 region 的合法取值。", subsidiary)
				state.Logger.Error(fmt.Sprintf("购买 %s 时发生未知错误: %s", item.PlanCode, errMsg), "purchase")
				fail(errMsg)
				return nil, Outcome{Attempted: true}
			}
		}
	}
	if region != "" {
		state.Logger.Info(fmt.Sprintf("区域配置: %s = %s(来源: %s)", apiDC, region, regionSrc), "purchase")
	}

	// 顺序固定：dedicated_datacenter → dedicated_os → (region)
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
		if err := client.PostWithContext(ctx, fmt.Sprintf("/order/cart/%s/item/%d/configuration", cartID, itemID),
			map[string]interface{}{"label": label, "value": value}, nil); err != nil {
			return err
		}
		state.Logger.Info(fmt.Sprintf("成功设置必需项: %s = %s", label, value), "purchase")
		return nil
	}
	for _, cfg := range configurations {
		if err := postConfig(cfg.label, cfg.value); err != nil {
			if ctx.Err() != nil {
				return nil, cancelledOutcome(state, item, "设置配置项")
			}
			errMsg := err.Error()
			state.Logger.Error(fmt.Sprintf("购买 %s 时发生 OVH API 错误(%s): %s", item.PlanCode, cfg.label, errMsg), "purchase")
			state.Logger.Error(fmt.Sprintf("错误发生时的购物车ID: %s", cartID), "purchase")
			state.Logger.Error(fmt.Sprintf("错误发生时的基础商品ID: %d", itemID), "purchase")
			fail(ovh.Explain(err))
			return nil, attemptOutcome(err)
		}
	}

	tl.mark("必需配置")

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
			if err := client.GetWithContext(ctx, fmt.Sprintf("/order/cart/%s/eco/options?%s", cartID, q.Encode()), &availableEcoOpts); err != nil {
				if ctx.Err() != nil {
					return nil, cancelledOutcome(state, item, "查硬件选项")
				}
				// 拉 eco/options 失败 → 中止订单。否则会用基础 plan 默认存储（多半是 HDD）下到错误配置
				errMsg := fmt.Sprintf("获取 Eco 硬件选项列表失败: %s（用户指定了 %d 个选项，无法验证，已取消下单避免下到错误配置）", ovh.Explain(err), len(filtered))
				state.Logger.Error(errMsg, "purchase")
				fail(errMsg)
				return nil, attemptOutcome(err)
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
				matchedOpt, matchedPC, tier := catalog.MatchEcoOption(availableEcoOpts, wanted)
				if item.Preorder {
					matchedOpt, matchedPC, tier = catalog.MatchPreorderEcoOption(availableEcoOpts, wanted)
				}
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
				if item.Preorder {
					state.Logger.Info(fmt.Sprintf("预订任务 %s 等待所选硬件配置开放: %v", item.ID, missing), "purchase")
					return nil, Outcome{WaitingForCatalog: true}
				}
				errMsg := fmt.Sprintf("用户请求的硬件选项 %v 未在 OVH 可用 Eco 选项中找到（已取消下单避免下到错误配置）", missing)
				state.Logger.Error(errMsg, "purchase")
				fail(errMsg)
				// 用户显式指定的选项对不上该子公司目录 = 确定性失败:eco/options 是按
				// (子公司, planCode) 算出来的固定集合,下一轮重试还是同一份,只会再刷一条失败 history。
				// 但"用户没指定、从 FQN 推来的"那批不算:下一轮有货的可能是另一条 FQN,
				// 推出来的段就不一样了,判死会把本来能抢到的任务掐掉。
				if len(item.Options) > 0 {
					return nil, Outcome{Fatal: true, Reason: errMsg}
				}
				return nil, Outcome{}
			}

			// 串行 POST 各 addon。每次 POST 服务端都要对同一 cart item 做 append +
			// 同 family 互斥(GenericOptionDefinition.exclusive)校验,schema 没有任何
			// 关于并发写同一 cart 的声明;通常只有 1-2 个 addon,串行代价不到 2 秒,
			// 不值得拿"整单取消"去赌 OVH 的并发宽容度。
			state.Logger.Info(fmt.Sprintf("依次添加 %d 个 Eco 选项: %v", len(todo), filtered), "purchase")
			for _, t := range todo {
				if err := client.PostWithContext(ctx, fmt.Sprintf("/order/cart/%s/eco/options", cartID), t.body, nil); err != nil {
					if ctx.Err() != nil {
						return nil, cancelledOutcome(state, item, "加硬件选项")
					}
					state.Logger.Error(fmt.Sprintf("添加 Eco 选项 %s 失败: %s", t.planCode, err.Error()), "purchase")
					// 关键选项添加失败 → 整单失败。不能静默继续 checkout,否则会下到错误配置。
					errMsg := fmt.Sprintf("添加 Eco 选项 %s 失败: %s（已取消下单避免下到错误配置）", t.planCode, ovh.Explain(err))
					fail(errMsg)
					return nil, attemptOutcome(err)
				}
				state.Logger.Info(fmt.Sprintf("成功添加 Eco 选项: %s", t.planCode), "purchase")
			}
			state.Logger.Info(fmt.Sprintf("共成功添加 %d 个硬件选项。", len(filtered)), "purchase")
		}
	} else {
		state.Logger.Info("⚠️ 用户未提供任何硬件选项，将使用默认配置下单", "purchase")
	}

	tl.mark("加硬件选项")
	couponWarning := ""
	if coupon := strings.TrimSpace(item.Coupon); coupon != "" {
		err := client.PostWithContext(ctx, "/order/cart/"+cartID+"/coupon", map[string]interface{}{"coupon": coupon}, nil)
		if ctx.Err() != nil {
			return nil, cancelledOutcome(state, item, "应用优惠码")
		}
		tl.mark("应用优惠码")
		if err != nil {
			var apiErr *ovhsdk.APIError
			if IsTransient(err) || !errors.As(err, &apiErr) || apiErr.Code < 400 || apiErr.Code >= 500 {
				recordTiming(timingKey, tl, "failed")
				fail("应用优惠码暂时失败: " + ovh.Explain(err))
				return nil, Outcome{}
			}
			// 明确被拒绝才回退;网络故障不能当作优惠码无效。
			couponWarning = "优惠码被 OVH 拒绝，本次未使用优惠码，价格不包含该码优惠。原因: " + ovh.Explain(err)
			state.Logger.Warn(couponWarning, "purchase")
		} else {
			state.Logger.Info("购物车优惠码已应用", "purchase")
		}
	}
	// URL、序列化正文和静态头在等待库存时全部确定,不会提前发送 checkout。
	call, err := ovh.PreparePost(client, "/order/cart/"+cartID+"/checkout", map[string]interface{}{
		"autoPayWithPreferredPaymentMethod": item.AutoPay,
	})
	if err != nil {
		return nil, attemptOutcome(err)
	}
	expires := time.Now().Add(5 * time.Minute)
	if raw, ok := cartResult["expire"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil && parsed.After(time.Now()) {
			expires = parsed.Add(-time.Minute)
			if cap := time.Now().Add(30 * time.Minute); expires.After(cap) {
				expires = cap
			}
		}
	}
	success = true // 准备成功后由 session 持有购物车,等库存期间不删除。
	return &preparedCart{client: client, call: call, id: cartID, subsidiary: subsidiary, couponWarning: couponWarning, expires: expires, preparedAt: time.Now()}, Outcome{}
}
