package app

import (
	"fmt"
	"testing"

	"github.com/ovh-buy/server/internal/types"
)

// 成功历史修复崩溃前未保存的队列状态,不会复活已经删除的任务。
func TestSuccessfulHistoryRestoresCompletion(t *testing.T) {
	st := enqueueTestState(t)
	st.Queue = []types.QueueItem{{ID: "ordered", Status: "running"}, {ID: "pending", Status: "running"}}
	st.History = []types.PurchaseHistoryEntry{{ID: "h1", TaskID: "ordered", Status: "success", OrderID: "123"}, {ID: "h2", TaskID: "removed", Status: "success", OrderID: "456"}, {ID: "h3", TaskID: "pending", Status: "failed"}}
	if err := st.SaveQueue(); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveHistory(); err != nil {
		t.Fatal(err)
	}
	st.LoadAll()
	if len(st.Queue) != 2 {
		t.Fatalf("task was recreated or removed: %+v", st.Queue)
	}
	for _, row := range st.Queue {
		if row.ID == "ordered" && row.Status != "completed" {
			t.Fatal("successful task revived")
		}
		if row.ID == "pending" && row.Status != "running" {
			t.Fatal("unrelated task was stopped")
		}
	}
	rows, err := st.DB.ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "ordered" && row.Status != "completed" {
			t.Fatal("repaired completion not persisted")
		}
	}
}

// 无法读取历史时不能把未知状态当作尚未下单。
func TestCompletionRepairRefusesUnreadableHistory(t *testing.T) {
	st := enqueueTestState(t)
	st.Queue = []types.QueueItem{{ID: "task", Status: "running"}}
	st.MarkLoadFailed("history", fmt.Errorf("fixture unreadable history"))
	if st.RestoreCompletedQueue() == nil {
		t.Fatal("missing history ignored")
	}
	if st.Queue[0].Status != "running" {
		t.Fatal("unknown task status fabricated")
	}
}
