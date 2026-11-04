package purchase

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/price"
	"github.com/ovh-buy/server/internal/types"
)

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
				cart, out := prepareCart(context.Background(), state, &item, state.Accounts[0], client, item.Options, "gra", newTimeline(), "test", true)
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
