package handlers

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

type orderPaymentMethod struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"`
	Label   string `json:"label"`
	Default bool   `json:"default"`
}
type orderPaymentInfo struct {
	account        types.OVHAccount
	client         *ovhsdk.Client
	AccountID      string               `json:"accountId"`
	AccountName    string               `json:"accountName"`
	OrderID        int64                `json:"orderId"`
	Status         string               `json:"status"`
	Amount         *float64             `json:"amount"`
	Currency       string               `json:"currency"`
	PriceText      string               `json:"priceText"`
	ExpirationDate string               `json:"expirationDate"`
	Attempted      bool                 `json:"attempted"`
	Methods        []orderPaymentMethod `json:"methods"`
	// Balances:账户里可用的信用余额(预付款/代金券/赠送额度/押金),Enough 表示同币种且足够付清整单。
	// 读取失败不影响绑定方式付款,只在 BalanceError 说明。
	Balances     []orderPaymentBalance `json:"balances"`
	BalanceError string                `json:"balanceError,omitempty"`
}
type orderPaymentBalance struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
	Text     string  `json:"text"`
	Enough   bool    `json:"enough"`
}

// maxOrderBalances 余额通常只有几项;上限防止异常账户拖慢付款预检。
const maxOrderBalances = 20

// loadOrderBalances 读取 GET /me/credit/balance 及详情(三区 PRODUCTION),只保留 usable 且金额为正的余额。
func loadOrderBalances(ctx context.Context, client *ovhsdk.Client, amount float64, currency string) ([]orderPaymentBalance, error) {
	var names []string
	if err := client.GetWithContext(ctx, "/me/credit/balance", &names); err != nil {
		return nil, err
	}
	if len(names) > maxOrderBalances {
		names = names[:maxOrderBalances]
	}
	out := []orderPaymentBalance{}
	for _, name := range names {
		var b struct {
			Name   string `json:"balanceName"`
			Type   string `json:"type"`
			Usable bool   `json:"usable"`
			Amount struct {
				Value    *float64 `json:"value"`
				Currency string   `json:"currencyCode"`
				Text     string   `json:"text"`
			} `json:"amount"`
		}
		if err := client.GetWithContext(ctx, "/me/credit/balance/"+url.PathEscape(name), &b); err != nil {
			return out, err
		}
		if b.Name == "" {
			b.Name = name
		}
		v := b.Amount.Value
		if !b.Usable || v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v <= 0 {
			continue
		}
		out = append(out, orderPaymentBalance{
			Name: b.Name, Type: b.Type, Amount: *v, Currency: b.Amount.Currency, Text: b.Amount.Text,
			Enough: b.Amount.Currency == currency && *v+1e-9 >= amount,
		})
	}
	return out, nil
}

type registeredPaymentMethod struct {
	ID             int64  `json:"paymentMethodId"`
	Type           string `json:"paymentType"`
	Label          string `json:"label"`
	Status         string `json:"status"`
	Default        bool   `json:"default"`
	ExpirationDate string `json:"expirationDate"`
}

// accountOrderStatuses 补齐付款状态,未知状态不开放付款入口。
func accountOrderStatuses(ctx context.Context, state *app.State, accountID string, orders []map[string]interface{}) {
	// 状态查询整体限时,权限不足或慢响应不拖住已有订单列表。
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	for _, order := range orders {
		sem <- struct{}{}
		wg.Add(1)
		go func(item map[string]interface{}) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				item["paymentStatusError"] = ctx.Err().Error()
				return
			}
			client, err := state.OVH.IsolatedClientFor(accountID)
			if err != nil {
				item["paymentStatusError"] = ovh.Explain(err)
				return
			}
			// SDK 的首次 /auth/time 不继承请求 ctx,另设相同超时上限。
			client.Timeout = 10 * time.Second
			var status string
			if err := client.GetWithContext(ctx, "/me/order/"+idToString(item["orderId"])+"/status", &status); err != nil {
				item["paymentStatusError"] = ovh.Explain(err)
			} else {
				item["orderStatus"] = effectiveOrderStatus(status, item["expirationDate"], time.Now())
			}
		}(order)
	}
	wg.Wait()
}

// effectiveOrderStatus 过了付款期限的未付款订单返回 "expired"。
//
// OVH 对没付款就过期的订单,/status 仍然返回 notPaid(实测 EU 独服订单 15 分钟就过期),
// 官方控制台把它们显示为已取消。照搬 notPaid 会让列表一直挂着「待付款 / 支付」,
// 点进去预检又因过期拒绝 —— 所以列表层直接按 expirationDate 换成 expired。
// 读不出期限时保持原状态,不猜。
func effectiveOrderStatus(status string, expiration interface{}, now time.Time) string {
	if status != "notPaid" {
		return status
	}
	raw, _ := expiration.(string)
	if raw == "" {
		return status
	}
	if expiry, ok := parseOVHTime(raw); ok && !expiry.After(now) {
		return "expired"
	}
	return status
}

// loadOrderPayment 只读取订单和已绑定方式,绝不发起扣款。
func loadOrderPayment(ctx context.Context, state *app.State, accountID string, orderID int64) (orderPaymentInfo, error) {
	info := orderPaymentInfo{OrderID: orderID, Methods: []orderPaymentMethod{}, Balances: []orderPaymentBalance{}}
	acc, ok := state.FindAccount(accountID)
	if !ok {
		return info, fmt.Errorf("下单账户不存在")
	}
	info.AccountID, info.AccountName = acc.ID, acc.Name
	client, err := state.OVH.IsolatedClientForAccount(acc)
	if err != nil {
		return info, err
	}
	info.account, info.client = acc, client
	client.Client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	path := fmt.Sprintf("/me/order/%d", orderID)
	var order struct {
		OrderID        int64  `json:"orderId"`
		ExpirationDate string `json:"expirationDate"`
		Price          struct {
			Value    *float64 `json:"value"`
			Currency string   `json:"currencyCode"`
			Text     string   `json:"text"`
		} `json:"priceWithTax"`
	}
	if err = client.GetWithContext(ctx, path, &order); err != nil {
		return info, err
	}
	if order.OrderID != orderID {
		return info, fmt.Errorf("订单响应与所选订单不符")
	}
	if err = client.GetWithContext(ctx, path+"/status", &info.Status); err != nil {
		return info, err
	}
	info.Amount, info.Currency, info.PriceText, info.ExpirationDate = order.Price.Value, order.Price.Currency, order.Price.Text, order.ExpirationDate
	if state.DB == nil {
		return info, fmt.Errorf("数据库不可用,不能安全确认付款状态")
	}
	info.Attempted, err = state.DB.GetKV(db.OrderPaymentKey(acc.ID, orderID), &struct {
		State string `json:"state"`
	}{})
	if err != nil {
		return info, err
	}
	if info.Status != "notPaid" || info.Attempted {
		return info, nil
	}
	if info.ExpirationDate != "" {
		expiry, valid := parseOVHTime(info.ExpirationDate)
		if !valid || !expiry.After(time.Now()) {
			return info, nil
		}
	}
	if info.Amount == nil || math.IsNaN(*info.Amount) || math.IsInf(*info.Amount, 0) || *info.Amount < 0 || info.Currency == "" {
		return info, fmt.Errorf("订单缺少可靠的金额或币种,请在 OVH 控制台付款")
	}
	if balances, balanceErr := loadOrderBalances(ctx, client, *info.Amount, info.Currency); balanceErr != nil {
		info.BalanceError = ovh.Explain(balanceErr)
	} else {
		info.Balances = balances
	}
	if ovh.EndpointRegion(acc.Endpoint) == "US" {
		var choices []struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		}
		if err = client.GetWithContext(ctx, path+"/availablePaymentMethod", &choices); err != nil {
			return info, err
		}
		for _, method := range choices {
			if method.ID > 0 {
				info.Methods = append(info.Methods, orderPaymentMethod{ID: method.ID, Type: method.Type, Label: method.Type})
			}
		}
		return info, nil
	}
	var methods struct {
		Registered []int64                   `json:"registered"`
		Methods    []registeredPaymentMethod `json:"paymentMethods"`
	}
	if err = client.GetWithContext(ctx, path+"/paymentMethods", &methods); err != nil {
		return info, err
	}
	available := map[int64]bool{}
	for _, id := range methods.Registered {
		available[id] = true
	}
	if len(methods.Methods) == 0 {
		for _, id := range methods.Registered {
			var method registeredPaymentMethod
			if err = client.GetWithContext(ctx, fmt.Sprintf("/me/payment/method/%d", id), &method); err != nil {
				return info, err
			}
			methods.Methods = append(methods.Methods, method)
		}
	}
	for _, method := range methods.Methods {
		if method.ID <= 0 || method.Status != "VALID" || (len(available) > 0 && !available[method.ID]) {
			continue
		}
		if method.ExpirationDate != "" {
			expiry, valid := parseOVHTime(method.ExpirationDate)
			if !valid || !expiry.After(time.Now()) {
				continue
			}
		}
		label := method.Label
		if label == "" {
			label = method.Type
		}
		info.Methods = append(info.Methods, orderPaymentMethod{ID: method.ID, Type: method.Type, Label: label, Default: method.Default})
	}
	return info, nil
}

// GetOrderPaymentOptions 返回可确认的金额、账户及此订单允许的绑定支付方式。
func GetOrderPaymentOptions(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("order_id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(400, gin.H{"error": "订单号无效"})
			return
		}
		info, err := loadOrderPayment(c.Request.Context(), state, c.Query("account"), id)
		if err != nil {
			c.JSON(502, gin.H{"error": "读取付款信息失败: " + ovh.Explain(err)})
			return
		}
		c.JSON(200, info)
	}
}

// PayAccountOrder 重新核对价格和方式,先持久登记再发送一次付款请求,不自动重试。
func PayAccountOrder(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("order_id"), 10, 64)
		var body struct {
			Confirm    bool     `json:"confirm"`
			MethodID   int64    `json:"paymentMethodId"`
			MethodType string   `json:"paymentType"`
			Amount     *float64 `json:"amount"`
			Currency   string   `json:"currency"`
		}
		if err != nil || id <= 0 || c.ShouldBindJSON(&body) != nil || !body.Confirm || body.MethodID <= 0 || body.Amount == nil || body.Currency == "" {
			c.JSON(400, gin.H{"error": "必须确认订单、金额及已绑定的支付方式"})
			return
		}
		info, err := loadOrderPayment(c.Request.Context(), state, c.Query("account"), id)
		if err != nil {
			c.JSON(502, gin.H{"error": "付款前核对失败: " + ovh.Explain(err)})
			return
		}
		if info.Status != "notPaid" || info.Attempted {
			c.JSON(409, gin.H{"error": "订单已处理或已有付款请求,请在 OVH 控制台核对,不要重复付款"})
			return
		}
		if info.Amount == nil || *info.Amount != *body.Amount || info.Currency != body.Currency {
			c.JSON(409, gin.H{"error": "订单金额或币种已变化,请重新打开确认窗口"})
			return
		}
		found := false
		for _, method := range info.Methods {
			if method.ID == body.MethodID && method.Type == body.MethodType {
				found = true
				break
			}
		}
		if !found {
			c.JSON(409, gin.H{"error": "订单已过期或该支付方式不可用,请刷新后重新选择"})
			return
		}
		current, exists := state.FindAccount(info.AccountID)
		if c.Request.Context().Err() != nil || !exists || !ovh.SameClientConfiguration(current, info.account) || current.Zone != info.account.Zone {
			c.JSON(http.StatusConflict, gin.H{"error": "账户配置已变化或请求已取消,未发起付款,请重新打开付款窗口核对"})
			return
		}
		claimed, err := state.DB.ClaimOrderPayment(info.AccountID, id)
		if err != nil {
			c.JSON(500, gin.H{"error": "付款请求未能登记,没有发起扣款"})
			return
		}
		if !claimed {
			c.JSON(409, gin.H{"error": "已有付款请求,请在 OVH 控制台核对,不要重复付款"})
			return
		}
		method := map[string]interface{}{"id": body.MethodID}
		if ovh.EndpointRegion(info.account.Endpoint) == "US" {
			method["type"] = body.MethodType
		}
		// 客户端断开后仍等 OVH 返回;结果不明确时保留登记,重启后也不重发。
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		err = info.client.PostWithContext(ctx, fmt.Sprintf("/me/order/%d/pay", id), map[string]interface{}{"paymentMethod": method}, nil)
		if err != nil {
			var apiErr *ovhsdk.APIError
			if errors.As(err, &apiErr) && apiErr.Code >= 400 && apiErr.Code < 500 && apiErr.Code != 408 && apiErr.Code != 409 && apiErr.Code != 429 {
				_ = state.DB.DeleteKV(db.OrderPaymentKey(info.AccountID, id))
				c.JSON(http.StatusBadGateway, gin.H{"error": "OVH 拒绝付款: " + ovh.Explain(err)})
				return
			}
			c.JSON(http.StatusConflict, gin.H{"error": "付款结果尚不能确认,请到 OVH 控制台核对,不要重复发起。" + ovh.Explain(err)})
			return
		}
		_ = state.DB.SetKV(db.OrderPaymentKey(info.AccountID, id), map[string]string{"state": "submitted"})
		state.Logger.Info(fmt.Sprintf("账户 %s 订单 %d 已提交绑定方式付款", info.AccountID, id), "payment")
		c.JSON(200, gin.H{"status": "submitted", "message": "付款请求已提交,请刷新核对订单状态；如需银行验证,请在 OVH 控制台完成。"})
	}
}

// PayAccountOrderWithBalance 用信用余额付清整单:POST /me/order/{orderId}/balance {balanceName}(三区 BETA)。
// 与绑定方式付款共用同一条订单付款登记,一个订单只发一次付款请求;只接受同币种且足够付清的余额,不做部分抵扣。
func PayAccountOrderWithBalance(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("order_id"), 10, 64)
		var body struct {
			Confirm     bool     `json:"confirm"`
			BalanceName string   `json:"balanceName"`
			Amount      *float64 `json:"amount"`
			Currency    string   `json:"currency"`
		}
		if err != nil || id <= 0 || c.ShouldBindJSON(&body) != nil || !body.Confirm || body.BalanceName == "" || body.Amount == nil || body.Currency == "" {
			c.JSON(400, gin.H{"error": "必须确认订单、金额及要使用的余额"})
			return
		}
		info, err := loadOrderPayment(c.Request.Context(), state, c.Query("account"), id)
		if err != nil {
			c.JSON(502, gin.H{"error": "付款前核对失败: " + ovh.Explain(err)})
			return
		}
		if info.Status != "notPaid" || info.Attempted {
			c.JSON(409, gin.H{"error": "订单已处理或已有付款请求,请在 OVH 控制台核对,不要重复付款"})
			return
		}
		if info.Amount == nil || *info.Amount != *body.Amount || info.Currency != body.Currency {
			c.JSON(409, gin.H{"error": "订单金额或币种已变化,请重新打开确认窗口"})
			return
		}
		found := false
		for _, b := range info.Balances {
			if b.Name == body.BalanceName && b.Enough {
				found = true
				break
			}
		}
		if !found {
			c.JSON(409, gin.H{"error": "该余额不可用、币种不同或不足以付清此订单,请刷新后重新选择"})
			return
		}
		current, exists := state.FindAccount(info.AccountID)
		if c.Request.Context().Err() != nil || !exists || !ovh.SameClientConfiguration(current, info.account) || current.Zone != info.account.Zone {
			c.JSON(http.StatusConflict, gin.H{"error": "账户配置已变化或请求已取消,未发起付款,请重新打开付款窗口核对"})
			return
		}
		claimed, err := state.DB.ClaimOrderPayment(info.AccountID, id)
		if err != nil {
			c.JSON(500, gin.H{"error": "付款请求未能登记,没有使用余额"})
			return
		}
		if !claimed {
			c.JSON(409, gin.H{"error": "已有付款请求,请在 OVH 控制台核对,不要重复付款"})
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		path := fmt.Sprintf("/me/order/%d/balance", id)
		err = info.client.PostWithContext(ctx, path, map[string]string{"balanceName": body.BalanceName}, nil)
		if err != nil {
			var apiErr *ovhsdk.APIError
			if errors.As(err, &apiErr) && apiErr.Code >= 400 && apiErr.Code < 500 && apiErr.Code != 408 && apiErr.Code != 409 && apiErr.Code != 429 {
				_ = state.DB.DeleteKV(db.OrderPaymentKey(info.AccountID, id))
				c.JSON(http.StatusBadGateway, gin.H{"error": "OVH 拒绝使用余额: " + ovh.Explain(err)})
				return
			}
			c.JSON(http.StatusConflict, gin.H{"error": "余额付款结果尚不能确认,请到 OVH 控制台核对,不要重复发起。" + ovh.Explain(err)})
			return
		}
		_ = state.DB.SetKV(db.OrderPaymentKey(info.AccountID, id), map[string]string{"state": "submitted", "method": "balance", "balance": body.BalanceName})
		state.Logger.Info(fmt.Sprintf("账户 %s 订单 %d 已提交余额 %s 付款", info.AccountID, id, body.BalanceName), "payment")
		// 只读核对一次订单状态,用于提示;不据此重发
		var status string
		statusCtx, statusCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer statusCancel()
		if info.client.GetWithContext(statusCtx, fmt.Sprintf("/me/order/%d/status", id), &status) == nil && status != "" && status != "notPaid" {
			c.JSON(200, gin.H{"status": "paid", "orderStatus": status, "message": "已用余额付款,订单状态: " + status})
			return
		}
		c.JSON(200, gin.H{"status": "submitted", "orderStatus": status, "message": "余额付款已提交,订单状态尚未更新,请稍后刷新核对。"})
	}
}
