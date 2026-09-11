package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
)

// ListServerAliases GET /api/server-control/aliases[?account=<id>]
// 返回该账户下所有服务器别名:`{ "<service_name>": "<alias>", ... }`。
// 账户为空 / 默认账户走 ovhAccountFor 的兜底,跟其它 server-control 端点一致。
func ListServerAliases(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		acc, ok := ovhAccountFor(state, c)
		if !ok {
			c.JSON(http.StatusPreconditionFailed, gin.H{"error": "未配置 OVH 账户", "code": "NO_OVH_ACCOUNT"})
			return
		}
		m, err := state.DB.ListAliasesByAccount(acc.ID)
		if err != nil {
			state.Logger.Error("list aliases: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if m == nil {
			m = map[string]string{}
		}
		c.JSON(http.StatusOK, m)
	}
}

// SetServerAlias PUT /api/server-control/:service_name/alias[?account=<id>]
// body: { "alias": "kele" }。alias 空串等于删除。
func SetServerAlias(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		acc, ok := ovhAccountFor(state, c)
		if !ok {
			c.JSON(http.StatusPreconditionFailed, gin.H{"error": "未配置 OVH 账户", "code": "NO_OVH_ACCOUNT"})
			return
		}
		svc := strings.TrimSpace(c.Param("service_name"))
		if svc == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 service_name"})
			return
		}
		var body struct {
			Alias string `json:"alias"`
		}
		_ = c.ShouldBindJSON(&body)
		alias := strings.TrimSpace(body.Alias)
		if err := state.DB.UpsertAlias(acc.ID, svc, alias); err != nil {
			state.Logger.Error("set alias: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "service_name": svc, "alias": alias})
	}
}

// DeleteServerAlias DELETE /api/server-control/:service_name/alias[?account=<id>]
func DeleteServerAlias(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		acc, ok := ovhAccountFor(state, c)
		if !ok {
			c.JSON(http.StatusPreconditionFailed, gin.H{"error": "未配置 OVH 账户", "code": "NO_OVH_ACCOUNT"})
			return
		}
		svc := strings.TrimSpace(c.Param("service_name"))
		if svc == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 service_name"})
			return
		}
		if err := state.DB.DeleteAlias(acc.ID, svc); err != nil {
			state.Logger.Error("delete alias: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	}
}
