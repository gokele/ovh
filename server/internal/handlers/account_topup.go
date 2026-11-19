package handlers

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// 预付款账户充值与券码兑换。
//
// 官方签名(EU/CA/US 的 /1.0/me.json):
//   - GET  /me/ovhAccount、GET /me/ovhAccount/{ovhAccountId}:EU/CA 有,US 没有这一组接口。
//   - POST /me/ovhAccount/{ovhAccountId}/creditOrder {amount}:amount 单位是「分」
//     ("The amount in cents you want to credit your account of"),只生成一张待付款订单,不扣款。
//   - POST /me/credit/code {inputCode, serviceId?}:三区都有,兑换后生成一笔余额变动。
//
// 两个写接口都不自动重试:充值订单重复生成不会扣款但会多出一张待付款订单,券码只能用一次。

// maxTopupCents 单次充值上限(10 万),防止多输一个 0 之类的误操作;实际限制以 OVH 返回为准。
const maxTopupCents = 10_000_000

type topupAccount struct {
	ID            string   `json:"id"`
	Balance       *float64 `json:"balance"`
	Currency      string   `json:"currency"`
	BalanceText   string   `json:"balanceText"`
	CanBeCredited bool     `json:"canBeCredited"`
	IsActive      bool     `json:"isActive"`
}

// topupSupported US 区没有 /me/ovhAccount 系列接口。
func topupSupported(endpoint string) bool { return ovh.EndpointRegion(endpoint) != "US" }

// GetTopupInfo GET /api/ovh/account/topup?account=ID:只读,列出可充值的预付款账户及当前余额。
func GetTopupInfo(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		acc, ok := ovhAccountFor(state, c)
		if !ok {
			noOVHRespAccount(c)
			return
		}
		resp := gin.H{"accountId": acc.ID, "supported": topupSupported(acc.Endpoint), "accounts": []topupAccount{}}
		if !topupSupported(acc.Endpoint) {
			c.JSON(http.StatusOK, resp)
			return
		}
		client, err := state.OVH.IsolatedClientForAccount(acc)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		ctx := c.Request.Context()
		var ids []string
		if err := client.GetWithContext(ctx, "/me/ovhAccount", &ids); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "读取预付款账户失败: " + ovh.Explain(err)})
			return
		}
		list := []topupAccount{}
		for i, id := range ids {
			if i >= 10 {
				break
			}
			var raw struct {
				ID      string `json:"ovhAccountId"`
				Balance struct {
					Value    *float64 `json:"value"`
					Currency string   `json:"currencyCode"`
					Text     string   `json:"text"`
				} `json:"balance"`
				CanBeCredited bool `json:"canBeCredited"`
				IsActive      bool `json:"isActive"`
			}
			if err := client.GetWithContext(ctx, "/me/ovhAccount/"+url.PathEscape(id), &raw); err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": "读取预付款账户详情失败: " + ovh.Explain(err)})
				return
			}
			if raw.ID == "" {
				raw.ID = id
			}
			list = append(list, topupAccount{ID: raw.ID, Balance: raw.Balance.Value, Currency: raw.Balance.Currency, BalanceText: raw.Balance.Text, CanBeCredited: raw.CanBeCredited, IsActive: raw.IsActive})
		}
		resp["accounts"] = list
		c.JSON(http.StatusOK, resp)
	}
}

// CreateTopupOrder POST /api/ovh/account/topup?account=ID
// body {confirm:true, ovhAccountId, amount}:amount 是用户输入的金额(最多两位小数),这里换算成分。
// 只生成待付款订单,返回订单号与 OVH 报价;付款走现有的订单付款流程,由用户再次确认。
func CreateTopupOrder(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Confirm      bool     `json:"confirm"`
			OvhAccountID string   `json:"ovhAccountId"`
			Amount       *float64 `json:"amount"`
		}
		if c.ShouldBindJSON(&body) != nil || !body.Confirm || body.OvhAccountID == "" || body.Amount == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "必须确认充值金额与预付款账户"})
			return
		}
		amount := *body.Amount
		cents := math.Round(amount * 100)
		if math.IsNaN(amount) || math.IsInf(amount, 0) || cents < 1 || cents > maxTopupCents || math.Abs(amount*100-cents) > 1e-6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "充值金额无效(大于 0、最多两位小数、不超过 100000)"})
			return
		}
		acc, ok := ovhAccountFor(state, c)
		if !ok {
			noOVHRespAccount(c)
			return
		}
		if !topupSupported(acc.Endpoint) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该账户所在站点(US)没有预付款账户充值接口"})
			return
		}
		client, err := state.OVH.IsolatedClientForAccount(acc)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		// 先核对该预付款账户属于此 OVH 账户且允许充值,不信任前端传来的 ID
		var target struct {
			CanBeCredited bool `json:"canBeCredited"`
			IsActive      bool `json:"isActive"`
		}
		if err := client.GetWithContext(c.Request.Context(), "/me/ovhAccount/"+url.PathEscape(body.OvhAccountID), &target); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "核对预付款账户失败,未生成订单: " + ovh.Explain(err)})
			return
		}
		if !target.CanBeCredited {
			c.JSON(http.StatusConflict, gin.H{"error": "OVH 显示该预付款账户当前不能充值,未生成订单"})
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		var order struct {
			OrderID        int64  `json:"orderId"`
			URL            string `json:"url"`
			ExpirationDate string `json:"expirationDate"`
			Price          struct {
				Value    *float64 `json:"value"`
				Currency string   `json:"currencyCode"`
				Text     string   `json:"text"`
			} `json:"priceWithTax"`
		}
		path := "/me/ovhAccount/" + url.PathEscape(body.OvhAccountID) + "/creditOrder"
		if err := client.PostWithContext(ctx, path, map[string]int64{"amount": int64(cents)}, &order); err != nil {
			var apiErr *ovhsdk.APIError
			if errors.As(err, &apiErr) && apiErr.Code >= 400 && apiErr.Code < 500 && apiErr.Code != 408 && apiErr.Code != 409 && apiErr.Code != 429 {
				c.JSON(http.StatusBadGateway, gin.H{"error": "OVH 拒绝生成充值订单: " + ovh.Explain(err)})
				return
			}
			c.JSON(http.StatusConflict, gin.H{"error": "充值订单结果尚不能确认,请先到订单记录或 OVH 控制台核对,不要重复生成。" + ovh.Explain(err)})
			return
		}
		if order.OrderID <= 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "OVH 未返回有效订单号,请到订单记录核对是否已生成充值订单"})
			return
		}
		state.Logger.Info(fmt.Sprintf("账户 %s 生成预付款充值订单 %d(%d 分)", acc.ID, order.OrderID, int64(cents)), "payment")
		c.JSON(http.StatusOK, gin.H{
			"accountId": acc.ID, "orderId": order.OrderID, "url": order.URL, "expirationDate": order.ExpirationDate,
			"amount": order.Price.Value, "currency": order.Price.Currency, "priceText": order.Price.Text,
		})
	}
}

// RedeemCreditCode POST /api/ovh/account/credit-code?account=ID body {code}:兑换券码到余额(三区 PRODUCTION)。
func RedeemCreditCode(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Code string `json:"code"`
		}
		if c.ShouldBindJSON(&body) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请输入券码"})
			return
		}
		code := strings.TrimSpace(body.Code)
		if code == "" || len(code) > 128 || strings.ContainsAny(code, " \t\r\n") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "券码格式无效"})
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		var movement struct {
			BalanceName string `json:"balanceName"`
			Type        string `json:"type"`
			Amount      struct {
				Value    *float64 `json:"value"`
				Currency string   `json:"currencyCode"`
				Text     string   `json:"text"`
			} `json:"amount"`
			ExpirationDate string `json:"expirationDate"`
		}
		if err := client.PostWithContext(ctx, "/me/credit/code", map[string]string{"inputCode": code}, &movement); err != nil {
			var apiErr *ovhsdk.APIError
			if errors.As(err, &apiErr) && apiErr.Code >= 400 && apiErr.Code < 500 && apiErr.Code != 408 && apiErr.Code != 429 {
				c.JSON(http.StatusBadGateway, gin.H{"error": "OVH 拒绝兑换: " + ovh.Explain(err)})
				return
			}
			c.JSON(http.StatusConflict, gin.H{"error": "兑换结果尚不能确认,请刷新余额核对后再决定是否重试。" + ovh.Explain(err)})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"balanceName": movement.BalanceName, "type": movement.Type, "amount": movement.Amount.Value,
			"currency": movement.Amount.Currency, "text": movement.Amount.Text, "expirationDate": movement.ExpirationDate,
		})
	}
}
