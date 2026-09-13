package telegram

// 发送者授权与频率限制。
//
// 收 update 只有长轮询一条路(见 poller.go),没有入站端点,
// 所以不存在"伪造来源"的问题 —— update 是我们自己用 Bot Token 拉回来的。
// 唯一要判的是「这条消息是不是配置里那个 chat 发的」。

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

const (
	// MaxTelegramBodyBytes 单次读取 Telegram 响应的上限。update 远小于此。
	MaxTelegramBodyBytes = 64 * 1024
	// UpdateIDRetentionDays update_id 幂等表保留天数
	UpdateIDRetentionDays = 7
	// ButtonTTL 一键下单按钮有效期，与旧的 messageUUIDCacheTTL 保持一致
	ButtonTTL = 24 * time.Hour
	// RateLimitWindow / RateLimitMaxPerWindow 单 chat 的处理频率上限
	RateLimitWindow       = 10 * time.Second
	RateLimitMaxPerWindow = 8
)

// IsAuthorizedActor 判断这条 update 的发送者是否是配置里那个 chat。
// 只认 config.TgChatID：
//   - 私聊：chat_id 等于配置值即可（Telegram 私聊的 chat_id 就是对方 user_id）；
//   - 群/超级群（chat_id 为负）：除 chat 匹配外，发送者必须在 TG_ALLOWED_USER_IDS 白名单里，
//     否则群里任何成员都能下单。
//
// 这层挡的是「secret 泄漏 / 兼容模式」下的越权，不是伪造来源 —— 伪造来源由 secret 挡。
func IsAuthorizedActor(state *app.State, chatID, userID interface{}) bool {
	want := normalizeID(state.Config.Get().TgChatID)
	if want == "" {
		return false
	}
	gotChat := normalizeID(idToString(chatID))
	gotUser := normalizeID(idToString(userID))

	if gotChat != "" && gotChat == want {
		if strings.HasPrefix(gotChat, "-") {
			allow := strings.TrimSpace(os.Getenv("TG_ALLOWED_USER_IDS"))
			if allow == "" {
				return false
			}
			return idInCSV(gotUser, allow)
		}
		return true
	}
	// 兼容：配置里填的是 user id，私聊时 chat_id 与之相等
	if gotUser != "" && gotUser == want && (gotChat == "" || gotChat == gotUser) {
		return true
	}
	return false
}

func idInCSV(id, csv string) bool {
	if id == "" {
		return false
	}
	// 走 SplitList:这串 chat ID 是用户在设置页手打的,中文输入法打出的全角逗号
	// 会让整条白名单匹配不上任何人 —— 表现是自己被锁在机器人外面,且毫无提示。
	for _, p := range types.SplitList(csv) {
		if normalizeID(p) == id {
			return true
		}
	}
	return false
}

// normalizeID 去掉 @ 前缀和小数点尾巴（JSON 数字解出来是 float64）
func normalizeID(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "@")
	if i := strings.Index(s, "."); i >= 0 {
		s = s[:i]
	}
	return s
}

// idToString 把 chat_id / user_id（float64 / json.Number / string）统一成字符串
func idToString(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return fmt.Sprintf("%.0f", x)
	case int64:
		return fmt.Sprintf("%d", x)
	case int:
		return fmt.Sprintf("%d", x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// ChatIDString 导出给 handler 做频率限制 key
func ChatIDString(v interface{}) string { return normalizeID(idToString(v)) }

// --- 进程内频率限制 ---

type rateBucket struct {
	windowStart time.Time
	count       int
}

var (
	rateMu   sync.Mutex
	rateByID = map[string]*rateBucket{}
)

// AllowRate 按 chat（取不到则 user）维度限流，返回是否放行。
func AllowRate(id string) bool {
	if id == "" {
		id = "unknown"
	}
	now := time.Now()
	rateMu.Lock()
	defer rateMu.Unlock()
	b, ok := rateByID[id]
	if !ok || now.Sub(b.windowStart) > RateLimitWindow {
		rateByID[id] = &rateBucket{windowStart: now, count: 1}
		return true
	}
	if b.count >= RateLimitMaxPerWindow {
		return false
	}
	b.count++
	return true
}
