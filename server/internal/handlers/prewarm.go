package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/orderterm"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

func PrepareOrderCart(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Duration    string   `json:"duration"`
			ID          string   `json:"id"`
			AccountID   string   `json:"account_id"`
			PlanCode    string   `json:"planCode"`
			Datacenters []string `json:"datacenters"`
			Options     []string `json:"options"`
			Quantity    int      `json:"quantity"`
			// LegacyAutoPay 旧页面字段,仅为兼容;true 会被拒绝(自动付款功能已删除)
			LegacyAutoPay *bool  `json:"autoPay"`
			Coupon        string `json:"coupon"`
			Preorder      bool   `json:"preorder"`
			Standby       bool   `json:"standby"`
		}
		if err := bindOrderJSON(c, &body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "备车参数不完整或数量超限", "code": "PREWARM_INVALID", "details": err.Error()})
			return
		}
		if rejectAutoPay(c, body.LegacyAutoPay) {
			return
		}
		if !validPrewarmID(body.ID) || body.AccountID == "" || strings.TrimSpace(body.PlanCode) == "" || len(body.PlanCode) > 200 || len(body.Datacenters) == 0 || body.Quantity < 1 || body.Quantity > types.MaxOrderQuantity || len(body.Datacenters)*body.Quantity > types.MaxOrderFanout {
			c.JSON(http.StatusBadRequest, gin.H{"error": "备车参数不完整或数量超限", "code": "PREWARM_INVALID"})
			return
		}
		duration, termErr := orderterm.Normalize(body.Duration)
		if termErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": termErr.Error()})
			return
		}
		body.Duration = duration

		if state.OrderPrewarm == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "备车功能尚未初始化", "code": "PREWARM_UNAVAILABLE"})
			return
		}
		items := make([]types.QueueItem, 0, len(body.Datacenters)*body.Quantity)
		seen := map[string]bool{}
		for _, dc := range body.Datacenters {
			dc = ovh.ConvertDisplayDCToAPIDC(strings.TrimSpace(dc))
			if strings.TrimSpace(dc) == "" || seen[dc] {
				c.JSON(http.StatusBadRequest, gin.H{"error": "机房不能为空或重复", "code": "PREWARM_INVALID"})
				return
			}
			seen[dc] = true
			for n := 0; n < body.Quantity; n++ {
				items = append(items, types.QueueItem{AccountID: body.AccountID, Duration: body.Duration, PlanCode: strings.TrimSpace(body.PlanCode), Datacenter: dc, Options: body.Options, Coupon: strings.TrimSpace(body.Coupon), Preorder: body.Preorder})
			}
		}
		lease := 2 * time.Minute
		if body.Standby {
			lease = 45 * time.Minute
		}
		owner := "web:" + body.ID
		if err := state.OrderPrewarm.Prepare(owner, items, lease, nil); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "PREWARM_UNAVAILABLE"})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"items": state.OrderPrewarm.Status(owner)})
	}
}

func validPrewarmID(id string) bool {
	if len(id) < 8 || len(id) > 100 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func GetOrderPrewarm(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		if state.OrderPrewarm == nil {
			c.JSON(http.StatusOK, gin.H{"items": []app.PrewarmStatus{}})
			return
		}
		owner := c.Query("id")
		if owner != "" {
			owner = "web:" + owner
		}
		c.JSON(http.StatusOK, gin.H{"items": state.OrderPrewarm.Status(owner)})
	}
}

func CancelOrderPrewarm(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		if state.OrderPrewarm != nil {
			state.OrderPrewarm.Cancel("web:" + c.Param("id"))
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}
