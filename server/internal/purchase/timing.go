package purchase

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

// 抢购耗时打点。
//
// 为什么需要:抢购输了之后,用户唯一能问的问题是"我慢在哪一步"。
// 在此之前日志只有一串"购物车创建成功""基础商品添加成功",没有一个时间数字 ——
// 于是"OVH 那边就是没货""我这台机器网络慢""某一步卡了 8 秒"这三种完全不同的情况
// 长得一模一样,而它们要采取的行动截然相反(换机型 / 换机器 / 改代码)。
//
// 打点只记墙钟耗时,不做任何判断。判断留给看的人 —— 这里给的是事实。

// phase 一个阶段的耗时
type phase struct {
	Name string
	Dur  time.Duration
}

// timeline 按顺序累积各阶段耗时。
// 不用加锁:PurchaseServer 是单 goroutine 顺序执行的。
type timeline struct {
	start time.Time
	last  time.Time
	items []phase
}

func newTimeline() *timeline {
	now := time.Now()
	return &timeline{start: now, last: now}
}

// mark 结束当前阶段。调用点就是这个阶段的结束点。
func (t *timeline) mark(name string) {
	if t == nil {
		return
	}
	now := time.Now()
	t.items = append(t.items, phase{Name: name, Dur: now.Sub(t.last)})
	t.last = now
}

// total 从开始到现在的总耗时
func (t *timeline) total() time.Duration {
	if t == nil {
		return 0
	}
	return time.Since(t.start)
}

// String 一行摘要,例如:
//
//	总 1832ms = 查库存 210ms + 建购物车 180ms + 绑定 90ms + 加购 940ms + 下单 412ms
//
// 各阶段之和会略小于总数(阶段之间还有本地计算),差值不刻意补齐 ——
// 硬凑一个"其它"字段只会让人以为那是某个真实步骤。
func (t *timeline) String() string {
	if t == nil || len(t.items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(t.items))
	for _, p := range t.items {
		parts = append(parts, fmt.Sprintf("%s %dms", p.Name, p.Dur.Milliseconds()))
	}
	return fmt.Sprintf("总 %dms = %s", t.total().Milliseconds(), strings.Join(parts, " + "))
}

// entries 转成可落库的结构
func (t *timeline) entries() []types.PhaseTiming {
	if t == nil || len(t.items) == 0 {
		return nil
	}
	out := make([]types.PhaseTiming, 0, len(t.items))
	for _, p := range t.items {
		out = append(out, types.PhaseTiming{Name: p.Name, Ms: p.Dur.Milliseconds()})
	}
	return out
}

// —— 最近一次抢购的耗时(按 planCode+机房 留一份,给前端"上次为什么慢"用)——
//
// 只留最近一次而不是全量:抢购一晚上能跑几万轮,全存下来是往数据库里灌日志。
// 真正值得回看的是"最后那次到底卡在哪"。

type lastTiming struct {
	At      string              `json:"at"`
	Total   int64               `json:"totalMs"`
	Phases  []types.PhaseTiming `json:"phases"`
	Outcome string              `json:"outcome"` // ordered / unavailable / failed
}

var (
	lastTimingMu sync.RWMutex
	lastTimings  = map[string]lastTiming{}
)

func recordTiming(key string, t *timeline, outcome string) {
	if t == nil || len(t.items) == 0 {
		return
	}
	lastTimingMu.Lock()
	defer lastTimingMu.Unlock()
	// 上限兜底:key 是 planCode+机房,用户可以订阅任意多个,不设限就是内存泄漏
	if len(lastTimings) > 500 {
		lastTimings = map[string]lastTiming{}
	}
	lastTimings[key] = lastTiming{
		At:      types.NowISO(),
		Total:   t.total().Milliseconds(),
		Phases:  t.entries(),
		Outcome: outcome,
	}
}

// TimingKey 打点的键:同一台机器在不同机房是两条独立的抢购链路
func TimingKey(planCode, datacenter string) string { return planCode + "@" + datacenter }

// LastTimings 最近一次各链路的耗时快照(给 /api/queue/timings)
func LastTimings() map[string]lastTiming {
	lastTimingMu.RLock()
	defer lastTimingMu.RUnlock()
	out := make(map[string]lastTiming, len(lastTimings))
	for k, v := range lastTimings {
		out[k] = v
	}
	return out
}

// recordTimingToHistory 把这一单的阶段耗时写进历史记录。
// 独立成一个函数而不是塞进 recordSuccess:成功路径上 recordSuccess 有两条分支
// (更新已有条目 / 新建条目),在两处各写一遍迟早会漏一处。
func recordTimingToHistory(state *app.State, taskID string, t *timeline) {
	if t == nil || len(t.items) == 0 {
		return
	}
	state.HistoryMu.Lock()
	defer state.HistoryMu.Unlock()
	for i := range state.History {
		if state.History[i].TaskID == taskID {
			state.History[i].Timing = t.entries()
			state.History[i].TotalMs = t.total().Milliseconds()
			go state.SaveHistory()
			return
		}
	}
}
