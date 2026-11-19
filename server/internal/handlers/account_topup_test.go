package handlers

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/types"
)

// topupFixture 完全离线的 OVH 预付款账户 / 券码响应,绝不访问真实账户。
func topupFixture(t *testing.T, region string, canCredit bool, code int) (*gin.Engine, *atomic.Int32, *atomic.Value) {
	t.Helper()
	st, _ := newWatchTestMonitor(t)
	st.Accounts = []types.OVHAccount{{ID: "acc", Name: "Topup account", Endpoint: "ovh-" + strings.ToLower(region), Zone: "IE", AppKey: "test", AppSecret: "test", ConsumerKey: "test", IsDefault: true}}
	client, err := st.OVH.ClientFor("acc")
	if err != nil {
		t.Fatal(err)
	}
	writes, payload := &atomic.Int32{}, &atomic.Value{}
	credit := "false"
	if canCredit {
		credit = "true"
	}
	client.Client = &http.Client{Transport: queuePreorderTransport(func(r *http.Request) (*http.Response, error) {
		body, status := "", 200
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/time"):
			body = jsonNumber(time.Now().Unix())
		case strings.HasSuffix(r.URL.Path, "/ovhAccount/FR.ovh-1/creditOrder"):
			writes.Add(1)
			b, _ := io.ReadAll(r.Body)
			payload.Store(string(b))
			status = code
			body = `{"message":"rejected"}`
			if code == 200 {
				body = `{"orderId":555,"url":"https://www.ovh.com/cgi-bin/order/display-order.cgi?orderId=555","priceWithTax":{"value":100,"currencyCode":"EUR","text":"100.00 €"}}`
			}
		case strings.HasSuffix(r.URL.Path, "/ovhAccount/FR.ovh-1"):
			body = `{"ovhAccountId":"FR.ovh-1","balance":{"value":12.5,"currencyCode":"EUR","text":"12.50 €"},"canBeCredited":` + credit + `,"isActive":true}`
		case strings.HasSuffix(r.URL.Path, "/ovhAccount"):
			body = `["FR.ovh-1"]`
		case strings.HasSuffix(r.URL.Path, "/credit/code"):
			writes.Add(1)
			b, _ := io.ReadAll(r.Body)
			payload.Store(string(b))
			status = code
			body = `{"message":"invalid code"}`
			if code == 200 {
				body = `{"balanceName":"VOUCHER-9","type":"VOUCHER","amount":{"value":20,"currencyCode":"EUR","text":"20.00 €"}}`
			}
		default:
			t.Errorf("unexpected OVH request: %s %s", r.Method, r.URL.Path)
			status, body = 404, `{}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	router := gin.New()
	router.GET("/topup", GetTopupInfo(st))
	router.POST("/topup", CreateTopupOrder(st))
	router.POST("/credit-code", RedeemCreditCode(st))
	return router, writes, payload
}

// 金额按「分」发送:100 → 10000;未确认、非法金额、超上限都不发请求。
func TestTopupSendsCentsAndValidates(t *testing.T) {
	router, writes, payload := topupFixture(t, "EU", true, 200)
	if w := payRequest(router, "GET", "/topup?account=acc", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"supported":true`) || !strings.Contains(w.Body.String(), `"FR.ovh-1"`) {
		t.Fatalf("info %d %s", w.Code, w.Body.String())
	}
	for _, bad := range []string{
		`{"confirm":false,"ovhAccountId":"FR.ovh-1","amount":100}`,
		`{"confirm":true,"ovhAccountId":"FR.ovh-1","amount":0}`,
		`{"confirm":true,"ovhAccountId":"FR.ovh-1","amount":-5}`,
		`{"confirm":true,"ovhAccountId":"FR.ovh-1","amount":10.005}`,
		`{"confirm":true,"ovhAccountId":"FR.ovh-1","amount":100001}`,
		`{"confirm":true,"amount":100}`,
	} {
		if w := payRequest(router, "POST", "/topup?account=acc", bad); w.Code != 400 {
			t.Fatalf("%s -> %d", bad, w.Code)
		}
	}
	if writes.Load() != 0 {
		t.Fatalf("invalid input reached OVH")
	}
	w := payRequest(router, "POST", "/topup?account=acc", `{"confirm":true,"ovhAccountId":"FR.ovh-1","amount":100}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"orderId":555`) || writes.Load() != 1 {
		t.Fatalf("topup %d %s", w.Code, w.Body.String())
	}
	if got, _ := payload.Load().(string); got != `{"amount":10000}` {
		t.Fatalf("payload %s", got)
	}
	if w := payRequest(router, "POST", "/topup?account=acc", `{"confirm":true,"ovhAccountId":"FR.ovh-1","amount":12.34}`); w.Code != 200 {
		t.Fatalf("decimal %d", w.Code)
	}
	if got, _ := payload.Load().(string); got != `{"amount":1234}` {
		t.Fatalf("decimal payload %s", got)
	}
}

// US 没有预付款账户接口;不能充值的账户不发 creditOrder。
func TestTopupRegionAndCreditability(t *testing.T) {
	router, writes, _ := topupFixture(t, "US", true, 200)
	if w := payRequest(router, "GET", "/topup?account=acc", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"supported":false`) {
		t.Fatalf("us info %d %s", w.Code, w.Body.String())
	}
	if w := payRequest(router, "POST", "/topup?account=acc", `{"confirm":true,"ovhAccountId":"FR.ovh-1","amount":100}`); w.Code != 400 || writes.Load() != 0 {
		t.Fatalf("us topup %d", w.Code)
	}
	router, writes, _ = topupFixture(t, "EU", false, 200)
	if w := payRequest(router, "POST", "/topup?account=acc", `{"confirm":true,"ovhAccountId":"FR.ovh-1","amount":100}`); w.Code != 409 || writes.Load() != 0 {
		t.Fatalf("not creditable %d writes=%d", w.Code, writes.Load())
	}
}

// 券码:去掉首尾空格后原样发送;空码不发;OVH 拒绝返回 502。
func TestRedeemCreditCode(t *testing.T) {
	router, writes, payload := topupFixture(t, "EU", true, 200)
	if w := payRequest(router, "POST", "/credit-code?account=acc", `{"code":"   "}`); w.Code != 400 || writes.Load() != 0 {
		t.Fatalf("empty code %d", w.Code)
	}
	w := payRequest(router, "POST", "/credit-code?account=acc", `{"code":"  ABC-123  "}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"VOUCHER-9"`) {
		t.Fatalf("redeem %d %s", w.Code, w.Body.String())
	}
	if got, _ := payload.Load().(string); got != `{"inputCode":"ABC-123"}` {
		t.Fatalf("payload %s", got)
	}
	router, _, _ = topupFixture(t, "EU", true, 400)
	if w := payRequest(router, "POST", "/credit-code?account=acc", `{"code":"BAD"}`); w.Code != 502 {
		t.Fatalf("rejected %d", w.Code)
	}
}
