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
		AutoPay:            s.AutoPay,
		Paused:             s.Paused,
		Options:            s.Options,
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
		AutoPay:            s.AutoPay,
		Paused:             s.Paused,
		Options:            s.Options,
	}
}

// LoadFromDB 启动时从 SQLite 加载订阅 + known_servers
func (m *Monitor) LoadFromDB() {
	subs, err := m.state.DB.ListMonitorSubscriptions()
	if err != nil {
		// 读失败时下面照样会把 m.subscriptions 重置成空,而 SaveToDB 是全表覆盖 ——
		// 不拦住的话,启动读一次失败就等于把用户所有监控订阅永久删了。
		m.state.MarkLoadFailed("monitor_subscriptions", err)
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
	// 先恢复暂停闸门,再检查已入队任务;重启或代理配置重载都不会漏掉关联暂停。
	m.publishPausedQueueScopesLocked()
	var stopped bool
	_, stopErr := m.state.DB.GetKV("monitor-manually-stopped", &stopped)
	m.state.SetMonitorStopped(stopped || stopErr != nil)
	scopes := m.queueScopesLocked(false)
	if stopped || stopErr != nil {
		scopes = m.queueScopesLocked(true)
	}
	if _, err := m.state.PauseMonitorQueues(scopes); err != nil {
		m.state.Logger.Error("恢复关联抢购暂停状态失败: "+err.Error(), "monitor")
	}

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
func (m *Monitor) SaveToDB() error {
	// 保存串行化:和 app.State.Save* 同一个问题 —— "拍快照 → 全表覆盖",
	// 两个并发保存里晚拍快照的可能先落库,新数据被旧快照整表覆盖。
	// 实测 60 条订阅并发保存,库里只剩 9~17 条。
	// 六个 HTTP 调用点(增/删/改/批量添加)会并发进来。
	if err := m.state.SaveBlocked("monitor_subscriptions"); err != nil {
		m.state.Logger.Error(err.Error(), "monitor")
		return err
	}
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
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
		return err
	}
	if err := m.state.DB.SetKV("monitor_known_servers", known); err != nil {
		m.state.Logger.Error("保存已知服务器失败: "+err.Error(), "monitor")
		return err
	}
	if err := m.state.DB.SetKV(kvMonitorInterval, interval); err != nil {
		m.state.Logger.Warn("保存检查间隔失败: "+err.Error(), "monitor")
		return err
	}
	m.state.Logger.Info(fmt.Sprintf("订阅数据已保存(检查间隔 %d 秒)", interval), "monitor")
	return nil
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
