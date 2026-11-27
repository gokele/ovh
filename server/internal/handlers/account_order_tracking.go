package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// 订单交付跟踪(只读)。官方签名(EU/CA/US /1.0/me.json):
//   - GET /me/order/{orderId}/status   PRODUCTION  billing.order.OrderStatusEnum
//   - GET /me/order/{orderId}/followUp BETA        billing.order.FollowUp[]:
//     step VALIDATING / VALIDATED / DELIVERING / AVAILABLE,status TODO / DOING / DONE / ERROR,
//     history[{date, label(FRAUD_CHECK / PAYMENT_RECEIVED / DELIVERY ...), description}]
//   - GET /me/order/{orderId}/details(+ /{orderDetailId}) PRODUCTION:商品明细
// followUp 是 BETA,读不到时只返回 followUpError,不影响状态和明细。

const maxTrackingDetails = 20

type trackingStep struct {
	Step    string            `json:"step"`
	Status  string            `json:"status"`
	History []trackingHistory `json:"history"`
}

type trackingHistory struct {
	Date        string `json:"date"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type trackingDetail struct {
	Description string  `json:"description"`
	DetailType  string  `json:"detailType"`
	Quantity    string  `json:"quantity"`
	Domain      string  `json:"domain"`
	Cancelled   bool    `json:"cancelled"`
	Total       *string `json:"total,omitempty"`
}

// GetOrderTracking GET /api/ovh/account/orders/:order_id/tracking?account=ID
func GetOrderTracking(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("order_id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "订单号无效"})
			return
		}
		acc, ok := ovhAccountFor(state, c)
		if !ok {
			noOVHRespAccount(c)
			return
		}
		client, err := state.OVH.IsolatedClientForAccount(acc)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		base := fmt.Sprintf("/me/order/%d", id)

		var status string
		if err := client.GetWithContext(ctx, base+"/status", &status); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "读取订单状态失败: " + ovh.Explain(err)})
			return
		}
		resp := gin.H{"accountId": acc.ID, "orderId": id, "status": status, "steps": []trackingStep{}, "details": []trackingDetail{}}

		// go-ovh 的 Client 不能并发使用:两路并行读取各用一个独立 client(共享 transport 连接池)
		detailClient, err := state.OVH.IsolatedClientForAccount(acc)
		if err != nil {
			noOVHRespAccount(c)
			return
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		wg.Add(2)
		go func() {
			defer wg.Done()
			steps, err := loadFollowUp(ctx, client, base)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				resp["followUpError"] = ovh.Explain(err)
				return
			}
			resp["steps"] = steps
		}()
		go func() {
			defer wg.Done()
			details, err := loadOrderDetails(ctx, detailClient, base)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				resp["detailsError"] = ovh.Explain(err)
				return
			}
			resp["details"] = details
		}()
		wg.Wait()
		c.JSON(http.StatusOK, resp)
	}
}

func loadFollowUp(ctx context.Context, client *ovhsdk.Client, base string) ([]trackingStep, error) {
	var raw []struct {
		Step    string `json:"step"`
		Status  string `json:"status"`
		History []struct {
			Date        string `json:"date"`
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"history"`
	}
	if err := client.GetWithContext(ctx, base+"/followUp", &raw); err != nil {
		return nil, err
	}
	steps := make([]trackingStep, 0, len(raw))
	for _, r := range raw {
		s := trackingStep{Step: r.Step, Status: r.Status, History: []trackingHistory{}}
		for _, h := range r.History {
			s.History = append(s.History, trackingHistory{Date: h.Date, Label: h.Label, Description: h.Description})
		}
		steps = append(steps, s)
	}
	return steps, nil
}

func loadOrderDetails(ctx context.Context, client *ovhsdk.Client, base string) ([]trackingDetail, error) {
	var ids []int64
	if err := client.GetWithContext(ctx, base+"/details", &ids); err != nil {
		return nil, err
	}
	if len(ids) > maxTrackingDetails {
		ids = ids[:maxTrackingDetails]
	}
	out := make([]trackingDetail, 0, len(ids))
	for _, did := range ids {
		var d struct {
			Description string `json:"description"`
			DetailType  string `json:"detailType"`
			Quantity    string `json:"quantity"`
			Domain      string `json:"domain"`
			Cancelled   bool   `json:"cancelled"`
			TotalPrice  struct {
				Text string `json:"text"`
			} `json:"totalPrice"`
		}
		if err := client.GetWithContext(ctx, fmt.Sprintf("%s/details/%d", base, did), &d); err != nil {
			return out, err
		}
		td := trackingDetail{Description: d.Description, DetailType: d.DetailType, Quantity: d.Quantity, Domain: d.Domain, Cancelled: d.Cancelled}
		if d.TotalPrice.Text != "" {
			t := d.TotalPrice.Text
			td.Total = &t
		}
		out = append(out, td)
	}
	return out, nil
}
