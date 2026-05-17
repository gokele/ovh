package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
	"github.com/ovh-buy/server/internal/vps"
)

// GetVPSSubscriptions GET /api/vps-monitor/subscriptions
func GetVPSSubscriptions(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.VPSSubsMu.Lock()
		defer state.VPSSubsMu.Unlock()
		if state.VPSSubscriptions == nil {
			c.JSON(http.StatusOK, []types.VPSSubscription{})
			return
		}
		// 保证每个 VPSSubscription 内部的 slice/map 字段不是 nil，
		// 否则前端调 .length 会爆（Python jsonify 在这点上等价于初始化为 []）
		for i := range state.VPSSubscriptions {
			if state.VPSSubscriptions[i].Datacenters == nil {
				state.VPSSubscriptions[i].Datacenters = []string{}
			}
			if state.VPSSubscriptions[i].History == nil {
				state.VPSSubscriptions[i].History = []map[string]interface{}{}
			}
			if state.VPSSubscriptions[i].LastStatus == nil {
				state.VPSSubscriptions[i].LastStatus = map[string]string{}
			}
		}
		c.JSON(http.StatusOK, state.VPSSubscriptions)
	}
}

// AddVPSSubscription POST /api/vps-monitor/subscriptions
func AddVPSSubscription(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		// VPS 监控同样要求 TG 通知可用
		if ok, reason := telegram.VerifyConfig(state); !ok {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Telegram 通知未配置或无效:" + reason})
			return
		}
		var body struct {
			PlanCode           string   `json:"planCode"`
			OvhSubsidiary      string   `json:"ovhSubsidiary"`
			Datacenters        []string `json:"datacenters"`
			MonitorLinux       *bool    `json:"monitorLinux"`
			MonitorWindows     *bool    `json:"monitorWindows"`
			NotifyAvailable    *bool    `json:"notifyAvailable"`
			NotifyUnavailable  *bool    `json:"notifyUnavailable"`
			AutoOrderAccountID string   `json:"autoOrderAccountId"` // 空 = 触发时只通知不下单
		}
		_ = c.ShouldBindJSON(&body)
		if body.PlanCode == "" {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "缺少planCode参数"})
			return
		}
		if body.AutoOrderAccountID != "" {
			if _, ok := state.FindAccount(body.AutoOrderAccountID); !ok {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "autoOrderAccountId 不存在"})
				return
			}
		}
		if body.OvhSubsidiary == "" {
			body.OvhSubsidiary = "IE"
		}
		monitorLinux := true
		if body.MonitorLinux != nil {
			monitorLinux = *body.MonitorLinux
		}
		monitorWindows := false
		if body.MonitorWindows != nil {
			monitorWindows = *body.MonitorWindows
		}
		notifyAvailable := true
		if body.NotifyAvailable != nil {
			notifyAvailable = *body.NotifyAvailable
		}
		notifyUnavailable := false
		if body.NotifyUnavailable != nil {
			notifyUnavailable = *body.NotifyUnavailable
		}

		state.VPSSubsMu.Lock()
		for _, s := range state.VPSSubscriptions {
			if s.PlanCode == body.PlanCode && s.OvhSubsidiary == body.OvhSubsidiary {
				state.VPSSubsMu.Unlock()
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "该VPS套餐已订阅"})
				return
			}
		}
		sub := types.VPSSubscription{
			ID:                 uuid.NewString(),
			PlanCode:           body.PlanCode,
			OvhSubsidiary:      body.OvhSubsidiary,
			Datacenters:        body.Datacenters,
			MonitorLinux:       monitorLinux,
			MonitorWindows:     monitorWindows,
			NotifyAvailable:    notifyAvailable,
			NotifyUnavailable:  notifyUnavailable,
			LastStatus:         map[string]string{},
			History:            []map[string]interface{}{},
			CreatedAt:          types.NowISO(),
			AutoOrderAccountID: body.AutoOrderAccountID,
		}
		state.VPSSubscriptions = append(state.VPSSubscriptions, sub)
		state.VPSSubsMu.Unlock()
		_ = vps.SaveSubscriptions(state)
		state.Logger.Info("添加VPS订阅: "+body.PlanCode+" (subsidiary: "+body.OvhSubsidiary+")", "vps_monitor")

		if !vps.Running() {
			vps.Start(state)
			state.Logger.Info("自动启动VPS监控", "vps_monitor")
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "已订阅 " + body.PlanCode, "subscription": sub})
	}
}

// RemoveVPSSubscription DELETE /api/vps-monitor/subscriptions/:subscription_id
func RemoveVPSSubscription(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("subscription_id")
		state.VPSSubsMu.Lock()
		original := len(state.VPSSubscriptions)
		kept := make([]types.VPSSubscription, 0, len(state.VPSSubscriptions))
		for _, s := range state.VPSSubscriptions {
			if s.ID != id {
				kept = append(kept, s)
			}
		}
		state.VPSSubscriptions = kept
		removed := len(state.VPSSubscriptions) < original
		empty := len(state.VPSSubscriptions) == 0
		state.VPSSubsMu.Unlock()
		if !removed {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "订阅不存在"})
			return
		}
		_ = vps.SaveSubscriptions(state)
		state.Logger.Info("删除VPS订阅: "+id, "vps_monitor")
		if empty && vps.Running() {
			vps.Stop(state)
			state.Logger.Info("所有订阅已删除，自动停止VPS监控", "vps_monitor")
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "订阅已删除"})
	}
}

// ClearVPSSubscriptions DELETE /api/vps-monitor/subscriptions/clear
func ClearVPSSubscriptions(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.VPSSubsMu.Lock()
		count := len(state.VPSSubscriptions)
		state.VPSSubscriptions = []types.VPSSubscription{}
		state.VPSSubsMu.Unlock()
		_ = vps.SaveSubscriptions(state)
		state.Logger.Info("清空所有VPS订阅 ("+strconv.Itoa(count)+" 项)", "vps_monitor")
		if vps.Running() {
			vps.Stop(state)
			state.Logger.Info("所有订阅已清空，自动停止VPS监控", "vps_monitor")
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "count": count, "message": "已清空 " + strconv.Itoa(count) + " 个订阅"})
	}
}

// GetVPSSubscriptionHistory GET /api/vps-monitor/subscriptions/:subscription_id/history
func GetVPSSubscriptionHistory(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("subscription_id")
		state.VPSSubsMu.Lock()
		var sub *types.VPSSubscription
		for i := range state.VPSSubscriptions {
			if state.VPSSubscriptions[i].ID == id {
				sub = &state.VPSSubscriptions[i]
				break
			}
		}
		state.VPSSubsMu.Unlock()
		if sub == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "订阅不存在"})
			return
		}
		hist := sub.History
		if hist == nil {
			hist = []map[string]interface{}{}
		}
		reversed := make([]map[string]interface{}, len(hist))
		for i, e := range hist {
			reversed[len(hist)-1-i] = e
		}
		c.JSON(http.StatusOK, reversed)
	}
}

// StartVPSMonitor POST /api/vps-monitor/start
func StartVPSMonitor(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		if vps.Running() {
			c.JSON(http.StatusOK, gin.H{"status": "info", "message": "VPS监控已在运行中"})
			return
		}
		if ok, reason := telegram.VerifyConfig(state); !ok {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Telegram 通知未配置或无效,无法启动 VPS 监控:" + reason})
			return
		}
		vps.Start(state)
		state.Logger.Info("VPS监控已启动", "vps_monitor")
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "VPS监控已启动"})
	}
}

// StopVPSMonitor POST /api/vps-monitor/stop
func StopVPSMonitor(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !vps.Running() {
			c.JSON(http.StatusOK, gin.H{"status": "info", "message": "VPS监控未运行"})
			return
		}
		vps.Stop(state)
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "VPS监控已停止"})
	}
}

// GetVPSMonitorStatus GET /api/vps-monitor/status
func GetVPSMonitorStatus(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.VPSSubsMu.Lock()
		count := len(state.VPSSubscriptions)
		interval := state.VPSCheckInterval
		state.VPSSubsMu.Unlock()
		c.JSON(http.StatusOK, gin.H{
			"running":              vps.Running(),
			"subscriptions_count":  count,
			"check_interval":       interval,
		})
	}
}

// SetVPSMonitorInterval PUT /api/vps-monitor/interval
func SetVPSMonitorInterval(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Interval int `json:"interval"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Interval < 60 {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "间隔不能小于60秒"})
			return
		}
		state.VPSSubsMu.Lock()
		state.VPSCheckInterval = body.Interval
		state.VPSSubsMu.Unlock()
		_ = vps.SaveSubscriptions(state)
		state.Logger.Info("VPS检查间隔已设置为 "+strconv.Itoa(body.Interval)+" 秒", "vps_monitor")
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "检查间隔已设置为 " + strconv.Itoa(body.Interval) + " 秒"})
	}
}

// ManualCheckVPS POST /api/vps-monitor/check/:plan_code
func ManualCheckVPS(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		planCode := c.Param("plan_code")
		var body struct {
			OvhSubsidiary string `json:"ovhSubsidiary"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.OvhSubsidiary == "" {
			body.OvhSubsidiary = "IE"
		}
		result := vps.CheckVPSDCAvailability(state, planCode, body.OvhSubsidiary)
		if result == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "获取VPS数据中心信息失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "data": result})
	}
}
