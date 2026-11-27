package app

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/types"
)

func bundleState(t *testing.T, items []types.QueueItem) *State {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	lg := logger.New(filepath.Join(dir, "t.log"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	s := NewState(storage.Paths{DataDir: dir}, config.New(database), lg, database)
	s.Queue = items
	if err := s.SaveQueue(); err != nil {
		t.Fatal(err)
	}
	return s
}

func task(id, dc, status string) types.QueueItem {
	return types.QueueItem{ID: id, AccountID: "acc", PlanCode: "plan", Datacenter: dc, Status: status, Options: []string{"old"}, Duration: "P1M", RetryInterval: 60, CreatedAt: "2026-11-27T00:00:0" + id[len(id)-1:] + "Z"}
}

func countBy(s *State) map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, it := range s.Queue {
		if out[it.Datacenter] == nil {
			out[it.Datacenter] = map[string]int{}
		}
		out[it.Datacenter][it.Status]++
	}
	return out
}

func TestBundleEditAppliesConfigToAllAndAdjustsPerDCQuantities(t *testing.T) {
	s := bundleState(t, []types.QueueItem{
		task("a1", "fra", "running"), task("a2", "fra", "paused"), task("a3", "fra", "running"),
		task("b1", "gra", "running"), task("c1", "rbx", "completed"),
	})
	d := "P12M"
	res, err := s.UpdateQueueBundle(BundleUpdate{
		IDs: []string{"a1", "a2", "a3", "b1", "c1"}, Options: []string{"new"}, Duration: &d, RetryInterval: 30, AutoPay: true,
		Quantities: map[string]int{"fra": 1, "gra": 3, "sbg": 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 4 || res.Removed != 2 {
		t.Fatalf("unexpected result %+v", res)
	}
	c := countBy(s)
	if c["fra"]["running"]+c["fra"]["paused"] != 1 || c["gra"]["running"] != 3 || c["sbg"]["running"] != 2 {
		t.Fatalf("wrong per-DC counts: %v", c)
	}
	// FRA 多出来的先删暂停的
	for _, it := range s.Queue {
		if it.ID == "a2" {
			t.Fatal("paused task should be removed first")
		}
		if it.Status == "completed" {
			if it.Options[0] != "old" || it.AutoPay {
				t.Fatal("completed task must never be modified")
			}
			continue
		}
		if it.Options[0] != "new" || it.Duration != "P12M" || it.RetryInterval != 30 || !it.AutoPay {
			t.Fatalf("config not applied in place to %s: %+v", it.ID, it)
		}
	}
	if !s.IsTaskDeleted("a2") {
		t.Fatal("removed task must be cancelled")
	}
	// 落库一致
	stored, err := s.DB.ListQueue()
	if err != nil || len(stored) != len(s.Queue) {
		t.Fatalf("queue not persisted: %v %d/%d", err, len(stored), len(s.Queue))
	}
}

func TestBundleEditSharedTotalMode(t *testing.T) {
	mk := func(id, dc, status string) types.QueueItem {
		it := task(id, dc, status)
		it.OrderGroup, it.GroupLimit = "g", 2
		return it
	}
	s := bundleState(t, []types.QueueItem{mk("a1", "fra", "running"), mk("a2", "fra", "running"), mk("b1", "gra", "completed"), mk("b2", "gra", "running")})
	// 合计 3 台(已下单 1)→ 每个机房还要 2 个任务;去掉 GRA,加上 RBX
	res, err := s.UpdateQueueBundle(BundleUpdate{IDs: []string{"a1", "a2", "b1", "b2"}, Options: []string{"old"}, AutoPay: false, Total: 3, Datacenters: []string{"fra", "rbx"}})
	if err != nil {
		t.Fatal(err)
	}
	c := countBy(s)
	if c["fra"]["running"] != 2 || c["rbx"]["running"] != 2 || c["gra"]["running"] != 0 || c["gra"]["completed"] != 1 {
		t.Fatalf("wrong counts %v (%+v)", c, res)
	}
	for _, it := range s.Queue {
		if it.Status != "completed" && (it.GroupLimit != 3 || it.OrderGroup != "g") {
			t.Fatalf("shared limit not applied: %+v", it)
		}
	}
}

func TestBundleEditRejectsMixedAndEmpty(t *testing.T) {
	other := task("x1", "fra", "running")
	other.PlanCode = "other"
	s := bundleState(t, []types.QueueItem{task("a1", "fra", "running"), other})
	if _, err := s.UpdateQueueBundle(BundleUpdate{IDs: []string{"a1", "x1"}}); err != ErrBundleMixed {
		t.Fatalf("mixed bundle accepted: %v", err)
	}
	if _, err := s.UpdateQueueBundle(BundleUpdate{IDs: []string{"a1"}, Quantities: map[string]int{"fra": 0}}); err != ErrBundleNoTarget {
		t.Fatalf("emptying the order accepted: %v", err)
	}
	if len(s.Queue) != 2 || s.Queue[0].Options[0] != "old" {
		t.Fatal("failed edit changed the queue")
	}
}

// 已有结账登记后改「合计几台」:数据库里的上限要同步,否则之后每次结账都会报上限已变。
func TestBundleEditSyncsRegisteredGroupLimit(t *testing.T) {
	mk := func(id, dc, status string) types.QueueItem {
		it := task(id, dc, status)
		it.OrderGroup, it.GroupLimit = "g", 2
		return it
	}
	s := bundleState(t, []types.QueueItem{mk("a1", "fra", "running"), mk("a2", "fra", "running")})
	if ok, err := s.DB.ClaimQueueOrder(s.Queue[0]); err != nil || !ok {
		t.Fatalf("first claim failed: %v", err)
	}
	if _, err := s.UpdateQueueBundle(BundleUpdate{IDs: []string{"a1", "a2"}, Options: []string{"old"}, Total: 4, Datacenters: []string{"fra"}}); err != nil {
		t.Fatal(err)
	}
	var next types.QueueItem
	for _, it := range s.Queue {
		if it.ID != "a1" {
			next = it
			break
		}
	}
	if next.GroupLimit != 4 {
		t.Fatalf("task limit not updated: %+v", next)
	}
	if ok, err := s.DB.ClaimQueueOrder(next); err != nil || !ok {
		t.Fatalf("claim after raising the total failed: %v", err)
	}
}
