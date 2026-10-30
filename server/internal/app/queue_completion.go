package app

import "github.com/ovh-buy/server/internal/types"

// RestoreCompletedQueue 用成功下单记录修复旧队列状态,防止完成状态未落库后重启再买一台。
// 只修复仍存在的任务,不会重新创建用户删除或旧版本已移除的任务。
func (s *State) RestoreCompletedQueue() error {
	if err := s.SaveBlocked("history"); err != nil {
		return err
	}
	if err := s.SaveBlocked("queue"); err != nil {
		return err
	}
	ordered := map[string]bool{}
	s.HistoryMu.Lock()
	for _, h := range s.History {
		if h.TaskID != "" && h.Status == "success" && h.OrderID != "" {
			ordered[h.TaskID] = true
		}
	}
	s.HistoryMu.Unlock()
	changed := false
	s.QueueMu.Lock()
	for i := range s.Queue {
		if ordered[s.Queue[i].ID] && s.Queue[i].Status != "completed" {
			s.Queue[i].Status = "completed"
			s.Queue[i].UpdatedAt = types.NowISO()
			s.UnregisterTaskCancel(s.Queue[i].ID)
			changed = true
		}
	}
	s.QueueMu.Unlock()
	if changed {
		return s.SaveQueue()
	}
	return nil
}
