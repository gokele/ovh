package app

import (
	"github.com/ovh-buy/server/internal/types"
	"testing"
)

// TestAutomaticOrderStopPersistsAfterTaskDeletion 覆盖任务已被删掉且重启后仍会再补货的情况。
func TestAutomaticOrderStopPersistsAfterTaskDeletion(t *testing.T) {
	s := enqueueTestState(t)
	item := types.QueueItem{ID: "stopped", AccountID: "eu", PlanCode: "plan", Datacenter: "TOR", Status: "running"}
	if err := s.EnqueueItems([]types.QueueItem{item}, false); err != nil {
		t.Fatal(err)
	}
	s.QueueMu.Lock()
	if err := s.SetAutomaticOrderBlockedLocked(item, true); err != nil {
		t.Fatal(err)
	}
	s.Queue = nil
	s.QueueMu.Unlock()
	// 丢弃内存停止记录模拟新进程,仅依赖数据库。
	s.automaticOrderBlocks = nil
	item.ID = "from-monitor"
	item.Datacenter = "ca-east-tor-a"
	if err := s.EnqueueAutomaticItems([]types.QueueItem{item}, true); err == nil {
		t.Fatal("monitor recreated deleted task after restart")
	}
	if len(s.Queue) != 0 {
		t.Fatal("blocked task was published")
	}
	other := item
	other.ID = "another-dc"
	other.Datacenter = "gra"
	if err := s.EnqueueAutomaticItems([]types.QueueItem{other}, true); err != nil {
		t.Fatal("stop leaked to another DC", err)
	}
	// 用户明确新建同款任务后才解除持久停止记录。
	item.ID = "manual"
	if err := s.EnqueueItems([]types.QueueItem{item}, false); err != nil {
		t.Fatal(err)
	}
	item.ID = "automatic-after-manual"
	if err := s.EnqueueAutomaticItems([]types.QueueItem{item}, true); err != nil {
		t.Fatal("explicit manual restart did not unlock", err)
	}
}

func TestAutomaticOrderCannotBypassLegacyPausedTask(t *testing.T) {
	s := enqueueTestState(t)
	s.Queue = []types.QueueItem{{ID: "legacy", PlanCode: "plan", Datacenter: "gra", Status: "paused"}}
	if err := s.EnqueueAutomaticItems([]types.QueueItem{{ID: "new", PlanCode: "plan", Datacenter: "GRA", Status: "running"}}, true); err == nil {
		t.Fatal("paused task bypassed")
	}
	s.DB.Close()
	if err := s.EnqueueAutomaticItems([]types.QueueItem{{ID: "another", PlanCode: "other", Datacenter: "gra", Status: "running"}}, true); err == nil {
		t.Fatal("database failure allowed automatic order")
	}
}

func TestGlobalMonitorStopBlocksLateAutomaticRequest(t *testing.T) {
	s := enqueueTestState(t)
	if err := s.DB.SetKV("monitor-manually-stopped", true); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueAutomaticItems([]types.QueueItem{{ID: "late-request", PlanCode: "plan", Status: "running"}}, true); err == nil {
		t.Fatal("stopped monitor's late HTTP request enqueued")
	}
}
