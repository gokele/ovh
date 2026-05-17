package monitor

import (
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
)

// Monitor 对应 Python: ServerMonitor 类
type Monitor struct {
	state *app.State

	subsMu        sync.Mutex
	subscriptions []*Subscription
	knownServers  map[string]struct{}

	running       bool
	checkInterval int // 全局固定 5 秒
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
type Subscription struct {
	PlanCode           string                 `json:"planCode"`
	Datacenters        []string               `json:"datacenters"`
	NotifyAvailable    bool                   `json:"notifyAvailable"`
	NotifyUnavailable  bool                   `json:"notifyUnavailable"`
	LastStatus         map[string]string      `json:"lastStatus"`
	CreatedAt          string                 `json:"createdAt"`
	History            []HistoryEntry         `json:"history"`
	ServerName         string                 `json:"serverName,omitempty"`
	AutoOrder          bool                   `json:"autoOrder,omitempty"`
	Quantity           int                    `json:"quantity,omitempty"`
	AutoOrderAccountID string                 `json:"autoOrderAccountId,omitempty"` // 空 = 触发时只通知不下单
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

// Snapshot 返回订阅列表副本（JSON 用），永不返回 nil
func (m *Monitor) Snapshot() []*Subscription {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	cp := make([]*Subscription, 0, len(m.subscriptions))
	for _, s := range m.subscriptions {
		// 保证 sub.History、sub.Datacenters、sub.LastStatus 均不为 nil
		if s.History == nil {
			s.History = []HistoryEntry{}
		}
		if s.Datacenters == nil {
			s.Datacenters = []string{}
		}
		if s.LastStatus == nil {
			s.LastStatus = map[string]string{}
		}
		cp = append(cp, s)
	}
	return cp
}

// Status 对应 Python: get_status
func (m *Monitor) Status() map[string]interface{} {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	subs := make([]*Subscription, len(m.subscriptions))
	for i, s := range m.subscriptions {
		// 与 Snapshot 保持一致：保证 sub 内 slice/map 字段不是 nil，
		// 避免前端调 .length 时报错
		if s.History == nil {
			s.History = []HistoryEntry{}
		}
		if s.Datacenters == nil {
			s.Datacenters = []string{}
		}
		if s.LastStatus == nil {
			s.LastStatus = map[string]string{}
		}
		subs[i] = s
	}
	return map[string]interface{}{
		"running":             m.running,
		"subscriptions_count": len(m.subscriptions),
		"known_servers_count": len(m.knownServers),
		"check_interval":      m.checkInterval,
		"subscriptions":       subs,
	}
}

// Running 监控是否在运行
func (m *Monitor) Running() bool {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	return m.running
}

// SetCheckInterval 已禁用，全局固定 5
func (m *Monitor) SetCheckInterval(_ int) {
	m.subsMu.Lock()
	m.checkInterval = 5
	m.subsMu.Unlock()
	m.state.Logger.Info("检查间隔已全局固定为5秒，无法修改", "monitor")
}

// nowBeijing 返回北京时间
func (m *Monitor) nowBeijing() time.Time {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Now().UTC().Add(8 * time.Hour)
	}
	return time.Now().In(loc)
}

func (m *Monitor) limitHistorySize(sub *Subscription, maxSize int) {
	if len(sub.History) > maxSize {
		sub.History = sub.History[len(sub.History)-maxSize:]
	}
}
