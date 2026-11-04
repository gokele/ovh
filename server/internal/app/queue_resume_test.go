package app

import (
	"testing"

	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/types"
)

func TestResumeFailureCannotPublishRunnableTask(t *testing.T) {
	state := enqueueTestState(t)
	state.Queue = []types.QueueItem{{ID: "resume", Status: "paused"}}
	if err := state.SaveQueue(); err != nil {
		t.Fatal(err)
	}
	if claimed, err := state.DB.ClaimQueueCheckout("resume"); err != nil || !claimed {
		t.Fatal("claim failed", err)
	}
	state.QueueMu.Lock()
	err := state.SetAutomaticOrderBlockedLocked(state.Queue[0], true)
	state.QueueMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.DB.Exec(`CREATE TRIGGER reject_queue_resume BEFORE UPDATE ON queue BEGIN SELECT RAISE(FAIL, 'fixture write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := state.ResumeQueueItem("resume", "running"); err == nil {
		t.Fatal("failed resume reported success")
	}
	if state.Queue[0].Status != "paused" {
		t.Fatal("unsaved task became runnable")
	}
	rows, err := state.DB.ListQueue()
	if err != nil || len(rows) != 1 || rows[0].Status != "paused" {
		t.Fatal(rows, err)
	}
	var blocked bool
	if found, err := state.DB.GetKV(state.automaticOrderKey(state.Queue[0]), &blocked); err != nil || !found || !blocked {
		t.Fatal("failed resume unblocked automatic orders", err)
	}
	if found, err := state.DB.GetKV(db.QueueCheckoutKey("resume"), &map[string]interface{}{}); err != nil || !found {
		t.Fatal("failed resume cleared the checkout claim", err)
	}
}

func TestExplicitResumeClearsOnlyTheSelectedCheckoutClaim(t *testing.T) {
	state := enqueueTestState(t)
	state.Queue = []types.QueueItem{{ID: "resume", Status: "failed", FailureCount: 5, LastCheckTime: 12345}}
	if err := state.SaveQueue(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"resume", "other"} {
		if won, err := state.DB.ClaimQueueCheckout(id); err != nil || !won {
			t.Fatal("claim failed", err)
		}
	}
	if err := state.ResumeQueueItem("resume", "running"); err != nil {
		t.Fatal(err)
	}
	if rows, err := state.DB.ListQueue(); err != nil || len(rows) != 1 || rows[0].FailureCount != 0 || rows[0].LastCheckTime != 0 {
		t.Fatal("resumed task retained exhausted retries or an old timer", rows, err)
	}
	for id, want := range map[string]bool{"resume": false, "other": true} {
		if found, err := state.DB.GetKV(db.QueueCheckoutKey(id), &map[string]interface{}{}); err != nil || found != want {
			t.Fatal(id, found, err)
		}
	}
}
