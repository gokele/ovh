package auth

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// SessionHeader 浏览器登录后每个请求带的会话令牌头。
const SessionHeader = "X-Auth-Token"

type credentialsBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
	SetupKey string `json:"setupKey"`
}

func bindCreds(c *gin.Context) (credentialsBody, bool) {
	var body credentialsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "AUTH_BAD_REQUEST", "error": "请求格式不正确"})
		return body, false
	}
	return body, true
}

// rateLimited 登录 / 初始化和中间件共用一套按 IP 的失败计数。
func rateLimited(c *gin.Context) bool {
	if blocked, wait := authBlocked(c.ClientIP(), time.Now()); blocked {
		secs := int(wait.Seconds()) + 1
		c.Header("Retry-After", strconv.Itoa(secs))
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"success":    false,
			"code":       "AUTH_RATE_LIMITED",
			"error":      "Too many failed attempts",
			"message":    "连续错误次数过多,请约 " + strconv.Itoa(secs) + " 秒后再试",
			"retryAfter": secs,
		})
		return true
	}
	return false
}

func validationError(c *gin.Context, err error) bool {
	code := ""
	switch {
	case errors.Is(err, ErrInvalidUsername):
		code = "AUTH_INVALID_USERNAME"
	case errors.Is(err, ErrPasswordTooShort):
		code = "AUTH_PASSWORD_TOO_SHORT"
	case errors.Is(err, ErrPasswordTooLong):
		code = "AUTH_PASSWORD_TOO_LONG"
	default:
		return false
	}
	c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": code, "error": err.Error()})
	return true
}

// RegisterRoutes 挂载 /api/auth/*。enabled=false(ENABLE_API_KEY_AUTH=false)时
// status 告诉前端不需要登录,其余端点照常可用但没有意义。
func RegisterRoutes(api *gin.RouterGroup, a *Accounts, enabled bool) {
	g := api.Group("/auth")

	g.GET("/status", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		setup := a.SetupRequired()
		c.JSON(http.StatusOK, gin.H{
			"success":       true,
			"authEnabled":   enabled,
			"setupRequired": enabled && setup,
		})
	})

	g.POST("/setup", func(c *gin.Context) {
		if rateLimited(c) {
			return
		}
		body, ok := bindCreds(c)
		if !ok {
			return
		}
		token, err := a.Setup(body.SetupKey, body.Username, body.Password, c.Request.UserAgent())
		switch {
		case err == nil:
			clearAuthFailures(c.ClientIP())
			c.JSON(http.StatusOK, gin.H{"success": true, "token": token, "username": a.Username()})
		case errors.Is(err, ErrSetupDone):
			c.JSON(http.StatusConflict, gin.H{"success": false, "code": "AUTH_SETUP_DONE", "error": err.Error()})
		case errors.Is(err, ErrBadSetupKey):
			recordAuthFailure(c.ClientIP(), time.Now())
			c.JSON(http.StatusForbidden, gin.H{"success": false, "code": "AUTH_BAD_SETUP_KEY", "error": err.Error()})
		case validationError(c, err):
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "code": "AUTH_INTERNAL", "error": "保存账号失败"})
		}
	})

	g.POST("/login", func(c *gin.Context) {
		if rateLimited(c) {
			return
		}
		body, ok := bindCreds(c)
		if !ok {
			return
		}
		if a.SetupRequired() {
			c.JSON(http.StatusConflict, gin.H{"success": false, "code": "SETUP_REQUIRED", "error": "还没有创建管理员账号"})
			return
		}
		token, err := a.Login(body.Username, body.Password, c.Request.UserAgent())
		if err != nil {
			if errors.Is(err, ErrBadCredentials) {
				recordAuthFailure(c.ClientIP(), time.Now())
				c.JSON(http.StatusForbidden, gin.H{"success": false, "code": "AUTH_BAD_CREDENTIALS", "error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "code": "AUTH_INTERNAL", "error": "登录失败"})
			return
		}
		clearAuthFailures(c.ClientIP())
		c.JSON(http.StatusOK, gin.H{"success": true, "token": token, "username": a.Username()})
	})

	g.GET("/me", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true, "username": a.Username(), "authEnabled": enabled})
	})

	g.POST("/logout", func(c *gin.Context) {
		a.Logout(strings.TrimSpace(c.GetHeader(SessionHeader)))
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	g.POST("/password", func(c *gin.Context) {
		var body struct {
			Current string `json:"currentPassword"`
			New     string `json:"newPassword"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "AUTH_BAD_REQUEST", "error": "请求格式不正确"})
			return
		}
		if rateLimited(c) {
			return
		}
		err := a.ChangePassword(strings.TrimSpace(c.GetHeader(SessionHeader)), body.Current, body.New)
		switch {
		case err == nil:
			c.JSON(http.StatusOK, gin.H{"success": true})
		case errors.Is(err, ErrBadCredentials):
			recordAuthFailure(c.ClientIP(), time.Now())
			c.JSON(http.StatusForbidden, gin.H{"success": false, "code": "AUTH_BAD_CURRENT_PASSWORD", "error": "当前密码不正确"})
		case validationError(c, err):
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "code": "AUTH_INTERNAL", "error": "修改密码失败"})
		}
	})
}
