package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

// planVerdict 机型归属判定。
//
// 上一版把"不在 eco 目录"一律归因成"跨区 planCode",这是错的:eco 目录只回答
// "本工具能不能用 /order/cart/{id}/eco 下单它",不回答"它属于哪个大区"。
// 实测(公开接口,2026-08):
//
//	EU 站点 availabilities 有 244 个 planCode,eco 目录只有 99 个 —— 145 个差集里
//	整条 Scale / HCI / SDS / High-Grade 产品线都在(23scaleamd*/21hcisap*/...)
//	US 站点 availabilities 423 个,eco 目录 143 个,差集 282 个
//
// 也就是说欧区账户下欧区的 Scale 机型,旧逻辑会甩出一句"请改用本区的 planCode",
// 完全是错误诊断。现在分三种情况给三种文案,判据换成 catalog.RegionOfPlan
// (走公开可用性接口,与主控口径一致)。
type planVerdict int

const (
	planVerdictOK          planVerdict = iota // 属于本区,而且本工具下得了单
	planVerdictUnknown                        // 探测/目录失败 —— 不下结论,别拿一次瞬断把任务判死
	planVerdictCrossRegion                    // ① 真跨区:别的大区才有它的库存记录
	planVerdictNotEco                         // ② 本区有它,但不属于 Eco 系列,本工具下不了
	planVerdictNoSuchPlan                     // ③ 三区都查不到:planCode 拼错 / 已下架
)

// classifyPlan 判定 planCode 对这个账户到底属于哪种情况,并给出面向用户的说明。
// 说明为空表示无需提示(OK / Unknown)。
//
// 顺序有讲究:先查本子公司的 eco 目录(2 小时内存缓存,不耗账户配额、不发网络请求),
// 命中就直接放行 —— 正常任务一次探测都不会发。只有"目录里没有"时才去探大区归属。
func classifyPlan(state *app.State, accountID, planCode string) (planVerdict, string) {
	acc, _ := state.FindAccount(accountID)
	accRegion := ovh.EndpointRegion(acc.Endpoint)
	subsidiary := catalog.SubsidiaryOfAccount(acc)

	_, catErr := catalog.AddonFamiliesForPlan(state, accountID, planCode)
	if catErr == nil {
		// 本子公司的 Eco 目录里就有它,能下单,不用再探
		return planVerdictOK, ""
	}
	if !errors.Is(catErr, catalog.ErrPlanNotInCatalog) {
		// 目录拉不动(网络 / 429):不下结论。这里下结论 = 一次瞬断把正常任务判死。
		state.Logger.Warn(fmt.Sprintf("[queue] 判定 %s 归属时目录拉取失败(子公司 %s)，本次不下结论: %s",
			planCode, subsidiary, catErr.Error()), "queue")
		return planVerdictUnknown, ""
	}

	// 本区排第一:命中就立刻返回,只花一次公开请求
	region, probeErr := catalog.RegionOfPlan(state, planCode, []string{accRegion, "EU", "US", "CA"})
	if probeErr != nil {
		state.Logger.Warn(fmt.Sprintf("[queue] 探测 %s 的大区归属失败，本次不下结论: %s",
			planCode, probeErr.Error()), "queue")
		return planVerdictUnknown, ""
	}

	switch {
	case region == "":
		// ③ 三区的可用性接口都没有这个 planCode 的任何记录(哪怕缺货也会有记录)
		return planVerdictNoSuchPlan, fmt.Sprintf(
			"机型 %s 在 OVH 的 EU / US / CA 三个站点都查不到任何库存记录（缺货的机型也会有记录）："+
				"planCode 可能拼错了，或者这个机型已经下架。",
			planCode)
	case region != accRegion:
		// ① 真跨区:别的大区有它的库存记录
		return planVerdictCrossRegion, fmt.Sprintf(
			"机型 %s 属于 OVH 的 %s 区，而账户 %s 在 %s 区（%s）。三个站点的目录 / 库存 / 购物车互不相通"+
				"（美区机型带 -us / -eu / -ca 后缀，欧区和加区不带），请改用本区的 planCode，或换一个 %s 区的账户下单。",
			planCode, region, acc.Name, accRegion, acc.Endpoint, region)
	}

	// ② 本区确实有这个机型的库存记录,只是它不走 Eco 下单链路
	return planVerdictNotEco, fmt.Sprintf(
		"机型 %s 在本区（%s 区，子公司 %s）确实有库存记录，但它不在该子公司的 Eco 目录里 —— "+
			"Scale / HCI / SDS / High-Grade 这些产品线都不走 Eco（实测欧区 244 个有库存的机型里 145 个如此）。"+
			"本工具的下单链路只有 /order/cart/eco 一条，买不了这台，请到 OVH 官网下单。",
		planCode, accRegion, subsidiary)
}

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
		// 探测失败(planVerdictUnknown)不拦 —— 一次网络瞬断不该让用户下不了单。
		if verdict, hint := classifyPlan(state, body.AccountID, body.PlanCode); hint != "" {
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
