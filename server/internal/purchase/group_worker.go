package purchase

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/types"
)

// 同配置合并抢购。
//
// 一个任务仍然对应一台服务器(数据库、编辑、暂停、历史都不变),变的只是调度:
// 同账户 + 型号 + 机房 + 选项 + 周期 + 自动付款 + 优惠码的运行中任务归成一组,由一个 worker 负责。
//   - 无货时只派一个任务(探测任务)查库存并预装购物车,其余不占 OVH 请求;
//   - 探测任务一看到货(查完库存、结账之前)就立刻启动其余组员同时下单;
//     公共库存探测信号到达时全组直接一起提交。并发上限 groupFanout,避免同一账户瞬间几十个结账撞 429。
//
// 预售任务走自己的开售状态机,不参与合并。
const (
	groupPrewarmSlots = 1
	groupFanout       = 10
)

// stockFoundKey 携带「探测任务看到货」的回调,由 PurchaseServer 在结账前调用。
type stockFoundKey struct{}

// groupKey 返回合并键;返回 "" 表示该任务单独调度。
func groupKey(state *app.State, item types.QueueItem) string {
	if item.Preorder {
		return ""
	}
	opts := append([]string(nil), item.Options...)
	for i := range opts {
		opts[i] = strings.TrimSpace(opts[i])
	}
	sort.Strings(opts)
	return strings.Join([]string{
		stockWatchAccount(state, item.AccountID),
		item.PlanCode,
		strings.ToLower(strings.TrimSpace(item.Datacenter)),
		strings.Join(opts, ","),
		strings.TrimSpace(item.Duration),
		strconv.FormatBool(item.AutoPay),
		strings.ToUpper(strings.TrimSpace(item.Coupon)),
	}, "|")
}

// groupMembers 当前仍在运行的组员,排序稳定:监控自动下单优先,其次先建的优先 ——
// 新加任务不会把正在预装的任务挤出预装名额。
func groupMembers(state *app.State, key string) []types.QueueItem {
	state.QueueMu.Lock()
	var out []types.QueueItem
	for _, it := range state.Queue {
		if it.Status == "running" && !state.IsTaskDeleted(it.ID) && groupKey(state, it) == key {
			out = append(out, it)
		}
	}
	state.QueueMu.Unlock()
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].QuickOrder != out[j].QuickOrder {
			return out[i].QuickOrder
		}
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// inFlight 保证同一个任务任何时刻只有一次尝试在跑。
// 任务改了配置(比如切换自动付款)会换组,旧组那一轮可能还在结账 —— 结账发出后不接受取消,
// 没有这把锁的话新组会对同一个任务再结账一次,等于多买一台。
type inFlight struct{ m sync.Map }

func (f *inFlight) acquire(id string) bool {
	_, loaded := f.m.LoadOrStore(id, struct{}{})
	return !loaded
}

func (f *inFlight) release(id string) { f.m.Delete(id) }

// groupWorker 一组同配置任务共用一个 worker,直到组里没有运行中的任务。
// wake 由调度器在组员增减时投递(QueueChanges 只能有一个消费者,组不能直接读它)。
func groupWorker(ctx context.Context, state *app.State, key, account, planCode string, purchase func(context.Context, *app.State, *types.QueueItem) Outcome, busy *inFlight, wake <-chan struct{}) {
	signals, unsubscribe := state.StockSignals.Subscribe(account, planCode)
	defer unsubscribe()
	sessions := map[string]*cartSession{}
	defer func() {
		for _, s := range sessions {
			s.discard()
		}
	}()

	var signal *app.StockSignal
	runAll := false
	for ctx.Err() == nil {
		members := groupMembers(state, key)
		if len(members) == 0 {
			return
		}
		// 离开本组的任务(完成/暂停/删除/改配置)释放它的预装购物车
		present := map[string]bool{}
		for _, m := range members {
			present[m.ID] = true
			if sessions[m.ID] == nil {
				sessions[m.ID] = &cartSession{}
			}
		}
		for id, s := range sessions {
			if !present[id] {
				s.discard()
				delete(sessions, id)
			}
		}

		targets := members
		if !runAll && signal == nil && len(members) > groupPrewarmSlots {
			targets = members[:groupPrewarmSlots]
			touchWaitingMembers(state, members[groupPrewarmSlots:])
		}
		sem := make(chan struct{}, groupFanout)
		var outcomes []Outcome
		fannedOut := false
		if len(targets) < len(members) {
			// 探测轮:探测任务看到货时立刻并发启动其余组员,与它自己的结账同时进行
			rest := members[len(targets):]
			restDone := make(chan []Outcome, 1)
			var once sync.Once
			onFound := func() {
				once.Do(func() {
					go func() { restDone <- runGroupRound(ctx, state, rest, sessions, nil, purchase, busy, sem, nil) }()
				})
			}
			outcomes = runGroupRound(ctx, state, targets, sessions, signal, purchase, busy, sem, onFound)
			select {
			case more := <-restDone:
				outcomes, fannedOut = append(outcomes, more...), true
			default:
				// 回调可能在最后一刻触发:用 once 占位,确认没有启动后再继续
				started := true
				once.Do(func() { started = false })
				if started {
					outcomes, fannedOut = append(outcomes, <-restDone...), true
				}
			}
		} else {
			outcomes = runGroupRound(ctx, state, targets, sessions, signal, purchase, busy, sem, nil)
		}
		signal, runAll = nil, false

		stockSeen, retryNow := false, false
		for _, o := range outcomes {
			if o.Success || o.Attempted || o.PauseTask {
				stockSeen = true
			}
			if o.RetryImmediately {
				retryNow = true
			}
		}
		// 兜底:探测任务看到货但回调没触发(例如复用了库存信号),其余组员不等下一轮,立刻全部提交
		if stockSeen && !fannedOut && len(targets) < len(members) {
			runAll = true
			continue
		}
		if retryNow {
			continue
		}

		interval, waitingForStock := groupInterval(state, members, outcomes)
		timer := time.NewTimer(interval)
		waiting := true
		for waiting {
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				waiting = false
			case <-wake:
				// 新加/恢复的组员不用等满一个间隔才开始预装
				timer.Stop()
				waiting = false
			case observed := <-signals:
				if waitingForStock && stockSignalOrderable(observed, members[0]) {
					timer.Stop()
					if time.Since(observed.DetectedAt) < time.Second {
						signal = &observed
					}
					runAll = true
					waiting = false
				}
			}
		}
	}
}

// runGroupRound 并发跑一轮,并发数受 groupFanout 限制。每个任务独立的取消函数与购物车会话。
// sem 由同一轮的探测与扩散共用,总并发不超过 groupFanout;onFound 非空时注入给本轮任务。
func runGroupRound(ctx context.Context, state *app.State, targets []types.QueueItem, sessions map[string]*cartSession, signal *app.StockSignal, purchase func(context.Context, *app.State, *types.QueueItem) Outcome, busy *inFlight, sem chan struct{}, onFound func()) []Outcome {
	outcomes := make([]Outcome, len(targets))
	var wg sync.WaitGroup
	for i, member := range targets {
		session := sessions[member.ID]
		wg.Add(1)
		go func(i int, member types.QueueItem, session *cartSession) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			outcomes[i] = runMember(ctx, state, member, session, signal, purchase, busy, onFound)
		}(i, member, session)
	}
	wg.Wait()
	return outcomes
}

func runMember(ctx context.Context, state *app.State, member types.QueueItem, session *cartSession, signal *app.StockSignal, purchase func(context.Context, *app.State, *types.QueueItem) Outcome, busy *inFlight, onFound func()) Outcome {
	if !busy.acquire(member.ID) {
		// 上一组的尝试还没结束(可能正在结账),这一轮跳过它
		return Outcome{WaitingForStock: true, RetrySoon: time.Second}
	}
	defer busy.release(member.ID)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	account := stockWatchAccount(state, member.AccountID)
	state.QueueMu.Lock()
	var current *types.QueueItem
	for i := range state.Queue {
		if state.Queue[i].ID == member.ID {
			if state.Queue[i].AccountID == "" && account != "" {
				state.Queue[i].AccountID = account
			}
			copy := state.Queue[i]
			current = &copy
			break
		}
	}
	if current == nil || current.Status != "running" || state.IsTaskDeleted(member.ID) {
		state.QueueMu.Unlock()
		return Outcome{Cancelled: true}
	}
	// 暂停/删除/编辑通过它中断本轮(结账发出后不再接受取消,见 PurchaseServer)
	state.RegisterTaskCancel(member.ID, cancel)
	state.QueueMu.Unlock()
	defer state.UnregisterTaskCancel(member.ID)

	attemptCtx := context.WithValue(ctx, cartSessionKey{}, session)
	if signal != nil {
		attemptCtx = context.WithValue(attemptCtx, stockSignalKey{}, *signal)
	}
	if onFound != nil {
		attemptCtx = context.WithValue(attemptCtx, stockFoundKey{}, onFound)
	}
	return runQueueAttempt(attemptCtx, state, *current, purchase)
}

// touchWaitingMembers 没轮到探测的组员也更新检查时间:界面上的「下次」倒计时跟着组走,
// 不会看起来像卡住了。尝试次数只算真正发过请求的。
func touchWaitingMembers(state *app.State, members []types.QueueItem) {
	now := float64(time.Now().UnixNano()) / 1e9
	ids := map[string]bool{}
	for _, m := range members {
		ids[m.ID] = true
	}
	state.QueueMu.Lock()
	defer state.QueueMu.Unlock()
	for i := range state.Queue {
		it := &state.Queue[i]
		if ids[it.ID] && it.Status == "running" {
			// 只更新检查时间,不计尝试次数:这些任务本轮没有向 OVH 发请求
			it.LastCheckTime = now
			it.OrderPhase = "waitingStock"
		}
	}
}

// groupInterval 组的等待间隔:取组员里最短的重试间隔,再按本轮结果调整(与单任务规则一致)。
func groupInterval(state *app.State, members []types.QueueItem, outcomes []Outcome) (time.Duration, bool) {
	interval := time.Duration(0)
	for _, m := range members {
		d := time.Duration(types.ClampRetryInterval(m.RetryInterval, state.Config.RetryInterval())) * time.Second
		if interval == 0 || d < interval {
			interval = d
		}
	}
	waitingForStock := false
	var retryAfter, retrySoon time.Duration
	quota, catalogWait := false, false
	for _, o := range outcomes {
		if o.WaitingForStock {
			waitingForStock = true
		}
		quota = quota || o.WaitingForQuota
		catalogWait = catalogWait || o.WaitingForCatalog
		if o.RetryAfter > retryAfter {
			retryAfter = o.RetryAfter
		}
		if o.RetrySoon > 0 && (retrySoon == 0 || o.RetrySoon < retrySoon) {
			retrySoon = o.RetrySoon
		}
	}
	if catalogWait && interval > catalog.PreorderCheckInterval {
		interval = catalog.PreorderCheckInterval
	}
	if quota {
		interval = 2 * time.Second
	}
	if retryAfter > interval {
		interval = retryAfter
	}
	if retrySoon > 0 && retrySoon < interval {
		interval = retrySoon
	}
	if interval <= 0 {
		interval = time.Second
	}
	return interval, waitingForStock
}
