package orderterm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

var ErrCartPeriod = errors.New("购物车购买周期不一致或无法确认，已停止下单")
var ErrCartConfiguration = errors.New("购物车实际配置不完整或无法确认，已停止下单")

// VerifyCart checks OVH's stored periods, including implicit and nested options.
// Run after the last cart mutation, so stock-triggered checkout needs no extra reads.
func VerifyCart(ctx context.Context, client *ovhsdk.Client, cartID string, baseItemID int64, wanted string, expectedPlanCodes ...string) error {
	if _, err := Normalize(wanted); err != nil {
		return err
	}
	var cart struct {
		Items []int64 `json:"items"`
	}
	if err := client.GetWithContext(ctx, "/order/cart/"+cartID, &cart); err != nil {
		return fmt.Errorf("读取购物车周期失败: %w", err)
	}
	if baseItemID <= 0 || len(cart.Items) == 0 {
		return fmt.Errorf("%w: 购物车没有可核验的主机项目", ErrCartPeriod)
	}
	pending := cart.Items
	seen := map[int64]bool{}
	stored := map[string]int{}
	for len(pending) > 0 {
		id := pending[0]
		pending = pending[1:]
		if id <= 0 {
			return fmt.Errorf("%w: 无效的项目编号", ErrCartPeriod)
		}
		if seen[id] {
			continue
		}
		if len(seen) >= 256 {
			return fmt.Errorf("%w: 项目数量异常", ErrCartPeriod)
		}
		var item struct {
			ID       int64   `json:"itemId"`
			Duration string  `json:"duration"`
			Options  []int64 `json:"options"`
			Settings struct {
				PlanCode string `json:"planCode"`
				Quantity *int64 `json:"quantity"`
			} `json:"settings"`
		}
		if err := client.GetWithContext(ctx, fmt.Sprintf("/order/cart/%s/item/%d", cartID, id), &item); err != nil {
			return fmt.Errorf("读取项目 %d 购买周期失败: %w", id, err)
		}
		if item.ID != id || item.Duration == "" || !Same(item.Duration, wanted) {
			return fmt.Errorf("%w: 项目 %d (%s) 实际周期 %q，要求 %s", ErrCartPeriod, id, item.Settings.PlanCode, item.Duration, Label(wanted))
		}
		seen[id] = true
		stored[item.Settings.PlanCode]++
		if len(expectedPlanCodes) > 0 && (stored[item.Settings.PlanCode] > 1 || (item.Settings.Quantity != nil && *item.Settings.Quantity != 1)) {
			return fmt.Errorf("%w: 项目 %s 数量不是单台或存在重复项目", ErrCartConfiguration, item.Settings.PlanCode)
		}
		if id == baseItemID && len(expectedPlanCodes) > 0 && item.Settings.PlanCode != expectedPlanCodes[0] {
			return fmt.Errorf("%w: 主机实际为 %q，要求 %s", ErrCartConfiguration, item.Settings.PlanCode, expectedPlanCodes[0])
		}
		pending = append(pending, item.Options...)
	}
	if !seen[baseItemID] {
		return fmt.Errorf("%w: 主机项目不在购物车中", ErrCartPeriod)
	}
	for _, code := range expectedPlanCodes {
		if stored[code] != 1 {
			return fmt.Errorf("%w: 缺少选项 %s", ErrCartConfiguration, code)
		}
	}
	if len(expectedPlanCodes) > 0 {
		for code := range stored {
			expected := false
			for _, wantedCode := range expectedPlanCodes {
				if code == wantedCode {
					expected = true
					break
				}
			}
			if !expected {
				// CPU follows the plan; OVH may add its required CPU/network defaults implicitly.
				for _, prefix := range []string{"cpu-", "processor-", "bandwidth-", "traffic-", "vrack-"} {
					if strings.HasPrefix(strings.ToLower(code), prefix) {
						expected = true
						break
					}
				}
			}
			if !expected {
				return fmt.Errorf("%w: 出现未选择的选项 %q", ErrCartConfiguration, code)
			}
		}
	}
	return nil
}
