package purchase

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/orderterm"
	"github.com/ovh-buy/server/internal/price"
	"github.com/ovh-buy/server/internal/types"
)

func TestEveryEcoComponentUsesAnnualPeriodAndStoredMonthlyItemsBlockCheckout(t *testing.T) {
	options := []string{"ram-32g", "softraid-2x512nvme", "bandwidth-1000", "vrack-bandwidth-1000", "security-license"}
	for _, wrong := range append([]string{"", "plan", "implicit-license"}, options...) {
		name := wrong
		if name == "" {
			name = "all-annual"
		}
		t.Run(name, func(t *testing.T) {
			state, _, checkouts, stock := hotFixture(t)
			stock.Store(true)
			base, _ := state.OVH.ClientFor("hot-eu")
			original := base.Client.Transport
			var mu sync.Mutex
			posted := map[string]int{}
			var verified atomic.Int32
			base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
				var body interface{}
				switch {
				case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/item/999"):
					body = map[string]interface{}{"itemId": 999, "duration": "P1M", "settings": map[string]interface{}{"planCode": "implicit-license"}}
				case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco"):
					body = []interface{}{map[string]interface{}{"planCode": "plan", "prices": []interface{}{map[string]interface{}{"duration": "P1M", "pricingMode": "default"}, map[string]interface{}{"duration": "P1Y", "pricingMode": "upfront12"}}}}
				case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco/options"):
					offers := []interface{}{}
					for _, code := range options {
						offers = append(offers, map[string]interface{}{"planCode": code, "prices": []interface{}{map[string]interface{}{"duration": "P1M", "pricingMode": "default"}, map[string]interface{}{"duration": "P12M", "pricingMode": "upfront12"}}})
					}
					body = offers
				case r.Method == "POST" && (strings.HasSuffix(r.URL.Path, "/eco") || strings.HasSuffix(r.URL.Path, "/eco/options")):
					raw, _ := io.ReadAll(r.Body)
					r.Body = io.NopCloser(strings.NewReader(string(raw)))
					var payload struct {
						Plan     string `json:"planCode"`
						Duration string `json:"duration"`
						Mode     string `json:"pricingMode"`
					}
					if err := json.Unmarshal(raw, &payload); err != nil {
						return nil, err
					}
					if !orderterm.Same(payload.Duration, "P12M") || payload.Mode != "upfront12" {
						t.Error("non-annual item submitted", payload)
					}
					mu.Lock()
					posted[payload.Plan]++
					mu.Unlock()
				}
				if body != nil {
					raw, _ := json.Marshal(body)
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
				}
				response, err := original.RoundTrip(r)
				if err == nil && wrong == "implicit-license" && r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/order/cart/cart-") && strings.Count(r.URL.Path, "/") == 3 {
					var cart struct {
						Items []int64 `json:"items"`
					}
					_ = json.NewDecoder(response.Body).Decode(&cart)
					_ = response.Body.Close()
					cart.Items = append(cart.Items, 999)
					raw, _ := json.Marshal(cart)
					response.Body = io.NopCloser(strings.NewReader(string(raw)))
				}
				if err == nil && r.Method == "GET" && strings.Contains(r.URL.Path, "/item/") && !strings.HasSuffix(r.URL.Path, "/requiredConfiguration") {
					verified.Add(1)
					raw, _ := io.ReadAll(response.Body)
					_ = response.Body.Close()
					var item map[string]interface{}
					_ = json.Unmarshal(raw, &item)
					settings, _ := item["settings"].(map[string]interface{})
					if wrong != "" && settings["planCode"] == wrong {
						item["duration"] = "P1M"
					}
					raw, _ = json.Marshal(item)
					response.Body = io.NopCloser(strings.NewReader(string(raw)))
				}
				return response, err
			})
			quote := price.GetInternal(state, "hot-eu", "plan", "gra", options, "P12M")
			item := poolItem()
			item.Duration = "P12M"
			item.Options = options
			state.Queue = []types.QueueItem{item}
			out := PurchaseServer(context.Background(), state, &item)
			if wrong == "" {
				if !quote.Success || !out.Success || checkouts.Load() != 1 {
					t.Fatal("valid annual cart rejected", quote.Error, out, checkouts.Load())
				}
			} else if quote.Success || out.Success || !out.Fatal || checkouts.Load() != 0 || !strings.Contains(out.Reason, wrong) {
				t.Fatal("stored monthly component reached checkout", wrong, quote.Error, out, checkouts.Load())
			}
			if verified.Load() == 0 {
				t.Fatal("actual cart periods were never checked")
			}
			mu.Lock()
			defer mu.Unlock()
			for _, code := range append([]string{"plan"}, options...) {
				if posted[code] != 2 {
					t.Fatal("selected option was omitted from quote or order", code, posted)
				}
			}
		})
	}
}

func TestPoolRejectsUnknownOrDifferentPreparedPeriod(t *testing.T) {
	state, _, checkouts, _ := hotFixture(t)
	pool := NewCartPool(state)
	t.Cleanup(pool.Close)
	item := poolItem()
	if err := pool.Prepare("period", []types.QueueItem{item}, time.Minute, nil); err != nil {
		t.Fatal(err)
	}
	waitWarm(t, func() bool { return pool.Ready(item) })
	for _, duration := range []string{"", "P1Y"} {
		pool.mu.Lock()
		pool.entries["period"][0].cart.duration = duration
		pool.mu.Unlock()
		if pool.Ready(item) || pool.Quote(item) != nil || pool.Take(item) != nil || checkouts.Load() != 0 {
			t.Fatal("unverified or wrong-period standby cart was available", duration)
		}
	}
}

func TestSessionRebuildsMonthlyStandbyAfterSelectingAnnualPeriod(t *testing.T) {
	state, carts, checkouts, stock := hotFixture(t)
	base, _ := state.OVH.ClientFor("hot-eu")
	original := base.Client.Transport
	base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco") {
			body = `[{"planCode":"plan","prices":[{"duration":"P1Y","pricingMode":"upfront12"}]}]`
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco/options") {
			body = `[{"planCode":"ram-32g","prices":[{"duration":"P1M","pricingMode":"default"},{"duration":"P12M","pricingMode":"upfront12"}]}]`
		}
		if body != "" {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/checkout") && carts.Load() != 2 {
			t.Error("old monthly cart was used")
		}
		return original.RoundTrip(r)
	})
	item := poolItem()
	state.Queue = []types.QueueItem{item}
	session := &cartSession{}
	defer session.discard()
	ctx := context.WithValue(context.Background(), cartSessionKey{}, session)
	if out := PurchaseServer(ctx, state, &item); !out.WaitingForStock || session.cart == nil {
		t.Fatal(out)
	}
	item.Duration = "P12M"
	state.Queue[0].Duration = "P12M"
	stock.Store(true)
	ctx = context.WithValue(ctx, stockSignalKey{}, app.StockSignal{AccountID: "hot-eu", PlanCode: "plan", DetectedAt: time.Now(), Variants: []map[string]interface{}{{"fqn": "plan.ram-32g", "datacenters": []interface{}{map[string]interface{}{"datacenter": "gra", "availability": "1H-low"}}}}})
	if out := PurchaseServer(ctx, state, &item); !out.Success || carts.Load() != 2 || checkouts.Load() != 1 {
		t.Fatal(out, carts.Load(), checkouts.Load())
	}
}

func TestAnnualCartQuoteAndCheckoutKeepChosenPeriod(t *testing.T) {
	for _, missingAddon := range []bool{false, true} {
		t.Run(map[bool]string{false: "annual", true: "addon-monthly-only"}[missingAddon], func(t *testing.T) {
			state, _, checkouts, stock := hotFixture(t)
			stock.Store(true)
			base, _ := state.OVH.ClientFor("hot-eu")
			original := base.Client.Transport
			var added atomic.Int32
			base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
				body := ""
				switch {
				case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco"):
					body = `[{"planCode":"plan","prices":[{"duration":"P1M","pricingMode":"default"},{"duration":"P1Y","pricingMode":"upfront12"}]}]`
				case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco/options"):
					body = `[{"planCode":"ram-32g","prices":[{"duration":"P1M","pricingMode":"default"},{"duration":"P12M","pricingMode":"upfront12"}]}]`
					if missingAddon {
						body = `[{"planCode":"ram-32g","prices":[{"duration":"P1M","pricingMode":"default"}]}]`
					}
				case r.Method == "POST" && (strings.HasSuffix(r.URL.Path, "/eco") || strings.HasSuffix(r.URL.Path, "/eco/options")):
					raw, _ := io.ReadAll(r.Body)
					r.Body = io.NopCloser(strings.NewReader(string(raw)))
					var payload map[string]interface{}
					_ = json.Unmarshal(raw, &payload)
					if payload["duration"] != "P1Y" && payload["duration"] != "P12M" || payload["pricingMode"] != "upfront12" {
						t.Error("wrong period/mode", payload)
					}
					added.Add(1)
				}
				if body != "" {
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				}
				return original.RoundTrip(r)
			})
			quote := price.GetInternal(state, "hot-eu", "plan", "gra", []string{"ram-32g"}, "P12M")
			if missingAddon {
				if quote.Success {
					t.Fatal("annual quote accepted a monthly-only addon")
				}
			} else if !quote.Success || quote.Price.Duration != "P1Y" || quote.Price.PricingMode != "upfront12" {
				t.Fatalf("wrong quote: %+v", quote)
			}
			pool := NewCartPool(state)
			state.OrderPrewarm = pool
			t.Cleanup(pool.Close)
			item := poolItem()
			item.Duration = "P12M"
			if missingAddon {
				client, _ := state.OVH.IsolatedClientFor("hot-eu")
				cart, out := prepareCart(context.Background(), state, &item, state.Accounts[0], client, item.Options, "gra", "plan.ram-32g", newTimeline(), "test", true)
				if cart != nil || !out.Fatal || checkouts.Load() != 0 {
					t.Fatal("period mismatch reached checkout", out)
				}
				return
			}
			if err := pool.Prepare("annual", []types.QueueItem{item}, time.Minute, nil); err != nil {
				t.Fatal(err)
			}
			waitWarm(t, func() bool { return pool.Ready(item) })
			monthly := item
			monthly.Duration = "P1M"
			if pool.Ready(monthly) {
				t.Fatal("monthly task can consume annual cart")
			}
			if pool.Status("annual")[0].Duration != "P12M" || checkouts.Load() != 0 {
				t.Fatal("preparation lost period or ordered early")
			}
			state.Queue = []types.QueueItem{item}
			if out := PurchaseServer(context.Background(), state, &item); !out.Success {
				t.Fatal(out)
			}
			if checkouts.Load() != 1 || added.Load() != 4 {
				t.Fatal("cart was rebuilt or checked out twice", added.Load(), checkouts.Load())
			}
			history, err := state.DB.ListHistory()
			if err != nil || len(history) != 1 || history[0].Duration != "P12M" {
				t.Fatal("period lost in history", history, err)
			}
		})
	}
}
