package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// GetSubscriptions GET /api/monitor/subscriptions
func GetSubscriptions(state *app.State, mon *monitor.Monitor) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, mon.Snapshot())
	}
}

// AddSubscription POST /api/monitor/subscriptions
func AddSubscription(state *app.State, mon *monitor.Monitor) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			PlanCode          string   `json:"planCode"`
			Datacenters       []string `json:"datacenters"`
			NotifyAvailable   *bool    `json:"notifyAvailable"`
			NotifyUnavailable *bool    `json:"notifyUnavailable"`
			AutoOrder         bool     `json:"autoOrder"`
			Quantity          int      `json:"quantity"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.PlanCode == "" {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "缺少planCode参数"})
			return
		}
		notifyAvailable := true
		notifyUnavailable := false
		if body.NotifyAvailable != nil {
			notifyAvailable = *body.NotifyAvailable
		}
		if body.NotifyUnavailable != nil {
			notifyUnavailable = *body.NotifyUnavailable
		}
		if body.Quantity < 1 {
			body.Quantity = 1
		}

		var serverName string
		state.ServerPlansMu.RLock()
		for _, s := range state.ServerPlans {
			if s.PlanCode == body.PlanCode {
				serverName = s.Name
				state.Logger.Info("找到服务器名称: "+serverName+" ("+body.PlanCode+")", "monitor")
				break
			}
		}
		state.ServerPlansMu.RUnlock()
		if serverName == "" {
			state.Logger.Warn("未找到服务器 "+body.PlanCode+" 的名称信息", "monitor")
		}

		mon.AddSubscription(body.PlanCode, body.Datacenters, notifyAvailable, notifyUnavailable,
			serverName, nil, nil, body.AutoOrder, body.Quantity)
		mon.SaveToDB()

		if !mon.Running() {
			mon.Start()
			state.Logger.Info("添加订阅后自动启动监控", "")
		}
		nameDisplay := serverName
		if nameDisplay == "" {
			nameDisplay = "未知名称"
		}
		state.Logger.Info("添加服务器订阅: "+body.PlanCode+" ("+nameDisplay+")", "")
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "已订阅 " + body.PlanCode})
	}
}

// BatchAddAll POST /api/monitor/subscriptions/batch-add-all
func BatchAddAll(state *app.State, mon *monitor.Monitor) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.ServerPlansMu.RLock()
		hasServers := len(state.ServerPlans) > 0
		state.ServerPlansMu.RUnlock()
		if !hasServers {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "服务器列表为空，请先刷新服务器列表"})
			return
		}

		var body struct {
			NotifyAvailable   *bool `json:"notifyAvailable"`
			NotifyUnavailable *bool `json:"notifyUnavailable"`
			AutoOrder         bool  `json:"autoOrder"`
		}
		_ = c.ShouldBindJSON(&body)
		notifyAvailable := true
		notifyUnavailable := false
		if body.NotifyAvailable != nil {
			notifyAvailable = *body.NotifyAvailable
		}
		if body.NotifyUnavailable != nil {
			notifyUnavailable = *body.NotifyUnavailable
		}

		existing := map[string]struct{}{}
		for _, s := range mon.Snapshot() {
			existing[s.PlanCode] = struct{}{}
		}

		added := 0
		skipped := 0
		errs := []string{}
		state.ServerPlansMu.RLock()
		plansCopy := make([]types.ServerPlan, len(state.ServerPlans))
		copy(plansCopy, state.ServerPlans)
		state.ServerPlansMu.RUnlock()

		for _, server := range plansCopy {
			pc := server.PlanCode
			if pc == "" {
				continue
			}
			if _, ok := existing[pc]; ok {
				skipped++
				continue
			}
			mon.AddSubscription(pc, []string{}, notifyAvailable, notifyUnavailable,
				server.Name, nil, nil, body.AutoOrder, 1)
			added++
			state.Logger.Debug("批量添加订阅: "+pc+" ("+server.Name+")", "monitor")
		}
		mon.SaveToDB()
		if !mon.Running() {
			mon.Start()
			state.Logger.Info("批量添加订阅后自动启动监控", "monitor")
		}

		message := "已添加 " + strconv.Itoa(added) + " 个服务器到监控（全机房监控）"
		if skipped > 0 {
			message += "，跳过 " + strconv.Itoa(skipped) + " 个已订阅的服务器"
		}
		if len(errs) > 0 {
			message += "，" + strconv.Itoa(len(errs)) + " 个失败"
		}
		state.Logger.Info("批量添加订阅完成: "+message, "monitor")
		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"added":   added,
			"skipped": skipped,
			"errors":  errs,
			"message": message,
		})
	}
}

// RemoveSubscription DELETE /api/monitor/subscriptions/:planCode
func RemoveSubscription(state *app.State, mon *monitor.Monitor) gin.HandlerFunc {
	return func(c *gin.Context) {
		planCode := c.Param("planCode")
		if mon.RemoveSubscription(planCode) {
			mon.SaveToDB()
			state.Logger.Info("删除服务器订阅: "+planCode, "")
			c.JSON(http.StatusOK, gin.H{"status": "success", "message": "已取消订阅 " + planCode})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "订阅不存在"})
	}
}

// ClearSubscriptions DELETE /api/monitor/subscriptions/clear
func ClearSubscriptions(state *app.State, mon *monitor.Monitor) gin.HandlerFunc {
	return func(c *gin.Context) {
		count := mon.ClearSubscriptions()
		mon.SaveToDB()
		state.Logger.Info("清空所有订阅 ("+strconv.Itoa(count)+" 项)", "")
		c.JSON(http.StatusOK, gin.H{"status": "success", "count": count, "message": "已清空 " + strconv.Itoa(count) + " 个订阅"})
	}
}

// GetSubscriptionHistory GET /api/monitor/subscriptions/:planCode/history
// 返回该订阅的历史记录数组（倒序，最新在前）。
func GetSubscriptionHistory(state *app.State, mon *monitor.Monitor) gin.HandlerFunc {
	return func(c *gin.Context) {
		planCode := c.Param("planCode")
		sub := mon.FindSubscription(planCode)
		if sub == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "订阅不存在"})
			return
		}
		history := sub.History
		if history == nil {
			history = []monitor.HistoryEntry{}
		}
		reversed := make([]monitor.HistoryEntry, len(history))
		for i, e := range history {
			reversed[len(history)-1-i] = e
		}
		c.JSON(http.StatusOK, reversed)
	}
}

// StartMonitor POST /api/monitor/start
func StartMonitor(state *app.State, mon *monitor.Monitor) gin.HandlerFunc {
	return func(c *gin.Context) {
		if mon.Start() {
			state.Logger.Info("用户启动服务器监控", "")
			c.JSON(http.StatusOK, gin.H{"status": "success", "message": "监控已启动"})
		} else {
			c.JSON(http.StatusOK, gin.H{"status": "info", "message": "监控已在运行中"})
		}
	}
}

// StopMonitor POST /api/monitor/stop
func StopMonitor(state *app.State, mon *monitor.Monitor) gin.HandlerFunc {
	return func(c *gin.Context) {
		if mon.Stop() {
			state.Logger.Info("用户停止服务器监控", "")
			c.JSON(http.StatusOK, gin.H{"status": "success", "message": "监控已停止"})
		} else {
			c.JSON(http.StatusOK, gin.H{"status": "info", "message": "监控未运行"})
		}
	}
}

// GetMonitorStatus GET /api/monitor/status
func GetMonitorStatus(state *app.State, mon *monitor.Monitor) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, mon.Status())
	}
}

// SetMonitorInterval PUT /api/monitor/interval
func SetMonitorInterval(state *app.State, mon *monitor.Monitor) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "info", "message": "检查间隔已全局固定为5秒，无法修改"})
	}
}

// TestNotification POST /api/monitor/test-notification
func TestNotification(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		msg := "🔔 服务器监控测试通知\n\n时间: " + time.Now().Format("2006-01-02 15:04:05") + "\n\n✅ Telegram通知配置正常！"
		if telegram.SendMessage(state, msg, nil) {
			state.Logger.Info("Telegram测试通知发送成功", "monitor")
			c.JSON(http.StatusOK, gin.H{"status": "success", "message": "测试通知已发送，请检查Telegram"})
		} else {
			state.Logger.Warn("Telegram测试通知发送失败", "monitor")
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "发送失败，请检查Telegram配置和日志"})
		}
	}
}
