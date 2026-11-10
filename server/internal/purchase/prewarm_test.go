package purchase

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

func waitWarm(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("preparation did not reach expected state")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func poolItem() types.QueueItem {
	return types.QueueItem{ID: "pooled", AccountID: "hot-eu", PlanCode: "plan", Datacenter: "gra", Options: []string{"ram-32g"}, Status: "running"}
}

func TestPoolPreparationIsReadOnlyAndConsumedOnce(t *testing.T) {
	state, carts, checkouts, _ := hotFixture(t)
	pool := NewCartPool(state)
	state.OrderPrewarm = pool
	t.Cleanup(pool.Close)
	item := poolItem()
	if err := pool.Prepare("web:one", []types.QueueItem{item}, 2*time.Minute, nil); err != nil {
		t.Fatal(err)
	}
	waitWarm(t, func() bool { return pool.Ready(item) })
	if quote := pool.Quote(item); quote == nil || quote.Prices["withTax"] != 39.98 || quote.Prices["currencyCode"] != "EUR" {
		t.Fatal("confirmed price was not retained", quote)
	}
	if carts.Load() != 1 || checkouts.Load() != 0 || len(state.Queue) != 0 || len(state.History) != 0 {
		t.Fatal("preparation ordered, enqueued or recorded failure")
	}
	for _, mutate := range []func(*types.QueueItem){
		func(i *types.QueueItem) { i.AccountID = "other" },
		func(i *types.QueueItem) { i.Datacenter = "bhs" },
		func(i *types.QueueItem) { i.PlanCode = "other" },
		func(i *types.QueueItem) { i.Options = []string{"ram-64g"} },
		func(i *types.QueueItem) { i.Coupon = "discount" },
		func(i *types.QueueItem) { i.Duration = "P12M" },
		func(i *types.QueueItem) { i.Preorder = true },
	} {
		other := item
		mutate(&other)
		if pool.Ready(other) {
			t.Fatalf("mismatched cart accepted: %+v", other)
		}
	}
	var taken atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 24; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cart := pool.Take(item); cart != nil {
				taken.Add(1)
				cart.cleanup()
			}
		}()
	}
	wg.Wait()
	if taken.Load() != 1 || pool.Ready(item) || checkouts.Load() != 0 {
		t.Fatal("cart consumed more than once", taken.Load())
	}
}

func TestOutOfStockSummaryRetainsCartWithExplicitCatalogEstimate(t *testing.T) {
	for _, available := range []bool{false, true} {
		t.Run(http.StatusText(400)+"-stock-"+map[bool]string{false: "absent", true: "present"}[available], func(t *testing.T) {
			state, carts, checkouts, stock := hotFixture(t)
			stock.Store(available)
			base, err := state.OVH.ClientFor("hot-eu")
			if err != nil {
				t.Fatal(err)
			}
			original := base.Client.Transport
			base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/summary") {
					return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"plan.ram-32g is not available in gra"}`)), Request: r}, nil
				}
				return original.RoundTrip(r)
			})
			pool := NewCartPool(state)
			state.OrderPrewarm = pool
			t.Cleanup(pool.Close)
			item := poolItem()
			if err = pool.Prepare("web:out-of-stock", []types.QueueItem{item}, time.Minute, nil); err != nil {
				t.Fatal(err)
			}
			if available {
				waitWarm(t, func() bool { s := pool.Status("web:out-of-stock"); return len(s) == 1 && s[0].State == "waiting" })
				if pool.Ready(item) || checkouts.Load() != 0 {
					t.Fatal("rejected available configuration bypassed verification")
				}
				return
			}
			waitWarm(t, func() bool { return pool.Ready(item) })
			if status := pool.Status("web:out-of-stock"); len(status) != 1 || !status[0].Estimated || checkouts.Load() != 0 {
				t.Fatal(status)
			}
			stock.Store(true)
			signal := app.StockSignal{AccountID: item.AccountID, PlanCode: item.PlanCode, DetectedAt: time.Now(), Variants: []map[string]interface{}{{"fqn": "plan.ram-32g", "datacenters": []interface{}{map[string]interface{}{"datacenter": "gra", "availability": "1H-low"}}}}}
			out := PurchaseServer(context.WithValue(context.Background(), stockSignalKey{}, signal), state, &item)
			if !out.Success || carts.Load() != 1 || checkouts.Load() != 1 {
				t.Fatal("out-of-stock cart was discarded instead of reused", out, carts.Load(), checkouts.Load())
			}
		})
	}
}

func TestPoolFeedsCheckoutWithoutRebuildingCart(t *testing.T) {
	state, carts, checkouts, stock := hotFixture(t)
	pool := NewCartPool(state)
	state.OrderPrewarm = pool
	t.Cleanup(pool.Close)
	item := poolItem()
	if err := pool.Prepare("monitor:ready", []types.QueueItem{item}, 0, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	waitWarm(t, func() bool { return pool.Ready(item) })
	stock.Store(true)
	signal := app.StockSignal{AccountID: item.AccountID, PlanCode: item.PlanCode, DetectedAt: time.Now(), Variants: []map[string]interface{}{{"fqn": "plan.ram-32g", "datacenters": []interface{}{map[string]interface{}{"datacenter": "gra", "availability": "1H-low"}}}}}
	out := PurchaseServer(context.WithValue(context.Background(), stockSignalKey{}, signal), state, &item)
	if !out.Success || carts.Load() != 1 || checkouts.Load() != 1 {
		t.Fatal(out, carts.Load(), checkouts.Load())
	}
	dispatch := LastTimings()[item.ID].Dispatch
	if dispatch == nil || !dispatch.Prewarmed || !dispatch.ConnectionReused || !dispatch.WroteRequest {
		t.Fatal(dispatch)
	}
	t.Logf("shared prewarm pool detection → local TCP write: %dµs", dispatch.DetectionToWriteUS)
}

func TestPoolExpiryCancellationAndAccountInvalidation(t *testing.T) {
	state, carts, checkouts, _ := hotFixture(t)
	pool := NewCartPool(state)
	state.OrderPrewarm = pool
	t.Cleanup(pool.Close)
	item := poolItem()
	if err := pool.Prepare("web:expiry", []types.QueueItem{item}, 45*time.Minute, nil); err != nil {
		t.Fatal(err)
	}
	waitWarm(t, func() bool { return pool.Ready(item) })
	// A late two-minute heartbeat cannot undo an explicit standby lease.
	if err := pool.Prepare("web:expiry", []types.QueueItem{item}, 2*time.Minute, nil); err != nil {
		t.Fatal(err)
	}
	if status := pool.Status("web:expiry"); len(status) != 1 || !status[0].Standby {
		t.Fatal(status)
	}
	pool.mu.Lock()
	pool.entries["web:expiry"][0].cart.expires = time.Now().Add(-time.Second)
	pool.mu.Unlock()
	if pool.Ready(item) {
		t.Fatal("expired cart accepted")
	}
	pool.maintain()
	waitWarm(t, func() bool { return pool.Ready(item) && carts.Load() == 2 })
	state.AccountsMu.Lock()
	state.Accounts[0].Zone = "FR"
	state.AccountsMu.Unlock()
	if pool.Ready(item) {
		t.Fatal("old account configuration accepted")
	}
	pool.maintain()
	if len(pool.Status("")) != 0 || checkouts.Load() != 0 {
		t.Fatal("invalidated preparation remained")
	}
	if err := pool.Prepare("web:expiry", []types.QueueItem{item}, time.Minute, nil); err != nil {
		t.Fatal("account change prevented fresh preparation", err)
	}
	waitWarm(t, func() bool { return pool.Ready(item) })
	pool.mu.Lock()
	pool.entries["web:expiry"][0].lease = time.Now().Add(-time.Second)
	pool.mu.Unlock()
	pool.maintain()
	if err := pool.Prepare("web:expiry", []types.QueueItem{item}, time.Minute, nil); err != nil {
		t.Fatal("expired lease could not be renewed", err)
	}
	pool.Cancel("web:late")
	if err := pool.Prepare("web:late", []types.QueueItem{item}, time.Minute, nil); err == nil {
		t.Fatal("cancelled request resurrected")
	}
}

func TestPoolBuildsIndependentCartsConcurrentlyWithABound(t *testing.T) {
	state, _, checkouts, _ := hotFixture(t)
	base, err := state.OVH.ClientFor("hot-eu")
	if err != nil {
		t.Fatal(err)
	}
	original := base.Client.Transport
	entered := make(chan struct{}, 20)
	release := make(chan struct{})
	var active, peak atomic.Int32
	base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/order/cart" && r.Method == http.MethodPost {
			n := active.Add(1)
			for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
			}
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				active.Add(-1)
				return nil, r.Context().Err()
			}
			active.Add(-1)
		}
		return original.RoundTrip(r)
	})
	pool := NewCartPool(state)
	state.OrderPrewarm = pool
	t.Cleanup(pool.Close)
	items := make([]types.QueueItem, 20)
	for i := range items {
		items[i] = poolItem()
	}
	if err = pool.Prepare("web:parallel", items, time.Minute, nil); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 8; n++ {
		select {
		case <-entered:
		case <-time.After(4 * time.Second):
			t.Fatal("independent carts serialized", n)
		}
	}
	if peak.Load() > 8 {
		t.Fatal("unbounded cart creation", peak.Load())
	}
	close(release)
	waitWarm(t, func() bool { s := pool.Status("web:parallel"); return len(s) == 1 && s[0].Ready == 20 })
	if checkouts.Load() != 0 || peak.Load() != 8 {
		t.Fatal(checkouts.Load(), peak.Load())
	}
}

func TestPoolCancellationDuringPreparation(t *testing.T) {
	state, _, checkouts, _ := hotFixture(t)
	base, err := state.OVH.ClientFor("hot-eu")
	if err != nil {
		t.Fatal(err)
	}
	original := base.Client.Transport
	entered := make(chan struct{})
	var deleted atomic.Int32
	base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/summary") {
			close(entered)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		if r.Method == http.MethodDelete {
			deleted.Add(1)
		}
		return original.RoundTrip(r)
	})
	pool := NewCartPool(state)
	state.OrderPrewarm = pool
	t.Cleanup(pool.Close)
	if err = pool.Prepare("web:cancel", []types.QueueItem{poolItem()}, time.Minute, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(4 * time.Second):
		t.Fatal("summary not reached")
	}
	pool.Cancel("web:cancel")
	waitWarm(t, func() bool { return deleted.Load() == 1 })
	if len(pool.Status("")) != 0 || checkouts.Load() != 0 {
		t.Fatal("cancelled preparation published")
	}
}

func TestPoolRejectsUnconfirmedPrice(t *testing.T) {
	state, _, checkouts, _ := hotFixture(t)
	base, err := state.OVH.ClientFor("hot-eu")
	if err != nil {
		t.Fatal(err)
	}
	original := base.Client.Transport
	base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		response, err := original.RoundTrip(r)
		if err == nil && strings.HasSuffix(r.URL.Path, "/summary") {
			response.Body.Close()
			response.Body = io.NopCloser(strings.NewReader(`{"prices":{"withTax":{"value":0,"currencyCode":"EUR"}}}`))
		}
		return response, err
	})
	pool := NewCartPool(state)
	state.OrderPrewarm = pool
	t.Cleanup(pool.Close)
	item := poolItem()
	if err = pool.Prepare("web:price", []types.QueueItem{item}, time.Minute, nil); err != nil {
		t.Fatal(err)
	}
	waitWarm(t, func() bool { s := pool.Status("web:price"); return len(s) == 1 && s[0].State == "waiting" })
	if pool.Ready(item) || checkouts.Load() != 0 {
		t.Fatal("invalid price considered ready")
	}
}

func TestPreparedCheckoutExpiredCartFallsBackButUnknownResultDoesNot(t *testing.T) {
	for _, code := range []int{404, 504} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			state, carts, checkouts, stock := hotFixture(t)
			base, err := state.OVH.ClientFor("hot-eu")
			if err != nil {
				t.Fatal(err)
			}
			original := base.Client.Transport
			var sends atomic.Int32
			base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/checkout") && sends.Add(1) == 1 {
					return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"message":"cart expired"}`)), Request: r}, nil
				}
				return original.RoundTrip(r)
			})
			pool := NewCartPool(state)
			state.OrderPrewarm = pool
			t.Cleanup(pool.Close)
			item := poolItem()
			if err = pool.Prepare("web:fallback", []types.QueueItem{item}, time.Minute, nil); err != nil {
				t.Fatal(err)
			}
			waitWarm(t, func() bool { return pool.Ready(item) })
			stock.Store(true)
			signal := app.StockSignal{AccountID: item.AccountID, PlanCode: item.PlanCode, DetectedAt: time.Now(), Variants: []map[string]interface{}{{"fqn": "plan.ram-32g", "datacenters": []interface{}{map[string]interface{}{"datacenter": "gra", "availability": "1H-low"}}}}}
			ctx := context.WithValue(context.Background(), stockSignalKey{}, signal)
			first := PurchaseServer(ctx, state, &item)
			if code == 404 {
				if !first.RetryImmediately || first.PauseTask {
					t.Fatal(first)
				}
				second := PurchaseServer(ctx, state, &item)
				if !second.Success || carts.Load() != 2 || sends.Load() != 2 || checkouts.Load() != 1 {
					t.Fatal(second, carts.Load(), sends.Load(), checkouts.Load())
				}
			} else {
				if first.RetryImmediately || !first.PauseTask {
					t.Fatal(first)
				}
				second := PurchaseServer(ctx, state, &item)
				if !second.PauseTask || sends.Load() != 1 || carts.Load() != 1 {
					t.Fatal("uncertain checkout was retried", second, sends.Load())
				}
			}
		})
	}
}

func TestPoolRefreshKeepsUsableOldCartUntilReplacementIsReady(t *testing.T) {
	state, carts, _, _ := hotFixture(t)
	pool := NewCartPool(state)
	state.OrderPrewarm = pool
	t.Cleanup(pool.Close)
	item := poolItem()
	if err := pool.Prepare("monitor:refresh", []types.QueueItem{item}, 0, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	waitWarm(t, func() bool { return pool.Ready(item) })
	pool.mu.Lock()
	entry := pool.entries["monitor:refresh"][0]
	old := entry.cart
	old.expires = time.Now().Add(20 * time.Second)
	pool.mu.Unlock()
	pool.maintain()
	if !pool.Ready(item) {
		t.Fatal("refresh discarded a still-usable cart")
	}
	waitWarm(t, func() bool { pool.mu.Lock(); defer pool.mu.Unlock(); return entry.cart != nil && entry.cart != old })
	if carts.Load() != 2 || !pool.Ready(item) {
		t.Fatal("replacement was not published", carts.Load())
	}
}

func TestConfigurationKeepsSelectedAddonsAndCompletesStockHardware(t *testing.T) {
	options := configurationOptions("plan.ram-32g.softraid-2x512nvme", []string{"ram-32g-plan", "bandwidth-1000"})
	if strings.Join(options, ",") != "ram-32g-plan,bandwidth-1000,softraid-2x512nvme" {
		t.Fatal(options)
	}
}
