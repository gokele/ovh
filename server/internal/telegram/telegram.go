package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
)

// VerifyConfig 检查 Telegram 是否可用:Token / Chat ID 是否填写 + bot 是否能 getMe + chat 是否可访问。
// 用于 AddSubscription 等"必须 TG 有效"的强制校验。
// 返回 (ok, 失败原因)。所有失败原因都是面向终端用户的中文短句。

// tokenRe 匹配 Telegram API URL 里的 bot token 段。
var tokenRe = regexp.MustCompile(`/bot[0-9]+:[A-Za-z0-9_-]+`)

// scrub 抹掉字符串里的 Bot Token。
//
// Go 的 *url.Error 文本长这样:
//
//	Post "https://api.telegram.org/bot<完整TOKEN>/sendMessage": dial tcp ...
//
// 而这类部署连 api.telegram.org 本来就常失败。以前这些 scrub(err.Error()) 被直接
// 拼进日志 —— 明文 Token 落进 logs/ 并通过 GET /api/logs 显示在前端;
// 有几处还把同一串当 error 返回,出现在「添加订阅」的报错和 webhook 信息接口里。
//
// Token 能冒充你发通知、甚至通过 webhook 触发下单,config.go 专门为它做了加密落库,
// 这条路等于把那份保护绕过去了。
func scrub(s string) string {
	return tokenRe.ReplaceAllString(s, "/bot***")
}

func VerifyConfig(state *app.State) (bool, string) {
	cfg := state.Config.Get()
	token := strings.TrimSpace(cfg.TgToken)
	chatID := strings.TrimSpace(cfg.TgChatID)
	if token == "" {
		return false, "未配置 Telegram Bot Token"
	}
	if chatID == "" {
		return false, "未配置 Telegram Chat ID"
	}
	client := &http.Client{Timeout: 10 * time.Second}

	// 1) getMe 验 token
	resp, err := client.Get("https://api.telegram.org/bot" + token + "/getMe")
	if err != nil {
		return false, "无法连接 Telegram API: " + scrub(err.Error())
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var r1 map[string]interface{}
	_ = json.Unmarshal(body, &r1)
	if ok, _ := r1["ok"].(bool); !ok {
		desc, _ := r1["description"].(string)
		if desc == "" {
			desc = "未知错误"
		}
		return false, "Telegram Token 无效: " + desc
	}

	// 2) getChat 验 chat_id (bot 是否能访问这个 chat)
	resp2, err := client.Get("https://api.telegram.org/bot" + token + "/getChat?chat_id=" + chatID)
	if err != nil {
		return false, "无法连接 Telegram API: " + scrub(err.Error())
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	var r2 map[string]interface{}
	_ = json.Unmarshal(body2, &r2)
	if ok, _ := r2["ok"].(bool); !ok {
		desc, _ := r2["description"].(string)
		if desc == "" {
			desc = "未知错误"
		}
		return false, "Telegram Chat ID 不可达: " + desc + " (请先给 bot 发一条消息)"
	}
	return true, ""
}

func SendMessage(state *app.State, message string, replyMarkup map[string]interface{}) bool {
	cfg := state.Config.Get()
	if cfg.TgToken == "" {
		state.Logger.Warn("Telegram消息未发送: Bot Token未在config中设置", "")
		return false
	}
	if cfg.TgChatID == "" {
		state.Logger.Warn("Telegram消息未发送: Chat ID未在config中设置", "")
		return false
	}

	// 超 4096 字符 Telegram 直接 400,整条消息丢掉 —— 而走这条路的是补货通知
	// 和抢购结果,恰恰是最不能丢的。截断后至少把前 4095 个字送到。
	// 按字符截不按字节:按字节切会把中文劈成半个字,发出去是乱码。
	if n := len([]rune(message)); n > MaxMessageRunes {
		state.Logger.Warn(fmt.Sprintf("Telegram 消息 %d 字符超过 %d 上限,已截断发送",
			n, MaxMessageRunes), "telegram")
		message = truncateRunes(message, MaxMessageRunes)
	}

	payload := map[string]interface{}{
		"chat_id": cfg.TgChatID,
		"text":    message,
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}

	if _, err := call(state, cfg.TgToken, "sendMessage", payload, 10*time.Second); err != nil {
		// 这里必须是 Error:补货通知发不出去 = 用户错过这一波货,
		// 而他不会知道曾经有过货
		state.Logger.Error("发送 Telegram 消息失败: "+err.Error(), "telegram")
		return false
	}
	state.Logger.Info("成功发送消息到 Telegram", "telegram")
	return true
}

// AnswerCallback 应答 callback_query
func AnswerCallback(state *app.State, callbackQueryID, text string, showAlert bool) {
	cfg := state.Config.Get()
	if cfg.TgToken == "" {
		return
	}
	// 官方限 0-200 字符,超了整个 answerCallbackQuery 被拒 ——
	// 表现是用户点了按钮、转圈半天没有任何提示,而按钮其实已经生效了。
	_, err := call(state, cfg.TgToken, "answerCallbackQuery", map[string]interface{}{
		"callback_query_id": callbackQueryID,
		"text":              truncateRunes(text, MaxCallbackAnswerRunes),
		"show_alert":        showAlert,
	}, 5*time.Second)
	if err != nil {
		// Debug 而不是 Warn:回调应答只是个气泡提示,失败不影响按钮本身的效果。
		// 但必须留痕 —— 以前这里完全静默,"点了没反应"根本无从查起。
		state.Logger.Debug("回应 Telegram 按钮点击失败: "+err.Error(), "telegram")
	}
}

// SendReply 回复指定消息。
//
// 这是所有 /命令 回复的主路径。以前它是 `if err == nil { resp.Body.Close() }` ——
// 网络错误和 Telegram 的 ok:false 全吞掉:用户发了 /queue 收不到任何东西,
// 而日志里一个字都没有,没法排查。
func SendReply(state *app.State, chatID interface{}, text string, replyToMessageID int64) {
	cfg := state.Config.Get()
	if cfg.TgToken == "" {
		return
	}
	// 超 4096 字符 Telegram 直接 400,整条消息丢掉。截断后至少把前 4095 个字送到,
	// 而不是让用户什么都收不到。
	text = truncateRunes(text, MaxMessageRunes)
	_, err := call(state, cfg.TgToken, "sendMessage", map[string]interface{}{
		"chat_id":             chatID,
		"text":                text,
		"reply_to_message_id": replyToMessageID,
	}, 10*time.Second)
	if err != nil {
		state.Logger.Warn("回复 Telegram 消息失败: "+err.Error(), "telegram")
	}
}

type OrderInfo struct {
	PlanCode   string
	Datacenter string
	// AccountRef 用户在命令里显式指定的账户。原样保留,由 handlers 去解析成具体账户 ——
	// telegram 包看不到账户列表,也不该看到。
	//
	//	"@us" / "@1"  指定一个账户
	//	"@all"        同区每个能买的账户各下一单(抢稀缺机器时翻倍机会)
	//
	// 空 = 没指定,由 planCode 反推。
	AccountRef string
	// Quantity 每个机房下几台。有上限,见 MaxOrderQuantity。
	Quantity int
	Options  []string
}

// 格式: <planCode> [机房] [数量] [配置...]
//
// 除 planCode 必须打头外,后面的部分**位置无关**,按形状认:
//
//	@xxx    → 账户(任意位置,手机上很容易顺手打在末尾)
//	纯数字   → 数量
//	3~4 字母 → 机房(不区分大小写,统一转小写)
//	其余     → 配置(addon planCode),逗号或空格分隔都认
//
// 以前这里是按**位置**猜的:先找带逗号的词当 options,剩下的按
// switch len(remaining) 分 case 1 / case 2。三种常见写法全都静默失效:
//
//	24ska01 gra softraid-2x960ssd     配置没逗号 → 配置被丢掉
//	24ska01 gra 2 softraid-2x960ssd   剩 3 个词 → switch 没有 case 3,
//	                                  机房、数量、配置**全部**丢掉
//	24ska01 GRA 2                     机房要求全小写 → 机房和数量都丢掉
//
// 而丢掉是没有任何报错的:任务照样建,用户拿到的是基础配置的机器。
// 这正是"TG 上下单总是无法选择配置"的来源。
func ParseOrderMessage(text string) *OrderInfo {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil
	}
	result := &OrderInfo{
		PlanCode: parts[0],
		Quantity: 1,
	}
	if len(parts) == 1 {
		return result
	}

	addOption := func(s string) {
		// 逗号分隔和空格分隔都认,混用也认
		for _, o := range strings.Split(s, ",") {
			if o = strings.TrimSpace(o); o != "" {
				result.Options = append(result.Options, o)
			}
		}
	}

	qtySet := false
	for _, p := range parts[1:] {
		switch {
		case strings.HasPrefix(p, "@"):
			// 账户。裸 @ 是打漏了,直接丢 —— 它不是配置项,
			// 当配置发给 OVH 只会换来一个看不懂的 400。
			if len(p) > 1 && result.AccountRef == "" {
				result.AccountRef = strings.ToLower(p[1:])
			}
		case !qtySet && isPositiveInt(p):
			// 只认第一个纯数字。第二个数字多半是配置里的型号
			// (比如手滑把 "2 960" 打成两个词),当数量会把数量改错。
			n, _ := parsePositiveInt(p)
			result.Quantity = clampQuantity(n)
			qtySet = true
		case result.Datacenter == "" && isDatacenterCode(p):
			// OVH 独服机房码都是 3~4 个纯字母(gra/rbx/sbg/bhs/waw/eri/sgp…),
			// 而 addon planCode 一律带连字符和数字(ram-64g-noecc-2133、
			// softraid-2x960ssd),两者不会撞。
			result.Datacenter = strings.ToLower(p)
		default:
			addOption(p)
		}
	}
	return result
}

// isDatacenterCode 长得像机房码:3~4 个 ASCII 字母,不区分大小写。
// 只判形状不查词表 —— OVH 随时开新机房,写死一张表就意味着
// 每开一个机房都要发版,而中间那段时间用户的 /buy 会静默丢掉机房。
func isDatacenterCode(s string) bool {
	if len(s) < 3 || len(s) > 4 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// isPositiveInt 纯十进制 ASCII 数字
func isPositiveInt(s string) bool {
	_, ok := parsePositiveInt(s)
	return ok
}

// parsePositiveInt 只接受纯十进制 ASCII 数字字符串，
// 不接受 "-1" / "+5" / " 3" 等带符号或空白的版本（strconv.Atoi 会通过）。
// MaxOrderQuantity 一条聊天消息能指定的最大数量。
// 没有上限时 "planCode 4000000000" 会让 order_processor 先把 40 亿个
// QueueItem append 进一个切片 —— 进程当场 OOM 被杀。
const MaxOrderQuantity = 20

// MaxOrderFanout 一条消息最多创建多少个抢购任务。
// 不指定机房时任务数 = 配置数 × 有货机房数 × 数量,很容易远超用户直觉。
const MaxOrderFanout = 60

// clampQuantity 把数量夹到 [1, MaxOrderQuantity]
func clampQuantity(n int) int {
	if n < 1 {
		return 1
	}
	if n > MaxOrderQuantity {
		return MaxOrderQuantity
	}
	return n
}

func parsePositiveInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MarkButtonPressed 把原消息上刚按下的那颗按钮标成已下单。
//
// 以前按完按钮，那条上架通知长得和没按过一模一样：几个机房按钮原样摆着，
// 没有任何痕迹说明哪个已经下过单了。在手机上翻回几条消息之前的通知再按一次，
// 是很自然的动作 —— 挡住它的只有服务端那道一次性 claim，
// 用户得到的反馈是一句冷冰冰的「按钮已被使用」，而他根本不记得自己按过。
//
// 这里直接用回调里带回来的原始键盘改一颗按钮的文案，不重建整个键盘 ——
// 其余机房的按钮要原样留着，用户很可能想多买几个机房。
func MarkButtonPressed(state *app.State, chatID interface{}, messageID int64, pressedData, newText string) {
	cfg := state.Config.Get()
	if cfg.TgToken == "" || messageID <= 0 {
		return
	}
	kb := rebuildKeyboard(state, chatID, messageID, pressedData, newText)
	if kb == nil {
		return
	}
	payload := map[string]interface{}{
		"chat_id":      chatID,
		"message_id":   messageID,
		"reply_markup": map[string]interface{}{"inline_keyboard": kb},
	}
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest(http.MethodPost,
		"https://api.telegram.org/bot"+cfg.TgToken+"/editMessageReplyMarkup",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		// 编辑失败不影响下单本身,只是少了个视觉反馈
		state.Logger.Debug("标记按钮已用失败: "+scrub(err.Error()), "telegram")
		return
	}
	resp.Body.Close()
}

// storedKeyboard 由 handlers 在回调里塞进来的原始键盘(来自 callback_query.message)。
// 用一个短生命周期的 map 传递,避免给 MarkButtonPressed 加一个巨大的参数。
var (
	kbMu    sync.Mutex
	kbCache = map[string][][]map[string]interface{}{}
)

// StashKeyboard 记下这条消息当前的键盘,供随后的 MarkButtonPressed 使用。
func StashKeyboard(chatID interface{}, messageID int64, kb [][]map[string]interface{}) {
	if kb == nil {
		return
	}
	kbMu.Lock()
	kbCache[kbKey(chatID, messageID)] = kb
	// 这个 map 只在"收到回调 → 标记按钮"之间活几毫秒,但异常路径可能不取走。
	// 攒到一定量就整体清掉,不引入定时器。
	if len(kbCache) > 256 {
		kbCache = map[string][][]map[string]interface{}{
			kbKey(chatID, messageID): kb,
		}
	}
	kbMu.Unlock()
}

func kbKey(chatID interface{}, messageID int64) string {
	return fmt.Sprintf("%v:%d", chatID, messageID)
}

// rebuildKeyboard 取出原键盘,把命中的那颗按钮改文案。
func rebuildKeyboard(state *app.State, chatID interface{}, messageID int64, pressedData, newText string) [][]map[string]interface{} {
	kbMu.Lock()
	kb, ok := kbCache[kbKey(chatID, messageID)]
	delete(kbCache, kbKey(chatID, messageID))
	kbMu.Unlock()
	if !ok || kb == nil {
		return nil
	}
	hit := false
	for _, row := range kb {
		for _, b := range row {
			if d, _ := b["callback_data"].(string); d == pressedData {
				b["text"] = newText
				hit = true
			}
		}
	}
	if !hit {
		return nil
	}
	return kb
}

// BotCommands 注册给 Telegram 的命令列表。
//
// 注册之后用户在聊天框打 "/" 就会看到这个菜单,点一下就发出去 ——
// 不用记、不用打字,在手机上尤其重要。
// 这是让一个 bot 显得"有人管"最便宜的一件事,而以前一条都没注册过。
var BotCommands = []map[string]string{
	{"command": "help", "description": "怎么用 / 下单格式"},
	{"command": "watch", "description": "盯着补货就抢（/watch 型号 [机房] [x数量]）"},
	{"command": "unwatch", "description": "不盯了（/unwatch 型号）"},
	{"command": "status", "description": "监控与队列总览"},
	{"command": "queue", "description": "正在抢的任务"},
	{"command": "cancel", "description": "取消任务（/cancel 任务号 或 all）"},
	{"command": "subs", "description": "监控订阅列表"},
	{"command": "recent", "description": "最近的抢购结果"},
	{"command": "accounts", "description": "可用的 OVH 账户"},
}

// RegisterCommands 把命令菜单推给 Telegram。
// 幂等,启动时调一次即可;失败只是少了个菜单,不影响任何功能,所以不返回错误。
func RegisterCommands(state *app.State) {
	cfg := state.Config.Get()
	if strings.TrimSpace(cfg.TgToken) == "" {
		return
	}
	payload := map[string]interface{}{"commands": BotCommands}
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest(http.MethodPost,
		"https://api.telegram.org/bot"+cfg.TgToken+"/setMyCommands",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		state.Logger.Debug("注册 Telegram 命令菜单失败: "+scrub(err.Error()), "telegram")
		return
	}
	defer resp.Body.Close()
	var r struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, MaxTelegramBodyBytes))
	_ = json.Unmarshal(respBody, &r)
	if r.OK {
		state.Logger.Info(fmt.Sprintf("已注册 %d 条 Telegram 命令菜单", len(BotCommands)), "telegram")
		return
	}
	state.Logger.Debug("注册 Telegram 命令菜单被拒: "+r.Description, "telegram")
}

// SendKeyboard 发一条带内联键盘的消息。
//
// SendReply 不带 reply_markup，而分步选择流程的每一步都需要按钮 ——
// 让用户在手机上点，而不是去背 ram-64g-noecc-2133 这种 addon 代码。
func SendKeyboard(state *app.State, chatID interface{}, replyToMessageID int64,
	text string, replyMarkup map[string]interface{}) {
	cfg := state.Config.Get()
	if cfg.TgToken == "" {
		return
	}
	payload := map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
	}
	if replyToMessageID > 0 {
		payload["reply_to_message_id"] = replyToMessageID
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest(http.MethodPost,
		"https://api.telegram.org/bot"+cfg.TgToken+"/sendMessage",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		state.Logger.Warn("发送带按钮的消息失败: "+scrub(err.Error()), "telegram")
		return
	}
	defer resp.Body.Close()
	// Telegram 对 callback_data 有 64 字节硬限制，超了它**不会报错**，
	// 按钮发出去就是点了没反应。这里把非 ok 响应记下来，否则这种故障完全无声。
	var r struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, MaxTelegramBodyBytes))
	_ = json.Unmarshal(b, &r)
	if !r.OK {
		state.Logger.Warn("Telegram 拒绝了带按钮的消息: "+r.Description, "telegram")
	}
}
