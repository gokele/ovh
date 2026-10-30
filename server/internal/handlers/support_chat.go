package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
)

// OfficialChatSession 使用选定账户申请一次性令牌,仅由用户主动打开窗口时调用。
// 令牌不落库、不写日志、不缓存;消息发送和会话身份展示由官方窗口负责。
func OfficialChatSession(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, private")
		c.Header("Pragma", "no-cache")
		var body struct {
			Product string `json:"product"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || (body.Product != "dedicated" && body.Product != "vps") {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "CHAT_INVALID_PRODUCT", "error": "请选择独服或 VPS 客服"})
			return
		}
		acc, client, err := supportClient(state, c)
		if err != nil {
			return
		}
		var me struct {
			Nichandle    string `json:"nichandle"`
			CustomerCode string `json:"customerCode"`
		}
		if err = client.GetWithContext(c.Request.Context(), "/me", &me); err != nil {
			chatFailure(c, err)
			return
		}
		customer := me.Nichandle
		if customer == "" {
			customer = me.CustomerCode
		}
		if customer == "" {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "code": "CHAT_IDENTITY_MISSING", "error": "OVH 未返回客户编号,未申请聊天令牌"})
			return
		}
		var auth struct {
			Token string `json:"token"`
		}
		if err = client.PostWithContext(c.Request.Context(), "/auth/token", nil, &auth); err != nil {
			chatFailure(c, err)
			return
		}
		if strings.TrimSpace(auth.Token) == "" || len(auth.Token) > 8192 {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "code": "CHAT_TOKEN_INVALID", "error": "OVH 未返回有效聊天令牌"})
			return
		}
		// 与官方 Manager 的 generateSnowChatUrl 参数一致;使用窗口导航,遵守官方 frame-ancestors 策略。
		values := url.Values{
			"sysparm_session_id": {auth.Token}, "sysparm_queue": {body.Product},
			"sysparm_region": {ovh.EndpointRegion(acc.Endpoint)}, "sysparm_language": {"en"},
			"sysparm_requester_session_language": {"en"}, "sysparm_branding_key": {"adrielly"},
			"sysparm_live_agent_only": {"false"}, "sysparm_skip_load_history": {"false"},
		}
		c.JSON(http.StatusOK, gin.H{"accountId": acc.ID, "accountName": acc.Name, "customerCode": customer,
			"launchUrl": "https://agent-chat.ovhcloud.com/sn_va_web_client_app_embed.do?" + values.Encode()})
	}
}

// chatFailure 保留认证/权限错误,不会把聊天令牌失败描述成工单已发送。
func chatFailure(c *gin.Context, err error) {
	status, payload := supportErrorResponse(err, false)
	payload["code"] = "CHAT_REQUEST_FAILED"
	payload["error"] = "无法准备官方聊天认证,请检查 GET /me、POST /auth/token 权限及账户状态"
	// 认证接口的原始错误可能携带敏感响应内容,仅返回状态码。
	payload["params"] = gin.H{"detail": fmt.Sprintf("HTTP %d", status)}
	c.JSON(status, payload)
}
