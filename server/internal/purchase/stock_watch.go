package purchase

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
)

// stockPollInterval 是运行中任务的公共库存探测间隔。
//
// 以前任务只在自己的 retryInterval 到点时查库存:手动任务默认 60 秒。
// 现在每个「大区 + 型号」只有一路只读探测,结果直达同区所有账户的同型号任务信箱;
// retryInterval 只决定失败后的完整重试节奏,不再决定发现补货的速度。
const stockPollInterval = 2 * time.Second

// stockMaxInFlight 固定节拍下允许重叠的请求数。往返较慢时(实测跨洲约 1 秒)
// 下一次探测照常按节拍发出,往返时间不再叠加到探测周期上;请求速率仍由节拍决定。
const stockMaxInFlight = 2

// stockWatchKey 库存接口免鉴权,同一大区所有账户看到的是同一份数据,所以按大区合并。
type stockWatchKey struct{ region, plan string }

type stockWatch struct {
	cancel   context.CancelFunc
	accounts map[string]int // 订阅账户 → 引用数
}

// stockWatcher 按大区和型号合并探测;任务数、机房数、账户数都不放大请求量。
type stockWatcher struct {
	state    *app.State
	ctx      context.Context
	interval time.Duration
	mu       sync.Mutex
	watches  map[stockWatchKey]*stockWatch
	closed   bool
	wg       sync.WaitGroup
	// fetch 仅供测试注入;nil 时用账户 transport 免签名读取
	fetch func(ctx context.Context, state *app.State, session *cartSession, account, plan string) ([]map[string]interface{}, error)
}

// newStockWatcher 创建绑定到队列处理器生命周期的探测器。
func newStockWatcher(ctx context.Context, state *app.State) *stockWatcher {
	return &stockWatcher{state: state, ctx: ctx, interval: stockPollInterval, watches: map[stockWatchKey]*stockWatch{}}
}

// regionOf 账户所在大区;找不到账户时退回账户 ID,保证不与别的账户错误合并。
func (w *stockWatcher) regionOf(account string) string {
	if w.state != nil {
		if acc, ok := w.state.FindAccount(account); ok {
			return ovh.EndpointRegion(acc.Endpoint)
		}
	}
	return "account:" + account
}

// subscribe 返回释放函数;同区最后一个任务离开时停止该型号的探测。
func (w *stockWatcher) subscribe(account, plan string) func() {
	if w == nil || account == "" || plan == "" {
		return func() {}
	}
	key := stockWatchKey{w.regionOf(account), plan}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.ctx.Err() != nil {
		return func() {}
	}
	watch := w.watches[key]
	if watch == nil {
		ctx, cancel := context.WithCancel(w.ctx)
		watch = &stockWatch{cancel: cancel, accounts: map[string]int{}}
		w.watches[key] = watch
		w.wg.Add(1)
		go func() { defer w.wg.Done(); w.run(ctx, key, watch) }()
	}
	watch.accounts[account]++
	released := false
	return func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if released {
			return
		}
		released = true
		watch.accounts[account]--
		if watch.accounts[account] <= 0 {
			delete(watch.accounts, account)
		}
		if len(watch.accounts) == 0 {
			watch.cancel()
			if w.watches[key] == watch {
				delete(w.watches, key)
			}
		}
	}
}

// close 停止全部探测并等待退出。
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

// pollInterval 跟随监控检查间隔加快(设置页最低 1 秒),但不慢于默认 2 秒。
func (w *stockWatcher) pollInterval() time.Duration {
	interval := w.interval
	if w.state != nil {
		if s := w.state.MonitorIntervalSeconds.Load(); s > 0 && time.Duration(s)*time.Second < interval {
			interval = time.Duration(s) * time.Second
		}
	}
	return interval
}

// subscribers 当前订阅该探测的账户,按 ID 排序以便轮换出站账户。
func (w *stockWatcher) subscribers(watch *stockWatch) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, 0, len(watch.accounts))
	for account := range watch.accounts {
		out = append(out, account)
	}
	sort.Strings(out)
	return out
}

// run 按固定节拍发出探测,最多 stockMaxInFlight 个请求重叠;失败时换同区下一个账户出站并退避。
// 每次结果以发出时间排序,迟到的旧结果不会覆盖较新的观察。
func (w *stockWatcher) run(ctx context.Context, key stockWatchKey, watch *stockWatch) {
	sem := make(chan struct{}, stockMaxInFlight)
	var inflight sync.WaitGroup
	defer inflight.Wait()
	var mu sync.Mutex
	failures, turn := 0, 0
	var newest time.Time // 已发布结果中最新的发出时间
	sessions := map[string]*cartSession{}
	defer func() {
		for _, s := range sessions {
			s.discard()
		}
	}()
	for ctx.Err() == nil {
		accounts := w.subscribers(watch)
		if len(accounts) == 0 {
			return
		}
		mu.Lock()
		account := accounts[turn%len(accounts)]
		session := sessions[account]
		if session == nil {
			session = &cartSession{}
			sessions[account] = session
		}
		delay := w.pollInterval()
		for i := 1; i <= failures && delay < 30*time.Second; i++ {
			delay *= 2
		}
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		mu.Unlock()

		select {
		case sem <- struct{}{}:
			inflight.Add(1)
			started := time.Now()
			go func() {
				defer inflight.Done()
				defer func() { <-sem }()
				pollCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
				defer cancel()
				// 同一账户的 session 不能并发使用(SDK 实例会改写 Timeout);重叠请求各自隔离。
				variants, err := w.poll(pollCtx, &mu, session, account, key.plan)
				mu.Lock()
				if err != nil {
					if ctx.Err() == nil {
						failures++
						turn++ // 换同区下一个账户的出站,避开单个坏代理
						if failures == 1 && w.state != nil {
							w.state.Logger.Debug("库存探测失败,任务仍按原重试节奏运行: "+err.Error(), "purchase")
						}
					}
					mu.Unlock()
					return
				}
				failures = 0
				if !started.After(newest) {
					mu.Unlock() // 比已发布的结果更早发出:内容可能更旧,丢弃
					return
				}
				newest = started
				detected := time.Now()
				mu.Unlock()
				for _, subscriber := range w.subscribers(watch) {
					w.state.StockSignals.Observe(app.StockSignal{AccountID: subscriber, PlanCode: key.plan, Variants: variants, DetectedAt: detected})
				}
			}()
		case <-ctx.Done():
			return
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

// poll 读取一次库存。每个请求使用独立隔离的 SDK 实例(共享账户 transport 连接池),
// 重叠请求互不干扰;session 只在 mu 保护下用于取得账户的基础 client。
func (w *stockWatcher) poll(ctx context.Context, mu *sync.Mutex, session *cartSession, account, plan string) ([]map[string]interface{}, error) {
	if w.fetch != nil {
		return w.fetch(ctx, w.state, session, account, plan)
	}
	mu.Lock()
	base, err := session.clientFor(w.state, account)
	mu.Unlock()
	if err != nil {
		return nil, err
	}
	client, err := ovh.IsolateClient(base)
	if err != nil {
		return nil, err
	}
	return ovh.GetAvailabilities(ctx, client, plan)
}
