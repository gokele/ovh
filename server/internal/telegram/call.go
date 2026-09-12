package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/netfp"
)

// MaxMessageRunes Telegram sendMessage 的 text 硬上限(字符数,不是字节)。
// 超了 Telegram 直接 400,消息**根本发不出去** —— 而这个工具最要紧的那条消息
// (补货通知)正是走这条路。实测最坏情况的补货通知 559 字符,离上限很远,
// 但拼了 OVH 原始报错的失败通知没有天然上限,必须截。
const MaxMessageRunes = 4096

// MaxCallbackAnswerRunes answerCallbackQuery 的 text 上限(官方:0-200 字符)。
const MaxCallbackAnswerRunes = 200

// httpClient Telegram 的出站客户端。
//
// 以前九个发送函数各自 `&http.Client{Timeout: ...}` —— 每个都带一个独立的
// Transport,连接池完全不复用,每次发消息都要重新 TLS 握手。
// 走 netfp.Shared 还顺带解决了另一件事:它带默认账户的代理配置,
// 而 api.telegram.org 在某些网络里必须走代理才通。
func httpClient(timeout time.Duration) *http.Client {
	return netfp.Shared(timeout)
}

// truncateRunes 按**字符**截断(不是字节)。
// 按字节切会把一个中文字劈成两半,发出去是乱码 —— 而这些消息基本都是中文。
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

// apiResult Telegram 所有方法的统一响应外壳。
type apiResult struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
	Result      json.RawMessage `json:"result"`
}

// call 调一次 Bot API。
//
// 这是这个包里唯一发请求的地方。统一它解决了三件以前各写各的事:
//
//  1. **响应必须检查**。以前 SendReply 和 AnswerCallback 是
//     `if err == nil { resp.Body.Close() }` —— 网络错误和 Telegram 的
//     ok:false 全都吞掉。用户发了 /queue 收不到任何回复,而日志里一个字都没有,
//     根本没法排查。Telegram 拒绝消息的常见原因(text 超长、chat 不存在、
//     bot 被踢出群、按钮 callback_data 超 64 字节)都只会体现在响应里。
//
//  2. **连接池复用**。九个各自的 http.Client 意味着九套 Transport。
//
//  3. **token 不能进日志**。错误信息里可能带完整 URL,scrub 统一处理。
//
// 返回的 error 已经是可以直接记日志的中文描述。
func call(state *app.State, token, method string, payload interface{}, timeout time.Duration) (*apiResult, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost,
		"https://api.telegram.org/bot"+token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %s", scrub(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient(timeout).Do(req)
	if err != nil {
		return nil, fmt.Errorf("连不上 Telegram: %s", scrub(err.Error()))
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, MaxTelegramBodyBytes))
	var r apiResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("Telegram 返回了无法解析的内容(HTTP %d): %s",
			resp.StatusCode, truncateRunes(strings.TrimSpace(string(raw)), 120))
	}
	if !r.OK {
		desc := r.Description
		if desc == "" {
			desc = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return &r, fmt.Errorf("Telegram 拒绝了 %s: %s (error_code=%d)", method, desc, r.ErrorCode)
	}
	return &r, nil
}
