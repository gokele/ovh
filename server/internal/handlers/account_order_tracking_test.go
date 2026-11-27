package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/types"
)

func trackingRouter(t *testing.T, followUpCode int) *gin.Engine {
	t.Helper()
	st, _ := newWatchTestMonitor(t)
	st.Accounts = []types.OVHAccount{{ID: "acc", Endpoint: "ovh-eu", Zone: "IE", AppKey: "test", AppSecret: "test", ConsumerKey: "test", IsDefault: true}}
	client, err := st.OVH.ClientFor("acc")
	if err != nil {
		t.Fatal(err)
	}
	client.Client = &http.Client{Transport: queuePreorderTransport(func(r *http.Request) (*http.Response, error) {
		body, status := "", 200
		if r.Method != http.MethodGet {
			t.Errorf("unexpected write %s %s", r.Method, r.URL.Path)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/time"):
			body = jsonNumber(time.Now().Unix())
		case strings.HasSuffix(r.URL.Path, "/order/77/status"):
			body = `"delivering"`
		case strings.HasSuffix(r.URL.Path, "/order/77/followUp"):
			status = followUpCode
			body = `{"message":"beta unavailable"}`
			if followUpCode == 200 {
				body = `[{"step":"VALIDATING","status":"DONE","history":[{"date":"2026-11-25T10:00:00+01:00","label":"PAYMENT_RECEIVED","description":"Payment received"}]},
				         {"step":"VALIDATED","status":"DONE","history":[]},
				         {"step":"DELIVERING","status":"DOING","history":[{"date":"2026-11-25T10:05:00+01:00","label":"DELIVERY","description":"Delivery in progress"}]},
				         {"step":"AVAILABLE","status":"TODO","history":[]}]`
			}
		case strings.HasSuffix(r.URL.Path, "/order/77/details/5"):
			body = `{"description":"KS-LE-3 | GRA","detailType":"DURATION","quantity":"1","totalPrice":{"text":"€15.99"}}`
		case strings.HasSuffix(r.URL.Path, "/order/77/details"):
			body = `[5]`
		default:
			t.Errorf("unexpected OVH request: %s %s", r.Method, r.URL.Path)
			status, body = 404, `{}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	router := gin.New()
	router.GET("/orders/:order_id/tracking", GetOrderTracking(st))
	return router
}

// 读取状态、交付步骤(followUp)与明细;只发 GET。
func TestOrderTracking(t *testing.T) {
	w := payRequest(trackingRouter(t, 200), "GET", "/orders/77/tracking?account=acc", "")
	var got struct {
		Status string         `json:"status"`
		Steps  []trackingStep `json:"steps"`
		Detail []struct {
			Description string `json:"description"`
			Total       string `json:"total"`
		} `json:"details"`
		FollowUpError string `json:"followUpError"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if got.Status != "delivering" || len(got.Steps) != 4 || got.Steps[2].Status != "DOING" || got.Steps[0].History[0].Label != "PAYMENT_RECEIVED" {
		t.Fatalf("steps %+v", got)
	}
	if len(got.Detail) != 1 || got.Detail[0].Description != "KS-LE-3 | GRA" || got.Detail[0].Total != "€15.99" || got.FollowUpError != "" {
		t.Fatalf("details %+v", got)
	}
}

// followUp 是 BETA:读不到时仍返回状态和明细,并说明原因;非法订单号不访问 OVH。
func TestOrderTrackingFollowUpFailureAndBadID(t *testing.T) {
	router := trackingRouter(t, 500)
	w := payRequest(router, "GET", "/orders/77/tracking?account=acc", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"followUpError"`) || !strings.Contains(w.Body.String(), `"KS-LE-3 | GRA"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w := payRequest(router, "GET", "/orders/abc/tracking?account=acc", ""); w.Code != 400 {
		t.Fatalf("bad id %d", w.Code)
	}
}
