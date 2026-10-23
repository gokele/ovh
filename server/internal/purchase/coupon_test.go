package purchase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/types"
)

// All catalog, signed API and checkout requests are handled locally.
func TestPurchaseCouponBeforeCheckout(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, tc := range []struct {
		name, coupon                          string
		status                                int
		preorder, cancel, reconciled, network bool
		checkout, warning                     bool
	}{
		{name: "blank", coupon: "  ", checkout: true},
		{name: "accepted", coupon: " Save10 ", status: 200, checkout: true},
		{name: "preorder", coupon: "Save10", status: 200, preorder: true, checkout: true},
		{name: "invalid", coupon: "Save10", status: 400, checkout: true, warning: true},
		{name: "forbidden", coupon: "Save10", status: 403, checkout: true, warning: true},
		{name: "reconciled after rejection", coupon: "Save10", status: 400, checkout: true, warning: true, reconciled: true},
		{name: "rate limited", coupon: "Save10", status: 429},
		{name: "server error", coupon: "Save10", status: 503},
		{name: "unrecognized transport error", coupon: "Save10", network: true},
		{name: "cancelled", coupon: "Save10", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			database, err := db.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			lg := logger.New(filepath.Join(dir, "test.log"), slog.New(slog.NewTextHandler(io.Discard, nil)))
			st := app.NewState(storage.Paths{DataDir: dir}, config.New(database), lg, database)
			st.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu", Zone: "PT", AppKey: "test", AppSecret: "test", ConsumerKey: "test"}}
			cfg := st.Config.Get()
			cfg.NotifyWebhookURL = "https://coupon-notify.invalid/test"
			st.Config.Set(cfg)
			if tc.reconciled {
				st.History = []types.PurchaseHistoryEntry{{ID: "previous", TaskID: "coupon-task", Status: "failed"}}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var steps []string
			couponCalls, checkoutCalls := 0, 0
			var messages []string
			transport := preorderTransport(func(r *http.Request) (*http.Response, error) {
				body, status := "{}", http.StatusOK
				path := r.URL.Path
				switch {
				case r.URL.Host == "coupon-notify.invalid":
					var payload map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					message, _ := payload["message"].(string)
					messages = append(messages, message)
				case strings.HasSuffix(path, "/me/order"):
					body = `[{"orderId":123,"url":"https://example.invalid/order"}]`
				case strings.HasSuffix(path, "/auth/time"):
					body = fmt.Sprint(time.Now().Unix())
				case strings.Contains(path, "/order/catalog/public/eco"):
					body = `{"plans":[{"planCode":"24sk202"}]}`
				case strings.Contains(path, "/dedicated/server/datacenter/availabilities"):
					body = `[{"fqn":"24sk202","datacenters":[{"datacenter":"gra","availability":"1H-low"}]}]`
				case strings.HasSuffix(path, "/order/cart") && r.Method == "POST":
					steps = append(steps, "cart")
					body = `{"cartId":"coupon-cart"}`
				case strings.HasSuffix(path, "/coupon-cart/assign"):
					steps = append(steps, "assign")
				case strings.HasSuffix(path, "/coupon-cart/eco") && r.Method == "POST":
					steps = append(steps, "item")
					body = `{"itemId":1}`
				case strings.HasSuffix(path, "/item/1/configuration"):
					steps = append(steps, "config")
				case strings.HasSuffix(path, "/coupon-cart/coupon") && r.Method == "POST":
					couponCalls++
					steps = append(steps, "coupon")
					var payload map[string]string
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["coupon"] != "Save10" {
						t.Errorf("coupon must be trimmed without changing case: %v, %v", payload, err)
					}
					if tc.cancel {
						cancel()
						return nil, context.Canceled
					}
					if tc.network {
						return nil, errors.New("transport closed")
					}
					status = tc.status
					body = `["Save10"]`
					if status != 200 {
						body = `{"message":"coupon fixture rejection"}`
					}
				case strings.HasSuffix(path, "/coupon-cart/checkout") && r.Method == "POST":
					checkoutCalls++
					steps = append(steps, "checkout")
					if tc.coupon != "  " && (len(steps) < 2 || steps[len(steps)-2] != "coupon") {
						t.Errorf("coupon must precede checkout after configuration: %v", steps)
					}
					var payload map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					if payload["autoPayWithPreferredPaymentMethod"] != false {
						t.Error("coupon changed auto-pay default")
					}
					// No real order; omit ID so the fixture also starts no detail-fetch goroutine.
					body = `{}`
					if tc.reconciled {
						status = http.StatusServiceUnavailable
						body = `{"message":"checkout timeout"}`
					}
				case strings.HasSuffix(path, "/order/cart/coupon-cart") && r.Method == "DELETE":
					steps = append(steps, "cleanup")
				default:
					return nil, fmt.Errorf("unexpected fixture request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			http.DefaultTransport = transport
			client, err := st.OVH.ClientFor("eu")
			if err != nil {
				t.Fatal(err)
			}
			client.Client = &http.Client{Transport: transport}
			item := &types.QueueItem{ID: "coupon-task", AccountID: "eu", PlanCode: "24sk202", Datacenter: "gra", Coupon: tc.coupon, Preorder: tc.preorder}
			outcome := PurchaseServer(ctx, st, item)
			wantCoupon := 1
			if strings.TrimSpace(tc.coupon) == "" {
				wantCoupon = 0
			}
			wantCheckout := 0
			if tc.checkout {
				wantCheckout = 1
			}
			if couponCalls != wantCoupon || checkoutCalls != wantCheckout || outcome.Success != tc.checkout || outcome.PauseTask || outcome.Attempted || outcome.Cancelled != tc.cancel {
				t.Fatalf("unexpected flow: outcome=%+v coupon=%d checkout=%d steps=%v", outcome, couponCalls, checkoutCalls, steps)
			}
			if tc.checkout {
				history, err := st.DB.ListHistory()
				if err != nil || len(history) != 1 || history[0].Status != "success" || (history[0].CouponWarning != "") != tc.warning {
					t.Fatalf("coupon warning not persisted with successful order: %+v %v", history, err)
				}
				if len(messages) != 1 || strings.Contains(messages[0], "未使用优惠码") != tc.warning {
					t.Fatalf("notification missing or misleading: %v", messages)
				}
				if tc.warning && !strings.Contains(history[0].CouponWarning, "未使用优惠码") {
					t.Fatal(history[0].CouponWarning)
				}
			}
			if !tc.checkout && (len(steps) == 0 || steps[len(steps)-1] != "cleanup") {
				t.Fatalf("failed cart was not cleaned: %v", steps)
			}
		})
	}
}
