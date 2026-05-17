package auth

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Config 认证配置（从环境变量初始化）
type Config struct {
	APIKey  string
	Enabled bool
	// WhitelistPaths 跳过验证的路径
	WhitelistPaths map[string]struct{}
}

// DefaultWhitelist 不需要 X-API-Key 即可访问的路径
func DefaultWhitelist() map[string]struct{} {
	return map[string]struct{}{
		"/health":                      {},
		"/api/health":                  {},
		"/api/version":                 {}, // 前端启动时拉版本号,登录前可见
		"/api/internal/monitor/price":  {},
		"/api/telegram/webhook":        {},
	}
}

// Middleware Gin 中间件：验证 X-API-Key
func Middleware(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.Enabled {
			c.Next()
			return
		}

		// CORS 预检放行
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		path := c.Request.URL.Path

		// 只验证 /api 路径
		if !strings.HasPrefix(path, "/api/") {
			c.Next()
			return
		}

		// 白名单放行
		if _, ok := cfg.WhitelistPaths[path]; ok {
			c.Next()
			return
		}

		key := c.GetHeader("X-API-Key")
		if key == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "Missing API key",
				"message": "缺少API密钥，请通过官方前端访问",
				"code":    "NO_API_KEY",
			})
			return
		}

		if key != cfg.APIKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "Invalid API key",
				"message": "API密钥无效，禁止访问",
				"code":    "INVALID_API_KEY",
			})
			return
		}

		// 可选时间戳校验（防重放）：与服务器时间相差超过 5 分钟则拒绝
		if ts := c.GetHeader("X-Request-Time"); ts != "" {
			if reqMs, err := strconv.ParseInt(ts, 10, 64); err == nil {
				diff := time.Now().UnixMilli() - reqMs
				if diff < 0 {
					diff = -diff
				}
				if diff > 5*60*1000 {
					c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
						"error":   "Request expired",
						"message": "请求已过期（时间戳验证失败）",
						"code":    "TIMESTAMP_EXPIRED",
					})
					return
				}
			}
		}

		c.Next()
	}
}
