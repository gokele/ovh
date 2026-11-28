package orderwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/notify"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

var (
	pollInterval        = 60 * time.Second
	startupDelay        = 20 * time.Second
	orderWindowDays     = 30
	maxOrdersPerAccount = 25
	requestTimeout      = 15 * time.Second
	nowFunc             = time.Now
)

const maxDetailItemsPerOrder = 5

// Loop 在后台周期轮询所有 OVH 账户的订单状态和交付进度变化并推送通知。
func Loop(state *app.State) {
	time.Sleep(startupDelay)
	RunOnce(context.Background(), state)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for range ticker.C {
		RunOnce(context.Background(), state)
	}
}

// RunOnce 对所有已配置 OVH 账户执行一轮订单变更检查与通知推送。
func RunOnce(ctx context.Context, state *app.State) {
	if state == nil || state.DB == nil || !state.HasAnyAccount() {
		return
	}
	if state.Config != nil && !state.Config.OrderUpdateNotifyEnabled() {
		return
	}
	if ok, _ := notify.AnyAvailable(state, false); !ok {
		return
	}

	state.AccountsMu.RLock()
	accounts := append([]types.OVHAccount(nil), state.Accounts...)
	state.AccountsMu.RUnlock()

	now := nowFunc()
	for _, acc := range accounts {
		if ctx.Err() != nil {
			return
		}
		pollAccount(ctx, state, acc, now)
	}
}

func pollAccount(ctx context.Context, state *app.State, acc types.OVHAccount, now time.Time) {
	client, err := state.OVH.IsolatedClientForAccount(acc)
	if err != nil {
		return
	}
	client.Timeout = requestTimeout

	recentIDs, err := fetchRecentOrderIDs(ctx, client, acc.Endpoint, now)
	if err != nil {
		state.Logger.Warn(fmt.Sprintf("订单监听拉取账户 %s 订单列表失败: %s", accountLabel(acc), ovh.Explain(err)), "orderwatch")
		return
	}

	seeded, err := state.DB.IsOrderWatchAccountSeeded(acc.ID)
	if err != nil {
		state.Logger.Warn(fmt.Sprintf("读取账户 %s 订单监听基线标记失败: %s", accountLabel(acc), err.Error()), "orderwatch")
		return
	}

	if !seeded {
		seedAccount(ctx, state, client, acc, recentIDs, now)
		return
	}

	saved, err := state.DB.ListOrderWatchSnapshots(acc.ID)
	if err != nil {
		state.Logger.Warn(fmt.Sprintf("读取账户 %s 订单监听快照失败: %s", accountLabel(acc), err.Error()), "orderwatch")
		return
	}

	candidateSet := make(map[string]struct{}, len(recentIDs)+len(saved))
	for _, id := range recentIDs {
		if _, ok := saved[id]; !ok {
			candidateSet[id] = struct{}{}
		}
	}
	for id, snap := range saved {
		if snap.Terminal {
			continue
		}
		if ref, ok := snapshotAgeRef(snap); ok && now.Sub(ref) > time.Duration(orderWindowDays)*24*time.Hour {
			continue
		}
		candidateSet[id] = struct{}{}
	}
	if len(candidateSet) == 0 {
		return
	}

	candidates := make([]string, 0, len(candidateSet))
	for id := range candidateSet {
		candidates = append(candidates, id)
	}
	sortOrderIDsAsc(candidates)

	for _, orderID := range candidates {
		if ctx.Err() != nil {
			return
		}
		prev, existed := saved[orderID]
		if !existed {
			handleNewOrder(ctx, state, client, acc, orderID, now)
		} else {
			handleExistingOrder(ctx, state, client, acc, prev, now)
		}
	}
}

func seedAccount(ctx context.Context, state *app.State, client *ovhsdk.Client, acc types.OVHAccount, recentIDs []string, now time.Time) {
	succeeded := 0
	for _, orderID := range recentIDs {
		if ctx.Err() != nil {
			return
		}
		meta, metaErr := fetchOrderMeta(ctx, client, orderID)
		rawStatus, statusErr := fetchOrderStatus(ctx, client, orderID)
		if statusErr != nil && metaErr != nil {
			continue
		}
		if statusErr != nil {
			continue
		}
		succeeded++
		effStatus := effectiveStatus(rawStatus, meta.ExpirationDate, now)
		terminal := isTerminalStatus(effStatus)

		stepsMap := map[string]string{}
		var eventKeys []string
		summary := meta.Summary
		if !terminal {
			if fu, err := fetchOrderFollowUp(ctx, client, orderID); err == nil {
				var events []FollowUpEvent
				stepsMap, events = extractStepsAndEvents(fu)
				eventKeys = make([]string, 0, len(events))
				for _, e := range events {
					eventKeys = append(eventKeys, eventKey(e))
				}
			}
			if ovh.EndpointRegion(acc.Endpoint) != "US" {
				if det, err := fetchOrderDetailsSummary(ctx, client, orderID); err == nil && det != "" {
					summary = det
				}
			}
		}
		if summary == "" {
			summary = summaryFromHistory(state, acc, orderID)
		}
		snap := db.OrderWatchSnapshot{
			AccountID:      acc.ID,
			OrderID:        orderID,
			OrderDate:      meta.Date,
			ExpirationDate: meta.ExpirationDate,
			OrderURL:       meta.URL,
			Summary:        summary,
			PriceText:      meta.PriceText,
			Status:         effStatus,
			Steps:          stepsMap,
			Events:         eventKeys,
			Terminal:       terminal,
			UpdatedAt:      types.NowISO(),
		}
		if err := state.DB.UpsertOrderWatchSnapshot(snap); err != nil {
			state.Logger.Warn(fmt.Sprintf("写入订单 %s 基线快照失败: %s", orderID, err.Error()), "orderwatch")
		}
		syncHistoryStatus(state, acc, orderID, rawStatus)
	}

	if len(recentIDs) > 0 && succeeded == 0 {
		return
	}
	if err := state.DB.MarkOrderWatchAccountSeeded(acc.ID, types.NowISO()); err != nil {
		state.Logger.Warn(fmt.Sprintf("保存账户 %s 基线标记失败: %s", accountLabel(acc), err.Error()), "orderwatch")
		return
	}
	state.Logger.Info(fmt.Sprintf("订单监听已完成账户 %s 基线同步（%d 笔订单）", accountLabel(acc), succeeded), "orderwatch")
}

func handleNewOrder(ctx context.Context, state *app.State, client *ovhsdk.Client, acc types.OVHAccount, orderID string, now time.Time) {
	rawStatus, err := fetchOrderStatus(ctx, client, orderID)
	if err != nil {
		state.Logger.Debug(fmt.Sprintf("读取新订单 %s 状态失败: %s", orderID, ovh.Explain(err)), "orderwatch")
		return
	}
	meta, _ := fetchOrderMeta(ctx, client, orderID)
	summary := meta.Summary
	if ovh.EndpointRegion(acc.Endpoint) != "US" {
		if det, err := fetchOrderDetailsSummary(ctx, client, orderID); err == nil && det != "" {
			summary = det
		}
	}
	if summary == "" {
		summary = summaryFromHistory(state, acc, orderID)
	}

	freshSteps := map[string]string{}
	var freshEvents []FollowUpEvent
	if fu, err := fetchOrderFollowUp(ctx, client, orderID); err == nil {
		freshSteps, freshEvents = extractStepsAndEvents(fu)
	}
	eventKeys := make([]string, 0, len(freshEvents))
	for _, e := range freshEvents {
		eventKeys = append(eventKeys, eventKey(e))
	}

	effStatus := effectiveStatus(rawStatus, meta.ExpirationDate, now)
	next := db.OrderWatchSnapshot{
		AccountID:      acc.ID,
		OrderID:        orderID,
		OrderDate:      meta.Date,
		ExpirationDate: meta.ExpirationDate,
		OrderURL:       meta.URL,
		Summary:        summary,
		PriceText:      meta.PriceText,
		Status:         effStatus,
		Steps:          freshSteps,
		Events:         eventKeys,
		Terminal:       isTerminalStatus(effStatus),
		UpdatedAt:      types.NowISO(),
	}

	inHistory := hasSuccessHistoryOrder(state, acc, orderID)
	var msg string
	if !inHistory {
		stepChanges := nonTodoStepChanges(freshSteps)
		msg = formatOrderNotification(acc, true, "", next, false, freshEvents, stepChanges)
	} else if (effStatus != "" && effStatus != "notPaid" && effStatus != "unknown") || hasSignificantFollowUp(freshSteps, freshEvents) {
		filteredEvents := filterOutOrderStarted(freshEvents)
		stepChanges := nonTodoStepChanges(freshSteps)
		statusChanged := effStatus != "" && effStatus != "notPaid" && effStatus != "unknown"
		msg = formatOrderNotification(acc, false, "notPaid", next, statusChanged, filteredEvents, stepChanges)
	}

	if msg != "" {
		if notify.Broadcast(state, msg, nil) > 0 {
			state.Logger.Info(fmt.Sprintf("已推送新订单动态: %s (%s)", orderID, effStatus), "orderwatch")
		} else {
			state.Logger.Warn(fmt.Sprintf("新订单 %s 动态推送未送达任何通道", orderID), "orderwatch")
		}
	}

	if err := state.DB.UpsertOrderWatchSnapshot(next); err != nil {
		state.Logger.Warn(fmt.Sprintf("保存新订单 %s 快照失败: %s", orderID, err.Error()), "orderwatch")
	}
	syncHistoryStatus(state, acc, orderID, rawStatus)
}

func handleExistingOrder(ctx context.Context, state *app.State, client *ovhsdk.Client, acc types.OVHAccount, prev db.OrderWatchSnapshot, now time.Time) {
	if prev.ExpirationDate == "" || prev.OrderURL == "" || (prev.Summary == "" && ovh.EndpointRegion(acc.Endpoint) != "US") {
		if meta, err := fetchOrderMeta(ctx, client, prev.OrderID); err == nil {
			if prev.OrderDate == "" {
				prev.OrderDate = meta.Date
			}
			if prev.ExpirationDate == "" {
				prev.ExpirationDate = meta.ExpirationDate
			}
			if prev.OrderURL == "" {
				prev.OrderURL = meta.URL
			}
			if prev.PriceText == "" {
				prev.PriceText = meta.PriceText
			}
		}
		if prev.Summary == "" && ovh.EndpointRegion(acc.Endpoint) != "US" {
			if det, err := fetchOrderDetailsSummary(ctx, client, prev.OrderID); err == nil && det != "" {
				prev.Summary = det
			}
		}
		if prev.Summary == "" {
			prev.Summary = summaryFromHistory(state, acc, prev.OrderID)
		}
	}

	rawStatus, err := fetchOrderStatus(ctx, client, prev.OrderID)
	if err != nil {
		state.Logger.Debug(fmt.Sprintf("查询订单 %s 状态失败: %s", prev.OrderID, ovh.Explain(err)), "orderwatch")
		return
	}
	effStatus := effectiveStatus(rawStatus, prev.ExpirationDate, now)

	var freshSteps map[string]string
	var freshEvents []FollowUpEvent
	followUpOK := false
	if fu, err := fetchOrderFollowUp(ctx, client, prev.OrderID); err == nil {
		freshSteps, freshEvents = extractStepsAndEvents(fu)
		followUpOK = true
	}

	next, statusChanged, newEvents, stepChanges := diffOrder(prev, effStatus, freshSteps, freshEvents, followUpOK)
	if rawStatus == "unknown" && !statusChanged && len(newEvents) == 0 && len(stepChanges) == 0 {
		syncHistoryStatus(state, acc, prev.OrderID, rawStatus)
		return
	}

	hasUpdate := statusChanged || len(newEvents) > 0 || len(stepChanges) > 0
	if hasUpdate {
		msg := formatOrderNotification(acc, false, prev.Status, next, statusChanged, newEvents, stepChanges)
		if notify.Broadcast(state, msg, nil) > 0 {
			state.Logger.Info(fmt.Sprintf("已推送订单 %s 更新 (%s)", prev.OrderID, next.Status), "orderwatch")
		} else {
			state.Logger.Warn(fmt.Sprintf("订单 %s 更新推送未送达任何通道", prev.OrderID), "orderwatch")
		}
	}

	metaChanged := next.Terminal != prev.Terminal || next.Summary != prev.Summary ||
		next.ExpirationDate != prev.ExpirationDate || next.OrderURL != prev.OrderURL || next.PriceText != prev.PriceText
	if hasUpdate || metaChanged {
		next.UpdatedAt = types.NowISO()
		if err := state.DB.UpsertOrderWatchSnapshot(next); err != nil {
			state.Logger.Warn(fmt.Sprintf("更新订单 %s 快照失败: %s", prev.OrderID, err.Error()), "orderwatch")
		}
	}

	syncHistoryStatus(state, acc, prev.OrderID, rawStatus)
}

func nonTodoStepChanges(steps map[string]string) []StepChange {
	var out []StepChange
	visited := make(map[string]bool, len(steps))
	for _, st := range canonicalSteps {
		if v := steps[st]; v != "" && v != "TODO" {
			out = append(out, StepChange{Step: st, NewStatus: v})
		}
		visited[st] = true
	}
	var extras []string
	for st := range steps {
		if !visited[st] {
			extras = append(extras, st)
		}
	}
	sort.Strings(extras)
	for _, st := range extras {
		if v := steps[st]; v != "" && v != "TODO" {
			out = append(out, StepChange{Step: st, NewStatus: v})
		}
	}
	return out
}

func filterOutOrderStarted(events []FollowUpEvent) []FollowUpEvent {
	out := make([]FollowUpEvent, 0, len(events))
	for _, e := range events {
		if e.Label == "ORDER_STARTED" {
			continue
		}
		out = append(out, e)
	}
	return out
}

func fetchRecentOrderIDs(ctx context.Context, client *ovhsdk.Client, endpoint string, now time.Time) ([]string, error) {
	var raw []interface{}
	var err error
	if ovh.EndpointRegion(endpoint) != "US" {
		from := now.UTC().AddDate(0, 0, -orderWindowDays).Format("2006-01-02T15:04:05+00:00")
		err = client.GetWithContext(ctx, "/me/order?date.from="+url.QueryEscape(from), &raw)
	}
	if ovh.EndpointRegion(endpoint) == "US" || err != nil {
		raw = nil
		if err2 := client.GetWithContext(ctx, "/me/order", &raw); err2 != nil {
			return nil, err2
		}
	}
	ids := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, v := range raw {
		s := idToString(v)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		ids = append(ids, s)
	}
	sortOrderIDsDesc(ids)
	if len(ids) > maxOrdersPerAccount {
		ids = ids[:maxOrdersPerAccount]
	}
	return ids, nil
}

func fetchOrderMeta(ctx context.Context, client *ovhsdk.Client, orderID string) (OrderMeta, error) {
	var raw struct {
		OrderID        json.RawMessage `json:"orderId"`
		Date           string          `json:"date"`
		ExpirationDate string          `json:"expirationDate"`
		URL            string          `json:"url"`
		PriceWithTax   struct {
			Text string `json:"text"`
		} `json:"priceWithTax"`
	}
	if err := client.GetWithContext(ctx, "/me/order/"+url.PathEscape(orderID), &raw); err != nil {
		return OrderMeta{OrderID: orderID}, err
	}
	return OrderMeta{
		OrderID:        orderID,
		Date:           strings.TrimSpace(raw.Date),
		ExpirationDate: strings.TrimSpace(raw.ExpirationDate),
		URL:            strings.TrimSpace(raw.URL),
		PriceText:      strings.TrimSpace(raw.PriceWithTax.Text),
	}, nil
}

func fetchOrderStatus(ctx context.Context, client *ovhsdk.Client, orderID string) (string, error) {
	var status string
	if err := client.GetWithContext(ctx, "/me/order/"+url.PathEscape(orderID)+"/status", &status); err != nil {
		return "", err
	}
	return strings.TrimSpace(status), nil
}

func fetchOrderFollowUp(ctx context.Context, client *ovhsdk.Client, orderID string) ([]FollowUpStep, error) {
	var steps []FollowUpStep
	if err := client.GetWithContext(ctx, "/me/order/"+url.PathEscape(orderID)+"/followUp", &steps); err != nil {
		return nil, err
	}
	return steps, nil
}

func fetchOrderDetailsSummary(ctx context.Context, client *ovhsdk.Client, orderID string) (string, error) {
	var ids []int64
	base := "/me/order/" + url.PathEscape(orderID) + "/details"
	if err := client.GetWithContext(ctx, base, &ids); err != nil {
		return "", err
	}
	if len(ids) > maxDetailItemsPerOrder {
		ids = ids[:maxDetailItemsPerOrder]
	}
	var primary []string
	var fallback []string
	for _, did := range ids {
		var d struct {
			Description string `json:"description"`
			DetailType  string `json:"detailType"`
			Cancelled   bool   `json:"cancelled"`
		}
		if err := client.GetWithContext(ctx, fmt.Sprintf("%s/%d", base, did), &d); err != nil {
			continue
		}
		desc := strings.TrimSpace(d.Description)
		if d.Cancelled || desc == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(d.DetailType), "INSTALLATION") {
			fallback = append(fallback, desc)
			continue
		}
		primary = append(primary, desc)
	}
	chosen := primary
	if len(chosen) == 0 {
		chosen = fallback
	}
	if len(chosen) == 0 {
		return "", nil
	}
	if len(chosen) == 1 {
		return chosen[0], nil
	}
	return fmt.Sprintf("%s 等 %d 项", chosen[0], len(chosen)), nil
}

func snapshotAgeRef(snap db.OrderWatchSnapshot) (time.Time, bool) {
	if t, ok := parseOVHTime(snap.OrderDate); ok {
		return t, true
	}
	return parseOVHTime(snap.UpdatedAt)
}

func hasSuccessHistoryOrder(state *app.State, acc types.OVHAccount, orderID string) bool {
	if state == nil {
		return false
	}
	state.HistoryMu.Lock()
	defer state.HistoryMu.Unlock()
	for _, h := range state.History {
		if h.Status != "success" || h.OrderID != orderID {
			continue
		}
		if h.AccountID == "" && acc.IsDefault || h.AccountID == acc.ID {
			return true
		}
	}
	return false
}

func summaryFromHistory(state *app.State, acc types.OVHAccount, orderID string) string {
	if state == nil {
		return ""
	}
	state.HistoryMu.Lock()
	defer state.HistoryMu.Unlock()
	for _, h := range state.History {
		if h.OrderID != orderID {
			continue
		}
		if h.AccountID != "" && h.AccountID != acc.ID {
			continue
		}
		if h.AccountID == "" && !acc.IsDefault {
			continue
		}
		if h.PlanCode != "" && h.Datacenter != "" {
			return h.PlanCode + " @ " + strings.ToUpper(h.Datacenter)
		}
		if h.PlanCode != "" {
			return h.PlanCode
		}
	}
	return ""
}

// syncHistoryStatus 把从 OVH 拿到的原始订单状态回写到购买历史（不写衍生值 expired，也不覆盖为 unknown）。
func syncHistoryStatus(state *app.State, acc types.OVHAccount, orderID, rawStatus string) {
	if state == nil || rawStatus == "" || rawStatus == "unknown" {
		return
	}
	state.HistoryMu.Lock()
	changed := false
	nowISO := types.NowISO()
	for i := range state.History {
		h := &state.History[i]
		if h.Status != "success" || h.OrderID != orderID {
			continue
		}
		if h.AccountID != "" && h.AccountID != acc.ID {
			continue
		}
		if h.AccountID == "" && !acc.IsDefault {
			continue
		}
		if h.OrderStatus != rawStatus {
			if h.OrderStatus != "" {
				state.Logger.Info(fmt.Sprintf("订单 %s 状态: %s → %s", orderID, h.OrderStatus, rawStatus), "purchase")
			}
			h.OrderStatus = rawStatus
			changed = true
		}
		h.OrderStatusAt = nowISO
	}
	state.HistoryMu.Unlock()
	if changed {
		go state.SaveHistory()
	}
}

func accountLabel(acc types.OVHAccount) string {
	if strings.TrimSpace(acc.Name) != "" {
		return acc.Name
	}
	return acc.ID
}

func idToString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return strings.TrimSpace(x.String())
	default:
		return ""
	}
}

func sortOrderIDsDesc(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool {
		return compareOrderID(ids[i], ids[j]) > 0
	})
}

func sortOrderIDsAsc(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool {
		return compareOrderID(ids[i], ids[j]) < 0
	})
}

func compareOrderID(a, b string) int {
	na, ea := strconv.ParseInt(a, 10, 64)
	nb, eb := strconv.ParseInt(b, 10, 64)
	if ea == nil && eb == nil {
		if na < nb {
			return -1
		}
		if na > nb {
			return 1
		}
		return 0
	}
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
