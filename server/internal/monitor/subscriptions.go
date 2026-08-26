package monitor

import (
	"fmt"
	"time"
)

// autoOrderAccountID:auto_order 触发时用哪个账户下单;空 = 只通知不下单
func (m *Monitor) AddSubscription(planCode string, datacenters []string, notifyAvailable, notifyUnavailable bool,
	serverName string, lastStatus map[string]string, history []HistoryEntry, autoOrder bool, quantity int,
	autoOrderAccountID string) {

	m.subsMu.Lock()
	defer m.subsMu.Unlock()

	for _, s := range m.subscriptions {
		if s.PlanCode == planCode {
			m.state.Logger.Warn(fmt.Sprintf("订阅已存在: %s，将更新配置（不会重置状态，避免重复通知）", planCode), "monitor")
			if datacenters == nil {
				datacenters = []string{}
			}
			// 必须持订阅自己的锁改:subsMu 只保护切片,而这条订阅此刻很可能
			// 正被检查 goroutine 读着(它拿的是同一个指针)。
			s.mu.Lock()
			s.Datacenters = datacenters
			s.NotifyAvailable = notifyAvailable
			s.NotifyUnavailable = notifyUnavailable
			s.AutoOrder = autoOrder
			if autoOrder {
				if quantity < 1 {
					quantity = 1
				}
				s.Quantity = quantity
			} else {
				s.Quantity = 0
			}
			s.ServerName = serverName
			s.AutoOrderAccountID = autoOrderAccountID
			if s.History == nil {
				s.History = []HistoryEntry{}
			}
			s.mu.Unlock()
			return
		}
	}

	if datacenters == nil {
		datacenters = []string{}
	}
	if lastStatus == nil {
		lastStatus = map[string]string{}
	}
	if history == nil {
		history = []HistoryEntry{}
	}
	sub := &Subscription{
		PlanCode:           planCode,
		Datacenters:        datacenters,
		NotifyAvailable:    notifyAvailable,
		NotifyUnavailable:  notifyUnavailable,
		LastStatus:         lastStatus,
		CreatedAt:          time.Now().Format(time.RFC3339Nano),
		History:            history,
		AutoOrderAccountID: autoOrderAccountID,
	}
	if autoOrder {
		if quantity < 1 {
			quantity = 1
		}
		sub.AutoOrder = true
		sub.Quantity = quantity
	}
	if serverName != "" {
		sub.ServerName = serverName
	}
	m.subscriptions = append(m.subscriptions, sub)
	displayName := planCode
	if serverName != "" {
		displayName = planCode + " (" + serverName + ")"
	}
	dcsStr := "全部"
	if len(datacenters) > 0 {
		dcsStr = fmt.Sprintf("%v", datacenters)
	}
	m.state.Logger.Info(fmt.Sprintf("添加订阅: %s, 数据中心: %s", displayName, dcsStr), "monitor")
}

func (m *Monitor) RemoveSubscription(planCode string) bool {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	original := len(m.subscriptions)
	kept := make([]*Subscription, 0, len(m.subscriptions))
	for _, s := range m.subscriptions {
		if s.PlanCode != planCode {
			kept = append(kept, s)
		}
	}
	m.subscriptions = kept
	if len(m.subscriptions) < original {
		m.state.Logger.Info("删除订阅: "+planCode, "monitor")
		return true
	}
	return false
}

func (m *Monitor) ClearSubscriptions() int {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	count := len(m.subscriptions)
	m.subscriptions = []*Subscription{}
	m.state.Logger.Info(fmt.Sprintf("清空所有订阅 (%d 项)", count), "monitor")
	return count
}

// FindSubscription 按 planCode 查找,返回的是**深拷贝**(与 Snapshot 同口径)。
// 不返回真身:调用方(handler 读 History、SubscriptionAsJSON 做 Marshal)都在 HTTP goroutine 里,
// 而检查 goroutine 正并发地往同一条订阅上 append History —— 直接把指针递出去就是数据竞争。
// 需要改订阅请走 AddSubscription / RemoveSubscription。
func (m *Monitor) FindSubscription(planCode string) *Subscription {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	for _, s := range m.subscriptions {
		if s.PlanCode == planCode {
			return s.snapshot()
		}
	}
	return nil
}

// SetKnownServers 用于从持久化恢复
func (m *Monitor) SetKnownServers(set map[string]struct{}) {
	m.subsMu.Lock()
	m.knownServers = set
	m.subsMu.Unlock()
}

// KnownServers 返回当前已知服务器集合（用于持久化）
func (m *Monitor) KnownServers() []string {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	out := make([]string, 0, len(m.knownServers))
	for k := range m.knownServers {
		out = append(out, k)
	}
	return out
}

// MessageUUIDCacheLookup 用于 webhook 回调时取回完整配置
func (m *Monitor) MessageUUIDCacheLookup(id string) *CachedMessage {
	m.cacheLock.Lock()
	defer m.cacheLock.Unlock()
	if cm, ok := m.messageUUIDCache[id]; ok {
		if time.Now().Unix()-int64(cm.Timestamp) < int64(m.messageUUIDCacheTTL.Seconds()) {
			return cm
		}
		delete(m.messageUUIDCache, id)
		m.state.Logger.Warn("UUID缓存已过期: "+id, "telegram")
	}
	return nil
}

// OptionsCacheLookup 兼容旧机制
func (m *Monitor) OptionsCacheLookup(key string) []string {
	m.cacheLock.Lock()
	defer m.cacheLock.Unlock()
	if c, ok := m.optionsCache[key]; ok {
		if time.Now().Unix()-int64(c.Timestamp) < int64(m.optionsCacheTTL.Seconds()) {
			return c.Options
		}
		delete(m.optionsCache, key)
		m.state.Logger.Warn("options缓存已过期: "+key, "telegram")
	}
	return nil
}

func (m *Monitor) cleanupExpiredCaches() {
	now := time.Now().Unix()
	// 顺带清理 SQLite 里过期的一键下单按钮，防止表无限增长
	if m.state.DB != nil {
		before := float64(time.Now().Add(-m.messageUUIDCacheTTL).Unix())
		if n, err := m.state.DB.DeleteExpiredTelegramButtons(before); err != nil {
			m.state.Logger.Debug("清理过期一键下单按钮失败: "+err.Error(), "telegram")
		} else if n > 0 {
			m.state.Logger.Debug(fmt.Sprintf("已清理 %d 个过期一键下单按钮", n), "telegram")
		}
	}
	m.cacheLock.Lock()
	defer m.cacheLock.Unlock()
	expUUIDs := []string{}
	for k, v := range m.messageUUIDCache {
		if now-int64(v.Timestamp) >= int64(m.messageUUIDCacheTTL.Seconds()) {
			expUUIDs = append(expUUIDs, k)
		}
	}
	for _, k := range expUUIDs {
		delete(m.messageUUIDCache, k)
	}
	expOpts := []string{}
	for k, v := range m.optionsCache {
		if now-int64(v.Timestamp) >= int64(m.optionsCacheTTL.Seconds()) {
			expOpts = append(expOpts, k)
		}
	}
	for _, k := range expOpts {
		delete(m.optionsCache, k)
	}
	// 顺带扫掉 resolveQueryAccount 的过期条目:它的 key 里带账户指纹,
	// 每改一次账户就换一批 key,旧 key 再也不会被读到 —— 不清就是只涨不降的内存。
	queryAccountMu.Lock()
	expChoices := 0
	for k, c := range queryAccountCache {
		if time.Since(c.at) >= c.effectiveTTL() {
			delete(queryAccountCache, k)
			expChoices++
		}
	}
	queryAccountMu.Unlock()
	if expChoices > 0 {
		m.state.Logger.Debug(fmt.Sprintf("清理过期查询账户选取缓存: %d 条", expChoices), "monitor")
	}
	if len(expUUIDs) > 0 || len(expOpts) > 0 {
		m.state.Logger.Debug(fmt.Sprintf("清理过期缓存: UUID=%d个, Options=%d个", len(expUUIDs), len(expOpts)), "monitor")
	}
}

// AddMessageUUID 缓存按钮对应的配置
func (m *Monitor) AddMessageUUID(id, planCode, datacenter string, options []string, configInfo map[string]interface{}) {
	now := float64(time.Now().Unix())
	m.cacheLock.Lock()
	m.messageUUIDCache[id] = &CachedMessage{
		PlanCode:   planCode,
		Datacenter: datacenter,
		Options:    options,
		ConfigInfo: configInfo,
		Timestamp:  now,
	}
	m.cacheLock.Unlock()

	// 同时落库：内存缓存进程重启就没了，按钮一点击就 400；
	// 落库后按钮跨重启可用，并且 used_at 让它只能被消费一次。
	if m.state.DB != nil {
		if err := m.state.DB.UpsertTelegramButton(id, planCode, datacenter, options, configInfo, now); err != nil {
			m.state.Logger.Warn("一键下单按钮落库失败（仍可用内存缓存）: "+err.Error(), "telegram")
		}
	}
}

// SubscriptionConfig 取一份订阅配置的只读快照。
// 为什么不直接把 *Subscription 交出去让调用方读字段:那些字段被检查 goroutine
// 并发改着,裸读是数据竞争。这里在订阅自己的锁里拷一份出去。
// 只拷配置字段 —— LastStatus / History 是状态,编辑接口不该碰。
func (m *Monitor) SubscriptionConfig(planCode string) SubscriptionConfig {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	for _, s := range m.subscriptions {
		if s.PlanCode != planCode {
			continue
		}
		s.mu.Lock()
		cfg := SubscriptionConfig{
			PlanCode:           s.PlanCode,
			Datacenters:        append([]string(nil), s.Datacenters...),
			NotifyAvailable:    s.NotifyAvailable,
			NotifyUnavailable:  s.NotifyUnavailable,
			ServerName:         s.ServerName,
			AutoOrder:          s.AutoOrder,
			Quantity:           s.Quantity,
			AutoOrderAccountID: s.AutoOrderAccountID,
		}
		s.mu.Unlock()
		return cfg
	}
	return SubscriptionConfig{}
}

// SubscriptionConfig 订阅里「用户可改的那部分」
type SubscriptionConfig struct {
	PlanCode           string
	Datacenters        []string
	NotifyAvailable    bool
	NotifyUnavailable  bool
	ServerName         string
	AutoOrder          bool
	Quantity           int
	AutoOrderAccountID string
}

// ClearAccountRefs 把内存订阅里对某账户的引用清掉。
//
// 删账户时 SQL 已经把 auto_order_account_id 清空,但 Monitor 内存里还是旧值 ——
// 而 SaveToDB 是拿内存整表 Replace 回写的,任何一次订阅增删改都会把已删的
// 账户 ID 复活回数据库,SQL 的级联清理等于白做。所以内存必须同步清。
// 返回清了几条,给日志用。
func (m *Monitor) ClearAccountRefs(accountID string) int {
	if accountID == "" {
		return 0
	}
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	n := 0
	for _, s := range m.subscriptions {
		s.mu.Lock()
		if s.AutoOrderAccountID == accountID {
			s.AutoOrderAccountID = ""
			// 账户没了就不可能下单,别让 AutoOrder 停留在"开着但买不了"的假状态
			s.AutoOrder = false
			n++
		}
		s.mu.Unlock()
	}
	return n
}
