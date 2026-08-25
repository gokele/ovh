package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// GetSettings GET /api/settings
func GetSettings(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := state.Config.Get()
		// webhook secret 不下发前端：它只在后端和 Telegram 之间使用，
		// 前端拿到也没用，暴露面反而变大。
		cfg.TgWebhookSecret = ""
		c.JSON(http.StatusOK, cfg)
	}
}

// SaveSettings POST /api/settings
func SaveSettings(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var newCfg types.Config
		if err := c.ShouldBindJSON(&newCfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
			return
		}

		prev := state.Config.Get()

		// 凭据去空白（前端粘贴时常带空格/换行，会导致 OVH 签名失败 "Invalid signature"）
		newCfg.AppKey = strings.TrimSpace(newCfg.AppKey)
		newCfg.AppSecret = strings.TrimSpace(newCfg.AppSecret)
		newCfg.ConsumerKey = strings.TrimSpace(newCfg.ConsumerKey)
		newCfg.TgToken = strings.TrimSpace(newCfg.TgToken)
		newCfg.TgChatID = strings.TrimSpace(newCfg.TgChatID)

		// webhook secret 前端不可见也不可改（GetSettings 已抹掉），
		// 这里必须从旧配置继承回来，否则前端保存一次设置就把 secret 清了，
		// Telegram 那边仍在校验旧 secret → 所有回调直接 401。
		newCfg.TgWebhookSecret = prev.TgWebhookSecret
		newCfg.TgWebhookSecretRegistered = prev.TgWebhookSecretRegistered

		// 默认值兜底
		if newCfg.Endpoint == "" {
			newCfg.Endpoint = "ovh-eu"
		}
		if newCfg.Zone == "" {
			newCfg.Zone = "IE"
		}

		if err := state.Config.Set(newCfg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			return
		}
		state.Logger.Info("API settings updated in config.json", "system")

		// TG 配置变更 → 同步发测试消息（1:1 对应 Python save_settings 2450-2463）
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
