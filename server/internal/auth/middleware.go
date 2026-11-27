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
	// Accounts 管理员账号与会话;nil 时只认设备令牌
	Accounts *Accounts
	Enabled  bool
	// WhitelistPaths 跳过验证的路径
	WhitelistPaths map[string]struct{}
	// DeviceTokenValid App 设备令牌校验(配对体系,见 internal/db/appdevices.go)。
	// 依赖注入而不是直接 import db:这个包要保持无状态可测。
	DeviceTokenValid func(token string) (int64, bool)
}

// DefaultWhitelist 不需要登录即可访问的路径
func DefaultWhitelist() map[string]struct{} {
	return map[string]struct{}{
		"/health":                   {},
		"/api/health":               {},
		"/api/version":              {}, // 前端启动时拉版本号,登录前可见
		"/api/version/check-update": {}, // 更新检查也免鉴权,登录前可提示
		"/api/app/pair":             {}, // App 配对兑换:凭 2 分钟一次性码,不需要密钥
		"/api/auth/status":          {}, // 登录页要先知道是登录还是首次初始化
		"/api/auth/login":           {},
		"/api/auth/setup":           {}, // 凭初始化密钥,且只在还没有管理员时有效
	}
}

// Middleware Gin 中间件：验证会话令牌(浏览器)或设备令牌(App)
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

		// 先看这个来源是不是已经猜错太多次。放在读 header 之前:
		// 被挡下的请求不该再进入任何比较逻辑。
		if blocked, wait := authBlocked(c.ClientIP(), time.Now()); blocked {
			secs := int(wait.Seconds()) + 1
			c.Header("Retry-After", strconv.Itoa(secs))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many failed attempts",
				"message": "密码或令牌连续错误次数过多,已暂时拒绝该来源的请求。" +
					"请等待约 " + strconv.Itoa(secs) + " 秒后再试",
				"code":       "AUTH_RATE_LIMITED",
				"retryAfter": secs,
			})
			return
		}

		// 浏览器:账户密码登录后拿到的会话令牌
		if token := strings.TrimSpace(c.GetHeader(SessionHeader)); token != "" && cfg.Accounts != nil {
			if user, ok := cfg.Accounts.ValidateSession(token); ok {
				c.Set("auth_user", user)
				clearAuthFailures(c.ClientIP())
				c.Next()
				return
			}
			recordAuthFailure(c.ClientIP(), time.Now())
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "Invalid session",
				"message": "登录已失效,请重新输入用户名和密码",
				"code":    "INVALID_SESSION",
			})
			return
		}

		// App:配对得到的设备令牌
		if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			if token != "" && cfg.DeviceTokenValid != nil {
				if id, ok := cfg.DeviceTokenValid(token); ok {
					c.Set("app_device_id", id)
					clearAuthFailures(c.ClientIP())
					c.Next()
					return
				}
			}
			recordAuthFailure(c.ClientIP(), time.Now())
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "Invalid device token",
				"message": "设备令牌无效或已被吊销。请到网页控制台重新配对(设置 → App 管理)",
				"code":    "INVALID_DEVICE_TOKEN",
			})
			return
		}

		// 旧版 X-API-Key 已停用:它只剩「首次创建账号时的初始化密钥」这一个用途
		if cfg.Accounts != nil && cfg.Accounts.SetupRequired() {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "Setup required",
				"message": "还没有创建管理员账号,请先在网页上完成初始化",
				"code":    "SETUP_REQUIRED",
			})
			return
		}
		if c.GetHeader("X-API-Key") != "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "API key retired",
				"message": "API 密钥登录已停用,请用用户名和密码登录",
				"code":    "API_KEY_RETIRED",
			})
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error":   "Not logged in",
			"message": "请先登录",
			"code":    "NO_SESSION",
		})
		return
	}
}
