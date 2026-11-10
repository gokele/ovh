package orderterm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

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
	type cartItem struct {
		ID       int64   `json:"itemId"`
		Duration string  `json:"duration"`
		Options  []int64 `json:"options"`
		Settings struct {
			PlanCode string `json:"planCode"`
			Quantity *int64 `json:"quantity"`
		} `json:"settings"`
	}
	pending := cart.Items
	seen := map[int64]bool{}
	stored := map[string]int{}
	for len(pending) > 0 {
		// 同一层的项目并行读取(只读);校验仍按原顺序逐项进行,报错信息不变。
		var batch []int64
		queued := map[int64]bool{}
		for _, id := range pending {
			if id <= 0 {
				return fmt.Errorf("%w: 无效的项目编号", ErrCartPeriod)
			}
			if seen[id] || queued[id] {
				continue
			}
			queued[id] = true
			batch = append(batch, id)
		}
		pending = nil
		if len(seen)+len(batch) > 256 {
			return fmt.Errorf("%w: 项目数量异常", ErrCartPeriod)
		}
		items := make([]cartItem, len(batch))
		if err := getItems(ctx, client, cartID, batch, func(i int) interface{} { return &items[i] }); err != nil {
			return err
		}
		for i, id := range batch {
			item := items[i]
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

// getItems 并行读取购物车项目。go-ovh 的 NewRequest 会改写共享 http.Client.Timeout,
// 所以先在当前 goroutine 依次签名,再并发发送;任一失败即返回第一个错误。
func getItems(ctx context.Context, client *ovhsdk.Client, cartID string, ids []int64, target func(int) interface{}) error {
	requests := make([]*http.Request, len(ids))
	for i, id := range ids {
		req, err := client.NewRequest(http.MethodGet, fmt.Sprintf("/order/cart/%s/item/%d", cartID, id), nil, true)
		if err != nil {
			return fmt.Errorf("读取项目 %d 购买周期失败: %w", id, err)
		}
		requests[i] = req.WithContext(ctx)
	}
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	limit := make(chan struct{}, 8)
	for i := range requests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			resp, err := client.Do(requests[i])
			if err == nil {
				err = client.UnmarshalResponse(resp, target(i))
			}
			if err != nil {
				errs[i] = fmt.Errorf("读取项目 %d 购买周期失败: %w", ids[i], err)
			}
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
