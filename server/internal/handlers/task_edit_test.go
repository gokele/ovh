package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/types"
)

func TestQueueEditPreservesIdentityAndRequiresPause(t *testing.T) {
	st, _ := newWatchTestMonitor(t)
	st.Queue = []types.QueueItem{{ID: "task", AccountID: "eu", PlanCode: "24sk202", Status: "running", Datacenter: "gra", Preorder: true, RetryCount: 9, FailureCount: 2}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st.RegisterTaskCancel("task", cancel)
	r := gin.New()
	r.PUT("/queue/:id", UpdateQueueItem(st))
	r.PUT("/queue/:id/status", UpdateQueueStatus(st))
	request := func(path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, path, strings.NewReader(body)))
		return w
	}
	const patch = `{"datacenter":" rbx ","options":["ram-64g","custom-addon"],"retryInterval":17,"autoPay":true,"coupon":" Save10 "}`
	if w := request("/queue/task", patch); w.Code != http.StatusConflict {
		t.Fatalf("running edit must be rejected: %d %s", w.Code, w.Body.String())
	}
	if w := request("/queue/task/status", `{"status":"paused"}`); w.Code != 200 || ctx.Err() != context.Canceled {
		t.Fatalf("pause did not cancel attempt: %d %v", w.Code, ctx.Err())
	}
	if w := request("/queue/task", patch); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	got, err := st.DB.ListQueue()
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	q := got[0]
	if q.ID != "task" || q.AccountID != "eu" || q.PlanCode != "24sk202" || !q.Preorder || q.Status != "paused" || q.RetryCount != 9 || q.FailureCount != 0 || q.Datacenter != "rbx" || q.Coupon != "Save10" || len(q.Options) != 2 || q.Options[1] != "custom-addon" || q.RetryInterval != 17 || !q.AutoPay {
		t.Fatalf("bad edited task: %+v", q)
	}
	for _, body := range []string{`{"datacenter":"gra","retryInterval":0}`, `{"datacenter":"gra","retryInterval":17,"autoPay":"yes"}`, `{"datacenter":"gra","retryInterval":17,`} {
		if w := request("/queue/task", body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request("/queue/missing", patch); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	st.DB.Close()
	if w := request("/queue/task", `{"datacenter":"gra","retryInterval":17}`); w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	if st.Queue[0].Datacenter != "rbx" || st.Queue[0].Coupon != "Save10" {
		t.Fatal("failed save published edited configuration")
	}
}

func TestMonitorEditAndPauseSelectExactConfiguration(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	for _, options := range [][]string{{"ram-32g"}, {"ram-64g"}, nil} {
		mon.AddSubscription("plan", []string{"gra"}, true, false, "", map[string]string{"gra": "unavailable"}, []monitor.HistoryEntry{{Status: "unavailable"}}, false, 0, "", false, options)
	}
	r := gin.New()
	r.PUT("/monitor/:planCode", UpdateSubscription(st, mon))
	r.POST("/monitor/:planCode/pause", PauseSubscription(st, mon))
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	if w := request("POST", "/monitor/plan/pause", `{"optionsKey":"ram-64g","paused":true}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("PUT", "/monitor/plan?optionsKey=ram-64g", `{"options":["ram-128g"],"datacenters":["rbx"],"notifyAvailable":false}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	sub := mon.FindSubscription("plan", "ram-128g")
	if sub == nil || !sub.Paused || sub.NotifyAvailable || sub.Datacenters[0] != "rbx" || sub.LastStatus["gra"] != "unavailable" || len(sub.History) != 1 {
		t.Fatalf("edit lost state or selected sibling: %+v", sub)
	}
	if first := mon.FindSubscription("plan", "ram-32g"); first.Paused || !first.NotifyAvailable || first.Datacenters[0] != "gra" {
		t.Fatalf("sibling changed: %+v", first)
	}
	if w := request("PUT", "/monitor/plan?optionsKey=ram-128g", `{"options":["ram-32g"]}`); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Explicit empty selects the all-configurations sibling, even when it isn't first.
	if w := request("PUT", "/monitor/plan?optionsKey=", `{"datacenters":["sbg"]}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", "/monitor/plan/pause", `{"optionsKey":"","paused":true}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	reloaded := monitor.New(st)
	reloaded.LoadFromDB()
	if all := reloaded.FindSubscription("plan", ""); all == nil || !all.Paused || all.Datacenters[0] != "sbg" {
		t.Fatalf("pause or edit lost on restart: %+v", all)
	}
	if specific := reloaded.FindSubscription("plan", "ram-128g"); specific == nil || !specific.Paused {
		t.Fatal("specific pause lost on restart")
	}
	for _, body := range []string{`{}`, `{"paused":"yes"}`, `{"paused":true,`} {
		if w := request("POST", "/monitor/plan/pause", body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request("PUT", "/monitor/plan?optionsKey=missing", `{"quantity":3}`); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
}
