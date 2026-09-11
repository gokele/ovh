package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/purchase"
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
			// AutoPay 下单成功后用默认支付方式自动付款(显式开关,默认关)
			AutoPay bool `json:"autoPay"`
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
		// 没给 / 给 0 = 用全局默认(设置页可改);超出区间夹回来
		body.RetryInterval = types.ClampRetryInterval(body.RetryInterval, state.Config.RetryInterval())
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
			AutoPay:       body.AutoPay,
		}
		// 入队 + 落库是一件事:EnqueueItems 失败会把这条从内存撤回,
		// 不留"这次能跑但重启就丢"的半成功任务
		if err := state.EnqueueItems([]types.QueueItem{item}, false); err != nil {
			state.Logger.Error("添加任务后保存队列失败,已撤回: "+err.Error(), "queue")
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "任务没能写进数据库，已撤回：" + err.Error(),
			})
			return
		}
		state.Logger.Info("添加任务 "+item.ID+" ("+item.PlanCode+" 在 "+item.Datacenter+", 账户 "+body.AccountID+") 到队列并立即启动 (状态: running)", "")
		// 不再有 warning 分支:落库要么成功、要么整条撤回并报错,没有中间态
		c.JSON(http.StatusOK, gin.H{"status": "success", "id": item.ID})
	}
}

// RemoveQueueItem DELETE /api/queue/:id
func RemoveQueueItem(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		// MarkTaskDeleted 除了打标记,还会取消这条任务正在进行的下单(如果它正跑在
		// PurchaseServer 里)。以前只打标记,处理器要到下一轮才看得见,这一轮照跑到结账。
		state.MarkTaskDeleted(id)
		state.Logger.Info("标记任务 "+id+" 为删除，正在进行的下单已取消，后台线程将停止处理", "system")

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
		if err := state.SaveQueue(); err != nil {
			// 删除没落库 → 重启后这条任务会"复活"并继续抢。必须告诉用户。
			state.Logger.Error("删除任务后保存队列失败: "+err.Error(), "queue")
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "已从运行中的队列移除，但没能写进数据库，重启后这条任务会重新出现：" + err.Error(),
			})
			return
		}
		if removed != nil {
			state.Logger.Info("Removed "+removed.PlanCode+" from queue (ID: "+id+")", "system")
		}
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	}
}

// UpdateQueueInterval PUT /api/queue/:id/interval  body: { "retryInterval": 秒 }
// 改一条正在跑的任务的重试间隔。处理器每轮都读 item 上的值,所以改完下一轮就生效。
func UpdateQueueInterval(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var body struct {
			RetryInterval int `json:"retryInterval"`
		}
		if err := c.ShouldBindJSON(&body); err != nil ||
			body.RetryInterval < types.MinRetryInterval || body.RetryInterval > types.MaxRetryInterval {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error",
				"error": fmt.Sprintf("重试间隔必须是 %d ~ %d 之间的整数秒", types.MinRetryInterval, types.MaxRetryInterval)})
			return
		}
		found := false
		state.QueueMu.Lock()
		for i := range state.Queue {
			if state.Queue[i].ID == id {
				state.Queue[i].RetryInterval = body.RetryInterval
				state.Queue[i].UpdatedAt = types.NowISO()
				found = true
				break
			}
		}
		state.QueueMu.Unlock()
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "error": "任务不存在"})
			return
		}
		if err := state.SaveQueue(); err != nil {
			state.Logger.Error("改任务间隔后保存队列失败: "+err.Error(), "queue")
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "间隔已在本次运行中改掉，但没能写进数据库，重启后会回到原值：" + err.Error(),
			})
			return
		}
		state.Logger.Info(fmt.Sprintf("任务 %s 重试间隔改为 %d 秒", id, body.RetryInterval), "queue")
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	}
}

// ClearQueue DELETE /api/queue/clear
func ClearQueue(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.QueueMu.Lock()
		count := len(state.Queue)
		for _, it := range state.Queue {
			state.MarkTaskDeleted(it.ID) // 同时取消正在进行的下单
		}
		state.Queue = []types.QueueItem{}
		state.QueueMu.Unlock()
		if err := state.SaveQueue(); err != nil {
			state.Logger.Error("清空队列后保存失败: "+err.Error(), "queue")
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "已清空运行中的队列，但没能写进数据库，重启后这些任务会重新出现：" + err.Error(),
			})
			return
		}
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
		if err := state.SaveQueue(); err != nil {
			state.Logger.Error("改任务状态后保存队列失败: "+err.Error(), "queue")
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "状态已在本次运行中改掉，但没能写进数据库，重启后会回到原状态：" + err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	}
}

// RefreshOrderStatuses POST /api/purchase-history/refresh-status
//
// 手动刷新所有未到终态订单的支付状态(GET /me/order/{id}/status)。
// 后台每 10 分钟也会自动刷,这里是给"我刚付完款想马上看到"的场景。
func RefreshOrderStatuses(state *app.State) gin.HandlerFunc {
	// 手动刷新会对每条未终态订单各打一次 /me/order/{id},而 force=true 正是用来
	// 跳过那个 2 分钟节流的 —— 等于把限流闸门交给用户的手速。
	// OVH 对 /me 命名空间有自己的限流,打多了返回 429,而抢购主链路
	// (查库存 / 建车 / 结账)跟它共用同一个账户配额:刷历史把配额刷没了,
	// 补货那一刻就抢不到。所以入口这层必须有自己的节流。
	var (
		mu       sync.Mutex
		lastCall time.Time
	)
	const minInterval = 15 * time.Second

	return func(c *gin.Context) {
		mu.Lock()
		if wait := minInterval - time.Since(lastCall); !lastCall.IsZero() && wait > 0 {
			mu.Unlock()
			c.JSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"error": fmt.Sprintf("刷新太频繁,请等 %d 秒。手动刷新会对每条未完成订单各查一次 OVH,"+
					"把账户配额刷光会影响正在跑的抢购。", int(wait.Seconds())+1),
				"retryAfterSeconds": int(wait.Seconds()) + 1,
			})
			return
		}
		lastCall = time.Now()
		mu.Unlock()

		n := purchase.RefreshOrderStatuses(state, true)
		c.JSON(http.StatusOK, gin.H{"success": true, "updated": n})
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
