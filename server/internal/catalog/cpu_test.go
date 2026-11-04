package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

type cpuCatalogTransport func(*http.Request) (*http.Response, error)

func (f cpuCatalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The plan/product relation and CPU object match OVH's EU, CA and US catalogs.
func TestLoadServerListResolvesProductCPU(t *testing.T) {
	for _, region := range []struct{ sub, endpoint, suffix string }{{"IE", "ovh-eu", ""}, {"CA", "ovh-ca", ""}, {"US", "ovh-us", "-us"}} {
		for _, tc := range []struct {
			name, want string
			change     func(plan, product, cpu, doc map[string]interface{})
		}{
			{name: "hardware table wins over names", want: "Intel Xeon E5-1650v4"},
			{name: "description without pipe", want: "Intel Xeon E5-1650v4", change: func(_, product, cpu, _ map[string]interface{}) {
				delete(cpu, "model")
				product["description"] = "KS-5-B - Intel Xeon E5-1650v4"
			}},
			{name: "brand already in model", want: "Intel Xeon E5-1650v4", change: func(_, _, cpu, _ map[string]interface{}) { cpu["model"] = "Intel Xeon E5-1650v4" }},
			{name: "two physical processors", want: "2x Intel Xeon E5-1650v4", change: func(_, _, cpu, _ map[string]interface{}) { cpu["number"] = 2 }},
			{name: "missing brand", want: "Xeon E5-1650v4", change: func(_, _, cpu, _ map[string]interface{}) { delete(cpu, "brand") }},
			{name: "string CPU", want: "Intel Xeon E5-1650v4", change: func(_, product, _, _ map[string]interface{}) {
				product["blobs"] = map[string]interface{}{"technical": map[string]interface{}{"server": map[string]interface{}{"cpu": "Intel Xeon E5-1650v4"}}}
			}},
			{name: "missing products still reads details", want: "AMD EPYC 4244P", change: func(plan, _, _, doc map[string]interface{}) {
				delete(doc, "products")
				delete(plan, "displayName")
				plan["details"] = map[string]interface{}{"properties": []interface{}{map[string]interface{}{"name": "cpu", "value": "AMD EPYC 4244P"}}}
			}},
			{name: "SYSLE details are not blocked by family label", want: "AMD EPYC 4244P", change: func(plan, _, _, doc map[string]interface{}) {
				doc["products"] = nil
				plan["planCode"] = "27sysle01-v1" + region.suffix
				plan["details"] = map[string]interface{}{"properties": []interface{}{map[string]interface{}{"name": "cpu", "value": "AMD EPYC 4244P"}}}
			}},
			{name: "inline product configurations", want: "Intel Xeon E-2386G", change: func(plan, product, _, doc map[string]interface{}) {
				delete(doc, "products")
				product["blobs"], product["description"] = nil, "Server hardware"
				product["configurations"] = []interface{}{map[string]interface{}{"name": "cpu", "value": "Intel Xeon E-2386G"}}
				plan["product"] = product
			}},
			{name: "name fallback keeps full model and brand", want: "Intel Xeon E5-1650v4 6c/12t 3.6GHz", change: func(plan, _, _, doc map[string]interface{}) {
				delete(doc, "products")
				delete(plan, "displayName")
				plan["invoiceName"] = "KS-5-B | Intel Xeon E5-1650v4 6c/12t 3.6GHz, 64 GB RAM"
			}},
			{name: "plan description fallback preserves case without pipe", want: "Intel Xeon E5-1650v4", change: func(plan, _, _, doc map[string]interface{}) {
				delete(doc, "products")
				delete(plan, "displayName")
				plan["description"] = "Server with Intel Xeon E5-1650v4, 64 GB RAM"
			}},
			{name: "unknown foreign key remains unknown", want: "N/A", change: func(plan, _, _, _ map[string]interface{}) {
				plan["product"] = "missing-product"
				delete(plan, "displayName")
			}},
		} {
			t.Run(region.sub+"/"+tc.name, func(t *testing.T) {
				plan := map[string]interface{}{"planCode": "26sk50b-v1" + region.suffix, "invoiceName": "KS-5-B", "displayName": "KS-5-B | Intel Wrong CPU", "product": "26sk50b"}
				cpu := map[string]interface{}{"brand": "Intel", "model": "Xeon E5-1650v4", "number": 1}
				product := map[string]interface{}{"name": "26sk50b", "description": "Intel Wrong Description", "blobs": map[string]interface{}{"technical": map[string]interface{}{"server": map[string]interface{}{"cpu": cpu}}}}
				doc := map[string]interface{}{"plans": []interface{}{plan}, "products": []interface{}{nil, 42, product, map[string]interface{}{"name": plan["planCode"], "description": "Intel Wrong Foreign Key"}}}
				if tc.change != nil {
					tc.change(plan, product, cpu, doc)
				}
				raw, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				transport := cpuCatalogTransport(func(r *http.Request) (*http.Response, error) {
					if r.Method != http.MethodGet {
						return nil, fmt.Errorf("server list must not write to OVH: %s %s", r.Method, r.URL)
					}
					body := ""
					switch {
					case strings.HasSuffix(r.URL.Path, "/order/catalog/public/eco"):
						if r.URL.Query().Get("ovhSubsidiary") != region.sub {
							t.Error("wrong subsidiary", r.URL)
						}
						body = string(raw)
					case strings.HasSuffix(r.URL.Path, "/auth/time"):
						body = fmt.Sprint(time.Now().Unix())
					case strings.HasSuffix(r.URL.Path, "/availabilities"):
						body = `[{"datacenters":[{"datacenter":"gra","availability":"unavailable"}]}]`
					default:
						return nil, fmt.Errorf("unexpected fixture request: %s", r.URL)
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})
				original := http.DefaultTransport
				http.DefaultTransport = transport
				t.Cleanup(func() { http.DefaultTransport = original })
				state := testState(t)
				state.Accounts = []types.OVHAccount{{ID: "cpu", Endpoint: region.endpoint, Zone: region.sub, AppKey: "fixture", AppSecret: "fixture", ConsumerKey: "fixture"}}
				state.OVH = ovh.NewFactory(nil, state.FindAccount)
				client, err := state.OVH.ClientFor("cpu")
				if err != nil {
					t.Fatal(err)
				}
				client.Client = &http.Client{Transport: transport}
				servers, failed := LoadServerList(state, "cpu")
				if failed != 0 || len(servers) != 1 {
					t.Fatalf("server loading failed: %d, %+v", failed, servers)
				}
				if servers[0].CPU != tc.want {
					t.Fatalf("CPU = %q, want %q", servers[0].CPU, tc.want)
				}
				if servers[0].Name != plan["invoiceName"] || servers[0].PlanCode != plan["planCode"] || len(servers[0].Datacenters) != 1 {
					t.Fatal("CPU parsing changed the plan or availability", servers[0])
				}
			})
		}
	}
}
