package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/numconv"
	"github.com/ovh-buy/server/internal/price"
	"github.com/ovh-buy/server/internal/sniper"
	"github.com/ovh-buy/server/internal/types"
)

// GetConfigOptions GET /api/config-sniper/options/:planCode
func GetConfigOptions(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		planCode := c.Param("planCode")
		client, err := state.OVH.Client()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "OVH客户端未配置"})
			return
		}
		var availabilities []map[string]interface{}
		q := url.Values{}
		q.Set("planCode", planCode)
		if err := client.Get("/dedicated/server/datacenter/availabilities?"+q.Encode(), &availabilities); err != nil {
			state.Logger.Error("获取配置选项错误: "+err.Error(), "")
			c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
			return
		}
		if len(availabilities) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"error":   "型号 " + planCode + " 不存在或API1中无数据",
			})
			return
		}

		configs := []gin.H{}
		seen := map[string]struct{}{}
		for _, item := range availabilities {
			memory, _ := item["memory"].(string)
			storageCfg, _ := item["storage"].(string)
			key := memory + "|" + storageCfg
			if memory == "" || storageCfg == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			memoryStd := catalog.StandardizeConfig(memory)
			storageStd := catalog.StandardizeConfig(storageCfg)
			state.Logger.Debug("API1 配置: memory="+memory+", storage="+storageCfg, "config_sniper")
			state.Logger.Debug("标准化后: memory="+memoryStd+", storage="+storageStd, "config_sniper")
			matched := sniper.FindMatchingAPI2Plans(state, memoryStd, storageStd, planCode)

			// 并发拉每个 matched API2 planCode 的可用性
			availResults := make([][]map[string]interface{}, len(matched))
			aSem := make(chan struct{}, 10)
			var aWg sync.WaitGroup
			for i, pc := range matched {
				aWg.Add(1)
				aSem <- struct{}{}
				go func(idx int, planCode string) {
					defer aWg.Done()
					defer func() { <-aSem }()
					q2 := url.Values{}
					q2.Set("planCode", planCode)
					var av []map[string]interface{}
					if err := client.Get("/dedicated/server/datacenter/availabilities?"+q2.Encode(), &av); err == nil {
						availResults[idx] = av
					}
				}(i, pc)
			}
			aWg.Wait()

			plancodesWithDCs := []gin.H{}
			for i, api2PC := range matched {
				api2Avail := availResults[i]
				if api2Avail == nil {
					continue
				}
				dcs := map[string]struct{}{}
				for _, it := range api2Avail {
					if dcsRaw, ok := it["datacenters"].([]interface{}); ok {
						for _, dcRaw := range dcsRaw {
							if dcMap, ok := dcRaw.(map[string]interface{}); ok {
								if d, ok := dcMap["datacenter"].(string); ok && d != "" {
									dcs[d] = struct{}{}
								}
							}
						}
					}
				}
				if len(dcs) > 0 {
					list := make([]string, 0, len(dcs))
					for k := range dcs {
						list = append(list, k)
					}
					plancodesWithDCs = append(plancodesWithDCs, gin.H{
						"planCode":    api2PC,
						"datacenters": list,
					})
				}
			}

			configs = append(configs, gin.H{
				"memory": gin.H{
					"code":    memory,
					"display": catalog.FormatMemoryDisplay(memory),
				},
				"storage": gin.H{
					"code":    storageCfg,
					"display": catalog.FormatStorageDisplay(storageCfg),
				},
				"matched_api2": plancodesWithDCs,
				"match_count":  len(plancodesWithDCs),
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"planCode": planCode,
			"configs":  configs,
			"total":    len(configs),
		})
	}
}

// GetConfigSniperTasks GET /api/config-sniper/tasks
func GetConfigSniperTasks(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		state.ConfigSniperMu.Lock()
		defer state.ConfigSniperMu.Unlock()
		tasks := state.ConfigSniperTasks
		if tasks == nil {
			tasks = []types.ConfigSniperTask{}
		}
		// 保护内部 slice 字段
		for i := range tasks {
			if tasks[i].MatchedAPI2 == nil {
				tasks[i].MatchedAPI2 = []string{}
			}
			if tasks[i].KnownPlanCodes == nil {
				tasks[i].KnownPlanCodes = []string{}
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"tasks":   tasks,
			"total":   len(tasks),
		})
	}
}

// CreateConfigSniperTask POST /api/config-sniper/tasks
func CreateConfigSniperTask(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			API1PlanCode string                 `json:"api1_planCode"`
			BoundConfig  map[string]interface{} `json:"bound_config"`
			Mode         string                 `json:"mode"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.API1PlanCode == "" || body.BoundConfig == nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "缺少必要参数"})
			return
		}
		if body.Mode == "" {
			body.Mode = "matched"
		}
		memory, _ := body.BoundConfig["memory"].(string)
		storageCfg, _ := body.BoundConfig["storage"].(string)
		memoryStd := catalog.StandardizeConfig(memory)
		storageStd := catalog.StandardizeConfig(storageCfg)
		currentMatched := sniper.FindMatchingAPI2Plans(state, memoryStd, storageStd, body.API1PlanCode)

		task := types.ConfigSniperTask{
			ID:           uuid.NewString(),
			API1PlanCode: body.API1PlanCode,
			BoundConfig:  body.BoundConfig,
			Enabled:      true,
			CreatedAt:    time.Now().Format(time.RFC3339Nano),
		}
		message := ""
		if body.Mode == "pending_match" {
			task.MatchStatus = "pending_match"
			task.MatchedAPI2 = []string{}
			task.KnownPlanCodes = currentMatched
			message = "⏳ 已创建待匹配任务（已排除 " + strconv.Itoa(len(currentMatched)) + " 个已知型号，等待新增型号）"
		} else {
			if len(currentMatched) > 0 {
				task.MatchStatus = "matched"
				task.MatchedAPI2 = currentMatched
				message = "✅ 已创建监控任务（监控 " + strconv.Itoa(len(currentMatched)) + " 个型号）"
			} else {
				task.MatchStatus = "pending_match"
				task.MatchedAPI2 = []string{}
				message = "⏳ 未找到匹配，已创建待匹配任务"
			}
			task.KnownPlanCodes = []string{}
		}

		state.ConfigSniperMu.Lock()
		state.ConfigSniperTasks = append(state.ConfigSniperTasks, task)
		cp := make([]types.ConfigSniperTask, len(state.ConfigSniperTasks))
		copy(cp, state.ConfigSniperTasks)
		state.ConfigSniperMu.Unlock()
		_ = state.DB.ReplaceSniperTasks(cp)
		state.Logger.Info("创建配置绑定任务: "+body.API1PlanCode+" - "+message, "config_sniper")

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"task":    task,
			"message": message,
		})
	}
}

// DeleteConfigSniperTask DELETE /api/config-sniper/tasks/:task_id
func DeleteConfigSniperTask(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		taskID := c.Param("task_id")
		state.ConfigSniperMu.Lock()
		idx := -1
		for i, t := range state.ConfigSniperTasks {
			if t.ID == taskID {
				idx = i
				break
			}
		}
		if idx == -1 {
			state.ConfigSniperMu.Unlock()
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "任务不存在"})
			return
		}
		api1 := state.ConfigSniperTasks[idx].API1PlanCode
		state.ConfigSniperTasks = append(state.ConfigSniperTasks[:idx], state.ConfigSniperTasks[idx+1:]...)
		cp := make([]types.ConfigSniperTask, len(state.ConfigSniperTasks))
		copy(cp, state.ConfigSniperTasks)
		state.ConfigSniperMu.Unlock()
		_ = state.DB.ReplaceSniperTasks(cp)
		state.Logger.Info("删除配置绑定任务: "+api1, "config_sniper")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "任务已删除"})
	}
}

// ToggleConfigSniperTask PUT /api/config-sniper/tasks/:task_id/toggle
func ToggleConfigSniperTask(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		taskID := c.Param("task_id")
		state.ConfigSniperMu.Lock()
		var found *types.ConfigSniperTask
		for i := range state.ConfigSniperTasks {
			if state.ConfigSniperTasks[i].ID == taskID {
				found = &state.ConfigSniperTasks[i]
				break
			}
		}
		if found == nil {
			state.ConfigSniperMu.Unlock()
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "任务不存在"})
			return
		}
		found.Enabled = !found.Enabled
		status := "启用"
		if !found.Enabled {
			status = "禁用"
		}
		cp := make([]types.ConfigSniperTask, len(state.ConfigSniperTasks))
		copy(cp, state.ConfigSniperTasks)
		state.ConfigSniperMu.Unlock()
		_ = state.DB.ReplaceSniperTasks(cp)
		state.Logger.Info(status+"配置绑定任务: "+found.API1PlanCode, "config_sniper")
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"enabled": found.Enabled,
			"message": "任务已" + status,
		})
	}
}

var quickOrderMu sync.Mutex

// QuickOrder POST /api/config-sniper/quick-order
func QuickOrder(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
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
		options := body.Options
		if len(options) == 0 {
			availByConfig := catalog.CheckServerAvailabilityWithConfigs(state, body.PlanCode)
			for _, cfg := range availByConfig {
				if dcStatus, ok := cfg.Datacenters[body.Datacenter]; ok &&
					dcStatus != "unavailable" && dcStatus != "unknown" && len(cfg.Options) > 0 {
					options = append(options, cfg.Options...)
					break
				}
			}
			if len(options) == 0 {
				err := "指定机房无可定价配置（" + body.PlanCode + "@" + body.Datacenter + "）"
				state.Logger.Warn("[config_sniper] "+err, "config_sniper")
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err})
				return
			}
		}

		priceResult := price.GetInternal(state, body.PlanCode, body.Datacenter, options)
		if !priceResult.Success {
			err := priceResult.Error
			if err == "" {
				err = "价格查询失败"
			}
			state.Logger.Warn("快速下单前价格校验失败: "+body.PlanCode+"@"+body.Datacenter+" - "+err, "config_sniper")
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "价格校验失败：" + err})
			return
		}
		if priceResult.Price == nil {
			state.Logger.Warn("快速下单前价格校验失败: price字段缺失", "config_sniper")
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "价格查询返回数据格式异常：缺少price字段"})
			return
		}
		withTaxRaw, _ := priceResult.Price.Prices["withTax"]
		if withTaxRaw == nil {
			state.Logger.Warn("快速下单前价格缺失或无效: "+body.PlanCode+"@"+body.Datacenter, "config_sniper")
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "该组合暂无有效价格，暂不支持下单"})
			return
		}
		if f, ok := numconv.ToFloat64(withTaxRaw); ok && f == 0 {
			state.Logger.Warn("快速下单前价格缺失或无效: "+body.PlanCode+"@"+body.Datacenter, "config_sniper")
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "该组合暂无有效价格，暂不支持下单"})
			return
		}

		// 去重（除非 fromMonitor + skipDuplicateCheck）
		if !(body.FromMonitor && body.SkipDuplicateCheck) {
			fp := fingerprint(options)
			state.QueueMu.Lock()
			for _, it := range state.Queue {
				if it.PlanCode == body.PlanCode && it.Datacenter == body.Datacenter &&
					(it.Status == "running" || it.Status == "pending" || it.Status == "paused") &&
					fingerprint(it.Options) == fp {
					state.QueueMu.Unlock()
					state.Logger.Info("检测到重复的队列任务（含配置），拒绝再次入队", "config_sniper")
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
							state.Logger.Info("检测到近期成功订单，拒绝再次入队", "config_sniper")
							c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "error": "刚刚已成功下过同配置订单，稍后再试"})
							return
						}
					}
				}
			}
			state.HistoryMu.Unlock()
		} else {
			state.Logger.Info("来自监控的批量下单，跳过重复检查", "config_sniper")
		}

		now := types.NowISO()
		item := types.QueueItem{
			ID:            uuid.NewString(),
			PlanCode:      body.PlanCode,
			Datacenter:    body.Datacenter,
			Options:       options,
			Status:        "running",
			RetryCount:    0,
			MaxRetries:    3,
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

		state.Logger.Info("快速下单: "+body.PlanCode+" ("+body.Datacenter+") 已加入队列", "config_sniper")

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "✅ " + body.PlanCode + " (" + body.Datacenter + ") 已加入购买队列",
			"price":   priceResult.Price,
			"options": options,
		})
	}
}

// CheckConfigSniperTask POST /api/config-sniper/tasks/:task_id/check
func CheckConfigSniperTask(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		taskID := c.Param("task_id")
		state.ConfigSniperMu.Lock()
		var task *types.ConfigSniperTask
		for i := range state.ConfigSniperTasks {
			if state.ConfigSniperTasks[i].ID == taskID {
				task = &state.ConfigSniperTasks[i]
				break
			}
		}
		state.ConfigSniperMu.Unlock()
		if task == nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "任务不存在"})
			return
		}
		switch task.MatchStatus {
		case "pending_match":
			sniper.HandlePendingMatchTask(state, task)
		case "matched":
			sniper.HandleMatchedTask(state, task)
		case "completed":
			c.JSON(http.StatusOK, gin.H{"success": true, "message": "任务已完成，无需检查"})
			return
		}
		now := time.Now().Format(time.RFC3339Nano)
		task.LastCheck = &now
		state.ConfigSniperMu.Lock()
		cp := make([]types.ConfigSniperTask, len(state.ConfigSniperTasks))
		copy(cp, state.ConfigSniperTasks)
		state.ConfigSniperMu.Unlock()
		_ = state.DB.ReplaceSniperTasks(cp)
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "检查完成",
			"task":    task,
		})
	}
}

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
	// sort
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j-1] > list[j]; j-- {
			list[j-1], list[j] = list[j], list[j-1]
		}
	}
	return strings.Join(list, "|")
}
