package purchase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/types"
)

type preorderCheckoutCase struct {
	name, memory, storage, duration, cpuOption, stockPlan, datacenter string
	optionFailure, checkoutFailure                                    int
	omitMemory, monthlyAddonOnly, noCatalog, catalogOutage, noPrices  bool
	success                                                           bool
}

// All public requests are intercepted; signed requests go only to this local fake OVH.
func preorderCheckoutFixture(t *testing.T, tc preorderCheckoutCase, index int) (*app.State, types.QueueItem, *atomic.Int32, *atomic.Int32, *atomic.Bool) {
	t.Helper()
	state := queueCompletionState(t)
	const plan = "27skle03-v1-mum"
	memoryCode, storageCode := "memory-opaque", "disk-opaque"
	if tc.noCatalog || tc.catalogOutage {
		memoryCode = "ram-64g-ddr4-ecc-2666-" + plan
		storageCode = "softraid-2x480ssd-" + plan
	}
	if tc.memory == "" {
		tc.memory = "ram-64g-ddr4-ecc-2666"
	}
	if tc.storage == "" {
		tc.storage = "softraid-2x480ssd"
	}
	if tc.duration == "" {
		tc.duration = "P1M"
	}
	variantPlan := plan
	if tc.stockPlan != "" {
		variantPlan = tc.stockPlan
	}
	var carts, checkouts atomic.Int32
	var stock atomic.Bool
	stock.Store(true)
	var mu sync.Mutex
	items := map[int64]map[string]interface{}{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		write := func(value interface{}) {
			if err := json.NewEncoder(w).Encode(value); err != nil {
				t.Error(err)
			}
		}
		prices := func(duration string) []map[string]interface{} {
			return []map[string]interface{}{{"duration": duration, "pricingMode": "default", "price": map[string]interface{}{"value": 30, "currencyCode": "EUR"}}}
		}
		switch {
		case strings.HasSuffix(path, "/auth/time"):
			fmt.Fprint(w, time.Now().Unix())
		case strings.HasSuffix(path, "/availabilities"):
			availability := "unavailable"
			if stock.Load() {
				availability = "1H-low"
			}
			fmt.Fprintf(w, `[{"fqn":"%s.ram-64g-ddr4-ecc-2666.softraid-2x480ssd","datacenters":[{"datacenter":"ynm","availability":%q}]}]`, variantPlan, availability)
		case path == "/order/cart" && r.Method == "POST":
			carts.Add(1)
			fmt.Fprint(w, `{"cartId":"preorder-cart","expire":"2099-01-01T00:00:00Z"}`)
		case strings.HasSuffix(path, "/assign") || strings.HasSuffix(path, "/configuration") || r.Method == "DELETE":
			fmt.Fprint(w, `{}`)
		case strings.HasSuffix(path, "/eco") && r.Method == "GET":
			write([]map[string]interface{}{{"planCode": plan, "prices": append(prices("P1M"), prices("P1Y")...)}})
		case strings.HasSuffix(path, "/eco/options") && r.Method == "GET":
			if tc.optionFailure != 0 {
				w.WriteHeader(tc.optionFailure)
				fmt.Fprint(w, `{"message":"catalog options not available yet"}`)
				return
			}
			duration := tc.duration
			if tc.monthlyAddonOnly {
				duration = "P1M"
			}
			write([]map[string]interface{}{{"planCode": memoryCode, "prices": prices(duration)}, {"planCode": storageCode, "prices": prices(duration)}, {"planCode": "bandwidth-default", "prices": prices(duration)}})
		case (strings.HasSuffix(path, "/eco") || strings.HasSuffix(path, "/eco/options")) && r.Method == "POST":
			var body struct {
				PlanCode string `json:"planCode"`
				Duration string `json:"duration"`
				Parent   int64  `json:"itemId"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if !strings.EqualFold(body.Duration, tc.duration) && !(body.Duration == "P1Y" && tc.duration == "P12M") {
				t.Errorf("period substituted: %s", body.Duration)
			}
			mu.Lock()
			defer mu.Unlock()
			id := int64(len(items) + 1)
			if !(tc.omitMemory && body.PlanCode == memoryCode) {
				items[id] = map[string]interface{}{"itemId": id, "duration": body.Duration, "settings": map[string]string{"planCode": body.PlanCode}, "options": []int64{}}
				if parent := items[body.Parent]; parent != nil {
					parent["options"] = append(parent["options"].([]int64), id)
				}
			}
			write(map[string]interface{}{"itemId": id})
		case path == "/order/cart/preorder-cart" && r.Method == "GET":
			write(map[string]interface{}{"items": []int64{1}})
		case strings.Contains(path, "/item/") && r.Method == "GET":
			id, _ := strconv.ParseInt(path[strings.LastIndex(path, "/")+1:], 10, 64)
			mu.Lock()
			defer mu.Unlock()
			write(items[id])
		case strings.HasSuffix(path, "/summary") && r.Method == "GET":
			if !stock.Load() {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"message":"not available in ynm"}`)
				return
			}
			fmt.Fprint(w, `{"prices":{"withTax":{"value":54,"currencyCode":"EUR"}}}`)
		case strings.HasSuffix(path, "/checkout") && r.Method == "POST":
			if !stock.Load() {
				t.Error("ordered without stock")
			}
			checkouts.Add(1)
			if tc.checkoutFailure != 0 {
				w.WriteHeader(tc.checkoutFailure)
				fmt.Fprint(w, `{"message":"model is not open for ordering yet"}`)
				return
			}
			fmt.Fprint(w, `{"orderId":123}`)
		case path == "/me/order/123":
			fmt.Fprint(w, `{"orderId":123}`)
		case path == "/me/order/123/status":
			fmt.Fprint(w, `"notPaid"`)
		default:
			t.Errorf("unexpected local API %s %s", r.Method, path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(api.Close)
	monthly := func(value, tax int64) []map[string]interface{} {
		return []map[string]interface{}{{"mode": "default", "intervalUnit": "month", "interval": 1, "price": value, "tax": tax, "capacities": []string{"renew"}}}
	}
	ramPrices := monthly(500000000, 100000000)
	if tc.noPrices {
		ramPrices = nil
	}
	basePrices := append(monthly(3000000000, 600000000), map[string]interface{}{"mode": "default", "price": 1000000000, "tax": 200000000, "capacities": []string{"installation"}})
	catalog := map[string]interface{}{
		"locale": map[string]string{"currencyCode": "EUR"},
		"plans": []map[string]interface{}{{"planCode": plan, "pricings": basePrices, "configurations": []map[string]interface{}{{"name": "dedicated_datacenter", "values": []string{"ynm"}}, {"name": "region", "values": []string{"canada"}}}, "addonFamilies": []map[string]interface{}{
			{"name": "memory", "mandatory": true, "default": memoryCode, "addons": []string{memoryCode}},
			{"name": "storage", "mandatory": true, "default": storageCode, "addons": []string{storageCode}},
			{"name": "bandwidth", "mandatory": true, "default": "bandwidth-default", "addons": []string{"bandwidth-default"}},
		}}},
		"addons": []map[string]interface{}{{"planCode": memoryCode, "product": tc.memory, "pricings": ramPrices}, {"planCode": storageCode, "product": tc.storage, "pricings": monthly(0, 0)}, {"planCode": "bandwidth-default", "product": "bandwidth", "pricings": monthly(0, 0)}},
	}
	if tc.noPrices {
		catalog["plans"].([]map[string]interface{})[0]["pricings"] = nil
	}
	if tc.noCatalog {
		catalog["plans"] = []map[string]interface{}{}
		catalog["addons"] = []map[string]interface{}{}
	}
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	http.DefaultTransport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Path, "/order/catalog/public/eco") {
			return nil, fmt.Errorf("unexpected outbound request %s", r.URL)
		}
		return &http.Response{StatusCode: func() int {
			if tc.catalogOutage {
				return 503
			}
			return 200
		}(), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(encoded))), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	// Isolate the package's public-catalog cache between fixtures.
	state.Accounts = []types.OVHAccount{{ID: "preorder-fixture", Endpoint: api.URL, Zone: fmt.Sprintf("PREORDER-API-%d", index), AppKey: "test", AppSecret: "test", ConsumerKey: "test"}}
	item := types.QueueItem{ID: "preorder-limit", AccountID: "preorder-fixture", PlanCode: plan, Datacenter: "ynm", Preorder: true, Duration: tc.duration, Status: "running", MaxRetries: 1, Options: []string{"ram-64g-ddr4-ecc-2666", "softraid-2x480ssd"}}
	if tc.datacenter != "" {
		item.Datacenter = tc.datacenter
	}
	if tc.cpuOption != "" {
		item.Options = append(item.Options, tc.cpuOption)
	}
	t.Cleanup(func() {
		state.HistoryMu.Lock()
		ordered := len(state.History) > 0 && state.History[0].Status == "success"
		state.HistoryMu.Unlock()
		if ordered {
			// The asynchronous detail write must finish before the temporary DB/log directory is removed.
			waitWarm(t, func() bool {
				rows, err := state.DB.ListHistory()
				return err == nil && len(rows) > 0 && rows[0].OrderStatus != ""
			})
		}
	})
	return state, item, &carts, &checkouts, &stock
}

func TestPreorderUsesLiveOrderingAPIWithoutCPUOrPublicPrice(t *testing.T) {
	for i, tc := range []preorderCheckoutCase{
		{name: "exact hardware", success: true},
		{name: "CPU and catalog price absent", noPrices: true, success: true},
		{name: "legacy CPU choice is ignored", cpuOption: "CPU-Intel-Xeon-E5-1650v4", success: true},
		{name: "model absent from public catalog but ordering API open", noCatalog: true, success: true},
		{name: "public catalog outage does not block ordering API", catalogOutage: true, success: true},
		{name: "annual host and all options", duration: "P12M", noPrices: true, success: true},
		{name: "model version cannot change", stockPlan: "27skle03-v2-mum"},
		{name: "model cannot change", stockPlan: "27skle04-v1-mum"},
		{name: "datacenter cannot change", datacenter: "gra"},
		{name: "memory frequency cannot change", memory: "ram-64g-ddr4-ecc-2133"},
		{name: "memory capacity cannot change", memory: "ram-32g-ddr4-ecc-2666"},
		{name: "ECC cannot change", memory: "ram-64g-ddr4-noecc-2666"},
		{name: "pure disks cannot become mixed", storage: "softraid-2x480ssd-2x8000sa"},
		{name: "live ordering options not open", optionFailure: 404},
		{name: "live ordering API outage", optionFailure: 503},
		{name: "checkout not open keeps waiting", noCatalog: true, checkoutFailure: 400},
		{name: "missing stored hardware", omitMemory: true},
		{name: "annual options not open", duration: "P12M", monthlyAddonOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, item, _, checkouts, _ := preorderCheckoutFixture(t, tc, i)
			session := &cartSession{}
			t.Cleanup(session.discard)
			out := PurchaseServer(context.WithValue(context.Background(), cartSessionKey{}, session), state, &item)
			if out.Success != tc.success || (checkouts.Load() == 1) != (tc.success || tc.checkoutFailure != 0) || out.Fatal || out.Attempted {
				t.Fatalf("out=%+v checkouts=%d", out, checkouts.Load())
			}
			if !tc.success {
				if session.cart != nil {
					t.Fatal("incomplete cart remained ready")
				}
				if found, err := state.DB.GetKV(db.QueueCheckoutKey(item.ID), &map[string]interface{}{}); err != nil || found {
					t.Fatal("blocked task claimed checkout", found, err)
				}
				return
			}
			history, err := state.DB.ListHistory()
			wantOptions := 3
			if tc.noCatalog || tc.catalogOutage {
				wantOptions = 2
			}
			if err != nil || len(history) != 1 || history[0].Status != "success" || len(history[0].Options) != wantOptions {
				t.Fatalf("actual configuration not recorded: %+v %v", history, err)
			}
			for _, option := range history[0].Options {
				if strings.HasPrefix(strings.ToLower(option), "cpu-") {
					t.Fatal("CPU was used as a matching condition")
				}
			}
		})
	}
}

func TestPreorderRejectsProductAliasDriftBetweenStockSelectionAndPreparation(t *testing.T) {
	state, item, carts, checkouts, _ := preorderCheckoutFixture(t, preorderCheckoutCase{memory: "ram-32g-ddr4-ecc-2666"}, 900)
	client, err := state.OVH.IsolatedClientFor(item.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	// These SKUs came from an earlier catalog where the memory product was 64GB.
	fqn := item.PlanCode + ".ram-64g-ddr4-ecc-2666.softraid-2x480ssd"
	cart, out := prepareCart(context.Background(), state, &item, state.Accounts[0], client, []string{"memory-opaque", "disk-opaque"}, item.Datacenter, fqn, newTimeline(), "", true)
	if cart != nil || !out.WaitingForCatalog || carts.Load() != 0 || checkouts.Load() != 0 {
		t.Fatal("a changed product alias escaped the stock constraints", out, carts.Load(), checkouts.Load())
	}
}

func TestPreorderPoolKeepsExactHardwareAndNeverOrdersDuringPreparation(t *testing.T) {
	for i, tc := range []preorderCheckoutCase{
		{name: "exact standby", success: true},
		{name: "changed frequency", memory: "ram-64g-ddr4-ecc-2133"},
		{name: "mixed disk", storage: "softraid-2x480ssd-2x8000sa"},
		{name: "options unavailable", optionFailure: 503},
		{name: "stored memory missing", omitMemory: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, item, carts, checkouts, stock := preorderCheckoutFixture(t, tc, 100+i)
			stock.Store(false)
			pool := NewCartPool(state)
			state.OrderPrewarm = pool
			t.Cleanup(pool.Close)
			if err := pool.Prepare("web:preorder", []types.QueueItem{item}, time.Minute, nil); err != nil {
				t.Fatal(err)
			}
			waitWarm(t, func() bool {
				s := pool.Status("web:preorder")
				return len(s) == 1 && (s[0].State == "ready" || s[0].State == "waiting")
			})
			if pool.Ready(item) != tc.success || checkouts.Load() != 0 {
				t.Fatal("unsafe pool", pool.Status("web:preorder"))
			}
			if !tc.success {
				if pool.Take(item) != nil {
					t.Fatal("incomplete cart taken")
				}
				return
			}
			stock.Store(true)
			signal := app.StockSignal{AccountID: item.AccountID, PlanCode: item.PlanCode, DetectedAt: time.Now(), Variants: []map[string]interface{}{{"fqn": item.PlanCode + ".ram-64g-ddr4-ecc-2666.softraid-2x480ssd", "datacenters": []interface{}{map[string]interface{}{"datacenter": "ynm", "availability": "1H-low"}}}}}
			out := PurchaseServer(context.WithValue(context.Background(), stockSignalKey{}, signal), state, &item)
			if !out.Success || checkouts.Load() != 1 || carts.Load() != 1 {
				t.Fatal("standby not reused", out, checkouts.Load(), carts.Load())
			}
		})
	}
}
