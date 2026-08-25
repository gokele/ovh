package monitor

import (
	"fmt"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
)

// Monitor 服务器补货监控器
type Monitor struct {
	state *app.State

	subsMu        sync.Mutex
	subscriptions []*Subscription
	knownServers  map[string]struct{}

	running       bool
	checkInterval int // 检查间隔(秒),可配,见 SetCheckInterval
	thread        *sync.WaitGroup
	maxWorkers    int

	// Options 缓存（旧机制，兼容性保留）
	optionsCache    map[string]*CachedOptions
	optionsCacheTTL time.Duration

	// UUID 消息缓存
	messageUUIDCache    map[string]*CachedMessage
	messageUUIDCacheTTL time.Duration

	cacheLock sync.Mutex

	// TG 健康检查时间戳:loop 每 5 分钟 verify 一次,失败就自停。
	// 不放 subsMu 下,简单用单独的锁。
	tgCheckMu   sync.Mutex
	lastTGCheck time.Time
}

type CachedOptions struct {
	Options   []string `json:"options"`
	Timestamp float64  `json:"timestamp"`
}

type CachedMessage struct {
	PlanCode   string                 `json:"planCode"`
	Datacenter string                 `json:"datacenter"`
	Options    []string               `json:"options"`
	ConfigInfo map[string]interface{} `json:"configInfo"`
	Timestamp  float64                `json:"timestamp"`
}

// Subscription 订阅条目(monitor 包内部用,落 SQLite 时转 types.Subscription)
//
// 并发约定:
//   - PlanCode / CreatedAt 创建后不再修改,可以不持锁读;
//   - 其余所有字段(通知开关、AutoOrder*、LastStatus、History、LastCheck*)一律由 mu 保护,
//     只能通过本文件里的带锁方法读写。
//
// 为什么不能只靠 Monitor.subsMu:loop 每轮从 m.subscriptions 复制出去的是 []*Subscription,
// 复制的只是指针。subsMu 保护得了切片本身,保护不了指针指向的结构体 ——
// 检查 goroutine 在锁外写 LastCheck*/History/LastStatus,而 HTTP 侧的 Status()/Snapshot()
// 同时在读同一批字段,go test -race 会直接报 DATA RACE(见 race_test.go)。
type Subscription struct {
	// mu 保护下面所有可变字段。加在结构体里而不是复用 subsMu:
	// 检查是「每订阅一个 goroutine」并发跑的,用一把全局锁会让 30 个订阅的检查串行化。
	mu sync.Mutex

	PlanCode           string            `json:"planCode"`
	Datacenters        []string          `json:"datacenters"`
	NotifyAvailable    bool              `json:"notifyAvailable"`
	NotifyUnavailable  bool              `json:"notifyUnavailable"`
	LastStatus         map[string]string `json:"lastStatus"`
	CreatedAt          string            `json:"createdAt"`
	History            []HistoryEntry    `json:"history"`
	ServerName         string            `json:"serverName,omitempty"`
	AutoOrder          bool              `json:"autoOrder,omitempty"`
	Quantity           int               `json:"quantity,omitempty"`
	AutoOrderAccountID string            `json:"autoOrderAccountId,omitempty"` // 空 = 触发时只通知不下单

	// —— 本轮可用性查询的诊断信息 ——
	// 只存内存、不落库(每轮检查都会重算,持久化没有意义)。
	// 存在的理由:EU / US / CA 三个站点的库存视图彼此独立,拿错站点查 planCode
	// OVH 返回的是 HTTP 200 + 空数组而不是错误。没有这几个字段的时候,
	// "订阅了美区机型但落到欧区账户去查"和"这台机器确实没货"在前端长得一模一样,
	// 监控可以静默失效好几个月都没人发现。
	LastCheckAt         string `json:"lastCheckAt,omitempty"`         // 最近一次查询时间
	LastCheckAccountID  string `json:"lastCheckAccountId,omitempty"`  // 实际用来查库存的账户
	LastCheckRegion     string `json:"lastCheckRegion,omitempty"`     // 该账户所属大区 EU / US / CA
	LastCheckSubsidiary string `json:"lastCheckSubsidiary,omitempty"` // 该账户的 ovhSubsidiary
	LastCheckError      string `json:"lastCheckError,omitempty"`      // 非空 = 本轮查询存在的问题(区域错配 / 拿不到数据),中文
}

// —— Subscription 的带锁访问器 ——
// 检查 goroutine 与 HTTP 侧(Status/Snapshot/SaveToDB)并发访问同一个 *Subscription,
// 所有可变字段的读写都必须走这里,不允许再在包内直接 sub.X = ...。

// subCheckConfig 一轮检查开始时取的配置快照。
// 取快照而不是边跑边读:一次检查要跑几十秒(询价 30s 超时),
// 期间用户可能改订阅配置,半程换配置会让状态机前后不一致(比如前半程按"通知有货"、
// 后半程按"不通知"走),而且每次读都要加锁。
type subCheckConfig struct {
	Datacenters        []string
	NotifyAvailable    bool
	NotifyUnavailable  bool
	ServerName         string
	AutoOrder          bool
	Quantity           int
	AutoOrderAccountID string
}

func (s *Subscription) checkConfig() subCheckConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	dcs := make([]string, len(s.Datacenters))
	copy(dcs, s.Datacenters)
	return subCheckConfig{
		Datacenters:        dcs,
		NotifyAvailable:    s.NotifyAvailable,
		NotifyUnavailable:  s.NotifyUnavailable,
		ServerName:         s.ServerName,
		AutoOrder:          s.AutoOrder,
		Quantity:           s.Quantity,
		AutoOrderAccountID: s.AutoOrderAccountID,
	}
}

// beginCheck 记录本轮用的查询账户,清空上轮的错误,并返回上轮的错误原因。
// 返回上轮原因是为了给日志去重:检查 5 秒一轮,同一条区域错配每轮 Warn 一次
// 一天能刷一万七千条重复日志,真正的新问题会被埋掉。
func (s *Subscription) beginCheck(at, accountID, region, subsidiary string) (prevErr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prevErr = s.LastCheckError
	s.LastCheckAt = at
	s.LastCheckAccountID = accountID
	s.LastCheckRegion = region
	s.LastCheckSubsidiary = subsidiary
	s.LastCheckError = ""
	return prevErr
}

func (s *Subscription) setCheckError(msg string) {
	s.mu.Lock()
	s.LastCheckError = msg
	s.mu.Unlock()
}

// statusSnapshot 取 LastStatus 的副本;检查过程中改的是副本,
// 最后用 replaceLastStatus 一次性写回,避免把半成品状态暴露给 HTTP 侧。
func (s *Subscription) statusSnapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.LastStatus))
	for k, v := range s.LastStatus {
		out[k] = v
	}
	return out
}

func (s *Subscription) replaceLastStatus(m map[string]string) {
	if m == nil {
		m = map[string]string{}
	}
	s.mu.Lock()
	s.LastStatus = m
	s.mu.Unlock()
}

// appendHistory 追加历史并就地裁剪到 maxSize 条(旧的先丢)。
func (s *Subscription) appendHistory(entries []HistoryEntry, maxSize int) {
	if len(entries) == 0 {
		return
	}
	s.mu.Lock()
	s.History = append(s.History, entries...)
	if maxSize > 0 && len(s.History) > maxSize {
		s.History = s.History[len(s.History)-maxSize:]
	}
	s.mu.Unlock()
}

// historySnapshot 历史记录副本(calcDuration / 落库 / JSON 输出用)。
// HistoryEntry.Config 是创建后就不再改的 map,浅拷贝即可。
func (s *Subscription) historySnapshot() []HistoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]HistoryEntry, len(s.History))
	copy(out, s.History)
	return out
}

// snapshot 深拷贝一份订阅,供 JSON 输出 / 落库使用。
// 必须逐字段拷(不能 *s):结构体里带 sync.Mutex,整体复制会被 go vet copylocks 挡下,
// 而且复制一把已被别的 goroutine 持有的锁本身就是错的。
func (s *Subscription) snapshot() *Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	dcs := make([]string, len(s.Datacenters))
	copy(dcs, s.Datacenters)
	last := make(map[string]string, len(s.LastStatus))
	for k, v := range s.LastStatus {
		last[k] = v
	}
	hist := make([]HistoryEntry, len(s.History))
	copy(hist, s.History)
	return &Subscription{
		PlanCode:           s.PlanCode,
		Datacenters:        dcs,
		NotifyAvailable:    s.NotifyAvailable,
		NotifyUnavailable:  s.NotifyUnavailable,
		LastStatus:         last,
		CreatedAt:          s.CreatedAt,
		History:            hist,
		ServerName:         s.ServerName,
		AutoOrder:          s.AutoOrder,
		Quantity:           s.Quantity,
		AutoOrderAccountID: s.AutoOrderAccountID,

		LastCheckAt:         s.LastCheckAt,
		LastCheckAccountID:  s.LastCheckAccountID,
		LastCheckRegion:     s.LastCheckRegion,
		LastCheckSubsidiary: s.LastCheckSubsidiary,
		LastCheckError:      s.LastCheckError,
	}
}

// HistoryEntry 历史记录条目
type HistoryEntry struct {
	Timestamp  string                 `json:"timestamp"`
	Datacenter string                 `json:"datacenter"`
	Status     string                 `json:"status"`
	ChangeType string                 `json:"changeType"`
	OldStatus  interface{}            `json:"oldStatus"`
	Config     map[string]interface{} `json:"config,omitempty"`
}

// New 创建监控器
func New(state *app.State) *Monitor {
	return &Monitor{
		state:               state,
		subscriptions:       []*Subscription{},
		knownServers:        map[string]struct{}{},
		checkInterval:       5,
		maxWorkers:          4,
		optionsCache:        map[string]*CachedOptions{},
		optionsCacheTTL:     24 * time.Hour,
		messageUUIDCache:    map[string]*CachedMessage{},
		messageUUIDCacheTTL: 24 * time.Hour,
	}
}

// Snapshot 返回订阅列表副本（JSON 用），永不返回 nil。
// 返回的是**深拷贝**:调用方(handler 里的 json.Marshal、main 里的计数)拿到的是冻结值,
// 检查 goroutine 同时在改真身也不会读到撕裂的数据。
// 反过来说,改 Snapshot 返回的对象不会影响真实订阅 —— 要改订阅走 AddSubscription。
func (m *Monitor) Snapshot() []*Subscription {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	cp := make([]*Subscription, 0, len(m.subscriptions))
	for _, s := range m.subscriptions {
		// snapshot 里 History / Datacenters / LastStatus 一律是非 nil 的副本,
		// 前端可以直接 .length,不用判空
		cp = append(cp, s.snapshot())
	}
	return cp
}

func (m *Monitor) Status() map[string]interface{} {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	subs := make([]*Subscription, len(m.subscriptions))
	for i, s := range m.subscriptions {
		subs[i] = s.snapshot()
	}
	// region_issues:本轮查不到库存的订阅 + 原因。
	// 单独拎出来是因为「查错大区」的表现是 OVH 200 + 空数组,前端从订阅列表上
	// 完全看不出异常(状态一直是无货),必须有个显式的位置告诉用户监控已经失效了。
	// 用上面已经拷好的 subs 而不是再读一遍真身:同一次响应里两处数据必须自洽。
	issues := []map[string]interface{}{}
	for _, s := range subs {
		if s.LastCheckError == "" {
			continue
		}
		issues = append(issues, map[string]interface{}{
			"planCode":   s.PlanCode,
			"serverName": s.ServerName,
			"accountId":  s.LastCheckAccountID,
			"region":     s.LastCheckRegion,
			"subsidiary": s.LastCheckSubsidiary,
			"checkedAt":  s.LastCheckAt,
			"error":      s.LastCheckError,
		})
	}
	return map[string]interface{}{
		"running":             m.running,
		"subscriptions_count": len(m.subscriptions),
		"known_servers_count": len(m.knownServers),
		"check_interval":      m.checkInterval,
		"subscriptions":       subs,
		"region_issues":       issues,
	}
}

// Running 监控是否在运行
func (m *Monitor) Running() bool {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	return m.running
}

// MinCheckInterval / MaxCheckInterval 检查间隔的合法区间。
// 下限 5 秒:再快没有意义(OVH 可用性接口本身有缓存),而且多订阅并发会撞 OVH 限流;
// 上限 1 小时:比这更慢的话补货基本抢不到,不如关掉监控。
const (
	MinCheckInterval = 5
	MaxCheckInterval = 3600
)

// ClampCheckInterval 把用户输入夹到合法区间
func ClampCheckInterval(v int) int {
	if v < MinCheckInterval {
		return MinCheckInterval
	}
	if v > MaxCheckInterval {
		return MaxCheckInterval
	}
	return v
}

// SetCheckInterval 设置检查间隔(秒)。会被夹到 [MinCheckInterval, MaxCheckInterval]。
// 正在运行的 loop 每轮结束都会重新读这个值,所以改完立即生效,不用重启监控。
func (m *Monitor) SetCheckInterval(v int) int {
	v = ClampCheckInterval(v)
	m.subsMu.Lock()
	m.checkInterval = v
	m.subsMu.Unlock()
	m.state.Logger.Info(fmt.Sprintf("监控检查间隔已设置为 %d 秒", v), "monitor")
	return v
}

// CheckInterval 当前间隔
func (m *Monitor) CheckInterval() int {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	return m.checkInterval
}

// nowBeijing 返回北京时间
func (m *Monitor) nowBeijing() time.Time {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Now().UTC().Add(8 * time.Hour)
	}
	return time.Now().In(loc)
}

// maxHistorySize 单条订阅保留的历史条数上限。
// 裁剪已经并进 (*Subscription).appendHistory —— 以前那个不带锁的 limitHistorySize
// 是在检查 goroutine 里直接改 sub.History 的,正是本轮要消掉的竞争源。
const maxHistorySize = 100
