package monitor

import (
	"encoding/json"
	"fmt"

	"github.com/ovh-buy/server/internal/types"
)

// kvMonitorInterval 检查间隔在 kv 表里的键
const kvMonitorInterval = "monitor_check_interval"

// monitor 包内部用 Subscription / HistoryEntry，
// 而 SQLite 层用 types.Subscription / types.SubscriptionHistoryEntry。
// 字段一一对应，下面提供双向转换。

// toDBSub 读 s 的可变字段,所以必须持 s.mu ——
// SaveToDB 是在自己的 goroutine 里跑的(定时落盘 / 退出前落盘),
// 此刻检查 goroutine 很可能正在 append History,不加锁就是竞争 + 可能读到撕裂的 slice header。
// 锁顺序:调用方(SaveToDB)先拿 subsMu 再进这里拿 s.mu,与 Snapshot/Status 一致,不会死锁。
func toDBSub(s *Subscription) types.Subscription {
	if s == nil {
		return types.Subscription{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hist := make([]types.SubscriptionHistoryEntry, 0, len(s.History))
	for _, h := range s.History {
		hist = append(hist, types.SubscriptionHistoryEntry{
			Timestamp:  h.Timestamp,
			Datacenter: h.Datacenter,
			Status:     h.Status,
			ChangeType: h.ChangeType,
			OldStatus:  h.OldStatus,
			Config:     h.Config,
		})
	}
	dcs := s.Datacenters
	if dcs == nil {
		dcs = []string{}
	}
	last := s.LastStatus
	if last == nil {
		last = map[string]string{}
	}
	return types.Subscription{
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
	}
}

func fromDBSub(s types.Subscription) *Subscription {
	hist := make([]HistoryEntry, 0, len(s.History))
	for _, h := range s.History {
		hist = append(hist, HistoryEntry{
			Timestamp:  h.Timestamp,
			Datacenter: h.Datacenter,
			Status:     h.Status,
			ChangeType: h.ChangeType,
			OldStatus:  h.OldStatus,
			Config:     h.Config,
		})
	}
	dcs := s.Datacenters
	if dcs == nil {
		dcs = []string{}
	}
	last := s.LastStatus
	if last == nil {
		last = map[string]string{}
	}
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
	}
}

// LoadFromDB 启动时从 SQLite 加载订阅 + known_servers
func (m *Monitor) LoadFromDB() {
	subs, err := m.state.DB.ListMonitorSubscriptions()
	if err != nil {
		m.state.Logger.Warn("加载监控订阅失败: "+err.Error(), "monitor")
	}
	known := []string{}
	if _, err := m.state.DB.GetKV("monitor_known_servers", &known); err != nil {
		m.state.Logger.Warn("加载已知服务器失败: "+err.Error(), "monitor")
	}

	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	m.subscriptions = make([]*Subscription, 0, len(subs))
	for _, s := range subs {
		m.subscriptions = append(m.subscriptions, fromDBSub(s))
	}
	knownSet := map[string]struct{}{}
	for _, k := range known {
		knownSet[k] = struct{}{}
	}
	m.knownServers = knownSet
	// 检查间隔从 kv 恢复;没存过或超出合法区间时夹回默认 5 秒
	interval := MinCheckInterval
	var saved int
	if ok, _ := m.state.DB.GetKV(kvMonitorInterval, &saved); ok && saved > 0 {
		interval = ClampCheckInterval(saved)
	}
	m.checkInterval = interval
	m.state.Logger.Info(fmt.Sprintf("监控检查间隔: %d 秒", interval), "monitor")
	m.state.Logger.Info("已加载订阅", "monitor")
}

// SaveToDB 把订阅 + known_servers 写回 SQLite
func (m *Monitor) SaveToDB() {
	m.subsMu.Lock()
	subs := make([]types.Subscription, 0, len(m.subscriptions))
	for _, s := range m.subscriptions {
		subs = append(subs, toDBSub(s))
	}
	known := make([]string, 0, len(m.knownServers))
	for k := range m.knownServers {
		known = append(known, k)
	}
	interval := m.checkInterval
	m.subsMu.Unlock()

	if err := m.state.DB.ReplaceMonitorSubscriptions(subs); err != nil {
		m.state.Logger.Error("保存监控订阅失败: "+err.Error(), "monitor")
		return
	}
	if err := m.state.DB.SetKV("monitor_known_servers", known); err != nil {
		m.state.Logger.Error("保存已知服务器失败: "+err.Error(), "monitor")
		return
	}
	if err := m.state.DB.SetKV(kvMonitorInterval, interval); err != nil {
		m.state.Logger.Warn("保存检查间隔失败: "+err.Error(), "monitor")
	}
	m.state.Logger.Info(fmt.Sprintf("订阅数据已保存(检查间隔 %d 秒)", interval), "monitor")
}

// SubscriptionAsJSON 帮助 handler 返回订阅
func (m *Monitor) SubscriptionAsJSON(planCode string) ([]byte, bool) {
	sub := m.FindSubscription(planCode)
	if sub == nil {
		return nil, false
	}
	b, _ := json.Marshal(sub)
	return b, true
}
