package telegram

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ovh-buy/server/internal/app"
)

// TaskAction 任务按钮的操作码。回调数据用完整任务 ID,避免短号前缀撞车;
// {"a":"qt","i":<uuid>,"o":"r"} 最长 60 字节,在 Telegram 64 字节上限内。
const (
	TaskPause  = "p"
	TaskResume = "r"
)

// TaskButton 生成一颗作用于单个抢购任务的内联按钮。
func TaskButton(text, taskID, op string) map[string]interface{} {
	data, _ := json.Marshal(map[string]string{"a": "qt", "i": taskID, "o": op})
	return map[string]interface{}{"text": text, "callback_data": string(data)}
}

// EditMessage 原地改写一条消息的文字和键盘;按钮操作后不再刷新消息。
func EditMessage(state *app.State, chatID interface{}, messageID int64, text string, replyMarkup map[string]interface{}) bool {
	cfg := state.Config.Get()
	if cfg.TgToken == "" || messageID <= 0 {
		return false
	}
	payload := map[string]interface{}{"chat_id": chatID, "message_id": messageID, "text": text}
	if replyMarkup != nil {
		payload["reply_markup"] = replyMarkup
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, "https://api.telegram.org/bot"+cfg.TgToken+"/editMessageText", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient(10 * time.Second).Do(req)
	if err != nil {
		state.Logger.Debug("编辑 Telegram 消息失败: "+scrub(err.Error()), "telegram")
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
