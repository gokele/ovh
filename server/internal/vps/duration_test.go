package vps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/orderterm"
	"github.com/ovh-buy/server/internal/types"
)

func TestVPSHostAndEveryOptionKeepSelectedPeriod(t *testing.T) {
	for _, tc := range []struct {
		name, wanted, base, addon, wrong string
		monthlyOnly                      bool
	}{
		{name: "default monthly", base: "P1M", addon: "P1M"},
		{name: "annual aliases", wanted: "P12M", base: "P1Y", addon: "P12M"},
		{name: "stored monthly host", wanted: "P12M", base: "P1Y", addon: "P12M", wrong: "vps-model"},
		{name: "stored monthly backup", wanted: "P12M", base: "P1Y", addon: "P12M", wrong: "backup"},
		{name: "stored monthly storage", wanted: "P12M", base: "P1Y", addon: "P12M", wrong: "storage"},
		{name: "stored monthly implicit OS", wanted: "P12M", base: "P1Y", addon: "P12M", wrong: "os-license"},
		{name: "monthly-only mandatory backup", wanted: "P12M", base: "P1Y", addon: "P12M", monthlyOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := lifecycleState(t)
			state.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu", Zone: "IE", AppKey: "test", AppSecret: "test", ConsumerKey: "test"}}
			sub := types.VPSSubscription{ID: "period", Duration: tc.wanted, PlanCode: "vps-model", OvhSubsidiary: "IE", OS: "Windows 2025", AutoOrder: true, AutoOrderAccountID: "eu", Quantity: 2}
			state.VPSSubscriptions = []types.VPSSubscription{sub}
			items := map[int64]map[string]interface{}{}
			posted := map[string]int{}
			checkouts := 0
			mode := "default"
			if tc.wanted != "" {
				mode = "upfront12"
			}
			transport := purchaseTestTransport(func(r *http.Request) (*http.Response, error) {
				path := r.URL.Path
				var body interface{} = map[string]interface{}{}
				switch {
				case strings.HasSuffix(path, "/auth/time"):
					body = time.Now().Unix()
				case r.Method == "DELETE":
				case strings.HasSuffix(path, "/order/cart"):
					body = map[string]interface{}{"cartId": "period-cart"}
				case strings.HasSuffix(path, "/assign"):
				case r.Method == "GET" && strings.HasSuffix(path, "/vps"):
					body = []interface{}{map[string]interface{}{"planCode": sub.PlanCode, "prices": []interface{}{map[string]interface{}{"duration": tc.base, "pricingMode": mode}}}}
				case r.Method == "GET" && strings.HasSuffix(path, "/vps/options"):
					opts := []interface{}{}
					for _, family := range []string{"automatedBackup", "storage"} {
						code, duration, pricingMode := "storage", tc.addon, mode
						if family == "automatedBackup" {
							code = "backup"
							if tc.monthlyOnly {
								duration, pricingMode = "P1M", "default"
							}
						}
						opts = append(opts, map[string]interface{}{"family": family, "mandatory": true, "planCode": code, "prices": []interface{}{map[string]interface{}{"duration": duration, "pricingMode": pricingMode, "capacities": []string{"renew"}, "price": map[string]interface{}{"value": 1}}}})
					}
					body = opts
				case r.Method == "POST" && (strings.HasSuffix(path, "/vps") || strings.HasSuffix(path, "/vps/options")):
					var payload map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						return nil, err
					}
					code, _ := payload["planCode"].(string)
					duration := tc.addon
					if code == sub.PlanCode {
						duration = tc.base
					}
					if payload["duration"] != duration || payload["pricingMode"] != mode || payload["quantity"] != float64(2) {
						t.Error("selected period/mode/quantity changed", payload)
					}
					posted[code]++
					id := int64(len(items) + 1)
					items[id] = map[string]interface{}{"itemId": id, "duration": payload["duration"], "settings": map[string]interface{}{"planCode": code}}
					if tc.wrong == code {
						items[id]["duration"] = "P1M"
					}
					body = items[id]
				case strings.HasSuffix(path, "/requiredConfiguration"):
					body = []interface{}{map[string]interface{}{"label": "vps_datacenter", "allowedValues": []string{"DE"}}, map[string]interface{}{"label": "vps_os", "allowedValues": []string{sub.OS}}}
				case r.Method == "POST" && strings.HasSuffix(path, "/configuration"):
					var payload map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						return nil, err
					}
					if payload["label"] == "vps_os" {
						// OVH can insert a license item while applying the OS configuration.
						duration := tc.base
						if tc.wrong == "os-license" {
							duration = "P1M"
						}
						items[2] = map[string]interface{}{"itemId": 2, "duration": duration, "settings": map[string]interface{}{"planCode": "os-license"}}
					}
				case r.Method == "GET" && strings.HasSuffix(path, "/order/cart/period-cart"):
					ids := []int64{}
					for id := range items {
						ids = append(ids, id)
					}
					body = map[string]interface{}{"items": ids}
				case r.Method == "GET" && strings.Contains(path, "/item/"):
					id, err := strconv.ParseInt(path[strings.LastIndex(path, "/")+1:], 10, 64)
					if err != nil || items[id] == nil {
						return nil, fmt.Errorf("unknown item: %s", path)
					}
					body = items[id]
				case strings.HasSuffix(path, "/checkout"):
					checkouts++
					body = map[string]interface{}{"orderId": 123}
				default:
					return nil, fmt.Errorf("unexpected fixture request: %s %s", r.Method, r.URL)
				}
				raw, err := json.Marshal(body)
				return testVPSResponse(r, 200, string(raw)), err
			})
			client, err := state.OVH.ClientFor("eu")
			if err != nil {
				t.Fatal(err)
			}
			client.Client = &http.Client{Transport: transport}
			out := purchaseVPS(context.Background(), state, sub, "DE")
			if tc.wrong != "" || tc.monthlyOnly {
				if !out.Fatal || out.Success || checkouts != 0 {
					t.Fatal("wrong period reached checkout", out, checkouts)
				}
				return
			}
			if !out.Success || checkouts != 1 {
				t.Fatal(out, checkouts)
			}
			for _, code := range []string{sub.PlanCode, "backup", "storage"} {
				if posted[code] != 1 {
					t.Fatal("component omitted or added twice", posted)
				}
			}
			for _, item := range items {
				if !orderterm.Same(item["duration"].(string), tc.wanted) {
					t.Fatal("stored period differs from selection", item)
				}
			}
		})
	}
}
