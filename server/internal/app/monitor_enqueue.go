package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

// orderOptionsKey 不改调用方切片,且不用可碰撞的分隔字符串表达配置。
func orderOptionsKey(options []string) string {
	copy := append([]string(nil), options...)
	sort.Strings(copy)
	value, _ := json.Marshal(copy)
	return string(value)
}

// EnqueueMonitorOrders 接收监控已验价的配置,直接持久化整批任务,省掉本地 HTTP 和重复验价。
func (s *State) EnqueueMonitorOrders(account, plan string, dcs, options []string, quantity int, duration string) error {
	if _, ok := s.FindAccount(account); !ok || account == "" {
		return fmt.Errorf("monitor order account is missing")
	}
	if plan == "" || quantity < 1 || quantity > types.MaxOrderQuantity || len(dcs) == 0 || len(dcs)*quantity > types.MaxOrderFanout {
		return fmt.Errorf("invalid monitor order quantity")
	}
	s.monitorEnqueueMu.Lock()
	defer s.monitorEnqueueMu.Unlock()
	fp := orderOptionsKey(options)
	targets := map[string]bool{}
	for _, dc := range dcs {
		key := ovh.ConvertDisplayDCToAPIDC(dc)
		if key == "" || targets[key] {
			return fmt.Errorf("invalid or repeated datacenter")
		}
		targets[key] = true
	}
	s.QueueMu.Lock()
	for _, item := range s.Queue {
		if (item.AccountID == account || item.AccountID == "") && item.PlanCode == plan && targets[ovh.ConvertDisplayDCToAPIDC(item.Datacenter)] && (item.Status == "running" || item.Status == "pending" || item.Status == "paused") && orderOptionsKey(item.Options) == fp {
			s.QueueMu.Unlock()
			return fmt.Errorf("matching order task already exists")
		}
	}
	s.QueueMu.Unlock()
	s.HistoryMu.Lock()
	for _, h := range s.History {
		if (h.AccountID != account && h.AccountID != "") || h.PlanCode != plan || !targets[ovh.ConvertDisplayDCToAPIDC(h.Datacenter)] || h.Status != "success" || orderOptionsKey(h.Options) != fp {
			continue
		}
		if at, ok := types.ParseTS(h.PurchaseTime); ok && time.Since(at) < 120*time.Second {
			s.HistoryMu.Unlock()
			return fmt.Errorf("matching order succeeded within 120 seconds")
		}
	}
	s.HistoryMu.Unlock()
	items := make([]types.QueueItem, 0, len(dcs)*quantity)
	now := types.NowISO()
	for _, dc := range dcs {
		for i := 0; i < quantity; i++ {
			items = append(items, types.QueueItem{ID: uuid.NewString(), AccountID: account, PlanCode: plan, Datacenter: dc, Options: append([]string(nil), options...), Status: "running", Duration: duration, MaxRetries: 20, RetryInterval: s.Config.QuickOrderRetryInterval(), CreatedAt: now, UpdatedAt: now, QuickOrder: true, Priority: 100})
		}
	}
	// 原有持久暂停/停止闸门和数据库失败回滚都在这个共同入口,不得绕开。
	return s.EnqueueAutomaticItems(items, true)
}

// QueueChanges 只唤醒调度器扫描新任务;已预装任务的库存走独立信箱。
func (s *State) QueueChanges() <-chan struct{} { return s.queueWake }

// WakeQueue 合并重复变更,不让落库后的控制路径等待调度器。
func (s *State) WakeQueue() {
	select {
	case s.queueWake <- struct{}{}:
	default:
	}
}
