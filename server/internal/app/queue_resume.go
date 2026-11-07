package app

import (
	"database/sql"
	"errors"

	"github.com/ovh-buy/server/internal/types"
)

var ErrQueueAlreadyOrdered = errors.New("该任务已下单,不能再次启动。如需再买,请新建任务。")

// ResumeQueueItem publishes a runnable task only after its state has been saved.
func (s *State) ResumeQueueItem(id, status string) error {
	if status != "running" && status != "pending" {
		return errors.New("无效的恢复状态")
	}
	if err := s.SaveBlocked("queue"); err != nil {
		return err
	}
	s.saveQueueMu.Lock()
	defer s.saveQueueMu.Unlock()
	s.QueueMu.Lock()
	defer s.QueueMu.Unlock()
	defer s.WakeQueue()
	for i, item := range s.Queue {
		if item.ID != id {
			continue
		}
		if item.Status == "completed" {
			return ErrQueueAlreadyOrdered
		}
		next := append([]types.QueueItem(nil), s.Queue...)
		next[i].Status, next[i].UpdatedAt = status, types.NowISO()
		next[i].StopReason = ""
		next[i].OrderPhase = ""
		resuming := item.Status != "running" && item.Status != "pending"
		if resuming {
			next[i].LastCheckTime = 0
		}
		if item.Status == "failed" {
			next[i].FailureCount = 0
		}
		key := s.automaticOrderKey(item)
		if err := s.DB.ResumeQueueItem(next[i], key, resuming); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrQueueNotFound
			}
			return err
		}
		if s.automaticOrderBlocks == nil {
			s.automaticOrderBlocks = map[string]bool{}
		}
		s.automaticOrderBlocks[key] = false
		s.Queue = next
		return nil
	}
	return ErrQueueNotFound
}
