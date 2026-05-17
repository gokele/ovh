package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/telegram"
)

// tgRecheckInterval loop 内 TG 健康检查节流间隔。5 分钟 verify 一次,
// 失败立即调 Stop() 自停监控。
const tgRecheckInterval = 5 * time.Minute

// checkTGOrStop 节流后 verify Telegram,失败则调 Stop() 让 loop 退出。
// 返回 true=继续 loop,false=已自停,loop 应该 break。
func (m *Monitor) checkTGOrStop() bool {
	m.tgCheckMu.Lock()
	due := time.Since(m.lastTGCheck) >= tgRecheckInterval
	m.tgCheckMu.Unlock()
	if !due {
		return true
	}
	ok, reason := telegram.VerifyConfig(m.state)
	m.tgCheckMu.Lock()
	m.lastTGCheck = time.Now()
	m.tgCheckMu.Unlock()
	if !ok {
		m.state.Logger.Error("Telegram 通知失效,自动停止服务器监控: "+reason, "monitor")
		m.Stop()
		return false
	}
	return true
}

// CheckNewServers 对应 Python: check_new_servers
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

// runSubscriptionCheck 对应 Python: _run_subscription_check
func (m *Monitor) runSubscriptionCheck(sub *Subscription, traceID string) {
	planCode := sub.PlanCode
	m.state.Logger.Info("开始处理订阅: "+planCode, "monitor")
	m.CheckAvailabilityChange(sub, traceID)
	m.state.Logger.Info("完成处理订阅: "+planCode, "monitor")
}

// monitorLoop 对应 Python: monitor_loop
func (m *Monitor) monitorLoop() {
	m.state.Logger.Info("监控循环已启动", "monitor")
	for {
		m.subsMu.Lock()
		running := m.running
		m.subsMu.Unlock()
		if !running {
			break
		}

		// TG 失效 → 自停。checkTGOrStop 内部已节流 5 分钟,且失败时调 Stop()。
		if !m.checkTGOrStop() {
			break
		}

		m.cleanupExpiredCaches()

		m.subsMu.Lock()
		count := len(m.subscriptions)
		subsCopy := make([]*Subscription, count)
		copy(subsCopy, m.subscriptions)
		interval := m.checkInterval
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
				m.subsMu.Lock()
				running := m.running
				m.subsMu.Unlock()
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
		} else {
			m.state.Logger.Info("当前无订阅，跳过检查", "monitor")
		}

		// 等下次（可中断 sleep）
		m.subsMu.Lock()
		running = m.running
		m.subsMu.Unlock()
		if running {
			m.state.Logger.Info(fmt.Sprintf("等待 %d 秒后进行下次检查...", interval), "monitor")
			for i := 0; i < interval; i++ {
				m.subsMu.Lock()
				running = m.running
				m.subsMu.Unlock()
				if !running {
					break
				}
				time.Sleep(time.Second)
			}
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

// Start 对应 Python: start
func (m *Monitor) Start() bool {
	m.subsMu.Lock()
	if m.running {
		m.subsMu.Unlock()
		m.state.Logger.Warn("监控已在运行中", "monitor")
		return false
	}
	m.running = true
	m.subsMu.Unlock()
	// 重置 TG 检查时间戳,保证启动后第一轮一定 verify
	m.tgCheckMu.Lock()
	m.lastTGCheck = time.Time{}
	m.tgCheckMu.Unlock()
	go m.monitorLoop()
	m.state.Logger.Info(fmt.Sprintf("服务器监控已启动 (检查间隔: %d秒)", m.checkInterval), "monitor")
	m.state.MonitorRunning = true
	return true
}

// Stop 对应 Python: stop
func (m *Monitor) Stop() bool {
	m.subsMu.Lock()
	if !m.running {
		m.subsMu.Unlock()
		m.state.Logger.Warn("监控未运行", "monitor")
		return false
	}
	m.running = false
	m.subsMu.Unlock()
	m.state.Logger.Info("正在停止服务器监控...", "monitor")
	m.state.MonitorRunning = false
	return true
}

// batchOrder 对应 Python: 监控->下单批量调用 quick-order。
// accountID:auto_order 账户;空时 batchOrder 不应该被调到(check.go 的 guard 已挡住),
// 这里再做一次防御性检查。
func (m *Monitor) batchOrder(planCode string, configInfo map[string]interface{}, targets []notification, quantity int, accountID string) {
	if accountID == "" {
		m.state.Logger.Warn("[monitor->order] 跳过自动下单: 订阅未指定 auto_order 账户", "monitor")
		return
	}
	if quantity < 0 {
		quantity = 0
	}
	totalOrders := len(targets) * quantity
	m.state.Logger.Info(fmt.Sprintf("[monitor->order] 开始批量下单: %s, 配置数=1, 数据中心数=%d, 数量=%d, 总订单数=%d",
		planCode, len(targets), quantity, totalOrders), "monitor")
	m.state.Logger.Info("[monitor->order] 下单条件：仅对从无货变有货的情况下单（过滤掉持续有货的情况）", "monitor")

	options := []string{}
	if configInfo != nil {
		if opts, ok := configInfo["options"].([]string); ok {
			options = opts
		} else if optsRaw, ok := configInfo["options"].([]interface{}); ok {
			for _, o := range optsRaw {
				if s, ok := o.(string); ok {
					options = append(options, s)
				}
			}
		}
	}

	successCount := 0
	failCount := 0

	for _, n := range targets {
		for i := 0; i < quantity; i++ {
			payload := map[string]interface{}{
				"account_id":         accountID,
				"planCode":           planCode,
				"datacenter":         n.dc,
				"options":            options,
				"fromMonitor":        true,
				"skipDuplicateCheck": true,
			}
			body, _ := json.Marshal(payload)
			req, _ := http.NewRequest(http.MethodPost,
				"http://127.0.0.1:"+m.state.Port+"/api/queue/quick-order",
				bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", m.state.APIKey)

			m.state.Logger.Info(fmt.Sprintf("[monitor->order] 尝试快速下单 (%d/%d): %s@%s, options=%v",
				i+1, quantity, planCode, n.dc, options), "monitor")

			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				failCount++
				m.state.Logger.Warn(fmt.Sprintf("[monitor->order] 快速下单请求异常 (%d/%d): %s",
					i+1, quantity, err.Error()), "monitor")
				continue
			}
			respBody := make([]byte, 0, 1024)
			buf := make([]byte, 1024)
			for {
				// 避免与外层 notification n 同名困扰阅读
				nr, rerr := resp.Body.Read(buf)
				if nr > 0 {
					respBody = append(respBody, buf[:nr]...)
				}
				if rerr != nil {
					break
				}
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				successCount++
				m.state.Logger.Info(fmt.Sprintf("[monitor->order] 快速下单成功 (%d/%d): %s@%s",
					i+1, quantity, planCode, n.dc), "monitor")
			} else {
				failCount++
				m.state.Logger.Warn(fmt.Sprintf("[monitor->order] 快速下单失败 (%d/%d, %d): %s",
					i+1, quantity, resp.StatusCode, string(respBody)), "monitor")
			}
		}
	}
	m.state.Logger.Info(fmt.Sprintf("[monitor->order] 批量下单完成: 成功=%d, 失败=%d, 总计=%d",
		successCount, failCount, totalOrders), "monitor")

	_ = bytes.NewReader(nil) // 保持 import bytes 使用
}
