package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/vps"
)

// GetVPSModels GET /api/vps-monitor/models?subsidiary=IE
//
// 当前还在售的 VPS 型号,来自 OVH 公开目录(不带凭据、不占账户配额)。
//
// 为什么不写死在前端:型号会整代下架。实测 vps-2025 全线已经退出下单漏斗,
// 而写死的下拉框里只有它 —— 订阅一个停售型号,库存接口老实返回"全部无货",
// 永远不跳变也就永远不通知,症状和"这机器确实抢手"一模一样。
func GetVPSModels(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		sub := c.Query("subsidiary")
		if sub == "" {
			// 不写死 IE:跟着当前账户所在站点走
			sub = vps.DefaultSubsidiary(state, c.Query("accountId"))
		}
		models, err := vps.Models(sub)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{
				"status":  "error",
				"message": err.Error(),
				"models":  []vps.Model{},
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"subsidiary": sub, "models": models})
	}
}
