package purchase

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
	"github.com/ovh-buy/server/internal/types"
)

func saleOffers(t *testing.T, raw string) []map[string]interface{} {
	t.Helper()
	var offers []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &offers); err != nil {
		t.Fatal(err)
	}
	return offers
}

func TestSaleOptionsPublishReadyModelBeforeAnotherModelReturns404(t *testing.T) {
	state, _, _, _, _ := saleFixture(t)
	probe := saleProbe{state: state, account: "sale-fixture"}
	defer probe.close()
	if _, _, err := probe.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	ready := &saleListener{plan: "27skle01-v1", duration: "P1M", changes: make(chan struct{}, 1)}
	waiting := &saleListener{plan: "27skle02-v1", duration: "P1M", changes: make(chan struct{}, 1)}
	watch := &accountSaleWatch{account: probe.session.account, listeners: map[*saleListener]bool{ready: true, waiting: true}}
	s := newSaleWatcher(context.Background(), state)
	offers := saleOffers(t, `[{"planCode":"27skle01-v1","prices":[{"duration":"P1M","pricingMode":"default","price":{"value":30,"currencyCode":"EUR"}}]},{"planCode":"27skle02-v1","prices":[{"duration":"P1M","pricingMode":"default","price":{"value":30,"currencyCode":"EUR"}}]}]`)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	probe.session.client.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/eco/options") {
			return testSaleResponse(r, 200, `{}`), nil
		}
		if r.URL.Query().Get("planCode") == waiting.plan {
			<-release
			return testSaleResponse(r, 404, `{"message":"addons not on sale"}`), nil
		}
		return testSaleResponse(r, 200, `[]`), nil
	})
	done := make(chan error, 1)
	go func() { _, _, err := s.pollOptions(context.Background(), &probe, watch, offers); done <- err }()
	select {
	case <-ready.changes:
	case <-time.After(time.Second):
		t.Fatal("a pending model delayed the ready model")
	}
	select {
	case <-done:
		t.Fatal("slow model did not remain pending")
	default:
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal("one unopened model blocked the whole account", err)
	}
	s.mu.Lock()
	snapshot := watch.latest[waiting.plan]
	s.mu.Unlock()
	if snapshot.ready("P1M", "default", nil) {
		t.Fatal("failed option discovery was treated as a confirmed quote")
	}
}

func TestSaleSignalsRequireExactModelAndPeriodAndOnlyWakeOnChange(t *testing.T) {
	s := newSaleWatcher(context.Background(), nil)
	monthly := &saleListener{plan: "27skle01-v1", duration: "P1M", changes: make(chan struct{}, 1)}
	annual := &saleListener{plan: "27skle01-v1", duration: "P12M", changes: make(chan struct{}, 1)}
	wrongModel := &saleListener{plan: "27skle01-v2", duration: "P1M", changes: make(chan struct{}, 1)}
	otherAccount := &saleListener{plan: monthly.plan, duration: monthly.duration, changes: make(chan struct{}, 1)}
	watch := &accountSaleWatch{listeners: map[*saleListener]bool{monthly: true, annual: true, wrongModel: true}}
	s.accounts["other"] = &accountSaleWatch{listeners: map[*saleListener]bool{otherAccount: true}}
	assertSignal := func(listener *saleListener, want bool) {
		t.Helper()
		select {
		case <-listener.changes:
			if !want {
				t.Fatal("unexpected sale signal", listener.plan, listener.duration)
			}
		default:
			if want {
				t.Fatal("missed sale signal", listener.plan, listener.duration)
			}
		}
	}
	monthlyOffers := saleOffers(t, `[{"planCode":"27skle01-v1","prices":[{"duration":"P1M","pricingMode":"default","price":{"value":30,"currencyCode":"EUR"}}]}]`)
	s.publish(watch, monthlyOffers)
	assertSignal(monthly, true)
	assertSignal(annual, false)
	assertSignal(wrongModel, false)
	assertSignal(otherAccount, false)
	s.publish(watch, monthlyOffers)
	assertSignal(monthly, false)
	annualOffers := saleOffers(t, `[{"planCode":"27skle01-v1","prices":[{"duration":"P1M","pricingMode":"default","price":{"value":30,"currencyCode":"EUR"}},{"duration":"P1Y","pricingMode":"default","price":{"value":30,"currencyCode":"EUR"}}]}]`)
	s.publish(watch, annualOffers)
	assertSignal(annual, true)
	assertSignal(monthly, true)
	s.publish(watch, nil)
	assertSignal(monthly, false)
	s.publish(watch, annualOffers)
	assertSignal(monthly, true)
	assertSignal(annual, true)
}

// No order endpoints are implemented: a discovery probe that adds/checks out an item fails this fixture.
func saleFixture(t *testing.T) (*app.State, *atomic.Bool, *atomic.Int32, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	state := queueCompletionState(t)
	var open atomic.Bool
	var carts, polls, deletes atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/auth/time":
			fmt.Fprint(w, time.Now().Unix())
		case r.Method == "POST" && r.URL.Path == "/order/cart":
			id := carts.Add(1)
			fmt.Fprintf(w, `{"cartId":"discovery-%d","expire":"2099-01-01T00:00:00Z"}`, id)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/assign"):
			if data, err := io.ReadAll(r.Body); err != nil || len(data) != 0 {
				t.Error("assign must have no body")
			}
			fmt.Fprint(w, `{}`)
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco"):
			polls.Add(1)
			if open.Load() {
				fmt.Fprint(w, `[{"planCode":"27skle01-v1","prices":[{"duration":"P1M","pricingMode":"default","price":{"value":30,"currencyCode":"EUR"}}]}]`)
			} else {
				fmt.Fprint(w, `[]`)
			}
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco/options"):
			fmt.Fprint(w, `[]`)
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/order/cart/discovery-"):
			deletes.Add(1)
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("discovery attempted an unexpected operation: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(api.Close)
	state.Accounts = []types.OVHAccount{{ID: "sale-fixture", Endpoint: api.URL, Zone: "IE", AppKey: "test", AppSecret: "test", ConsumerKey: "test"}}
	return state, &open, &carts, &polls, &deletes
}

func TestSaleWatcherSharesProbeAndWakesBeforeRetryWithoutRevivingPausedTask(t *testing.T) {
	state, open, carts, polls, deletes := saleFixture(t)
	for i := 0; i < 4; i++ {
		state.Queue = append(state.Queue, types.QueueItem{ID: fmt.Sprint(i), AccountID: "sale-fixture", PlanCode: "27skle01-v1", Datacenter: "gra", Preorder: true, Status: "running", RetryInterval: 60})
	}
	var calls [4]atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		processQueueLoop(ctx, state, func(_ context.Context, _ *app.State, item *types.QueueItem) Outcome {
			index := int(item.ID[0] - '0')
			if calls[index].Add(1) == 1 {
				return Outcome{WaitingForCatalog: true}
			}
			return Outcome{Success: true}
		})
	}()
	defer func() { cancel(); <-done }()
	waitWarm(t, func() bool {
		return calls[0].Load() == 1 && calls[1].Load() == 1 && calls[2].Load() == 1 && calls[3].Load() == 1 && polls.Load() > 0
	})
	state.QueueMu.Lock()
	state.Queue[0].Status = "paused"
	state.UnregisterTaskCancel("0") // Force the final state check to reject a late signal too.
	state.QueueMu.Unlock()
	started := time.Now()
	open.Store(true)
	waitWarm(t, func() bool { return calls[1].Load() >= 2 && calls[2].Load() >= 2 && calls[3].Load() >= 2 })
	if calls[0].Load() != 1 || carts.Load() != 1 || polls.Load() < 2 {
		t.Fatal("duplicate probes or paused task resumed", calls[0].Load(), carts.Load(), polls.Load())
	}
	cancel()
	<-done
	if deletes.Load() != 1 {
		t.Fatal("idle discovery cart was not cleaned", deletes.Load())
	}
	t.Logf("sale change woke three waiting tasks in %s (60s task retry, one shared 2s poll)", time.Since(started))
}

func TestSaleProbeRenewsExpiredCartAndChangedAccount(t *testing.T) {
	state, _, carts, polls, deletes := saleFixture(t)
	probe := saleProbe{state: state, account: "sale-fixture"}
	defer probe.close()
	for i := 0; i < 3; i++ {
		if i == 1 {
			probe.cart.expires = time.Now().Add(-time.Second)
		}
		if i == 2 {
			state.AccountsMu.Lock()
			state.Accounts[0].ConsumerKey = "changed"
			state.AccountsMu.Unlock()
		}
		if _, _, err := probe.poll(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if carts.Load() != 3 || polls.Load() != 3 || deletes.Load() != 2 {
		t.Fatal("stale discovery context reused", carts.Load(), polls.Load(), deletes.Load())
	}
}

func TestSaleProbeHonorsRetryAfterAndCanBeCancelled(t *testing.T) {
	state, _, _, _, _ := saleFixture(t)
	probe := saleProbe{state: state, account: "sale-fixture"}
	defer probe.close()
	if _, _, err := probe.poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	client := probe.session.client
	original := client.Client.Transport
	client.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco") {
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"90"}}, Body: http.NoBody, Request: r}, nil
		}
		return original.RoundTrip(r)
	})
	_, after, err := probe.poll(context.Background())
	if err == nil || after != 90*time.Second || saleRetryDelay(1, after) < after {
		t.Fatal("429 ignored", after, err)
	}
	client.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco") {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return original.RoundTrip(r)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err := probe.poll(ctx); err == nil || ctx.Err() == nil {
		t.Fatal("probe did not stop with its account", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if saleRetryAfter(now.Add(time.Minute).Format(http.TimeFormat), now) != time.Minute || saleRetryAfter("invalid", now) != 0 || saleRetryAfter("-1", now) != 0 {
		t.Fatal("Retry-After parsing is unsafe")
	}
	if saleRetryDelay(3, 0) < 8*time.Second || saleRetryDelay(1000, 0) > 75*time.Second {
		t.Fatal("backoff did not grow or was unbounded")
	}
}

// 基础商品刚开售就唤醒任务建车;已开售型号不被覆盖成空选项,也不重复唤醒。
func TestBaseOpeningWakesBeforeOptionPricesWithoutClobbering(t *testing.T) {
	s := newSaleWatcher(context.Background(), nil)
	listener := &saleListener{plan: "27skle01-v1", duration: "P1M", changes: make(chan struct{}, 1)}
	watch := &accountSaleWatch{listeners: map[*saleListener]bool{listener: true}, latest: map[string]saleSnapshot{}}
	closed := saleOffers(t, `[{"planCode":"27skle01-v1","prices":[]}]`)
	open := saleOffers(t, `[{"planCode":"27skle01-v1","prices":[{"duration":"P1M","pricingMode":"default","price":{"value":30,"currencyCode":"EUR"}}]}]`)
	s.publishBaseOpen(watch, closed)
	select {
	case <-listener.changes:
		t.Fatal("unpriced base must not wake tasks")
	default:
	}
	s.publishBaseOpen(watch, open)
	select {
	case <-listener.changes:
	default:
		t.Fatal("base opening did not wake the task before option prices")
	}
	if watch.latest[listener.plan].ready("P1M", "default", nil) {
		t.Fatal("base-only snapshot must never be ready to check out")
	}
	// 选项报价到达后,再次的 base-only 发布不能把它清空。
	withOptions := watch.latest[listener.plan]
	withOptions.options = []map[string]interface{}{{"planCode": "ram"}}
	watch.latest[listener.plan] = withOptions
	s.publishBaseOpen(watch, open)
	select {
	case <-listener.changes:
		t.Fatal("already-open model woke again")
	default:
	}
	if len(watch.latest[listener.plan].options) != 1 {
		t.Fatal("option quotes were clobbered by a base-only publish")
	}
}
