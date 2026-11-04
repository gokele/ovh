package vps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/types"
)

type purchaseTestTransport func(*http.Request) (*http.Response, error)

func (f purchaseTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testVPSResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestVPSCheckoutUncertaintyAndCancellation(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, tc := range []struct {
		name                                         string
		duration                                     string
		status                                       int
		response                                     string
		cancel, invalidOS, annualOnly, deleteAccount bool
	}{
		{name: "gateway error", status: 504, response: `{"message":"timeout"}`},
		{name: "missing order ID", status: 200, response: `{}`},
		{name: "business rejection", status: 400, response: `{"message":"out of stock"}`},
		{name: "success", status: 200, response: `{"orderId":123,"url":"https://orders.invalid/123"}`},
		{name: "deleted account after checkout", status: 200, response: `{"orderId":123,"url":"https://orders.invalid/123"}`, deleteAccount: true},
		{name: "cancel before checkout", cancel: true},
		{name: "unsupported OS", invalidOS: true},
		{name: "annual only", annualOnly: true},
		{name: "success", annualOnly: true, duration: "P12M", status: 200, response: `{"orderId":123,"url":"https://orders.invalid/123"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := lifecycleState(t)
			state.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu", Zone: "IE", AppKey: "test", AppSecret: "test", ConsumerKey: "test"}}
			sub := types.VPSSubscription{Duration: tc.duration, ID: "checkout", PlanCode: "vps-model", OvhSubsidiary: "IE", AutoOrder: true, AutoOrderAccountID: "eu", Quantity: 1}
			if tc.invalidOS {
				sub.OS = "Windows unknown"
			}
			state.VPSSubscriptions = []types.VPSSubscription{sub}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var checkouts atomic.Int32
			var leakedLinks atomic.Int32
			transport := purchaseTestTransport(func(r *http.Request) (*http.Response, error) {
				body, status := `{}`, 200
				path := r.URL.Path
				switch {
				case strings.HasSuffix(path, "/auth/time"):
					body = fmt.Sprint(time.Now().Unix())
				case r.URL.Hostname() == "notify.invalid":
					body, _ := io.ReadAll(r.Body)
					if strings.Contains(string(body), "orders.invalid") {
						leakedLinks.Add(1)
					}
				case r.Method == "DELETE" && strings.Contains(path, "/order/cart/"):
				case strings.HasSuffix(path, "/order/cart"):
					body = `{"cartId":"fixture"}`
				case strings.HasSuffix(path, "/order/cart/fixture") && r.Method == "GET":
					body = `{"items":[1]}`
				case strings.HasSuffix(path, "/order/cart/fixture/item/1") && r.Method == "GET":
					body = `{"itemId":1,"duration":"P1M","options":[]}`
					if tc.duration != "" {
						body = strings.Replace(body, "P1M", "P1Y", 1)
					}
				case strings.HasSuffix(path, "/assign"):
				case strings.HasSuffix(path, "/requiredConfiguration"):
					body = `[{"label":"vps_datacenter","allowedValues":["DE"]},{"label":"vps_os","allowedValues":["Debian 13"]}]`
				case strings.HasSuffix(path, "/configuration"):
				case strings.HasSuffix(path, "/vps/options"):
					body = `[]`
					if tc.cancel {
						cancel()
					}
				case strings.HasSuffix(path, "/vps") && r.Method == "GET":
					body = `[{"planCode":"vps-model","prices":[{"duration":"P1M","pricingMode":"default","capacities":["renew"]}]}]`
					if tc.annualOnly {
						body = strings.Replace(body, "P1M", "P1Y", 1)
					}
				case strings.HasSuffix(path, "/vps") && r.Method == "POST":
					var payload map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					want := "P1M"
					if tc.duration != "" {
						want = "P1Y"
					}
					if payload["duration"] != want {
						t.Error("VPS period silently changed", payload)
					}
					body = `{"itemId":1}`
				case strings.HasSuffix(path, "/checkout"):
					checkouts.Add(1)
					if tc.deleteAccount {
						state.AccountsMu.Lock()
						state.Accounts = nil
						state.AccountsMu.Unlock()
					}
					status, body = tc.status, tc.response
				default:
					return nil, fmt.Errorf("unexpected fixture request: %s", r.URL)
				}
				return testVPSResponse(r, status, body), nil
			})
			http.DefaultTransport = transport
			client, err := state.OVH.ClientFor("eu")
			if err != nil {
				t.Fatal(err)
			}
			client.Client = &http.Client{Transport: transport}
			out := Outcome{}
			if tc.name == "success" || tc.deleteAccount {
				autoOrderOnRestock(ctx, state, sub, []map[string]interface{}{{"name": "DE", "code": "eu-west-lim"}})
				history, err := state.DB.ListHistory()
				if err != nil || len(history) != 1 || history[0].OrderID != "123" || history[0].Status != "success" || history[0].Duration != tc.duration {
					reason := ""
					if len(history) > 0 && history[0].ErrorMessage != nil {
						reason = *history[0].ErrorMessage
					}
					t.Fatalf("success history not durably saved: %+v %v reason=%s", history, err, reason)
				}
				subs, err := state.DB.ListVPSSubscriptions()
				if err != nil || len(subs) != 1 || subs[0].AutoOrder {
					t.Fatalf("successful subscription remained armed: %+v %v", subs, err)
				}
				out.Success = true
				if leakedLinks.Load() != 0 {
					t.Fatal("credential-bearing order link leaked to notification")
				}
			} else {
				out = purchaseVPS(ctx, state, sub, "DE")
			}
			if tc.cancel || tc.invalidOS || (tc.annualOnly && tc.duration == "") {
				if checkouts.Load() != 0 || out.Success {
					t.Fatalf("invalid/cancelled configuration reached checkout: %+v", out)
				}
				if tc.cancel && !out.Cancelled {
					t.Fatal("cancellation was reported as a failure")
				}
				return
			}
			uncertain := tc.status == 504 || tc.response == `{}`
			if out.Uncertain != uncertain || out.Success != (tc.name == "success" || tc.deleteAccount) {
				t.Fatalf("wrong outcome: %+v", out)
			}
			out = purchaseVPS(ctx, state, sub, "DE")
			want := int32(1)
			if tc.status == 400 {
				want = 2
			} else if !out.Uncertain && !((tc.name == "success" || tc.deleteAccount) && out.Cancelled) {
				t.Fatal("existing checkout was not blocked")
			}
			if checkouts.Load() != want {
				t.Fatalf("checkout requests=%d want=%d", checkouts.Load(), want)
			}
		})
	}
}
