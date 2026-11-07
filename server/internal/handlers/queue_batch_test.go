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

func TestQueueBatchCreatesSharedQuotaAtomicallyAndPreservesLegacyQuantity(t *testing.T) {
	previous := http.DefaultTransport
	http.DefaultTransport = queuePreorderTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"plans":[{"planCode":"plan"}]}`)), Request: r}, nil
	})
	defer func() { http.DefaultTransport = previous }()
	state, _ := newWatchTestMonitor(t)
	state.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu", Zone: "BATCH-TEST"}}
	router := gin.New()
	router.POST("/queue", AddQueueItem(state))
	send := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/queue", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, r)
		return w
	}
	for _, body := range []string{
		`{"account_id":"eu","planCode":"plan","datacenters":["gra","fra"],"quantity":0}`,
		`{"account_id":"eu","planCode":"plan","datacenters":["gra","fra"],"quantity":1.5}`,
		`{"account_id":"eu","planCode":"plan","datacenters":["gra","fra"],"quantity":21}`,
		`{"account_id":"eu","planCode":"plan","datacenters":["mum","ynm"],"quantity":1}`,
		`{"account_id":"eu","planCode":"plan","datacenters":["gra"],"datacenter":"fra"}`,
		`{"account_id":"eu","planCode":"plan","datacenters":["gra"],"purchaseMode":"typo"}`,
	} {
		if w := send(body); w.Code != 400 || len(state.Queue) != 0 {
			t.Fatal("invalid batch partially enqueued", w.Code, w.Body.String(), len(state.Queue))
		}
	}
	w := send(`{"account_id":"eu","planCode":"plan","datacenters":["gra","fra"],"quantity":2,"purchaseMode":"total","duration":"P12M","coupon":" SAVE "}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	rows, err := state.DB.ListQueue()
	if err != nil || len(rows) != 4 {
		t.Fatal(rows, err)
	}
	group := rows[0].OrderGroup
	if group == "" {
		t.Fatal("shared quota missing")
	}
	for _, row := range rows {
		if row.OrderGroup != group || row.GroupLimit != 2 || row.Duration != "P12M" || row.Coupon != "SAVE" {
			t.Fatal("batch intent differs", row)
		}
	}
	w = send(`{"account_id":"eu","planCode":"plan","datacenter":"gra"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	rows, err = state.DB.ListQueue()
	if err != nil || len(rows) != 5 || rows[4].OrderGroup != "" || rows[4].GroupLimit != 0 {
		t.Fatal("legacy single task changed", rows, err)
	}
}
