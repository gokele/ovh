package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/numconv"
)

// engagementEndStrategies 对应 schema 的 services.billing.engagement.EndStrategyEnum,
// 本地先挡一道,免得拼错大小写只能换回 OVH 的英文枚举错误。
var engagementEndStrategies = map[string]bool{
	"CANCEL_SERVICE":                         true,
	"REACTIVATE_ENGAGEMENT":                  true,
	"STOP_ENGAGEMENT_FALLBACK_DEFAULT_PRICE": true,
	"STOP_ENGAGEMENT_KEEP_PRICE":             true,
}

// serviceIDForDedicated 从 /dedicated/server/{name}/serviceInfos 拿 serviceId(数字 ID),
// engagement / mitigation 等基于服务 ID 的端点都要先做这步换算。失败返 0。
func serviceIDForDedicated(client *ovhsdk.Client, svc string) (int64, error) {
	var info map[string]interface{}
	if err := client.Get("/dedicated/server/"+svc+"/serviceInfos", &info); err != nil {
		return 0, err
	}
	id, _ := numconv.ToInt64(info["serviceId"])
	if id <= 0 {
		return 0, fmt.Errorf("serviceInfos 未返回 serviceId")
	}
	return id, nil
}

// GetEngagement GET /api/server-control/:service_name/engagement
// 返回当前 engagement 详情(承诺期开始 / 结束 / 到期策略)。无 engagement 返 null。
func GetEngagement(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		serviceID, err := serviceIDForDedicated(client, svc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		var eng map[string]interface{}
		if err := client.Get(fmt.Sprintf("/services/%d/billing/engagement", serviceID), &eng); err != nil {
			// 只有 404 才等价于"该服务没有 engagement(标准月付)";其它错误如实上报,
			// 否则查询失败会被前端渲染成确定性的"未签合同期",误导用户做退订决策
			if ovhIsNotFound(err) {
				c.JSON(http.StatusOK, gin.H{"success": true, "engagement": nil, "serviceId": serviceID})
				return
			}
			state.Logger.Error(fmt.Sprintf("查询服务器 %s(serviceId=%d) 合同期失败: %s", svc, serviceID, err.Error()), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "查询合同期失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "engagement": eng, "serviceId": serviceID})
	}
}

// GetEngagementAvailable GET /api/server-control/:service_name/engagement/available
// 列出可订阅的 engagement 价格选项(不同周期 / 不同折扣)。
func GetEngagementAvailable(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		serviceID, err := serviceIDForDedicated(client, svc)
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

// GetEngagementRequest GET /api/server-control/:service_name/engagement/request
// 查询是否有进行中的 engagement 变更请求(用户已请求但未生效)。无 → null。
func GetEngagementRequest(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		serviceID, err := serviceIDForDedicated(client, svc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		var req map[string]interface{}
		if err := client.Get(fmt.Sprintf("/services/%d/billing/engagement/request", serviceID), &req); err != nil {
			// 同上:只有 404 才是"没有进行中的请求"。查询失败若被说成"无请求",
			// 前端会把订阅按钮重新放开,用户对同一服务重复提交 engagement 请求
			if ovhIsNotFound(err) {
				c.JSON(http.StatusOK, gin.H{"success": true, "request": nil})
				return
			}
			state.Logger.Error(fmt.Sprintf("查询服务器 %s(serviceId=%d) 合同期变更请求失败: %s", svc, serviceID, err.Error()), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "查询合同期变更请求失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "request": req})
	}
}

// CreateEngagementRequest POST /api/server-control/:service_name/engagement/request
// 提交 engagement 变更请求(切换到更长承诺期)。body: { pricingMode }
func CreateEngagementRequest(state *app.State) gin.HandlerFunc {
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
		serviceID, err := serviceIDForDedicated(client, svc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		var result map[string]interface{}
		err = client.Post(fmt.Sprintf("/services/%d/billing/engagement/request", serviceID),
			map[string]interface{}{"pricingMode": body.PricingMode}, &result)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("服务器 %s engagement 请求已提交: pricingMode=%s", svc, body.PricingMode), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "合同期变更请求已提交", "request": result})
	}
}

// DeleteEngagementRequest DELETE /api/server-control/:service_name/engagement/request
// 取消进行中的 engagement 变更请求(还没生效前可撤回)。
func DeleteEngagementRequest(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		serviceID, err := serviceIDForDedicated(client, svc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		if err := client.Delete(fmt.Sprintf("/services/%d/billing/engagement/request", serviceID), nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("服务器 %s engagement 请求已撤销", svc), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "合同期变更请求已撤销"})
	}
}

// UpdateEngagementEndRule PUT /api/server-control/:service_name/engagement/end-rule
// 改 engagement 到期策略(自动续 / 转月付 / 销毁等)。body: { strategy }
func UpdateEngagementEndRule(state *app.State) gin.HandlerFunc {
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
		if !engagementEndStrategies[body.Strategy] {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "无效的到期策略: " + body.Strategy})
			return
		}
		// CANCEL_SERVICE 会在承诺期结束时直接销毁服务器,且不可撤销,
		// 所以要求调用方显式带 confirm:true,避免误点一次按钮就把机器排进销毁队列
		if body.Strategy == "CANCEL_SERVICE" && !body.Confirm {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "到期自动销毁服务属于不可撤销操作,请二次确认后重试(需带 confirm:true)"})
			return
		}
		serviceID, err := serviceIDForDedicated(client, svc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		if body.Strategy == "CANCEL_SERVICE" {
			state.Logger.Warn(fmt.Sprintf("服务器 %s 到期策略即将设为 CANCEL_SERVICE(承诺期结束后销毁服务)", svc), "server_control")
		}
		if err := client.Put(fmt.Sprintf("/services/%d/billing/engagement/endRule", serviceID),
			map[string]interface{}{"strategy": body.Strategy}, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("服务器 %s engagement endRule 已改为 %s", svc, body.Strategy), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "到期策略已更新"})
	}
}
