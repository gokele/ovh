package app

import (
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/types"
)

// DefaultServerBucket 默认账户视角的缓存桶 key。
// SQLite 里的 servers 只有一张全局表（没有 account 维度），启动回灌和落盘都只认这个桶。
const DefaultServerBucket = ""

// serverListBucket 一个账户视角下的目录快照
type serverListBucket struct {
	data []types.ServerPlan
	ts   time.Time
}

// ServerListCache 服务器列表内存缓存，按"账户视角"分桶（懒加载：仅访问触发刷新，无后台定时器）。
//
// 为什么要分桶：目录内容由账户的 endpoint(EU/US/CA) + ovhSubsidiary 共同决定。
// 单桶时 ?account=<US 账户> 的请求在 TTL 内会直接命中默认(EU)账户那份目录，
// 用户切了账户却看到别人的机型集合；而"非默认账户干脆不写缓存"的保守做法
// 又会让这些账户每次刷新都重拉全部 plan，把 OVH 429 风险放大回去。分桶后两头都解决。
type ServerListCache struct {
	mu      sync.RWMutex
	buckets map[string]*serverListBucket
	TTL     time.Duration
}

// NewServerListCache 默认 2 小时 TTL
func NewServerListCache() *ServerListCache {
	return &ServerListCache{TTL: 2 * time.Hour, buckets: map[string]*serverListBucket{}}
}

// GetBucket 返回指定账户视角的缓存副本和是否仍在 TTL 内
func (s *ServerListCache) GetBucket(key string) ([]types.ServerPlan, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b := s.buckets[key]
	if b == nil {
		return nil, false
	}
	cp := make([]types.ServerPlan, len(b.data))
	copy(cp, b.data)
	return cp, time.Since(b.ts) < s.TTL
}

// TimestampOf 指定桶的写入时间；桶不存在返回 nil。
// 以前 Timestamp 是导出字段、被 handler 直接裸读，跟 Set 之间没有同步；
// 改成带锁的方法顺手把这个数据竞争一起消掉。
func (s *ServerListCache) TimestampOf(key string) *time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b := s.buckets[key]
	if b == nil {
		return nil
	}
	ts := b.ts
	return &ts
}

// SetBucket 更新指定桶，时间戳=NOW；data 为空表示删除该桶
func (s *ServerListCache) SetBucket(key string, data []types.ServerPlan) {
	s.SetBucketAt(key, data, time.Now())
}

// SetBucketAt 用指定时间戳更新指定桶。
// 启动时从 SQLite 回灌历史数据要用这个，保留真实的 updated_at，
// 否则旧数据被当作刚拉的，过期判断会出错。
func (s *ServerListCache) SetBucketAt(key string, data []types.ServerPlan, ts time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(data) == 0 {
		delete(s.buckets, key)
		return
	}
	cp := make([]types.ServerPlan, len(data))
	copy(cp, data)
	s.buckets[key] = &serverListBucket{data: cp, ts: ts}
}

// Clear 清空全部桶（"清内存缓存"语义：所有账户视角一起清）
func (s *ServerListCache) Clear() {
	s.mu.Lock()
	s.buckets = map[string]*serverListBucket{}
	s.mu.Unlock()
}

// Get / Set / SetAt 是默认桶的简写，保留给不关心账户的调用方
func (s *ServerListCache) Get() ([]types.ServerPlan, bool) { return s.GetBucket(DefaultServerBucket) }

func (s *ServerListCache) Set(data []types.ServerPlan) { s.SetBucket(DefaultServerBucket, data) }

func (s *ServerListCache) SetAt(data []types.ServerPlan, ts time.Time) {
	s.SetBucketAt(DefaultServerBucket, data, ts)
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

	// 多账户:内存里持有全部 OVH 账户副本(启动从 SQLite 加载),
	// OVH Factory 通过 FindAccount 闭包按 id 查询
	AccountsMu sync.RWMutex
	Accounts   []types.OVHAccount

	QueueMu sync.Mutex
	Queue   []types.QueueItem

	HistoryMu sync.Mutex
	History   []types.PurchaseHistoryEntry

	ServerPlansMu sync.RWMutex
	ServerPlans   []types.ServerPlan

	DeletedTaskIDsMu sync.Mutex
	DeletedTaskIDs   map[string]struct{}

	VPSSubsMu        sync.Mutex
	VPSSubscriptions []types.VPSSubscription
	VPSCheckInterval int

	MonitorRunning        bool
	QueueProcessorRunning bool
}

// NewState 构造应用状态。DB 必须已 Open。
func NewState(paths storage.Paths, cfg *config.Store, lg *logger.Logger, sqliteDB *db.DB) *State {
	s := &State{
		Paths:                 paths,
		Config:                cfg,
		Logger:                lg,
		ServerCache:           NewServerListCache(),
		DB:                    sqliteDB,
		DeletedTaskIDs:        make(map[string]struct{}),
		Accounts:              []types.OVHAccount{},
		Queue:                 []types.QueueItem{},
		History:               []types.PurchaseHistoryEntry{},
		ServerPlans:           []types.ServerPlan{},
		VPSSubscriptions:      []types.VPSSubscription{},
		VPSCheckInterval:      60,
		QueueProcessorRunning: true,
	}
	// Factory 闭包注入 lookup,允许按 id 查账户(空 id → 默认)
	s.OVH = ovh.NewFactory(cfg, s.FindAccount)
	return s
}

// HasAnyAccount 是否至少有一个 OVH 账户。
// 多账户场景下,旧的 state.Config.HasCredentials() 不再可靠(新用户的 kv['config'] 可能为空),
// 凡是判断"系统能不能调 OVH"都应该走这个。
func (s *State) HasAnyAccount() bool {
	s.AccountsMu.RLock()
	defer s.AccountsMu.RUnlock()
	return len(s.Accounts) > 0
}

// FindAccount 多账户查找。id="" 返回默认账户(没默认 → 第一个);否则按 ID 精确匹配。
// OVH Factory 的 lookup 走这个,所有 ClientFor(accountID) 都会绕一圈到这里。
func (s *State) FindAccount(id string) (types.OVHAccount, bool) {
	s.AccountsMu.RLock()
	defer s.AccountsMu.RUnlock()
	if id == "" {
		for _, a := range s.Accounts {
			if a.IsDefault {
				return a, true
			}
		}
		if len(s.Accounts) > 0 {
			return s.Accounts[0], true
		}
		return types.OVHAccount{}, false
	}
	for _, a := range s.Accounts {
		if a.ID == id {
			return a, true
		}
	}
	return types.OVHAccount{}, false
}

// ServerCacheKey 目录缓存的分桶 key。
// 目录内容只由 endpoint(决定打哪个大区的 API) + zone(ovhSubsidiary，决定卖哪些机型) 决定，
// 所以按这两项分桶而不是按 accountID —— 同 endpoint 同 zone 的两个账户没必要各拉一遍 96 个 plan。
// 默认账户视角统一映射到 DefaultServerBucket：它对应 SQLite 里那张唯一的 servers 表，
// 启动回灌和落盘都认这个 key，账户查不到时也退到它（此时压根调不了 OVH）。
func (s *State) ServerCacheKey(accountID string) string {
	acc, ok := s.FindAccount(accountID)
	if !ok {
		return DefaultServerBucket
	}
	if def, defOK := s.FindAccount(""); defOK &&
		strings.EqualFold(acc.Endpoint, def.Endpoint) && strings.EqualFold(acc.Zone, def.Zone) {
		return DefaultServerBucket
	}
	return strings.ToLower(acc.Endpoint) + "|" + strings.ToUpper(acc.Zone)
}

// ReloadAccounts 从 SQLite 重新加载账户到内存,并把整个 OVH client 缓存清掉,
// 强制下次 ClientFor() 用最新凭据重建。
// 账户 CRUD 操作完成后调一次。
func (s *State) ReloadAccounts() error {
	accs, err := s.DB.ListAccounts()
	if err != nil {
		return err
	}
	s.AccountsMu.Lock()
	if accs == nil {
		accs = []types.OVHAccount{}
	}
	s.Accounts = accs
	s.AccountsMu.Unlock()
	s.OVH.InvalidateAll()
	return nil
}

// LoadAll 启动时从 SQLite 加载全部持久化数据到内存。
// 列表字段保证非 nil（JSON 序列化为 [] 而非 null）。
func (s *State) LoadAll() {
	// accounts: 必须最先加载,因为别的数据/loop 都按 account_id 索引
	s.migrateLegacyConfigToAccount() // 老用户从 kv['config'] 自动建默认账户
	if accs, err := s.DB.ListAccounts(); err == nil {
		if accs == nil {
			accs = []types.OVHAccount{}
		}
		s.AccountsMu.Lock()
		s.Accounts = accs
		s.AccountsMu.Unlock()
		s.Logger.Info("已加载 OVH 账户: "+intStr(len(accs))+" 个", "system")
	} else {
		s.Logger.Error("load accounts: "+err.Error(), "system")
	}

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
			// 用白名单而不是 `!= unavailable`:comingSoon(即将上线尚未开卖)下不了单,
			// 算进"可用服务器"会让仪表盘数字虚高
			if ovh.IsAvailableForOrder(dc.Availability) {
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

// migrateLegacyConfigToAccount 老用户升级:如果 SQLite 里没账户但 kv['config'] 有
// 完整 OVH 凭据,自动把它建成一个名为"默认账户"的 OVHAccount,设默认,
// 并把现有所有 queue/history/sniper_task 的 account_id 列回填指向它。
// 已经有账户的话什么都不做,幂等。
func (s *State) migrateLegacyConfigToAccount() {
	n, err := s.DB.CountAccounts()
	if err != nil {
		s.Logger.Error("count accounts: "+err.Error(), "system")
		return
	}
	if n > 0 {
		return // 已经有账户,跳过
	}
	cfg := s.Config.Get()
	if cfg.AppKey == "" || cfg.AppSecret == "" || cfg.ConsumerKey == "" {
		// 没老凭据,首次安装,等用户在 OvhCredsGate 创建第一个账户
		return
	}
	zone := cfg.Zone
	if zone == "" {
		zone = "IE"
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "ovh-eu"
	}
	iam := cfg.IAM
	if iam == "" {
		iam = "go-ovh-" + strings.ToLower(zone)
	}
	acc := types.OVHAccount{
		ID:          uuid.NewString(),
		Name:        "默认账户",
		Endpoint:    endpoint,
		Zone:        zone,
		AppKey:      cfg.AppKey,
		AppSecret:   cfg.AppSecret,
		ConsumerKey: cfg.ConsumerKey,
		IAM:         iam,
		IsDefault:   true,
		CreatedAt:   types.NowISO(),
	}
	if err := s.DB.UpsertAccount(acc); err != nil {
		s.Logger.Error("migrate legacy config to account: "+err.Error(), "system")
		return
	}
	// 回填现有数据的 account_id 列(从空值 → 新账户 ID)
	for _, stmt := range []string{
		`UPDATE queue SET account_id = ? WHERE account_id = '' OR account_id IS NULL`,
		`UPDATE history SET account_id = ? WHERE account_id = '' OR account_id IS NULL`,
		`UPDATE config_sniper_tasks SET account_id = ? WHERE account_id = '' OR account_id IS NULL`,
	} {
		if _, err := s.DB.Exec(stmt, acc.ID); err != nil {
			s.Logger.Warn("backfill account_id: "+err.Error(), "system")
		}
	}
	s.Logger.Info("已把旧 kv['config'] 迁移成默认账户: "+acc.Name+" ("+acc.Zone+")", "system")
}

// intStr 小工具,避免在 LoadAll 里临时引 strconv
func intStr(n int) string {
	// 简化版,只处理 0..9999
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
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
