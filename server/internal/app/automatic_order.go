package app

import (
	"encoding/json"
	"fmt"

	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

// automaticOrderKey 使用稳定账户及 API 机房名,让停止记录跨重启生效。
func (s *State) automaticOrderKey(item types.QueueItem) string {
	account := item.AccountID
	if acc, ok := s.FindAccount(account); ok {
		account = acc.ID
	}
	key, _ := json.Marshal([]string{account, item.PlanCode, ovh.ConvertDisplayDCToAPIDC(item.Datacenter)})
	return "automatic-order-block:" + string(key)
}

// SetAutomaticOrderBlockedLocked 由持有 QueueMu 的暂停/删除/恢复入口调用。
func (s *State) SetAutomaticOrderBlockedLocked(item types.QueueItem, blocked bool) error {
	key := s.automaticOrderKey(item)
	if s.automaticOrderBlocks == nil {
		s.automaticOrderBlocks = map[string]bool{}
	}
	// 写库失败时本轮仍禁止自动建单,不会把停止操作悄悄变成继续下单。
	if blocked {
		s.automaticOrderBlocks[key] = true
	}
	if s.DB == nil {
		return fmt.Errorf("数据库不可用,不能保存自动下单停止状态")
	}
	if err := s.DB.SetKV(key, blocked); err != nil {
		return err
	}
	s.automaticOrderBlocks[key] = blocked
	return nil
}

// checkAutomaticOrderLocked 将持久停止记录和旧版本暂停任务作为入队硬闸门。
func (s *State) checkAutomaticOrderLocked(item types.QueueItem) error {
	key := s.automaticOrderKey(item)
	blocked := s.automaticOrderBlocks[key]
	if s.DB == nil {
		return fmt.Errorf("数据库不可用,禁止自动下单")
	}
	var stopped bool
	if _, err := s.DB.GetKV("monitor-manually-stopped", &stopped); err != nil {
		return err
	}
	if stopped {
		return fmt.Errorf("服务器监控已停止,禁止自动建单")
	}
	var persisted bool
	if _, err := s.DB.GetKV(key, &persisted); err != nil {
		return err
	}
	if blocked || persisted {
		return fmt.Errorf("该账户/型号/机房已停止抢购,请手动恢复或新建任务后再自动下单")
	}
	for _, existing := range s.Queue {
		if existing.Status == "paused" && s.automaticOrderKey(existing) == key {
			return fmt.Errorf("该型号有已暂停的抢购任务,禁止监控重新建单")
		}
	}
	return nil
}
