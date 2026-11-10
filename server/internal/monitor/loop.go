package monitor

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/notify"
)

// tgRecheckInterval loop 内 TG 健康检查节流间隔。5 分钟 verify 一次,
// 失败立即调 Stop() 自停监控。
const tgRecheckInterval = 5 * time.Minute

// checkNotifyOrStop 节流后体检通知通道,**全部**不可用才自停。
//
// 以前这里只看 Telegram,TG 一失效就停整个监控 —— 于是 bot 被封、token 过期、
// 或者机器一时连不上 api.telegram.org,用户失去的不是一条通知,而是整个补货监控,
// 且只有翻日志才知道。现在只要还有一条通道能送达(比如 Webhook),监控就继续跑。
//
// 返回 true=继续 loop,false=已自停。
func (m *Monitor) checkNotifyOrStop() bool {
	m.tgCheckMu.Lock()
	due := time.Since(m.lastTGCheck) >= tgRecheckInterval
	m.tgCheckMu.Unlock()
	if !due {
		return true
	}
	ok, reason := notify.AnyAvailable(m.state, false) // 周期性只查配置:真 POST 对 Server酱/Bark 就是发通知(issue #2)
	m.tgCheckMu.Lock()
	m.lastTGCheck = time.Now()
	m.tgCheckMu.Unlock()
	if !ok {
		m.state.Logger.Error("所有通知通道都不可用,自动停止服务器监控("+reason+
			")。配一条 Webhook 作为备用通道可以避免这种全盲。", "monitor")
		m.Stop()
		return false
	}
	return true
}

func (m *Monitor) CheckNewServers(currentServerList []map[string]interface{}) {
	current := map[string]struct{}{}
	for _, s := range currentServerList {
		if pc, ok := s["planCode"].(string); ok && pc != "" {
			current[pc] = struct{}{}
		}
	}
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	if len(m.knownServers) == 0 {
		m.knownServers = current
		m.state.Logger.Info(fmt.Sprintf("初始化已知服务器列表: %d 台", len(current)), "monitor")
		return
	}
	newServers := []string{}
	for k := range current {
		if _, ok := m.knownServers[k]; !ok {
			newServers = append(newServers, k)
		}
	}
	if len(newServers) > 0 {
		for _, code := range newServers {
			for _, s := range currentServerList {
				if pc, _ := s["planCode"].(string); pc == code {
					m.SendNewServerAlert(s)
				}
			}
		}
		m.knownServers = current
		m.state.Logger.Info(fmt.Sprintf("检测到 %d 台新服务器上架", len(newServers)), "monitor")
	}
}

func (m *Monitor) runSubscriptionCheck(sub *Subscription, traceID string) {
	planCode := sub.PlanCode
	m.state.Logger.Info("开始处理订阅: "+planCode, "monitor")
	m.CheckAvailabilityChange(sub, traceID)
	m.state.Logger.Info("完成处理订阅: "+planCode, "monitor")
}

// stillMine 这个循环该不该继续:running 为真且代际号还是自己那一代
func (m *Monitor) stillMine(gen int64) bool {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	return m.running && m.generation == gen
}

func (m *Monitor) monitorLoopGen(gen int64) {
	m.state.Logger.Info("监控循环已启动", "monitor")
	for {
		roundStart := time.Now()
		if !m.stillMine(gen) {
			break
		}

		// 通知通道全挂 → 自停。checkNotifyOrStop 内部已节流 5 分钟,且失败时调 Stop()。
		if !m.checkNotifyOrStop() {
			break
		}

		m.cleanupExpiredCaches()

		m.subsMu.Lock()
		count := len(m.subscriptions)
		subsCopy := make([]*Subscription, count)
		copy(subsCopy, m.subscriptions)
		m.subsMu.Unlock()

		if count > 0 {
			m.state.Logger.Info(fmt.Sprintf("开始检查 %d 个订阅...", count), "monitor")
			workers := m.maxWorkers
			if count < workers {
				workers = count
			}
			if workers < 1 {
				workers = 1
			}
			sem := make(chan struct{}, workers)
			var wg sync.WaitGroup
			for _, sub := range subsCopy {
				if sub.checkConfig().Paused {
					continue // 暂停的订阅:保留配置与历史,本轮不检查
				}
				running := m.stillMine(gen)
				if !running {
					break
				}
				if !m.stillInSubscriptions(sub) {
					m.state.Logger.Debug(fmt.Sprintf("订阅 %s 在检查期间被删除，跳过", sub.PlanCode), "monitor")
					continue
				}
				traceID := uuid.NewString()
				wg.Add(1)
				sem <- struct{}{}
				go func(s *Subscription, tid string) {
					defer wg.Done()
					defer func() { <-sem }()
					defer func() {
						if r := recover(); r != nil {
							m.state.Logger.Error(fmt.Sprintf("[trace:%s] 并发检查订阅 %s 时异常: %v",
								tid, s.PlanCode, r), "monitor")
						}
					}()
					m.runSubscriptionCheck(s, tid)
				}(sub, traceID)
			}
			wg.Wait()

			// 本轮改出了新状态就落库。
			// 以前这一步根本不存在:LastStatus / History 只活在内存里,
			// 只有用户手动增删改订阅时才顺带被写进去。程序重启后 LastStatus 是空的,
			// 下一轮就把"现在有货"误判成首次检查的初始存量 —— 不通知,也不自动下单。
			// 用户重启一次,就正好错过一次补货。
			if m.dirty.Swap(false) {
				m.SaveToDB()
			}
		} else {
			m.state.Logger.Info("当前无订阅，跳过检查", "monitor")
		}

		// 按轮次起点计时,不再把网络/验价耗时额外加到检查间隔上。
		for m.stillMine(gen) {
			remaining := time.Duration(m.CheckInterval())*time.Second - time.Since(roundStart)
			if remaining <= 0 {
				break
			}
			if remaining > 100*time.Millisecond {
				remaining = 100 * time.Millisecond
			}
			time.Sleep(remaining)
		}

	}
	m.state.Logger.Info("监控循环已停止", "monitor")
}

func (m *Monitor) stillInSubscriptions(sub *Subscription) bool {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	for _, s := range m.subscriptions {
		if s == sub {
			return true
		}
	}
	return false
}

// 配置改变或暂停后,旧一轮即使完成了网络请求也不能触发通知或下单。
func (m *Monitor) checkCurrent(sub *Subscription, cfg subCheckConfig) bool {
	if !m.stillInSubscriptions(sub) {
		return false
	}
	sub.mu.Lock()
	defer sub.mu.Unlock()
	return !sub.Paused && sub.revision == cfg.Revision
}

func (m *Monitor) Start() bool {
	m.subsMu.Lock()
	// 手动启动/新增订阅也会解除全局停止记录;失败时不启动。
	if m.state.DB != nil {
		if err := m.state.DB.SetKV("monitor-manually-stopped", false); err != nil {
			m.subsMu.Unlock()
			m.state.Logger.Error("保存监控启动状态失败: "+err.Error(), "monitor")
			return false
		}
	}
	m.state.SetMonitorStopped(false)
	if m.running {
		m.subsMu.Unlock()
		m.state.Logger.Warn("监控已在运行中", "monitor")
		return false
	}
	m.running = true
	m.generation++
	gen := m.generation
	// MonitorRunning 与 checkInterval 都要在锁内取/写:
	// Start 可能与 loop 自停时调的 Stop、以及 SetCheckInterval 并发,
	// 出锁之后再写就是无同步的写-写竞争(go test -race 会报)。
	m.state.MonitorRunning = true
	interval := m.checkInterval
	m.subsMu.Unlock()
	// 重置 TG 检查时间戳,保证启动后第一轮一定 verify
	m.tgCheckMu.Lock()
	m.lastTGCheck = time.Time{}
	m.tgCheckMu.Unlock()
	go m.monitorLoopGen(gen)
	if m.state.OrderPrewarm != nil {
		go m.syncPrewarm()
	}
	m.state.Logger.Info(fmt.Sprintf("服务器监控已启动 (检查间隔: %d秒)", interval), "monitor")
	return true
}

// Stop 只停止本轮进程,内部健康检查也会调用。
func (m *Monitor) Stop() bool {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	return m.stopLocked()
}

// StopByUser 将停止意图与循环停止串行化,避免并发启动覆盖持久状态。
func (m *Monitor) StopByUser() (bool, int, error) {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	m.state.SetMonitorStopped(true)
	stopped := m.stopLocked()
	count, pauseErr := m.state.PauseMonitorQueues(m.queueScopesLocked(true))
	if m.state.DB == nil {
		return stopped, count, errors.Join(pauseErr, fmt.Errorf("数据库不可用,不能保存停止状态"))
	}
	err := m.state.DB.SetKV("monitor-manually-stopped", true)
	return stopped, count, errors.Join(pauseErr, err)
}

// stopLocked 使循环和已取出的所有检查快照同时失效。
func (m *Monitor) stopLocked() bool {
	if !m.running {
		m.state.Logger.Warn("监控未运行", "monitor")
		return false
	}
	m.running = false
	if m.state.OrderPrewarm != nil {
		m.state.OrderPrewarm.CancelPrefix("monitor:")
		m.state.OrderPrewarm.CancelPrefix("notify:")
	}
	// 让正在等库存/询价结果的旧检查失效,快速重启也不能复活。
	for _, sub := range m.subscriptions {
		sub.mu.Lock()
		sub.revision++
		sub.mu.Unlock()
	}
	m.state.MonitorRunning = false
	m.state.Logger.Info("正在停止服务器监控...", "monitor")
	return true
}

// batchOrder 直接把已验证配置的整批任务交给共享队列,不再调用自身 HTTP 接口。
func (m *Monitor) batchOrder(planCode string, configInfo map[string]interface{}, targets []notification, quantity int, accountID string, shouldContinue func() bool) {
	if !shouldContinue() || accountID == "" {
		return
	}
	var options []string
	if configInfo != nil {
		if values, ok := configInfo["options"].([]string); ok {
			options = append([]string(nil), values...)
		} else if values, ok := configInfo["options"].([]interface{}); ok {
			for _, value := range values {
				if option, ok := value.(string); ok {
					options = append(options, option)
				}
			}
		}
	}
	dcs := make([]string, 0, len(targets))
	for _, target := range targets {
		dcs = append(dcs, target.dc)
	}
	if !shouldContinue() {
		return
	}
	duration, _ := configInfo["duration"].(string)
	if err := m.state.EnqueueMonitorOrders(accountID, planCode, dcs, options, quantity, duration); err != nil {
		m.state.Logger.Warn("[monitor->order] 入队未执行: "+err.Error(), "monitor")
	}
}

// AutoStartAllowed 重启遵守用户的全局停止记录,读库失败时不自动恢复。
func (m *Monitor) AutoStartAllowed() bool {
	if m.state.DB == nil {
		return false
	}
	var stopped bool
	if _, err := m.state.DB.GetKV("monitor-manually-stopped", &stopped); err != nil {
		m.state.Logger.Error("读取监控停止状态失败,不自动启动: "+err.Error(), "monitor")
		return false
	}
	return !stopped
}
