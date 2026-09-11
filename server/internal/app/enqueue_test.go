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

func enqueueTestState(t *testing.T) *State {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	lg := logger.New(filepath.Join(dir, "t.log"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	return NewState(storage.Paths{DataDir: dir}, config.New(database), lg, database)
}

// 入队和落库必须是一件事:要么内存和磁盘都有,要么两边都没有。
//
// 半成功状态("这次能跑但重启就丢")对抢购是最坏的 —— 它看起来完全正常:
// 界面上任务在跑、日志里也在重试,直到某次重启后它凭空消失,
// 而用户以为自己一直在抢这台机器。
func TestEnqueueItemsAllOrNothing(t *testing.T) {
	s := enqueueTestState(t)

	items := []types.QueueItem{
		{ID: "a", PlanCode: "24sk602", Datacenter: "gra", Status: "running"},
		{ID: "b", PlanCode: "24sk602", Datacenter: "rbx", Status: "running"},
	}
	if err := s.EnqueueItems(items, false); err != nil {
		t.Fatalf("正常入队不该失败: %v", err)
	}
	if len(s.Queue) != 2 {
		t.Fatalf("内存里应有 2 条,实际 %d", len(s.Queue))
	}
	rows, err := s.DB.ListQueue()
	if err != nil || len(rows) != 2 {
		t.Fatalf("库里应有 2 条,实际 %d (err=%v)", len(rows), err)
	}

	// prepend:快速下单要抢在已有任务前面
	if err := s.EnqueueItems([]types.QueueItem{{ID: "c", PlanCode: "x", Status: "running"}}, true); err != nil {
		t.Fatal(err)
	}
	if s.Queue[0].ID != "c" {
		t.Fatalf("prepend 应放队首,实际队首是 %s", s.Queue[0].ID)
	}

	// 落库被拒(启动时这张表没读出来)→ 必须整批撤回,内存不留残留
	s.MarkLoadFailed("queue", errStub{})
	before := len(s.Queue)
	err = s.EnqueueItems([]types.QueueItem{
		{ID: "d", PlanCode: "y", Status: "running"},
		{ID: "e", PlanCode: "z", Status: "running"},
	}, false)
	if err == nil {
		t.Fatal("落库被拒时必须返回错误,否则调用方会告诉用户「已加入队列」")
	}
	if len(s.Queue) != before {
		t.Fatalf("失败后内存必须回到原样:期望 %d 条,实际 %d —— 残留的任务会跑但重启就没",
			before, len(s.Queue))
	}
	for _, it := range s.Queue {
		if it.ID == "d" || it.ID == "e" {
			t.Fatalf("撤回不干净,%s 还在内存里", it.ID)
		}
	}

	// 空批次是合法的空操作,不该报错也不该写库
	if err := s.EnqueueItems(nil, false); err != nil {
		t.Fatalf("空批次不该报错: %v", err)
	}
}

type errStub struct{}

func (errStub) Error() string { return "模拟启动时读表失败" }
