package sniper

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// FindMatchingAPI2Plans 对应 Python: find_matching_api2_plans
func FindMatchingAPI2Plans(state *app.State, memoryStd, storageStd, targetPlanCode string) []string {
	client, err := state.OVH.Client()
	if err != nil {
		return []string{}
	}
	cfg := state.Config.Get()
	var catalogResp map[string]interface{}
	if err := client.Get("/order/catalog/public/eco?ovhSubsidiary="+cfg.Zone, &catalogResp); err != nil {
		state.Logger.Error("查找匹配 API2 planCode 时出错: "+err.Error(), "")
		return []string{}
	}

	matched := []string{}
	state.Logger.Info("🔍 配置匹配模式：查找所有相同配置的型号", "config_sniper")

	plans, _ := catalogResp["plans"].([]interface{})
	for _, planRaw := range plans {
		plan, ok := planRaw.(map[string]interface{})
		if !ok {
			continue
		}
		planCode, _ := plan["planCode"].(string)
		af, _ := plan["addonFamilies"].([]interface{})

		memoryOpts := []string{}
		storageOpts := []string{}
		for _, fRaw := range af {
			family, ok := fRaw.(map[string]interface{})
			if !ok {
				continue
			}
			familyName := strings.ToLower(getString(family, "name", ""))
			addons, _ := family["addons"].([]interface{})
			if familyName == "memory" {
				for _, addonRaw := range addons {
					addon, ok := addonRaw.(string)
					if !ok {
						continue
					}
					if catalog.StandardizeConfig(addon) == memoryStd {
						memoryOpts = append(memoryOpts, addon)
					}
				}
			} else if familyName == "storage" {
				for _, addonRaw := range addons {
					addon, ok := addonRaw.(string)
					if !ok {
						continue
					}
					if catalog.StandardizeConfig(addon) == storageStd {
						storageOpts = append(storageOpts, addon)
					}
				}
			}
		}
		if len(memoryOpts) > 0 && len(storageOpts) > 0 {
			for _, memConfig := range memoryOpts {
				for _, storConfig := range storageOpts {
					memStd := catalog.StandardizeConfig(memConfig)
					storStd := catalog.StandardizeConfig(storConfig)
					state.Logger.Debug(fmt.Sprintf("API2 扫描: %s, memory=%s, storage=%s", planCode, memStd, storStd), "config_sniper")
					if strings.Contains(memStd, "64g") {
						state.Logger.Info(fmt.Sprintf("🔍 发现 64GB 配置: %s | %s → %s | %s → %s",
							planCode, memConfig, memStd, storConfig, storStd), "config_sniper")
					}
					if memStd == memoryStd && storStd == storageStd {
						if !contains(matched, planCode) {
							matched = append(matched, planCode)
							state.Logger.Info("✓ API2 配置匹配: "+planCode, "config_sniper")
						}
					}
				}
			}
		}
	}
	state.Logger.Info(fmt.Sprintf("配置匹配完成，找到 %d 个 API2 planCode", len(matched)), "config_sniper")
	return matched
}

// CheckAndQueuePlanCode 对应 Python: check_and_queue_plancode
func CheckAndQueuePlanCode(state *app.State, api2PlanCode string, task *types.ConfigSniperTask, boundConfig map[string]interface{}) bool {
	client, err := state.OVH.Client()
	if err != nil {
		return false
	}
	queuedCount := 0
	cfg := state.Config.Get()

	var availabilities []map[string]interface{}
	q := url.Values{}
	q.Set("planCode", api2PlanCode)
	if err := client.Get("/dedicated/server/datacenter/availabilities?"+q.Encode(), &availabilities); err != nil {
		state.Logger.Warn(fmt.Sprintf("查询 %s 可用性失败: %s", api2PlanCode, err.Error()), "config_sniper")
		return false
	}

	bMem, _ := boundConfig["memory"].(string)
	bStor, _ := boundConfig["storage"].(string)

	for _, item := range availabilities {
		itemMemory, _ := item["memory"].(string)
		itemStorage, _ := item["storage"].(string)
		itemFQN, _ := item["fqn"].(string)
		if !catalog.MatchConfig(bMem, bStor, itemMemory, itemStorage) {
			continue
		}
		dcsRaw, _ := item["datacenters"].([]interface{})
		for _, dcRaw := range dcsRaw {
			dc, ok := dcRaw.(map[string]interface{})
			if !ok {
				continue
			}
			availability, _ := dc["availability"].(string)
			datacenter, _ := dc["datacenter"].(string)
			if availability == "unavailable" || availability == "unknown" {
				continue
			}
			state.Logger.Info(fmt.Sprintf("🎯 发现可用！API2=%s 配置=%s 机房=%s 状态=%s",
				api2PlanCode, itemFQN, datacenter, availability), "config_sniper")

			telegram.SendMessage(state, fmt.Sprintf(
				"📦 配置有货通知！\n源型号: %s\n绑定配置: %s\n匹配型号: %s\n实际配置: %s\n机房: %s\n库存状态: %s",
				task.API1PlanCode,
				catalog.FormatConfigDisplay(bMem, bStor),
				api2PlanCode,
				catalog.FormatConfigDisplay(itemMemory, itemStorage),
				datacenter, availability,
			), nil)

			// 检查队列重复
			duplicate := false
			state.QueueMu.Lock()
			for _, qit := range state.Queue {
				if qit.PlanCode == api2PlanCode && qit.Datacenter == datacenter && qit.ConfigSniperTaskID == task.ID {
					duplicate = true
					break
				}
			}
			state.QueueMu.Unlock()
			if duplicate {
				state.Logger.Debug(api2PlanCode+" ("+datacenter+") 已在队列中，跳过", "config_sniper")
				continue
			}

			// 找匹配 addons
			hardwareOptions := []string{}
			var catalogResp map[string]interface{}
			if err := client.Get("/order/catalog/public/eco?ovhSubsidiary="+cfg.Zone, &catalogResp); err == nil {
				if plans, ok := catalogResp["plans"].([]interface{}); ok {
					for _, planRaw := range plans {
						plan, ok := planRaw.(map[string]interface{})
						if !ok {
							continue
						}
						if pc, _ := plan["planCode"].(string); pc == api2PlanCode {
							af, _ := plan["addonFamilies"].([]interface{})
							for _, fRaw := range af {
								family, ok := fRaw.(map[string]interface{})
								if !ok {
									continue
								}
								familyName := strings.ToLower(getString(family, "name", ""))
								addons, _ := family["addons"].([]interface{})
								if familyName == "memory" {
									target := catalog.StandardizeConfig(bMem)
									for _, ar := range addons {
										if a, ok := ar.(string); ok && catalog.StandardizeConfig(a) == target {
											hardwareOptions = append(hardwareOptions, a)
											state.Logger.Debug("添加 memory 选项: "+a, "config_sniper")
											break
										}
									}
								} else if familyName == "storage" {
									target := catalog.StandardizeConfig(bStor)
									for _, ar := range addons {
										if a, ok := ar.(string); ok && catalog.StandardizeConfig(a) == target {
											hardwareOptions = append(hardwareOptions, a)
											state.Logger.Debug("添加 storage 选项: "+a, "config_sniper")
											break
										}
									}
								}
							}
							break
						}
					}
				}
			}

			now := types.NowISO()
			qitem := types.QueueItem{
				ID:                 uuid.NewString(),
				PlanCode:           api2PlanCode,
				Datacenter:         datacenter,
				Options:            hardwareOptions,
				Status:             "running",
				RetryCount:         0,
				MaxRetries:         3,
				RetryInterval:      30,
				CreatedAt:          now,
				UpdatedAt:          now,
				LastCheckTime:      0,
				ConfigSniperTaskID: task.ID,
			}
			state.QueueMu.Lock()
			state.Queue = append(state.Queue, qitem)
			state.QueueMu.Unlock()
			_ = state.SaveQueue()
			queuedCount++
			state.Logger.Info(fmt.Sprintf("🚀 已添加 %s (%s) 到购买队列", api2PlanCode, datacenter), "config_sniper")

			telegram.SendMessage(state, fmt.Sprintf(
				"🎯 自动下单触发！\n源型号: %s\n绑定配置: %s\n下单型号: %s\n实际配置: %s\n机房: %s\n库存状态: %s\n✅ 已加入购买队列",
				task.API1PlanCode,
				catalog.FormatConfigDisplay(bMem, bStor),
				api2PlanCode,
				catalog.FormatConfigDisplay(itemMemory, itemStorage),
				datacenter, availability,
			), nil)
		}
	}
	return queuedCount > 0
}

// HandlePendingMatchTask 对应 Python: handle_pending_match_task
func HandlePendingMatchTask(state *app.State, task *types.ConfigSniperTask) {
	boundCfg := task.BoundConfig
	memory, _ := boundCfg["memory"].(string)
	storage, _ := boundCfg["storage"].(string)
	memoryStd := catalog.StandardizeConfig(memory)
	storageStd := catalog.StandardizeConfig(storage)

	current := FindMatchingAPI2Plans(state, memoryStd, storageStd, task.API1PlanCode)

	allKnown := map[string]struct{}{}
	for _, pc := range task.KnownPlanCodes {
		allKnown[pc] = struct{}{}
	}
	for _, pc := range task.MatchedAPI2 {
		allKnown[pc] = struct{}{}
	}
	newPlanCodes := []string{}
	for _, pc := range current {
		if _, ok := allKnown[pc]; !ok {
			newPlanCodes = append(newPlanCodes, pc)
		}
	}

	if len(newPlanCodes) > 0 {
		task.MatchedAPI2 = append(task.MatchedAPI2, newPlanCodes...)
		state.Logger.Info(fmt.Sprintf("✅ 发现新增 planCode！%s 新增 %d 个：%s",
			task.API1PlanCode, len(newPlanCodes), strings.Join(newPlanCodes, ", ")), "config_sniper")

		telegram.SendMessage(state, fmt.Sprintf(
			"🆕 发现新增配置！\n源型号: %s\n绑定配置: %s\n新增型号: %s\n总计匹配: %d 个",
			task.API1PlanCode,
			catalog.FormatConfigDisplay(memory, storage),
			strings.Join(newPlanCodes, ", "),
			len(task.MatchedAPI2),
		), nil)
		_ = saveTasks(state)

		hasQueued := false
		if state.Config.HasCredentials() {
			for _, newPC := range newPlanCodes {
				if CheckAndQueuePlanCode(state, newPC, task, boundCfg) {
					hasQueued = true
				}
			}
		}
		if hasQueued {
			task.MatchStatus = "completed"
			_ = saveTasks(state)
			state.Logger.Info("✅ 未匹配任务完成！"+task.API1PlanCode+" 发现新增并已下单，任务结束", "config_sniper")
			telegram.SendMessage(state, fmt.Sprintf(
				"🎉 待匹配任务完成！\n源型号: %s\n绑定配置: %s\n新增型号: %s\n✅ 已下单所有机房，任务完成",
				task.API1PlanCode,
				catalog.FormatConfigDisplay(memory, storage),
				strings.Join(newPlanCodes, ", "),
			), nil)
		}
	} else {
		state.Logger.Debug("待匹配任务 "+task.API1PlanCode+" 暂无新增", "config_sniper")
	}
}

// HandleMatchedTask 对应 Python: handle_matched_task
func HandleMatchedTask(state *app.State, task *types.ConfigSniperTask) {
	boundCfg := task.BoundConfig
	// 1:1 对应 Python app.py:4631-4633：尝试创建 ovh client，凭据格式错误也会失败
	if _, err := state.OVH.Client(); err != nil {
		return
	}
	hasQueued := false
	for _, api2PC := range task.MatchedAPI2 {
		if CheckAndQueuePlanCode(state, api2PC, task, boundCfg) {
			hasQueued = true
		}
	}
	if hasQueued {
		task.MatchStatus = "completed"
		_ = saveTasks(state)
		state.Logger.Info("✅ 任务完成！"+task.API1PlanCode+" 已加入购买队列，停止监控", "config_sniper")
		memory, _ := boundCfg["memory"].(string)
		storage, _ := boundCfg["storage"].(string)
		telegram.SendMessage(state, fmt.Sprintf(
			"🎉 配置狙击任务完成！\n源型号: %s\n绑定配置: %s\n✅ 已加入购买队列，任务自动完成",
			task.API1PlanCode,
			catalog.FormatConfigDisplay(memory, storage),
		), nil)
	}
}

var running bool
var runningMu sync.Mutex

// MonitorLoop 对应 Python: config_sniper_monitor_loop
func MonitorLoop(state *app.State) {
	runningMu.Lock()
	if running {
		runningMu.Unlock()
		state.Logger.Warn("配置绑定狙击监控已在运行，跳过重复启动", "config_sniper")
		return
	}
	running = true
	runningMu.Unlock()

	state.Logger.Info("配置绑定狙击监控已启动（60秒轮询）", "config_sniper")
	for {
		runningMu.Lock()
		isRunning := running
		runningMu.Unlock()
		if !isRunning {
			break
		}
		state.ConfigSniperMu.Lock()
		snapshot := make([]types.ConfigSniperTask, len(state.ConfigSniperTasks))
		copy(snapshot, state.ConfigSniperTasks)
		state.ConfigSniperMu.Unlock()

		state.Logger.Debug(fmt.Sprintf("监控循环: 任务数=%d", len(snapshot)), "config_sniper")

		for i := range snapshot {
			task := &snapshot[i]
			state.ConfigSniperMu.Lock()
			stillExists := false
			for j := range state.ConfigSniperTasks {
				if state.ConfigSniperTasks[j].ID == task.ID {
					stillExists = true
					// 引用最新的指针进行修改
					task = &state.ConfigSniperTasks[j]
					break
				}
			}
			state.ConfigSniperMu.Unlock()
			if !stillExists {
				continue
			}
			if !task.Enabled {
				continue
			}
			switch task.MatchStatus {
			case "pending_match":
				HandlePendingMatchTask(state, task)
			case "matched":
				HandleMatchedTask(state, task)
			case "completed":
				continue
			}
			now := time.Now().Format(time.RFC3339Nano)
			task.LastCheck = &now
		}

		state.ConfigSniperMu.Lock()
		empty := len(state.ConfigSniperTasks) == 0
		state.ConfigSniperMu.Unlock()
		if !empty {
			_ = saveTasks(state)
		} else {
			state.Logger.Warn("监控循环跳过保存：任务列表为空", "config_sniper")
		}
		time.Sleep(60 * time.Second)
	}
}

func saveTasks(state *app.State) error {
	state.ConfigSniperMu.Lock()
	cp := make([]types.ConfigSniperTask, len(state.ConfigSniperTasks))
	copy(cp, state.ConfigSniperTasks)
	state.ConfigSniperMu.Unlock()
	if err := state.DB.ReplaceSniperTasks(cp); err != nil {
		state.Logger.Error("保存配置狙击任务时出错: "+err.Error(), "")
		return err
	}
	state.Logger.Info(fmt.Sprintf("已保存 %d 个配置绑定狙击任务", len(cp)), "")
	return nil
}

func getString(m map[string]interface{}, key, fallback string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return fallback
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
