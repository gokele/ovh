package telegram

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
)

// SecretTokenHeader Telegram 在 setWebhook 带了 secret_token 之后，
// 每次回调都会带上这个请求头。它是唯一能证明「这条回调真的来自 Telegram」的凭据 ——
// /api/telegram/webhook 在鉴权白名单里（Telegram 不可能带 X-API-Key），
// 没有它任何人只要知道 URL 就能伪造一条 callback_query 触发下单。
const SecretTokenHeader = "X-Telegram-Bot-Api-Secret-Token"

const (
	// MaxTelegramBodyBytes webhook 请求体上限。Telegram 的 update 远小于此。
	MaxTelegramBodyBytes = 64 * 1024
	// UpdateIDRetentionDays update_id 幂等表保留天数
	UpdateIDRetentionDays = 7
	// ButtonTTL 一键下单按钮有效期，与旧的 messageUUIDCacheTTL 保持一致
	ButtonTTL = 24 * time.Hour
	// RateLimitWindow / RateLimitMaxPerWindow 单 chat 的处理频率上限
	RateLimitWindow       = 10 * time.Second
	RateLimitMaxPerWindow = 8
)

// EnsureWebhookSecret 取（必要时生成）webhook secret_token。
// 优先环境变量 TG_WEBHOOK_SECRET；否则用库里已有的；都没有就生成 32 字节随机值落库。
func EnsureWebhookSecret(state *app.State) (string, error) {
	if env := strings.TrimSpace(os.Getenv("TG_WEBHOOK_SECRET")); env != "" {
		cfg := state.Config.Get()
		if cfg.TgWebhookSecret != env {
			cfg.TgWebhookSecret = env
			// 环境变量本身已经够用，落库失败不阻断
			if err := state.Config.Set(cfg); err != nil {
				state.Logger.Warn("写入 TG_WEBHOOK_SECRET 到配置失败: "+err.Error(), "telegram")
			}
		}
		return env, nil
	}
	cfg := state.Config.Get()
	if s := strings.TrimSpace(cfg.TgWebhookSecret); s != "" {
		return s, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成 webhook secret 失败: %w", err)
	}
	secret := hex.EncodeToString(b)
	cfg.TgWebhookSecret = secret
	if err := state.Config.Set(cfg); err != nil {
		return "", err
	}
	state.Logger.Info("已生成 Telegram webhook secret_token 并落库", "telegram")
	return secret, nil
}

// MarkWebhookSecretRegistered setWebhook 带 secret 成功后调用。
// 只有标记过之后 webhook 才会强制校验 secret —— 升级前注册的老 webhook
// 不带这个头，直接强制校验会让所有按钮和文本下单立刻全挂。
func MarkWebhookSecretRegistered(state *app.State) {
	cfg := state.Config.Get()
	if cfg.TgWebhookSecretRegistered {
		return
	}
	cfg.TgWebhookSecretRegistered = true
	if err := state.Config.Set(cfg); err != nil {
		state.Logger.Warn("标记 webhook secret 已注册失败: "+err.Error(), "telegram")
		return
	}
	state.Logger.Info("Telegram webhook 已启用 secret_token 强校验", "telegram")
}

// WebhookSecretEnforced 当前是否强制校验 secret
func WebhookSecretEnforced(state *app.State) bool {
	if strings.EqualFold(os.Getenv("TG_WEBHOOK_SECRET_OPTIONAL"), "true") {
		return false
	}
	return state.Config.Get().TgWebhookSecretRegistered
}

// ValidateWebhookSecret 校验请求头里的 secret_token。
// 返回 (ok, 是否处于兼容模式)。兼容模式下放行但调用方应该打警告日志。
func ValidateWebhookSecret(state *app.State, headerValue string) (ok bool, legacy bool) {
	if !WebhookSecretEnforced(state) {
		return true, true
	}
	want, err := EnsureWebhookSecret(state)
	if err != nil || want == "" {
		// 强校验模式却拿不到 secret，属于配置损坏，拒绝比放行安全
		return false, false
	}
	got := strings.TrimSpace(headerValue)
	if got == "" || len(got) != len(want) {
		return false, false
	}
	// 常量时间比较，避免时序侧信道
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1, false
}

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
	for _, p := range strings.Split(csv, ",") {
		if normalizeID(strings.TrimSpace(p)) == id {
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

// AutoUpgradeWebhookSecret 启动时自愈：如果 Telegram 那边已经注册过 webhook，
// 但这个 secret 还没推过去（升级上来的老部署），就用同一个 URL 重新 setWebhook 一次，
// 把 secret_token 带上。之后 webhook 自动进入强校验模式，用户无需手动操作。
//
// 没配 Token、没注册过 webhook、或者已经是强校验模式时，这个函数什么都不做。
func AutoUpgradeWebhookSecret(state *app.State) {
	cfg := state.Config.Get()
	if strings.TrimSpace(cfg.TgToken) == "" {
		return
	}
	if cfg.TgWebhookSecretRegistered {
		return
	}
	ok, info, errMsg := GetWebhookInfo(state)
	if !ok {
		state.Logger.Debug("跳过 webhook secret 自愈（无法读取 webhook 信息）: "+errMsg, "telegram")
		return
	}
	current, _ := info["url"].(string)
	if strings.TrimSpace(current) == "" {
		// 从没注册过 webhook：用户之后在设置页点「注册」时自然会带上 secret
		return
	}
	state.Logger.Info("检测到 webhook 未启用 secret_token，正在用同一 URL 重新注册以启用强校验", "telegram")
	if done, msg, _ := SetWebhook(state, current); done {
		state.Logger.Info("✅ webhook secret_token 已启用: "+msg, "telegram")
	} else {
		state.Logger.Warn("webhook secret_token 自动启用失败（保持兼容模式）: "+msg, "telegram")
	}
}
