package purchase

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/types"
)

type launchFixtureState struct {
	state                                                     *app.State
	item                                                      types.QueueItem
	baseOpen, optionsOpen                                     atomic.Bool
	rejectStock                                               atomic.Bool
	stockReads                                                atomic.Int32
	checkoutStatus, sent, basePosts, optionPosts, configPosts atomic.Int32
	options                                                   string
	stockNow                                                  atomic.Value
}

// Every write is handled by the local hotFixture server; outbound public reads are intercepted.
func saleLaunchFixture(t *testing.T, period, stockStatus string) *launchFixtureState {
	t.Helper()
	state, _, orders, stock := hotFixture(t)
	stock.Store(true) // The cart API accepts orders even when the separately mocked stock feed lags.
	h := &launchFixtureState{state: state, item: types.QueueItem{ID: "sale-launch", AccountID: "hot-eu", PlanCode: "plan", Datacenter: "gra", Status: "running", Preorder: true, Duration: period, RetryInterval: 60, Options: []string{"ram-32g-ddr4-ecc-2400", "softraid-2x450nvme"}}}
	base, err := state.OVH.ClientFor(h.item.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	original := base.Client.Transport
	base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		body := ""
		status := 200
		switch {
		case strings.HasSuffix(path, "/availabilities"):
			h.stockReads.Add(1)
			if h.rejectStock.Load() {
				return testSaleResponse(r, 503, `{"message":"stock feed unavailable"}`), nil
			}
			current := stockStatus
			if v, ok := h.stockNow.Load().(string); ok {
				current = v
			}
			body = fmt.Sprintf(`[{"fqn":"plan.ram-32g-ddr4-ecc-2400.softraid-2x450nvme","datacenters":[{"datacenter":"gra","availability":%q},{"datacenter":"fra","availability":%q}]}]`, current, current)
		case strings.HasSuffix(path, "/eco") && r.Method == "GET":
			body = `[]`
			if h.baseOpen.Load() {
				body = fmt.Sprintf(`[{"planCode":"plan","prices":[{"duration":%q,"pricingMode":"default","price":{"value":30,"currencyCode":"EUR"}}]}]`, period)
			}
		case strings.HasSuffix(path, "/eco/options") && r.Method == "GET":
			prices := `[]`
			if h.optionsOpen.Load() {
				prices = fmt.Sprintf(`[{"duration":%q,"pricingMode":"default","price":{"value":0,"currencyCode":"EUR"}}]`, period)
			}
			body = fmt.Sprintf(`[{"planCode":"ram-32g-ddr4-ecc-2400","prices":%s},{"planCode":"softraid-2x450nvme","prices":%s}]`, prices, prices)
			if h.options != "" {
				body = h.options
			}
		case strings.HasSuffix(path, "/eco") && r.Method == "POST":
			h.basePosts.Add(1)
			if !h.baseOpen.Load() {
				status, body = 400, `{"message":"not open yet"}`
			}
		case strings.HasSuffix(path, "/eco/options") && r.Method == "POST":
			h.optionPosts.Add(1)
		case strings.HasSuffix(path, "/configuration") && r.Method == "POST":
			h.configPosts.Add(1)
		case strings.HasSuffix(path, "/checkout") && r.Method == "POST":
			h.sent.Add(1)
			if code := h.checkoutStatus.Load(); code != 0 {
				status, body = int(code), `{"message":"launch checkout rejected"}`
			}
		}
		if body != "" {
			return testSaleResponse(r, status, body), nil
		}
		return original.RoundTrip(r)
	})
	t.Cleanup(func() {
		if orders.Load() > 0 {
			waitWarm(t, func() bool {
				rows, err := state.DB.ListHistory()
				if err != nil || len(rows) == 0 {
					return false
				}
				for _, r := range rows {
					if r.OrderStatus == "" {
						return false
					}
				}
				return true
			})
		}
	})
	return h
}

func testSaleResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestPreorderPriceOpensWithLaggingStock(t *testing.T) {
	for _, status := range []string{"unknown", "comingSoon", "unavailable"} {
		for _, period := range []string{"P1M", "P12M"} {
			t.Run(status+"/"+period, func(t *testing.T) {
				h := saleLaunchFixture(t, period, status)
				h.baseOpen.Store(true)
				h.optionsOpen.Store(true)
				out := PurchaseServer(context.Background(), h.state, &h.item)
				if !out.Success || h.sent.Load() != 1 {
					t.Fatal(out, h.sent.Load())
				}
				if out := PurchaseServer(context.Background(), h.state, &h.item); !out.PauseTask || h.sent.Load() != 1 {
					t.Fatal("duplicate checkout", out)
				}
			})
		}
	}
}

func TestPreorderRetainsConfiguredCartUntilAddonPricesOpen(t *testing.T) {
	h := saleLaunchFixture(t, "P1M", "unknown")
	h.baseOpen.Store(true)
	session := &cartSession{}
	defer session.discard()
	ctx := context.WithValue(context.Background(), cartSessionKey{}, session)
	first := PurchaseServer(ctx, h.state, &h.item)
	if !first.WaitingForCatalog || session.partial == nil || session.cart != nil || h.sent.Load() != 0 {
		t.Fatal("partial cart was not retained safely", first)
	}
	base, configs := h.basePosts.Load(), h.configPosts.Load()
	h.optionsOpen.Store(true)
	out := PurchaseServer(ctx, h.state, &h.item)
	if !out.Success || h.sent.Load() != 1 || h.basePosts.Load() != base || h.configPosts.Load() != configs || h.optionPosts.Load() != 2 {
		t.Fatal("repeated preparation or wrong options", out, h.basePosts.Load(), h.optionPosts.Load(), h.configPosts.Load())
	}
}

func TestAddonOnlyOpeningWakesRealPurchaseBeforeRetry(t *testing.T) {
	h := saleLaunchFixture(t, "P1M", "unknown")
	h.baseOpen.Store(true)
	h.state.Queue = []types.QueueItem{h.item}
	if err := h.state.SaveQueue(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); processQueueLoop(ctx, h.state, PurchaseServer) }()
	defer func() { cancel(); <-done }()
	waitWarm(t, func() bool { return h.configPosts.Load() >= 3 })
	if h.sent.Load() != 0 {
		t.Fatal("base price alone created an order")
	}
	base, configs := h.basePosts.Load(), h.configPosts.Load()
	start := time.Now()
	h.optionsOpen.Store(true)
	waitWarm(t, func() bool { return h.sent.Load() == 1 })
	if time.Since(start) >= catalog.PreorderCheckInterval || h.basePosts.Load() != base || h.configPosts.Load() != configs {
		t.Fatal("missed addon signal or discarded partial cart")
	}
	t.Logf("addon-only opening woke actual PurchaseServer in %s, without recreating/configuring the base item", time.Since(start))
}

func TestPreorderPauseRejectsLateSaleSignal(t *testing.T) {
	h := saleLaunchFixture(t, "P1M", "unknown")
	h.baseOpen.Store(true)
	h.state.Queue = []types.QueueItem{h.item}
	if err := h.state.SaveQueue(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); processQueueLoop(ctx, h.state, PurchaseServer) }()
	defer func() { cancel(); <-done }()
	waitWarm(t, func() bool { return h.configPosts.Load() >= 3 })
	h.state.QueueMu.Lock()
	h.state.Queue[0].Status = "paused"
	h.state.UnregisterTaskCancel(h.item.ID)
	h.state.QueueMu.Unlock()
	h.optionsOpen.Store(true)
	time.Sleep(2200 * time.Millisecond)
	if h.sent.Load() != 0 {
		t.Fatal("a paused task checked out after the sale opened")
	}
}

func TestPreorderQuoteGateRejectsUnknownPriceAndWrongPeriod(t *testing.T) {
	for _, tc := range []struct{ name, prices string }{
		{"missing amount", `[{"duration":"P1M","pricingMode":"default"}]`},
		{"null amount", `[{"duration":"P1M","pricingMode":"default","price":{"value":null,"currencyCode":"EUR"}}]`},
		{"wrong currency", `[{"duration":"P1M","pricingMode":"default","price":{"value":0,"currencyCode":"USD"}}]`},
		{"wrong period", `[{"duration":"P12M","pricingMode":"default","price":{"value":0,"currencyCode":"EUR"}}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := saleLaunchFixture(t, "P1M", "1H-low")
			h.baseOpen.Store(true)
			h.optionsOpen.Store(true)
			h.options = fmt.Sprintf(`[{"planCode":"ram-32g-ddr4-ecc-2400","prices":%s},{"planCode":"softraid-2x450nvme","prices":%s}]`, tc.prices, tc.prices)
			out := PurchaseServer(context.Background(), h.state, &h.item)
			if !out.WaitingForCatalog || h.sent.Load() != 0 {
				t.Fatal("bad quote authorized checkout", out)
			}
		})
	}
}

func TestTotalQuantityAcrossConcurrentDatacenters(t *testing.T) {
	for _, limit := range []int{1, 2} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			h := saleLaunchFixture(t, "P1M", "unknown")
			h.baseOpen.Store(true)
			h.optionsOpen.Store(true)
			for i, dc := range []string{"gra", "fra", "gra", "fra"} {
				item := h.item
				item.ID = fmt.Sprint(i)
				item.Datacenter = dc
				item.OrderGroup = "test-group"
				item.GroupLimit = limit
				h.state.Queue = append(h.state.Queue, item)
			}
			if err := h.state.SaveQueue(); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			start := make(chan struct{})
			for _, it := range h.state.Queue {
				wg.Add(1)
				go func(item types.QueueItem) {
					defer wg.Done()
					<-start
					out := PurchaseServer(context.Background(), h.state, &item)
					if out.Fatal || out.PauseTask {
						t.Errorf("unexpected result %+v", out)
					}
				}(it)
			}
			close(start)
			wg.Wait()
			if h.sent.Load() != int32(limit) {
				t.Fatal("total limit exceeded or not filled", h.sent.Load(), limit)
			}
			out := PurchaseServer(context.Background(), h.state, &h.state.Queue[len(h.state.Queue)-1])
			if out.GroupStopped != "groupComplete" && !out.PauseTask {
				t.Fatal("extra candidate was not stopped", out)
			}
		})
	}
}

func TestUncertainOrderHoldsTotalQuotaAndDefiniteRefusalReleasesIt(t *testing.T) {
	for _, code := range []int32{400, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			h := saleLaunchFixture(t, "P1M", "unknown")
			h.baseOpen.Store(true)
			h.optionsOpen.Store(true)
			h.checkoutStatus.Store(code)
			a, b := h.item, h.item
			a.ID = "first"
			b.ID = "second"
			a.OrderGroup = "group-one"
			b.OrderGroup = a.OrderGroup
			a.GroupLimit = 1
			b.GroupLimit = 1
			h.state.Queue = []types.QueueItem{a, b}
			if err := h.state.SaveQueue(); err != nil {
				t.Fatal(err)
			}
			first := PurchaseServer(context.Background(), h.state, &a)
			h.checkoutStatus.Store(0)
			second := PurchaseServer(context.Background(), h.state, &b)
			if code == 400 {
				if !first.WaitingForCatalog || !second.Success || h.sent.Load() != 2 {
					t.Fatal(first, second, h.sent.Load())
				}
			} else {
				if !first.PauseTask || second.GroupStopped != "groupUncertain" || h.sent.Load() != 1 {
					t.Fatal(first, second, h.sent.Load())
				}
			}
		})
	}
}

func TestChangedAddonPricingModeRebuildsWithoutCheckingOutOldMode(t *testing.T) {
	h := saleLaunchFixture(t, "P1M", "unknown")
	h.baseOpen.Store(true)
	h.optionsOpen.Store(true)
	base, err := h.state.OVH.ClientFor(h.item.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	original := base.Client.Transport
	var quoted atomic.Bool
	base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		if !quoted.Load() && r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco") {
			return testSaleResponse(r, 200, `[{"planCode":"plan","prices":[{"duration":"P1M","pricingMode":"default","price":{"value":null,"currencyCode":"EUR"}}]}]`), nil
		}
		return original.RoundTrip(r)
	})
	session := &cartSession{}
	defer session.discard()
	ctx := context.WithValue(context.Background(), cartSessionKey{}, session)
	if out := PurchaseServer(ctx, h.state, &h.item); !out.WaitingForCatalog || session.cart == nil {
		t.Fatal("unpriced cart not retained", out)
	}
	quoted.Store(true)
	h.options = `[{"planCode":"ram-32g-ddr4-ecc-2400","prices":[{"duration":"P1M","pricingMode":"special","price":{"value":0,"currencyCode":"EUR"}}]},{"planCode":"softraid-2x450nvme","prices":[{"duration":"P1M","pricingMode":"special","price":{"value":0,"currencyCode":"EUR"}}]}]`
	if out := PurchaseServer(ctx, h.state, &h.item); !out.RetryImmediately || h.sent.Load() != 0 || session.cart != nil {
		t.Fatal("old mode was used", out)
	}
	if out := PurchaseServer(ctx, h.state, &h.item); !out.Success || h.sent.Load() != 1 || h.basePosts.Load() != 2 {
		t.Fatal("new pricing mode did not rebuild immediately", out)
	}
}

func TestSaleBackoffBlocksPreparationWrites(t *testing.T) {
	h := saleLaunchFixture(t, "P1M", "unknown")
	h.baseOpen.Store(true)
	h.optionsOpen.Store(true)
	ctx := context.WithValue(context.Background(), saleSignalKey{}, saleSnapshot{retryAt: time.Now().Add(time.Minute)})
	out := PurchaseServer(ctx, h.state, &h.item)
	if !out.WaitingForCatalog || out.RetryAfter < 50*time.Second || h.basePosts.Load() != 0 || h.configPosts.Load() != 0 || h.sent.Load() != 0 {
		t.Fatal("sale backoff was ignored", out)
	}
}

func TestVerifiedFullConfigurationUsesSaleSignalWithoutAnotherStockRequest(t *testing.T) {
	h := saleLaunchFixture(t, "P1M", "unknown")
	h.baseOpen.Store(true)
	h.optionsOpen.Store(true)
	base, err := h.state.OVH.ClientFor(h.item.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	original := base.Client.Transport
	var priced atomic.Bool
	base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		if !priced.Load() && r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco") {
			return testSaleResponse(r, 200, `[{"planCode":"plan","prices":[{"duration":"P1M","pricingMode":"default","price":{"value":null,"currencyCode":"EUR"}}]}]`), nil
		}
		return original.RoundTrip(r)
	})
	session := &cartSession{}
	defer session.discard()
	ctx := context.WithValue(context.Background(), cartSessionKey{}, session)
	if out := PurchaseServer(ctx, h.state, &h.item); !out.WaitingForCatalog || session.cart == nil {
		t.Fatal("verified cart not prepared", out)
	}
	time.Sleep(120 * time.Millisecond) // Expire the stock bus's short cache so an unwanted fetch fails.
	priced.Store(true)
	snapshot, err := readSaleSnapshot(ctx, session.client, session.account, session.cart.id, h.item.PlanCode)
	if err != nil {
		t.Fatal(err)
	}
	before := h.stockReads.Load()
	h.rejectStock.Store(true)
	out := PurchaseServer(context.WithValue(ctx, saleSignalKey{}, snapshot), h.state, &h.item)
	if !out.Success || h.sent.Load() != 1 || h.stockReads.Load() != before {
		t.Fatal("sale dispatch still depended on the stock endpoint", out, h.stockReads.Load(), before)
	}
	trace := LastTimings()[h.item.ID].Dispatch
	if trace == nil || !trace.Prewarmed || !trace.WroteRequest {
		t.Fatal("prepared dispatch not traced", trace)
	}
	t.Logf("verified sale signal to local TCP write: %d microseconds", trace.DetectionToWriteUS)
}

// 有货再下:报价开放后备好车但不结账,直到库存接口在目标机房出现可下单状态。
func TestPreorderWaitStockHoldsArmedCartUntilStockAppears(t *testing.T) {
	h := saleLaunchFixture(t, "P1M", "comingSoon")
	h.item.PreorderWaitStock = true
	h.baseOpen.Store(true)
	h.optionsOpen.Store(true)
	session := &cartSession{}
	defer session.discard()
	ctx := context.WithValue(context.Background(), cartSessionKey{}, session)
	out := PurchaseServer(ctx, h.state, &h.item)
	if !out.WaitingForStock || h.sent.Load() != 0 {
		t.Fatalf("must wait for stock without checkout: %+v sent=%d", out, h.sent.Load())
	}
	if session.cart == nil {
		t.Fatal("prepared cart should be kept while waiting for stock")
	}
	posts := h.basePosts.Load()
	h.stockNow.Store("1H-low")
	time.Sleep(150 * time.Millisecond) // 越过 StockSignals 的 100ms 合并窗口
	out = PurchaseServer(ctx, h.state, &h.item)
	if !out.Success || h.sent.Load() != 1 {
		t.Fatalf("stock appeared but no checkout: %+v sent=%d", out, h.sent.Load())
	}
	if h.basePosts.Load() != posts {
		t.Fatal("armed cart should be reused, not rebuilt")
	}
}

// 有货再下不能走"已核验配置跳过库存"的捷径:报价信号新鲜也必须真的读库存。
func TestPreorderWaitStockNeverSkipsStockRead(t *testing.T) {
	h := saleLaunchFixture(t, "P1M", "comingSoon")
	h.item.PreorderWaitStock = true
	session := &cartSession{cart: &preparedCart{fqn: "plan.ram-32g-ddr4-ecc-2400.softraid-2x450nvme"}}
	sale := saleSnapshot{account: types.OVHAccount{ID: "x"}, plan: "plan", at: time.Now()}
	session.account = sale.account
	ctx := context.WithValue(context.Background(), saleSignalKey{}, sale)
	if fqn, _ := preorderPinnedFQN(ctx, session, &h.item, nil, "", []string{"ram-32g-ddr4-ecc-2400", "softraid-2x450nvme"}); fqn != "" {
		t.Fatal("wait-for-stock presale must not pin FQN", fqn)
	}
	h.item.PreorderWaitStock = false
	if fqn, _ := preorderPinnedFQN(ctx, session, &h.item, nil, "", []string{"ram-32g-ddr4-ecc-2400", "softraid-2x450nvme"}); fqn == "" {
		t.Fatal("sale-triggered presale should keep the verified shortcut")
	}
}
