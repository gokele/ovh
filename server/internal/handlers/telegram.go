package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// updateCtx 处理一条 Telegram update 的上下文。
//
// 为什么不直接用 *gin.Context:update 有两条来路 ——
// webhook(Telegram 推给我们)和 long polling(我们去 getUpdates 拉)。
// 后者压根没有 HTTP 请求上下文,而授权、幂等、限流、下单这一整套逻辑
// 两边必须**完全一样**,不能各写一份(写两份就意味着哪天只在一边修了 bug)。
// 所以把 gin 从处理链路里摘掉,只留下"状态码 + 响应体"这个抽象:
// webhook 把它写回 HTTP 响应,poller 只拿它记日志。
type updateCtx struct {
	status int
	body   gin.H
}

func (u *updateCtx) JSON(status int, body gin.H) {
	u.status, u.body = status, body
}

// ProcessUpdate 处理一条从 Telegram 拉回来的 update。
func ProcessUpdate(state *app.State, mon *monitor.Monitor, data map[string]interface{}) *updateCtx {
	u := &updateCtx{status: http.StatusOK, body: gin.H{"ok": true}}

	// 1) 发送者授权 —— 必须排在幂等写入之前。
	//
	// 以前顺序是「幂等 → 授权」,于是未授权请求也会先往 telegram_updates 写一行。
	// 兼容模式下(见下面 legacy 的说明)任何人都能走到这一步,于是可以:
	//   · 无限插行撑爆磁盘(update_id 是任意 int64,清理只在 %50==0 时抽样触发)
	//   · **投毒**:预占一段连续的 update_id,之后 Telegram 真发来的同号 update
	//     被判成"重复投递"直接丢弃 —— 一键下单和文本下单静默失效,
	//     日志里只有一行「忽略重复投递」。对抢购工具来说这是最坏的失败模式。
	if !telegram.IsAuthorizedActor(state, actorChatID(data), actorUserID(data)) {
		state.Logger.Warn("拒绝未授权的 Telegram 请求(未写入幂等表)", "telegram")
		u.JSON(http.StatusForbidden, gin.H{"ok": false, "error": "unauthorized_actor"})
		return u
	}

	// 2) update_id 幂等。
	//
	// offset 是在**下一次** getUpdates 时才确认的 —— 处理完还没来得及推进 offset
	// 就崩了、或者被自更新重启了,这条 update 会重发一遍。
	// 没有这一步,一次版本升级就能重复下单。
	if updateID := parseUpdateID(data["update_id"]); updateID > 0 && state.DB != nil {
		claimed, err := state.DB.TryClaimTelegramUpdate(updateID)
		if err != nil {
			state.Logger.Warn("update_id 幂等写入失败: "+err.Error(), "telegram")
		} else if !claimed {
			state.Logger.Info(fmt.Sprintf("忽略重复投递的 update_id=%d", updateID), "telegram")
			u.JSON(http.StatusOK, gin.H{"ok": true, "duplicate": true})
			return u
		}
		// 顺带清理超过保留期的旧记录（抽样触发，避免每条都删一次）
		if updateID%50 == 0 {
			before := float64(time.Now().Add(-time.Duration(telegram.UpdateIDRetentionDays) * 24 * time.Hour).Unix())
			if n, err := state.DB.CleanupTelegramUpdates(before); err == nil && n > 0 {
				state.Logger.Debug(fmt.Sprintf("已清理 %d 条过期 update_id", n), "telegram")
			}
		}
	}

	// 处理 callback_query（一键下单按钮）
	if cb, ok := data["callback_query"].(map[string]interface{}); ok {
		handleTelegramCallback(state, mon, u, cb)
		return u
	}

	// 处理普通消息（文本下单）
	if msg, ok := data["message"].(map[string]interface{}); ok {
		handleTelegramMessage(state, mon, u, msg)
		return u
	}
	return u
}

// actorChatID / actorUserID 从 update 里取出发送者标识,
// callback_query 和 message 两种形态各取各的位置。
// 提前取是为了把授权判断挪到幂等写入之前(见 webhook handler 里的说明)。
func actorChatID(data map[string]interface{}) interface{} {
	if cb, ok := data["callback_query"].(map[string]interface{}); ok {
		msg, _ := cb["message"].(map[string]interface{})
		return getNested(msg, "chat", "id")
	}
	if msg, ok := data["message"].(map[string]interface{}); ok {
		return getNested(msg, "chat", "id")
	}
	return nil
}

func actorUserID(data map[string]interface{}) interface{} {
	if cb, ok := data["callback_query"].(map[string]interface{}); ok {
		from, _ := cb["from"].(map[string]interface{})
		return from["id"]
	}
	if msg, ok := data["message"].(map[string]interface{}); ok {
		from, _ := msg["from"].(map[string]interface{})
		return from["id"]
	}
	return nil
}

// handleTelegramCallback 处理「一键下单」按钮回调。
func handleTelegramCallback(state *app.State, mon *monitor.Monitor, u *updateCtx, cb map[string]interface{}) {
	cbData, _ := cb["data"].(string)
	message, _ := cb["message"].(map[string]interface{})
	chatID := getNested(message, "chat", "id")
	messageID, _ := getNumOrFloat(message["message_id"])
	// 记下这条消息当前的键盘,下单成功后把按过的那颗改成「已下单」。
	// 回调里 Telegram 会把原 reply_markup 一起带回来,不用我们自己存。
	telegram.StashKeyboard(chatID, int64(messageID), extractKeyboard(message))
	fromUser, _ := cb["from"].(map[string]interface{})
	userID, _ := getNumOrFloat(fromUser["id"])
	state.Logger.Info(fmt.Sprintf("收到Telegram回调: user_id=%v, callback_data=%s...", userID, truncate(cbData, 50)), "telegram")

	// 4) 发送者授权：只有配置的那个 chat 能下单
	if !telegram.IsAuthorizedActor(state, chatID, fromUser["id"]) {
		state.Logger.Warn(fmt.Sprintf("拒绝未授权的 Telegram 回调: chat_id=%v, user_id=%v", chatID, userID), "telegram")
		telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "无权限", true)
		u.JSON(http.StatusForbidden, gin.H{"ok": false, "error": "unauthorized_actor"})
		return
	}

	// 5) 频率限制
	rateKey := telegram.ChatIDString(chatID)
	if rateKey == "" {
		rateKey = telegram.ChatIDString(fromUser["id"])
	}
	if !telegram.AllowRate(rateKey) {
		telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "操作过于频繁，请稍后再试", true)
		u.JSON(http.StatusTooManyRequests, gin.H{"ok": false, "error": "rate_limited"})
		return
	}

	callbackObj, ok := decodeCallbackData(state, cbData)
	if !ok {
		u.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "Invalid callback data format"})
		return
	}

	action := strOr(callbackObj, "a", "action")
	// /watch 的分步选择。它不花钱(只是建订阅),走自己的分支,
	// 不经过下面那套一次性按钮的 claim 逻辑 —— 那套是给"按一次就下单"用的。
	if action == "wf" {
		if handleFlowCallback(state, mon, cb, callbackObj, chatID, int64(messageID)) {
			u.JSON(http.StatusOK, gin.H{"ok": true, "handled": "watch_flow"})
			return
		}
	}
	if action != "add_to_queue" {
		state.Logger.Warn("未知的action: "+action, "telegram")
		u.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "Unknown action: " + action})
		return
	}

	buttonID := strOr(callbackObj, "u", "uuid")
	// 没有按钮 id 就不许下单。
	//
	// 以前 claim / 过期 / 重放三道判定全在 if buttonID != "" 里面 ——
	// callback_data 里不带 u,planCode/机房/配置就直接取 JSON 的 p/d/o,
	// telegram_order_buttons 一次都不查。于是 README 承诺的
	// 「同一个按钮只能下单一次、超 24h 作废」对任何能构造 body 的人都不成立:
	// 同一组 {"a":"add_to_queue","p":"...","d":"..."} 换个 update_id 就能重放,
	// 想下几单下几单。
	//
	// 正常 TG 客户端改不了 callback_data,所以这条要配合兼容模式或 secret 泄漏;
	// 但既然一次性 nonce 是我们唯一的防重放,就不该留一条绕过它的路。
	if buttonID == "" {
		state.Logger.Warn("拒绝没有按钮 id 的下单回调(无法防重放)", "telegram")
		telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "按钮已失效,请等下一条通知", true)
		u.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "missing_button_id"})
		return
	}
	planCode := strOr(callbackObj, "p", "planCode")
	dc := strOr(callbackObj, "d", "datacenter")
	// btnAccountID:发通知时记下的「触发订阅所用账户」。planCode 是分区的,
	// 用它下单才不会把欧区机型落到美区账户上。空 = 老按钮/无账户维度 → 退回默认账户。
	btnAccountID := ""
	var options []string
	if optsRaw, ok := callbackObj["o"]; ok {
		options = toStringSlice(optsRaw)
	} else if optsRaw, ok := callbackObj["options"]; ok {
		options = toStringSlice(optsRaw)
	}

	claimed := false // 是否占用了 DB 里的一次性按钮（失败要回滚）
	if buttonID != "" {
		row, ok, err := state.DB.ClaimTelegramButton(buttonID)
		if err != nil {
			state.Logger.Error("认领一键下单按钮失败: "+err.Error(), "telegram")
			u.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "claim_button_failed"})
			return
		}
		if ok {
			// 按钮过期（默认 24h）→ 归还并拒绝，避免用很久以前的库存信息下单
			if time.Since(time.Unix(int64(row.CreatedAt), 0)) > telegram.ButtonTTL {
				_ = state.DB.UnclaimTelegramButton(buttonID)
				state.Logger.Warn("一键下单按钮已过期: "+buttonID, "telegram")
				telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "该按钮已过期，请等待新的上架通知", true)
				u.JSON(http.StatusGone, gin.H{"ok": false, "error": "button_expired"})
				return
			}
			claimed = true
			planCode = row.PlanCode
			dc = row.Datacenter
			options = db.ParseTelegramButtonOptions(row.Options)
			btnAccountID = strings.TrimSpace(row.AccountID)
			state.Logger.Info(fmt.Sprintf("✅ 按钮已认领: id=%s, %s@%s, options=%v, account=%s",
				buttonID, planCode, dc, options, btnAccountID), "telegram")
		} else {
			// 认领失败：要么已经点过（重放），要么这条按钮根本不存在
			if _, exists, _ := state.DB.GetTelegramButton(buttonID); exists {
				state.Logger.Warn("一键下单按钮已被使用过，拒绝重复下单: "+buttonID, "telegram")
				telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "该按钮已使用过", true)
				u.JSON(http.StatusConflict, gin.H{"ok": false, "error": "button_already_used"})
				return
			}
			// 库里没有 → 退回内存缓存（升级前发出、只存在内存里的老按钮）
			if cached := mon.MessageUUIDCacheLookup(buttonID); cached != nil {
				planCode = cached.PlanCode
				dc = cached.Datacenter
				options = cached.Options
				state.Logger.Info("从内存缓存恢复按钮配置（旧按钮）: "+buttonID, "telegram")
			} else {
				// 库里没有、内存缓存也没有 → 拒绝。
				// 以前这里只打一行 Warn 就继续往下走,照样拿 JSON 里的 p/d 下单 ——
				// 按钮行被 DeleteExpiredTelegramButtons 清掉、换库、换机器都会落进
				// 这个分支,等于防重放形同虚设。
				state.Logger.Warn("按钮 UUID 不存在,拒绝下单: "+buttonID, "telegram")
				telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "按钮已失效,请等下一条通知", true)
				u.JSON(http.StatusGone, gin.H{"ok": false, "error": "button_not_found"})
				return
			}
		}
	}

	if planCode == "" || dc == "" {
		if claimed {
			_ = state.DB.UnclaimTelegramButton(buttonID)
		}
		u.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "Missing planCode or datacenter"})
		return
	}
	if len(options) == 0 {
		if cachedOpts := mon.OptionsCacheLookup(planCode + "|" + dc); len(cachedOpts) > 0 {
			options = cachedOpts
			state.Logger.Info("✅ 从缓存恢复 options: "+planCode+"|"+dc, "telegram")
		}
	}

	// 入队账户:优先用按钮里记下的那个(monitor 发通知时写入的触发订阅账户),
	// 它和查到这批库存的那个大区一致;按钮没记账户(老按钮/单账户)才退回默认账户。
	// 留空不入队 —— 会让下游 history / 账户 chip 全都对不上号。
	//
	// 一个账户都没有时不能"留空照样入队":这一单永远下不出去，
	// 用户却收到一句"已添加到抢购队列"，等于把失败藏到几十次重试之后。
	acc, hasAcc := state.FindAccount(btnAccountID)
	if !hasAcc && btnAccountID != "" {
		// 按钮里的账户已被删除 —— 不能直接失败(用户还有别的账户可下),
		// 但必须落一条 Warn:退回默认账户很可能就是跨区下错单的那一刻。
		state.Logger.Warn("按钮记录的账户已不存在，退回默认账户: "+btnAccountID, "telegram")
		btnAccountID = ""
		acc, hasAcc = state.FindAccount("")
	}
	if !hasAcc {
		if claimed {
			_ = state.DB.UnclaimTelegramButton(buttonID)
		}
		state.Logger.Warn("Telegram 一键下单被拒绝：系统里没有任何 OVH 账户", "telegram")
		telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "未配置 OVH 账户", true)
		telegram.SendReply(state, chatID, "❌ 未配置任何 OVH 账户，无法下单。请先在控制台添加账户。", int64(messageID))
		u.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "no_account"})
		return
	}
	accountID := acc.ID
	// planCode 是分区的：欧区 planCode 落到美区账户上，OVH 只会返回空库存而不报错。
	// 按钮带账户时这里就是发通知时那个订阅的账户；退回默认账户的情况仍可能选错，
	// 所以把"这一单会用哪个账户、哪个子公司/大区"明确写进日志和回复，
	// 让用户在真正扣款前就能看出账户选错了。
	accSub := strings.ToUpper(strings.TrimSpace(acc.Zone))
	if accSub == "" {
		accSub = ovh.DefaultSubsidiaryForEndpoint(acc.Endpoint)
	}
	accLabel := fmt.Sprintf("%s（子公司 %s / %s 区）", acc.Name, accSub, ovh.SubsidiaryRegion(accSub))
	if btnAccountID == "" {
		// 明示"这是兜底账户"而不是通知里那个订阅账户，用户才知道要核对
		accLabel += "［默认账户］"
	}
	item := types.QueueItem{
		ID:            uuid.NewString(),
		AccountID:     accountID,
		PlanCode:      planCode,
		Datacenter:    dc,
		Options:       options,
		Status:        "running",
		CreatedAt:     types.NowISO(),
		UpdatedAt:     types.NowISO(),
		RetryInterval: state.Config.RetryInterval(),
		RetryCount:    0,
		LastCheckTime: 0,
		FromTelegram:  true,
	}
	// 入队 + 落库统一走 EnqueueItems:失败会自动撤回内存里的那条,
	// 这里只需要把按钮归还、告诉用户
	if err := state.EnqueueItems([]types.QueueItem{item}, false); err != nil {
		if claimed {
			_ = state.DB.UnclaimTelegramButton(buttonID)
		}
		state.Logger.Error("一键下单落库失败,已撤回: "+err.Error(), "telegram")
		telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "没能保存，请重试", true)
		telegram.SendReply(state, chatID,
			"❌ 任务没能写进数据库，已撤回（避免出现重启就消失的假任务）：\n"+err.Error(), int64(messageID))
		u.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "save_failed"})
		return
	}
	optsStr := strings.Join(options, ", ")
	if optsStr == "" {
		optsStr = "无（默认配置）"
	}
	state.Logger.Info(fmt.Sprintf("Telegram用户 %v 通过按钮添加到队列: %s@%s, 配置选项: %s, 账户: %s",
		userID, planCode, dc, optsStr, accLabel), "telegram")
	confirmMsg := fmt.Sprintf(
		"✅ 已加入抢购队列\n\n型号: %s\n机房: %s\n配置: %s\n账户: %s\n\n"+
			"系统会一直重试到抢到为止。\n"+
			"查看进度 /queue · 取消 /cancel %s",
		planCode, strings.ToUpper(dc), optsStr, accLabel, shortID(item.ID))
	telegram.AnswerCallback(state, fmt.Sprintf("%v", cb["id"]), "已加入队列，开始抢了", false)
	telegram.SendReply(state, chatID, confirmMsg, int64(messageID))
	// 把按过的那颗按钮标掉,免得用户翻回这条通知又按一次
	// (服务端有一次性 claim 挡着,但用户只会收到一句"按钮已被使用",不知道是自己按过的)
	telegram.MarkButtonPressed(state, chatID, int64(messageID), cbData,
		"✅ "+strings.ToUpper(dc)+" 已下单")
	u.JSON(http.StatusOK, gin.H{"ok": true})
}

// handleTelegramMessage 处理文本下单消息。
func handleTelegramMessage(state *app.State, mon *monitor.Monitor, u *updateCtx, msg map[string]interface{}) {
	text, _ := msg["text"].(string)
	text = strings.TrimSpace(text)
	chatID := getNested(msg, "chat", "id")
	messageID, _ := getNumOrFloat(msg["message_id"])
	fromUser, _ := msg["from"].(map[string]interface{})
	userID, _ := getNumOrFloat(fromUser["id"])
	username, _ := fromUser["username"].(string)
	if username == "" {
		username = "未知用户"
	}
	state.Logger.Info(fmt.Sprintf("收到Telegram普通消息: user_id=%v, username=%s, text=%s",
		userID, username, truncate(text, 100)), "telegram")

	// 发送者授权
	if !telegram.IsAuthorizedActor(state, chatID, fromUser["id"]) {
		state.Logger.Warn(fmt.Sprintf("拒绝未授权的 Telegram 消息: chat_id=%v, user_id=%v", chatID, userID), "telegram")
		u.JSON(http.StatusForbidden, gin.H{"ok": false, "error": "unauthorized_actor"})
		return
	}

	// 频率限制
	rateKey := telegram.ChatIDString(chatID)
	if rateKey == "" {
		rateKey = telegram.ChatIDString(fromUser["id"])
	}
	if !telegram.AllowRate(rateKey) {
		telegram.SendReply(state, chatID, "⚠️ 操作过于频繁，请稍后再试", int64(messageID))
		u.JSON(http.StatusTooManyRequests, gin.H{"ok": false, "error": "rate_limited"})
		return
	}

	// 命令排在花钱动作之前。命令只读或只做撤销,兼容模式下也该能用 ——
	// 恰恰是那个模式最需要 /help 把"去注册 secret"这句话讲给用户听。
	if handleCommand(state, mon, chatID, int64(messageID), text) {
		u.JSON(http.StatusOK, gin.H{"ok": true, "handled": "command"})
		return
	}

	orderInfo := telegram.ParseOrderMessage(text)
	if orderInfo == nil {
		// 以前这里是 Debug 一行日志然后静默丢掉 —— 用户发了东西,
		// 屏幕上什么都没发生,他无从知道自己该发什么。
		state.Logger.Debug("消息不是下单格式，回提示", "telegram")
		telegram.SendReply(state, chatID,
			"没看懂这条消息。\n\n下单格式：<型号> [机房] [数量] [配置]\n"+
				"例：24sk602 gra 2\n\n发 /help 看完整说明。", int64(messageID))
		u.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	state.Logger.Info(fmt.Sprintf("解析下单消息: planCode=%s, datacenter=%s, quantity=%d, options=%v",
		orderInfo.PlanCode, orderInfo.Datacenter, orderInfo.Quantity, orderInfo.Options), "telegram")
	// —— 账户怎么定 ——
	//
	// 1) 命令里显式写了 @账户 → 照做，**不再确认**。
	//    用户已经明确表达过了，再拦一下纯属碍事，而抢购最不能忍的就是多一次往返。
	// 2) 没写 → 按 planCode 查它在哪个账户的目录里，然后**确认一次**再下。
	//    账户选错的后果是"永远抢不到"，OVH 不报错、日志里也没异常 ——
	//    用户唯一能发现的机会就是下单前看到它落在哪儿。
	if ref := orderInfo.AccountRef; ref != "" {
		accs, errMsg := resolveAccountRef(state, mon, ref, orderInfo.PlanCode)
		if errMsg != "" {
			telegram.SendReply(state, chatID, "❌ "+errMsg, int64(messageID))
			u.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "bad_account_ref"})
			return
		}
		telegram.SendReply(state, chatID, runOrder(state, orderInfo, accs), int64(messageID))
		u.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	ra := resolveOrderAccount(state, mon, orderInfo.PlanCode)
	if ra.Account.ID == "" {
		telegram.SendReply(state, chatID, "❌ 无法下单\n\n"+ra.Reason, int64(messageID))
		u.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "no_account"})
		return
	}
	// 候选账户：能买这个 planCode 的都列出来，让确认那一步可以一键改
	alt := []types.OVHAccount{ra.Account}
	if mon != nil {
		if buyable := mon.AccountsForPlan(orderInfo.PlanCode); len(buyable) > 0 {
			alt = buyable
		}
	}
	askOrderConfirm(state, chatID, int64(messageID), orderInfo, ra, alt)
	u.JSON(http.StatusOK, gin.H{"ok": true, "handled": "order_confirm"})
	return
}

// parseUpdateID 从 update JSON 里取 update_id（JSON 数字解出来可能是 float64 / json.Number）
func parseUpdateID(v interface{}) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case int64:
		return x
	}
	return 0
}

// decodeCallbackData 解析 callback_data，支持 "b64:" 前缀的 base64 包装和裸 JSON 两种。
func decodeCallbackData(state *app.State, cbData string) (map[string]interface{}, bool) {
	payload := []byte(cbData)
	if strings.HasPrefix(cbData, "b64:") {
		base64Part := cbData[4:]
		if missing := len(base64Part) % 4; missing != 0 {
			base64Part += strings.Repeat("=", 4-missing)
		}
		decoded, err := base64.StdEncoding.DecodeString(base64Part)
		if err != nil {
			state.Logger.Warn(fmt.Sprintf("base64解码失败（可能是数据被截断）: %s, base64_len=%d", err.Error(), len(cbData[4:])), "telegram")
			return nil, false
		}
		payload = decoded
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(payload, &obj); err != nil {
		state.Logger.Error("解析callback_data JSON失败: "+err.Error()+", data="+truncate(cbData, 100), "telegram")
		return nil, false
	}
	return obj, true
}

func getNested(m map[string]interface{}, keys ...string) interface{} {
	var cur interface{} = m
	for _, k := range keys {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

func getNumOrFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func strOr(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func toStringSlice(v interface{}) []string {
	out := []string{}
	switch x := v.(type) {
	case []interface{}:
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, x...)
	}
	return out
}

// extractKeyboard 从 callback_query.message 里取出原始 inline_keyboard。
// JSON 解出来是 []interface{} 套 map，这里转成 MarkButtonPressed 要的形状。
func extractKeyboard(message map[string]interface{}) [][]map[string]interface{} {
	markup, ok := message["reply_markup"].(map[string]interface{})
	if !ok {
		return nil
	}
	rows, ok := markup["inline_keyboard"].([]interface{})
	if !ok {
		return nil
	}
	out := make([][]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		cells, ok := r.([]interface{})
		if !ok {
			continue
		}
		row := make([]map[string]interface{}, 0, len(cells))
		for _, c := range cells {
			if b, ok := c.(map[string]interface{}); ok {
				row = append(row, b)
			}
		}
		out = append(out, row)
	}
	return out
}
