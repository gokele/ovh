package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
)

// managerOrderURL 订单在 OVH 控制面板里的深链。
//
// 三个大区各有各的控制面板,彼此不认对方的订单号(账户体系本来就独立):
//
//	EU → https://www.ovh.com/manager/      (301 → manager.eu.ovhcloud.com)
//	CA → https://ca.ovh.com/manager/       (301 → manager.ca.ovhcloud.com)
//	US → https://us.ovhcloud.com/manager/  (301 → manager.us.ovhcloud.com)
//
// 以前这里写死 www.ovh.com,US / CA 账户点开链接会落到欧洲面板,
// 面板里根本没有这个订单号,只能看到一个"订单不存在"。
// 直接用重定向后的 manager.*.ovhcloud.com,少一跳也少一次 www 侧的地区改写。
func managerOrderURL(endpoint string, orderID int64) string {
	host := "manager.eu.ovhcloud.com"
	switch ovh.EndpointRegion(endpoint) {
	case "US":
		host = "manager.us.ovhcloud.com"
	case "CA":
		host = "manager.ca.ovhcloud.com"
	}
	return fmt.Sprintf("https://%s/dedicated/#/billing/order?orderId=%d", host, orderID)
}

// orderMappingEntry 一个账户的订单映射结果
type orderMappingEntry struct {
	mapping map[string]interface{}
	at      time.Time
}

// orderMappingCache 简单内存缓存。
// /me/order 是 per-nichandle 数据(不同 consumer key 拿到的订单集合完全不同),
// 所以缓存必须按账户分 key,否则 A 账户同步完 10 分钟内 B 账户会拿到 A 的订单号。
var (
	orderMappingMu       sync.Mutex
	orderMappingCache    = map[string]orderMappingEntry{} // accountID → 结果
	orderMappingDuration = 10 * time.Minute
)

// usOrderScanLimit US 的 /me/order 不支持任何服务端过滤,只能拉全量 id 再本地筛。
// 老账户可能有上千订单,每个订单还要打 status/details/order 三次 API,
// 所以按订单号倒序(OVH 订单号递增,号大的更新)只回扫最近这么多个。
const usOrderScanLimit = 200

// invalidateOrderMappingCache 清掉指定账户的订单映射缓存(删账户时调)
func invalidateOrderMappingCache(accountID string) {
	orderMappingMu.Lock()
	delete(orderMappingCache, accountID)
	orderMappingMu.Unlock()
}

// GetOrderMapping GET /api/server-control/order-mapping
func GetOrderMapping(state *app.State) gin.HandlerFunc {
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
		// 用解析后的账户 ID 做 key,保证 ?account= 省略(默认账户)和显式传默认账户 id 命中同一条
		cacheKey := acc.ID
		forceRefresh := strings.EqualFold(c.Query("forceRefresh"), "true")

		orderMappingMu.Lock()
		if entry, hit := orderMappingCache[cacheKey]; !forceRefresh && hit && time.Since(entry.at) < orderMappingDuration {
			cached := entry.mapping
			orderMappingMu.Unlock()
			state.Logger.Info(fmt.Sprintf("返回账户 %s 缓存的订单映射数据（共 %d 条）", acc.Name, len(cached)), "server_control")
			c.JSON(http.StatusOK, gin.H{
				"success":   true,
				"mapping":   cached,
				"cached":    true,
				"cacheTime": entry.at.UTC().Format(time.RFC3339),
			})
			return
		}
		orderMappingMu.Unlock()

		state.Logger.Info("开始同步订单映射数据...", "server_control")

		// degraded 标记本次结果是否不完整。不完整的结果不写缓存,
		// 否则一次 OVH 抖动会让残缺映射粘住 10 分钟,用户再点同步还是同样的残缺数据。
		degraded := false
		// truncated 只表示"扫描范围被上限截断",它是稳定的、可复现的结果,
		// 和 OVH 抖动导致的残缺不是一回事,所以不影响缓存。
		truncated := false

		// 1. 获取所有服务器创建时间(并发拉 serviceInfos)
		var serverList []string
		if err := client.Get("/dedicated/server", &serverList); err != nil {
			// /dedicated/server 没有"未开通"语义:没有服务器时返回空数组而不是报错。
			// 所以这里的任何错误都是真错误,原样回给前端,不要退化成 30 天窗口再报 success。
			state.Logger.Error("获取服务器列表失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取服务器列表失败: " + err.Error()})
			return
		}
		svcDetails, svcFailed, svcErr := parallelGetStringsCounted(client, serverList, func(sn string) string {
			return "/dedicated/server/" + sn + "/serviceInfos"
		}, 10)
		if svcFailed > 0 {
			degraded = true
			msg := ""
			if svcErr != nil {
				msg = svcErr.Error()
			}
			// 少几台服务器会让下面的时间范围收窄,订单窗口跟着变窄,老服务器就查不到订单了
			state.Logger.Warn(fmt.Sprintf("%d/%d 台服务器的 serviceInfos 拉取失败,订单查询时间范围可能偏窄: %s",
				svcFailed, len(serverList), msg), "server_control")
		}
		creationDates := []string{}
		for _, svcInfo := range svcDetails {
			if svcInfo == nil {
				continue
			}
			if cd, ok := svcInfo["creation"].(string); ok && cd != "" {
				creationDates = append(creationDates, cd)
			}
		}

		var dateFrom, dateTo time.Time
		parsedDates := []time.Time{}
		for _, ds := range creationDates {
			t, err := parseFlexible(ds)
			if err == nil {
				parsedDates = append(parsedDates, t)
			}
		}
		if len(parsedDates) > 0 {
			earliest := parsedDates[0]
			latest := parsedDates[0]
			for _, t := range parsedDates[1:] {
				if t.Before(earliest) {
					earliest = t
				}
				if t.After(latest) {
					latest = t
				}
			}
			dateFrom = earliest.Add(-15 * 24 * time.Hour)
			dateTo = latest.Add(15 * 24 * time.Hour)
			state.Logger.Info(fmt.Sprintf("服务器创建时间范围: %s 到 %s, 订单查询范围: %s 到 %s",
				earliest.Format("2006-01-02"), latest.Format("2006-01-02"),
				dateFrom.Format("2006-01-02"), dateTo.Format("2006-01-02")), "server_control")
		} else {
			dateTo = time.Now().UTC()
			dateFrom = dateTo.Add(-30 * 24 * time.Hour)
			state.Logger.Info("没有可用的服务器创建时间,订单查询退回最近 30 天", "server_control")
		}

		// 2. 取订单 id 列表
		// 按大区判断而不是比 "ovh-us" 字符串:endpoint 是用户可填字段,
		// 归属一律走 ovh 包那张权威表(kimsufi/soyoustart 别名也能正确归大区)。
		isUS := ovh.EndpointRegion(acc.Endpoint) == "US"
		var allOrderIDs []int64
		orderDates := map[int64]string{}
		orderDateFailedIDs := map[int64]bool{}
		// scanHint 给用户解释"为什么映射是空的",空字符串表示没什么好解释的
		scanHint := ""
		if isUS {
			// US 的 /me/order 一个 query 参数都没有(本地 schema 与 live api.us.ovhcloud.com/1.0/me.json 均确认)。
			// OVH 对未声明参数是 400 拒绝还是静默忽略没有任何保证,不赌它的宽容度:
			// 干脆不传 date.*,改成按订单号倒序回扫最近 usOrderScanLimit 个,再用订单自己的 date 本地过滤。
			if err := client.Get("/me/order", &allOrderIDs); err != nil {
				state.Logger.Error("获取订单列表失败: "+err.Error(), "server_control")
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取订单列表失败: " + err.Error()})
				return
			}
			sort.Slice(allOrderIDs, func(i, j int) bool { return allOrderIDs[i] > allOrderIDs[j] })
			if len(allOrderIDs) > usOrderScanLimit {
				state.Logger.Warn(fmt.Sprintf("US 账户共有 %d 个订单,本次只回扫最近 %d 个", len(allOrderIDs), usOrderScanLimit), "server_control")
				allOrderIDs = allOrderIDs[:usOrderScanLimit]
				truncated = true
			}
			// 本地按时间窗口过滤:date 是可空字段,取不到就保留(宁可多查一单也不要漏)
			orderDates, orderDateFailedIDs = fetchOrderDates(client, allOrderIDs, 10)
			inWindow := []int64{}
			var oldest, newest time.Time
			for _, id := range allOrderIDs {
				t, err := parseFlexible(orderDates[id])
				if err == nil {
					if oldest.IsZero() || t.Before(oldest) {
						oldest = t
					}
					if newest.IsZero() || t.After(newest) {
						newest = t
					}
				}
				if err != nil || (!t.Before(dateFrom) && !t.After(dateTo)) {
					inWindow = append(inWindow, id)
				}
			}
			// 回扫窗口(按订单号倒序取的最近 N 单)和目标时间窗(由服务器开通时间推出来的)
			// 完全可能不相交:一台 5 年前开通的老服务器,目标窗落在很久以前,而我们扫的是最新的 N 单。
			// 这种情况下 mapping 必然是空的,必须说清楚,否则用户只看到一个 truncated:true 无从下手。
			if len(inWindow) == 0 && !oldest.IsZero() {
				scanHint = fmt.Sprintf("本次只回扫了最近 %d 个订单(下单时间 %s ~ %s),与目标时间范围 %s ~ %s 没有交集,因此没有匹配到任何订单;这些服务器的订单可能更早,需要放宽回扫上限才能查到",
					len(allOrderIDs),
					oldest.Format("2006-01-02"), newest.Format("2006-01-02"),
					dateFrom.Format("2006-01-02"), dateTo.Format("2006-01-02"))
				state.Logger.Warn("[订单映射] "+scanHint, "server_control")
			}
			allOrderIDs = inWindow
		} else {
			// EU / CA 的 /me/order 支持 date.from / date.to(可选 datetime),交给服务端过滤
			dateFromStr := dateFrom.UTC().Format("2006-01-02T15:04:05+00:00")
			dateToStr := dateTo.UTC().Format("2006-01-02T15:04:05+00:00")
			state.Logger.Debug("日期范围查询: from="+dateFromStr+", to="+dateToStr, "server_control")
			path := "/me/order?date.from=" + url.QueryEscape(dateFromStr) + "&date.to=" + url.QueryEscape(dateToStr)
			if err := client.Get(path, &allOrderIDs); err != nil {
				state.Logger.Error("获取订单列表失败: "+err.Error(), "server_control")
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取订单列表失败: " + err.Error()})
				return
			}
		}
		state.Logger.Info(fmt.Sprintf("时间范围内获取到 %d 个订单", len(allOrderIDs)), "server_control")

		// 3. 过滤已取消订单（5 并发）
		// billing.order.OrderStatusEnum 的合法值只有
		// cancelled / cancelling / checking / delivered / delivering / documentsRequested / notPaid / unknown,
		// 取消态就 cancelled 和 cancelling 两个(旧代码比的 cancelledByCustomer* 是不存在的死条件)。
		validIDs := []int64{}
		statuses := map[int64]string{}
		skippedCancelled := 0
		statusFailed := 0
		statusCounts := map[string]int{}
		var muVal sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 5)
		for _, oid := range allOrderIDs {
			wg.Add(1)
			sem <- struct{}{}
			go func(id int64) {
				defer wg.Done()
				defer func() { <-sem }()
				var status string
				if err := ovhGetWithRetry(client, fmt.Sprintf("/me/order/%d/status", id), &status); err != nil {
					muVal.Lock()
					statusFailed++
					muVal.Unlock()
					return
				}
				lower := strings.ToLower(status)
				muVal.Lock()
				statusCounts[status]++
				if lower == "cancelled" || lower == "cancelling" {
					skippedCancelled++
				} else {
					statuses[id] = status
					validIDs = append(validIDs, id)
				}
				muVal.Unlock()
			}(oid)
		}
		wg.Wait()
		if statusFailed > 0 {
			degraded = true
			state.Logger.Warn(fmt.Sprintf("%d 个订单的状态拉取失败,这些订单本次不会出现在映射里", statusFailed), "server_control")
		}
		if len(statusCounts) > 0 {
			parts := []string{}
			for k, v := range statusCounts {
				parts = append(parts, fmt.Sprintf("%s: %d", k, v))
			}
			state.Logger.Info("订单状态统计: "+strings.Join(parts, ", "), "server_control")
		}
		state.Logger.Info(fmt.Sprintf("过滤后得到 %d 个有效订单（跳过 %d 个已取消/取消中订单,%d 个状态获取失败）",
			len(validIDs), skippedCancelled, statusFailed), "server_control")

		// 4. 取订单日期。US 分支上面已经拉过一遍了,别再打一次。
		if !isUS {
			orderDates, orderDateFailedIDs = fetchOrderDates(client, validIDs, 10)
		}
		// orderInfoFailed 只统计"真正会参与映射"的订单:US 分支要对最多 usOrderScanLimit 个订单
		// 拉日期,其中大量订单随后会被取消状态/时间窗过滤掉,把它们的失败也算进来会导致
		// 「账户越大越容易 degraded → 越不缓存 → 下次又全量重扫」的放大效应。
		orderInfoFailed := 0
		for _, id := range validIDs {
			if orderDateFailedIDs[id] {
				orderInfoFailed++
			}
		}
		if orderInfoFailed > 0 {
			degraded = true
			// 日期缺失会让"同一服务器保留哪张订单"的比较失真,必须报出来
			state.Logger.Warn(fmt.Sprintf("%d 个订单的详情(/me/order/{id})拉取失败,这些订单的下单日期会是空的", orderInfoFailed), "server_control")
		}

		// 5. 处理订单明细（10 并发）
		mapping := map[string]map[string]interface{}{}
		var muMap sync.Mutex
		var wg2 sync.WaitGroup
		sem2 := make(chan struct{}, 10)
		processed := 0
		errorCount := 0
		// detailItemFailed 明细项级(/me/order/{id}/details/{did})的失败。
		// 以前是裸 continue:一条明细拉不到,那台服务器就整条从 mapping 里消失,
		// 而 degraded 不置位、残缺结果照样进缓存,粘住 10 分钟。
		detailItemFailed := 0
		for _, oid := range validIDs {
			wg2.Add(1)
			sem2 <- struct{}{}
			go func(id int64) {
				defer wg2.Done()
				defer func() { <-sem2 }()
				var detailIDs []int64
				if err := ovhGetWithRetry(client, fmt.Sprintf("/me/order/%d/details", id), &detailIDs); err != nil {
					muMap.Lock()
					errorCount++
					muMap.Unlock()
					return
				}
				muMap.Lock()
				orderDate := orderDates[id]
				orderStatus := statuses[id]
				muMap.Unlock()
				if orderStatus == "" {
					orderStatus = "unknown"
				}
				orderURL := managerOrderURL(acc.Endpoint, id)

				for _, did := range detailIDs {
					var d map[string]interface{}
					if err := ovhGetWithRetry(client, fmt.Sprintf("/me/order/%d/details/%d", id, did), &d); err != nil {
						muMap.Lock()
						detailItemFailed++
						muMap.Unlock()
						continue
					}
					// billing.OrderDetail.cancelled 是必填 boolean:整单没取消但某条明细被取消是常态,
					// 这种明细不能算作有效的服务器订单映射
					if cancelled, _ := d["cancelled"].(bool); cancelled {
						continue
					}
					serviceName, _ := d["domain"].(string)
					description, _ := d["description"].(string)
					if serviceName == "" {
						continue
					}
					isDedicated := strings.Contains(strings.ToLower(description), "dedicated") ||
						strings.Contains(strings.ToLower(description), "server") ||
						strings.Contains(serviceName, ".ip-") ||
						strings.HasPrefix(serviceName, "ns")
					if !isDedicated {
						continue
					}
					info := map[string]interface{}{
						"orderId":     id,
						"orderDate":   orderDate,
						"orderStatus": orderStatus,
						"orderUrl":    orderURL,
						"detailId":    did,
						"price":       d["totalPrice"],
						"description": description,
					}
					muMap.Lock()
					existing, exists := mapping[serviceName]
					if !exists {
						mapping[serviceName] = info
						processed++
						state.Logger.Info(fmt.Sprintf("✅ 找到服务器映射: %s -> 订单 %d", serviceName, id), "server_control")
					} else {
						existingDate, _ := existing["orderDate"].(string)
						existingID, _ := existing["orderId"].(int64)
						if orderIsNewer(orderDate, id, existingDate, existingID) {
							mapping[serviceName] = info
							state.Logger.Info(fmt.Sprintf("🔄 更新服务器映射: %s -> 订单 %d (更新)", serviceName, id), "server_control")
						}
					}
					muMap.Unlock()
				}
			}(oid)
		}
		wg2.Wait()
		if errorCount > 0 {
			degraded = true
		}
		if detailItemFailed > 0 {
			degraded = true
			state.Logger.Warn(fmt.Sprintf("%d 条订单明细(details/{id})拉取失败,相关服务器可能没出现在映射里", detailItemFailed), "server_control")
		}

		final := map[string]interface{}{}
		for k, v := range mapping {
			final[k] = v
		}
		// 只有完整的结果才进缓存
		if degraded {
			state.Logger.Warn("本次订单映射结果不完整,跳过缓存,下次请求会重新同步", "server_control")
		} else {
			orderMappingMu.Lock()
			orderMappingCache[cacheKey] = orderMappingEntry{mapping: final, at: time.Now()}
			orderMappingMu.Unlock()
		}

		state.Logger.Info(fmt.Sprintf("订单映射同步完成: 成功处理 %d 个服务器映射，%d 个订单明细获取失败", processed, errorCount), "server_control")
		state.Logger.Info(fmt.Sprintf("返回订单映射数据: 共 %d 个映射", len(final)), "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success":           true,
			"mapping":           final,
			"total":             len(final),
			"processedOrders":   len(validIDs),
			"cached":            false,
			"degraded":          degraded,
			"truncated":         truncated,
			"skippedCancelled":  skippedCancelled,
			"serviceInfoFailed": svcFailed,
			"statusFailed":      statusFailed,
			"orderInfoFailed":   orderInfoFailed,
			"detailFailed":      errorCount,
			"detailItemFailed":  detailItemFailed,
			"hint":              scanHint,
			"syncTime":          time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// fetchOrderDates 并发拉订单主体,只为取 date 字段(billing.Order.date 是可空 datetime)。
// 失败不能悄悄当成空日期:空日期会让下面的"保留更新的那张订单"比较固定偏向先写入者,
// 所以第二个返回值给的是失败的订单 id 集合 —— 调用方要按"这单最后有没有真的用上"来计数,
// 只回一个总数会把被过滤掉的订单也算进 degraded。
func fetchOrderDates(client *ovhsdk.Client, ids []int64, concurrency int) (map[int64]string, map[int64]bool) {
	if concurrency <= 0 {
		concurrency = 10
	}
	dates := map[int64]string{}
	failed := map[int64]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for _, oid := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(id int64) {
			defer wg.Done()
			defer func() { <-sem }()
			var info map[string]interface{}
			if err := ovhGetWithRetry(client, fmt.Sprintf("/me/order/%d", id), &info); err != nil {
				mu.Lock()
				failed[id] = true
				mu.Unlock()
				return
			}
			d, _ := info["date"].(string)
			mu.Lock()
			dates[id] = d
			mu.Unlock()
		}(oid)
	}
	wg.Wait()
	return dates, failed
}

// orderIsNewer 同一台服务器出现在多张订单里时判断谁更新:
// 先按下单日期比(带时区偏移,必须解析后比而不是比字符串),
// 日期缺失或相同就退回订单号——OVH 订单号递增,号大的是后下的。
func orderIsNewer(date string, id int64, oldDate string, oldID int64) bool {
	ta, ea := parseFlexible(date)
	tb, eb := parseFlexible(oldDate)
	if ea == nil && eb == nil {
		if !ta.Equal(tb) {
			return ta.After(tb)
		}
		return id > oldID
	}
	if ea == nil {
		return true
	}
	if eb == nil {
		return false
	}
	return id > oldID
}

func parseFlexible(s string) (time.Time, error) {
	if strings.Contains(s, "T") {
		if s2 := strings.Replace(s, "Z", "+00:00", 1); s2 != "" {
			if t, err := time.Parse("2006-01-02T15:04:05-07:00", s2); err == nil {
				return t, nil
			}
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t, nil
			}
		}
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("无法解析日期: %s", s)
}
