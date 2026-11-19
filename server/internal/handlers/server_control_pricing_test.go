package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/types"
)

// 续费价 = 主服务 + 同币种同周期的选项(与 OVH 官方控制台 Service.addOptions 一致);
// 其它币种的选项不硬加并标 partial;安装费 = 目录里型号 + 各选项(plan.code)的安装费,不含税。
func TestServerPricingSumsOptionsAndCatalogSetupFee(t *testing.T) {
	st, _ := newWatchTestMonitor(t)
	st.Accounts = []types.OVHAccount{{ID: "acc", Endpoint: "ovh-eu", Zone: "IE", AppKey: "test", AppSecret: "test", ConsumerKey: "test", IsDefault: true}}
	client, err := st.OVH.ClientFor("acc")
	if err != nil {
		t.Fatal(err)
	}
	client.Client = &http.Client{Transport: queuePreorderTransport(func(r *http.Request) (*http.Response, error) {
		body, status := "", 200
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/time"):
			body = jsonNumber(time.Now().Unix())
		case strings.HasSuffix(r.URL.Path, "/serviceInfos"):
			body = `{"serviceId":42}`
		case strings.HasSuffix(r.URL.Path, "/services/42/options"):
			body = `[{"billing":{"plan":{"code":"softraid-2x8000sa"},"pricing":{"duration":"P1M","price":{"value":3,"currencyCode":"EUR"}}}},
			        {"billing":{"plan":{"code":"ram-32g"},"pricing":{"duration":"P1M","price":{"value":0,"currencyCode":"EUR"}}}},
			        {"billing":{"plan":{"code":"odd-usd"},"pricing":{"duration":"P1M","price":{"value":5,"currencyCode":"USD"}}}},
			        {"billing":{"pricing":null}}]`
		case strings.HasSuffix(r.URL.Path, "/services/42"):
			body = `{"billing":{"plan":{"code":"ks-le-3"},"pricing":{"duration":"P1M","interval":1,"price":{"value":15.99,"currencyCode":"EUR","text":"15.99 €"}}}}`
		default:
			t.Errorf("unexpected OVH request: %s %s", r.Method, r.URL.Path)
			status, body = 404, `{}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	old := configSetupFee
	t.Cleanup(func() { configSetupFee = old })
	configSetupFee = func(_ *app.State, accountID, planCode string, addons []string) (catalog.PriceParts, string, int, error) {
		if accountID != "acc" || planCode != "ks-le-3" || strings.Join(addons, ",") != "softraid-2x8000sa,ram-32g,odd-usd" {
			t.Errorf("fee lookup %s %s %v", accountID, planCode, addons)
		}
		return catalog.PriceParts{Price: 69.99, Tax: 16.1, OK: true}, "EUR", 0, nil
	}
	router := gin.New()
	router.GET("/sc/:service_name/pricing", GetServerPricing(st))
	w := payRequest(router, "GET", "/sc/ns1.ip-1-2-3.eu/pricing?account=acc", "")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var got struct {
		Renewal struct {
			Amount   float64 `json:"amount"`
			Base     float64 `json:"base"`
			Currency string  `json:"currency"`
			Duration string  `json:"duration"`
			Options  int     `json:"options"`
			Partial  bool    `json:"partial"`
		} `json:"renewal"`
		Installation struct {
			OK     bool    `json:"ok"`
			Amount float64 `json:"amount"`
			Tax    float64 `json:"tax"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	r := got.Renewal
	if r.Amount != 18.99 || r.Base != 15.99 || r.Currency != "EUR" || r.Duration != "P1M" || r.Options != 1 || !r.Partial {
		t.Fatalf("renewal %+v", r)
	}
	if !got.Installation.OK || got.Installation.Amount != 69.99 || got.Installation.Tax != 16.1 {
		t.Fatalf("installation %+v", got.Installation)
	}
}
