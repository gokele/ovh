package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

// queueOrderView 保留原任务字段,附加成功订单编号;不依赖队列数据库额外列。
type queueOrderView struct {
	types.QueueItem
	OrderID string `json:"orderId,omitempty"`
}

// GetQueue GET /api/queue
func GetQueue(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.QueueMu.Lock()
		// 返回副本，确保为空时序列化为 [] 而不是 null
		cp := make([]types.QueueItem, len(state.Queue))
		copy(cp, state.Queue)
		state.QueueMu.Unlock()
		orders := make(map[string]string)
		state.HistoryMu.Lock()
		for _, h := range state.History {
			if h.Status == "success" && h.OrderID != "" {
				orders[h.TaskID] = h.OrderID
			}
		}
		state.HistoryMu.Unlock()
		view := make([]queueOrderView, len(cp))
		for i, item := range cp {
			view[i].QueueItem = item
			if item.Status == "completed" {
				view[i].OrderID = orders[item.ID]
			}
		}
		c.JSON(http.StatusOK, view)
	}
}

// GetPurchaseHistory GET /api/purchase-history
func GetPurchaseHistory(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.HistoryMu.Lock()
		cp := make([]types.PurchaseHistoryEntry, len(state.History))
		copy(cp, state.History)
		state.HistoryMu.Unlock()
		c.JSON(http.StatusOK, cp)
	}
}
