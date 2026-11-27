package purchase

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

func sameConfigQueue(n int) []types.QueueItem {
	out := make([]types.QueueItem, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, types.QueueItem{
			ID: fmt.Sprintf("t%02d", i), PlanCode: "plan", Datacenter: "gra", Options: []string{"ram", "disk"},
			Status: "running", RetryInterval: 60, CreatedAt: fmt.Sprintf("2026-11-27T00:00:%02dZ", i),
		})
	}
	return out
}

func runLoop(t *testing.T, state *app.State, purchase func(context.Context, *app.State, *types.QueueItem) Outcome) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); processQueueLoop(ctx, state, purchase) }()
	return func() { cancel(); <-done }
}

// 无货时同配置只派一个任务查库存/预装,其余不发请求。
func TestSameConfigGroupOnlyPrewarmsFewWhileOutOfStock(t *testing.T) {
	state := queueCompletionState(t)
	state.Queue = sameConfigQueue(12)
	var mu sync.Mutex
	called := map[string]int{}
	stop := runLoop(t, state, func(_ context.Context, _ *app.State, item *types.QueueItem) Outcome {
		mu.Lock()
		called[item.ID]++
		mu.Unlock()
		return Outcome{WaitingForStock: true}
	})
	time.Sleep(600 * time.Millisecond)
	stop()
	mu.Lock()
	defer mu.Unlock()
	if len(called) != groupPrewarmSlots {
		t.Fatalf("expected %d prewarming tasks, got %d: %v", groupPrewarmSlots, len(called), called)
	}
	for _, id := range []string{"t00", "t01", "t02"}[:groupPrewarmSlots] {
		if called[id] == 0 {
			t.Fatalf("oldest tasks must hold the prewarm slots, %s missing: %v", id, called)
		}
	}
	// 没轮到的组员也跟着组更新检查时间
	state.QueueMu.Lock()
	defer state.QueueMu.Unlock()
	for _, it := range state.Queue {
		if it.LastCheckTime == 0 {
			t.Fatalf("waiting member %s was never touched", it.ID)
		}
	}
}

// 预装任务看到货:其余组员立刻全部提交,每个任务只提交一次,并发不超过 groupFanout。
func TestSameConfigGroupFansOutWhenStockSeen(t *testing.T) {
	state := queueCompletionState(t)
	state.Queue = sameConfigQueue(25)
	var inFlightNow, peak atomic.Int32
	var mu sync.Mutex
	submitted := map[string]int{}
	var stockOpen atomic.Bool
	go func() { time.Sleep(300 * time.Millisecond); stockOpen.Store(true) }()
	stop := runLoop(t, state, func(_ context.Context, _ *app.State, item *types.QueueItem) Outcome {
		if !stockOpen.Load() {
			return Outcome{WaitingForStock: true, RetrySoon: 100 * time.Millisecond}
		}
		n := inFlightNow.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		inFlightNow.Add(-1)
		mu.Lock()
		submitted[item.ID]++
		mu.Unlock()
		return Outcome{Success: true, Attempted: true}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(submitted)
		mu.Unlock()
		if n == 25 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	mu.Lock()
	defer mu.Unlock()
	if len(submitted) != 25 {
		t.Fatalf("all members must be submitted once stock is seen, got %d", len(submitted))
	}
	for id, n := range submitted {
		if n != 1 {
			t.Fatalf("task %s submitted %d times", id, n)
		}
	}
	if p := peak.Load(); p > groupFanout {
		t.Fatalf("concurrent checkouts %d exceed fan-out limit %d", p, groupFanout)
	}
	state.QueueMu.Lock()
	defer state.QueueMu.Unlock()
	for _, it := range state.Queue {
		if it.Status != "completed" {
			t.Fatalf("task %s not completed: %s", it.ID, it.Status)
		}
	}
}

// 不同配置(机房、自动付款、选项)不合并;选项顺序不同仍算同配置;预售不合并。
func TestGroupKeyOnlyMergesIdenticalConfigs(t *testing.T) {
	state := queueCompletionState(t)
	base := types.QueueItem{AccountID: "acc", PlanCode: "plan", Datacenter: "gra", Options: []string{"a", "b"}, Duration: "P1M"}
	same := base
	same.Options = []string{"b", "a"}
	same.Datacenter = "GRA"
	if groupKey(state, base) != groupKey(state, same) {
		t.Fatal("option order / datacenter case must not split a group")
	}
	for name, mutate := range map[string]func(*types.QueueItem){
		"datacenter": func(i *types.QueueItem) { i.Datacenter = "rbx" },
		"autoPay":    func(i *types.QueueItem) { i.AutoPay = true },
		"options":    func(i *types.QueueItem) { i.Options = []string{"a"} },
		"duration":   func(i *types.QueueItem) { i.Duration = "P12M" },
		"coupon":     func(i *types.QueueItem) { i.Coupon = "X" },
		"account":    func(i *types.QueueItem) { i.AccountID = "other" },
	} {
		other := base
		other.Options = append([]string(nil), base.Options...)
		mutate(&other)
		if groupKey(state, base) == groupKey(state, other) {
			t.Fatalf("different %s must not merge", name)
		}
	}
	pre := base
	pre.Preorder = true
	if groupKey(state, pre) != "" {
		t.Fatal("preorder tasks are scheduled individually")
	}
}

// 任务换组时旧组那一轮还在结账:新组不得对同一任务再提交一次。
func TestRegroupedTaskIsNeverSubmittedTwiceConcurrently(t *testing.T) {
	state := queueCompletionState(t)
	state.Queue = sameConfigQueue(1)
	var concurrent, peak atomic.Int32
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	stop := runLoop(t, state, func(ctx context.Context, _ *app.State, item *types.QueueItem) Outcome {
		n := concurrent.Add(1)
		defer concurrent.Add(-1)
		if n > peak.Load() {
			peak.Store(n)
		}
		entered <- struct{}{}
		<-release // 模拟已发出、不接受取消的结账
		return Outcome{WaitingForStock: true}
	})
	defer stop()
	<-entered
	// 切换自动付款 → 换组;旧组被取消但结账还没返回
	state.QueueMu.Lock()
	state.Queue[0].AutoPay = true
	state.QueueMu.Unlock()
	state.WakeQueue()
	time.Sleep(400 * time.Millisecond)
	if p := peak.Load(); p > 1 {
		t.Fatalf("task submitted concurrently by two groups (%d)", p)
	}
	close(release)
}

// 切换自动付款只改这一项,已下单任务不动,运行中的任务换到新组。
func TestSetQueueAutoPayOnlyTouchesUnfinishedTasks(t *testing.T) {
	state := queueCompletionState(t)
	state.Queue = sameConfigQueue(3)
	state.Queue[2].Status = "completed"
	if err := state.SaveQueue(); err != nil {
		t.Fatal(err)
	}
	n, err := state.SetQueueAutoPay([]string{"t00", "t01", "t02"}, true)
	if err != nil || n != 2 {
		t.Fatalf("expected 2 updated, got %d %v", n, err)
	}
	if !state.Queue[0].AutoPay || !state.Queue[1].AutoPay || state.Queue[2].AutoPay {
		t.Fatal("auto-pay toggled on the wrong tasks")
	}
	if state.Queue[0].Datacenter != "gra" || len(state.Queue[0].Options) != 2 || state.Queue[0].Status != "running" {
		t.Fatal("other settings must stay unchanged")
	}
	if n, _ := state.SetQueueAutoPay([]string{"t00"}, true); n != 0 {
		t.Fatal("no-op toggle reported changes")
	}
}

// 探测任务一看到货(结账前)就启动其余组员:它们与探测任务的结账同时进行,而不是等它结束。
func TestProbeStartsRestBeforeItsOwnCheckoutFinishes(t *testing.T) {
	state := queueCompletionState(t)
	state.Queue = sameConfigQueue(6)
	var stockOpen atomic.Bool
	restEntered := make(chan string, 8)
	probeDone := make(chan struct{})
	var mu sync.Mutex
	submitted := map[string]int{}
	stop := runLoop(t, state, func(ctx context.Context, _ *app.State, item *types.QueueItem) Outcome {
		if !stockOpen.Load() {
			return Outcome{WaitingForStock: true, RetrySoon: 50 * time.Millisecond}
		}
		if notify, ok := ctx.Value(stockFoundKey{}).(func()); ok {
			// 探测任务:报告有货,然后「结账」,直到看到其余组员都已经开始
			notify()
			for i := 0; i < 5; i++ {
				select {
				case <-restEntered:
				case <-time.After(2 * time.Second):
					t.Error("rest of the group did not start while the probe was checking out")
					return Outcome{Success: true, Attempted: true}
				}
			}
			close(probeDone)
		} else {
			restEntered <- item.ID
			<-probeDone
		}
		mu.Lock()
		submitted[item.ID]++
		mu.Unlock()
		return Outcome{Success: true, Attempted: true}
	})
	time.Sleep(150 * time.Millisecond)
	stockOpen.Store(true)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(submitted)
		mu.Unlock()
		if n == 6 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	mu.Lock()
	defer mu.Unlock()
	if len(submitted) != 6 {
		t.Fatalf("expected all 6 submitted, got %d", len(submitted))
	}
	for id, n := range submitted {
		if n != 1 {
			t.Fatalf("task %s submitted %d times", id, n)
		}
	}
}
