package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ovh-buy/server/internal/app"
)

// VerifyConfig 检查 Telegram 是否可用:Token / Chat ID 是否填写 + bot 是否能 getMe + chat 是否可访问。
// 用于 AddSubscription 等"必须 TG 有效"的强制校验。
// 返回 (ok, 失败原因)。所有失败原因都是面向终端用户的中文短句。
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
		return false, "无法连接 Telegram API: " + err.Error()
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
		return false, "无法连接 Telegram API: " + err.Error()
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

// SendMessage 对应 Python: send_telegram_msg
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

	state.Logger.Info(fmt.Sprintf("准备发送Telegram消息，ChatID: %s, TokenLength: %d", cfg.TgChatID, len(cfg.TgToken)), "")

	url := "https://api.telegram.org/bot" + cfg.TgToken + "/sendMessage"
	payload := map[string]interface{}{
		"chat_id": cfg.TgChatID,
		"text":    message,
	}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}

	body, _ := json.Marshal(payload)

	state.Logger.Info("发送HTTP请求到Telegram API: "+url[:min(45, len(url))]+"...", "")

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		state.Logger.Error("发送Telegram消息时发生未预期错误: "+err.Error(), "")
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		state.Logger.Error("发送Telegram消息时发生网络错误: "+err.Error(), "")
		return false
	}
	defer resp.Body.Close()

	state.Logger.Info(fmt.Sprintf("Telegram API响应: 状态码=%d", resp.StatusCode), "")

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		state.Logger.Info("Telegram响应数据: "+string(respBody), "")
		state.Logger.Info("成功发送消息到Telegram", "")
		return true
	}
	state.Logger.Error(fmt.Sprintf("发送消息到Telegram失败: 状态码=%d, 响应=%s", resp.StatusCode, string(respBody)), "")
	return false
}

// SetWebhook 调用 Telegram setWebhook
func SetWebhook(state *app.State, webhookURL string) (bool, string, map[string]interface{}) {
	cfg := state.Config.Get()
	if cfg.TgToken == "" {
		return false, "未配置 Telegram Bot Token", nil
	}
	if !strings.HasPrefix(webhookURL, "http://") && !strings.HasPrefix(webhookURL, "https://") {
		return false, "Webhook URL 必须以 http:// 或 https:// 开头", nil
	}
	if !strings.HasSuffix(webhookURL, "/api/telegram/webhook") {
		webhookURL = strings.TrimSuffix(webhookURL, "/") + "/api/telegram/webhook"
	}
	state.Logger.Info("正在设置 Telegram Webhook: "+webhookURL, "telegram")

	setURL := "https://api.telegram.org/bot" + cfg.TgToken + "/setWebhook"
	req, _ := http.NewRequest(http.MethodPost, setURL+"?url="+webhookURL, nil)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		state.Logger.Error("请求 Telegram API 失败: "+err.Error(), "telegram")
		return false, err.Error(), nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	_ = json.Unmarshal(body, &result)
	if ok, _ := result["ok"].(bool); ok {
		state.Logger.Info("✅ Telegram Webhook 设置成功: "+webhookURL, "telegram")
		// 获取 webhook info
		var info map[string]interface{}
		infoResp, err := client.Get("https://api.telegram.org/bot" + cfg.TgToken + "/getWebhookInfo")
		if err == nil {
			infoBody, _ := io.ReadAll(infoResp.Body)
			infoResp.Body.Close()
			var infoResult map[string]interface{}
			_ = json.Unmarshal(infoBody, &infoResult)
			if r, ok := infoResult["result"].(map[string]interface{}); ok {
				info = r
			}
		}
		return true, webhookURL, info
	}
	desc, _ := result["description"].(string)
	state.Logger.Error("Telegram Webhook 设置失败: "+desc, "telegram")
	return false, desc, nil
}

// GetWebhookInfo
func GetWebhookInfo(state *app.State) (bool, map[string]interface{}, string) {
	cfg := state.Config.Get()
	if cfg.TgToken == "" {
		return false, nil, "未配置 Telegram Bot Token"
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.telegram.org/bot" + cfg.TgToken + "/getWebhookInfo")
	if err != nil {
		state.Logger.Error("请求 Telegram API 失败: "+err.Error(), "telegram")
		return false, nil, err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	_ = json.Unmarshal(body, &result)
	if ok, _ := result["ok"].(bool); ok {
		if r, ok := result["result"].(map[string]interface{}); ok {
			return true, r, ""
		}
		return true, nil, ""
	}
	desc, _ := result["description"].(string)
	return false, nil, desc
}

// AnswerCallback 应答 callback_query
func AnswerCallback(state *app.State, callbackQueryID, text string, showAlert bool) {
	cfg := state.Config.Get()
	if cfg.TgToken == "" {
		return
	}
	payload := map[string]interface{}{
		"callback_query_id": callbackQueryID,
		"text":              text,
		"show_alert":        showAlert,
	}
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest(http.MethodPost,
		"https://api.telegram.org/bot"+cfg.TgToken+"/answerCallbackQuery",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// SendReply 回复指定消息
func SendReply(state *app.State, chatID interface{}, text string, replyToMessageID int64) {
	cfg := state.Config.Get()
	if cfg.TgToken == "" {
		return
	}
	payload := map[string]interface{}{
		"chat_id":             chatID,
		"text":                text,
		"reply_to_message_id": replyToMessageID,
	}
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest(http.MethodPost,
		"https://api.telegram.org/bot"+cfg.TgToken+"/sendMessage",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// OrderInfo 对应 Python parse_telegram_order_message 返回
type OrderInfo struct {
	PlanCode   string
	Datacenter string
	Quantity   int
	Options    []string
}

// ParseOrderMessage 对应 Python: parse_telegram_order_message
// 格式: plancode [datacenter] [quantity] [options(逗号分隔)]
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
	remaining := []string{}
	if len(parts) > 1 {
		remaining = parts[1:]
	}
	if len(remaining) == 0 {
		return result
	}

	// 找包含逗号的部分 = options
	optionsStart := -1
	for i, p := range remaining {
		if strings.Contains(p, ",") {
			optionsStart = i
			break
		}
	}
	if optionsStart >= 0 {
		optsText := strings.Join(remaining[optionsStart:], " ")
		for _, o := range strings.Split(optsText, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				result.Options = append(result.Options, o)
			}
		}
		remaining = remaining[:optionsStart]
	}

	switch len(remaining) {
	case 1:
		p := remaining[0]
		if n, ok := parsePositiveInt(p); ok {
			result.Quantity = n
		} else if len(p) >= 3 && len(p) <= 4 && isAllLowerAlpha(p) {
			result.Datacenter = p
		}
	case 2:
		p1, p2 := remaining[0], remaining[1]
		if len(p1) >= 3 && len(p1) <= 4 && isAllLowerAlpha(p1) {
			result.Datacenter = p1
			if n, ok := parsePositiveInt(p2); ok {
				result.Quantity = n
			}
		} else if n, ok := parsePositiveInt(p1); ok {
			result.Quantity = n
			if len(p2) >= 3 && len(p2) <= 4 && isAllLowerAlpha(p2) {
				result.Datacenter = p2
			}
		}
	}
	return result
}

// parsePositiveInt 严格匹配 Python str.isdigit() 行为：只接受纯十进制 ASCII 数字字符串，
// 不接受 "-1" / "+5" / " 3" 等带符号或空白的版本（strconv.Atoi 会通过）。
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

func isAllLowerAlpha(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) || !unicode.IsLower(r) {
			return false
		}
	}
	return len(s) > 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
