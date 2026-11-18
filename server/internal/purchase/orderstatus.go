package purchase

import (
	"fmt"
	"strings"
	"time"

	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

// 订单支付状态。
//
// "下单成功"≠"已付款":checkout 是 autoPayWithPreferredPaymentMethod:false(除非开了
// 自动付款开关),订单创建后要用户自己付,逾期作废。历史里以前只有"success",
// 用户永远不知道这单到底付了没、有没有过期。
//
// 状态来自 GET /me/order/{orderId}/status,三区都有,返回 billing.order.OrderStatusEnum:
//
//	notPaid              待付款
//	checking             付款已收到,OVH 在核验
//	delivering           已付款,交付中
//	delivered            已交付(终态)
//	cancelling/cancelled 取消中/已取消(终态;过期未付也走这里)
//	documentsRequested   OVH 要求补材料
//	unknown
//
// 注意 /me/order/{id}/payment 端点美区没有,所以只用 /status 这一条。

// orderStatusTerminal 到了这些状态就不用再刷了
var orderStatusTerminal = map[string]bool{"delivered": true, "cancelled": true}

// orderStatusMinInterval 同一单两次刷新的最小间隔:手动狂点刷新也不该打爆 OVH
const orderStatusMinInterval = 2 * time.Minute

// orderStatusMaxAge 太老的订单不再刷:超过付款窗口很久还没终态,多半是 OVH 侧
// 状态不更新了,继续刷只是浪费配额
const orderStatusMaxAge = 30 * 24 * time.Hour

// FetchOrderStatus 读一次订单状态。响应体就是一个 JSON 字符串。
func FetchOrderStatus(client *ovhsdk.Client, orderID string) (string, error) {
	var status string
	if err := client.Get("/me/order/"+orderID+"/status", &status); err != nil {
		return "", err
	}
	return strings.TrimSpace(status), nil
}

// RefreshOrderStatuses 把所有还没到终态的成功订单刷一遍状态。
// force=true 忽略节流(手动刷新用),返回更新了几条。
func RefreshOrderStatuses(state *app.State, force bool) int {
	// 先拍快照,网络请求不能拿着 HistoryMu
	state.HistoryMu.Lock()
	type target struct {
		entryID, orderID, accountID, statusAt, status, purchaseTime string
	}
	var targets []target
	for _, h := range state.History {
		if h.Status != "success" || h.OrderID == "" || orderStatusTerminal[h.OrderStatus] {
			continue
		}
		// 按历史条目自己的 ID 定位,不能按 TaskID。
		// VPS 那边一条订阅下的每一单 TaskID 都是 "vps:"+sub.ID,同一个值;
		// 按 TaskID 回写只会命中第一条,于是第二单的状态被写到第一单头上,
		// 而第二单自己永远拿不到状态 → 每一轮都重新去查,直到 30 天上限。
		targets = append(targets, target{h.ID, h.OrderID, h.AccountID, h.OrderStatusAt, h.OrderStatus, h.PurchaseTime})
	}
	state.HistoryMu.Unlock()

	now := time.Now()
	updated := 0
	for _, t := range targets {
		// 这两处以前用 time.Parse(time.RFC3339, ...) 解 NowISO 写出来的时间戳,
		// 而 NowISO 不带时区 → 每次都解析失败 → `err == nil &&` 短路 → 两个节流
		// 全是死代码:30 天上限没生效,2 分钟最小间隔也没生效,
		// 后台每 10 分钟就把所有未终态订单全查一遍,手动刷新更是完全不设防。
		if pt, ok := types.ParseTS(t.purchaseTime); ok && now.Sub(pt) > orderStatusMaxAge {
			continue
		}
		if !force && t.statusAt != "" {
			if at, ok := types.ParseTS(t.statusAt); ok && now.Sub(at) < orderStatusMinInterval {
				continue
			}
		}
		client, err := state.OVH.IsolatedClientFor(t.accountID)
		if err != nil {
			// 账户删了 —— 这单的状态永远查不到了,不用每轮都报
			continue
		}
		status, err := FetchOrderStatus(client, t.orderID)
		if err != nil {
			state.Logger.Warn(fmt.Sprintf("查询订单 %s 状态失败: %s", t.orderID, err.Error()), "purchase")
			continue
		}
		if applyOrderStatus(state, t.entryID, status) {
			updated++
		}
	}
	if updated > 0 {
		go state.SaveHistory()
	}
	return updated
}

// applyOrderStatus 写回一条历史的状态。状态变了才算"更新"(日志用),
// 但 OrderStatusAt 每次都记,节流靠它。
func applyOrderStatus(state *app.State, entryID, status string) bool {
	state.HistoryMu.Lock()
	defer state.HistoryMu.Unlock()
	for i := range state.History {
		if state.History[i].ID != entryID {
			continue
		}
		changed := state.History[i].OrderStatus != status
		if changed && state.History[i].OrderStatus != "" {
			state.Logger.Info(fmt.Sprintf("订单 %s 状态: %s → %s", state.History[i].OrderID,
				state.History[i].OrderStatus, status), "purchase")
		}
		state.History[i].OrderStatus = status
		state.History[i].OrderStatusAt = types.NowISO()
		return changed
	}
	return false
}

// OrderStatusLoop 后台定时刷新。用户付款发生在下单之后的任意时刻,
// 只在下单那一刻查一次是不够的。
func OrderStatusLoop(state *app.State) {
	// 启动先等一会,别和开机时的一堆 OVH 请求挤在一起
	time.Sleep(90 * time.Second)
	for {
		if n := RefreshOrderStatuses(state, false); n > 0 {
			state.Logger.Info(fmt.Sprintf("后台刷新订单状态:%d 条有变化", n), "purchase")
		}
		time.Sleep(10 * time.Minute)
	}
}
