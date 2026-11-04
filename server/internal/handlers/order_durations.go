package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/orderterm"
	"github.com/ovh-buy/server/internal/ovh"
)

// GetOrderDurations reads actual cart offers; it never adds an item or checks out.
func GetOrderDurations(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		account, ok := state.FindAccount(c.Query("account_id"))
		product, plan := c.DefaultQuery("product", "eco"), strings.TrimSpace(c.Query("planCode"))
		if !ok || c.Query("account_id") == "" || plan == "" || (product != "eco" && product != "vps") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少有效账户、型号或产品类型"})
			return
		}
		subsidiary := catalog.SubsidiaryOfAccount(account)
		if selected := strings.ToUpper(strings.TrimSpace(c.Query("subsidiary"))); selected != "" && product == "vps" {
			if !ovh.KnownSubsidiary(selected) || ovh.SubsidiaryRegion(selected) != ovh.EndpointRegion(account.Endpoint) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "所选子公司与下单账户地区不一致"})
				return
			}
			subsidiary = selected
		}
		client, err := state.OVH.IsolatedClientForAccount(account)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		var cart struct {
			ID string `json:"cartId"`
		}
		if err := client.PostWithContext(ctx, "/order/cart", map[string]interface{}{"ovhSubsidiary": subsidiary}, &cart); err != nil || cart.ID == "" {
			c.JSON(http.StatusBadGateway, gin.H{"error": "无法读取 OVH 可选购买时长，请稍后重试"})
			return
		}
		defer func() {
			cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
			defer done()
			_ = client.DeleteWithContext(cleanup, "/order/cart/"+cart.ID, nil)
		}()
		if err := client.PostWithContext(ctx, "/order/cart/"+cart.ID+"/assign", nil, nil); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "无法读取 OVH 可选购买时长，请稍后重试"})
			return
		}
		offers, err := orderterm.Offers(ctx, client, cart.ID, product)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "无法读取 OVH 可选购买时长，请稍后重试"})
			return
		}
		for _, offer := range offers {
			if offer["planCode"] == plan {
				c.JSON(http.StatusOK, gin.H{"durations": orderterm.Durations(offer["prices"])})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"durations": []string{}})
	}
}
