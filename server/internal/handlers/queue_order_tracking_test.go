package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

func TestQueueTrackingOrderBelongsToExactAccountAndTask(t *testing.T) {
	st := &app.State{Queue: []types.QueueItem{
		{ID: "shared", AccountID: "a", Status: "completed"},
		{ID: "shared", AccountID: "b", Status: "completed"},
		{ID: "many", AccountID: "a", Status: "completed"},
		{ID: "invalid", AccountID: "a", Status: "completed"},
		{ID: "orphan", Status: "completed"},
		{ID: "active", AccountID: "a", Status: "running"},
	}}
	add := func(task, account, status, order string) {
		st.History = append(st.History, types.PurchaseHistoryEntry{TaskID: task, AccountID: account, Status: status, OrderID: order})
	}
	add("shared", "a", "success", "101")
	add("shared", "b", "success", "202")
	add("shared", "a", "failed", "303")
	add("shared", "b", "success", "202") // Same order repeated, not a second order.
	add("many", "a", "success", "401")
	add("many", "a", "success", "402")
	add("many", "a", "success", "401") // A third row cannot make the match unique again.
	add("invalid", "a", "success", "001")
	add("invalid", "a", "success", "501")
	add("orphan", "b", "success", "601")
	add("active", "a", "success", "701")

	r := gin.New()
	r.GET("/queue", GetQueue(st))
	w := payRequest(r, http.MethodGet, "/queue", "")
	var rows []queueOrderView
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &rows) != nil || len(rows) != len(st.Queue) {
		t.Fatal(w.Code, w.Body)
	}
	for i, want := range []string{"101", "202", "", "", "", ""} {
		if got := rows[i].OrderID; got != want {
			t.Errorf("%s/%s: got %q, want %q", rows[i].AccountID, rows[i].ID, got, want)
		}
	}
}
