package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

// GetQueue GET /api/queue
func GetQueue(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.QueueMu.Lock()
		// 返回副本，确保为空时序列化为 [] 而不是 null
		cp := make([]types.QueueItem, len(state.Queue))
		copy(cp, state.Queue)
		state.QueueMu.Unlock()
		c.JSON(http.StatusOK, cp)
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
