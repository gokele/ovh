package purchase

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

// 同账户同型号的多个任务只产生一路探测,结果投递到每个任务的信箱;最后一个任务离开即停止。
func TestStockWatcherSharesOnePollAndStops(t *testing.T) {
	state := queueCompletionState(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newStockWatcher(ctx, state)
	w.interval = 20 * time.Millisecond
	var polls atomic.Int32
	w.fetch = func(context.Context, *app.State, *cartSession, string, string) ([]map[string]interface{}, error) {
		polls.Add(1)
		return []map[string]interface{}{{"datacenters": []interface{}{map[string]interface{}{"datacenter": "gra", "availability": "1H-low"}}}}, nil
	}
	signals, unsubscribe := state.StockSignals.Subscribe("acc", "plan")
	defer unsubscribe()
	stopA := w.subscribe("acc", "plan")
	stopB := w.subscribe("acc", "plan")
	select {
	case s := <-signals:
		if !stockSignalOrderable(s, types.QueueItem{Datacenter: "gra"}) {
			t.Fatal("signal not orderable")
		}
	case <-time.After(time.Second):
		t.Fatal("no signal from shared poll")
	}
	w.mu.Lock()
	if len(w.watches) != 1 {
		t.Fatalf("want 1 shared watch, got %d", len(w.watches))
	}
	w.mu.Unlock()
	stopA()
	stopA() // 重复释放不能把别的任务的引用也扣掉
	w.mu.Lock()
	alive := len(w.watches)
	w.mu.Unlock()
	if alive != 1 {
		t.Fatal("watch stopped while a task still uses it")
	}
	stopB()
	w.close()
	before := polls.Load()
	time.Sleep(60 * time.Millisecond)
	if polls.Load() != before {
		t.Fatal("poll continued after last task left")
	}
}

// 任务忙时收到的补货信号超过 1 秒后才被读到,也必须立即唤醒,而不是睡满 60 秒重试间隔。
func TestStaleStockSignalStillWakesTask(t *testing.T) {
	state := queueCompletionState(t)
	state.Queue = []types.QueueItem{{ID: "t1", PlanCode: "plan", Datacenter: "gra", Status: "running", RetryInterval: 60}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	first := make(chan struct{})
	var sawSignal atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		processQueueLoop(ctx, state, func(c context.Context, _ *app.State, _ *types.QueueItem) Outcome {
			if calls.Add(1) == 1 {
				close(first)
			} else if _, ok := c.Value(stockSignalKey{}).(app.StockSignal); ok {
				sawSignal.Store(true)
			}
			return Outcome{WaitingForStock: true}
		})
	}()
	defer func() { cancel(); <-done }()
	<-first
	time.Sleep(50 * time.Millisecond)
	state.StockSignals.Publish(app.StockSignal{PlanCode: "plan", DetectedAt: time.Now().Add(-2 * time.Second),
		Variants: []map[string]interface{}{{"datacenters": []interface{}{map[string]interface{}{"datacenter": "gra", "availability": "1H-low"}}}}})
	deadline := time.Now().Add(time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() < 2 {
		t.Fatal("stale restock signal was dropped; task kept sleeping")
	}
	if sawSignal.Load() {
		t.Fatal("stale signal must trigger a fresh stock read, not be reused for checkout")
	}
}

// 共享库存探测跟随更快的监控间隔,但永远不慢于默认 2 秒。
func TestStockPollFollowsFasterMonitorInterval(t *testing.T) {
	state := queueCompletionState(t)
	w := newStockWatcher(context.Background(), state)
	if got := w.pollInterval(); got != stockPollInterval {
		t.Fatalf("default = %s", got)
	}
	state.MonitorIntervalSeconds.Store(1)
	if got := w.pollInterval(); got != time.Second {
		t.Fatalf("1s monitor should speed polling, got %s", got)
	}
	state.MonitorIntervalSeconds.Store(30)
	if got := w.pollInterval(); got != stockPollInterval {
		t.Fatalf("slow monitor must not slow polling, got %s", got)
	}
}
