package orderwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/types"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResp(r *http.Request, code int, body string) *http.Response {
	return &http.Response{
		StatusCode: code,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

type mockOrderState struct {
	mu          sync.Mutex
	orderIDs    []string
	orders      map[string]mockOrder
	followUpErr map[string]int
	calls       []string
}

type mockOrder struct {
	date           string
	expirationDate string
	url            string
	priceText      string
	status         string
	followUp       []FollowUpStep
	details        map[string]string // detailId -> json
}

func newTestState(t *testing.T, acc types.OVHAccount, mock *mockOrderState) (*app.State, *[]string, *sync.Mutex) {
	t.Helper()
	resetThrottle()
	t.Cleanup(resetThrottle)
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	var mu sync.Mutex
	var msgs []string
	wh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var p struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(raw, &p)
		mu.Lock()
		msgs = append(msgs, p.Text)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	t.Cleanup(wh.Close)

	cfg := config.New(database)
	on := true
	if err := cfg.Set(types.Config{NotifyWebhookURL: wh.URL, NotifyOrderUpdates: &on, NotifyOrderExpiry: &on}); err != nil {
		t.Fatalf("set config: %v", err)
	}
	lg := logger.New(dir, nil)
	state := app.NewState(storage.Paths{DataDir: dir}, cfg, lg, database)
	state.Accounts = []types.OVHAccount{acc}
	if err := database.UpsertAccount(acc); err != nil {
		t.Fatalf("upsert account: %v", err)
	}

	if mock != nil {
		base, err := state.OVH.ClientForAccount(acc)
		if err != nil {
			t.Fatalf("client for account: %v", err)
		}
		base.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			path := r.URL.Path
			if strings.HasSuffix(path, "/auth/time") {
				return jsonResp(r, 200, fmt.Sprint(time.Now().Unix())), nil
			}
			mock.mu.Lock()
			defer mock.mu.Unlock()
			full := path
			if r.URL.RawQuery != "" {
				full += "?" + r.URL.RawQuery
			}
			mock.calls = append(mock.calls, full)

			if strings.HasSuffix(path, "/me/order") {
				b, _ := json.Marshal(mock.orderIDs)
				return jsonResp(r, 200, string(b)), nil
			}
			const prefix = "/1.0/me/order/"
			idx := strings.Index(path, prefix)
			if idx < 0 {
				return jsonResp(r, 404, `{"message":"not found"}`), nil
			}
			rest := path[idx+len(prefix):]
			parts := strings.Split(rest, "/")
			oid := parts[0]
			ord, ok := mock.orders[oid]
			if !ok {
				return jsonResp(r, 404, `{"message":"order not found"}`), nil
			}
			switch len(parts) {
			case 1:
				body, _ := json.Marshal(map[string]interface{}{
					"orderId":        oid,
					"date":           ord.date,
					"expirationDate": ord.expirationDate,
					"url":            ord.url,
					"priceWithTax":   map[string]string{"text": ord.priceText},
				})
				return jsonResp(r, 200, string(body)), nil
			case 2:
				switch parts[1] {
				case "status":
					body, _ := json.Marshal(ord.status)
					return jsonResp(r, 200, string(body)), nil
				case "followUp":
					if code := mock.followUpErr[oid]; code != 0 {
						return jsonResp(r, code, `{"message":"beta error"}`), nil
					}
					body, _ := json.Marshal(ord.followUp)
					return jsonResp(r, 200, string(body)), nil
				case "details":
					var ids []int64
					for k := range ord.details {
						var n int64
						fmt.Sscan(k, &n)
						ids = append(ids, n)
					}
					body, _ := json.Marshal(ids)
					return jsonResp(r, 200, string(body)), nil
				}
			case 3:
				if parts[1] == "details" {
					if d, ok := ord.details[parts[2]]; ok {
						return jsonResp(r, 200, d), nil
					}
				}
			}
			return jsonResp(r, 404, `{"message":"unknown"}`), nil
		})}
	}

	return state, &msgs, &mu
}

func takeMessages(msgs *[]string, mu *sync.Mutex) []string {
	mu.Lock()
	defer mu.Unlock()
	out := append([]string(nil), (*msgs)...)
	*msgs = nil
	return out
}

func TestOrderWatchLifecycleSeedingAndTerminalStop(t *testing.T) {
	origNow := nowFunc
	fixedNow := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return fixedNow }
	t.Cleanup(func() { nowFunc = origNow })

	acc := types.OVHAccount{
		ID: "eu1", Name: "主号EU", Endpoint: "ovh-eu", Zone: "IE",
		AppKey: "ak", AppSecret: "as", ConsumerKey: "ck", IsDefault: true,
	}
	mock := &mockOrderState{
		orderIDs:    []string{"100", "200"},
		followUpErr: map[string]int{},
		orders: map[string]mockOrder{
			"100": {
				date:           "2026-11-27T10:00:00+00:00",
				expirationDate: "2026-11-27T11:00:00+00:00",
				url:            "https://www.ovh.com/cgi-bin/order/display-order.cgi?orderId=100",
				priceText:      "€10.00",
				status:         "delivered",
			},
			"200": {
				date:           "2026-11-28T11:50:00+00:00",
				expirationDate: "2026-11-28T12:15:00+00:00",
				url:            "https://www.ovh.com/cgi-bin/order/display-order.cgi?orderId=200",
				priceText:      "€15.99",
				status:         "notPaid",
				details:        map[string]string{"1": `{"description":"KS-LE-1 | GRA","detailType":"DURATION","cancelled":false}`},
				followUp: []FollowUpStep{
					{Step: "VALIDATING", Status: "DOING", History: []FollowUpEvent{{Date: "2026-11-28T11:50:05+00:00", Label: "ORDER_STARTED", Description: "ORDER_STARTED"}}},
					{Step: "VALIDATED", Status: "TODO"},
					{Step: "DELIVERING", Status: "TODO"},
					{Step: "AVAILABLE", Status: "TODO"},
				},
			},
		},
	}

	state, msgs, mu := newTestState(t, acc, mock)

	// 1. 首次基线同步：历史已有订单应静默入库，不推送通知
	RunOnce(context.Background(), state)
	if got := takeMessages(msgs, mu); len(got) != 0 {
		t.Fatalf("expected 0 messages on seed, got %v", got)
	}

	// 2. 新建一笔手动订单（不在 state.History 里）→ 推送「已下单」
	mock.mu.Lock()
	mock.orderIDs = append(mock.orderIDs, "300")
	mock.orders["300"] = mockOrder{
		date:           "2026-11-28T12:01:00+00:00",
		expirationDate: "2026-11-28T12:20:00+00:00",
		url:            "https://www.ovh.com/cgi-bin/order/display-order.cgi?orderId=300",
		priceText:      "€29.99",
		status:         "notPaid",
		details:        map[string]string{"10": `{"description":"RISE-1 | RBX","detailType":"DURATION","cancelled":false}`},
		followUp: []FollowUpStep{
			{Step: "VALIDATING", Status: "DOING", History: []FollowUpEvent{{Date: "2026-11-28T12:01:02+00:00", Label: "ORDER_STARTED", Description: "ORDER_STARTED"}}},
			{Step: "VALIDATED", Status: "TODO"},
			{Step: "DELIVERING", Status: "TODO"},
			{Step: "AVAILABLE", Status: "TODO"},
		},
	}
	mock.mu.Unlock()

	RunOnce(context.Background(), state)
	got := takeMessages(msgs, mu)
	if len(got) != 1 || !strings.Contains(got[0], "已下单") || !strings.Contains(got[0], "300") || !strings.Contains(got[0], "RISE-1 | RBX") {
		t.Fatalf("expected new order push, got %v", got)
	}

	// 3. 订单 300 已付款（checking + PAYMENT_RECEIVED）
	mock.mu.Lock()
	o300 := mock.orders["300"]
	o300.status = "checking"
	o300.followUp[0].History = append(o300.followUp[0].History, FollowUpEvent{Date: "2026-11-28T12:02:00+00:00", Label: "PAYMENT_RECEIVED", Description: "Payment received"})
	mock.orders["300"] = o300
	mock.mu.Unlock()

	RunOnce(context.Background(), state)
	got = takeMessages(msgs, mu)
	if len(got) != 1 || !strings.Contains(got[0], "已付款") || !strings.Contains(got[0], "已收到付款") {
		t.Fatalf("expected paid push, got %v", got)
	}

	// 4. 订单 300 已验证通过（VALIDATED=DONE + ORDER_ACCEPTED）
	mock.mu.Lock()
	o300 = mock.orders["300"]
	o300.followUp[0].Status = "DONE"
	o300.followUp[1].Status = "DONE"
	o300.followUp[1].History = []FollowUpEvent{{Date: "2026-11-28T12:03:00+00:00", Label: "ORDER_ACCEPTED", Description: "Order accepted"}}
	mock.orders["300"] = o300
	mock.mu.Unlock()

	RunOnce(context.Background(), state)
	got = takeMessages(msgs, mu)
	if len(got) != 1 || !strings.Contains(got[0], "已验证") || !strings.Contains(got[0], "订单已接受") {
		t.Fatalf("expected validated push, got %v", got)
	}

	// 5. 订单 300 开始交付（delivering + DELIVERING=DOING + DELIVERY）
	mock.mu.Lock()
	o300 = mock.orders["300"]
	o300.status = "delivering"
	o300.followUp[2].Status = "DOING"
	o300.followUp[2].History = []FollowUpEvent{{Date: "2026-11-28T12:04:00+00:00", Label: "DELIVERY", Description: "Delivery started"}}
	mock.orders["300"] = o300
	mock.mu.Unlock()

	RunOnce(context.Background(), state)
	got = takeMessages(msgs, mu)
	if len(got) != 1 || !strings.Contains(got[0], "开始交付") {
		t.Fatalf("expected delivering push, got %v", got)
	}

	// 6. 订单 300 已交付（delivered + AVAILABLE=DONE）
	mock.mu.Lock()
	o300 = mock.orders["300"]
	o300.status = "delivered"
	o300.followUp[2].Status = "DONE"
	o300.followUp[3].Status = "DONE"
	mock.orders["300"] = o300
	mock.mu.Unlock()

	RunOnce(context.Background(), state)
	got = takeMessages(msgs, mu)
	if len(got) != 1 || !strings.Contains(got[0], "已交付") {
		t.Fatalf("expected delivered push, got %v", got)
	}

	// 7. 已交付后再轮询：应不再查询 300 的状态/followUp，也不应推送
	mock.mu.Lock()
	mock.calls = nil
	mock.mu.Unlock()
	RunOnce(context.Background(), state)
	if got = takeMessages(msgs, mu); len(got) != 0 {
		t.Fatalf("expected 0 pushes without changes, got %v", got)
	}
	mock.mu.Lock()
	for _, c := range mock.calls {
		if strings.Contains(c, "/order/300/") || strings.Contains(c, "/order/100/") {
			t.Fatalf("terminal order should not be polled again, saw call %s", c)
		}
	}
	mock.mu.Unlock()
}

func TestOrderWatchSniperDedupAndFastAutoPayAndFollowUpOutage(t *testing.T) {
	origNow := nowFunc
	fixedNow := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return fixedNow }
	t.Cleanup(func() { nowFunc = origNow })

	acc := types.OVHAccount{
		ID: "eu1", Name: "主号EU", Endpoint: "ovh-eu", Zone: "IE",
		AppKey: "ak", AppSecret: "as", ConsumerKey: "ck", IsDefault: true,
	}
	mock := &mockOrderState{
		orderIDs:    []string{},
		followUpErr: map[string]int{},
		orders:      map[string]mockOrder{},
	}
	state, msgs, mu := newTestState(t, acc, mock)
	// 先完成空基线初始化
	RunOnce(context.Background(), state)

	// 抢购历史里已有未付款订单 401（已发过下单成功通知），且 OVH 侧仍是 notPaid 无进展 → 不重复推「已下单」
	// 抢购历史里已有自动扣款成功的订单 402（首次发现已变成 checking + PAYMENT_RECEIVED）→ 必须推送付款进展
	state.History = []types.PurchaseHistoryEntry{
		{ID: "h1", AccountID: "eu1", PlanCode: "24sk10", Datacenter: "gra", Status: "success", OrderID: "401"},
		{ID: "h2", AccountID: "eu1", PlanCode: "24sk20", Datacenter: "rbx", Status: "success", OrderID: "402"},
	}

	mock.mu.Lock()
	mock.orderIDs = []string{"401", "402"}
	mock.orders["401"] = mockOrder{
		date:           "2026-11-28T12:00:00+00:00",
		expirationDate: "2026-11-28T12:15:00+00:00",
		status:         "notPaid",
		followUp: []FollowUpStep{
			{Step: "VALIDATING", Status: "DOING", History: []FollowUpEvent{{Date: "2026-11-28T12:00:01+00:00", Label: "ORDER_STARTED", Description: "ORDER_STARTED"}}},
		},
	}
	mock.orders["402"] = mockOrder{
		date:           "2026-11-28T12:00:00+00:00",
		expirationDate: "2026-11-28T12:15:00+00:00",
		status:         "checking",
		followUp: []FollowUpStep{
			{Step: "VALIDATING", Status: "DOING", History: []FollowUpEvent{
				{Date: "2026-11-28T12:00:01+00:00", Label: "ORDER_STARTED", Description: "ORDER_STARTED"},
				{Date: "2026-11-28T12:00:10+00:00", Label: "PAYMENT_RECEIVED", Description: "Paid"},
			}},
		},
	}
	mock.mu.Unlock()

	RunOnce(context.Background(), state)
	got := takeMessages(msgs, mu)
	if len(got) != 1 || !strings.Contains(got[0], "402") || !strings.Contains(got[0], "已付款") {
		t.Fatalf("expected only 402 paid notification (401 suppressed), got %v", got)
	}

	// followUp 临时返回 500，然后恢复：不应抹掉已见事件或重复推送 402 的 PAYMENT_RECEIVED
	mock.mu.Lock()
	mock.followUpErr["402"] = 500
	mock.mu.Unlock()
	RunOnce(context.Background(), state)
	if got = takeMessages(msgs, mu); len(got) != 0 {
		t.Fatalf("expected 0 pushes during followUp 500, got %v", got)
	}

	mock.mu.Lock()
	delete(mock.followUpErr, "402")
	mock.mu.Unlock()
	RunOnce(context.Background(), state)
	if got = takeMessages(msgs, mu); len(got) != 0 {
		t.Fatalf("expected 0 duplicate pushes after followUp recovers, got %v", got)
	}

	// 401 超过 expirationDate：触发已过期未付款推送，但不污染 state.History.OrderStatus 为 expired
	fixedNow = fixedNow.Add(20 * time.Minute)
	RunOnce(context.Background(), state)
	got = takeMessages(msgs, mu)
	if len(got) != 1 || !strings.Contains(got[0], "401") || !strings.Contains(got[0], "已过期未付款") {
		t.Fatalf("expected 401 expired notification, got %v", got)
	}
	if state.History[0].OrderStatus != "notPaid" {
		t.Fatalf("history OrderStatus must remain raw notPaid, got %q", state.History[0].OrderStatus)
	}
}

func TestOrderWatchUSRegionConfigToggleAndAccountDelete(t *testing.T) {
	origNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = origNow })

	acc := types.OVHAccount{
		ID: "us1", Name: "美区", Endpoint: "ovh-us", Zone: "US",
		AppKey: "ak", AppSecret: "as", ConsumerKey: "ck", IsDefault: true,
	}
	mock := &mockOrderState{
		orderIDs:    []string{"500"},
		followUpErr: map[string]int{},
		orders: map[string]mockOrder{
			"500": {
				date:           "2026-11-28T11:00:00+00:00",
				expirationDate: "2026-11-28T13:00:00+00:00",
				status:         "notPaid",
			},
		},
	}
	state, msgs, mu := newTestState(t, acc, mock)

	// 种子阶段应不带 date.from 且不请求 /details
	RunOnce(context.Background(), state)
	mock.mu.Lock()
	for _, c := range mock.calls {
		if strings.Contains(c, "date.from") {
			t.Fatalf("US account must not use date.from, saw %s", c)
		}
		if strings.Contains(c, "/details") {
			t.Fatalf("US account must not call /details, saw %s", c)
		}
	}
	mock.calls = nil
	mock.mu.Unlock()

	// 关闭开关：零 OVH 请求、零推送
	disabled := false
	cfg := state.Config.Get()
	cfg.NotifyOrderUpdates = &disabled
	if err := state.Config.Set(cfg); err != nil {
		t.Fatalf("disable config: %v", err)
	}
	mock.mu.Lock()
	o500 := mock.orders["500"]
	o500.status = "delivered"
	mock.orders["500"] = o500
	mock.mu.Unlock()

	RunOnce(context.Background(), state)
	if got := takeMessages(msgs, mu); len(got) != 0 {
		t.Fatalf("expected 0 pushes when disabled, got %v", got)
	}
	mock.mu.Lock()
	if len(mock.calls) != 0 {
		t.Fatalf("expected 0 OVH calls when disabled, got %v", mock.calls)
	}
	mock.mu.Unlock()

	// 删除账户应级联清理 order_watch_accounts 与 order_watch_orders
	if err := state.DB.DeleteAccount("us1"); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	seeded, err := state.DB.IsOrderWatchAccountSeeded("us1")
	if err != nil || seeded {
		t.Fatalf("expected seeded cleared after delete, seeded=%v err=%v", seeded, err)
	}
	snaps, err := state.DB.ListOrderWatchSnapshots("us1")
	if err != nil || len(snaps) != 0 {
		t.Fatalf("expected snapshots cleared after delete, len=%d err=%v", len(snaps), err)
	}
}

func TestSettingsDefaultOffAndPreserveNotifyOrderUpdates(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	cfgStore := config.New(database)
	if cfgStore.OrderUpdateNotifyEnabled() || cfgStore.OrderExpiryNotifyEnabled() {
		t.Fatal("order update and expiry notifications must be off by default")
	}
	on := true
	c := cfgStore.Get()
	c.NotifyOrderUpdates = &on
	if err := cfgStore.Set(c); err != nil {
		t.Fatal(err)
	}
	reloaded := config.New(database)
	if !reloaded.OrderUpdateNotifyEnabled() || reloaded.OrderExpiryNotifyEnabled() {
		t.Fatal("reloaded config must preserve the enabled switch and keep expiry off")
	}
	_ = atomic.Int32{}
}

// resetThrottle 节流时间按账户/订单记在包级变量里,各测试互不影响。
func resetThrottle() {
	throttle.Lock()
	throttle.listed = map[string]time.Time{}
	throttle.polled = map[string]time.Time{}
	throttle.Unlock()
}
