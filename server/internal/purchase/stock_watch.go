package purchase

import (
	"context"
	"github.com/ovh-buy/server/internal/ovh"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
)

// stockPollInterval 是运行中任务的公共库存探测间隔。
//
// 以前任务只在自己的 retryInterval 到点时查库存:手动任务默认 60 秒,
// 没有监控订阅覆盖的型号要等将近一分钟才发现补货。现在每个账户/型号
// 只有一个只读探测,结果经 StockSignals 直达所有同型号任务的信箱;
// retryInterval 只决定失败后的完整重试节奏,不再决定发现补货的速度。
const stockPollInterval = 2 * time.Second

type stockWatchKey struct{ account, plan string }

type stockWatch struct {
	cancel context.CancelFunc
	refs   int
}

// stockWatcher 按账户和型号合并探测;任务数量和机房数量不放大请求量。
type stockWatcher struct {
	state    *app.State
	ctx      context.Context
	interval time.Duration
	mu       sync.Mutex
	watches  map[stockWatchKey]*stockWatch
	closed   bool
	wg       sync.WaitGroup
	fetch    func(ctx context.Context, state *app.State, session *cartSession, account, plan string) ([]map[string]interface{}, error)
}

func newStockWatcher(ctx context.Context, state *app.State) *stockWatcher {
	return &stockWatcher{state: state, ctx: ctx, interval: stockPollInterval, watches: map[stockWatchKey]*stockWatch{}, fetch: fetchAvailabilities}
}

func fetchAvailabilities(ctx context.Context, state *app.State, session *cartSession, account, plan string) ([]map[string]interface{}, error) {
	client, err := session.clientFor(state, account)
	if err != nil {
		return nil, err
	}
	return ovh.GetAvailabilities(ctx, client, plan)
}

// subscribe 返回释放函数;最后一个任务离开时停止该型号的探测。
func (w *stockWatcher) subscribe(account, plan string) func() {
	if w == nil || account == "" || plan == "" {
		return func() {}
	}
	key := stockWatchKey{account, plan}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.ctx.Err() != nil {
		return func() {}
	}
	watch := w.watches[key]
	if watch == nil {
		ctx, cancel := context.WithCancel(w.ctx)
		watch = &stockWatch{cancel: cancel}
		w.watches[key] = watch
		w.wg.Add(1)
		go func() { defer w.wg.Done(); w.run(ctx, key) }()
	}
	watch.refs++
	released := false
	return func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if released {
			return
		}
		released = true
		watch.refs--
		if watch.refs <= 0 {
			watch.cancel()
			if w.watches[key] == watch {
				delete(w.watches, key)
			}
		}
	}
}

func (w *stockWatcher) close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.closed = true
	for _, watch := range w.watches {
		watch.cancel()
	}
	w.mu.Unlock()
	w.wg.Wait()
}

func (w *stockWatcher) run(ctx context.Context, key stockWatchKey) {
	session := &cartSession{}
	defer session.discard()
	failures := 0
	for ctx.Err() == nil {
		pollCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		_, err := w.state.StockSignals.Fetch(pollCtx, key.account, key.plan, func() ([]map[string]interface{}, error) {
			return w.fetch(pollCtx, w.state, session, key.account, key.plan)
		})
		cancel()
		delay := w.interval
		if err != nil && ctx.Err() == nil {
			failures++
			for i := 1; i < failures && delay < 30*time.Second; i++ {
				delay *= 2
			}
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			if failures == 1 {
				w.state.Logger.Debug("库存探测失败,任务仍按原重试节奏运行: "+err.Error(), "purchase")
			}
		} else {
			failures = 0
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
