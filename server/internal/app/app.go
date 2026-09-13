package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/netfp"
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

	// 正在跑 PurchaseServer 的任务 → 取消函数。
	// DeletedTaskIDs 只是个标记,处理器要到下一轮才会看它;而一轮下单链路有 10 次
	// OVH 调用、每次最长 60s。用户删任务的那一刻如果链路正跑到一半,这里的 cancel
	// 让正在进行的 HTTP 调用立刻中断,而不是把这一轮跑完 —— 包括结账。
	taskCancelMu sync.Mutex
	taskCancel   map[string]context.CancelFunc

	VPSSubsMu sync.Mutex

	// 保存串行化锁。Save* 是"快照 + 全表覆盖",两个并发保存里
	// 晚拍快照的可能先落库,把新数据覆盖掉 —— 必须让"拍快照到写完"整段串行。
	// 和上面那些数据锁分开:数据锁保护内存读写(要短),这些保护落库顺序(会持有到 IO 结束)。
	saveQueueMu   sync.Mutex
	saveHistoryMu sync.Mutex
	saveServersMu sync.Mutex

	// 启动时哪张表没读出来。
	//
	// Save* 全是"DELETE 整表 + 重新 INSERT 内存快照"。LoadAll 读失败时只记了条日志,
	// 内存留空,于是**第一次保存就把那张表整个抹了** —— 加一列忘了同步结构体、
	// 库文件被别的进程锁住、磁盘临时 IO 错,任何一种都够把用户全部抢购历史/队列
	// 永久删掉,而且删得静悄悄。
	//
	// 所以:读失败的表一律禁止再写。宁可这次运行不落库,也不能拿空内存去覆盖磁盘上
	// 那份还完好的数据。用户重启一次(或修好 schema)就能恢复。
	loadFailedMu     sync.RWMutex
	loadFailed       map[string]string
	VPSSubscriptions []types.VPSSubscription
	VPSCheckInterval int

	MonitorRunning        bool
	QueueProcessorRunning bool

	// onProxyError 代理故障回调,由 main 接到 proxyguard。
	onProxyError func(accountID string, err error)
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
		taskCancel:            make(map[string]context.CancelFunc),
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
		s.MarkLoadFailed("ovh_accounts", err)
	}

	// queue
	if items, err := s.DB.ListQueue(); err == nil {
		s.Queue = items
	} else {
		s.MarkLoadFailed("queue", err)
	}
	if s.Queue == nil {
		s.Queue = []types.QueueItem{}
	}

	// history
	if items, err := s.DB.ListHistory(); err == nil {
		s.History = items
	} else {
		s.MarkLoadFailed("history", err)
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
		s.MarkLoadFailed("servers", err)
	}
	if s.ServerPlans == nil {
		s.ServerPlans = []types.ServerPlan{}
	}

	// vps subscriptions
	if subs, err := s.DB.ListVPSSubscriptions(); err == nil {
		s.VPSSubscriptions = subs
	} else {
		s.MarkLoadFailed("vps_subscriptions", err)
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

// Save* 系列都是"拍内存快照 → 全表 DELETE+INSERT"。
//
// 快照和写库必须在同一把锁里,否则并发调用会丢数据:
// A 拍快照(60 条)→ B 拍快照(59 条)→ B 先写库 → A 后写库,
// 库里最后是 A 那份也就罢了 —— 真实情况是 goroutine 调度随机,
// 晚拍的快照经常先落库,新数据被旧快照整表覆盖。
//
// 实测(internal/app 的并发测试):一边追加 60 条历史一边并发触发保存,
// 库里最后只剩 19~35 条,丢一半以上。而代码里有八处 `go state.SaveHistory()`
// 是 fire-and-forget 调用的,批量抢购时会同时触发 —— 抢到的订单记录
// 就这么没了,重启后历史里查无此单。
//
// 每类数据一把独立的保存锁:历史和队列互不阻塞。
// 注意这把锁必须包住整个"快照+写库",而不只是写库。
func (s *State) SaveQueue() error {
	if err := s.SaveBlocked("queue"); err != nil {
		return err
	}
	s.saveQueueMu.Lock()
	defer s.saveQueueMu.Unlock()
	s.QueueMu.Lock()
	cp := make([]types.QueueItem, len(s.Queue))
	copy(cp, s.Queue)
	s.QueueMu.Unlock()
	return s.DB.ReplaceQueue(cp)
}

// MarkLoadFailed 记下某张表启动时没读出来,之后禁止覆盖写它。
func (s *State) MarkLoadFailed(table string, err error) {
	s.loadFailedMu.Lock()
	if s.loadFailed == nil {
		s.loadFailed = make(map[string]string)
	}
	s.loadFailed[table] = err.Error()
	s.loadFailedMu.Unlock()
	s.Logger.Error(
		"load "+table+" 失败,已禁止本次运行覆盖写该表(避免用空内存抹掉磁盘上的数据): "+err.Error(),
		"system",
	)
}

// SaveBlocked 读失败的表返回非 nil,调用方据此放弃保存。
func (s *State) SaveBlocked(table string) error {
	s.loadFailedMu.RLock()
	reason, bad := s.loadFailed[table]
	s.loadFailedMu.RUnlock()
	if !bad {
		return nil
	}
	return fmt.Errorf("拒绝写 %s:启动时这张表就没读出来(%s),再写会用空内存覆盖掉磁盘上的数据。请修复后重启", table, reason)
}

// LoadFailures 返回启动时读失败的表,给健康检查/前端横幅用。
func (s *State) LoadFailures() map[string]string {
	s.loadFailedMu.RLock()
	defer s.loadFailedMu.RUnlock()
	out := make(map[string]string, len(s.loadFailed))
	for k, v := range s.loadFailed {
		out[k] = v
	}
	return out
}

// MarkTaskDeleted 标记任务已删除,并取消它正在进行的下单(如果有)。
//
// 所有删任务的入口(网页删单个 / 清空、TG /cancel、处理器复核)都必须走这里。
// 只写 DeletedTaskIDs 不调 cancel 的话,PurchaseServer 会把这一轮跑完 —— 包括结账:
// 用户在"有货"通知弹出后两秒内点了删除,单照样下出去。
// EnqueueItems 入队并落库。失败时把这批从内存里撤回,再把错返回给调用方。
//
// 四条入队路径(网页新建 / 快速下单 / TG 一键按钮 / TG 文本下单)以前各写各的,
// 对"落库失败"的处理有四种:撤回+归还按钮、返回 warning 但内存保留、
// 返回带警告的文本、以及 `_ = SaveQueue()` 直接吞掉。
// 最后那种在自动下单路径上 —— 监控发现有货、建了任务、落库失败无人知晓,
// 重启后任务没了,而用户以为一直在抢。
//
// 统一成一种语义:**要么内存和磁盘都有,要么两边都没有**。
// 半成功状态("这次能跑但重启就丢")对抢购来说是最坏的,它看起来完全正常。
//
// prepend=true 把这批放到队首(快速下单要抢在别的任务前面)。
func (s *State) EnqueueItems(items []types.QueueItem, prepend bool) error {
	if len(items) == 0 {
		return nil
	}
	s.QueueMu.Lock()
	// 队列总量闸门。放在这里是因为四条入队路径(网页新建 / 快速下单 / TG 文本下单 /
	// 监控自动下单)全都汇到这个函数 —— 加一次就都受保护。
	//
	// 要防的是"多打一个数字"这种事:网页端建任务的循环没有上界,
	// 数量填 9999 × 5 个机房 = 近 5 万条任务,而每一条都是一次真实下单尝试。
	// 正常用法离 MaxQueueItems 很远,撞上它基本可以断定是填错了。
	if len(s.Queue)+len(items) > types.MaxQueueItems {
		have, want := len(s.Queue), len(items)
		s.QueueMu.Unlock()
		return fmt.Errorf(
			"队列里已有 %d 条任务,这次还要加 %d 条,会超过上限 %d。"+
				"每条任务都是一次真实的下单尝试 —— 请先确认数量没填错,"+
				"或到队列页清理掉不需要的任务",
			have, want, types.MaxQueueItems)
	}
	if prepend {
		s.Queue = append(append([]types.QueueItem{}, items...), s.Queue...)
	} else {
		s.Queue = append(s.Queue, items...)
	}
	s.QueueMu.Unlock()

	if err := s.SaveQueue(); err != nil {
		// 撤回:按 ID 精确删,不能按下标 —— 这中间别的 goroutine 可能也在增删
		ids := make(map[string]struct{}, len(items))
		for _, it := range items {
			ids[it.ID] = struct{}{}
		}
		s.QueueMu.Lock()
		kept := make([]types.QueueItem, 0, len(s.Queue))
		for _, it := range s.Queue {
			if _, bad := ids[it.ID]; !bad {
				kept = append(kept, it)
			}
		}
		s.Queue = kept
		s.QueueMu.Unlock()
		return err
	}
	return nil
}

func (s *State) MarkTaskDeleted(id string) {
	s.DeletedTaskIDsMu.Lock()
	s.DeletedTaskIDs[id] = struct{}{}
	s.DeletedTaskIDsMu.Unlock()

	s.taskCancelMu.Lock()
	cancel := s.taskCancel[id]
	delete(s.taskCancel, id)
	s.taskCancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// IsTaskDeleted 任务是否已被标记删除。
func (s *State) IsTaskDeleted(id string) bool {
	s.DeletedTaskIDsMu.Lock()
	defer s.DeletedTaskIDsMu.Unlock()
	_, ok := s.DeletedTaskIDs[id]
	return ok
}

// RegisterTaskCancel 登记一个任务这一轮下单的取消函数。PurchaseServer 开跑前调。
//
// 调用方登记完必须再查一次 IsTaskDeleted:登记前一瞬间刚好被删的话,
// MarkTaskDeleted 那时还找不到 cancel 函数,ctx 不会被取消 —— 那次复核把这个窗口堵上。
func (s *State) RegisterTaskCancel(id string, cancel context.CancelFunc) {
	s.taskCancelMu.Lock()
	if s.taskCancel == nil {
		s.taskCancel = make(map[string]context.CancelFunc)
	}
	s.taskCancel[id] = cancel
	s.taskCancelMu.Unlock()
}

// UnregisterTaskCancel 一轮下单结束后注销并释放 ctx。用 defer 调,成败都要走。
// 已被 MarkTaskDeleted 摘掉的话这里是空操作。
func (s *State) UnregisterTaskCancel(id string) {
	s.taskCancelMu.Lock()
	cancel := s.taskCancel[id]
	delete(s.taskCancel, id)
	s.taskCancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// SaveHistory 把内存中 History 整表覆盖写入 SQLite
func (s *State) SaveHistory() error {
	if err := s.SaveBlocked("history"); err != nil {
		return err
	}
	s.saveHistoryMu.Lock()
	defer s.saveHistoryMu.Unlock()
	s.HistoryMu.Lock()
	cp := make([]types.PurchaseHistoryEntry, len(s.History))
	copy(cp, s.History)
	s.HistoryMu.Unlock()
	return s.DB.ReplaceHistory(cp)
}

// SaveServers 把内存中 ServerPlans 整表覆盖写入 SQLite
func (s *State) SaveServers() error {
	if err := s.SaveBlocked("servers"); err != nil {
		return err
	}
	s.saveServersMu.Lock()
	defer s.saveServersMu.Unlock()
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

// HTTPClientFor 按账户的出站配置建一个普通 HTTP 客户端。
//
// 给那些**不带凭据**但仍然打 OVH 的请求用：公开目录、可用性探测等等。
// 它们以前一律走直连 —— 于是即便每个账户都配了代理，这些请求仍然从本机
// 真实 IP 发出去，出口隔离漏了一半。
//
// accountID 为空 → 默认账户的配置。账户不存在 → 直连（这些是公开接口，
// 没有账户也该能查，不该因为找不到账户就整个功能失效）。
//
// 和带凭据那条路一样：配了代理却建不出来时返回错误，**不退回直连**。
func (s *State) HTTPClientFor(accountID string, timeout time.Duration) (*http.Client, error) {
	acc, ok := s.FindAccount(accountID)
	if !ok {
		return &http.Client{Timeout: timeout}, nil
	}
	prof, _ := netfp.LookupProfile(acc.Fingerprint)
	return netfp.Client(netfp.Options{
		ProxyURL: acc.ProxyURL,
		Profile:  prof,
		Timeout:  timeout,
		OnProxyError: func(e error) {
			if s.onProxyError != nil {
				s.onProxyError(acc.ID, e)
			}
		},
	})
}

// SetProxyErrorHook 注入代理故障回调，供 HTTPClientFor 建出来的客户端使用。
func (s *State) SetProxyErrorHook(fn func(accountID string, err error)) {
	s.onProxyError = fn
}
