package orderwatch

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/types"
)

var tuningAcc = types.OVHAccount{
	ID: "eu1", Name: "主号EU", Endpoint: "ovh-eu", Zone: "IE",
	AppKey: "ak", AppSecret: "as", ConsumerKey: "ck", IsDefault: true,
}

func unpaidOrder(date, exp string) mockOrder {
	return mockOrder{date: date, expirationDate: exp, priceText: "€10.00", status: "notPaid",
		details: map[string]string{"1": `{"description":"KS-LE-1 | GRA","detailType":"DURATION","cancelled":false}`}}
}

// clock 让测试推进 nowFunc;结束时还原。
func clock(t *testing.T, start time.Time) func(time.Time) {
	t.Helper()
	orig := nowFunc
	now := start
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = orig })
	return func(next time.Time) { now = next }
}

func setVar(t *testing.T, p *time.Duration, v time.Duration) {
	t.Helper()
	orig := *p
	*p = v
	t.Cleanup(func() { *p = orig })
}

func statusCalls(m *mockOrderState, orderID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.calls {
		if strings.HasSuffix(c, "/me/order/"+orderID+"/status") {
			n++
		}
	}
	return n
}

func base() time.Time { return time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC) }

// 一个账户一轮里多笔订单变化,只发一条带标题的合并消息。
func TestOrderWatchMergesOneAccountRoundIntoOneMessage(t *testing.T) {
	clock(t, base())
	mock := &mockOrderState{orderIDs: []string{"200", "300"}, followUpErr: map[string]int{}, orders: map[string]mockOrder{
		"200": unpaidOrder("2026-11-28T11:50:00+00:00", "2026-11-28T13:00:00+00:00"),
		"300": unpaidOrder("2026-11-28T11:51:00+00:00", "2026-11-28T13:00:00+00:00"),
	}}
	state, msgs, mu := newTestState(t, tuningAcc, mock)
	RunOnce(context.Background(), state) // 基线
	takeMessages(msgs, mu)

	mock.mu.Lock()
	for _, id := range []string{"200", "300"} {
		o := mock.orders[id]
		o.status = "checking"
		mock.orders[id] = o
	}
	mock.mu.Unlock()
	RunOnce(context.Background(), state)
	got := takeMessages(msgs, mu)
	if len(got) != 1 || !strings.Contains(got[0], "2 笔订单更新") || !strings.Contains(got[0], "200") || !strings.Contains(got[0], "300") {
		t.Fatalf("expected one merged message for both orders, got %v", got)
	}
}

// 过期提醒关闭时,待付款→过期不发消息,但快照照常记为终态,之后不再查询。
func TestOrderWatchExpiryMutedByDefault(t *testing.T) {
	set := clock(t, base())
	mock := &mockOrderState{orderIDs: []string{"200"}, followUpErr: map[string]int{}, orders: map[string]mockOrder{
		"200": unpaidOrder("2026-11-28T11:50:00+00:00", "2026-11-28T12:05:00+00:00"),
	}}
	state, msgs, mu := newTestState(t, tuningAcc, mock)
	cfg := state.Config.Get()
	cfg.NotifyOrderExpiry = nil
	if err := state.Config.Set(cfg); err != nil {
		t.Fatal(err)
	}
	RunOnce(context.Background(), state)
	set(base().Add(10 * time.Minute))
	RunOnce(context.Background(), state)
	if got := takeMessages(msgs, mu); len(got) != 0 {
		t.Fatalf("expiry must be silent when its alert is off, got %v", got)
	}
	snaps, err := state.DB.ListOrderWatchSnapshots(tuningAcc.ID)
	if err != nil || !snaps["200"].Terminal || snaps["200"].Status != "expired" {
		t.Fatalf("expired order must still be recorded as terminal: %+v %v", snaps["200"], err)
	}
}

// 刚出现、还不在抢购历史里的新订单先等一等,过了等待期仍不在历史里才报「已下单」。
func TestOrderWatchDefersFreshUnknownOrders(t *testing.T) {
	set := clock(t, base())
	setVar(t, &newOrderGrace, 6*time.Minute)
	mock := &mockOrderState{orderIDs: []string{}, followUpErr: map[string]int{}, orders: map[string]mockOrder{}}
	state, msgs, mu := newTestState(t, tuningAcc, mock)
	RunOnce(context.Background(), state) // 空基线

	mock.mu.Lock()
	mock.orderIDs = []string{"500"}
	mock.orders["500"] = unpaidOrder("2026-11-28T11:58:00+00:00", "2026-11-28T13:00:00+00:00")
	mock.mu.Unlock()
	RunOnce(context.Background(), state)
	if got := takeMessages(msgs, mu); len(got) != 0 {
		t.Fatalf("a 2-minute-old order not yet in history must wait, got %v", got)
	}
	if snaps, _ := state.DB.ListOrderWatchSnapshots(tuningAcc.ID); len(snaps) != 0 {
		t.Fatal("deferred order must not be recorded yet")
	}
	set(base().Add(5 * time.Minute))
	RunOnce(context.Background(), state)
	if got := takeMessages(msgs, mu); len(got) != 1 || !strings.Contains(got[0], "已下单") {
		t.Fatalf("after the grace period an unrecorded order is announced, got %v", got)
	}
}

// 抢购开了自动付款、结账后不久被取消:paycheck 会报,这里不重复;没开自动付款时照常报。
func TestOrderWatchSkipsWhatPaycheckReports(t *testing.T) {
	for _, autoPay := range []bool{true, false} {
		t.Run(fmt.Sprint("autoPay=", autoPay), func(t *testing.T) {
			set := clock(t, base())
			mock := &mockOrderState{orderIDs: []string{"400"}, followUpErr: map[string]int{}, orders: map[string]mockOrder{
				"400": unpaidOrder("2026-11-28T11:59:00+00:00", "2026-11-28T13:00:00+00:00"),
			}}
			state, msgs, mu := newTestState(t, tuningAcc, mock)
			state.Queue = []types.QueueItem{{ID: "t1", AccountID: tuningAcc.ID, Status: "completed", AutoPay: autoPay}}
			state.History = []types.PurchaseHistoryEntry{{ID: "h1", TaskID: "t1", AccountID: tuningAcc.ID, Status: "success", OrderID: "400", PurchaseTime: "2026-11-28T11:59:00Z"}}
			RunOnce(context.Background(), state)
			takeMessages(msgs, mu)

			mock.mu.Lock()
			o := mock.orders["400"]
			o.status = "cancelled"
			mock.orders["400"] = o
			mock.mu.Unlock()
			set(base().Add(2 * time.Minute))
			RunOnce(context.Background(), state)
			got := takeMessages(msgs, mu)
			if autoPay && len(got) != 0 {
				t.Fatalf("cancellation within the auto-pay check window is reported by paycheck, got %v", got)
			}
			if !autoPay && (len(got) != 1 || !strings.Contains(got[0], "已取消")) {
				t.Fatalf("without auto-pay the cancellation must be pushed, got %v", got)
			}
		})
	}
}

// 待付款这类安静的订单与订单列表按 5 分钟节流;在变化中的订单每轮都查。
func TestOrderWatchThrottlesQuietOrdersAndList(t *testing.T) {
	set := clock(t, base())
	setVar(t, &quietPollInterval, 5*time.Minute)
	setVar(t, &listInterval, 5*time.Minute)
	mock := &mockOrderState{orderIDs: []string{"200", "300"}, followUpErr: map[string]int{}, orders: map[string]mockOrder{
		"200": unpaidOrder("2026-11-28T11:50:00+00:00", "2026-11-28T13:00:00+00:00"),
		"300": func() mockOrder { o := unpaidOrder("2026-11-28T11:50:00+00:00", ""); o.status = "delivering"; return o }(),
	}}
	state, _, _ := newTestState(t, tuningAcc, mock)
	RunOnce(context.Background(), state) // 基线
	RunOnce(context.Background(), state) // 第一轮:都查
	quiet, busy := statusCalls(mock, "200"), statusCalls(mock, "300")
	set(base().Add(time.Minute))
	RunOnce(context.Background(), state)
	if statusCalls(mock, "200") != quiet {
		t.Fatal("an unpaid order must not be re-queried within the quiet interval")
	}
	if statusCalls(mock, "300") != busy+1 {
		t.Fatal("an order in delivery must be queried every round")
	}
	set(base().Add(6 * time.Minute))
	RunOnce(context.Background(), state)
	if statusCalls(mock, "200") != quiet+1 {
		t.Fatal("the unpaid order must be queried again after the quiet interval")
	}
}

// 合并消息过长时,每单只保留标题、订单号、商品和状态,总长受限。
func TestMergedMessageStaysWithinTelegramLimit(t *testing.T) {
	var msgs []string
	for i := 0; i < 40; i++ {
		msgs = append(msgs, fmt.Sprintf("✅ OVH 订单已交付\n\n订单号: %d\n账户: 主号EU\n商品: KS-LE-1 | GRA\n金额: €10.00\n状态: 交付中 → 已交付\n进展:\n• %s\n链接: https://example.invalid/%d", 1000+i, strings.Repeat("长描述", 20), i))
	}
	out := mergeAccountMessages(tuningAcc, msgs)
	if n := len([]rune(out)); n > maxMergedChars+200 {
		t.Fatalf("merged message too long: %d", n)
	}
	if !strings.Contains(out, "40 笔订单更新") || !strings.Contains(out, "订单号: 1000") || strings.Contains(out, "链接:") {
		t.Fatalf("compact form must keep headline/order and drop details: %q", out[:200])
	}
}
