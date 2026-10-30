package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

// paymentFixture 提供完全离线的 OVH 订单与扣款响应,绝不访问真实账户。
func paymentFixture(t *testing.T, region, status, expiry string, code int) (*app.State, *gin.Engine, *atomic.Int32, *atomic.Value) {
	t.Helper()
	st, _ := newWatchTestMonitor(t)
	st.Accounts = []types.OVHAccount{{ID: "pay", Name: "Payment account", Endpoint: "ovh-" + strings.ToLower(region), Zone: "IE", AppKey: "test", AppSecret: "test", ConsumerKey: "test", IsDefault: true}}
	client, err := st.OVH.ClientFor("pay")
	if err != nil {
		t.Fatal(err)
	}
	calls, payload := &atomic.Int32{}, &atomic.Value{}
	client.Client = &http.Client{Transport: queuePreorderTransport(func(r *http.Request) (*http.Response, error) {
		body, responseCode := "", 200
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/time"):
			body = jsonNumber(time.Now().Unix())
		case strings.HasSuffix(r.URL.Path, "/order/123/status"):
			b, _ := json.Marshal(status)
			body = string(b)
		case strings.HasSuffix(r.URL.Path, "/order/123/paymentMethods"):
			body = `{"registered":[7],"paymentMethods":[{"paymentMethodId":7,"paymentType":"CREDIT_CARD","label":"Saved card","status":"VALID","default":true},{"paymentMethodId":8,"paymentType":"CREDIT_CARD","status":"INVALID"}]}`
		case strings.HasSuffix(r.URL.Path, "/order/123/availablePaymentMethod"):
			body = `[{"id":7,"type":"CREDIT_CARD"}]`
		case strings.HasSuffix(r.URL.Path, "/order/123/pay"):
			calls.Add(1)
			b, _ := io.ReadAll(r.Body)
			payload.Store(string(b))
			responseCode = code
			body = `{"message":"test rejection"}`
		case strings.HasSuffix(r.URL.Path, "/order/123"):
			b, _ := json.Marshal(map[string]interface{}{"orderId": 123, "expirationDate": expiry, "priceWithTax": map[string]interface{}{"value": 39.98, "currencyCode": "EUR", "text": "€39.98"}})
			body = string(b)
		default:
			t.Errorf("unexpected OVH request: %s %s", r.Method, r.URL.Path)
			responseCode = 404
			body = `{}`
		}
		return &http.Response{StatusCode: responseCode, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	router := gin.New()
	router.GET("/orders/:order_id/payment", GetOrderPaymentOptions(st))
	router.POST("/orders/:order_id/pay", PayAccountOrder(st))
	return st, router, calls, payload
}

// jsonNumber 返回 OVH 时间接口的 JSON 数字。
func jsonNumber(n int64) string { b, _ := json.Marshal(n); return string(b) }

// payRequest 执行应用 API 请求,不调用真实网络。
func payRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

const validPayment = `{"confirm":true,"paymentMethodId":7,"paymentType":"CREDIT_CARD","amount":39.98,"currency":"EUR"}`

func TestOrderPaymentRegionalPayloadAndDuplicateProtection(t *testing.T) {
	for _, region := range []string{"EU", "CA", "US"} {
		t.Run(region, func(t *testing.T) {
			_, r, calls, payload := paymentFixture(t, region, "notPaid", "2099-01-01T00:00:00Z", 200)
			w := payRequest(r, "GET", "/orders/123/payment?account=pay", "")
			if w.Code != 200 || calls.Load() != 0 {
				t.Fatalf("read-only preflight %d %s", w.Code, w.Body)
			}
			var info orderPaymentInfo
			if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil || len(info.Methods) != 1 || info.Methods[0].ID != 7 {
				t.Fatalf("methods: %+v %v", info, err)
			}
			// Two simultaneous clicks must share one durable claim.
			var wg sync.WaitGroup
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					w := payRequest(r, "POST", "/orders/123/pay?account=pay", validPayment)
					if w.Code != 200 && w.Code != 409 {
						t.Errorf("pay %d %s", w.Code, w.Body)
					}
				}()
			}
			wg.Wait()
			if calls.Load() != 1 {
				t.Fatalf("pay calls=%d", calls.Load())
			}
			want := `{"paymentMethod":{"id":7}}`
			if region == "US" {
				want = `{"paymentMethod":{"id":7,"type":"CREDIT_CARD"}}`
			}
			if payload.Load() != want {
				t.Fatalf("payload %s want %s", payload.Load(), want)
			}
			w = payRequest(r, "POST", "/orders/123/pay?account=pay", validPayment)
			if w.Code != 409 || calls.Load() != 1 {
				t.Fatal("duplicate payment escaped journal")
			}
		})
	}
}

func TestOrderPaymentRejectsStaleOrUnconfirmedInput(t *testing.T) {
	for _, tc := range []struct{ name, status, expiry, body string }{
		{"unconfirmed", "notPaid", "", strings.Replace(validPayment, `"confirm":true`, `"confirm":false`, 1)},
		{"paid", "delivered", "", validPayment},
		{"cancelled", "cancelled", "", validPayment},
		{"expired", "notPaid", "2000-01-01T00:00:00Z", validPayment},
		{"changed amount", "notPaid", "", strings.Replace(validPayment, "39.98", "40", 1)},
		{"changed currency", "notPaid", "", strings.Replace(validPayment, "EUR", "USD", 1)},
		{"invalid method", "notPaid", "", strings.Replace(validPayment, `"paymentMethodId":7`, `"paymentMethodId":8`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, r, calls, _ := paymentFixture(t, "EU", tc.status, tc.expiry, 200)
			w := payRequest(r, "POST", "/orders/123/pay?account=pay", tc.body)
			if w.Code < 400 || calls.Load() != 0 {
				t.Fatalf("unsafe request %d pay=%d", w.Code, calls.Load())
			}
		})
	}
}

func TestOrderPaymentAmbiguousResultsNeverRetry(t *testing.T) {
	for _, code := range []int{408, 409, 429, 503} {
		t.Run(jsonNumber(int64(code)), func(t *testing.T) {
			_, r, calls, _ := paymentFixture(t, "EU", "notPaid", "", code)
			for i := 0; i < 2; i++ {
				w := payRequest(r, "POST", "/orders/123/pay?account=pay", validPayment)
				if w.Code != 409 {
					t.Fatalf("status %d: %s", w.Code, w.Body)
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("ambiguous response retried %d times", calls.Load())
			}
		})
	}
}

func TestOrderPaymentDefiniteRejectionAllowsFreshConfirmation(t *testing.T) {
	_, r, calls, _ := paymentFixture(t, "EU", "notPaid", "", 400)
	for i := 0; i < 2; i++ {
		w := payRequest(r, "POST", "/orders/123/pay?account=pay", validPayment)
		if w.Code != 502 {
			t.Fatalf("rejected response %d %s", w.Code, w.Body)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("definite rejection retained ambiguous payment claim")
	}
}

func TestMonitorUserStopPersistsAcrossReload(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	r := gin.New()
	r.POST("/stop", StopMonitor(st, mon))
	// Already stopped also needs to save the user's explicit stop intent.
	w := payRequest(r, "POST", "/stop", `{}`)
	if w.Code != 200 || mon.AutoStartAllowed() {
		t.Fatalf("stop not persisted: %d %s", w.Code, w.Body)
	}
	item := types.QueueItem{ID: "late", PlanCode: "plan", Status: "running"}
	if err := st.EnqueueAutomaticItems([]types.QueueItem{item}, true); err == nil {
		t.Fatal("late automatic request bypassed stopped monitor")
	}
}

func TestQueuePauseAndDeletePreventAutomaticRecreation(t *testing.T) {
	for _, action := range []string{"pause", "delete", "clear", "telegram"} {
		t.Run(action, func(t *testing.T) {
			st, _ := newWatchTestMonitor(t)
			item := types.QueueItem{ID: "task", AccountID: "eu", PlanCode: "plan", Datacenter: "gra", Status: "running"}
			if err := st.EnqueueItems([]types.QueueItem{item}, false); err != nil {
				t.Fatal(err)
			}
			r := gin.New()
			r.PUT("/queue/:id/status", UpdateQueueStatus(st))
			r.DELETE("/queue/clear", ClearQueue(st))
			r.DELETE("/queue/:id", RemoveQueueItem(st))
			var w *httptest.ResponseRecorder
			switch action {
			case "pause":
				w = payRequest(r, "PUT", "/queue/task/status", `{"status":"paused"}`)
			case "delete":
				w = payRequest(r, "DELETE", "/queue/task", "")
			case "clear":
				w = payRequest(r, "DELETE", "/queue/clear", "")
			case "telegram":
				if out := cancelText(st, []string{"task"}); !strings.Contains(out, "取消") {
					t.Fatal(out)
				}
			}
			if w != nil && w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			item.ID = "monitor-new"
			if err := st.EnqueueAutomaticItems([]types.QueueItem{item}, true); err == nil {
				t.Fatal("monitor recreated stopped task")
			}
		})
	}
}

// 默认账户在慢请求期间改变时,订单记录仍绑定最初查询的账户。
func TestOrderListPinsResolvedAccount(t *testing.T) {
	st, r, calls, _ := paymentFixture(t, "EU", "notPaid", "", 200)
	st.Accounts = append(st.Accounts, types.OVHAccount{ID: "other", Endpoint: "ovh-ca", AppKey: "test", AppSecret: "test", ConsumerKey: "test"})
	base, _ := st.OVH.ClientFor("pay")
	original := base.Client.Transport
	base.Client.Transport = queuePreorderTransport(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/me/order") {
			st.AccountsMu.Lock()
			st.Accounts[0].IsDefault = false
			st.Accounts[1].IsDefault = true
			st.AccountsMu.Unlock()
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`[123]`)), Request: req}, nil
		}
		return original.RoundTrip(req)
	})
	r.GET("/orders", GetAccountOrders(st))
	w := payRequest(r, "GET", "/orders", "")
	var orders []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &orders); err != nil || w.Code != 200 || len(orders) != 1 {
		t.Fatalf("orders: %d %s %v", w.Code, w.Body, err)
	}
	if orders[0]["accountId"] != "pay" || orders[0]["orderStatus"] != "notPaid" || calls.Load() != 0 {
		t.Fatalf("wrong account/status: %v", orders)
	}
}
