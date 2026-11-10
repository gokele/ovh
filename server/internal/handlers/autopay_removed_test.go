package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/types"
)

// 自动付款已删除:旧页面带 autoPay:false 照常下单,autoPay:true 明确拒绝且不入队。
func TestAutoPayRequestsAreRefused(t *testing.T) {
	state, mon := newWatchTestMonitor(t)
	state.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu", Zone: "BATCH-TEST"}}
	// 订阅接口先探测通知通道;用本机假 webhook 让请求走到自动付款检查。
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer hook.Close()
	cfg := state.Config.Get()
	cfg.NotifyWebhookURL = hook.URL
	state.Config.Set(cfg)
	router := gin.New()
	router.POST("/queue", AddQueueItem(state))
	router.POST("/monitor", AddSubscription(state, mon))
	send := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, r)
		return w
	}
	w := send("/queue", `{"account_id":"eu","planCode":"plan","datacenter":"gra","autoPay":true}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "AUTO_PAY_REMOVED") || len(state.Queue) != 0 {
		t.Fatalf("autoPay:true must be refused without enqueueing: %d %s", w.Code, w.Body.String())
	}
	w = send("/monitor", `{"planCode":"plan","autoOrder":true,"autoOrderAccountId":"eu","autoPay":true}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "AUTO_PAY_REMOVED") {
		t.Fatalf("subscription autoPay:true must be refused: %d %s", w.Code, w.Body.String())
	}
}
