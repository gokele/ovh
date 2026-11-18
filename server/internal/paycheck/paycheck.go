// Package paycheck 核对「请求了自动付款」的订单是否真的付上了。
//
// 官方依据(EU/CA/US /1.0/order.json 与 /1.0/me.json):
//   - 结账参数 autoPayWithPreferredPaymentMethod 只表示「请用默认支付方式自动付款」。
//     默认支付方式(me.payment.PaymentMethod.default=true)不存在或状态不是 VALID 时,
//     OVH 不会扣款,订单保持 notPaid 并在到期后作废。
//   - 订单是否已付以 GET /me/order/{orderId}/status(billing.order.OrderStatusEnum)为准:
//     notPaid 未付;checking / delivering / delivered 表示已进入付款后的处理;cancelled / cancelling 已取消。
package paycheck

import (
	"fmt"
	"time"

	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/notify"
)

// Schedule 结账后依次等待这些间隔再查一次状态;总计约 3 分钟。
var Schedule = []time.Duration{30 * time.Second, time.Minute, 90 * time.Second}

// Watch 在后台核对自动付款结果:付上了只记日志,仍未付或已取消时通知用户。
// label 是通知里的订单描述(型号@机房),link 是需要登录的订单页链接。
func Watch(state *app.State, client *ovhsdk.Client, label, orderID, link string) {
	go run(state, client, label, orderID, link, time.Sleep)
}

// run 按 Schedule 轮询订单状态;sleep 可注入以便测试。
func run(state *app.State, client *ovhsdk.Client, label, orderID, link string, sleep func(time.Duration)) {
	last := ""
	for _, delay := range Schedule {
		sleep(delay)
		var status string
		if err := client.Get("/me/order/"+orderID+"/status", &status); err != nil {
			state.Logger.Debug(fmt.Sprintf("自动付款核对:读取订单 %s 状态失败: %s", orderID, err.Error()), "purchase")
			continue
		}
		last = status
		switch status {
		case "checking", "delivering", "delivered":
			state.Logger.Info(fmt.Sprintf("自动付款核对:订单 %s 已付款(状态 %s)", orderID, status), "purchase")
			return
		case "cancelled", "cancelling":
			notify.Broadcast(state, fmt.Sprintf("⚠️ %s 的订单 %s 已被取消（状态 %s），自动付款未完成。\n订单页: %s", label, orderID, status, link), nil)
			return
		}
	}
	if last == "" {
		notify.Broadcast(state, fmt.Sprintf("⚠️ %s 的订单 %s 请求了自动付款，但几分钟内无法读取订单状态，请到订单页确认是否已扣款。\n订单页: %s", label, orderID, link), nil)
		return
	}
	notify.Broadcast(state, fmt.Sprintf("⚠️ %s 的订单 %s 请求了自动付款，但结账约 3 分钟后仍是「%s」，自动付款没有完成。\n"+
		"常见原因：账户没有有效的默认支付方式（已过期、被拒或未设置）。请尽快在订单页手动付款，逾期订单会自动作废。\n订单页: %s",
		label, orderID, last, link), nil)
}
