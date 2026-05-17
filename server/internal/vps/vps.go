package vps

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/numconv"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

var (
	runningMu sync.Mutex
	running   bool
)

// CheckVPSDCAvailability 对应 Python: check_vps_datacenter_availability
func CheckVPSDCAvailability(state *app.State, planCode, ovhSubsidiary string) map[string]interface{} {
	baseURL := state.Config.APIBaseURL()
	u := baseURL + "/v1/vps/order/rule/datacenter"
	params := url.Values{}
	params.Set("ovhSubsidiary", ovhSubsidiary)
	params.Set("planCode", planCode)
	fullURL := u + "?" + params.Encode()

	state.Logger.Info(fmt.Sprintf("检查VPS可用性: %s (subsidiary: %s)", planCode, ovhSubsidiary), "vps_monitor")

	req, _ := http.NewRequest(http.MethodGet, fullURL, nil)
	req.Header.Set("accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		state.Logger.Error("检查VPS可用性时出错: "+err.Error(), "vps_monitor")
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		state.Logger.Error(fmt.Sprintf("获取VPS数据中心信息失败: HTTP %d", resp.StatusCode), "vps_monitor")
		return nil
	}
	var data map[string]interface{}
	_ = json.Unmarshal(body, &data)
	state.Logger.Info("VPS "+planCode+" 数据中心信息获取成功", "vps_monitor")
	return data
}

// SaveSubscriptions 把订阅 + check_interval 写回 SQLite
func SaveSubscriptions(state *app.State) error {
	state.VPSSubsMu.Lock()
	subs := make([]types.VPSSubscription, len(state.VPSSubscriptions))
	copy(subs, state.VPSSubscriptions)
	interval := state.VPSCheckInterval
	state.VPSSubsMu.Unlock()
	if err := state.DB.ReplaceVPSSubscriptions(subs); err != nil {
		state.Logger.Error("保存VPS订阅时出错: "+err.Error(), "")
		return err
	}
	if err := state.DB.SetKV("vps_check_interval", interval); err != nil {
		state.Logger.Error("保存VPS检查间隔时出错: "+err.Error(), "")
		return err
	}
	state.Logger.Info(fmt.Sprintf("已保存 %d 个VPS订阅", len(subs)), "")
	return nil
}

var vpsModelMap = map[string]string{
	"vps-2025-model1": "VPS-1",
	"vps-2025-model2": "VPS-2",
	"vps-2025-model3": "VPS-3",
	"vps-2025-model4": "VPS-4",
	"vps-2025-model5": "VPS-5",
	"vps-2025-model6": "VPS-6",
}

var statusMap = map[string]string{
	"available":                       "现货",
	"out-of-stock":                    "无货",
	"out-of-stock-preorder-allowed":   "缺货（可预订）",
	"unavailable":                     "不可用",
	"unknown":                         "未知",
}

// SendSummaryNotification 对应 Python: send_vps_summary_notification
func SendSummaryNotification(state *app.State, planCode string, dcs []map[string]interface{}, changeType string) bool {
	cfg := state.Config.Get()
	if cfg.TgToken == "" || cfg.TgChatID == "" || len(dcs) == 0 {
		return false
	}
	planDisplay, ok := vpsModelMap[planCode]
	if !ok {
		planDisplay = planCode
	}
	var emoji, title string
	switch changeType {
	case "initial":
		emoji, title = "📊", "VPS初始状态"
	case "available":
		emoji, title = "🎉", "VPS补货通知"
	default:
		emoji, title = "📦", "VPS下架通知"
	}
	var sb strings.Builder
	sb.WriteString(emoji + " " + title + "\n\n")
	sb.WriteString("套餐: " + planDisplay + "\n")
	sb.WriteString("时间: " + time.Now().Format("2006-01-02 15:04:05") + "\n\n")
	for idx, dc := range dcs {
		st, _ := dc["status"].(string)
		statusCN, ok := statusMap[st]
		if !ok {
			statusCN = st
		}
		name, _ := dc["name"].(string)
		code, _ := dc["code"].(string)
		sb.WriteString(fmt.Sprintf("%d. %s (%s)\n   状态: %s", idx+1, name, code, statusCN))
		if days, ok := numconv.ToInt64(dc["days"]); ok && days > 0 {
			sb.WriteString(fmt.Sprintf(" | 预计交付: %d天", days))
		}
		sb.WriteString("\n")
	}
	if changeType == "available" {
		sb.WriteString("\n💡 快去抢购吧！")
	}
	result := telegram.SendMessage(state, sb.String(), nil)
	if result {
		state.Logger.Info(fmt.Sprintf("✅ VPS汇总通知发送成功: %s (%d个机房)", planCode, len(dcs)), "vps_monitor")
	} else {
		state.Logger.Warn(fmt.Sprintf("⚠️ VPS汇总通知发送失败: %s", planCode), "vps_monitor")
	}
	return result
}

// MonitorLoop 对应 Python: vps_monitor_loop
func MonitorLoop(state *app.State) {
	state.Logger.Info("VPS监控循环已启动", "vps_monitor")
	for {
		runningMu.Lock()
		isRunning := running
		runningMu.Unlock()
		if !isRunning {
			break
		}

		state.VPSSubsMu.Lock()
		subs := make([]types.VPSSubscription, len(state.VPSSubscriptions))
		copy(subs, state.VPSSubscriptions)
		interval := state.VPSCheckInterval
		state.VPSSubsMu.Unlock()

		if len(subs) > 0 {
			state.Logger.Info(fmt.Sprintf("开始检查 %d 个VPS订阅...", len(subs)), "vps_monitor")
			for idx := range subs {
				runningMu.Lock()
				isRunning = running
				runningMu.Unlock()
				if !isRunning {
					break
				}
				sub := &subs[idx]
				ovhSub := sub.OvhSubsidiary
				if ovhSub == "" {
					ovhSub = "IE"
				}
				currentData := CheckVPSDCAvailability(state, sub.PlanCode, ovhSub)
				if currentData == nil {
					state.Logger.Warn("无法获取VPS "+sub.PlanCode+" 的数据中心信息", "vps_monitor")
					continue
				}
				dcsRaw, _ := currentData["datacenters"].([]interface{})
				if sub.LastStatus == nil {
					sub.LastStatus = map[string]string{}
				}
				lastStatus := sub.LastStatus
				monitoredDCs := sub.Datacenters

				initialAvailable := []map[string]interface{}{}
				newAvailable := []map[string]interface{}{}
				newUnavailable := []map[string]interface{}{}
				isFirstCheckOverall := len(lastStatus) == 0

				for _, dcRaw := range dcsRaw {
					dc, ok := dcRaw.(map[string]interface{})
					if !ok {
						continue
					}
					code, _ := dc["code"].(string)
					name, _ := dc["datacenter"].(string)
					currentStatus, _ := dc["status"].(string)
					daysI64, _ := numconv.ToInt64(dc["daysBeforeDelivery"])
					days := int(daysI64)

					if len(monitoredDCs) > 0 {
						found := false
						for _, m := range monitoredDCs {
							if m == code {
								found = true
								break
							}
						}
						if !found {
							continue
						}
					}
					oldStatus, hasOld := lastStatus[code]
					if !hasOld {
						initialAvailable = append(initialAvailable, map[string]interface{}{
							"name":   name,
							"code":   code,
							"status": currentStatus,
							"days":   days,
						})
						if currentStatus != "out-of-stock" && currentStatus != "out-of-stock-preorder-allowed" {
							sub.History = append(sub.History, map[string]interface{}{
								"timestamp":      time.Now().Format(time.RFC3339Nano),
								"datacenter":     name,
								"datacenterCode": code,
								"status":         currentStatus,
								"changeType":     "available",
								"oldStatus":      nil,
							})
						}
					} else {
						wasUnavail := oldStatus == "out-of-stock" || oldStatus == "out-of-stock-preorder-allowed"
						isUnavail := currentStatus == "out-of-stock" || currentStatus == "out-of-stock-preorder-allowed"
						if wasUnavail && !isUnavail {
							newAvailable = append(newAvailable, map[string]interface{}{
								"name":   name,
								"code":   code,
								"status": currentStatus,
								"days":   days,
							})
							sub.History = append(sub.History, map[string]interface{}{
								"timestamp":      time.Now().Format(time.RFC3339Nano),
								"datacenter":     name,
								"datacenterCode": code,
								"status":         currentStatus,
								"changeType":     "available",
								"oldStatus":      oldStatus,
							})
						} else if !wasUnavail && isUnavail {
							newUnavailable = append(newUnavailable, map[string]interface{}{
								"name":   name,
								"code":   code,
								"status": currentStatus,
								"days":   days,
							})
							sub.History = append(sub.History, map[string]interface{}{
								"timestamp":      time.Now().Format(time.RFC3339Nano),
								"datacenter":     name,
								"datacenterCode": code,
								"status":         currentStatus,
								"changeType":     "unavailable",
								"oldStatus":      oldStatus,
							})
						}
					}
					lastStatus[code] = currentStatus
				}

				if isFirstCheckOverall && len(initialAvailable) > 0 && sub.NotifyAvailable {
					state.Logger.Info(fmt.Sprintf("VPS %s 初始状态检查完成，%d个数据中心", sub.PlanCode, len(initialAvailable)), "vps_monitor")
					SendSummaryNotification(state, sub.PlanCode, initialAvailable, "initial")
				} else {
					if len(newAvailable) > 0 && sub.NotifyAvailable {
						state.Logger.Info(fmt.Sprintf("VPS %s 补货：%d个数据中心", sub.PlanCode, len(newAvailable)), "vps_monitor")
						SendSummaryNotification(state, sub.PlanCode, newAvailable, "available")
					}
					if len(newUnavailable) > 0 && sub.NotifyUnavailable {
						state.Logger.Info(fmt.Sprintf("VPS %s 下架：%d个数据中心", sub.PlanCode, len(newUnavailable)), "vps_monitor")
						SendSummaryNotification(state, sub.PlanCode, newUnavailable, "unavailable")
					}
				}

				sub.LastStatus = lastStatus
				if len(sub.History) > 100 {
					sub.History = sub.History[len(sub.History)-100:]
				}
				time.Sleep(time.Second)
			}
			// 按 ID 合并写回：保留循环中对 LastStatus/History 的更新，不覆盖循环期间用户新增/删除的订阅
			state.VPSSubsMu.Lock()
			byID := map[string]*types.VPSSubscription{}
			for i := range subs {
				byID[subs[i].ID] = &subs[i]
			}
			for i := range state.VPSSubscriptions {
				if updated, ok := byID[state.VPSSubscriptions[i].ID]; ok {
					state.VPSSubscriptions[i].LastStatus = updated.LastStatus
					state.VPSSubscriptions[i].History = updated.History
				}
			}
			state.VPSSubsMu.Unlock()
			_ = SaveSubscriptions(state)
		} else {
			state.Logger.Info("当前无VPS订阅，跳过检查", "vps_monitor")
		}

		runningMu.Lock()
		isRunning = running
		runningMu.Unlock()
		if isRunning {
			state.Logger.Info(fmt.Sprintf("等待 %d 秒后进行下次VPS检查...", interval), "vps_monitor")
			for i := 0; i < interval; i++ {
				runningMu.Lock()
				isRunning = running
				runningMu.Unlock()
				if !isRunning {
					break
				}
				time.Sleep(time.Second)
			}
		}
	}
	state.Logger.Info("VPS监控循环已停止", "vps_monitor")
}

// Start 启动监控
func Start(state *app.State) bool {
	runningMu.Lock()
	if running {
		runningMu.Unlock()
		return false
	}
	running = true
	runningMu.Unlock()
	go MonitorLoop(state)
	state.Logger.Info(fmt.Sprintf("VPS监控已启动 (检查间隔: %d秒)", state.VPSCheckInterval), "vps_monitor")
	return true
}

// Stop 停止监控
func Stop(state *app.State) bool {
	runningMu.Lock()
	if !running {
		runningMu.Unlock()
		return false
	}
	running = false
	runningMu.Unlock()
	state.Logger.Info("正在停止VPS监控...", "vps_monitor")
	return true
}

// Running 返回是否在运行
func Running() bool {
	runningMu.Lock()
	defer runningMu.Unlock()
	return running
}
