package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

type testPrewarmer struct {
	owner     string
	items     []types.QueueItem
	lease     time.Duration
	cancelled string
	calls     int
}

func (p *testPrewarmer) Prepare(owner string, items []types.QueueItem, lease time.Duration, _ func() bool) error {
	p.owner, p.items, p.lease = owner, items, lease
	p.calls++
	return nil
}
func (*testPrewarmer) Ready(types.QueueItem) bool { return false }
func (p *testPrewarmer) Cancel(owner string)      { p.cancelled = owner }
func (*testPrewarmer) CancelPrefix(string)        {}
func (p *testPrewarmer) Status(string) []app.PrewarmStatus {
	return []app.PrewarmStatus{{ID: p.owner, State: "preparing"}}
}
func (*testPrewarmer) Close() {}

func TestPrepareRouteValidatesAndNeverEnqueues(t *testing.T) {
	state, _ := newWatchTestMonitor(t)
	pool := &testPrewarmer{}
	state.OrderPrewarm = pool
	router := gin.New()
	router.POST("/purchase/prewarm", PrepareOrderCart(state))
	router.DELETE("/purchase/prewarm/:id", CancelOrderPrewarm(state))
	request := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/purchase/prewarm", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	valid := `{"id":"unique-request","account_id":"eu","planCode":"plan","datacenters":["mum","bhs"],"quantity":2,"options":["ram-32g"],"coupon":"SAVE","autoPay":false,"standby":true,"duration":"P1Y"}`
	if w := request(valid); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if pool.owner != "web:unique-request" || pool.lease != 45*time.Minute || len(pool.items) != 4 || pool.items[0].Datacenter != "ynm" || pool.items[0].Duration != "P12M" || pool.items[0].Coupon != "SAVE" || len(state.Queue) != 0 || len(state.History) != 0 {
		t.Fatalf("wrong intent: %+v", pool)
	}
	for _, body := range []string{
		strings.Replace(valid, `"P1Y"`, `"P-1M"`, 1),
		strings.Replace(valid, `"quantity":2`, `"quantity":21`, 1),
		strings.Replace(valid, `"quantity":2`, `"quantity":1.5`, 1),
		strings.Replace(valid, `"mum","bhs"`, `"mum","ynm"`, 1),
		strings.Replace(valid, `"unique-request"`, `"bad:id"`, 1),
		strings.Replace(valid, `"account_id":"eu"`, `"account_id":""`, 1),
	} {
		if w := request(body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if pool.calls != 1 {
		t.Fatal("invalid intent reached preparation", pool.calls)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/purchase/prewarm/unique-request", nil))
	if w.Code != 200 || pool.cancelled != "web:unique-request" {
		t.Fatal(w.Code, pool.cancelled)
	}
}
