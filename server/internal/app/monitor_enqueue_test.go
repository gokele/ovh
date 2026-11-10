package app

import (
	"sync"
	"testing"

	"github.com/ovh-buy/server/internal/types"
)

// 内存直入必须保持明确数量、账户、硬件和付款开关,同时拒绝重复补货事件。
func TestMonitorDirectEnqueuePreservesQuantityAndDeduplicates(t *testing.T) {
	state := enqueueTestState(t)
	state.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu"}}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = state.EnqueueMonitorOrders("eu", "plan", []string{"gra", "bhs"}, []string{"ram-32g"}, 2, "P12M")
		}()
	}
	wg.Wait()
	if len(state.Queue) != 4 {
		t.Fatal("duplicate or missing tasks", len(state.Queue))
	}
	for _, item := range state.Queue {
		if item.Duration != "P12M" || item.AccountID != "eu" || !item.QuickOrder || len(item.Options) != 1 || item.Options[0] != "ram-32g" {
			t.Fatal(item)
		}
	}
	persisted, err := state.DB.ListQueue()
	if err != nil || len(persisted) != 4 || persisted[0].Duration != "P12M" {
		t.Fatal(err, len(persisted))
	}
	select {
	case <-state.QueueChanges():
	default:
		t.Fatal("scheduler was not notified")
	}
}

// 全局停止、持久暂停、数据库失败和过大数量仍然禁止监控建单。
func TestMonitorDirectEnqueueCannotBypassStopOrPersistence(t *testing.T) {
	for _, mode := range []string{"stopped", "blocked", "closed", "quantity"} {
		t.Run(mode, func(t *testing.T) {
			state := enqueueTestState(t)
			state.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu"}}
			quantity := 1
			switch mode {
			case "stopped":
				if err := state.DB.SetKV("monitor-manually-stopped", true); err != nil {
					t.Fatal(err)
				}
			case "blocked":
				state.QueueMu.Lock()
				err := state.SetAutomaticOrderBlockedLocked(types.QueueItem{AccountID: "eu", PlanCode: "plan", Datacenter: "gra"}, true)
				state.QueueMu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
			case "closed":
				state.DB.Close()
			case "quantity":
				quantity = types.MaxOrderQuantity + 1
			}
			if err := state.EnqueueMonitorOrders("eu", "plan", []string{"gra"}, nil, quantity, ""); err == nil || len(state.Queue) != 0 {
				t.Fatal("safety gate bypassed", err, state.Queue)
			}
		})
	}
}

// 旧版本缺少账户标识的运行任务仍需去重,不能在启动固定账户前被自动下单绕过。
func TestMonitorDirectEnqueueHonorsLegacyUnscopedTasks(t *testing.T) {
	state := enqueueTestState(t)
	state.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu"}}
	state.Queue = []types.QueueItem{{ID: "legacy", PlanCode: "plan", Datacenter: "gra", Status: "running"}}
	if err := state.EnqueueMonitorOrders("eu", "plan", []string{"gra"}, nil, 1, ""); err == nil || len(state.Queue) != 1 {
		t.Fatal("legacy task duplicated", err)
	}
}
