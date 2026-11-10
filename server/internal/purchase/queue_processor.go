package purchase

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

const maxConcurrentTasks = 64

// failNotice 把同一批任务的终止通知合并成一条。
//
// 为什么必须合并:一条 /buy 最多扇出 60 个任务(MaxOrderFanout),
// 而跨区 planCode 这类 Fatal 是**每个任务第一轮就判**的 ——
// 不合并的话用户会在几秒内收到 60 条一模一样的消息,
// 手机被刷屏不说,真正要看的那条原因也被淹了。
//
// 合并键是 (planCode, 原因),因为同一批失败的原因必然相同;
// 机房不同不拆开 —— "24sk602 在 6 个机房全失败"是一件事,不是六件。
var failNotice = &failNotifier{seen: map[string]*failGroup{}}

type failGroup struct {
	first    time.Time
	count    int
	notified bool
	planCode string
	reason   string
	fatal    bool
	dcs      []string
}

type failNotifier struct {
	mu   sync.Mutex
	seen map[string]*failGroup
}

// failNoticeWindow 同一个 (planCode, 原因) 在这个窗口内只发一条。
// 30 秒足够覆盖一批扇出任务的首轮判定(它们几乎同时跑),
// 又短到不会把"半分钟后又一批真的失败了"给吞掉。
const failNoticeWindow = 30 * time.Second

// take 判断这条终止要不要发通知。返回 (消息, 是否发送)。
// 窗口内第一条立刻发并带上"还会有更多"的说明;后续的只累加计数,不再发。
func (f *failNotifier) take(item *types.QueueItem, reason string, fatal bool) (string, bool) {
	key := item.PlanCode + "|" + reason
	now := time.Now()

	f.mu.Lock()
	defer f.mu.Unlock()
	// 顺手清过期的,不引定时器
	for k, g := range f.seen {
		if now.Sub(g.first) > failNoticeWindow {
			delete(f.seen, k)
		}
	}
	g, ok := f.seen[key]
	if !ok {
		g = &failGroup{first: now, planCode: item.PlanCode, reason: reason, fatal: fatal}
		f.seen[key] = g
	}
	g.count++
	if item.Datacenter != "" && len(g.dcs) < 8 {
		dup := false
		for _, d := range g.dcs {
			if d == item.Datacenter {
				dup = true
				break
			}
		}
		if !dup {
			g.dcs = append(g.dcs, item.Datacenter)
		}
	}
	if g.notified {
		return "", false
	}
	g.notified = true
	return buildTaskFailedMessage(item, reason, fatal), true
}

// buildTaskFailedMessage 任务终止时发给用户的通知。
//
// 要回答三件事:哪一单、为什么停、我现在能做什么。
// 尤其是"能做什么"—— Fatal 和用尽重试的下一步完全不同:
// 前者改了才有用(换本区 planCode / 重新配账户),后者可以直接重开。
func buildTaskFailedMessage(item *types.QueueItem, reason string, fatal bool) string {
	var b strings.Builder
	if fatal {
		b.WriteString("🛑 抢购任务已停止（重试也不会变）" + "\n" + "\n")
	} else {
		b.WriteString("⚠️ 抢购任务已停止（连续下单失败）" + "\n" + "\n")
	}
	b.WriteString("型号：" + item.PlanCode + "\n")
	if item.Datacenter != "" {
		b.WriteString("机房：" + strings.ToUpper(item.Datacenter) + "\n")
	}
	b.WriteString("原因：" + reason + "\n")
	if fatal {
		b.WriteString("\n" + "这类失败换个时间点重试结果一样，需要先改掉原因。" + "\n" +
			"常见的是型号和账户不在同一个区 —— 三个大区的目录互不相通，" + "\n" +
			"同一台机器在不同区是不同的型号代码。发 /accounts 看当前账户。" + "\n")
	} else {
		b.WriteString("\n" + "库存可能已经被抢完。想接着抢就重新下一单。" + "\n")
	}
	b.WriteString("\n" + "查看 /queue · 历史里有完整报错")
	return b.String()
}

// ProcessQueueLoop 持续执行未完成任务,已下单任务保留为只读记录。
func ProcessQueueLoop(state *app.State) {
	processQueueLoop(context.Background(), state, PurchaseServer)
}

// processQueueLoop 为每个任务维护独立 worker,慢查询/落库/通知不再挡住其它机房。
func processQueueLoop(loopCtx context.Context, state *app.State, purchase func(context.Context, *app.State, *types.QueueItem) Outcome) {
	ctx, cancel := context.WithCancel(loopCtx)
	sales := newSaleWatcher(ctx, state)
	stocks := newStockWatcher(ctx, state)
	var wg sync.WaitGroup
	defer func() { cancel(); sales.close(); stocks.close(); wg.Wait() }()
	wg.Add(1)
	go func() { defer wg.Done(); transportWarmLoop(ctx, state) }()
	type worker struct {
		cancel context.CancelFunc
		done   chan struct{}
	}
	active := map[string]worker{}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	nextRestore := time.Time{}
	for ctx.Err() == nil {
		if state.SaveBlocked("history") == nil && state.SaveBlocked("queue") == nil {
			if !time.Now().Before(nextRestore) {
				if err := state.RestoreCompletedQueue(); err != nil {
					state.Logger.Error("修复已下单任务失败: "+err.Error(), "queue")
				}
				nextRestore = time.Now().Add(time.Second)
			}
			state.QueueMu.Lock()
			items := append([]types.QueueItem(nil), state.Queue...)
			state.QueueMu.Unlock()
			sort.SliceStable(items, func(i, j int) bool {
				if items[i].QuickOrder != items[j].QuickOrder {
					return items[i].QuickOrder
				}
				return items[i].CreatedAt > items[j].CreatedAt
			})
			running := map[string]bool{}
			allIDs := map[string]bool{}
			for _, item := range items {
				allIDs[item.ID] = true
				if item.Status == "running" {
					running[item.ID] = true
				}
			}
			for id, w := range active {
				select {
				case <-w.done:
					delete(active, id)
				default:
					if !running[id] {
						w.cancel()
					}
				}
			}
			state.DeletedTaskIDsMu.Lock()
			for id := range state.DeletedTaskIDs {
				if _, busy := active[id]; !allIDs[id] && !busy {
					delete(state.DeletedTaskIDs, id)
				}
			}
			state.DeletedTaskIDsMu.Unlock()
			// 一次下单最多扇出 60 个任务;64 个独立 worker 覆盖整批且限制资源占用。
			for _, item := range items {
				if !running[item.ID] || state.IsTaskDeleted(item.ID) {
					continue
				}
				if _, ok := active[item.ID]; ok {
					continue
				}
				if len(active) >= maxConcurrentTasks {
					break
				}
				workerCtx, stop := context.WithCancel(ctx)
				done := make(chan struct{})
				active[item.ID] = worker{stop, done}
				wg.Add(1)
				go func(item types.QueueItem) {
					defer wg.Done()
					defer close(done)
					defer stop()
					if !item.Preorder || item.PreorderWaitStock {
						defer stocks.subscribe(stockWatchAccount(state, item.AccountID), item.PlanCode)()
					}
					taskWorker(workerCtx, state, item, purchase, sales)
				}(item)
			}
		} else {
			for _, w := range active {
				w.cancel()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-state.QueueChanges():
		}
	}
}

// stockWatchAccount 与 taskWorker 使用同一个解析后的账户 ID 订阅信号。
func stockWatchAccount(state *app.State, account string) string {
	if acc, ok := state.FindAccount(account); ok {
		return acc.ID
	}
	return account
}

// taskWorker 的直接信箱可以在重试定时器之前唤醒已预装的无货任务。
func taskWorker(ctx context.Context, state *app.State, item types.QueueItem, purchase func(context.Context, *app.State, *types.QueueItem) Outcome, sales *saleWatcher) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	session := &cartSession{}
	defer session.discard()
	account := item.AccountID
	if acc, ok := state.FindAccount(account); ok {
		account = acc.ID
	}
	signals, unsubscribe := state.StockSignals.Subscribe(account, item.PlanCode)
	defer unsubscribe()
	var saleChanges <-chan struct{}
	if item.Preorder {
		var stop func()
		saleChanges, stop = sales.subscribe(account, item.PlanCode, item.Duration)
		defer stop()
	}
	state.QueueMu.Lock()
	if ctx.Err() != nil {
		state.QueueMu.Unlock()
		return
	}
	for i := range state.Queue {
		if state.Queue[i].ID == item.ID && state.Queue[i].AccountID == "" && account != "" {
			state.Queue[i].AccountID = account
			break
		}
	}
	state.RegisterTaskCancel(item.ID, cancel)
	state.QueueMu.Unlock()
	defer state.UnregisterTaskCancel(item.ID)
	ctx = context.WithValue(ctx, cartSessionKey{}, session)
	var signal *app.StockSignal
	for ctx.Err() == nil {
		state.QueueMu.Lock()
		var current *types.QueueItem
		for _, it := range state.Queue {
			if it.ID == item.ID {
				copy := it
				current = &copy
				break
			}
		}
		state.QueueMu.Unlock()
		if current == nil || current.Status != "running" || state.IsTaskDeleted(item.ID) {
			return
		}
		attemptCtx := ctx
		if current.Preorder {
			attemptCtx = context.WithValue(attemptCtx, saleSignalKey{}, sales.latest(account, current.PlanCode))
		}
		if signal != nil {
			attemptCtx = context.WithValue(attemptCtx, stockSignalKey{}, *signal)
			signal = nil
		}
		outcome := runQueueAttempt(attemptCtx, state, *current, purchase)
		if outcome.Success || outcome.Fatal || outcome.PauseTask || outcome.Cancelled || outcome.GroupStopped != "" {
			return
		}
		if outcome.RetryImmediately {
			continue
		}
		interval := time.Duration(types.ClampRetryInterval(current.RetryInterval, state.Config.RetryInterval())) * time.Second
		if outcome.WaitingForCatalog && interval > catalog.PreorderCheckInterval {
			interval = catalog.PreorderCheckInterval
		}
		if outcome.WaitingForQuota {
			interval = 2 * time.Second
		}
		if outcome.RetryAfter > interval {
			interval = outcome.RetryAfter
		}
		if outcome.RetrySoon > 0 && outcome.RetrySoon < interval {
			interval = outcome.RetrySoon
		}
		timer := time.NewTimer(interval)
		waiting := true
		for waiting {
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				waiting = false
			case <-saleChanges:
				if outcome.WaitingForCatalog || outcome.WaitingForStock {
					timer.Stop()
					waiting = false
				}
			case observed := <-signals:
				if outcome.WaitingForStock && stockSignalOrderable(observed, *current) {
					timer.Stop()
					// 任务忙于建车时收到的信号可能已超过 1 秒:不再丢弃,立即唤醒并重新查库存,
					// 只有新鲜信号才直接复用,避免按陈旧库存结账。
					if time.Since(observed.DetectedAt) < time.Second {
						signal = &observed
					}
					waiting = false
				}
			}
		}
	}
}

// stockSignalOrderable 只唤醒确实出现可购买库存的目标机房;最终硬件匹配仍由 PurchaseServer 复核。
func stockSignalOrderable(signal app.StockSignal, item types.QueueItem) bool {
	for _, variant := range signal.Variants {
		dcs, _ := variant["datacenters"].([]interface{})
		for _, raw := range dcs {
			dc, _ := raw.(map[string]interface{})
			name, _ := dc["datacenter"].(string)
			status, _ := dc["availability"].(string)
			if name == ovh.ConvertDisplayDCToAPIDC(item.Datacenter) && catalog.IsAvailableForOrder(status) {
				return true
			}
		}
	}
	return false
}
