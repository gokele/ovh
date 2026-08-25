package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
	"github.com/ovh-buy/server/internal/vps"
)

// 订阅编辑。
//
// 为什么单独做 PUT 而不是让前端「删了再加」:订阅身上带着 LastStatus 和 History。
// 删掉重建会把这两样清空,而 LastStatus 正是「有货/没货」的判断基准 ——
// 一台本来就有货的机器被重建订阅后,下一轮检查会被当成「无货→有货」的跳变,
// 于是用户凌晨收到一条根本没发生的补货通知,自动下单还会真的去下单。
// 改配置就该只改配置,不该动状态。

// UpdateSubscription PUT /api/monitor/subscriptions/:planCode
//
// 只改配置,不重置状态。planCode 是身份,从路径取,body 里改不了。
// 传了哪个字段就改哪个,没传的保持原样 —— 前端可以只发一个 quantity 而不用回传整个订阅。
func UpdateSubscription(state *app.State, mon *monitor.Monitor) gin.HandlerFunc {
	return func(c *gin.Context) {
		planCode := c.Param("planCode")
		sub := mon.FindSubscription(planCode)
		if sub == nil {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "订阅不存在: " + planCode})
			return
		}

		var body struct {
			Datacenters        *[]string `json:"datacenters"`
			NotifyAvailable    *bool     `json:"notifyAvailable"`
			NotifyUnavailable  *bool     `json:"notifyUnavailable"`
			AutoOrder          *bool     `json:"autoOrder"`
			Quantity           *int      `json:"quantity"`
			AutoOrderAccountID *string   `json:"autoOrderAccountId"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "请求体格式错误: " + err.Error()})
			return
		}

		// 先取当前值作为默认,再用 body 里给了的字段覆盖
		cur := mon.SubscriptionConfig(planCode)
		datacenters := cur.Datacenters
		notifyAvailable := cur.NotifyAvailable
		notifyUnavailable := cur.NotifyUnavailable
		autoOrder := cur.AutoOrder
		quantity := cur.Quantity
		accountID := cur.AutoOrderAccountID

		if body.Datacenters != nil {
			datacenters = *body.Datacenters
		}
		if body.NotifyAvailable != nil {
			notifyAvailable = *body.NotifyAvailable
		}
		if body.NotifyUnavailable != nil {
			notifyUnavailable = *body.NotifyUnavailable
		}
		if body.AutoOrder != nil {
			autoOrder = *body.AutoOrder
		}
		if body.Quantity != nil {
			quantity = *body.Quantity
		}
		if body.AutoOrderAccountID != nil {
			accountID = *body.AutoOrderAccountID
			// 空串是合法值:表示「触发时只通知、不下单」
			if accountID != "" {
				if _, ok := state.FindAccount(accountID); !ok {
					c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "autoOrderAccountId 不存在"})
					return
				}
			}
		}
		if quantity < 1 {
			quantity = 1
		}

		// 换了下单账户就重新做一次区域预检:欧区机型配美区账户,补货时会被 OVH 400 掉
		var regionWarning, region, subsidiary string
		if body.AutoOrderAccountID != nil {
			region, subsidiary, regionWarning = mon.PreflightRegion(planCode, accountID)
		}

		// AddSubscription 对已存在的 planCode 是就地改配置,不会重置 LastStatus / History
		mon.AddSubscription(planCode, datacenters, notifyAvailable, notifyUnavailable,
			cur.ServerName, nil, nil, autoOrder, quantity, accountID)
		mon.SaveToDB()
		state.Logger.Info("更新服务器订阅配置: "+planCode, "monitor")

		resp := gin.H{"status": "success", "message": "已更新 " + planCode, "subscription": mon.FindSubscription(planCode)}
		if region != "" {
			resp["region"] = region
			resp["subsidiary"] = subsidiary
		}
		if regionWarning != "" {
			state.Logger.Warn("订阅区域预检告警: "+planCode+" - "+regionWarning, "monitor")
			resp["status"] = "warning"
			resp["regionWarning"] = regionWarning
		}
		c.JSON(http.StatusOK, resp)
	}
}

// UpdateVPSSubscription PUT /api/vps-monitor/subscriptions/:subscription_id
//
// 同样只改配置。子公司允许改(选错站点是最常见的配置错误),但要走和新建一样的
// 归一化 + 站点校验 + 探测,否则改完就是个查错站点、永远显示无货的订阅。
func UpdateVPSSubscription(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("subscription_id")

		var body struct {
			OvhSubsidiary      *string   `json:"ovhSubsidiary"`
			Datacenters        *[]string `json:"datacenters"`
			MonitorLinux       *bool     `json:"monitorLinux"`
			MonitorWindows     *bool     `json:"monitorWindows"`
			NotifyAvailable    *bool     `json:"notifyAvailable"`
			NotifyUnavailable  *bool     `json:"notifyUnavailable"`
			AutoOrderAccountID *string   `json:"autoOrderAccountId"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "请求体格式错误: " + err.Error()})
			return
		}

		// 先在锁里取一份快照,校验和探测都在锁外做 —— 探测要发 HTTP,
		// 拿着 VPSSubsMu 去等网络会把整个监控循环卡住
		state.VPSSubsMu.Lock()
		var cur types.VPSSubscription
		found := false
		for _, s := range state.VPSSubscriptions {
			if s.ID == id {
				cur, found = s, true
				break
			}
		}
		state.VPSSubsMu.Unlock()
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "订阅不存在: " + id})
			return
		}

		next := cur
		if body.Datacenters != nil {
			next.Datacenters = *body.Datacenters
		}
		if body.MonitorLinux != nil {
			next.MonitorLinux = *body.MonitorLinux
		}
		if body.MonitorWindows != nil {
			next.MonitorWindows = *body.MonitorWindows
		}
		if body.NotifyAvailable != nil {
			next.NotifyAvailable = *body.NotifyAvailable
		}
		if body.NotifyUnavailable != nil {
			next.NotifyUnavailable = *body.NotifyUnavailable
		}
		if body.AutoOrderAccountID != nil {
			next.AutoOrderAccountID = *body.AutoOrderAccountID
		}
		if body.OvhSubsidiary != nil {
			next.OvhSubsidiary = vps.NormalizeSubsidiary(*body.OvhSubsidiary)
			if next.OvhSubsidiary == "" {
				next.OvhSubsidiary = vps.DefaultSubsidiary(state, next.AutoOrderAccountID)
			}
			if !ovh.KnownSubsidiary(next.OvhSubsidiary) {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "未知的 OVH 子公司 " + next.OvhSubsidiary +
					":EU 区 CZ DE ES EU FI FR GB IE IT LT MA NL PL PT SN TN;CA 区 ASIA AU CA IN QC SG WE WS;US 区 US"})
				return
			}
		}

		// 账户和子公司必须同站点。注意这里两个字段都可能刚被改过,
		// 所以校验必须用合并后的 next,不能只看 body 里传了哪个
		if next.AutoOrderAccountID != "" {
			acc, ok := state.FindAccount(next.AutoOrderAccountID)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "autoOrderAccountId 不存在"})
				return
			}
			accRegion := ovh.EndpointRegion(acc.Endpoint)
			subRegion := ovh.SubsidiaryRegion(next.OvhSubsidiary)
			if accRegion != subRegion {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "自动下单账户在 " + vps.RegionLabel(accRegion) +
					",而订阅子公司 " + next.OvhSubsidiary + " 属于 " + vps.RegionLabel(subRegion) +
					";两个站点的库存和购物车不互通,请换成同区的账户或子公司"})
				return
			}
		}

		// 换了站点就重新探一次:拿错区的 planCode 只会表现为「永远无货」
		if next.OvhSubsidiary != cur.OvhSubsidiary {
			if _, err := vps.CheckVPSDCAvailability(state, next.PlanCode, next.OvhSubsidiary); err != nil {
				var ce *vps.CheckError
				if errors.As(err, &ce) && ce.Permanent() {
					c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
					return
				}
				state.Logger.Warn("更新订阅前探测VPS可用性失败(不影响更新): "+err.Error(), "vps_monitor")
			}
		}

		state.VPSSubsMu.Lock()
		// 改子公司相当于换了一套库存视图,旧的 LastStatus 是对另一个站点的判断,
		// 留着会让下一轮检查基于错误的基准算跳变。History 是既成事实,保留。
		resetStatus := next.OvhSubsidiary != cur.OvhSubsidiary
		idx := -1
		for i, s := range state.VPSSubscriptions {
			if s.ID == id {
				idx = i
				break
			}
		}
		if idx < 0 {
			// 校验期间被别人删了
			state.VPSSubsMu.Unlock()
			c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "订阅不存在: " + id})
			return
		}
		// 拿锁里的当前值做基准,别把并发写进来的 History / LastStatus 覆盖掉
		live := state.VPSSubscriptions[idx]
		live.OvhSubsidiary = next.OvhSubsidiary
		live.Datacenters = next.Datacenters
		live.MonitorLinux = next.MonitorLinux
		live.MonitorWindows = next.MonitorWindows
		live.NotifyAvailable = next.NotifyAvailable
		live.NotifyUnavailable = next.NotifyUnavailable
		live.AutoOrderAccountID = next.AutoOrderAccountID
		if resetStatus {
			live.LastStatus = map[string]string{}
		}
		state.VPSSubscriptions[idx] = live
		state.VPSSubsMu.Unlock()

		_ = vps.SaveSubscriptions(state)
		state.Logger.Info("更新VPS订阅配置: "+live.PlanCode+" (subsidiary: "+live.OvhSubsidiary+")", "vps_monitor")

		resp := gin.H{"status": "success", "message": "已更新 " + live.PlanCode, "subscription": live}
		if resetStatus {
			resp["message"] = "已更新 " + live.PlanCode + "(站点改为 " + live.OvhSubsidiary + ",库存状态已重新开始判断)"
		}
		c.JSON(http.StatusOK, resp)
	}
}
