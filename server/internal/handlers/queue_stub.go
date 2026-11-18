package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

// queueOrderView 保留原任务字段,附加成功订单编号;不依赖队列数据库额外列。
type queueOrderView struct {
	types.QueueItem
	OrderID string `json:"orderId,omitempty"`
	// PlanName 商品名(如 KS-LE-6),取自该任务所属账户的目录缓存;缓存里没有就留空,不为它请求 OVH
	PlanName string `json:"planName,omitempty"`
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
		// 每个账户视角的目录缓存只取一次(GetBucket 返回副本)
		names := map[string]map[string]string{}
		planName := func(accountID, planCode string) string {
			key := state.ServerCacheKey(accountID)
			m, ok := names[key]
			if !ok {
				m = map[string]string{}
				if state.ServerCache != nil {
					plans, _ := state.ServerCache.GetBucket(key)
					for _, p := range plans {
						// 目录里的名字形如 "ADVANCE-1 | AMD EPYC 4244P",只取商品名
						n := strings.TrimSpace(strings.SplitN(p.Name, "|", 2)[0])
						if n != "" && n != p.PlanCode {
							m[p.PlanCode] = n
						}
					}
				}
				names[key] = m
			}
			return m[planCode]
		}
		view := make([]queueOrderView, len(cp))
		for i, item := range cp {
			view[i].QueueItem = item
			view[i].PlanName = planName(item.AccountID, item.PlanCode)
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
