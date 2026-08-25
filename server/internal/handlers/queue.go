package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/types"
)

// AddQueueItem POST /api/queue
// 多账户:body 必须带 account_id,后端用它确定下单走哪个账户
func AddQueueItem(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			AccountID     string   `json:"account_id"`
			PlanCode      string   `json:"planCode"`
			Datacenter    string   `json:"datacenter"`
			Options       []string `json:"options"`
			RetryInterval int      `json:"retryInterval"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.AccountID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "缺少 account_id"})
			return
		}
		if _, ok := state.FindAccount(body.AccountID); !ok {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "account_id 不存在"})
			return
		}
		body.PlanCode = strings.TrimSpace(body.PlanCode)
		body.Datacenter = strings.TrimSpace(body.Datacenter)
		if body.PlanCode == "" || body.Datacenter == "" {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "缺少 planCode 或 datacenter"})
			return
		}
		// 入队前挡住"这个账户根本买不到这台机器"的任务(跨区 / 非 Eco / planCode 不存在)。
		// 前端的下单对话框有独立的账户选择器(web/src/routes/servers.tsx:646),
		// 机型列表却是按另一个账户拉的 —— 拿欧区机型配美区账户是一键就能做出来的组合。
		// 而这种任务进了队列后:availabilities 返回 200 + 空数组 → PurchaseServer 判"无货"
		// → 按 retryInterval 永远重试,日志里永远只有一句"当前无货",用户看不出错在哪。
		// 所以在这里就说清楚,而不是让它在后台空转到天荒地老。
		// 探测失败(catalog.PlanVerdictUnknown)不拦 —— 一次网络瞬断不该让用户下不了单。
		if verdict, hint := catalog.ClassifyPlan(state, body.AccountID, body.PlanCode, "queue"); hint != "" {
			state.Logger.Warn(fmt.Sprintf("[queue] 拒绝任务(判定 %d): %s", verdict, hint), "queue")
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": hint})
			return
		}
		if body.RetryInterval == 0 {
			body.RetryInterval = 30
		}
		item := types.QueueItem{
			ID:            uuid.NewString(),
			AccountID:     body.AccountID,
			PlanCode:      body.PlanCode,
			Datacenter:    body.Datacenter,
			Options:       body.Options,
			Status:        "running",
			CreatedAt:     types.NowISO(),
			UpdatedAt:     types.NowISO(),
			RetryInterval: body.RetryInterval,
			RetryCount:    0,
			LastCheckTime: 0,
		}
		state.QueueMu.Lock()
		state.Queue = append(state.Queue, item)
		state.QueueMu.Unlock()
		_ = state.SaveQueue()
		state.Logger.Info("添加任务 "+item.ID+" ("+item.PlanCode+" 在 "+item.Datacenter+", 账户 "+body.AccountID+") 到队列并立即启动 (状态: running)", "")
		c.JSON(http.StatusOK, gin.H{"status": "success", "id": item.ID})
	}
}

// RemoveQueueItem DELETE /api/queue/:id
func RemoveQueueItem(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		state.DeletedTaskIDsMu.Lock()
		state.DeletedTaskIDs[id] = struct{}{}
		state.DeletedTaskIDsMu.Unlock()
		state.Logger.Info("标记任务 "+id+" 为删除，后台线程将立即停止处理", "system")

		state.QueueMu.Lock()
		var removed *types.QueueItem
		// 重新分配新 slice，避免 [:0] 与原 backing array 共享导致快照读到已被覆盖的元素
		kept := make([]types.QueueItem, 0, len(state.Queue))
		for i := range state.Queue {
			if state.Queue[i].ID == id {
				cp := state.Queue[i]
				removed = &cp
				continue
			}
			kept = append(kept, state.Queue[i])
		}
		state.Queue = kept
		state.QueueMu.Unlock()
		_ = state.SaveQueue()
		if removed != nil {
			state.Logger.Info("Removed "+removed.PlanCode+" from queue (ID: "+id+")", "system")
		}
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	}
}

// ClearQueue DELETE /api/queue/clear
func ClearQueue(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.QueueMu.Lock()
		count := len(state.Queue)
		state.DeletedTaskIDsMu.Lock()
		for _, it := range state.Queue {
			state.DeletedTaskIDs[it.ID] = struct{}{}
		}
		state.DeletedTaskIDsMu.Unlock()
		state.Queue = []types.QueueItem{}
		state.QueueMu.Unlock()
		_ = state.SaveQueue()
		state.Logger.Info("Cleared all queue items ("+strconv.Itoa(count)+" items removed)", "")
		c.JSON(http.StatusOK, gin.H{"status": "success", "count": count})
	}
}

// UpdateQueueStatus PUT /api/queue/:id/status
func UpdateQueueStatus(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var body struct {
			Status string `json:"status"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Status == "" {
			body.Status = "pending"
		}
		state.QueueMu.Lock()
		for i := range state.Queue {
			if state.Queue[i].ID == id {
				state.Queue[i].Status = body.Status
				state.Queue[i].UpdatedAt = types.NowISO()
				state.Logger.Info("Updated "+state.Queue[i].PlanCode+" status to "+body.Status, "")
				break
			}
		}
		state.QueueMu.Unlock()
		_ = state.SaveQueue()
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	}
}

// ClearPurchaseHistory DELETE /api/purchase-history
func ClearPurchaseHistory(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.HistoryMu.Lock()
		state.History = state.History[:0]
		state.HistoryMu.Unlock()
		_ = state.SaveHistory()
		state.Logger.Info("Purchase history cleared", "")
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	}
}
