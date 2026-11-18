package app

import (
	"reflect"
	"testing"

	"github.com/ovh-buy/server/internal/types"
)

// 同型号两条订阅(GRA / RBX)都被暂停,只恢复 GRA 那条:一键恢复只能列出 GRA 的任务,
// RBX 的任务仍归另一条暂停中的订阅管;用户手动暂停或其它原因暂停的任务也不列。
func TestHeldMonitorTasksScopedToResumedSubscription(t *testing.T) {
	s := &State{DeletedTaskIDs: map[string]struct{}{}}
	s.Queue = []types.QueueItem{
		{ID: "gra", PlanCode: "p", Datacenter: "gra", Status: "paused", StopReason: StopReasonMonitorPaused},
		{ID: "rbx", PlanCode: "p", Datacenter: "rbx", Status: "paused", StopReason: StopReasonMonitorPaused},
		{ID: "manual", PlanCode: "p", Datacenter: "gra", Status: "paused"},
		{ID: "running", PlanCode: "p", Datacenter: "gra", Status: "running"},
		{ID: "other", PlanCode: "q", Datacenter: "gra", Status: "paused", StopReason: StopReasonMonitorPaused},
	}
	gra := MonitorQueueScope{PlanCode: "p", Datacenters: []string{"gra"}}
	rbx := MonitorQueueScope{PlanCode: "p", Datacenters: []string{"rbx"}}
	if got := s.HeldMonitorTasks(gra, []MonitorQueueScope{rbx}); !reflect.DeepEqual(got, []string{"gra"}) {
		t.Fatalf("got %v", got)
	}
	// 不限机房的订阅恢复了,但 RBX 那条仍暂停:RBX 任务仍不能放出
	all := MonitorQueueScope{PlanCode: "p"}
	if got := s.HeldMonitorTasks(all, []MonitorQueueScope{rbx}); !reflect.DeepEqual(got, []string{"gra"}) {
		t.Fatalf("got %v", got)
	}
	if got := s.HeldMonitorTasks(all, nil); !reflect.DeepEqual(got, []string{"gra", "rbx"}) {
		t.Fatalf("got %v", got)
	}
}
