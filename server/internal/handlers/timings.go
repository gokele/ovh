package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/purchase"
)

// GetPurchaseTimings GET /api/queue/timings
//
// 每条抢购链路(机型@机房)最近一次的阶段耗时。
// 抢购输了之后用户唯一能问的问题是"我慢在哪一步" —— 这个接口给的就是那串数字。
// 只留最近一次:一晚上跑几万轮,全存下来是往数据库里灌日志,而真正值得回看的
// 只有"最后那次卡在哪"。
func GetPurchaseTimings(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"timings": purchase.LastTimings()})
	}
}
