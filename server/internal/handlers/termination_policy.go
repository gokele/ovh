package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
)

// 到期终止策略。
//
// 血的教训:这件事**不能**用 POST /{service}/terminate。
// OVH 的生命周期动作枚举 services.expanded.Lifecycle.ActionEnum 把两件事分得很清楚:
//
//	terminate                    立即终止 —— 提交后 OVH 当场把服务器暂停,
//	                             并邮件通知"5 天内不付款就彻底清除硬盘数据"
//	terminateAtExpirationDate    到期终止 —— 用到当期结束,到期日才销毁
//
// /{service}/terminate 端点对应的是前者,它没有"到期"这个选项可选。
// 到期终止只能通过 PUT /services/{serviceId} 的 terminationPolicy 字段设置。
// 我一开始用错了端点,把用户一台在跑的服务器直接弄停了。
//
// terminationPolicy 的三个取值(三区一致):
//
//	empty                        不终止(也就是撤销已提交的到期终止)
//	terminateAtExpirationDate    到期日终止
//	terminateAtEngagementDate    合同期结束时终止(仅对有合同期的服务有意义)
const (
	TerminationNone         = "empty"
	TerminationAtExpiration = "terminateAtExpirationDate"
	TerminationAtEngagement = "terminateAtEngagementDate"
)

var terminationPolicies = map[string]bool{
	TerminationNone:         true,
	TerminationAtExpiration: true,
	TerminationAtEngagement: true,
}

// setTerminationPolicy PUT /services/{serviceId}
//
// services.update.Service 的三个字段(displayName / renew / terminationPolicy)全部
// canBeNull:true,所以只发要改的那一个是合法的 —— 不需要像 RenewType 那样补全。
func setTerminationPolicy(client *ovhsdk.Client, serviceID int64, policy string) error {
	return client.Put(fmt.Sprintf("/services/%d", serviceID),
		map[string]interface{}{"terminationPolicy": policy}, nil)
}

// UpdateTerminationPolicy PUT /api/server-control/:service_name/termination-policy
func UpdateTerminationPolicy(state *app.State) gin.HandlerFunc {
	return terminationPolicyHandler(state, serviceIDForDedicated, "server_control", "服务器")
}

// UpdateVpsTerminationPolicy PUT /api/vps-control/:service_name/termination-policy
func UpdateVpsTerminationPolicy(state *app.State) gin.HandlerFunc {
	return terminationPolicyHandler(state, serviceIDForVps, "vps_control", "VPS")
}

func terminationPolicyHandler(
	state *app.State,
	resolveID func(*ovhsdk.Client, string) (int64, error),
	logSource, label string,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Policy string `json:"policy"`
		}
		_ = c.ShouldBindJSON(&body)
		if !terminationPolicies[body.Policy] {
			c.JSON(http.StatusBadRequest, gin.H{"success": false,
				"error": "policy 必须是 empty / terminateAtExpirationDate / terminateAtEngagementDate 之一"})
			return
		}

		serviceID, err := resolveID(client, svc)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false,
				"error": "取 serviceId 失败: " + err.Error()})
			return
		}
		if err := setTerminationPolicy(client, serviceID, body.Policy); err != nil {
			state.Logger.Error(label+" "+svc+" 设置终止策略失败: "+err.Error(), logSource)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}

		msg := "已设为到期终止:到期日之前照常使用,到期后销毁"
		if body.Policy == TerminationNone {
			msg = "已取消终止,服务恢复正常续费"
		} else if body.Policy == TerminationAtEngagement {
			msg = "已设为合同期结束时终止"
		}
		state.Logger.Warn(fmt.Sprintf("%s %s 终止策略改为 %s", label, svc, body.Policy), logSource)
		c.JSON(http.StatusOK, gin.H{"success": true, "message": msg, "policy": body.Policy})
	}
}

// lifecycleTermination 读回终止状态。
//
// 这是文档指定的唯一可靠读回路径:services.update.Service.terminationPolicy 只管写,
// 读要走 GET /services/{serviceId} 的 billing.lifecycle.current ——
// pendingActions(services.expanded.Lifecycle.ActionEnum[],含 terminate /
// terminateAtExpirationDate / terminateAtEngagementDate / deleteAtExpiration)
// 和 terminationDate("Scheduled termination date")。
//
// 旧接口 serviceInfos.renew.deleteAtExpiration 会不会随 terminationPolicy 同步,
// 文档没有任何说法 —— 所以界面显示"当前是不是到期终止"不能赌它,必须读这里。
func lifecycleTermination(client *ovhsdk.Client, serviceID int64) (scheduled bool, date string, err error) {
	var svc struct {
		Billing struct {
			Lifecycle struct {
				Current struct {
					PendingActions  []string `json:"pendingActions"`
					TerminationDate string   `json:"terminationDate"`
				} `json:"current"`
			} `json:"lifecycle"`
		} `json:"billing"`
	}
	if err := client.Get(fmt.Sprintf("/services/%d", serviceID), &svc); err != nil {
		return false, "", err
	}
	for _, a := range svc.Billing.Lifecycle.Current.PendingActions {
		switch a {
		case "terminate", "terminateAtExpirationDate", "terminateAtEngagementDate", "deleteAtExpiration":
			return true, svc.Billing.Lifecycle.Current.TerminationDate, nil
		}
	}
	return false, "", nil
}

// attachTerminationState 把终止状态并进 serviceInfo 响应。
// 读取失败不致命 —— 退回旧的 renew.deleteAtExpiration 判断,但要记日志,
// 别让"读不到"悄悄变成"没有终止计划"。
func attachTerminationState(state *app.State, client *ovhsdk.Client,
	resolveID func(*ovhsdk.Client, string) (int64, error),
	svc, logSource string, out map[string]interface{}) {
	serviceID, err := resolveID(client, svc)
	if err != nil {
		state.Logger.Warn(svc+" 取 serviceId 失败,终止状态回退到 renew 字段: "+err.Error(), logSource)
		return
	}
	scheduled, date, err := lifecycleTermination(client, serviceID)
	if err != nil {
		state.Logger.Warn(svc+" 读取生命周期失败,终止状态回退到 renew 字段: "+err.Error(), logSource)
		return
	}
	out["terminationScheduled"] = scheduled
	if date != "" {
		out["terminationDate"] = date
	}
}
