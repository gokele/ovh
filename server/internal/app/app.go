package app

import (
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/types"
)

// ServerListCache 服务器列表内存缓存
type ServerListCache struct {
	mu        sync.RWMutex
	Data      []types.ServerPlan
	Timestamp *time.Time
	TTL       time.Duration
}

// NewServerListCache 默认 2 小时 TTL（懒加载：仅访问触发刷新，无后台定时器）
func NewServerListCache() *ServerListCache {
	return &ServerListCache{TTL: 2 * time.Hour}
}

// Get 返回缓存副本和是否有效
func (s *ServerListCache) Get() ([]types.ServerPlan, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Timestamp == nil {
		return nil, false
	}
	valid := time.Since(*s.Timestamp) < s.TTL
	cp := make([]types.ServerPlan, len(s.Data))
	copy(cp, s.Data)
	return cp, valid
}

// Set 更新缓存，时间戳=NOW
func (s *ServerListCache) Set(data []types.ServerPlan) {
	s.SetAt(data, time.Now())
}

// SetAt 用指定时间戳更新缓存。
// 启动时从 SQLite 回灌历史数据要用这个，保留真实的 updated_at，
// 否则旧数据被当作刚拉的，过期判断会出错。
func (s *ServerListCache) SetAt(data []types.ServerPlan, ts time.Time) {
	s.mu.Lock()
	s.Data = data
	s.Timestamp = &ts
	s.mu.Unlock()
}

// State 聚合所有共享运行状态
type State struct {
	Paths       storage.Paths
	Config      *config.Store
	OVH         *ovh.Factory
	Logger      *logger.Logger
	ServerCache *ServerListCache
	DB          *db.DB // SQLite 持久化层

	APIKey string
	Port   string

	QueueMu sync.Mutex
	Queue   []types.QueueItem

	HistoryMu sync.Mutex
	History   []types.PurchaseHistoryEntry

	ServerPlansMu sync.RWMutex
	ServerPlans   []types.ServerPlan

	DeletedTaskIDsMu sync.Mutex
	DeletedTaskIDs   map[string]struct{}

	ConfigSniperMu    sync.Mutex
	ConfigSniperTasks []types.ConfigSniperTask

	VPSSubsMu        sync.Mutex
	VPSSubscriptions []types.VPSSubscription
	VPSCheckInterval int

	MonitorRunning        bool
	QueueProcessorRunning bool
}

// NewState 构造应用状态。DB 必须已 Open。
func NewState(paths storage.Paths, cfg *config.Store, lg *logger.Logger, sqliteDB *db.DB) *State {
	return &State{
		Paths:                 paths,
		Config:                cfg,
		Logger:                lg,
		OVH:                   ovh.NewFactory(cfg),
		ServerCache:           NewServerListCache(),
		DB:                    sqliteDB,
		DeletedTaskIDs:        make(map[string]struct{}),
		Queue:                 []types.QueueItem{},
		History:               []types.PurchaseHistoryEntry{},
		ServerPlans:           []types.ServerPlan{},
		ConfigSniperTasks:     []types.ConfigSniperTask{},
		VPSSubscriptions:      []types.VPSSubscription{},
		VPSCheckInterval:      60,
		QueueProcessorRunning: true,
	}
}

// LoadAll 启动时从 SQLite 加载全部持久化数据到内存。
// 列表字段保证非 nil（JSON 序列化为 [] 而非 null）。
func (s *State) LoadAll() {
	// queue
	if items, err := s.DB.ListQueue(); err == nil {
		s.Queue = items
	} else {
		s.Logger.Error("load queue: "+err.Error(), "system")
	}
	if s.Queue == nil {
		s.Queue = []types.QueueItem{}
	}

	// history
	if items, err := s.DB.ListHistory(); err == nil {
		s.History = items
	} else {
		s.Logger.Error("load history: "+err.Error(), "system")
	}
	if s.History == nil {
		s.History = []types.PurchaseHistoryEntry{}
	}

	// servers
	if plans, err := s.DB.ListServers(); err == nil && len(plans) > 0 {
		s.ServerPlans = plans
		// 用 SQLite 里真实的 updated_at 重建缓存时间戳，
		// 这样过期的旧数据下次访问能正确触发刷新；NOW 会导致旧数据被当作"刚刷的"。
		if tsMs, err := s.DB.ServersUpdatedAt(); err == nil && tsMs > 0 {
			s.ServerCache.SetAt(plans, time.UnixMilli(tsMs))
		} else {
			s.ServerCache.Set(plans)
		}
		s.Logger.Info("已从 SQLite 加载服务器目录并同步到缓存", "system")
	} else if err != nil {
		s.Logger.Error("load servers: "+err.Error(), "system")
	}
	if s.ServerPlans == nil {
		s.ServerPlans = []types.ServerPlan{}
	}

	// config sniper
	if tasks, err := s.DB.ListSniperTasks(); err == nil {
		s.ConfigSniperTasks = tasks
	} else {
		s.Logger.Error("load config sniper: "+err.Error(), "system")
	}
	if s.ConfigSniperTasks == nil {
		s.ConfigSniperTasks = []types.ConfigSniperTask{}
	}

	// vps subscriptions
	if subs, err := s.DB.ListVPSSubscriptions(); err == nil {
		s.VPSSubscriptions = subs
	} else {
		s.Logger.Error("load vps subs: "+err.Error(), "system")
	}
	if s.VPSSubscriptions == nil {
		s.VPSSubscriptions = []types.VPSSubscription{}
	}
	// vps check interval 存 kv
	var ci int
	if ok, _ := s.DB.GetKV("vps_check_interval", &ci); ok && ci > 0 {
		s.VPSCheckInterval = ci
	}
}

// CountActiveQueues 统计未完成的队列项
func (s *State) CountActiveQueues() int {
	s.QueueMu.Lock()
	defer s.QueueMu.Unlock()
	cnt := 0
	for _, it := range s.Queue {
		if it.Status == "running" || it.Status == "pending" || it.Status == "paused" {
			cnt++
		}
	}
	return cnt
}

// CountAvailableServers 统计有库存的型号
func (s *State) CountAvailableServers() int {
	s.ServerPlansMu.RLock()
	defer s.ServerPlansMu.RUnlock()
	cnt := 0
	for _, p := range s.ServerPlans {
		for _, dc := range p.Datacenters {
			if dc.Availability != "unavailable" && dc.Availability != "unknown" {
				cnt++
				break
			}
		}
	}
	return cnt
}

// CountPurchase 统计成功/失败订单数
func (s *State) CountPurchase() (success, failed int) {
	s.HistoryMu.Lock()
	defer s.HistoryMu.Unlock()
	for _, h := range s.History {
		switch h.Status {
		case "success":
			success++
		case "failed":
			failed++
		}
	}
	return
}

// SaveQueue 把内存中 Queue 整表覆盖写入 SQLite
func (s *State) SaveQueue() error {
	s.QueueMu.Lock()
	cp := make([]types.QueueItem, len(s.Queue))
	copy(cp, s.Queue)
	s.QueueMu.Unlock()
	return s.DB.ReplaceQueue(cp)
}

// SaveHistory 把内存中 History 整表覆盖写入 SQLite
func (s *State) SaveHistory() error {
	s.HistoryMu.Lock()
	cp := make([]types.PurchaseHistoryEntry, len(s.History))
	copy(cp, s.History)
	s.HistoryMu.Unlock()
	return s.DB.ReplaceHistory(cp)
}

// SaveServers 把内存中 ServerPlans 整表覆盖写入 SQLite
func (s *State) SaveServers() error {
	s.ServerPlansMu.RLock()
	cp := make([]types.ServerPlan, len(s.ServerPlans))
	copy(cp, s.ServerPlans)
	s.ServerPlansMu.RUnlock()
	return s.DB.ReplaceServers(cp)
}

// SaveAll 一次性保存所有数据
func (s *State) SaveAll() {
	if err := s.SaveQueue(); err != nil {
		s.Logger.Error("save queue: "+err.Error(), "system")
	}
	if err := s.SaveHistory(); err != nil {
		s.Logger.Error("save history: "+err.Error(), "system")
	}
	if err := s.SaveServers(); err != nil {
		s.Logger.Error("save servers: "+err.Error(), "system")
	}
}
