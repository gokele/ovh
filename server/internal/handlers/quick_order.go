package handlers

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/numconv"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/price"
	"github.com/ovh-buy/server/internal/types"
)

// quickOrderMu 串行化 quick-order 入队的逻辑,避免并发同 plan@dc 重复入队
var quickOrderMu sync.Mutex

// QuickOrder POST /api/queue/quick-order
// 监控触发的"立即下单"或者外部主动调用:验证账户 + 拉一次价格 → 直接塞队列头(高优先级 + 2 秒重试)。
// 这条端点是 monitor.batchOrder() auto-order 触发时走的 HTTP 路径。
func QuickOrder(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			AccountID          string   `json:"account_id"` // 必填,哪个账户下单
			PlanCode           string   `json:"planCode"`
			Datacenter         string   `json:"datacenter"`
			Options            []string `json:"options"`
			FromMonitor        bool     `json:"fromMonitor"`
			SkipDuplicateCheck bool     `json:"skipDuplicateCheck"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.PlanCode == "" || body.Datacenter == "" {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "缺少 planCode 或 datacenter"})
			return
		}
		if body.AccountID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 account_id"})
			return
		}
		if _, ok := state.FindAccount(body.AccountID); !ok {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "account_id 不存在"})
			return
		}
		options := body.Options
		if len(options) == 0 {
			availByConfig := catalog.CheckServerAvailabilityWithConfigs(state, body.PlanCode, body.AccountID)
			// cfg.Datacenters 的 key 是 OVH 原样返回的 AvailabilityDatacenterEnum(孟买是 ynm),
			// 而 body.Datacenter 是前端显示码(孟买是 mum),必须和 purchase / price 一样先转换
			dcKey := ovh.ConvertDisplayDCToAPIDC(body.Datacenter)
			// ConfigAvailability 用三个字段表达三种完全不同的"没配上":
			//   CatalogError + CatalogMissing=false → OVH 目录拉取失败(网络/429),重试有用
			//   CatalogMissing=true                 → 目录拉到了,但本子公司目录没这个 planCode,
			//                                         此时 CatalogError 里已经是完整的人话说明
			//   OptionsNote                         → 目录和 plan 都在,只是这套内存/存储在
			//                                         本子公司买不到(实测 US 35 条、EU/CA 各 18 条)
			// 以前这三种被一律拼成"OVH 目录拉取失败："+原文,于是 CatalogMissing 那条
			// 会显示成"OVH 目录拉取失败：XX 子公司的目录里没有 planCode YY",自相矛盾。
			catalogErr := ""
			catalogMissingMsg := ""
			optionsNote := ""
			dcSeen := false
			for _, cfg := range availByConfig {
				if cfg.CatalogMissing && catalogMissingMsg == "" {
					catalogMissingMsg = cfg.CatalogError
				} else if cfg.CatalogError != "" && !cfg.CatalogMissing && catalogErr == "" {
					catalogErr = cfg.CatalogError
				}
				dcStatus, ok := cfg.Datacenters[dcKey]
				if !ok || !catalog.IsAvailableForOrder(dcStatus) {
					continue
				}
				dcSeen = true
				if len(cfg.Options) > 0 {
					options = append(options, cfg.Options...)
					break
				}
				// 这条 FQN 在用户要的机房确实有货,却一个可下单 addon 都没匹配上 ——
				// OptionsNote 说的就是这件事,照实转达比报"无可定价配置"准确得多
				if cfg.OptionsNote != "" && optionsNote == "" {
					optionsNote = cfg.OptionsNote
				}
			}
			if len(options) == 0 {
				// 目录没拉下来时 cfg.Options 恒为空,报"无可定价配置"会把 OVH 瞬断
				// 误导成"这台机器没配置可买",几种情况必须分开说
				err := "指定机房无可定价配置（" + body.PlanCode + "@" + body.Datacenter + "）"
				switch {
				case catalogMissingMsg != "":
					// 原文已经是完整的人话(哪个子公司、哪个站点、为什么),不要再加前缀
					err = catalogMissingMsg
				case catalogErr != "":
					err = "OVH 目录拉取失败，无法匹配可下单配置：" + catalogErr
				case optionsNote != "":
					err = optionsNote
				case len(availByConfig) == 0:
					// availByConfig 为空 = /dedicated/server/datacenter/availabilities 返回了空数组。
					// 拿别的站点的 planCode 查同样是 HTTP 200 + [](实测 24rise01-v1 在 US 站点 n=0),
					// 和"这台机器暂时没货"长得一样 —— 交给 classifyPlan 分辨到底是哪种。
					if _, hint := catalog.ClassifyPlan(state, body.AccountID, body.PlanCode, "quick_order"); hint != "" {
						err = hint
					}
				case !dcSeen:
					// 机型有记录,但用户要的机房这一刻没有任何一条 FQN 可下单 = 单纯没货
					err = "机型 " + body.PlanCode + " 在机房 " + body.Datacenter + " 当前无货"
				}
				state.Logger.Warn("[quick_order] "+err, "quick_order")
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err})
				return
			}
		}

		priceResult := price.GetInternal(state, body.AccountID, body.PlanCode, body.Datacenter, options)
		if !priceResult.Success {
			err := priceResult.Error
			if err == "" {
				err = "价格查询失败"
			}
			state.Logger.Warn("快速下单前价格校验失败: "+body.PlanCode+"@"+body.Datacenter+" - "+err, "quick_order")
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "价格校验失败：" + err})
			return
		}
		if priceResult.Degraded {
			// 询价时有必填配置没设上,同样的配置在 purchase.go 是 fail-fast,
			// 放进队列只会浪费一次抢购尝试
			state.Logger.Warn("快速下单前价格校验降级，拒绝入队: "+priceResult.DegradedReason, "quick_order")
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "购物车配置不完整，暂不支持下单：" + priceResult.DegradedReason})
			return
		}
		if priceResult.Price == nil {
			state.Logger.Warn("快速下单前价格校验失败: price字段缺失", "quick_order")
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "价格查询返回数据格式异常：缺少price字段"})
			return
		}
		withTaxRaw, _ := priceResult.Price.Prices["withTax"]
		if withTaxRaw == nil {
			state.Logger.Warn("快速下单前价格缺失或无效: "+body.PlanCode+"@"+body.Datacenter, "quick_order")
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "该组合暂无有效价格，暂不支持下单"})
			return
		}
		if f, ok := numconv.ToFloat64(withTaxRaw); ok && f == 0 {
			state.Logger.Warn("快速下单前价格缺失或无效: "+body.PlanCode+"@"+body.Datacenter, "quick_order")
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "该组合暂无有效价格，暂不支持下单"})
			return
		}

		// 去重:防止同一 plan@dc + 同 options 的任务被重复入队(除非监控来源 + 显式跳过)
		quickOrderMu.Lock()
		defer quickOrderMu.Unlock()
		if !(body.FromMonitor && body.SkipDuplicateCheck) {
			fp := fingerprint(options)
			state.QueueMu.Lock()
			for _, it := range state.Queue {
				if it.PlanCode == body.PlanCode && it.Datacenter == body.Datacenter &&
					(it.Status == "running" || it.Status == "pending" || it.Status == "paused") &&
					fingerprint(it.Options) == fp {
					state.QueueMu.Unlock()
					state.Logger.Info("检测到重复的队列任务（含配置），拒绝再次入队", "quick_order")
					c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "error": "已存在相同配置的购买任务，稍后再试"})
					return
				}
			}
			state.QueueMu.Unlock()

			nowTS := time.Now().Unix()
			state.HistoryMu.Lock()
			for i := len(state.History) - 1; i >= 0; i-- {
				h := state.History[i]
				if h.PlanCode == body.PlanCode && h.Datacenter == body.Datacenter && h.Status == "success" &&
					fingerprint(h.Options) == fp {
					if t, err := time.Parse(time.RFC3339Nano, h.PurchaseTime); err == nil {
						if nowTS-t.Unix() < 120 {
							state.HistoryMu.Unlock()
							state.Logger.Info("检测到近期成功订单，拒绝再次入队", "quick_order")
							c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "error": "刚刚已成功下过同配置订单，稍后再试"})
							return
						}
					}
				}
			}
			state.HistoryMu.Unlock()
		} else {
			state.Logger.Info("来自监控的批量下单，跳过重复检查", "quick_order")
		}

		now := types.NowISO()
		item := types.QueueItem{
			ID:         uuid.NewString(),
			AccountID:  body.AccountID,
			PlanCode:   body.PlanCode,
			Datacenter: body.Datacenter,
			Options:    options,
			Status:     "running",
			RetryCount: 0,
			// MaxRetries 封顶的是**真正提交并失败的次数**(FailureCount),无货轮次不计。
			// 早先按"检查轮次"封顶是错的:监控只在 无货→有货 的跳变上重新触发 quick-order,
			// 一旦库存持续可见而下单侧连续吃 429/5xx,任务用尽轮次置 failed 后
			// 就再也没人补这一枪 —— 自动下单会静默停摆到库存先消失再回来。
			// 20 次真实失败已经足够说明不是偶发抖动;确定性错误另有 Fatal 闸门当场终止。
			MaxRetries:    20,
			RetryInterval: 2,
			CreatedAt:     now,
			UpdatedAt:     now,
			LastCheckTime: 0,
			QuickOrder:    true,
			Priority:      100,
		}
		state.QueueMu.Lock()
		state.Queue = append([]types.QueueItem{item}, state.Queue...)
		state.QueueMu.Unlock()
		_ = state.SaveQueue()

		state.Logger.Info("快速下单: "+body.PlanCode+" ("+body.Datacenter+") 已加入队列", "quick_order")

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "✅ " + body.PlanCode + " (" + body.Datacenter + ") 已加入购买队列",
			"price":   priceResult.Price,
			"options": options,
		})
	}
}

// fingerprint 排序后用 "|" 连接的 options 指纹,用于队列去重
func fingerprint(opts []string) string {
	if len(opts) == 0 {
		return ""
	}
	uniq := map[string]struct{}{}
	for _, o := range opts {
		s := strings.TrimSpace(o)
		if s != "" {
			uniq[s] = struct{}{}
		}
	}
	list := make([]string, 0, len(uniq))
	for s := range uniq {
		list = append(list, s)
	}
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j-1] > list[j]; j-- {
			list[j-1], list[j] = list[j], list[j-1]
		}
	}
	return strings.Join(list, "|")
}
