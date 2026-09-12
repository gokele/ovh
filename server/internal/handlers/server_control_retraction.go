package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
)

// retractionReasons OVH 的 billing.order.RetractionReasonEnum 全集。
// 这是个必填枚举,传别的值 OVH 直接 400。前端的下拉就照这份渲染,
// 中文只是说明,发给 OVH 的始终是英文码。
var retractionReasons = []gin.H{
	{"value": "expensive", "label": "太贵了"},
	{"value": "performance", "label": "性能不符合预期"},
	{"value": "reliability", "label": "稳定性不符合预期"},
	{"value": "difficulty", "label": "用起来太麻烦"},
	{"value": "competitor", "label": "改用别家了"},
	{"value": "unused", "label": "用不上了"},
	{"value": "other", "label": "其它"},
}

func isValidRetractionReason(v string) bool {
	for _, r := range retractionReasons {
		if r["value"] == v {
			return true
		}
	}
	return false
}

// orderForService 从订单映射里找出这台机器对应的订单号。
//
// 只读 GetOrderMapping 那套缓存(按账户分 key,10 分钟),不触发同步 ——
// 同步是几十个 OVH 请求的全量扫描,而它和抢购主链路共用账户配额。
// 缓存冷时返回错误,让前端提示用户去点「同步订单」。
func orderForService(state *app.State, c *gin.Context, serviceName string) (int64, bool, error) {
	mapping, err := orderMappingFor(state, c)
	if err != nil {
		return 0, false, err
	}
	raw, ok := mapping[serviceName]
	if !ok {
		return 0, false, nil
	}
	info, ok := raw.(map[string]interface{})
	if !ok {
		return 0, false, nil
	}
	switch v := info["orderId"].(type) {
	case int64:
		return v, true, nil
	case float64:
		return int64(v), true, nil
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil, nil
	}
	return 0, false, nil
}

// GetRetraction GET /api/server-control/:service_name/retraction
//
// 回答"这台机器现在还能不能无理由退"。
//
// 判据是 OVH 返回的 billing.Order.retractionDate,不是自己按"开通不到 14 天"去算:
//   - 这个 fix 之前下的单在 checkout 时就弃权了,OVH 不会给它们 retractionDate。
//     自己算 14 天的话,那些机器会显示一个点了必然失败的退款按钮。
//   - 撤回期从哪个事件起算、不同产品是不是都 14 天,是 OVH 的合同细节。
//     它已经把答案算好放在字段里了,没有理由再猜一遍。
//
// eligible=false 时 reason 说明是哪一种"不能退",三种含义完全不同:
// 没有撤回权 / 已经过期 / 我们没查到订单。
func GetRetraction(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		acc, ok := ovhAccountFor(state, c)
		if !ok {
			noOVHResp(c)
			return
		}
		svc := strings.TrimSpace(c.Param("service_name"))
		if svc == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 service_name"})
			return
		}

		orderID, found, err := orderForService(state, c, svc)
		if err != nil {
			// 映射没拉到 ≠ 这台机器没有撤回权。必须说成"没查到",
			// 否则用户会以为权利已经没了而放弃,而真相只是这次同步失败。
			c.JSON(http.StatusOK, gin.H{
				"success":  true,
				"eligible": false,
				"reason":   "order_lookup_failed",
				"message":  "没能查到这台机器对应的订单（" + err.Error() + "），无法判断是否还能撤回，请重试",
				"reasons":  retractionReasons,
			})
			return
		}
		if !found {
			c.JSON(http.StatusOK, gin.H{
				"success":  true,
				"eligible": false,
				"reason":   "order_not_found",
				"message":  "没找到这台机器对应的订单。订单映射按账户缓存，可以到「同步订单」刷新后再看",
				"reasons":  retractionReasons,
			})
			return
		}

		var order map[string]interface{}
		if err := client.Get(fmt.Sprintf("/me/order/%d", orderID), &order); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success":  true,
				"eligible": false,
				"reason":   "order_read_failed",
				"orderId":  orderID,
				"message":  "订单详情读取失败（" + err.Error() + "），无法判断是否还能撤回，请重试",
				"reasons":  retractionReasons,
			})
			return
		}

		retractionDate, _ := order["retractionDate"].(string)
		orderURL := ovh.ManagerOrderURL(acc.Endpoint, strconv.FormatInt(orderID, 10))

		if retractionDate == "" {
			// OVH 没给撤回截止日 = 这单没有撤回权。
			// 最常见的原因就是下单时弃了权(本程序 v0.1.24 之前的所有订单都是)。
			c.JSON(http.StatusOK, gin.H{
				"success":  true,
				"eligible": false,
				"reason":   "waived",
				"orderId":  orderID,
				"orderUrl": orderURL,
				"message":  "这一单没有撤回权。v0.1.24 之前下的单在结账时就放弃了 14 天撤回期，之后下的单不会",
				"reasons":  retractionReasons,
			})
			return
		}

		deadline, ok := parseOVHTime(retractionDate)
		if !ok {
			c.JSON(http.StatusOK, gin.H{
				"success":        true,
				"eligible":       false,
				"reason":         "bad_date",
				"orderId":        orderID,
				"orderUrl":       orderURL,
				"retractionDate": retractionDate,
				"message":        "OVH 返回的撤回截止时间解析不了：" + retractionDate,
				"reasons":        retractionReasons,
			})
			return
		}
		if time.Now().After(deadline) {
			c.JSON(http.StatusOK, gin.H{
				"success":        true,
				"eligible":       false,
				"reason":         "expired",
				"orderId":        orderID,
				"orderUrl":       orderURL,
				"retractionDate": retractionDate,
				"message":        "撤回期已于 " + deadline.Local().Format("2006-01-02 15:04") + " 结束",
				"reasons":        retractionReasons,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success":        true,
			"eligible":       true,
			"orderId":        orderID,
			"orderUrl":       orderURL,
			"retractionDate": retractionDate,
			"hoursLeft":      int(time.Until(deadline).Hours()),
			"reasons":        retractionReasons,
		})
	}
}

// PostRetraction POST /api/server-control/:service_name/retraction
// body: { "reason": "expensive", "comment": "可选说明", "confirm": true }
//
// 申请无理由撤单。这一步会把订单退掉、服务器随之注销 —— 不可逆,
// 所以要求 confirm:true,和重装、终止服务同一套把关。
func PostRetraction(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		svc := strings.TrimSpace(c.Param("service_name"))
		var body struct {
			Reason  string `json:"reason"`
			Comment string `json:"comment"`
			Confirm bool   `json:"confirm"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求体不合法: " + err.Error()})
			return
		}
		if !body.Confirm {
			// 撤单会连带注销服务器。前端已经有二次确认,这里是服务端的最后一道闸:
			// 少一个 confirm 就少一次"手滑把在跑的机器退掉"的可能。
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "撤单不可逆（订单退款 + 服务器注销），必须带 confirm:true",
			})
			return
		}
		if !isValidRetractionReason(body.Reason) {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "reason 必须是 OVH 的合法取值之一，见 GET 同路径返回的 reasons",
				"reasons": retractionReasons,
			})
			return
		}

		orderID, found, err := orderForService(state, c, svc)
		if err != nil || !found {
			msg := "没找到这台机器对应的订单"
			if err != nil {
				msg = "订单查询失败: " + err.Error()
			}
			c.JSON(http.StatusNotFound, gin.H{"success": false, "error": msg})
			return
		}

		payload := map[string]interface{}{"reason": body.Reason}
		if cm := strings.TrimSpace(body.Comment); cm != "" {
			payload["comment"] = cm
		}
		if err := client.Post(fmt.Sprintf("/me/order/%d/retraction", orderID), payload, nil); err != nil {
			state.Logger.Error(fmt.Sprintf("申请撤单失败 (订单 %d / %s): %s", orderID, svc, err.Error()), "server_control")
			c.JSON(http.StatusBadGateway, gin.H{
				"success": false,
				"error":   "OVH 拒绝了撤单申请: " + err.Error(),
			})
			return
		}
		state.Logger.Warn(fmt.Sprintf("已为 %s 申请撤单 (订单 %d, 理由 %s)", svc, orderID, body.Reason), "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"orderId": orderID,
			"message": "撤单申请已提交。OVH 会审核并退款，服务器随之注销；进度可在控制面板的订单页查看",
		})
	}
}

// parseOVHTime OVH 的 datetime 是 RFC3339(带时区)。
// 保留一个不带时区的兜底:个别端点历史上返回过 "2006-01-02T15:04:05"。
func parseOVHTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(s), time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
