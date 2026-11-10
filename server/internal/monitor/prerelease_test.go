package monitor

import (
	"fmt"
	"github.com/ovh-buy/server/internal/price"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
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

type prereleaseTransport func(*http.Request) (*http.Response, error)

func (f prereleaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the real monitor, price gate and notification path using only local fixtures.
func TestPrereleaseMonitorDoesNotAlertOrOrderBeforeListing(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, tc := range []struct {
		name, subsidiary, plan string
		listed                 bool
		transient              bool
	}{
		{"not listed", "FI", "27sysle02-v1", false, false},
		{"listed", "PT", "27sysle02-v1", true, false},
		{"other product family", "LT", "25scale01-v1", false, false},
		// 补货时询价超时/限流:不能因此跳过自动下单
		{"transient price failure still orders", "PT", "27sysle02-v1", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			database, err := db.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { database.Close() })
			lg := logger.New(filepath.Join(dir, "test.log"), slog.New(slog.NewTextHandler(io.Discard, nil)))
			st := app.NewState(storage.Paths{DataDir: dir}, config.New(database), lg, database)
			st.Accounts = []types.OVHAccount{{ID: "issue12-" + tc.subsidiary, Endpoint: "ovh-eu", Zone: tc.subsidiary, AppKey: "test", AppSecret: "test", ConsumerKey: "test"}}
			st.Config.Set(types.Config{NotifyWebhookURL: "https://notify.invalid/ovh-issue12"})
			st.Port = "19000"
			plan := tc.plan
			fqn := plan + ".ram-128g-ddr4-ecc-2666.softraid-3x1920nvme"
			var quotes, alerts, orders atomic.Int32
			stock := "1H-low"
			transport := prereleaseTransport(func(r *http.Request) (*http.Response, error) {
				status, body := http.StatusOK, `{}`
				switch {
				case strings.HasSuffix(r.URL.Path, "/auth/time"):
					body = fmt.Sprint(time.Now().Unix())
				case strings.Contains(r.URL.Path, "/order/catalog/public/eco"):
					body = `{"plans":[]}`
					if tc.listed {
						body = `{"plans":[{"planCode":"27sysle02-v1"}]}`
					}
				case strings.Contains(r.URL.Path, "/dedicated/server/datacenter/availabilities"):
					body = fmt.Sprintf(`[{"fqn":%q,"memory":"ram-128g-ddr4-ecc-2666","storage":"softraid-3x1920nvme","datacenters":[{"datacenter":"bhs","availability":%q}]}]`, fqn, stock)
				case r.URL.Path == "/api/internal/monitor/price":
					quotes.Add(1)
					body = `{"success":false,"error":"plan is not in this subsidiary's catalog"}`
					if tc.listed {
						body = `{"success":true,"price":{"prices":{"withTax":30,"currencyCode":"EUR"}}}`
					}
				case r.URL.Hostname() == "notify.invalid":
					alerts.Add(1)
				default:
					orders.Add(1)
					return nil, fmt.Errorf("unexpected request blocked by fixture: %s", r.URL)
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			http.DefaultTransport = transport
			client, err := st.OVH.ClientFor(st.Accounts[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			client.Client = &http.Client{Transport: transport}
			m := New(st)
			m.priceFn = func(string, string, string, []string, string) price.Result {
				quotes.Add(1)
				if tc.transient {
					return price.Result{Error: "价格校验超过 8 秒未完成", Transient: true}
				}
				if tc.listed {
					return price.Result{Success: true, Price: &price.PriceInfo{Prices: map[string]interface{}{"withTax": 30, "currencyCode": "EUR"}}}
				}
				return price.Result{Error: "plan is not in this subsidiary's catalog"}
			}
			oldStatus := "price_check_failed"
			if tc.name == "other product family" {
				oldStatus = "unavailable"
			}
			m.AddSubscription(plan, []string{"bhs"}, true, true, "", map[string]string{"bhs|" + fqn: oldStatus}, nil,
				!tc.listed || tc.transient, 1, st.Accounts[0].ID, nil, "")
			// A restored old failure must not cause repeated initial alerts either.
			sub := fromDBSub(toDBSub(m.subscriptions[0]))
			m.subscriptions[0] = sub
			states := []string{"1H-low", "unavailable", "comingSoon", "1H-high", "1H-low"}
			if tc.listed || tc.name == "other product family" {
				states = []string{"1H-low", "1H-low"}
			}
			for _, value := range states {
				stock = value
				m.CheckAvailabilityChange(sub, "issue12")
				// Expire the 100ms shared stock snapshot between observations.
				time.Sleep(110 * time.Millisecond)
			}
			if tc.transient {
				if alerts.Load() != 1 || quotes.Load() == 0 || len(st.Queue) == 0 || sub.statusSnapshot()["bhs|"+fqn] != "available" {
					t.Fatalf("transient price failure must still alert and auto-order: alerts=%d quotes=%d queue=%d status=%v", alerts.Load(), quotes.Load(), len(st.Queue), sub.statusSnapshot())
				}
			} else if tc.listed {
				if alerts.Load() != 1 || quotes.Load() == 0 || sub.statusSnapshot()["bhs|"+fqn] != "available" {
					t.Fatalf("listed recovery must alert once: alerts=%d quotes=%d status=%v", alerts.Load(), quotes.Load(), sub.statusSnapshot())
				}
				if sub.snapshot().LastCheckError != "" {
					t.Fatal("listed plan retained a waiting-for-catalog warning")
				}
			} else if tc.name == "other product family" {
				if alerts.Load() != 1 || quotes.Load() == 0 || len(st.Queue) != 0 {
					t.Fatalf("other families must retain stock diagnostics without orders: alerts=%d quotes=%d queue=%d", alerts.Load(), quotes.Load(), len(st.Queue))
				}
			} else {
				if quotes.Load() != 0 || alerts.Load() != 0 || len(st.Queue) != 0 {
					t.Fatalf("unlisted stock must wait without price checks, alerts or orders: quotes=%d alerts=%d queue=%d", quotes.Load(), alerts.Load(), len(st.Queue))
				}
				if !strings.Contains(sub.snapshot().LastCheckError, "销售目录") {
					t.Fatal("missing a visible waiting-for-catalog diagnostic")
				}
			}
			if orders.Load() != 0 {
				t.Fatalf("fixture saw %d unexpected requests", orders.Load())
			}
		})
	}
}
