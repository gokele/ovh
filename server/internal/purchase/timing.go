package purchase

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
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
	start       time.Time
	last        time.Time
	items       []phase
	dispatch    *ovh.DispatchSnapshot
	dispatchKey string
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
	At            string                `json:"at"`
	Total         int64                 `json:"totalMs"`
	Phases        []types.PhaseTiming   `json:"phases"`
	Dispatch      *ovh.DispatchSnapshot `json:"dispatch,omitempty"`
	DispatchStats *dispatchStats        `json:"dispatchStats,omitempty"`
	dispatchKey   string
	Outcome       string `json:"outcome"` // ordered / unavailable / failed
}

var (
	lastTimingMu    sync.RWMutex
	lastTimings     = map[string]lastTiming{}
	dispatchSamples = map[string][]int64{}
)

type dispatchStats struct {
	Samples int   `json:"samples"`
	P50US   int64 `json:"p50Us"`
	P99US   int64 `json:"p99Us"`
}

func summarizeDispatch(samples []int64) *dispatchStats {
	if len(samples) == 0 {
		return nil
	}
	ordered := append([]int64(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	return &dispatchStats{Samples: len(ordered), P50US: ordered[(len(ordered)*50+99)/100-1], P99US: ordered[(len(ordered)*99+99)/100-1]}
}

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
	if t.dispatchKey != "" && t.dispatch != nil && t.dispatch.WroteRequest {
		if _, exists := dispatchSamples[t.dispatchKey]; !exists && len(dispatchSamples) >= 500 {
			for old := range dispatchSamples {
				delete(dispatchSamples, old)
				break
			}
		}
		samples := dispatchSamples[t.dispatchKey]
		if len(samples) >= 256 {
			copy(samples, samples[1:])
			samples = samples[:255]
		}
		dispatchSamples[t.dispatchKey] = append(samples, t.dispatch.DetectionToWriteUS)
	}
	lastTimings[key] = lastTiming{
		At:          types.NowISO(),
		Total:       t.total().Milliseconds(),
		Phases:      t.entries(),
		Outcome:     outcome,
		Dispatch:    t.dispatch,
		dispatchKey: t.dispatchKey,
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
		v.DispatchStats = summarizeDispatch(dispatchSamples[v.dispatchKey])
		out[k] = v
	}
	return out
}

// recordTimingToHistory 把这一单的阶段耗时写进历史记录。
// 独立成一个函数而不是塞进 recordSuccess:成功路径上 recordSuccess 有两条分支
// (更新已有条目 / 新建条目),在两处各写一遍迟早会漏一处。
func recordTimingToHistory(state *app.State, taskID, createdAt string, t *timeline) {
	if t == nil || len(t.items) == 0 {
		return
	}
	state.HistoryMu.Lock()
	defer func() {
		state.HistoryMu.Unlock()
		_ = state.SaveHistory() // 同步:耗时数据丢了不影响下单,但排查慢在哪全靠它
	}()
	for i := range state.History {
		if state.History[i].TaskID == taskID {
			state.History[i].Timing = t.entries()
			state.History[i].TotalMs = t.total().Milliseconds()
			if d := t.dispatch; d != nil && d.WroteRequest {
				state.History[i].DetectionToWriteUs = d.DetectionToWriteUS
				state.History[i].Prewarmed = d.Prewarmed
				state.History[i].ConnectionReused = d.ConnectionReused
			}
			if created, ok := types.ParseTS(createdAt); ok {
				state.History[i].WaitMs = time.Since(created).Milliseconds()
			}
			return
		}
	}
}

// orderTimingNote 成功通知里的「抢购耗时」段落:发现库存到发出结账、本轮链路、从建任务到抢到。
// 只写确实测到的数字;没有写出记录或没有创建时间的项直接省略。
func orderTimingNote(t *timeline, createdAt string) string {
	if t == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("⏱ 抢购耗时\n")
	if d := t.dispatch; d != nil && d.WroteRequest {
		b.WriteString("发现库存 → 发出结账：" + formatMicros(d.DetectionToWriteUS))
		var notes []string
		if d.Prewarmed {
			notes = append(notes, "购物车已预装")
		}
		if d.ConnectionReused {
			notes = append(notes, "复用连接")
		}
		if len(notes) > 0 {
			b.WriteString("（" + strings.Join(notes, "，") + "）")
		}
		b.WriteString("\n")
	}
	// 截止到最后一个阶段(下单返回)为止,不把之后的落库和通知时间算进去
	if total := t.last.Sub(t.start); total > 0 {
		b.WriteString(fmt.Sprintf("本轮链路：%dms", total.Milliseconds()))
		if len(t.items) > 0 {
			parts := make([]string, 0, len(t.items))
			for _, p := range t.items {
				parts = append(parts, fmt.Sprintf("%s %dms", p.Name, p.Dur.Milliseconds()))
			}
			b.WriteString("（" + strings.Join(parts, " · ") + "）")
		}
		b.WriteString("\n")
	}
	if created, ok := types.ParseTS(createdAt); ok {
		b.WriteString("从创建任务到抢到：" + formatWait(time.Since(created)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatMicros 亚毫秒保留两位小数,其余按毫秒显示。
func formatMicros(us int64) string {
	if us < 1000 {
		return fmt.Sprintf("%.2fms", float64(us)/1000)
	}
	return fmt.Sprintf("%dms", us/1000)
}

// formatWait 等待时长:秒 / 分秒 / 时分 / 天时。
func formatWait(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d 分 %d 秒", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时 %d 分", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%d 天 %d 小时", int(d.Hours())/24, int(d.Hours())%24)
	}
}
