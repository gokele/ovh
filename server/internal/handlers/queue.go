package handlers

import (
	"errors"
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
	"github.com/ovh-buy/server/internal/orderterm"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/purchase"
	"github.com/ovh-buy/server/internal/types"
)

// AddQueueItem POST /api/queue
// 多账户:body 必须带 account_id,后端用它确定下单走哪个账户
func AddQueueItem(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Duration  string `json:"duration"`
			AccountID string `json:"account_id"`
			// AccountIDs 多账户一起抢同一单:任一账户抢到都计入同一份数量上限,台数不会按账户翻倍。
			AccountIDs    []string `json:"account_ids"`
			PlanCode      string   `json:"planCode"`
			Datacenter    string   `json:"datacenter"`
			Datacenters   []string `json:"datacenters"`
			Quantity      *int     `json:"quantity"`
			PurchaseMode  string   `json:"purchaseMode"`
			Options       []string `json:"options"`
			RetryInterval int      `json:"retryInterval"`
			// LegacyAutoPay 旧页面字段,仅为兼容;true 会被拒绝(自动付款功能已删除)
			LegacyAutoPay *bool `json:"autoPay"`
			Preorder      bool  `json:"preorder"`
			// PreorderWaitStock 预售等库存出现再结账,默认开售即下
			PreorderWaitStock bool   `json:"preorderWaitStock"`
			Coupon            string `json:"coupon"`
		}
		if err := bindOrderJSON(c, &body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "任务参数格式不正确", "details": err.Error()})
			return
		}
		if rejectAutoPay(c, body.LegacyAutoPay) {
			return
		}
		duration, termErr := orderterm.Normalize(body.Duration)
		if termErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": termErr.Error()})
			return
		}
		body.Duration = duration

		accounts, accErr := queueAccounts(state, body.AccountID, body.AccountIDs)
		if accErr != nil {
			c.JSON(http.StatusBadRequest, accErr)
			return
		}
		body.AccountID = accounts[0]
		body.PlanCode = strings.TrimSpace(body.PlanCode)
		body.Datacenter = strings.TrimSpace(body.Datacenter)
		if len(body.Datacenters) == 0 && body.Datacenter != "" {
			body.Datacenters = []string{body.Datacenter}
		} else if body.Datacenter != "" {
			c.JSON(400, gin.H{"error": "datacenter 与 datacenters 不能同时填写"})
			return
		}
		qty := 1
		if body.Quantity != nil {
			qty = *body.Quantity
		}
		if body.PlanCode == "" || qty < 1 || qty > types.MaxOrderQuantity || len(body.Datacenters) == 0 || len(accounts)*len(body.Datacenters)*qty > types.MaxOrderFanout || (body.PurchaseMode != "" && body.PurchaseMode != "perDatacenter" && body.PurchaseMode != "total") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请填写型号、机房及有效数量和购买方式"})
			return
		}
		seen := map[string]bool{}
		for i, dc := range body.Datacenters {
			dc = ovh.ConvertDisplayDCToAPIDC(strings.TrimSpace(dc))
			if dc == "" || seen[dc] {
				c.JSON(400, gin.H{"error": "机房不能为空或重复"})
				return
			}
			seen[dc] = true
			body.Datacenters[i] = dc
		}

		// 入队前挡住"这个账户根本买不到这台机器"的任务(跨区 / 非 Eco / planCode 不存在)。
		// 前端的下单对话框有独立的账户选择器(web/src/routes/servers.tsx:646),
		// 机型列表却是按另一个账户拉的 —— 拿欧区机型配美区账户是一键就能做出来的组合。
		// 而这种任务进了队列后:availabilities 返回 200 + 空数组 → PurchaseServer 判"无货"
		// → 按 retryInterval 永远重试,日志里永远只有一句"当前无货",用户看不出错在哪。
		// 所以在这里就说清楚,而不是让它在后台空转到天荒地老。
		// 探测失败(catalog.PlanVerdictUnknown)不拦 —— 一次网络瞬断不该让用户下不了单。
		for _, account := range accounts {
			if verdict, hint := catalog.ClassifyPlan(state, account, body.PlanCode, "queue"); hint != "" && !(body.Preorder && verdict == catalog.PlanVerdictPrerelease) {
				state.Logger.Warn(fmt.Sprintf("[queue] 拒绝任务(判定 %d): %s", verdict, hint), "queue")
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": hint, "account_id": account})
				return
			}
		}
		// 没给 / 给 0 = 用全局默认(设置页可改);超出区间夹回来
		body.RetryInterval = types.ClampRetryInterval(body.RetryInterval, state.Config.RetryInterval())
		items := make([]types.QueueItem, 0, len(accounts)*len(body.Datacenters)*qty)
		ids := make([]string, 0, cap(items))
		group := ""
		if body.PurchaseMode == "total" {
			group = uuid.NewString()
		}
		for _, dc := range body.Datacenters {
			dcGroup := group
			if dcGroup == "" && len(accounts) > 1 {
				// 按机房下单 + 多账户:每个机房一份上限,多个账户只是多几条抢的通道。
				dcGroup = uuid.NewString()
			}
			for _, account := range accounts {
				for n := 0; n < qty; n++ {
					item := types.QueueItem{ID: uuid.NewString(), AccountID: account, Duration: body.Duration, PlanCode: body.PlanCode, Datacenter: dc,
						Options: append([]string(nil), body.Options...), Status: "running", CreatedAt: types.NowISO(), UpdatedAt: types.NowISO(), RetryInterval: body.RetryInterval,
						Preorder: body.Preorder, PreorderWaitStock: body.Preorder && body.PreorderWaitStock, Coupon: strings.TrimSpace(body.Coupon), OrderGroup: dcGroup}
					if dcGroup != "" {
						item.GroupLimit = qty
					}
					items = append(items, item)
					ids = append(ids, item.ID)
				}
			}
		}

		// 入队 + 落库是一件事:EnqueueItems 失败会把这条从内存撤回,
		// 不留"这次能跑但重启就丢"的半成功任务
		if err := state.EnqueueItems(items, false); err != nil {
			state.Logger.Error("添加任务后保存队列失败,已撤回: "+err.Error(), "queue")
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "任务没能写进数据库，已撤回：" + err.Error(),
			})
			return
		}
		state.Logger.Info(fmt.Sprintf("添加 %d 个 %s 抢购任务（账户 %s，总量限制 %s）", len(items), body.PlanCode, strings.Join(accounts, ","), body.PurchaseMode), "queue")
		c.JSON(http.StatusOK, gin.H{"status": "success", "id": ids[0], "ids": ids, "count": len(ids)})
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
		var blockErr error
		// 重新分配新 slice，避免 [:0] 与原 backing array 共享导致快照读到已被覆盖的元素
		kept := make([]types.QueueItem, 0, len(state.Queue))
		for i := range state.Queue {
			if state.Queue[i].ID == id {
				cp := state.Queue[i]
				removed = &cp
				blockErr = state.SetAutomaticOrderBlockedLocked(cp, true)
				continue
			}
			kept = append(kept, state.Queue[i])
		}
		state.Queue = kept
		state.QueueMu.Unlock()
		if err := errors.Join(blockErr, state.SaveQueue()); err != nil {
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

// UpdateQueueItem PUT /api/queue/:id,一个任务仍对应一台服务器。
func UpdateQueueItem(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Duration      *string  `json:"duration"`
			Datacenter    string   `json:"datacenter"`
			Options       []string `json:"options"`
			RetryInterval int      `json:"retryInterval"`
			// LegacyAutoPay 旧页面字段,仅为兼容;true 会被拒绝(自动付款功能已删除)
			LegacyAutoPay *bool  `json:"autoPay"`
			Coupon        string `json:"coupon"`
		}
		if err := bindOrderJSON(c, &body); err != nil || strings.TrimSpace(body.Datacenter) == "" || body.RetryInterval < types.MinRetryInterval || body.RetryInterval > types.MaxRetryInterval {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "请填写机房及有效的重试间隔"})
			return
		}
		if rejectAutoPay(c, body.LegacyAutoPay) {
			return
		}
		if body.Duration != nil {
			if _, err := orderterm.Normalize(*body.Duration); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}
		if err := state.UpdateQueueItem(c.Param("id"), strings.TrimSpace(body.Datacenter), body.Options, body.RetryInterval, strings.TrimSpace(body.Coupon), body.Duration); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, app.ErrQueueNotFound) {
				status = http.StatusNotFound
			} else if errors.Is(err, app.ErrQueueNotEditable) {
				status = http.StatusConflict
			}
			c.JSON(status, gin.H{"status": "error", "error": err.Error()})
			return
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
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "error": "任务不存在", "code": "E10FEEC40"})
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
		var blockErr error
		for _, it := range state.Queue {
			blockErr = errors.Join(blockErr, state.SetAutomaticOrderBlockedLocked(it, true))
			state.MarkTaskDeleted(it.ID) // 同时取消正在进行的下单
		}
		state.Queue = []types.QueueItem{}
		state.QueueMu.Unlock()
		if err := errors.Join(blockErr, state.SaveQueue()); err != nil {
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
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "任务状态参数格式不正确"})
			return
		}
		switch body.Status {
		case "pending", "running", "paused", "completed", "failed":
		default:
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "无效的任务状态"})
			return
		}
		found := false
		var blockErr error
		if body.Status == "running" || body.Status == "pending" {
			if err := state.RestoreCompletedQueue(); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "无法核对已下单记录,未恢复任务: " + err.Error()})
				return
			}
			if err := state.ResumeQueueItem(id, body.Status); err != nil {
				switch {
				case errors.Is(err, app.ErrQueueAlreadyOrdered):
					c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "QUEUE_ALREADY_ORDERED"})
				case errors.Is(err, app.ErrQueueNotFound):
					c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "E10FEEC40"})
				default:
					c.JSON(http.StatusInternalServerError, gin.H{"error": "恢复失败,任务保持原状态: " + err.Error()})
				}
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "success"})
			return
		}
		state.QueueMu.Lock()
		for i := range state.Queue {
			if state.Queue[i].ID == id {
				if state.Queue[i].Status == "completed" && body.Status != "completed" {
					state.QueueMu.Unlock()
					c.JSON(http.StatusConflict, gin.H{"error": "该任务已下单,不能再次启动。如需再买,请新建任务。", "code": "QUEUE_ALREADY_ORDERED"})
					return
				}
				blockErr = state.SetAutomaticOrderBlockedLocked(state.Queue[i], body.Status != "running" && body.Status != "pending")
				state.Queue[i].Status = body.Status
				state.Queue[i].UpdatedAt = types.NowISO()
				if body.Status != "running" {
					state.UnregisterTaskCancel(id)
				}
				found = true
				state.Logger.Info("Updated "+state.Queue[i].PlanCode+" status to "+body.Status, "")
				break
			}
		}
		state.QueueMu.Unlock()
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "error": "任务不存在", "code": "E10FEEC40"})
			return
		}
		if err := errors.Join(blockErr, state.SaveQueue()); err != nil {
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
		// 落库失败必须报出去。上面的 ClearQueue 就是这么写的,只有这里
		// 用 `_ =` 把错吞了 —— 表现是前端弹"已清空"、库里其实一条没删,
		// 重启后整个购买历史原样复活。用户会以为是程序把删掉的记录找回来了。
		state.HistoryMu.Lock()
		backup := make([]types.PurchaseHistoryEntry, len(state.History))
		copy(backup, state.History)
		state.History = []types.PurchaseHistoryEntry{}
		state.HistoryMu.Unlock()

		if err := state.SaveHistory(); err != nil {
			// 内存回滚,让两边保持一致 —— 否则界面是空的、库里是满的,
			// 下一次任何写历史的操作都会把整表按"空"覆盖回去。
			// 回滚要保留这中间新落的记录(下单完成会 append),所以是拼接不是覆盖。
			state.HistoryMu.Lock()
			state.History = append(backup, state.History...)
			state.HistoryMu.Unlock()
			state.Logger.Error("清空购买历史失败: "+err.Error(), "queue")
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "没能写进数据库，购买历史未被清空：" + err.Error(),
			})
			return
		}
		state.Logger.Info("Purchase history cleared", "")
		c.JSON(http.StatusOK, gin.H{"status": "success"})
	}
}

// queueAccounts 解析下单账户:兼容旧的单个 account_id,也接受多账户 account_ids。
// 返回去重后的实际账户 ID,第一个是主账户;任一账户不存在都整单拒绝。
func queueAccounts(state *app.State, single string, many []string) ([]string, gin.H) {
	refs := append([]string(nil), many...)
	if len(refs) == 0 && single != "" {
		refs = []string{single}
	}
	if len(refs) == 0 {
		return nil, gin.H{"status": "error", "error": "缺少 account_id", "code": "E34CBF1D4"}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		acc, ok := state.FindAccount(strings.TrimSpace(ref))
		if !ok || strings.TrimSpace(ref) == "" {
			return nil, gin.H{"status": "error", "error": "account_id 不存在", "code": "E7B315B13", "account_id": ref}
		}
		if seen[acc.ID] {
			continue
		}
		seen[acc.ID] = true
		out = append(out, acc.ID)
	}
	return out, nil
}
