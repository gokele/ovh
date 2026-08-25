package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
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
		// 否则前端调 .length 会爆
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
		var autoOrderAccount types.OVHAccount
		if body.AutoOrderAccountID != "" {
			acc, ok := state.FindAccount(body.AutoOrderAccountID)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "autoOrderAccountId 不存在"})
				return
			}
			autoOrderAccount = acc
		}

		// 子公司决定连哪个站点(EU / US / CA 三套独立系统),必须先归一化再校验:
		// OVH 只认大写枚举,小写会被 EU/CA 站点 400 掉,而 US 站点根本不校验它,
		// 串区在美区不报错、只是悄悄返回美国机房的库存 —— 只能本地兜住。
		body.OvhSubsidiary = vps.NormalizeSubsidiary(body.OvhSubsidiary)
		if body.OvhSubsidiary == "" {
			// 不再写死 IE:有自动下单账户就跟着账户所在站点走
			body.OvhSubsidiary = vps.DefaultSubsidiary(state, body.AutoOrderAccountID)
		}
		if !ovh.KnownSubsidiary(body.OvhSubsidiary) {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "未知的 OVH 子公司 " + body.OvhSubsidiary +
				":EU 区 CZ DE ES EU FI FR GB IE IT LT MA NL PL PT SN TN;CA 区 ASIA AU CA IN QC SG WE WS;US 区 US"})
			return
		}
		// 自动下单账户必须和订阅子公司在同一个站点,否则补货触发时账户压根买不到这批货
		if body.AutoOrderAccountID != "" {
			accRegion := ovh.EndpointRegion(autoOrderAccount.Endpoint)
			subRegion := ovh.SubsidiaryRegion(body.OvhSubsidiary)
			if accRegion != subRegion {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "自动下单账户在 " + vps.RegionLabel(accRegion) +
					",而订阅子公司 " + body.OvhSubsidiary + " 属于 " + vps.RegionLabel(subRegion) +
					";两个站点的库存和购物车不互通,请换成同区的账户或子公司"})
				return
			}
		}
		// 先探一次:planCode 分区(US 目录才有 -eu / -ca 后缀码),
		// 拿错区的码 OVH 回 404,监控起来只会表现为"永远无货",不如在订阅时就说清楚。
		if _, err := vps.CheckVPSDCAvailability(state, body.PlanCode, body.OvhSubsidiary); err != nil {
			var ce *vps.CheckError
			if errors.As(err, &ce) && ce.Permanent() {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
				return
			}
			// 临时故障不拦订阅,只记一笔
			state.Logger.Warn("订阅前探测VPS可用性失败(不影响订阅): "+err.Error(), "vps_monitor")
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
			// 老订阅里可能存着小写/空的子公司,比较前统一归一化,
			// 免得 "ie" 和 "IE" 被当成两个订阅、对同一份库存重复查两遍
			if s.PlanCode == body.PlanCode && vps.NormalizeSubsidiary(s.OvhSubsidiary) == body.OvhSubsidiary {
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
			"running":             vps.Running(),
			"subscriptions_count": count,
			"check_interval":      interval,
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
			AccountID     string `json:"accountId"`
		}
		_ = c.ShouldBindJSON(&body)
		// 同 Add:归一化 + 跟账户所在站点兜底,不再写死 IE
		body.OvhSubsidiary = vps.NormalizeSubsidiary(body.OvhSubsidiary)
		if body.OvhSubsidiary == "" {
			body.OvhSubsidiary = vps.DefaultSubsidiary(state, body.AccountID)
		}
		result, err := vps.CheckVPSDCAvailability(state, planCode, body.OvhSubsidiary)
		if err != nil {
			// 区域 / 套餐配错属于用户输入问题:400 + 中文说明,不要 500,也不要静默空数据
			var ce *vps.CheckError
			if errors.As(err, &ce) && ce.Permanent() {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
				return
			}
			c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": "获取VPS数据中心信息失败:" + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":        "success",
			"ovhSubsidiary": body.OvhSubsidiary,
			"region":        ovh.SubsidiaryRegion(body.OvhSubsidiary),
			"data":          result,
		})
	}
}
