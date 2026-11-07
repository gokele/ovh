package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/purchase"
	"github.com/ovh-buy/server/internal/types"
)

// This diagnostic exercises the committed release, including unsupported request fields.
// Every HTTP request is intercepted; no live cart, order or payment can be created.
func TestUserPreorderEndToEndSimulation(t *testing.T) {
	const plan = "27skle02-v1"
	const ram = "ram-32g-ddr3-ecc-1333"
	const nvme = "softraid-2x450nvme"
	const hdd = "softraid-2x2000sa"
	fixture, err := os.ReadFile("../purchase/testdata/preorder-stock.json")
	if err != nil || !bytes.Contains(fixture, []byte(plan+"."+ram+"."+nvme)) {
		t.Fatal("simulation configuration is missing from the captured public stock feed", err)
	}
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	for index, tc := range []struct {
		name, duration, offeredRAM, offeredDisk, dc string
		shorthand, hintFields, priceField           bool
		checkout                                    bool
	}{
		{name: "exact_raw_options", checkout: true},
		{name: "annual_host_and_all_addons", duration: "P12M", checkout: true},
		{name: "ECC_mismatch_is_blocked", offeredRAM: "ram-32g-ddr3-noecc-1333"},
		{name: "frequency_drift_is_blocked", offeredRAM: "ram-32g-ddr3-ecc-1600"},
		{name: "mixed_disk_substitution_is_blocked", offeredDisk: "softraid-2x450nvme-2x2000sa"},
		{name: "wrong_datacenter_is_blocked", dc: "fra"},
		{name: "short_options_are_not_supported", shorthand: true},
		{name: "unsupported_hint_fields_are_rejected", hintFields: true},
		{name: "unsupported_maxPrice_is_rejected", priceField: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, _ := newWatchTestMonitor(t)
			duration := tc.duration
			if duration == "" {
				duration = "P1M"
			}
			dc := tc.dc
			if dc == "" {
				dc = "gra"
			}
			offeredRAM, offeredDisk := tc.offeredRAM, tc.offeredDisk
			if offeredRAM == "" {
				offeredRAM = ram
			}
			if offeredDisk == "" {
				offeredDisk = nvme
			}
			open := false
			carts, checkouts, previews := 0, 0, 0
			items := map[int64]map[string]interface{}{}
			var added []string
			var paymentFlags []bool
			row := func(disk string) map[string]interface{} {
				return map[string]interface{}{
					"planCode": plan, "server": "27skle02", "memory": ram, "storage": disk,
					"fqn":         plan + "." + ram + "." + disk,
					"datacenters": []map[string]interface{}{{"datacenter": "gra", "availability": "1H-low"}},
				}
			}
			response := func(r *http.Request, status int, body interface{}) (*http.Response, error) {
				data, err := json.Marshal(body)
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
			}
			transport := queuePreorderTransport(func(r *http.Request) (*http.Response, error) {
				path := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/1.0"), "/v1")
				switch {
				case path == "/auth/time":
					return response(r, 200, time.Now().Unix())
				case path == "/order/catalog/public/eco":
					return response(r, 200, map[string]interface{}{"plans": []interface{}{}, "addons": []interface{}{}})
				case strings.HasSuffix(path, "/availabilities"):
					if r.URL.Host != "eu.api.ovh.com" {
						return response(r, 200, []interface{}{})
					}
					// HDD appears first so ignored hint fields cannot accidentally look correct.
					return response(r, 200, []map[string]interface{}{row(hdd), row(nvme)})
				case path == "/order/cart" && r.Method == "POST":
					carts++
					items = map[int64]map[string]interface{}{}
					added = nil
					return response(r, 200, map[string]interface{}{"cartId": "simulation-cart", "expire": "2099-01-01T00:00:00Z"})
				case strings.HasSuffix(path, "/assign") || strings.HasSuffix(path, "/configuration") || r.Method == "DELETE":
					return response(r, 200, map[string]interface{}{})
				case path == "/order/cart/simulation-cart/eco" && r.Method == "GET":
					if !open {
						return response(r, 200, []interface{}{})
					}
					return response(r, 200, []map[string]interface{}{{"planCode": plan, "prices": []map[string]interface{}{{"duration": duration, "pricingMode": "default", "price": map[string]interface{}{"value": 30, "currencyCode": "EUR"}}}}})
				case strings.HasSuffix(path, "/eco/options") && r.Method == "GET":
					options := []map[string]interface{}{}
					for _, code := range []string{offeredRAM, offeredDisk, hdd} {
						options = append(options, map[string]interface{}{"planCode": code + "-" + plan, "prices": []map[string]interface{}{{"duration": duration, "pricingMode": "default", "price": map[string]interface{}{"value": 30, "currencyCode": "EUR"}}}})
					}
					return response(r, 200, options)
				case (strings.HasSuffix(path, "/eco") || strings.HasSuffix(path, "/eco/options")) && r.Method == "POST":
					if !open {
						return response(r, 400, map[string]interface{}{"message": "not yet on sale"})
					}
					var body map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						return nil, err
					}
					if body["duration"] != duration {
						t.Errorf("wrong period: %v", body)
					}
					if body["quantity"] != float64(1) {
						t.Errorf("wrong quantity: %v", body)
					}
					code, _ := body["planCode"].(string)
					id := int64(len(items) + 1)
					items[id] = map[string]interface{}{
						"itemId": id, "duration": body["duration"], "options": []int64{},
						"settings": map[string]interface{}{"planCode": code, "quantity": 1},
					}
					if parent, ok := body["itemId"].(float64); ok {
						p := items[int64(parent)]
						p["options"] = append(p["options"].([]int64), id)
						added = append(added, code)
					}
					return response(r, 200, map[string]interface{}{"itemId": id})
				case path == "/order/cart/simulation-cart" && r.Method == "GET":
					ids := []int64{}
					for id := range items {
						ids = append(ids, id)
					}
					return response(r, 200, map[string]interface{}{"cartId": "simulation-cart", "items": ids})
				case strings.HasPrefix(path, "/order/cart/simulation-cart/item/") && r.Method == "GET":
					id, err := strconv.ParseInt(path[strings.LastIndex(path, "/")+1:], 10, 64)
					if err != nil {
						return nil, err
					}
					return response(r, 200, items[id])
				case strings.HasSuffix(path, "/checkout") && r.Method == "GET":
					previews++
					return response(r, 200, map[string]interface{}{"prices": map[string]interface{}{
						"withoutTax": map[string]interface{}{"value": 99.0, "currencyCode": "EUR"},
						"withTax":    map[string]interface{}{"value": 118.8, "currencyCode": "EUR"},
					}})
				case strings.HasSuffix(path, "/checkout") && r.Method == "POST":
					var body map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						return nil, err
					}
					paid, _ := body["autoPayWithPreferredPaymentMethod"].(bool)
					paymentFlags = append(paymentFlags, paid)
					checkouts++
					return response(r, 200, map[string]interface{}{"orderId": 123, "url": "https://example.invalid/order/123"})
				case path == "/me/order/123/status":
					return response(r, 200, "notPaid")
				case path == "/me/order/123":
					return response(r, 200, map[string]interface{}{"orderId": 123})
				default:
					return nil, fmt.Errorf("blocked unexpected outbound request: %s %s", r.Method, r.URL)
				}
			})
			http.DefaultTransport = transport
			account := types.OVHAccount{ID: fmt.Sprintf("simulation-%d", index), Endpoint: "ovh-eu", Zone: fmt.Sprintf("SIMULATION-%d", index), AppKey: "test", AppSecret: "test", ConsumerKey: "test"}
			state.Accounts = []types.OVHAccount{account}
			base, err := state.OVH.ClientFor(account.ID)
			if err != nil {
				t.Fatal(err)
			}
			base.Client = &http.Client{Transport: transport}
			body := map[string]interface{}{
				"account_id": account.ID, "planCode": plan, "datacenter": dc,
				"options": []string{ram, nvme}, "preorder": true, "autoPay": false, "duration": duration,
			}
			if tc.shorthand {
				body["options"] = []string{"32g", "2x450"}
			}
			if tc.hintFields {
				delete(body, "options")
				body["hwRam"], body["hwDisk"], body["allowFreqDrift"] = "32g", "2x450nvme", false
				body["maxPrice"] = 1.0
			}
			if tc.priceField {
				body["maxPrice"] = 1.0
			}
			raw, _ := json.Marshal(body)
			router := gin.New()
			router.POST("/queue", AddQueueItem(state))
			request := httptest.NewRequest("POST", "/queue", bytes.NewReader(raw))
			request.Header.Set("Content-Type", "application/json")
			record := httptest.NewRecorder()
			router.ServeHTTP(record, request)
			if tc.hintFields || tc.priceField {
				queued, err := state.DB.ListQueue()
				if record.Code != http.StatusBadRequest || err != nil || len(queued) != 0 || carts != 0 || checkouts != 0 {
					t.Fatalf("unsupported constraints were accepted: status=%d queue=%d carts=%d checkout=%d err=%v", record.Code, len(queued), carts, checkouts, err)
				}
				t.Logf("unsupported constraints rejected before queue/cart/order creation: %d", record.Code)
				return
			}
			if record.Code != 200 {
				t.Fatalf("enqueue failed: %d %s", record.Code, record.Body)
			}
			queued, err := state.DB.ListQueue()
			if err != nil || len(queued) != 1 || !queued[0].Preorder {
				t.Fatalf("enqueue persistence failed: %v %v", queued, err)
			}
			item := queued[0]
			before := purchase.PurchaseServer(context.Background(), state, &item)
			if checkouts != 0 || before.Success {
				t.Fatalf("ordered before sale: %+v", before)
			}
			open = true
			after := purchase.PurchaseServer(context.Background(), state, &item)
			if after.Success != tc.checkout || (checkouts == 1) != tc.checkout {
				t.Fatalf("unexpected result: %+v; checkouts=%d options=%v", after, checkouts, added)
			}
			if tc.checkout {
				wantDisk := nvme + "-" + plan
				expected := map[string]bool{ram + "-" + plan: true, wantDisk: true}
				if len(added) != 2 {
					t.Fatalf("unexpected addons: %v", added)
				}
				for _, code := range added {
					if !expected[code] {
						t.Fatalf("unexpected hardware: %v", added)
					}
				}
				if len(paymentFlags) != 1 || paymentFlags[0] {
					t.Fatal("simulation changed auto-payment")
				}
				again := purchase.PurchaseServer(context.Background(), state, &item)
				if !again.PauseTask || checkouts != 1 {
					t.Fatalf("duplicate checkout: %+v %d", again, checkouts)
				}
				deadline := time.Now().Add(4 * time.Second)
				for {
					history, err := state.DB.ListHistory()
					if err == nil && len(history) > 0 && history[0].OrderStatus != "" {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("order detail backfill did not finish", err)
					}
					time.Sleep(time.Millisecond)
				}
			}
			t.Logf("enqueue=%d, beforeSale=%+v, afterSale=%+v, carts=%d, checkoutPOST=%d, previewGET=%d, submittedAddons=%v", record.Code, before, after, carts, checkouts, previews, added)

		})
	}
}
