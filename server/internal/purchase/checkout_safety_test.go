package purchase

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/types"
)

func TestCheckoutRequiresValidOrderIDAndSurvivesLostQueueStatus(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       int
	}{{"missing", `{}`, 200}, {"zero", `{"orderId":0}`, 200}, {"negative", `{"orderId":-1}`, 200}, {"fractional", `{"orderId":1.5}`, 200}, {"gateway", `{}`, 504}} {
		t.Run(tc.name, func(t *testing.T) {
			state, _, checkouts, stock := hotFixture(t)
			stock.Store(true)
			base, err := state.OVH.ClientFor("hot-eu")
			if err != nil {
				t.Fatal(err)
			}
			original := base.Client.Transport
			base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
				response, err := original.RoundTrip(r)
				if err == nil && strings.HasSuffix(r.URL.Path, "/checkout") {
					response.Body.Close()
					response.StatusCode, response.Body = tc.code, io.NopCloser(strings.NewReader(tc.body))
				}
				return response, err
			})
			item := types.QueueItem{ID: "checkout", AccountID: "hot-eu", PlanCode: "plan", Datacenter: "gra", Status: "running"}
			state.Queue = []types.QueueItem{item}
			out := runQueueAttempt(context.Background(), state, item, PurchaseServer)
			if out.Success || !out.PauseTask || state.Queue[0].Status != "paused" || checkouts.Load() != 1 {
				t.Fatalf("unsafe checkout outcome: %+v", out)
			}
			history, err := state.DB.ListHistory()
			if err != nil || len(history) != 1 || history[0].Status != "uncertain" || history[0].OrderID != "" {
				t.Fatal(history, err)
			}
			// Simulate a crash before the paused queue state or history was saved.
			state.History, state.Queue[0].Status = nil, "running"
			out = runQueueAttempt(context.Background(), state, item, PurchaseServer)
			if !out.PauseTask || checkouts.Load() != 1 {
				t.Fatal("durable checkout claim allowed another order", out)
			}
			if found, err := state.DB.GetKV(db.QueueCheckoutKey(item.ID), &map[string]interface{}{}); err != nil || !found {
				t.Fatal("checkout claim was lost", err)
			}
		})
	}
}

func TestLateFailureCannotEraseConfirmedOrder(t *testing.T) {
	state := queueCompletionState(t)
	state.History = []types.PurchaseHistoryEntry{{ID: "history", TaskID: "ordered", Status: "success", OrderID: "123"}}
	for _, status := range []string{"failed", "uncertain"} {
		recordFailureStatus(state, &types.QueueItem{ID: "ordered"}, "late result", status)
		if state.History[0].Status != "success" || state.History[0].OrderID != "123" {
			t.Fatal("late failure erased a confirmed order")
		}
	}
}
