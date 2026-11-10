package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/types"
)

// 多账户一起抢:任务按账户扇出,但数量上限共享,台数不会按账户翻倍;预售触发方式随任务保存。
func TestQueueMultiAccountSharesQuantityAndKeepsPreorderTrigger(t *testing.T) {
	previous := http.DefaultTransport
	http.DefaultTransport = queuePreorderTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"plans":[{"planCode":"plan"}]}`)), Request: r}, nil
	})
	defer func() { http.DefaultTransport = previous }()
	state, _ := newWatchTestMonitor(t)
	state.Accounts = []types.OVHAccount{{ID: "a1", Endpoint: "ovh-eu", Zone: "BATCH-TEST"}, {ID: "a2", Endpoint: "ovh-eu", Zone: "BATCH-TEST"}}
	router := gin.New()
	router.POST("/queue", AddQueueItem(state))
	send := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/queue", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, r)
		return w
	}
	if w := send(`{"account_ids":["a1","missing"],"planCode":"plan","datacenters":["gra"]}`); w.Code != 400 || len(state.Queue) != 0 {
		t.Fatal("unknown account must reject the whole batch", w.Code, w.Body.String())
	}
	w := send(`{"account_ids":["a1","a2","a1"],"planCode":"plan","datacenters":["gra","fra"],"quantity":1,"purchaseMode":"perDatacenter","preorder":true,"preorderWaitStock":true}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	rows, err := state.DB.ListQueue()
	if err != nil || len(rows) != 4 {
		t.Fatal("want 2 accounts x 2 datacenters", len(rows), err)
	}
	groups := map[string]map[string]bool{}
	for _, row := range rows {
		if row.OrderGroup == "" || row.GroupLimit != 1 || !row.Preorder || !row.PreorderWaitStock {
			t.Fatalf("task lost shared limit or trigger: %+v", row)
		}
		if groups[row.OrderGroup] == nil {
			groups[row.OrderGroup] = map[string]bool{}
		}
		groups[row.OrderGroup][row.AccountID+"@"+row.Datacenter] = true
	}
	if len(groups) != 2 {
		t.Fatal("per-datacenter mode should share one limit per datacenter across accounts", groups)
	}
	for _, members := range groups {
		if len(members) != 2 {
			t.Fatal("each datacenter group should contain both accounts", members)
		}
	}
	// 普通任务不能带上预售触发标记。
	w = send(`{"account_id":"a1","planCode":"plan","datacenter":"gra","preorderWaitStock":true}`)
	rows, _ = state.DB.ListQueue()
	if w.Code != 200 || rows[len(rows)-1].PreorderWaitStock {
		t.Fatal("wait-stock flag leaked to non-preorder task")
	}
}
