package purchase

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/notify"
	"github.com/ovh-buy/server/internal/numconv"
	"github.com/ovh-buy/server/internal/orderterm"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
	"time"
)

// Outcome 一次抢购尝试的结果。
//
// 以前 PurchaseServer 只返回 bool,队列处理器分不出"这轮没货,下轮再来"和
// "这台机器这个账户永远买不到"。后者(跨区 planCode / 非 Eco 机型 / 账户被删)
// 会按 retryInterval 无限重试,而且每一轮都往 history 写一条失败记录 ——
// 一个手滑选错区的任务能刷满整个抢购历史。Fatal 就是用来终止这类任务的。
type Outcome struct {
	Success           bool
	WaitingForStock   bool
	WaitingForCatalog bool
	WaitingForQuota   bool
	GroupStopped      string
	RetryAfter        time.Duration
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
	// Cancelled:任务在下单途中被用户删除,本轮主动终止。
	// 不是失败:不写 history、不计 FailureCount、不算 Attempted。
	// 结账之前的任何一步都可能落到这里;结账一旦发出就不再接受取消(见 checkout 处注释)。
	Cancelled bool
	// PauseTask:结账登记或响应无法确认,暂停任务等人工核对。
	// 不计 FailureCount(不是确定性失败);不发 Fatal(用户确认无单后可恢复)
	PauseTask        bool
	RetryImmediately bool
}

// cancelledOutcome 下单途中任务被删除时的返回。
// 只记日志:history 里不留痕(用户自己删的,不是失败),耗时统计也不记(不是一次完整尝试)。
func cancelledOutcome(state *app.State, item *types.QueueItem, stage string) Outcome {
	state.Logger.Info(fmt.Sprintf("任务 %s (%s @ %s) 在「%s」阶段被取消,本轮终止",
		item.ID, item.PlanCode, item.Datacenter, stage), "purchase")
	return Outcome{Cancelled: true}
}

// 多账户:用 item.AccountID 取对应 OVH client 和 subsidiary。
//
// ctx 由队列处理器给,用户删任务时会被取消(app.State.MarkTaskDeleted)。
// 结账之前的每一次 OVH 调用都挂在它上面,取消即中断;结账本身例外,见那里的注释。
func PurchaseServer(ctx context.Context, state *app.State, item *types.QueueItem) Outcome {
	selectedDuration, termErr := orderterm.Normalize(item.Duration)
	if termErr != nil {
		return Outcome{Fatal: true, Reason: termErr.Error()}
	}
	if state.DB == nil || item.ID == "" {
		return Outcome{Fatal: true, Reason: "数据库或任务 ID 不可用,未发起下单"}
	}
	session, managed := ctx.Value(cartSessionKey{}).(*cartSession)
	if !managed {
		session = &cartSession{}
		defer session.discard()
	}
	// 结账登记只由本任务 worker 写入或释放;同一会话确认过一次不存在后,热路径不再重复读库。
	// 登记本身(ClaimQueueOrder)仍在结账前原子写入。
	if !session.checkoutClear {
		if found, err := state.DB.GetKV(db.QueueCheckoutKey(item.ID), &map[string]interface{}{}); err != nil {
			return Outcome{PauseTask: true, Reason: "无法核对结账登记,未发起下单: " + err.Error()}
		} else if found {
			_ = state.DB.MarkQueueOrder(*item, "uncertain")
			return Outcome{PauseTask: true, Reason: "此任务已有结账登记,请先核对 OVH 订单后再决定是否恢复任务"}
		}
		session.checkoutClear = managed
	}
	if item.OrderGroup != "" {
		quota, err := state.DB.QueueQuota(*item)
		if err != nil {
			return Outcome{PauseTask: true, Reason: "无法核对总购买量: " + err.Error()}
		}
		if quota.Uncertain > 0 {
			return Outcome{GroupStopped: "groupUncertain"}
		}
		if quota.Ordered >= item.GroupLimit {
			return Outcome{GroupStopped: "groupComplete"}
		}
		if quota.Reserved >= item.GroupLimit {
			return Outcome{WaitingForQuota: true}
		}
	}
	client, err := session.clientFor(state, item.AccountID)
	if err != nil {
		// 账户不存在 / 凭据缺失 / endpoint 非法 —— 都是重试一万次也不会变的错。
		// 以前这里连 history 都不写,任务就在后台每 retryInterval 秒静默失败一次。
		errMsg := fmt.Sprintf("账户 %s 不可用: %s（重试无用，请检查账户是否还在、API 密钥是否填全）",
			item.AccountID, err.Error())
		state.Logger.Error("PurchaseServer: 取 OVH client 失败 ("+item.AccountID+"): "+err.Error(), "purchase")
		recordFailure(state, item, errMsg)
		return Outcome{Fatal: true, Reason: errMsg}
	}
	item.AccountID = session.accountID
	if item.Preorder {
		if out, wait := preorderSaleBackoff(ctx, session); wait {
			return out
		}
	}
	tl := newTimeline()
	tl.dispatchKey = item.AccountID + "\x00" + TimingKey(item.PlanCode, ovh.ConvertDisplayDCToAPIDC(item.Datacenter))
	intent := preparationKey(*item, session.account)
	if session.cart != nil && (session.cart.intent != intent || !time.Now().Before(session.cart.expires)) {
		session.cart.cleanupInBackground()
		session.cart = nil
		session.nextPrepare = time.Time{}
	}
	if session.cart == nil {
		if pool, ok := state.OrderPrewarm.(*CartPool); ok {
			if prepared := pool.Take(*item); prepared != nil {
				session.cart, session.client, client = prepared, prepared.client, prepared.client
			}
		}
	}

	preorderStock, preorderOrder := item.Options, item.Options
	if item.Preorder {
		var out Outcome
		var wait bool
		if preorderStock, preorderOrder, out, wait = preorderHardware(state, session, item); wait {
			return out
		}
	}
	// 每一轮都打点。抢购的胜负常常只差几百毫秒,而"差在哪"没有数字就只能靠猜。
	timingKey := item.ID
	if timingKey == "" {
		timingKey = TimingKey(item.PlanCode, item.Datacenter)
	}

	// 检查可用性
	var availabilities []map[string]interface{}
	detectedAt := time.Time{}
	pinnedFQN := ""
	if item.Preorder {
		pinnedFQN, detectedAt = preorderPinnedFQN(ctx, session, item, client, intent, preorderStock)
	}
	if pinnedFQN != "" {
		// This is the saved preparation, not a claim of fresh available inventory. Checkout still requires all live quotes.
		availabilities = []map[string]interface{}{{"fqn": pinnedFQN, "datacenters": []interface{}{map[string]interface{}{"datacenter": ovh.ConvertDisplayDCToAPIDC(item.Datacenter)}}}}
	} else if signal, ok := ctx.Value(stockSignalKey{}).(app.StockSignal); ok && signal.AccountID == session.accountID && signal.PlanCode == item.PlanCode && time.Since(signal.DetectedAt) < time.Second {
		availabilities, detectedAt = signal.Variants, signal.DetectedAt
	} else {
		observation, err := state.StockSignals.Fetch(ctx, session.accountID, item.PlanCode, func() ([]map[string]interface{}, error) {
			return ovh.GetAvailabilities(ctx, client, item.PlanCode)
		})
		if err != nil {
			if ctx.Err() != nil {
				return cancelledOutcome(state, item, "查库存")
			}
			tl.mark("查库存")
			recordTiming(timingKey, tl, "failed")
			errMsg := err.Error()
			state.Logger.Error(fmt.Sprintf("购买 %s 时发生 OVH API 错误: %s", item.PlanCode, errMsg), "purchase")
			recordFailure(state, item, ovh.Explain(err))
			return attemptOutcome(err)
		}
		availabilities, detectedAt = observation.Variants, observation.DetectedAt
	}

	if pinnedFQN != "" {
		tl.mark("复用已核验配置")
	} else {
		tl.mark("查库存")
	}

	// 空数组 ≠ 暂时没货。OVH 三个站点的机型目录彼此独立,拿别的站点的 planCode 查
	// /dedicated/server/datacenter/availabilities 同样是 HTTP 200 + [](实测:
	// 24rise01-v1 在 US 站点 n=0、24rise01-v1-us 在 EU/CA 站点 n=0),
	// 和"这台机器暂时没货"长得一模一样。不区分的话跨区任务会按重试间隔永远刷下去,
	// 日志里只有一句"当前无货",用户永远看不出是自己选错了区的机型。
	// classifyPlan 给出确定性结论时直接判 Fatal:这类任务重试到天荒地老也不会变。
	if len(availabilities) == 0 {
		if verdict, msg := catalog.ClassifyPlan(state, item.AccountID, item.PlanCode, "purchase"); msg != "" {
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
	if item.Preorder {
		// 预订保存的是原始 FQN 硬件段;参数消失也不能忽略它后改买其他配置。
		wantedHW = preorderStock
	}
	candidates := availabilities
	if item.Preorder {
		candidates = preorderCandidates(availabilities, item.PlanCode)
	}
	if len(wantedHW) > 0 {
		matchedAvs := []map[string]interface{}{}
		for _, av := range candidates {
			fqn, _ := av["fqn"].(string)
			matches := fqnCoversOptions(fqn, wantedHW)
			if item.Preorder {
				matches = preorderFQNCoversOptions(fqn, wantedHW) && strings.EqualFold(strings.SplitN(fqn, ".", 2)[0], item.PlanCode)
			}
			if matches {
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
			recordTiming(timingKey, tl, "unavailable")
			return Outcome{WaitingForStock: true}
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
		for _, av := range candidates {
			dcs, _ := av["datacenters"].([]interface{})
			for _, raw := range dcs {
				dc, _ := raw.(map[string]interface{})
				if dc["datacenter"] == apiDC {
					availableFQN, _ = av["fqn"].(string)
					break
				}
			}
			if availableFQN != "" {
				break
			}
		}
		// 没有该配置/机房时不能猜配置造车;目录未上架仍按原逻辑等待。
		if availableFQN == "" {
			recordTiming(timingKey, tl, "unavailable")
			return Outcome{WaitingForStock: true}
		}
	}

	// 决定本次下单使用的硬件 options：
	// - 用户显式指定了 options → 直接用（fail-fast 由后面的 eco/options 处理）
	// - 用户没指定 → 从可用 FQN 推断 addon planCode，确保订单走"实际有货的那套配置"
	effectiveOptions := configurationOptions(availableFQN, item.Options)
	if item.Preorder {
		effectiveOptions = preorderConfigurationOptions(availableFQN, preorderStock, preorderOrder)
	}

	ready := session.cart != nil && session.cart.fqn == availableFQN && session.cart.duration != "" && orderterm.Same(session.cart.duration, selectedDuration) && time.Now().Before(session.cart.expires)
	prewarmed := ready && session.cart.preparedAt.Before(detectedAt)
	if !ready {
		if !foundAvailable && session.partial == nil && time.Now().Before(session.nextPrepare) {
			if _, ok := ctx.Value(saleSignalKey{}).(saleSnapshot); !ok {
				return Outcome{WaitingForStock: true}
			}
		}
		session.nextPrepare = time.Now().Add(30 * time.Second)
		if item.Preorder {
			session.nextPrepare = time.Now().Add(catalog.PreorderCheckInterval)
		}
		if session.cart != nil {
			session.cart.cleanup()
			session.cart = nil
		}
		prepared, outcome := prepareCart(ctx, state, item, session.account, client, effectiveOptions, apiDC, availableFQN, tl, timingKey, !foundAvailable)
		if prepared == nil {
			if outcome.WaitingForCatalog {
				return outcome
			}
			if !foundAvailable && !outcome.Cancelled {
				return Outcome{WaitingForStock: true}
			}
			return outcome
		}
		prepared.stockOptions = append([]string(nil), preorderStock...)
		prepared.orderOptions = append([]string(nil), preorderOrder...)
		session.cart = prepared
	}
	cart := session.cart
	subsidiary := cart.subsidiary
	couponWarning := cart.couponWarning
	if item.Preorder {
		at, fire, out := preorderConfirmSale(ctx, state, session, cart, item, foundAvailable)
		if !fire {
			return out
		}
		if !item.PreorderWaitStock {
			// 开售即下:报价开放即为触发时刻。有货再下保留库存检测时间。
			detectedAt = at
		}
		foundAvailable = true
		prewarmed = cart.preparedAt.Before(detectedAt)
	}
	if !foundAvailable {
		recordTiming(timingKey, tl, "unavailable")
		return Outcome{WaitingForStock: true}
	}
	// 结账前最后一次复核。这是整条链路里唯一真正花钱的一步,也是取消能起作用的
	// 最后一个点 —— 从这里往下,结账请求一旦发出就**不再接受取消**:
	// 请求已经到了 OVH、我们这边却中途断开,订单可能已经生成而我们不知道,
	// 既没 history 也没通知,那比"多下了一单"更糟。
	if ctx.Err() != nil || state.IsTaskDeleted(item.ID) {
		return cancelledOutcome(state, item, "结账前复核")
	}
	if !time.Now().Before(cart.expires) {
		cart.cleanupInBackground()
		session.cart = nil
		session.nextPrepare = time.Time{}
		return Outcome{RetryImmediately: true}
	}
	currentAccount, exists := state.FindAccount(cart.account.ID)
	if !exists || !ovh.SameClientConfiguration(currentAccount, cart.account) || currentAccount.Zone != cart.account.Zone {
		return cancelledOutcome(state, item, "账户配置已变化")
	}
	session.checkoutClear = false
	claimed, err := state.DB.ClaimQueueOrder(*item)
	if err == nil && !claimed && item.OrderGroup != "" {
		return Outcome{WaitingForQuota: true}
	}
	if err != nil || !claimed {
		return Outcome{PauseTask: true, Reason: "结账登记失败或任务已有结账请求,请先核对 OVH 订单"}
	}
	setOrderPhase(state, item.ID, "submitting")
	if ctx.Err() != nil || state.IsTaskDeleted(item.ID) {
		_ = state.DB.ReleaseQueueOrder(*item)
		return cancelledOutcome(state, item, "结账登记后复核")
	}
	var checkoutResult map[string]interface{}
	trace := &ovh.DispatchTrace{DetectedAt: detectedAt}
	// 结账一旦发出就等待结果,不取消或重试结果不确定的请求。
	session.cart = nil

	checkoutErr := cart.call.Send(context.WithoutCancel(ctx), trace, &checkoutResult)
	orderID := numconv.ToString(checkoutResult["orderId"])
	if checkoutErr == nil {
		if id, err := strconv.ParseInt(orderID, 10, 64); err != nil || id <= 0 {
			checkoutErr = fmt.Errorf("结账响应缺少有效订单号,请到 OVH 控制台核对订单")
		}
	}
	dispatch := trace.Snapshot(prewarmed)
	tl.dispatch = &dispatch
	state.Logger.Info(fmt.Sprintf("[派发] task=%s account=%s %s@%s detection_to_write=%dus wrote=%t reused=%t prewarmed=%t", item.ID, item.AccountID, item.PlanCode, item.Datacenter, dispatch.DetectionToWriteUS, dispatch.WroteRequest, dispatch.ConnectionReused, dispatch.Prewarmed), "purchase")
	if err := checkoutErr; err != nil {
		retryPrepared := ready && expiredCartError(err)
		if !checkoutUncertain(err) {
			if retryPrepared {
				cart.cleanupInBackground()
			} else {
				cart.cleanup()
			}
			if releaseErr := state.DB.ReleaseQueueOrder(*item); releaseErr != nil {
				recordFailureStatus(state, item, "结账被拒绝但登记未能清除,请人工核对: "+releaseErr.Error(), "uncertain")
				return Outcome{PauseTask: true}
			}
		}
		// POST /item/{id}/configuration 对取值不做任何校验 —— 实测在 EU 车上把
		// dedicated_datacenter 设成 "hil"、region 设成 "usa" 都会 200 返回配置项 id,
		// 直到 summary/checkout 才报 "<fqn> is not available in hil"。
		// 也就是说"配置全设成功"根本不代表值合法,错的机房/区域只能在这里现原形,
		// 必须把它翻成用户看得懂的话,否则历史里只有一句英文 OVH 报错。
		// 用 Explain 而不是 err.Error():这条是用户在「购买历史」里唯一能看到的说明。
		// Explain 把原文原样嵌在后面,所以下面那句 Contains 判断照样成立。
		errMsg := ovh.Explain(err)
		if strings.Contains(errMsg, "is not available in") {
			errMsg = fmt.Sprintf("%s（机房 %s 不在子公司 %s 的 %s 目录里；OVH 三站目录独立，同一机型在不同区可选的机房不同）",
				errMsg, apiDC, subsidiary, item.PlanCode)
		}
		tl.mark("下单")
		recordTiming(timingKey, tl, "failed")
		state.Logger.Error(fmt.Sprintf("购买 %s 时发生 OVH API 错误: %s (%s)", item.PlanCode, errMsg, tl.String()), "purchase")
		if couponWarning != "" {
			errMsg += "；" + couponWarning
		}
		// /me/order 返回 ID 列表,也不支持按 planCode 筛选,不能据此猜测本次订单。
		if checkoutUncertain(err) {
			_ = state.DB.MarkQueueOrder(*item, "uncertain")
			recordFailureStatus(state, item, errMsg, "uncertain")
			state.Logger.Warn("checkout 结果未能确认,暂停任务等待人工确认(防重复下单)", "purchase")
			notify.Broadcast(state, fmt.Sprintf(
				"⚠️ 抢购 %s@%s 结账结果不明确,无法确认订单是否已创建\n\n"+
					"任务已暂停。请到 OVH 订单页查看是否已有一笔未付订单:\n"+
					"- 有:等付款即可,任务不需要恢复\n"+
					"- 没有:点下面的按钮或在队列页恢复运行", item.PlanCode, item.Datacenter),
				map[string]interface{}{"inline_keyboard": [][]map[string]interface{}{{
					telegram.TaskButton("确认 OVH 没有这笔订单，恢复任务", item.ID, telegram.TaskResume),
				}}})
			return Outcome{Attempted: false, PauseTask: true}
		}
		if item.Preorder && listingNotReady(err) && !retryPrepared {
			state.Logger.Info("预订下单接口暂未接受所选配置，继续等待: "+errMsg, "purchase")
			return Outcome{WaitingForCatalog: true, Reason: errMsg}
		}
		recordFailure(state, item, errMsg)
		// checkout 这一步最要紧:补货瞬间大家都在下单,429 是常态。
		// 把它记成一次"真正的失败尝试"会让任务在唯一有货的那一分钟里自己判死。
		outcome := attemptOutcome(err)
		outcome.RetryImmediately = retryPrepared
		if outcome.RetryImmediately {
			outcome.Attempted = false
		}
		return outcome
	}
	if err := state.DB.MarkQueueOrder(*item, "ordered"); err != nil {
		state.Logger.Error("订单已创建，但总量登记未更新: "+err.Error(), "purchase")
	}
	tl.mark("下单")
	recordTiming(timingKey, tl, "ordered")

	orderURL, _ := checkoutResult["url"].(string)

	// checkout 已返回订单 ID,cart 已成功转 order,标记成功阻止 defer 删除
	// 立刻记成功 —— 价格和过期时间空着,后台异步补
	orderedItem := *item
	if item.Preorder {
		orderedItem.Options = append([]string(nil), cart.options...)
	}
	recordSuccess(state, &orderedItem, orderID, orderURL, "", nil, couponWarning)
	recordTimingToHistory(state, item.ID, tl)
	state.Logger.Info("[耗时] "+item.PlanCode+"@"+item.Datacenter+" 抢购成功 "+tl.String(), "purchase")

	// 异步补:从 /me/order/{orderID} 读 expirationDate + 价格,写回 history
	if orderID != "" {
		go backfillOrderDetail(state, client, item.ID, orderID)
	}

	state.Logger.Info(fmt.Sprintf("成功购买 %s 在 %s (订单ID: %s)",
		item.PlanCode, item.Datacenter, orderID), "purchase")

	// 成功通知走统一通知层(Telegram + Webhook 至少一条可达就发)。
	// 以前被 TG 配置判断包住:Webhook-only 用户收不到"抢到了但没付款",
	// 订单逾期作废 —— 这是整条链路里最不能丢的一条消息(issue #2)。
	acc, _ := state.FindAccount(item.AccountID)
	message := BuildOrderSuccessMessage(&orderedItem, orderID, ovh.ManagerOrderURL(acc.Endpoint, orderID))
	if couponWarning != "" {
		message += "\n\n⚠️ " + couponWarning
	}
	if notify.Broadcast(state, message, nil) > 0 {
		state.Logger.Info("已为订单 "+orderID+" 发送订单成功通知。", "purchase")
	} else {
		state.Logger.Warn("订单 "+orderID+" 已创建,但成功通知未送达任何通道。", "purchase")
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

// regionAllowedValues 从 requiredConfiguration 响应里取 region 的合法取值 + 是否必填。
//
// schema 里 order.cart.ConfigurationRequirements 只声明了 label/required/type,但三区实测
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

// Preserve explicit addons and fill unspecified hardware from the selected stock variant.
func configurationOptions(fqn string, explicit []string) []string {
	options := append([]string(nil), explicit...)
	parts := strings.Split(fqn, ".")
	for _, segment := range parts[1:] {
		covered := false
		for _, option := range explicit {
			if fqnSegmentMatchesOption(segment, option) {
				covered = true
				break
			}
		}
		if !covered && segment != "" {
			options = append(options, segment)
		}
	}
	return options
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

func preorderFQNCoversOptions(fqn string, opts []string) bool {
	parts := strings.Split(fqn, ".")
	if len(parts) < 2 {
		return false
	}
	for _, wanted := range opts {
		found := false
		for _, part := range parts[1:] {
			if strings.EqualFold(part, wanted) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func preorderConfigurationOptions(fqn string, stock, order []string) []string {
	options := append([]string(nil), order...)
	parts := strings.Split(fqn, ".")
	for _, part := range parts[1:] {
		covered := false
		for _, wanted := range stock {
			if strings.EqualFold(part, wanted) {
				covered = true
				break
			}
		}
		if !covered {
			options = append(options, part)
		}
	}
	return catalog.OrderOptions(options)
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

func recordSuccess(state *app.State, item *types.QueueItem, orderID, orderURL, expirationTime string, priceInfo *types.PriceInfo, couponWarning string) {
	// 锁内只改内存,SaveHistory 在锁外 —— 它自己会拿 HistoryMu(快照),
	// Go mutex 不可重入,持锁同步调它(c419176 引入)首次下单成功即死锁全队列
	state.HistoryMu.Lock()
	defer func() {
		state.HistoryMu.Unlock()
		_ = state.SaveHistory()
	}()
	now := types.NowISO()

	for i := range state.History {
		if state.History[i].TaskID == item.ID {
			state.History[i].Status = "success"
			state.History[i].AccountID = item.AccountID
			state.History[i].Duration = item.Duration
			state.History[i].OrderID = orderID
			state.History[i].OrderURL = orderURL
			state.History[i].ErrorMessage = nil
			state.History[i].CouponWarning = couponWarning
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
			return
		}
	}

	entry := types.PurchaseHistoryEntry{
		ID:            uuid.NewString(),
		TaskID:        item.ID,
		AccountID:     item.AccountID,
		Duration:      item.Duration,
		PlanCode:      item.PlanCode,
		Datacenter:    item.Datacenter,
		Options:       item.Options,
		Status:        "success",
		CouponWarning: couponWarning,
		OrderID:       orderID,
		OrderURL:      orderURL,
		PurchaseTime:  now,
		AttemptCount:  item.RetryCount,
	}
	if expirationTime != "" {
		entry.ExpirationTime = expirationTime
	}
	if priceInfo != nil {
		entry.Price = priceInfo
	}
	state.History = append(state.History, entry)
	state.Logger.Info("创建抢购历史(成功) 任务ID: "+item.ID, "purchase")
}

func recordFailure(state *app.State, item *types.QueueItem, errMsg string) {
	recordFailureStatus(state, item, errMsg, "failed")
}

func recordFailureStatus(state *app.State, item *types.QueueItem, errMsg, status string) {
	state.HistoryMu.Lock()
	defer func() {
		state.HistoryMu.Unlock()
		_ = state.SaveHistory()
	}()
	now := types.NowISO()

	for i := range state.History {
		if state.History[i].TaskID == item.ID {
			if state.History[i].Status == "success" && state.History[i].OrderID != "" {
				return
			}
			state.History[i].Status = status
			state.History[i].AccountID = item.AccountID
			state.History[i].Duration = item.Duration
			state.History[i].OrderID = ""
			state.History[i].OrderURL = ""
			em := errMsg
			state.History[i].ErrorMessage = &em
			state.History[i].PurchaseTime = now
			state.History[i].AttemptCount = item.RetryCount
			state.History[i].Options = item.Options
			state.Logger.Info("更新抢购历史(失败) 任务ID: "+item.ID, "purchase")
			return
		}
	}
	em := errMsg
	entry := types.PurchaseHistoryEntry{
		ID:           uuid.NewString(),
		TaskID:       item.ID,
		AccountID:    item.AccountID,
		Duration:     item.Duration,
		PlanCode:     item.PlanCode,
		Datacenter:   item.Datacenter,
		Options:      item.Options,
		Status:       status,
		ErrorMessage: &em,
		PurchaseTime: now,
		AttemptCount: item.RetryCount,
	}
	state.History = append(state.History, entry)
	state.Logger.Info("创建抢购历史(失败) 任务ID: "+item.ID, "purchase")
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
	// 指的是前者，拿 retractionDate 当付款截止时间会让用户误判付款窗口。
	//
	// 注意 retractionDate 现在是有意义的:checkout 不再传 waiveRetractationPeriod,
	// 撤回权没被放弃,这个日期就是"在此之前还能申请无理由撤单"的真实截止时间。
	// 而这个 fix 之前下的单都弃权了,OVH 对它们不返回 retractionDate ——
	// 前端据此判断要不要显示撤回入口,不用自己算 14 天(算了就会给老订单
	// 显示一个点了必然失败的按钮)。
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

	// 支付状态也顺手读一次(之后由 OrderStatusLoop 定时刷)。
	// 拿到的多半是 notPaid —— 但"明确的待付款"和"什么都不知道"对用户是两回事
	orderStatus := ""
	if st, err := FetchOrderStatus(client, orderID); err != nil {
		state.Logger.Warn(fmt.Sprintf("查询订单 %s 状态失败: %s", orderID, err.Error()), "purchase")
	} else {
		orderStatus = st
	}

	if expirationTime == "" && priceInfo == nil && orderStatus == "" {
		return
	}

	state.HistoryMu.Lock()
	// defer 先 Unlock 再 Save:SaveHistory 内部拿 HistoryMu(快照),
	// 持锁调它 = 同 goroutine 不可重入死锁(跟 recordSuccess 同型 bug)
	defer func() {
		state.HistoryMu.Unlock()
		_ = state.SaveHistory()
	}()
	for i := range state.History {
		if state.History[i].TaskID != taskID {
			continue
		}
		changed := false
		if orderStatus != "" {
			state.History[i].OrderStatus = orderStatus
			state.History[i].OrderStatusAt = types.NowISO()
			changed = true
		}
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
		}
		return
	}
}

// BuildOrderSuccessMessage 抢购成功通知的正文。
//
// 抽出来是因为这是整个系统里唯一一条"你花钱了"的消息 ——
// 措辞错一个字用户就可能少付一笔钱或者多买一台。要能单独看、单独测。
//
// 关键在于"成功"两个字的真实语义:checkout 返回订单号就算成功,
// 但订单是**未付款**的,逾期会自动作废。不把这句写出来,
// 用户看到 🎉 就睡了,第二天机器没了还以为是我们没抢到。
func BuildOrderSuccessMessage(item *types.QueueItem, orderID, managerURL string) string {
	payNote := "⚠️ 订单尚未付款：请尽快打开订单链接完成付款,逾期未付订单会自动作废。\n" +
		"(撤回资格与退款金额以 OVH 订单页面显示为准)\n"
	if item.AutoPay {
		// 只承诺我们真正知道的:已请求自动付款 ≠ 扣款一定成功
		// (默认支付方式失效/余额不足时 OVH 不会扣成),让用户去核对
		payNote = "💳 已请求用账户默认支付方式自动付款,请打开订单链接核对扣款是否成功。\n" +
			"(申请撤回的资格和退款金额以 OVH 订单页为准)\n"
	}
	// 发控制面板深链,不发 checkout 返回的那个 url ——
	// 后者是带凭证的下载链接(OVH 的 billing.Order 里 url 旁边就是 password),
	// 而这条消息会进 Telegram 群和用户配的任意 webhook。
	// 带凭证那份仍然存在本地历史里,界面上照样一键可付。
	linkURL := managerURL
	if linkURL == "" {
		linkURL = "请在 OVH 控制面板 → 账单 → 订单 中查看"
	}
	msg := fmt.Sprintf("🎉 OVH 服务器下单成功！\n\n服务器型号 (Plan Code): %s\n数据中心: %s\n订单 ID: %s\n订单链接: %s\n\n%s",
		item.PlanCode, item.Datacenter, orderID, linkURL, payNote)
	if len(item.Options) > 0 {
		msg += "自定义配置: " + strings.Join(item.Options, ", ") + "\n"
	}
	msg += "\n抢购任务ID: " + item.ID
	return msg
}
