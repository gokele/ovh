// Package proxyguard 盯着各账户的出站代理，挂了就停掉那个账户的活儿并告诉用户。
//
// 为什么不能只是"记一条日志就算了"：
// 配代理的目的就是让每个账户从各自的出口出去。代理一断，如果程序还在
// 若无其事地重试，用户看到的是"一直抢不到"，而真正的原因（出口断了）
// 只躺在日志里。抢购是有时间窗口的，等他翻日志的时候机器早被买走了。
//
// 所以这里做三件事，缺一不可：
//  1. 把该账户进行中的任务**暂停**（不是删除 —— 代理修好要能接着抢）
//  2. 把该账户的订阅**关掉自动下单**（留着订阅本身，通知照常）
//  3. 发 Telegram / Webhook 通知，说清楚是哪个账户、哪个代理、怎么恢复
package proxyguard

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/netfp"
	"github.com/ovh-buy/server/internal/notify"
	"github.com/ovh-buy/server/internal/types"
)

const (
	// failThreshold 连续多少次代理失败才动手。
	//
	// 不能一次就停：代理偶尔抖一下很常见，而停任务的代价是错过补货窗口。
	// 也不能太宽：连续失败意味着这个账户此刻**一单都下不出去**，
	// 拖着只是让用户以为还在抢。
	failThreshold = 3

	// failWindow 计数窗口。超过这么久没再失败就清零 ——
	// 一天里偶发三次不该被当成"代理挂了"。
	failWindow = 2 * time.Minute

	// notifyCooldown 同一账户两次告警的最小间隔，避免刷屏。
	notifyCooldown = 30 * time.Minute
)

type accountState struct {
	fails      int
	lastFail   time.Time
	trippedAt  time.Time
	lastNotify time.Time
}

// Guard 代理健康看门狗。
type Guard struct {
	state *app.State

	mu  sync.Mutex
	acc map[string]*accountState

	// reload 让 Monitor 从库里重读订阅。由 main 注入 ——
	// 改了库但内存里的 Monitor 还拿着旧值,自动下单会继续触发。
	reload func()
	// setDisabled 带 Monitor 锁的"关掉某账户全部订阅自动下单"入口,main 注入。
	// 以前绕过它裸改库,监控轮末整表落库会把 AutoOrder=true 写回去(竞态)
	setDisabled func(accountID string) int
}

var global *Guard

// Init 在 main 里调用一次。
func Init(state *app.State) *Guard {
	global = &Guard{state: state, acc: map[string]*accountState{}}
	return global
}

// Report 由出站 transport 在代理失败时回调。accountID 为空则忽略。
func Report(accountID string, err error) {
	if global != nil {
		global.Report(accountID, err)
	}
}

// ReportSuccess 一次成功的出站请求。用来清零计数 —— 代理恢复了就别再记着旧账。
func ReportSuccess(accountID string) {
	if global != nil {
		global.ReportSuccess(accountID)
	}
}

// Tripped 这个账户当前是不是因为代理故障被停了。
func Tripped(accountID string) bool {
	if global == nil {
		return false
	}
	global.mu.Lock()
	defer global.mu.Unlock()
	st, ok := global.acc[accountID]
	return ok && !st.trippedAt.IsZero()
}

// Status 给界面看的快照：账户 ID → 是否跳闸 / 连续失败次数。
func Status() map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	if global == nil {
		return out
	}
	global.mu.Lock()
	defer global.mu.Unlock()
	for id, st := range global.acc {
		e := map[string]interface{}{"fails": st.fails, "tripped": !st.trippedAt.IsZero()}
		if !st.trippedAt.IsZero() {
			e["trippedAt"] = st.trippedAt.Format(time.RFC3339)
		}
		if !st.lastFail.IsZero() {
			e["lastFailAt"] = st.lastFail.Format(time.RFC3339)
		}
		out[id] = e
	}
	return out
}

func (g *Guard) ReportSuccess(accountID string) {
	if accountID == "" {
		return
	}
	g.mu.Lock()
	st, ok := g.acc[accountID]
	if !ok || (st.fails == 0 && st.trippedAt.IsZero()) {
		g.mu.Unlock()
		return
	}
	wasTripped := !st.trippedAt.IsZero()
	down := time.Duration(0)
	if wasTripped {
		down = time.Since(st.trippedAt).Round(time.Second)
	}
	st.fails = 0
	st.trippedAt = time.Time{}
	g.mu.Unlock()

	if !wasTripped {
		return
	}
	// 通了就说一声。但任务**不自动恢复**：用户可能已经手动处理过了，
	// 替他改回去反而制造意外 —— 尤其"恢复"意味着重新开始花钱。
	acc, _ := g.state.FindAccount(accountID)
	g.state.Logger.Info(fmt.Sprintf("账户 %s 的出站代理已恢复(中断约 %s)", accLabel(acc), down), "proxy")
	notify.Broadcast(g.state, fmt.Sprintf(
		"✅ 出站代理已恢复\n\n账户: %s\n中断约: %s\n\n"+
			"被暂停的抢购任务**没有**自动恢复 —— 确认代理稳定后发 /queue 查看，\n"+
			"在控制台把它们改回运行，或者重新下单。",
		accLabel(acc), down), nil)
}

func (g *Guard) Report(accountID string, err error) {
	if accountID == "" || !netfp.IsProxyError(err) {
		return
	}
	g.mu.Lock()
	st, ok := g.acc[accountID]
	if !ok {
		st = &accountState{}
		g.acc[accountID] = st
	}
	now := time.Now()
	// 窗口外的旧账清零
	if !st.lastFail.IsZero() && now.Sub(st.lastFail) > failWindow {
		st.fails = 0
	}
	st.fails++
	st.lastFail = now
	shouldTrip := st.fails >= failThreshold && st.trippedAt.IsZero()
	remind := !st.trippedAt.IsZero() && now.Sub(st.lastNotify) > notifyCooldown
	if shouldTrip {
		st.trippedAt = now
	}
	if shouldTrip || remind {
		st.lastNotify = now
	}
	fails := st.fails
	g.mu.Unlock()

	switch {
	case shouldTrip:
		g.trip(accountID, err)
	case remind:
		acc, _ := g.state.FindAccount(accountID)
		g.state.Logger.Warn(fmt.Sprintf("账户 %s 出站代理仍不可用: %s", accLabel(acc), err.Error()), "proxy")
	default:
		acc, _ := g.state.FindAccount(accountID)
		g.state.Logger.Warn(fmt.Sprintf("账户 %s 出站代理失败(第 %d 次): %s",
			accLabel(acc), fails, err.Error()), "proxy")
	}
}

// trip 停活儿 + 通知。
func (g *Guard) trip(accountID string, cause error) {
	acc, _ := g.state.FindAccount(accountID)
	label := accLabel(acc)
	g.state.Logger.Error(fmt.Sprintf(
		"账户 %s 的出站代理连续 %d 次不可用，已暂停该账户的抢购任务: %s",
		label, failThreshold, cause.Error()), "proxy")

	paused := g.pauseQueue(accountID)
	subs := g.disableAutoOrder(accountID)

	var b strings.Builder
	b.WriteString("🚨 出站代理不可用，已停下这个账户的活儿\n\n")
	b.WriteString("账户: " + label + "\n")
	b.WriteString("代理: " + netfp.ScrubProxyURL(acc.ProxyURL) + "\n")
	b.WriteString("原因: " + cause.Error() + "\n\n")
	if paused > 0 {
		b.WriteString(fmt.Sprintf("已暂停 %d 个抢购任务（没有删除，修好后可以接着抢）\n", paused))
	}
	if subs > 0 {
		b.WriteString(fmt.Sprintf("已关掉 %d 条订阅的自动下单（订阅还在，补货通知照常发）\n", subs))
	}
	if paused == 0 && subs == 0 {
		b.WriteString("这个账户当前没有在跑的任务。\n")
	}
	b.WriteString("\n为什么要停：代理断了等于这个账户的出口没了，" +
		"继续重试只会一直失败，而你看到的现象只是「一直抢不到」。\n\n")
	b.WriteString("修好代理后：发 /queue 看被暂停的任务，在控制台改回运行；" +
		"订阅的自动下单需要重新打开。")
	notify.Broadcast(g.state, b.String(), nil)
}

// pauseQueue 暂停该账户所有进行中的任务。
//
// 用 paused 而不是 failed：代理是能修好的，任务不该因为出口断了就被判死。
func (g *Guard) pauseQueue(accountID string) int {
	g.state.QueueMu.Lock()
	n := 0
	for i := range g.state.Queue {
		it := &g.state.Queue[i]
		if it.AccountID != accountID {
			continue
		}
		if it.Status != "running" && it.Status != "pending" {
			continue
		}
		it.Status = "paused"
		it.UpdatedAt = types.NowISO()
		n++
	}
	g.state.QueueMu.Unlock()
	if n > 0 {
		if err := g.state.SaveQueue(); err != nil {
			// 落库失败必须说：不说的话重启后任务会带着 running 状态复活，
			// 顶着一个断掉的代理继续刷
			g.state.Logger.Error("暂停任务后保存队列失败: "+err.Error(), "proxy")
		}
	}
	return n
}

// disableAutoOrder 关掉该账户订阅的自动下单。
//
// 只关自动下单，不删订阅：补货通知照常发，用户还能手动点按钮
// （手动那一下会用他当时选的账户，未必是这个坏掉的）。
//
// 内存里的 Monitor 还拿着旧值，所以这里只改库 + 记一笔，
// 由 main 注入的 reload 回调去让它生效（monitor 包 import 不进来）。
func (g *Guard) disableAutoOrder(accountID string) int {
	// 走 Monitor 的带锁 API,不再绕过内存态直接改库 ——
	// 以前"读全表→改→写回"全程不持监控的锁,监控循环轮末的整表落库
	// 用内存里的 AutoOrder=true 覆盖回去,代理挂了但自动下单照常触发
	g.mu.Lock()
	fn := g.setDisabled
	g.mu.Unlock()
	if fn == nil {
		g.state.Logger.Warn("Monitor 带锁变更入口未注入,无法安全关闭自动下单(跳过而不是裸改库)", "proxy")
		return 0
	}
	return fn(accountID)
}

// SetDisableAutoOrder 注入带 Monitor 锁的禁用入口。
func SetDisableAutoOrder(fn func(accountID string) int) {
	if global != nil {
		global.mu.Lock()
		global.setDisabled = fn
		global.mu.Unlock()
	}
}

// SetReload 注入"让 Monitor 重读订阅"的回调。
func SetReload(fn func()) {
	if global != nil {
		global.mu.Lock()
		global.reload = fn
		global.mu.Unlock()
	}
}

func accLabel(a types.OVHAccount) string {
	zone := strings.ToUpper(strings.TrimSpace(a.Zone))
	if a.Name == "" {
		return a.ID
	}
	if zone == "" {
		return a.Name
	}
	return a.Name + "（" + zone + "）"
}
