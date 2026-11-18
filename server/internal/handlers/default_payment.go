package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
)

// defaultPaymentInfo 自动付款能否生效的依据:账户的默认支付方式及其状态。
type defaultPaymentInfo struct {
	AccountID string `json:"accountId"`
	// Usable 默认支付方式存在、状态 VALID 且未过期;为 false 时 OVH 不会自动扣款
	Usable         bool   `json:"usable"`
	Found          bool   `json:"found"`
	Label          string `json:"label,omitempty"`
	PaymentType    string `json:"paymentType,omitempty"`
	Status         string `json:"status,omitempty"`
	ExpirationDate string `json:"expirationDate,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

// loadDefaultPayment 读取默认支付方式。官方 /1.0/me.json(EU/CA/US 一致):
// GET /me/payment/method?default=true 返回 ID 列表,GET /me/payment/method/{id} 给出
// default / status(VALID 才可扣款)/ expirationDate。只读,不修改任何支付设置。
func loadDefaultPayment(ctx context.Context, state *app.State, accountID string) (defaultPaymentInfo, error) {
	acc, ok := state.FindAccount(accountID)
	if !ok {
		return defaultPaymentInfo{}, fmt.Errorf("账户不存在")
	}
	info := defaultPaymentInfo{AccountID: acc.ID}
	client, err := state.OVH.IsolatedClientForAccount(acc)
	if err != nil {
		return info, err
	}
	var ids []int64
	if err := client.GetWithContext(ctx, "/me/payment/method?default=true", &ids); err != nil {
		return info, err
	}
	if len(ids) == 0 {
		info.Reason = "账户没有设置默认支付方式，自动付款不会扣款"
		return info, nil
	}
	var method registeredPaymentMethod
	if err := client.GetWithContext(ctx, fmt.Sprintf("/me/payment/method/%d", ids[0]), &method); err != nil {
		return info, err
	}
	info.Found = true
	info.Label, info.PaymentType, info.Status, info.ExpirationDate = method.Label, method.Type, method.Status, method.ExpirationDate
	switch {
	case !method.Default:
		info.Reason = "OVH 返回的支付方式不是默认支付方式"
	case method.Status != "VALID":
		info.Reason = "默认支付方式状态为 " + method.Status + "，自动付款不会扣款"
	default:
		info.Usable = true
		if method.ExpirationDate != "" {
			if expiry, ok := parseOVHTime(method.ExpirationDate); ok && !expiry.After(time.Now()) {
				info.Usable = false
				info.Reason = "默认支付方式已过期，自动付款不会扣款"
			}
		}
	}
	return info, nil
}

// GetDefaultPaymentMethod GET /api/ovh/account/default-payment-method?account=ID
// 开启自动付款前显示该账户的默认支付方式是否可用;读取失败不阻止开启,只如实返回错误。
func GetDefaultPaymentMethod(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		info, err := loadDefaultPayment(ctx, state, c.Query("account"))
		if err != nil {
			c.JSON(502, gin.H{"success": false, "error": "读取默认支付方式失败: " + ovh.Explain(err)})
			return
		}
		c.JSON(200, info)
	}
}
