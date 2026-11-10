package purchase

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/notify"
	"github.com/ovh-buy/server/internal/orderterm"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// 结账结果不明时的只读核对。
//
// 官方依据(EU/CA /1.0/me.json):
//   - GET /me/order?date.from= 列出该时间之后创建的订单号(三区都有)。
//   - GET /me/order/{id}/details 列出订单明细号。
//   - GET /me/order/{id}/details/{detailId}/extension 返回下单时的 plan.code、plan.duration
//     与 configurations(含 dedicated_datacenter)。该接口标为 BETA,US 站 schema 没有。
//
// 规则(稳优先于快):
//   - 只读,不重试结账、不恢复任务。
//   - 恰好一笔「型号 + 机房 + 周期」都吻合、且没有记到其他任务名下的订单,才把任务标为已下单。
//   - 多笔吻合、US 账户、读不到明细或到时仍未找到:任务保持暂停,只把看到的情况告诉用户。
//   - 用户在核对期间恢复了任务(结账登记被清除)或任务已不再暂停时,放弃核对结果。
var resolveSchedule = []time.Duration{10 * time.Second, 20 * time.Second, 30 * time.Second, time.Minute, 3 * time.Minute}

// resolveClockSkew 查询起点往前留的余量,覆盖本机与 OVH 的时钟偏差。
const resolveClockSkew = 2 * time.Minute

type orderResolver struct {
	mu      sync.Mutex
	active  map[string]bool // 正在核对的任务
	claimed map[string]bool // 已记到某个任务名下的「账户\x00订单号」
}

var resolver = &orderResolver{active: map[string]bool{}, claimed: map[string]bool{}}

// orderExtension 是 billing.ItemDetail 中核对需要的字段。
type orderExtension struct {
	Order struct {
		Plan struct {
			Code     string `json:"code"`
			Duration string `json:"duration"`
		} `json:"plan"`
		Configurations []struct {
			Label string `json:"label"`
			Value string `json:"value"`
		} `json:"configurations"`
	} `json:"order"`
}

// resolveTarget 一次核对需要的全部输入;启动时拷贝,核对期间不再读任务的可变状态。
type resolveTarget struct {
	item     types.QueueItem
	account  types.OVHAccount
	apiDC    string
	duration string
	sentAt   time.Time
	// autoResume 结账收到 429(限流)时为 true:OVH 几乎可以肯定没有处理这次请求。
	// 只有在结账后满 autoResumeAfter 仍然一笔新订单都没有(不只是没有吻合的)时才自动恢复任务。
	autoResume bool
}

// autoResumeAfter 自动恢复前至少观察的时间。
const autoResumeAfter = time.Minute

// startOrderResolver 在后台核对一笔结果不明的结账;同一任务同时只运行一个核对。
func startOrderResolver(state *app.State, target resolveTarget) {
	resolver.mu.Lock()
	if resolver.active[target.item.ID] {
		resolver.mu.Unlock()
		return
	}
	resolver.active[target.item.ID] = true
	resolver.mu.Unlock()
	go func() {
		defer func() {
			resolver.mu.Lock()
			delete(resolver.active, target.item.ID)
			resolver.mu.Unlock()
		}()
		runOrderResolver(state, target, resolveSchedule, time.Sleep)
	}()
}

// runOrderResolver 按计划多次查询,找到唯一吻合订单即确认;sleep 可注入以便测试。
func runOrderResolver(state *app.State, target resolveTarget, schedule []time.Duration, sleep func(time.Duration)) {
	client, err := state.OVH.IsolatedClientForAccount(target.account)
	if err != nil {
		state.Logger.Warn("结果不明的结账无法自动核对(账户不可用): "+err.Error(), "purchase")
		return
	}
	us := ovh.EndpointRegion(target.account.Endpoint) == "US"
	var lastCandidates []string
	var elapsed time.Duration
	for _, delay := range schedule {
		sleep(delay)
		elapsed += delay
		if !resolverStillNeeded(state, target.item.ID) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		matches, candidates, err := findMatchingOrders(ctx, state, client, target, us)
		cancel()
		if err != nil {
			state.Logger.Debug("核对结果不明的结账时读取订单失败,稍后再试: "+err.Error(), "purchase")
			continue
		}
		lastCandidates = candidates
		if target.autoResume && !us && len(candidates) == 0 && elapsed >= autoResumeAfter {
			if autoResumeTask(state, target) {
				return
			}
		}
		if us || len(matches) != 1 {
			if len(matches) > 1 {
				break // 多笔吻合:无法判断是哪一笔,交给用户
			}
			continue
		}
		if confirmResolvedOrder(state, client, target, matches[0]) {
			return
		}
		break
	}
	if !resolverStillNeeded(state, target.item.ID) {
		return
	}
	notify.Broadcast(state, buildUnresolvedMessage(target, lastCandidates, us), map[string]interface{}{"inline_keyboard": [][]map[string]interface{}{{
		telegram.TaskButton("确认 OVH 没有这笔订单，恢复任务", target.item.ID, telegram.TaskResume),
	}}})
}

// resolverStillNeeded 任务仍处于暂停且结账登记仍在,才需要继续核对。
func resolverStillNeeded(state *app.State, taskID string) bool {
	found, err := state.DB.GetKV(db.QueueCheckoutKey(taskID), &map[string]interface{}{})
	if err != nil || !found {
		return false
	}
	state.QueueMu.Lock()
	defer state.QueueMu.Unlock()
	for _, it := range state.Queue {
		if it.ID == taskID {
			return it.Status == "paused"
		}
	}
	return false
}

// findMatchingOrders 返回吻合的未认领订单号,以及时间窗内所有未认领的新订单号(供提示用)。
// US 站没有明细扩展接口,只返回候选,不做吻合判断。
func findMatchingOrders(ctx context.Context, state *app.State, client *ovhsdk.Client, target resolveTarget, us bool) (matches, candidates []string, err error) {
	from := target.sentAt.Add(-resolveClockSkew).UTC().Format(time.RFC3339)
	var ids []int64
	if err = client.GetWithContext(ctx, "/me/order?date.from="+url.QueryEscape(from), &ids); err != nil {
		return nil, nil, err
	}
	var fresh []int64
	for _, id := range ids {
		orderID := fmt.Sprint(id)
		if resolverClaimed(target.account.ID, orderID) || (state != nil && historyHasOrder(state, target.account.ID, orderID)) {
			continue
		}
		fresh = append(fresh, id)
		candidates = append(candidates, orderID)
	}
	if us || len(fresh) == 0 {
		return nil, candidates, nil
	}
	// 只读查询并发:先并发取所有候选订单的明细号,再并发取全部明细扩展。
	details := make([][]int64, len(fresh))
	detailPaths := make([]string, len(fresh))
	for i, id := range fresh {
		detailPaths[i] = fmt.Sprintf("/me/order/%d/details", id)
	}
	if err = ovh.GetParallel(ctx, client, detailPaths, 6, func(i int) interface{} { return &details[i] }); err != nil {
		return nil, nil, err
	}
	var extPaths []string
	var owner []int
	for i, id := range fresh {
		for _, detail := range details[i] {
			extPaths = append(extPaths, fmt.Sprintf("/me/order/%d/details/%d/extension", id, detail))
			owner = append(owner, i)
		}
	}
	exts := make([]orderExtension, len(extPaths))
	if err = ovh.GetParallel(ctx, client, extPaths, 8, func(i int) interface{} { return &exts[i] }); err != nil {
		return nil, nil, err
	}
	matched := map[int]bool{}
	for k, ext := range exts {
		if extensionMatches(ext, target) {
			matched[owner[k]] = true
		}
	}
	for i, id := range fresh {
		if matched[i] {
			matches = append(matches, fmt.Sprint(id))
		}
	}
	return matches, candidates, nil
}

// extensionMatches 型号、机房必须完全一致;周期给出时也必须一致。
func extensionMatches(ext orderExtension, target resolveTarget) bool {
	if !strings.EqualFold(ext.Order.Plan.Code, target.item.PlanCode) {
		return false
	}
	if d := ext.Order.Plan.Duration; d != "" && target.duration != "" && !orderterm.Same(d, target.duration) {
		return false
	}
	for _, cfg := range ext.Order.Configurations {
		if cfg.Label == "dedicated_datacenter" {
			return strings.EqualFold(cfg.Value, target.apiDC)
		}
	}
	return false
}

// resolverClaimed 订单是否已记到某个任务名下(本次运行认领过,或历史里已有成功记录)。
func resolverClaimed(accountID, orderID string) bool {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	return resolver.claimed[accountID+"\x00"+orderID]
}

// historyHasOrder 历史中已有同账户同订单号的成功记录。
func historyHasOrder(state *app.State, accountID, orderID string) bool {
	state.HistoryMu.Lock()
	defer state.HistoryMu.Unlock()
	for _, h := range state.History {
		if h.OrderID == orderID && (h.AccountID == "" || h.AccountID == accountID) {
			return true
		}
	}
	return false
}

// confirmResolvedOrder 原子认领订单号并把任务标为已下单;认领失败或任务状态已变则不改动。
func confirmResolvedOrder(state *app.State, client *ovhsdk.Client, target resolveTarget, orderID string) bool {
	key := target.account.ID + "\x00" + orderID
	resolver.mu.Lock()
	if resolver.claimed[key] || historyHasOrder(state, target.account.ID, orderID) {
		resolver.mu.Unlock()
		return false
	}
	resolver.claimed[key] = true
	resolver.mu.Unlock()

	if !resolverStillNeeded(state, target.item.ID) {
		resolver.mu.Lock()
		delete(resolver.claimed, key)
		resolver.mu.Unlock()
		return false
	}
	state.QueueMu.Lock()
	updated := false
	for i := range state.Queue {
		if state.Queue[i].ID == target.item.ID && state.Queue[i].Status == "paused" {
			state.Queue[i].Status = "completed"
			state.Queue[i].UpdatedAt = types.NowISO()
			updated = true
			break
		}
	}
	state.QueueMu.Unlock()
	if !updated {
		resolver.mu.Lock()
		delete(resolver.claimed, key)
		resolver.mu.Unlock()
		return false
	}
	item := target.item
	if err := state.DB.MarkQueueOrder(item, "ordered"); err != nil {
		state.Logger.Error("已核实订单,但总量登记未更新: "+err.Error(), "purchase")
	}
	recordSuccess(state, &item, orderID, "", "", nil, "")
	_ = state.SaveQueue()
	go backfillOrderDetail(state, client, item.ID, orderID)
	state.Logger.Info(fmt.Sprintf("结果不明的结账已核实: 任务 %s 对应订单 %s", item.ID, orderID), "purchase")
	message := "✅ 已在 OVH 核实到这笔订单（结账时网络结果不明）\n\n" +
		BuildOrderSuccessMessage(&item, orderID, ovh.ManagerOrderURL(target.account.Endpoint, orderID))
	notify.Broadcast(state, message, nil)
	return true
}

// buildUnresolvedMessage 核对结束仍无法确认时的说明;列出看到的新订单号,不替用户下结论。
func buildUnresolvedMessage(target resolveTarget, candidates []string, us bool) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("⚠️ %s@%s 的结账结果仍无法自动确认\n\n", target.item.PlanCode, strings.ToUpper(target.item.Datacenter)))
	switch {
	case us:
		b.WriteString("美国站没有订单明细接口，无法按型号和机房自动核对。\n")
	case len(candidates) == 0:
		b.WriteString("结账后几分钟内，账户里没有出现新的订单。\n")
	default:
		b.WriteString("账户里有新订单，但无法唯一对应到这个任务。\n")
	}
	if len(candidates) > 0 {
		shown := candidates
		if len(shown) > 5 {
			shown = shown[:5]
		}
		b.WriteString("新订单号：" + strings.Join(shown, "、") + "\n")
	}
	b.WriteString("\n任务保持暂停。请到 OVH 订单页确认：\n- 有这笔订单：去付款，任务不用恢复\n- 没有：点下面的按钮恢复任务")
	return b.String()
}

// autoResumeTask 429 后观察期内账户没有任何新订单:恢复任务继续抢购并通知用户。
// 与手动恢复同一路径(清除结账登记与名额),任务已不在暂停状态时不做任何事。
func autoResumeTask(state *app.State, target resolveTarget) bool {
	if !resolverStillNeeded(state, target.item.ID) {
		return false
	}
	if err := state.ResumeQueueItem(target.item.ID, "running"); err != nil {
		state.Logger.Warn("429 后自动恢复任务失败,保持暂停: "+err.Error(), "purchase")
		return false
	}
	state.Logger.Info(fmt.Sprintf("任务 %s 结账被限流(429),%s 内账户没有新订单,已自动恢复", target.item.ID, autoResumeAfter), "purchase")
	notify.Broadcast(state, fmt.Sprintf("🔁 %s@%s 结账时被 OVH 限流（429）。\n结账后 1 分钟内账户里没有任何新订单，确认未下单，任务已自动恢复继续抢购。",
		target.item.PlanCode, strings.ToUpper(target.item.Datacenter)), nil)
	return true
}
