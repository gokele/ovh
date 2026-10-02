package auth

import (
	"crypto/subtle"
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
	// DeviceTokenValid App 设备令牌校验(配对体系,见 internal/db/appdevices.go)。
	// 依赖注入而不是直接 import db:这个包要保持无状态可测。
	DeviceTokenValid func(token string) (int64, bool)
}

// DefaultWhitelist 不需要 X-API-Key 即可访问的路径
func DefaultWhitelist() map[string]struct{} {
	return map[string]struct{}{
		"/health":                     {},
		"/api/health":                 {},
		"/api/version":                {}, // 前端启动时拉版本号,登录前可见
		"/api/version/check-update":   {}, // 更新检查也免鉴权,登录前可提示
		"/api/internal/monitor/price": {},
		"/api/app/pair":               {}, // App 配对兑换:凭 2 分钟一次性码,不需要密钥
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

		// 先看这个来源是不是已经猜错太多次。放在读 header 之前:
		// 被挡下的请求不该再进入任何比较逻辑。
		if blocked, wait := authBlocked(c.ClientIP(), time.Now()); blocked {
			secs := int(wait.Seconds()) + 1
			c.Header("Retry-After", strconv.Itoa(secs))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many failed attempts",
				"message": "API 密钥连续错误次数过多,已暂时拒绝该来源的请求。" +
					"请等待约 " + strconv.Itoa(secs) + " 秒后再试;" +
					"如果忘了密钥,它在服务器的 .env 文件里(API_SECRET_KEY)",
				"code":       "AUTH_RATE_LIMITED",
				"retryAfter": secs,
			})
			return
		}

		key := c.GetHeader("X-API-Key")

		// App 设备令牌:Authorization: Bearer <token>。
		// 与 X-API-Key 同权(整个 /api 面都可用),但可单独吊销 ——
		// 手机丢了在网页端点一下 Revoke,不用换主密钥、其他设备不掉线。
		// 令牌校验失败同样计入失败限流:两个入口共用同一把爆破闸。
		if key == "" {
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
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "Missing API key",
				"message": "缺少API密钥，请通过官方前端访问",
				"code":    "NO_API_KEY",
			})
			return
		}

		// 常量时间比较。远程时序攻击在网络抖动面前基本不可行,
		// 但同项目的 telegram/security.go 已经用了 ConstantTimeCompare,
		// 没有理由这里松一档。
		if subtle.ConstantTimeCompare([]byte(key), []byte(cfg.APIKey)) != 1 {
			// 记一次失败。不限次数的话,端口可达的人可以一秒几千次地猜 ——
			// 而这个服务能用你的 OVH 账户下单。
			recordAuthFailure(c.ClientIP(), time.Now())
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":   "Invalid API key",
				"message": "API 密钥无效。它在服务器的 .env 文件里(API_SECRET_KEY);连续错误多次后会被暂时锁定",
				"code":    "INVALID_API_KEY",
			})
			return
		}
		// 对了就清零:打错一次的正常用户不该被后面的请求连坐
		clearAuthFailures(c.ClientIP())

		// X-Request-Time 时间戳校验。
		//
		// 注意它提供的防护接近于零:头不存在就跳过、解析失败也跳过,
		// 而官方前端从来不发这个头(全项目 grep 零命中)。攻击者当然更不会发。
		// 保留只是为了兼容可能存在的外部调用方;真正的防护是 API Key 本身。
		// 不要因为它出现在 CORS AllowHeaders 里就以为这是一道有效的闸。
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
