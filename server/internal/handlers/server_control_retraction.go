package handlers

import (
	"fmt"
	ovhsdk "github.com/ovh/go-ovh/ovh"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// retractionSupportedRegion 这个大区的 API 有没有撤单接口。
//
// 实测三份 schema(eu / api.us.ovhcloud.com / ca):
//
//	EU  POST /me/order/{id}/retraction  ✓
//	CA  POST /me/order/{id}/retraction  ✓
//	US  ——  整个接口不存在,只有只读的 /me/refund
//
// 而三个区的 billing.Order 都带 retractionDate 字段。也就是说美区账户完全
// 可能查到一个撤回截止日、界面上显示出按钮,点下去 404 —— 正是这个功能
// 最该避免的那种按钮。所以区域必须单独挡,不能只依赖 retractionDate 有没有。
func retractionSupportedRegion(endpoint string) bool {
	return ovh.EndpointRegion(endpoint) != "US"
}

func isValidRetractionReason(v string) bool {
	for _, r := range retractionReasons {
		if r["value"] == v {
			return true
		}
	}
	return false
}

// retractionLookup 一次查找的结果。负结果也缓存 —— 页面每次渲染都会问一遍,
// 不缓存的话"这台机器没有可撤订单"这个结论要反复用几十个请求去重算。
type retractionLookup struct {
	orderID int64
	found   bool
	at      time.Time
}

var (
	retractionLookupMu    sync.Mutex
	retractionLookupCache = map[string]retractionLookup{} // accountID|serviceName → 结果
)

const (
	// retractionLookupTTL 查找结果的缓存时长。撤回期是按天算的,
	// 5 分钟内不会有新订单进入或退出窗口。
	retractionLookupTTL = 5 * time.Minute
	// retractionScanDays 往回扫多少天的订单。
	// 撤回期 14 天,多给一周的余量:OVH 可能从交付而不是下单起算,
	// 而这个函数的判据是订单自己的 retractionDate,扫宽一点不会误判,
	// 只是多看几个订单头。
	retractionScanDays = 21
)

// retractableOrderFor 找出这台机器**还在撤回期内**的订单号。
//
// 为什么不用 GetOrderMapping 那套缓存:它只在用户点「同步订单」时才填充,
// 而前端根本没有那个入口(grep order-mapping 在 web/ 里零命中)——
// 依赖它等于这个功能永远不显示。这是 v0.1.24/v0.1.25 的实际状况。
//
// 也不该顺手触发那套同步:它是几十个 OVH 请求的全量扫描(所有服务器的
// serviceInfos + 所有订单的所有明细),而配额和抢购主链路共用。
//
// 这里的做法是按撤回期的实际范围剪枝:
//  1. GET /me/order?date.from=21天前 —— 只拿最近的订单号,通常个位数
//  2. 对每个订单 GET /me/order/{id} 看 retractionDate ——
//     没有或已过期的当场剪掉,一个请求换一次剪枝
//  3. 只有还在窗口内的(通常 0~1 个)才去查明细找 serviceName
//
// 绝大多数账户在第 2 步就全被剪光,总开销是"最近订单数 + 1"个请求。
func retractableOrderFor(state *app.State, c *gin.Context, client *ovhsdk.Client,
	accountID, serviceName string) (int64, bool, error) {

	key := accountID + "|" + serviceName
	retractionLookupMu.Lock()
	if e, hit := retractionLookupCache[key]; hit && time.Since(e.at) < retractionLookupTTL {
		retractionLookupMu.Unlock()
		return e.orderID, e.found, nil
	}
	retractionLookupMu.Unlock()

	remember := func(id int64, found bool) {
		retractionLookupMu.Lock()
		retractionLookupCache[key] = retractionLookup{orderID: id, found: found, at: time.Now()}
		retractionLookupMu.Unlock()
	}

	// 订单映射碰巧热着就先用它,省掉下面整套扫描
	if mapping, err := orderMappingFor(state, c); err == nil {
		if raw, ok := mapping[serviceName]; ok {
			if info, ok := raw.(map[string]interface{}); ok {
				if id, ok := toOrderID(info["orderId"]); ok {
					remember(id, true)
					return id, true, nil
				}
			}
		}
	}

	from := time.Now().AddDate(0, 0, -retractionScanDays).UTC().Format(time.RFC3339)
	var ids []int64
	if err := client.Get("/me/order?date.from="+url.QueryEscape(from), &ids); err != nil {
		return 0, false, fmt.Errorf("读取最近订单列表失败: %w", err)
	}
	// 新的在前:撤回期内的订单必然是最近下的
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })

	for _, id := range ids {
		var order map[string]interface{}
		if err := client.Get(fmt.Sprintf("/me/order/%d", id), &order); err != nil {
			continue // 单个订单读不到不影响别的
		}
		rd, _ := order["retractionDate"].(string)
		if rd == "" {
			continue // 没有撤回权,剪掉
		}
		if dl, ok := parseOVHTime(rd); !ok || time.Now().After(dl) {
			continue // 已过期,剪掉
		}
		// 这一单还在窗口内 —— 值得花请求去看它是不是这台机器
		var detailIDs []int64
		if err := client.Get(fmt.Sprintf("/me/order/%d/details", id), &detailIDs); err != nil {
			continue
		}
		for _, did := range detailIDs {
			var d map[string]interface{}
			if err := client.Get(fmt.Sprintf("/me/order/%d/details/%d", id, did), &d); err != nil {
				continue
			}
			// 整单没取消但某条明细被取消是常态,这种不算
			if cancelled, _ := d["cancelled"].(bool); cancelled {
				continue
			}
			if dom, _ := d["domain"].(string); dom == serviceName {
				remember(id, true)
				return id, true, nil
			}
		}
	}
	remember(0, false)
	return 0, false, nil
}

// toOrderID JSON 里的订单号可能是 int64 / float64 / string
func toOrderID(v interface{}) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case float64:
		return int64(t), true
	case string:
		n, err := strconv.ParseInt(t, 10, 64)
		return n, err == nil
	}
	return 0, false
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

		if !retractionSupportedRegion(acc.Endpoint) {
			// 挡在最前面:美区连接口都没有,再往下查订单纯属浪费一次 OVH 调用
			c.JSON(http.StatusOK, gin.H{
				"success":  true,
				"eligible": false,
				"reason":   "region_unsupported",
				"message":  "美区（OVHcloud US）的 API 没有撤单接口，这台机器不能从这里申请撤单。如需退款请联系 OVH 美区客服",
				"reasons":  retractionReasons,
			})
			return
		}

		orderID, found, err := retractableOrderFor(state, c, client, acc.ID, svc)
		if err != nil {
			// 查询失败 ≠ 这台机器没有撤回权。必须说成"没查到" ——
			// 说成"不能退"的话用户会以为权利已经没了而放弃,而真相只是这次请求挂了。
			c.JSON(http.StatusOK, gin.H{
				"success":  true,
				"eligible": false,
				"reason":   "order_lookup_failed",
				"message":  "查询订单失败（" + err.Error() + "），无法判断是否还能撤回，请重试",
				"reasons":  retractionReasons,
			})
			return
		}
		if !found {
			// retractableOrderFor 扫的是"最近 21 天里还在撤回期内的订单",
			// 所以没找到有好几种含义,不能笼统说成"没找到订单"——
			// 那听起来像系统出了问题,而实际上多半是这台机器本来就不在撤回期内。
			c.JSON(http.StatusOK, gin.H{
				"success":  true,
				"eligible": false,
				"reason":   "no_retractable_order",
				"message": fmt.Sprintf(
					"最近 %d 天内没有找到这台机器仍在撤回期的订单。可能是："+
						"① 这台机器下单已超过 %d 天；② v0.1.25 之前下的单在结账时就放弃了撤回权"+
						"（之后下的单不会）；③ 撤回期已经结束",
					retractionScanDays, retractionScanDays),
				"reasons": retractionReasons,
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
			// OVH 没给撤回截止日 = 它认为这单没有撤回权。
			//
			// 原因不止一种,不能像以前那样只报"下单时弃权了"—— 那是把其中一种可能
			// 说成了确定结论。企业账户(legalform 不是 individual)本来就没有消费者
			// 撤回权,某些产品也可能不在范围内。这里只陈述事实 + 列可能性,
			// 不替 OVH 解释它为什么不给。
			c.JSON(http.StatusOK, gin.H{
				"success":  true,
				"eligible": false,
				"reason":   "no_retraction_right",
				"orderId":  orderID,
				"orderUrl": orderURL,
				"message": "OVH 没有给这张订单撤回期。常见原因：" +
					"① v0.1.24 之前下的单在结账时就放弃了撤回权；" +
					"② 企业/机构账户没有消费者撤回权；③ 该产品不在撤回范围内",
				"reasons": retractionReasons,
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
		acc, ok := ovhAccountFor(state, c)
		if !ok {
			noOVHResp(c)
			return
		}
		if !retractionSupportedRegion(acc.Endpoint) {
			// 前端在 GET 那步就不会显示按钮,这里是纵深防御:接口可以被直接调,
			// 而打过去的结果是 OVH 404,报错会很难懂
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "美区（OVHcloud US）的 API 没有撤单接口，无法从这里申请撤单",
			})
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

		orderID, found, err := retractableOrderFor(state, c, client, acc.ID, svc)
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
