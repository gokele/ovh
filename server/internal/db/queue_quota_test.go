package db

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ovh-buy/server/internal/types"
)

func TestTotalOrderQuotaPersistsAndReservesAtomically(t *testing.T) {
	dir := t.TempDir()
	database, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	items := []types.QueueItem{}
	for i := 0; i < 12; i++ {
		items = append(items, types.QueueItem{ID: fmt.Sprint(i), PlanCode: "plan", Status: "running", OrderGroup: "group", GroupLimit: 2, StopReason: "groupComplete"})
	}
	if err := database.ReplaceQueue(items); err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, item := range items {
		wg.Add(1)
		go func(item types.QueueItem) {
			defer wg.Done()
			won, err := database.ClaimQueueOrder(item)
			if err != nil {
				t.Error(err)
			}
			if won {
				wins.Add(1)
			}
		}(item)
	}
	wg.Wait()
	if wins.Load() != 2 {
		t.Fatal("quota was not atomic", wins.Load())
	}
	var claimed []string
	if err := database.Select(&claimed, `SELECT task_id FROM queue_order_claims ORDER BY task_id`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	rows, err := database.ListQueue()
	if err != nil || len(rows) != 12 || rows[0].OrderGroup != "group" || rows[0].GroupLimit != 2 || rows[0].StopReason != "groupComplete" {
		t.Fatal("group configuration lost", rows, err)
	}
	byID := map[string]types.QueueItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	ordered, uncertain := byID[claimed[0]], byID[claimed[1]]
	if err := database.MarkQueueOrder(ordered, "ordered"); err != nil {
		t.Fatal(err)
	}
	if err := database.MarkQueueOrder(uncertain, "uncertain"); err != nil {
		t.Fatal(err)
	}
	quota, err := database.QueueQuota(items[0])
	if err != nil || quota.Reserved != 2 || quota.Ordered != 1 || quota.Uncertain != 1 {
		t.Fatal(quota, err)
	}
	if err := database.ResumeQueueItem(uncertain, "test:block", true); err != nil {
		t.Fatal(err)
	}
	quota, err = database.QueueQuota(items[0])
	if err != nil || quota.Reserved != 1 || quota.Uncertain != 0 || quota.Ordered != 1 {
		t.Fatal("explicit resume did not release only the uncertain slot", quota, err)
	}
	for _, item := range items {
		if item.ID != ordered.ID && item.ID != uncertain.ID {
			won, err := database.ClaimQueueOrder(item)
			if err != nil || !won {
				t.Fatal("released slot not reusable", won, err)
			}
			break
		}
	}
}

func TestQuotaClaimRollbackAndImmutableLimit(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	a := types.QueueItem{ID: "a", PlanCode: "plan", Status: "running", OrderGroup: "one", GroupLimit: 1}
	b := a
	b.ID = "b"
	if err := database.ReplaceQueue([]types.QueueItem{a, b}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ClaimQueueCheckout(a.ID); err != nil {
		t.Fatal(err)
	}
	if won, err := database.ClaimQueueOrder(a); err != nil || won {
		t.Fatal("task guard ignored", won, err)
	}
	if quota, err := database.QueueQuota(a); err != nil || quota.Reserved != 0 {
		t.Fatal("failed task claim leaked group quota", quota, err)
	}
	if won, err := database.ClaimQueueOrder(b); err != nil || !won {
		t.Fatal(won, err)
	}
	a.GroupLimit = 2
	if won, err := database.ClaimQueueOrder(a); err == nil || won {
		t.Fatal("existing limit changed", won, err)
	}
}

func TestMissingPendingTaskMakesGroupUncertain(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	item := types.QueueItem{ID: "deleted-during-checkout", OrderGroup: "group", GroupLimit: 1, Status: "running", PlanCode: "plan"}
	if err := database.ReplaceQueue([]types.QueueItem{item}); err != nil {
		t.Fatal(err)
	}
	if won, err := database.ClaimQueueOrder(item); err != nil || !won {
		t.Fatal(won, err)
	}
	if err := database.ReplaceQueue(nil); err != nil {
		t.Fatal(err)
	}
	if quota, err := database.QueueQuota(item); err != nil || quota.Uncertain != 1 || quota.Reserved != 1 {
		t.Fatal("deleted checkout released a potentially used slot", quota, err)
	}
}
