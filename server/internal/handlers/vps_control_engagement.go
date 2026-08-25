package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/numconv"
)

// 区域核对结论(逐条对过 EU / US / CA 三站的 /1.0/services.json):
// /services/{serviceId}/billing/engagement 及其 available / request / endRule 子路径三区都注册,
// services.billing.engagement.EndStrategyEnum 的四个取值三区完全一致
// (CANCEL_SERVICE / REACTIVATE_ENGAGEMENT / STOP_ENGAGEMENT_FALLBACK_DEFAULT_PRICE /
// STOP_ENGAGEMENT_KEEP_PRICE),/vps/{sn}/serviceInfos 也三区都有(US 标 BETA)。
// 合同期这一块不需要区域门控 —— 别看到"美区功能少"就顺手加,这里加了会把美区合同期管理砍掉。

// serviceIDForVps 从 /vps/{name}/serviceInfos 拿 serviceId,engagement 端点需要数字 ID。
func serviceIDForVps(client *ovhsdk.Client, svc string) (int64, error) {
	var info map[string]interface{}
	if err := client.Get("/vps/"+svc+"/serviceInfos", &info); err != nil {
		return 0, err
	}
	id, _ := numconv.ToInt64(info["serviceId"])
	if id <= 0 {
		return 0, fmt.Errorf("serviceInfos 未返回 serviceId")
	}
	return id, nil
}

// GetVpsEngagement GET /api/vps-control/:service_name/engagement
func GetVpsEngagement(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		serviceID, err := serviceIDForVps(client, svc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		var eng map[string]interface{}
		if err := client.Get(fmt.Sprintf("/services/%d/billing/engagement", serviceID), &eng); err != nil {
			// 只有 404 才是「该服务没有合同期」这个正常业务态。该端点是 BETA,5xx/限流是常态,
			// 把它们也吞成 null 会让合同期内的机器显示成「无合同期」,必须原样报出去。
			if ovhIsNotFound(err) {
				c.JSON(http.StatusOK, gin.H{"success": true, "engagement": nil, "serviceId": serviceID})
				return
			}
			state.Logger.Error(fmt.Sprintf("VPS %s 查询合同期失败(serviceId=%d): %s", svc, serviceID, err.Error()), "vps_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "engagement": eng, "serviceId": serviceID})
	}
}

// GetVpsEngagementAvailable GET /api/vps-control/:service_name/engagement/available
func GetVpsEngagementAvailable(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		serviceID, err := serviceIDForVps(client, svc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		var pricings []map[string]interface{}
		if err := client.Get(fmt.Sprintf("/services/%d/billing/engagement/available", serviceID), &pricings); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "pricings": pricings})
	}
}

// GetVpsEngagementRequest GET /api/vps-control/:service_name/engagement/request
func GetVpsEngagementRequest(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		serviceID, err := serviceIDForVps(client, svc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		var req map[string]interface{}
		if err := client.Get(fmt.Sprintf("/services/%d/billing/engagement/request", serviceID), &req); err != nil {
			// 同上:404 = 当前没有待处理的合同期变更请求,其余错误一律上报,
			// 否则用户会以为「没有待处理请求」而重复提交。
			if ovhIsNotFound(err) {
				c.JSON(http.StatusOK, gin.H{"success": true, "request": nil})
				return
			}
			state.Logger.Error(fmt.Sprintf("VPS %s 查询合同期变更请求失败(serviceId=%d): %s", svc, serviceID, err.Error()), "vps_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "request": req})
	}
}

// CreateVpsEngagementRequest POST /api/vps-control/:service_name/engagement/request
func CreateVpsEngagementRequest(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			PricingMode string `json:"pricingMode"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.PricingMode == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 pricingMode 参数"})
			return
		}
		serviceID, err := serviceIDForVps(client, svc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		var result map[string]interface{}
		if err := client.Post(fmt.Sprintf("/services/%d/billing/engagement/request", serviceID),
			map[string]interface{}{"pricingMode": body.PricingMode}, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("VPS %s engagement 请求已提交: %s", svc, body.PricingMode), "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "合同期变更请求已提交", "request": result})
	}
}

// DeleteVpsEngagementRequest DELETE /api/vps-control/:service_name/engagement/request
func DeleteVpsEngagementRequest(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		serviceID, err := serviceIDForVps(client, svc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		if err := client.Delete(fmt.Sprintf("/services/%d/billing/engagement/request", serviceID), nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("VPS %s engagement 请求已撤销", svc), "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "合同期变更请求已撤销"})
	}
}

// UpdateVpsEngagementEndRule PUT /api/vps-control/:service_name/engagement/end-rule
func UpdateVpsEngagementEndRule(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Strategy string `json:"strategy"`
			Confirm  bool   `json:"confirm"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Strategy == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 strategy 参数"})
			return
		}
		// 与专用服务器侧同一套闸门:枚举白名单 + 销毁类操作强制二次确认。
		// UI 已经在弹二次确认,但闸门必须落在服务端 —— 否则脚本一次请求就能把 VPS 排进销毁队列。
		if !engagementEndStrategies[body.Strategy] {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "无效的到期策略: " + body.Strategy})
			return
		}
		if body.Strategy == "CANCEL_SERVICE" && !body.Confirm {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "到期自动销毁服务属于不可撤销操作,请二次确认后重试(需带 confirm:true)"})
			return
		}
		if body.Strategy == "CANCEL_SERVICE" {
			state.Logger.Warn(fmt.Sprintf("VPS %s 到期策略即将设为 CANCEL_SERVICE(承诺期结束后销毁服务)", svc), "vps_control")
		}
		serviceID, err := serviceIDForVps(client, svc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		if err := client.Put(fmt.Sprintf("/services/%d/billing/engagement/endRule", serviceID),
			map[string]interface{}{"strategy": body.Strategy}, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("VPS %s engagement endRule 已改为 %s", svc, body.Strategy), "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "到期策略已更新"})
	}
}
