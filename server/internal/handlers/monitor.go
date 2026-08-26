package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/notify"
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
		// 监控订阅必须有可用的 Telegram 通知,否则没意义
		if ok, reason := notify.AnyAvailable(state, true); !ok {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "没有可用的通知通道(Telegram / Webhook 至少配一个):" + reason})
			return
		}
		var body struct {
			PlanCode           string   `json:"planCode"`
			Datacenters        []string `json:"datacenters"`
			NotifyAvailable    *bool    `json:"notifyAvailable"`
			NotifyUnavailable  *bool    `json:"notifyUnavailable"`
			AutoOrder          bool     `json:"autoOrder"`
			Quantity           int      `json:"quantity"`
			AutoOrderAccountID string   `json:"autoOrderAccountId"` // 空 = 触发时只通知不下单
			// AutoPay 下单成功后用默认支付方式自动付款。默认 false ——
			// 自动扣钱必须是显式打开的开关
			AutoPay bool `json:"autoPay"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.PlanCode == "" {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "缺少planCode参数"})
			return
		}
		// 校验 auto_order_account_id 引用的账户真的存在
		if body.AutoOrderAccountID != "" {
			if _, ok := state.FindAccount(body.AutoOrderAccountID); !ok {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "autoOrderAccountId 不存在"})
				return
			}
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

		// 区域预检:OVH 的 EU / US / CA 是三套独立系统,拿错站点查 planCode 返回的是
		// 200 + 空数组而不是报错。订阅一个没人能查的机型会静默失效,所以下单前就告诉用户。
		// 只提示不拦截 —— 目录有 2 小时缓存、OVH 也会抖动,不能凭一次探测把人挡在门外。
		region, subsidiary, regionWarning := mon.PreflightRegion(body.PlanCode, body.AutoOrderAccountID)

		mon.AddSubscription(body.PlanCode, body.Datacenters, notifyAvailable, notifyUnavailable,
			serverName, nil, nil, body.AutoOrder, body.Quantity, body.AutoOrderAccountID, body.AutoPay)
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
		resp := gin.H{"status": "success", "message": "已订阅 " + body.PlanCode}
		if region != "" {
			resp["region"] = region
			resp["subsidiary"] = subsidiary
			resp["message"] = "已订阅 " + body.PlanCode + "（由 " + region + " 站点账户监控）"
		}
		if regionWarning != "" {
			state.Logger.Warn("订阅区域预检告警: "+body.PlanCode+" - "+regionWarning, "monitor")
			resp["status"] = "warning"
			resp["regionWarning"] = regionWarning
		}
		c.JSON(http.StatusOK, resp)
	}
}

// BatchAddAll POST /api/monitor/subscriptions/batch-add-all
func BatchAddAll(state *app.State, mon *monitor.Monitor) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 同 AddSubscription:批量添加也要求 TG 有效
		if ok, reason := notify.AnyAvailable(state, true); !ok {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "没有可用的通知通道(Telegram / Webhook 至少配一个):" + reason})
			return
		}
		state.ServerPlansMu.RLock()
		hasServers := len(state.ServerPlans) > 0
		state.ServerPlansMu.RUnlock()
		if !hasServers {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "服务器列表为空，请先刷新服务器列表"})
			return
		}

		var body struct {
			NotifyAvailable    *bool  `json:"notifyAvailable"`
			NotifyUnavailable  *bool  `json:"notifyUnavailable"`
			AutoOrder          bool   `json:"autoOrder"`
			AutoOrderAccountID string `json:"autoOrderAccountId"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.AutoOrderAccountID != "" {
			if _, ok := state.FindAccount(body.AutoOrderAccountID); !ok {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "autoOrderAccountId 不存在"})
				return
			}
		}
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
				server.Name, nil, nil, body.AutoOrder, 1, body.AutoOrderAccountID, false)
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
		// 启动前先验 TG,broken TG 不让起,免得起来一圈检查发不出去白跑
		if ok, reason := notify.AnyAvailable(state, true); !ok {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Telegram 通知未配置或无效,无法启动监控:" + reason})
			return
		}
		if mon.Start() {
			state.Logger.Info("用户启动服务器监控", "")
			c.JSON(http.StatusOK, gin.H{"status": "success", "message": "监控已启动"})
		} else {
			c.JSON(http.StatusOK, gin.H{"status": "info", "message": "监控已在运行中"})
		}
	}
}

// VerifyTelegram GET /api/telegram/verify
// 前端添加订阅对话框打开时调一次,决定是否允许提交。
// 后端不缓存,每次请求都真去 telegram API 探一下,前端 React Query 控频率(5min staleTime)。
func VerifyTelegram(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, reason := telegram.VerifyConfig(state)
		c.JSON(http.StatusOK, gin.H{"ok": ok, "reason": reason})
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
		var body struct {
			Interval      *int `json:"interval"`
			CheckInterval *int `json:"check_interval"` // 前端历史字段名,一并收
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "请求体格式错误"})
			return
		}
		v := body.Interval
		if v == nil {
			v = body.CheckInterval
		}
		if v == nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "缺少 interval 参数"})
			return
		}
		applied := mon.SetCheckInterval(*v)
		mon.SaveToDB()
		msg := fmt.Sprintf("检查间隔已设置为 %d 秒", applied)
		if applied != *v {
			msg = fmt.Sprintf("检查间隔已调整为 %d 秒(合法范围 %d-%d 秒)", applied, monitor.MinCheckInterval, monitor.MaxCheckInterval)
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": msg, "check_interval": applied})
	}
}

// TestNotification POST /api/monitor/test-notification
// 逐条通道发测试消息并返回结果 —— 只说"发送失败"没用,用户需要知道是哪条挂了。
func TestNotification(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		msg := "🔔 OVH 控制台通知测试\n\n时间: " + time.Now().Format("2006-01-02 15:04:05") +
			"\n\n收到这条说明该通道可用。"
		delivered := notify.Broadcast(state, msg, nil)
		chans := notify.Status(state, false)
		if delivered > 0 {
			state.Logger.Info(fmt.Sprintf("测试通知已送达 %d 个通道", delivered), "monitor")
			c.JSON(http.StatusOK, gin.H{
				"status": "success", "delivered": delivered, "channels": chans,
				"message": fmt.Sprintf("已发往 %d 个通道，请检查是否收到", delivered),
			})
			return
		}
		state.Logger.Warn("测试通知一个通道都没送达", "monitor")
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error", "delivered": 0, "channels": chans,
			"message": "没有任何通道送达。请在设置页配置 Telegram 或 Webhook",
		})
	}
}

// GetNotifyChannels GET /api/notify/channels
// 通知通道体检。?verify=true 会真的去调远端(Telegram getMe / 向 webhook 发一条测试)
func GetNotifyChannels(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		verify := strings.EqualFold(c.Query("verify"), "true")
		chans := notify.Status(state, verify)
		any := false
		for _, ch := range chans {
			if ch.Configured && ch.OK {
				any = true
			}
		}
		c.JSON(http.StatusOK, gin.H{"channels": chans, "anyAvailable": any, "verified": verify})
	}
}
