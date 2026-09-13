package app

import (
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
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

// 队列总量闸门。
//
// 要防的是"多打一个数字":网页端建任务的循环是 for dc { for i < qty { 建一条 } },
// 两个上界都没有 —— 数量填 9999、选 5 个机房就是近 5 万条任务,
// 而每一条都是一次真实的下单尝试。TG 那条路早就有 MaxOrderQuantity/MaxOrderFanout,
// 网页端没有,这是同一个能力只做了一半的老毛病。
//
// 闸门放在 EnqueueItems 是因为四条入队路径全汇到这里,加一次就都受保护。
func TestEnqueueItemsRejectsRunawayBatch(t *testing.T) {
	s := enqueueTestState(t)

	mk := func(n int, prefix string) []types.QueueItem {
		out := make([]types.QueueItem, n)
		for i := range out {
			out[i] = types.QueueItem{
				ID: fmt.Sprintf("%s-%d", prefix, i), PlanCode: "24sk602",
				Datacenter: "gra", Status: "running",
			}
		}
		return out
	}

	// 先填到接近上限:这一批必须能进
	if err := s.EnqueueItems(mk(types.MaxQueueItems-1, "ok"), false); err != nil {
		t.Fatalf("上限内的批次不该被拒: %v", err)
	}
	if len(s.Queue) != types.MaxQueueItems-1 {
		t.Fatalf("队列应有 %d 条,实际 %d", types.MaxQueueItems-1, len(s.Queue))
	}

	// 正好到上限:仍然放行,闸门不能早关一条
	if err := s.EnqueueItems(mk(1, "edge"), false); err != nil {
		t.Fatalf("正好到上限不该被拒: %v", err)
	}

	// 再加就必须拒绝,而且一条都不能落进去(全有或全无)
	before := len(s.Queue)
	err := s.EnqueueItems(mk(10, "over"), false)
	if err == nil {
		t.Fatal("超过上限的批次没有被拒绝")
	}
	if len(s.Queue) != before {
		t.Errorf("被拒的批次污染了队列:之前 %d 条,现在 %d 条", before, len(s.Queue))
	}
	// 报错要说清楚是什么情况,不能只甩一个 "limit exceeded"
	for _, want := range []string{"上限", "下单尝试"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("报错里缺少 %q,用户看不懂该怎么办:%s", want, err.Error())
		}
	}

	// 落库的内容要和内存一致 —— 被拒的那批不能出现在库里
	rows, derr := s.DB.ListQueue()
	if derr != nil {
		t.Fatal(derr)
	}
	if len(rows) != before {
		t.Errorf("库里有 %d 条,内存里 %d 条", len(rows), before)
	}
}
