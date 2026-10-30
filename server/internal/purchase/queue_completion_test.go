package purchase

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/types"
)

// queueCompletionState 只使用临时数据库和虚构任务,不发送任何 OVH 请求。
func queueCompletionState(t *testing.T) *app.State {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	lg := logger.New(filepath.Join(dir, "test.log"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	return app.NewState(storage.Paths{DataDir: dir}, config.New(database), lg, database)
}

// 成功任务必须在列表和数据库中保留,后续循环与重启不会重复下单。
func TestQueueRetainsOrderedTasksWithoutRepurchasing(t *testing.T) {
	st := queueCompletionState(t)
	st.Queue = []types.QueueItem{{ID: "ordered", PlanCode: "plan", Status: "running", RetryInterval: 1}, {ID: "paused", PlanCode: "other", Status: "paused"}}
	if err := st.SaveQueue(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		processQueueLoop(ctx, st, func(context.Context, *app.State, *types.QueueItem) Outcome {
			calls.Add(1)
			return Outcome{Success: true}
		})
	}()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(3 * time.Second)
	for {
		st.QueueMu.Lock()
		complete := len(st.Queue) == 2 && st.Queue[0].Status == "completed"
		st.QueueMu.Unlock()
		if complete {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completed task was lost or never completed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(1100 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("repeated purchase: %d", calls.Load())
	}
	rows, err := st.DB.ListQueue()
	if err != nil || len(rows) != 2 {
		t.Fatalf("lost persistent queue: %+v %v", rows, err)
	}
	var status string
	for _, row := range rows {
		if row.ID == "ordered" {
			status = row.Status
		}
	}
	if status != "completed" {
		t.Fatalf("completion not saved: %+v", rows)
	}
}
