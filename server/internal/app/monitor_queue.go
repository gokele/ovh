package app

import (
	"fmt"
	"strings"

	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

// MonitorQueueScope 描述一个订阅关联的账户、型号、机房和选项范围。
// 未绑定账户/机房/选项表示该维度不限,与监控订阅本身的含义一致。
type MonitorQueueScope struct {
	PlanCode    string
	AccountID   string
	Datacenters []string
	Options     []string
}

// matchesMonitorScope 对已入队任务保留完整配置匹配,兼容旧版本缺失网络选项的任务。
func (s *State) matchesMonitorScope(item types.QueueItem, scope MonitorQueueScope) bool {
	if item.PlanCode != scope.PlanCode {
		return false
	}
	if scope.AccountID != "" {
		accountID := item.AccountID
		if acc, ok := s.FindAccount(accountID); ok {
			accountID = acc.ID
		}
		if accountID != scope.AccountID {
			return false
		}
	}
	if len(scope.Datacenters) > 0 {
		found := false
		for _, dc := range scope.Datacenters {
			if ovh.ConvertDisplayDCToAPIDC(dc) == ovh.ConvertDisplayDCToAPIDC(item.Datacenter) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	have := map[string]bool{}
	for _, option := range item.Options {
		have[option] = true
	}
	for _, wanted := range scope.Options {
		if have[wanted] {
			continue
		}
		prefix := ""
		if strings.HasPrefix(wanted, "bandwidth-") {
			prefix = "bandwidth-"
		}
		if strings.HasPrefix(wanted, "vrack-") {
			prefix = "vrack-"
		}
		if prefix == "" {
			return false
		}
		// 旧监控任务只有硬件选项;已明确选择另一网络规格的任务不属于此订阅。
		for option := range have {
			if strings.HasPrefix(option, prefix) {
				return false
			}
		}
	}
	return true
}

// SetPausedMonitorScopes 发布监控快照,入队在同一 QueueMu 下检查,不会反向获取监控锁。
func (s *State) SetPausedMonitorScopes(scopes []MonitorQueueScope) {
	s.QueueMu.Lock()
	s.pausedMonitorScopes = scopes
	s.QueueMu.Unlock()
}

// PauseMonitorQueues 先中断本轮尝试并暂停内存任务,再持久化;保存失败也绝不继续执行。
func (s *State) PauseMonitorQueues(scopes []MonitorQueueScope) (int, error) {
	s.saveQueueMu.Lock()
	defer s.saveQueueMu.Unlock()
	s.QueueMu.Lock()
	defer s.QueueMu.Unlock()
	count := 0
	for i := range s.Queue {
		item := &s.Queue[i]
		if item.Status != "running" && item.Status != "pending" {
			continue
		}
		for _, scope := range scopes {
			if !s.matchesMonitorScope(*item, scope) {
				continue
			}
			item.Status = "paused"
			item.UpdatedAt = types.NowISO()
			s.UnregisterTaskCancel(item.ID)
			count++
			break
		}
	}
	if count == 0 {
		return 0, nil
	}
	if err := s.SaveBlocked("queue"); err != nil {
		return count, err
	}
	if s.DB == nil {
		return count, fmt.Errorf("数据库不可用,关联抢购任务仅在本次运行中暂停")
	}
	return count, s.DB.ReplaceQueue(s.Queue)
}

// SetMonitorStopped 即使保存停止状态失败,本次进程仍拒绝迟到的自动建单。
func (s *State) SetMonitorStopped(stopped bool) {
	s.QueueMu.Lock()
	s.monitorManuallyStopped = stopped
	s.QueueMu.Unlock()
}
