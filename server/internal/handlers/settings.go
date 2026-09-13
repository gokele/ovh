package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// GetSettings GET /api/settings
func GetSettings(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, state.Config.Get())
	}
}

// SaveSettings POST /api/settings
func SaveSettings(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var newCfg types.Config
		if err := c.ShouldBindJSON(&newCfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": ovh.Explain(err)})
			return
		}

		prev := state.Config.Get()

		// 凭据去空白（前端粘贴时常带空格/换行，会导致 OVH 签名失败 "Invalid signature"）
		newCfg.AppKey = strings.TrimSpace(newCfg.AppKey)
		newCfg.AppSecret = strings.TrimSpace(newCfg.AppSecret)
		newCfg.ConsumerKey = strings.TrimSpace(newCfg.ConsumerKey)
		newCfg.TgToken = strings.TrimSpace(newCfg.TgToken)
		newCfg.TgChatID = strings.TrimSpace(newCfg.TgChatID)
		// 同样去空白:webhook 地址末尾带个换行,POST 出去就是 DNS 解析失败
		newCfg.NotifyWebhookURL = strings.TrimSpace(newCfg.NotifyWebhookURL)
		// 在保存这一步就把地址挡下来。放过去的话,用户要等到真有货那一刻
		// 才会发现通知发不出去 —— 而那正是唯一不能出错的时刻。
		if newCfg.NotifyWebhookURL != "" {
			u, err := url.Parse(newCfg.NotifyWebhookURL)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error",
					"message": "通知 Webhook 地址不合法,必须是完整的 http:// 或 https:// 地址"})
				return
			}
		}

		// 默认值兜底
		if newCfg.Endpoint == "" {
			newCfg.Endpoint = "ovh-eu"
		}
		if newCfg.Zone == "" {
			newCfg.Zone = "IE"
		}
		// 前端留空 / 传 0 = 用默认;超出区间夹回来。存进去的永远是明确的数
		newCfg.DefaultRetryInterval = types.ClampRetryInterval(newCfg.DefaultRetryInterval, types.DefaultTaskRetryInterval)
		newCfg.QuickOrderRetryInterval = types.ClampRetryInterval(newCfg.QuickOrderRetryInterval, types.DefaultQuickRetryInterval)

		if err := state.Config.Set(newCfg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": ovh.Explain(err)})
			return
		}
		state.Logger.Info("API settings updated in config.json", "system")

		// Token 变了就得重拉长轮询。
		//
		// 收 update 只有这一条路,而循环是拿着旧 Token 在跑的 ——
		// 不重启的话用户换完 Token 保存,界面一切正常,但从此一条命令、
		// 一个按钮都收不到,而且没有任何地方会提示他。
		// 首次填 Token 同理:启动时没有 Token,poller 根本没起来。
		if newCfg.TgToken != prev.TgToken {
			state.Logger.Info("Telegram Token 已变更,重启长轮询", "telegram")
			go RestartPoller(state)
		}

		// TG 配置变更 → 同步发一条测试消息
		if newCfg.TgToken != "" && newCfg.TgChatID != "" {
			changed := newCfg.TgToken != prev.TgToken || newCfg.TgChatID != prev.TgChatID
			if changed || prev.TgToken == "" || prev.TgChatID == "" {
				state.Logger.Info("Telegram Token或Chat ID已更新/设置。尝试发送Telegram测试消息到 Chat ID: "+newCfg.TgChatID, "")
				if telegram.SendMessage(state, "OVH 控制台: Telegram 通知已成功配置 (来自 Go 后端测试)", nil) {
					state.Logger.Info("Telegram 测试消息发送成功。", "")
				} else {
					state.Logger.Warn("Telegram 测试消息发送失败。请检查 Token 和 Chat ID 以及后端日志。", "")
				}
			} else {
				state.Logger.Info("Telegram 配置未更改，跳过测试消息。", "")
			}
		} else {
			state.Logger.Info("未配置 Telegram Token 或 Chat ID，跳过测试消息。", "")
		}

		c.JSON(http.StatusOK, gin.H{"status": "success"})
	}
}

// VerifyAuth POST /api/verify-auth
func VerifyAuth(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := ovhClientFor(state, c)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"valid": false})
			return
		}
		var me map[string]interface{}
		if err := client.Get("/me", &me); err != nil {
			state.Logger.Error("Authentication verification failed: "+err.Error(), "system")
			c.JSON(http.StatusOK, gin.H{"valid": false})
			return
		}
		c.JSON(http.StatusOK, gin.H{"valid": true})
	}
}

// EndpointConfig GET /api/endpoint-config
func EndpointConfig(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := state.Config.Get()
		c.JSON(http.StatusOK, gin.H{
			"endpoint": cfg.Endpoint,
			"zone":     cfg.Zone,
		})
	}
}
