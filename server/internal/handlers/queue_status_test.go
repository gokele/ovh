package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/types"
)

func TestQueuePauseCancelsAttemptAndCanResume(t *testing.T) {
	st, _ := newWatchTestMonitor(t)
	st.Queue = []types.QueueItem{{ID: "task", Status: "running"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st.RegisterTaskCancel("task", cancel)
	r := gin.New()
	r.PUT("/queue/:id/status", UpdateQueueStatus(st))
	for _, status := range []string{"paused", "running"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/queue/task/status", strings.NewReader(`{"status":"`+status+`"}`)))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", status, w.Code, w.Body.String())
		}
		if ctx.Err() != context.Canceled {
			t.Fatal("pausing must cancel the in-flight purchase")
		}
		if st.IsTaskDeleted("task") {
			t.Fatal("pausing must not mark a resumable task as deleted")
		}
		rows, err := st.DB.ListQueue()
		if err != nil || len(rows) != 1 || rows[0].Status != status {
			t.Fatalf("status not persisted: %+v %v", rows, err)
		}
	}
	resumed, release := context.WithCancel(context.Background())
	defer release()
	st.RegisterTaskCancel("task", release)
	defer st.UnregisterTaskCancel("task")
	if resumed.Err() != nil {
		t.Fatal("a resumed task must be allowed to start a new attempt")
	}
}

func TestQueueStatusRejectsMalformedOrMissingTask(t *testing.T) {
	st, _ := newWatchTestMonitor(t)
	st.Queue = []types.QueueItem{{ID: "task", Status: "running"}}
	r := gin.New()
	r.PUT("/queue/:id/status", UpdateQueueStatus(st))
	for _, tc := range []struct {
		id, body string
		want     int
	}{
		{"task", `{}`, http.StatusBadRequest},
		{"task", `{"status":17}`, http.StatusBadRequest},
		{"task", `{"status":"anything"}`, http.StatusBadRequest},
		{"task", `{"status":"paused",`, http.StatusBadRequest},
		{"missing", `{"status":"paused"}`, http.StatusNotFound},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/queue/"+tc.id+"/status", strings.NewReader(tc.body)))
		if w.Code != tc.want || st.Queue[0].Status != "running" {
			t.Errorf("%s: status=%d, queue=%+v", tc.body, w.Code, st.Queue)
		}
	}
}

func TestAddQueueRejectsPartiallyDecodedParameters(t *testing.T) {
	st, _ := newWatchTestMonitor(t)
	st.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu", Zone: "IE"}}
	r := gin.New()
	r.POST("/queue", AddQueueItem(st))
	r.POST("/quick-order", QuickOrder(st))
	for _, path := range []string{"/queue", "/quick-order"} {
		for _, extra := range []string{`"options":17`, `"autoPay":"true"`} {
			body := `{"account_id":"eu","planCode":"27skle01-v1","datacenter":"gra",` + extra + `}`
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
			if w.Code != http.StatusBadRequest || len(st.Queue) != 0 {
				t.Fatalf("partially decoded request reached the queue: %s, status=%d", body, w.Code)
			}
		}
	}
}
