package purchase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

// 独立 worker 应同时派发整批,第一个慢任务不能挡住第 11 个及之后的任务。
func TestQueueDoesNotWaitForBatchBarrier(t *testing.T) {
	state := queueCompletionState(t)
	for i := 0; i < 16; i++ {
		state.Queue = append(state.Queue, types.QueueItem{ID: fmt.Sprint(i), PlanCode: "plan", Datacenter: "gra", Status: "running", RetryInterval: 60})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan string, 16)
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		processQueueLoop(ctx, state, func(ctx context.Context, _ *app.State, item *types.QueueItem) Outcome {
			entered <- item.ID
			select {
			case <-release:
				return Outcome{Success: true}
			case <-ctx.Done():
				return Outcome{Cancelled: true}
			}
		})
	}()
	defer func() { cancel(); <-done }()
	seen := map[string]bool{}
	for i := 0; i < 16; i++ {
		select {
		case id := <-entered:
			if seen[id] {
				t.Fatal("same task concurrently dispatched", id)
			}
			seen[id] = true
		case <-time.After(3 * time.Second):
			t.Fatal("a slow batch blocked later tasks", len(seen))
		}
	}
	close(release)
}

// 暂停空闲 worker 后,即使收到迟到库存信号也不能再次调用购买入口。
func TestPausedWorkerRejectsLateStockSignal(t *testing.T) {
	state := queueCompletionState(t)
	state.Queue = []types.QueueItem{{ID: "paused", PlanCode: "plan", Datacenter: "gra", Status: "running", RetryInterval: 60}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		processQueueLoop(ctx, state, func(context.Context, *app.State, *types.QueueItem) Outcome {
			if calls.Add(1) == 1 {
				close(started)
			}
			return Outcome{WaitingForStock: true}
		})
	}()
	defer func() { cancel(); <-done }()
	<-started
	state.QueueMu.Lock()
	state.Queue[0].Status = "paused"
	state.UnregisterTaskCancel("paused")
	state.QueueMu.Unlock()
	state.StockSignals.Publish(app.StockSignal{PlanCode: "plan", Variants: []map[string]interface{}{{"datacenters": []interface{}{map[string]interface{}{"datacenter": "gra", "availability": "1H-low"}}}}, DetectedAt: time.Now()})
	time.Sleep(120 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatal("pause ignored", calls.Load())
	}
}

// hotFixture 所有 OVH 写入都到本机假 API;公开目录也被离线 transport 截获。
func hotFixture(t *testing.T) (*app.State, *atomic.Int32, *atomic.Int32, *atomic.Bool) {
	t.Helper()
	state := queueCompletionState(t)
	var carts, checkouts atomic.Int32
	var stock atomic.Bool
	var itemsMu sync.Mutex
	cartItems := map[string]map[int64]map[string]interface{}{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) >= 3 && parts[0] == "order" && parts[1] == "cart" {
			cartID := parts[2]
			if r.Method == "POST" && (strings.HasSuffix(path, "/eco") || strings.HasSuffix(path, "/eco/options")) {
				var payload struct {
					Duration    string `json:"duration"`
					PlanCode    string `json:"planCode"`
					PricingMode string `json:"pricingMode"`
					ParentID    int64  `json:"itemId"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				itemsMu.Lock()
				defer itemsMu.Unlock()
				if cartItems[cartID] == nil {
					cartItems[cartID] = map[int64]map[string]interface{}{}
				}
				id := int64(len(cartItems[cartID]) + 1)
				item := map[string]interface{}{"itemId": id, "duration": payload.Duration, "options": []int64{}, "settings": map[string]interface{}{"planCode": payload.PlanCode, "pricingMode": payload.PricingMode}}
				cartItems[cartID][id] = item
				if parent := cartItems[cartID][payload.ParentID]; parent != nil {
					parent["options"] = append(parent["options"].([]int64), id)
				}
				_ = json.NewEncoder(w).Encode(item)
				return
			}
			if r.Method == "GET" && len(parts) == 3 {
				itemsMu.Lock()
				defer itemsMu.Unlock()
				ids := []int64{}
				for id := range cartItems[cartID] {
					ids = append(ids, id)
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"cartId": cartID, "items": ids})
				return
			}
			if r.Method == "GET" && len(parts) == 5 && parts[3] == "item" {
				id, _ := strconv.ParseInt(parts[4], 10, 64)
				itemsMu.Lock()
				defer itemsMu.Unlock()
				item := cartItems[cartID][id]
				if item == nil {
					w.WriteHeader(404)
					return
				}
				_ = json.NewEncoder(w).Encode(item)
				return
			}
		}
		switch {
		case strings.HasSuffix(path, "/auth/time"):
			fmt.Fprint(w, time.Now().Unix())
		case strings.HasSuffix(path, "/availabilities"):
			status := "unavailable"
			if stock.Load() {
				status = "1H-low"
			}
			fmt.Fprintf(w, `[{"fqn":"plan.ram-32g","datacenters":[{"datacenter":"gra","availability":%q}]}]`, status)
		case path == "/order/cart" && r.Method == "POST":
			id := carts.Add(1)
			fmt.Fprintf(w, `{"cartId":"cart-%d","expire":"2099-01-01T00:00:00Z"}`, id)
		case strings.HasSuffix(path, "/requiredConfiguration"):
			fmt.Fprint(w, `[{"label":"region","required":true,"allowedValues":["europe"]}]`)
		case strings.HasSuffix(path, "/eco") && r.Method == "GET":
			if stock.Load() {
				fmt.Fprint(w, `[{"planCode":"plan","prices":[{"duration":"P1M","pricingMode":"default","price":{"value":30,"currencyCode":"EUR"}}]}]`)
			} else {
				fmt.Fprint(w, `[]`)
			}
		case strings.HasSuffix(path, "/eco/options") && r.Method == "GET":
			fmt.Fprint(w, `[{"planCode":"ram-32g","prices":[{"duration":"P1M","pricingMode":"default","price":{"value":30,"currencyCode":"EUR"}}]}]`)
		case strings.HasSuffix(path, "/summary") && r.Method == "GET":
			fmt.Fprint(w, `{"prices":{"withTax":{"value":39.98,"currencyCode":"EUR"}}}`)
		case strings.HasSuffix(path, "/checkout"):
			if !stock.Load() {
				t.Error("preparation created an order before stock")
			}
			checkouts.Add(1)
			fmt.Fprint(w, `{"orderId":123}`)
		case path == "/me/order/123":
			fmt.Fprint(w, `{"orderId":123}`)
		case path == "/me/order/123/status":
			fmt.Fprint(w, `"notPaid"`)
		case strings.HasSuffix(path, "/assign") || strings.HasSuffix(path, "/configuration") || strings.HasSuffix(path, "/eco/options") || r.Method == "DELETE":
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected local API operation %s %s", r.Method, path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	original := http.DefaultTransport
	http.DefaultTransport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Path, "/order/catalog/public/eco") {
			return nil, fmt.Errorf("unexpected outbound public request %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"locale":{"currencyCode":"EUR"},"plans":[{"planCode":"plan","pricings":[{"mode":"default","intervalUnit":"month","interval":1,"price":3998000000}]}],"addons":[{"planCode":"ram-32g","pricings":[{"mode":"default","intervalUnit":"month","interval":1,"price":0}]}]}`)), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	state.Accounts = []types.OVHAccount{{ID: "hot-eu", Endpoint: server.URL, Zone: "IE", AppKey: "fixture", AppSecret: "fixture", ConsumerKey: "fixture"}}
	return state, &carts, &checkouts, &stock
}

// 已预装的任务接到库存信号后只能发 checkout,不会重新建车、查库存或改硬件。
func TestPreparedCartIsReusedOnStockSignal(t *testing.T) {
	state, carts, checkouts, stock := hotFixture(t)
	item := types.QueueItem{ID: "warm", AccountID: "hot-eu", PlanCode: "plan", Datacenter: "gra", Options: []string{"ram-32g"}, Status: "running"}
	session := &cartSession{}
	ctx := context.WithValue(context.Background(), cartSessionKey{}, session)
	defer session.discard()
	first := PurchaseServer(ctx, state, &item)
	if !first.WaitingForStock || carts.Load() != 1 || checkouts.Load() != 0 || session.cart == nil {
		t.Fatal(first, carts.Load(), checkouts.Load())
	}
	stock.Store(true)
	signal := app.StockSignal{AccountID: "hot-eu", PlanCode: "plan", DetectedAt: time.Now(), Variants: []map[string]interface{}{{"fqn": "plan.ram-32g", "datacenters": []interface{}{map[string]interface{}{"datacenter": "gra", "availability": "1H-low"}}}}}
	outcome := PurchaseServer(context.WithValue(ctx, stockSignalKey{}, signal), state, &item)
	if !outcome.Success || carts.Load() != 1 || checkouts.Load() != 1 {
		t.Fatal(outcome, carts.Load(), checkouts.Load())
	}
	timing := LastTimings()["warm"]
	if timing.Dispatch == nil || !timing.Dispatch.WroteRequest || !timing.Dispatch.Prewarmed || !timing.Dispatch.ConnectionReused {
		t.Fatal(timing)
	}
	t.Logf("prepared full purchase dispatch over real local TCP: %dus", timing.Dispatch.DetectionToWriteUS)
}

func TestPreparedSessionCannotKeepOldCouponOrHardwareIntent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*types.QueueItem)
	}{
		{"coupon", func(i *types.QueueItem) { i.Coupon = "discount" }},
		{"hardware", func(i *types.QueueItem) { i.Options = []string{"ram-64g"} }},
		{"model", func(i *types.QueueItem) { i.PlanCode = "other" }},
		{"datacenter", func(i *types.QueueItem) { i.Datacenter = "bhs" }},
		{"period", func(i *types.QueueItem) { i.Duration = "P12M" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, _, checkouts, _ := hotFixture(t)
			item := poolItem()
			session := &cartSession{}
			ctx := context.WithValue(context.Background(), cartSessionKey{}, session)
			defer session.discard()
			if out := PurchaseServer(ctx, state, &item); !out.WaitingForStock || session.cart == nil {
				t.Fatal("no prepared cart", out)
			}
			old := session.cart
			tc.change(&item)
			ctx, cancel := context.WithCancel(ctx)
			cancel() // Reject new network operations; invalidation must already remove the old intent.
			_ = PurchaseServer(ctx, state, &item)
			if session.cart == old || checkouts.Load() != 0 {
				t.Fatal("old prepared intent remained usable")
			}
		})
	}
}

func TestPreparedPreorderDoesNotPinUnselectedHardwareWhenStockVariantChanges(t *testing.T) {
	state, carts, checkouts, stock := hotFixture(t)
	base, err := state.OVH.ClientFor("hot-eu")
	if err != nil {
		t.Fatal(err)
	}
	original := base.Client.Transport
	base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		if stock.Load() {
			switch {
			case strings.HasSuffix(r.URL.Path, "/availabilities"):
				body = `[{"fqn":"plan.ram-64g","datacenters":[{"datacenter":"gra","availability":"1H-low"}]}]`
			case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco/options"):
				body = `[{"planCode":"ram-64g","prices":[{"duration":"P1M","pricingMode":"default","price":{"value":30,"currencyCode":"EUR"}}]}]`
			}
		}
		if body != "" {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
		return original.RoundTrip(r)
	})
	item := poolItem()
	item.Preorder, item.Options = true, nil
	session := &cartSession{}
	defer session.discard()
	ctx := context.WithValue(context.Background(), cartSessionKey{}, session)
	if out := PurchaseServer(ctx, state, &item); !out.WaitingForCatalog || session.cart == nil {
		t.Fatal(out)
	}
	stock.Store(true)
	signal := app.StockSignal{AccountID: item.AccountID, PlanCode: item.PlanCode, DetectedAt: time.Now(), Variants: []map[string]interface{}{{"fqn": "plan.ram-64g", "datacenters": []interface{}{map[string]interface{}{"datacenter": "gra", "availability": "1H-low"}}}}}
	out := PurchaseServer(context.WithValue(ctx, stockSignalKey{}, signal), state, &item)
	if !out.Success || carts.Load() != 2 || checkouts.Load() != 1 {
		t.Fatal("old inferred hardware became an explicit constraint", out, carts.Load(), checkouts.Load())
	}
}

// 配置过期或账户出站 client 失效时,旧购物车不得被继续使用。
func TestExpiredCartAndChangedAccountAreRebuilt(t *testing.T) {
	for _, invalidate := range []bool{false, true} {
		t.Run(fmt.Sprint(invalidate), func(t *testing.T) {
			state, carts, checkouts, _ := hotFixture(t)
			item := types.QueueItem{ID: "expire", AccountID: "hot-eu", PlanCode: "plan", Datacenter: "gra", Options: []string{"ram-32g"}}
			session := &cartSession{}
			ctx := context.WithValue(context.Background(), cartSessionKey{}, session)
			defer session.discard()
			if !PurchaseServer(ctx, state, &item).WaitingForStock {
				t.Fatal("not prepared")
			}
			if !invalidate {
				session.nextPrepare = time.Time{}
			}
			if invalidate {
				state.OVH.Invalidate("hot-eu")
			} else {
				session.cart.expires = time.Now().Add(-time.Second)
			}
			if !PurchaseServer(ctx, state, &item).WaitingForStock || carts.Load() != 2 || checkouts.Load() != 0 {
				t.Fatal("stale preparation reused", carts.Load(), checkouts.Load())
			}
		})
	}
}

// 预装过程的单次 API 错误不得消耗真实下单失败次数或立即重复造车。
func TestPreparationDoesNotWriteFailedOrderHistory(t *testing.T) {
	state, _, _, _ := hotFixture(t)
	base, err := state.OVH.ClientFor("hot-eu")
	if err != nil {
		t.Fatal(err)
	}
	original := base.Client.Transport
	base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" && r.URL.Path == "/order/cart" {
			return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"out of stock"}`)), Request: r}, nil
		}
		return original.RoundTrip(r)
	})
	session := &cartSession{}
	ctx := context.WithValue(context.Background(), cartSessionKey{}, session)
	defer session.discard()
	item := types.QueueItem{ID: "prep-fail", AccountID: "hot-eu", PlanCode: "plan", Datacenter: "gra"}
	for i := 0; i < 2; i++ {
		outcome := PurchaseServer(ctx, state, &item)
		if !outcome.WaitingForStock || outcome.Attempted || outcome.Fatal {
			t.Fatal(outcome)
		}
	}
	rows, err := state.DB.ListHistory()
	if err != nil || len(rows) != 0 {
		encoded, _ := json.Marshal(rows)
		t.Fatal(err, string(encoded))
	}
}

// 基准并行回调只验证状态隔离,真实网络派发分位数由 ovh 包的 TCP 测试给出。
func TestIndependentWorkersNeverUseTheSameTaskConcurrently(t *testing.T) {
	state := queueCompletionState(t)
	state.Queue = []types.QueueItem{{ID: "one", PlanCode: "plan", Status: "running", RetryInterval: 1}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	entered := make(chan struct{})
	var active, max atomic.Int32
	var once sync.Once
	go func() {
		defer close(done)
		processQueueLoop(ctx, state, func(ctx context.Context, _ *app.State, _ *types.QueueItem) Outcome {
			n := active.Add(1)
			defer active.Add(-1)
			max.Store(n)
			once.Do(func() { close(entered) })
			<-ctx.Done()
			return Outcome{Cancelled: true}
		})
	}()
	<-entered
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done
	if max.Load() != 1 {
		t.Fatal("overlapping attempts")
	}
}

// 从库存发布到真实 TCP 写出的完整 worker 链路,无需等待 60 秒重试定时器。
func TestQueueSignalDispatchesPrebuiltCheckoutImmediately(t *testing.T) {
	state, carts, checkouts, stock := hotFixture(t)
	state.Queue = []types.QueueItem{{ID: "signal-worker", AccountID: "hot-eu", PlanCode: "plan", Datacenter: "gra", Options: []string{"ram-32g"}, Status: "running", RetryInterval: 60}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); processQueueLoop(ctx, state, PurchaseServer) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(4 * time.Second)
	for LastTimings()["signal-worker"].Outcome != "unavailable" {
		if time.Now().After(deadline) {
			t.Fatal("cart was not prebuilt")
		}
		time.Sleep(time.Millisecond)
	}
	stock.Store(true)
	detected := time.Now()
	state.StockSignals.Publish(app.StockSignal{AccountID: "hot-eu", PlanCode: "plan", DetectedAt: detected, Variants: []map[string]interface{}{{"fqn": "plan.ram-32g", "datacenters": []interface{}{map[string]interface{}{"datacenter": "gra", "availability": "1H-low"}}}}})
	for LastTimings()["signal-worker"].Outcome != "ordered" {
		if time.Since(detected) > 2*time.Second {
			t.Fatal("signal waited for retry timer")
		}
		time.Sleep(time.Millisecond)
	}
	timing := LastTimings()["signal-worker"]
	if carts.Load() != 1 || checkouts.Load() != 1 || timing.Dispatch == nil || !timing.Dispatch.WroteRequest || !timing.Dispatch.Prewarmed {
		t.Fatal(carts.Load(), checkouts.Load(), timing)
	}
	t.Logf("channel + worker + prepared checkout + local TCP write: %dus reused=%t", timing.Dispatch.DetectionToWriteUS, timing.Dispatch.ConnectionReused)
}
