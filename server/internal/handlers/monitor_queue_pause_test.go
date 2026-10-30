package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/types"
)

// 监控暂停必须取消关联尝试,并精确到账户、机房和配置,包括旧网络选项缺失的任务。
func TestMonitorPauseAlsoPausesMatchingQueue(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	st.Accounts = []types.OVHAccount{{ID: "eu", IsDefault: true}}
	options := []string{"ram-32g", "bandwidth-1000"}
	mon.AddSubscription("plan", []string{"TOR"}, true, false, "", nil, nil, true, 1, "eu", false, options)
	matching := types.QueueItem{AccountID: "eu", PlanCode: "plan", Datacenter: "ca-east-tor-a", Options: options, Status: "running"}
	tasks := []struct {
		id, status, account, dc string
		options                 []string
		plan                    string
		pause                   bool
	}{
		{"running", "running", "eu", "ca-east-tor-a", options, "plan", true},
		{"pending-default", "pending", "", "tor", options, "plan", true},
		{"legacy", "running", "eu", "tor", []string{"ram-32g"}, "plan", true},
		{"other-account", "running", "other", "tor", options, "plan", false},
		{"other-dc", "running", "eu", "gra", options, "plan", false},
		{"other-ram", "running", "eu", "tor", []string{"ram-64g", "bandwidth-1000"}, "plan", false},
		{"other-bandwidth", "running", "eu", "tor", []string{"ram-32g", "bandwidth-500"}, "plan", false},
		{"other-plan", "running", "eu", "tor", options, "other", false},
		{"completed", "completed", "eu", "tor", options, "plan", false},
		{"failed", "failed", "eu", "tor", options, "plan", false},
	}
	contexts := map[string]context.Context{}
	for _, tc := range tasks {
		item := matching
		item.ID = tc.id
		item.Status = tc.status
		item.AccountID = tc.account
		item.Datacenter = tc.dc
		item.Options = tc.options
		item.PlanCode = tc.plan
		st.Queue = append(st.Queue, item)
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		contexts[tc.id] = ctx
		st.RegisterTaskCancel(tc.id, cancel)
	}
	if err := st.SaveQueue(); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/monitor/:planCode/pause", PauseSubscription(st, mon))
	body := fmt.Sprintf(`{"optionsKey":%q,"paused":true}`, monitor.OptionsKey(options))
	w := payRequest(router, "POST", "/monitor/plan/pause", body)
	var result struct {
		PausedTasks int `json:"pausedTasks"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || result.PausedTasks != 3 {
		t.Fatalf("pause %d %s", w.Code, w.Body)
	}
	saved, err := st.DB.ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]types.QueueItem{}
	for _, item := range saved {
		byID[item.ID] = item
	}
	for _, tc := range tasks {
		expected := tc.status
		if tc.pause {
			expected = "paused"
		}
		if byID[tc.id].Status != expected || (contexts[tc.id].Err() != nil) != tc.pause {
			t.Fatalf("%s: status=%s cancelled=%v", tc.id, byID[tc.id].Status, contexts[tc.id].Err())
		}
	}
	// 暂停的 RAM32 不应阻断另一个配置的自动入队。
	different := matching
	different.ID = "new-64g"
	different.Options = []string{"ram-64g", "bandwidth-1000"}
	if err := st.EnqueueAutomaticItems([]types.QueueItem{different}, true); err != nil {
		t.Fatal("unrelated config blocked", err)
	}
	// 恢复监控不会擅自把已经暂停的抢购任务恢复运行。
	w = payRequest(router, "POST", "/monitor/plan/pause", strings.Replace(body, `"paused":true`, `"paused":false`, 1))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	for _, item := range st.Queue {
		if item.ID == "running" && item.Status != "paused" {
			t.Fatal("resume restarted purchase")
		}
	}
}

// 即使暂停时还没有任务,已经在请求途中的自动建单也必须被挡住,且重启后仍有效。
func TestPausedMonitorRejectsLateEnqueueAfterReload(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	mon.AddSubscription("plan", nil, true, false, "", nil, nil, true, 1, "eu", false, nil)
	r := gin.New()
	r.POST("/pause/:planCode", PauseSubscription(st, mon))
	w := payRequest(r, "POST", "/pause/plan", `{"optionsKey":"","paused":true}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	item := types.QueueItem{ID: "late", AccountID: "eu", PlanCode: "plan", Datacenter: "gra", Status: "running"}
	if err := st.EnqueueAutomaticItems([]types.QueueItem{item}, true); err == nil {
		t.Fatal("late task escaped paused subscription")
	}
	// 清空内存闸门,确认它来自已保存的订阅,而不是仅依赖某个暂停队列任务。
	st.SetPausedMonitorScopes(nil)
	reloaded := monitor.New(st)
	reloaded.LoadFromDB()
	if err := st.EnqueueAutomaticItems([]types.QueueItem{item}, true); err == nil {
		t.Fatal("restart lost subscription gate")
	}
}

// 全局停止在监控已经没运行时也要暂停所有订阅关联任务,保留不相关任务。
func TestStopMonitorPausesAllSubscribedQueues(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	mon.AddSubscription("a", nil, true, false, "", nil, nil, false, 0, "", false, nil)
	mon.AddSubscription("b", []string{"gra"}, true, false, "", nil, nil, false, 0, "", false, nil)
	st.Queue = []types.QueueItem{{ID: "a", PlanCode: "a", Status: "running"}, {ID: "b", PlanCode: "b", Datacenter: "gra", Status: "pending"}, {ID: "unrelated", PlanCode: "c", Status: "running"}, {ID: "other-dc", PlanCode: "b", Datacenter: "rbx", Status: "running"}}
	router := gin.New()
	router.POST("/stop", StopMonitor(st, mon))
	w := payRequest(router, "POST", "/stop", `{}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	for i, item := range st.Queue {
		want := "running"
		if i < 2 {
			want = "paused"
		}
		if item.Status != want {
			t.Fatalf("%s: %s", item.ID, item.Status)
		}
	}
	if mon.AutoStartAllowed() {
		t.Fatal("global stop did not persist")
	}
}

// 取消与自动入队竞争时,无论谁先获得队列锁,最终都不能残留运行中的关联任务。
func TestMonitorPauseRacesAutomaticEnqueue(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	mon.AddSubscription("plan", nil, true, false, "", nil, nil, true, 1, "eu", false, nil)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_ = st.EnqueueAutomaticItems([]types.QueueItem{{ID: fmt.Sprint(i), PlanCode: "plan", AccountID: "eu", Status: "running", Datacenter: "gra"}}, true)
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		if _, err := mon.SetPaused("plan", nil, true); err != nil {
			t.Error(err)
		}
	}()
	close(start)
	wg.Wait()
	for _, item := range st.Queue {
		if item.Status != "paused" {
			t.Fatalf("running task survived pause: %s", item.ID)
		}
	}
}

// 队列保存失败也必须立刻停掉尝试并报错;已保存的监控暂停可在重启时修复旧队列。
func TestMonitorPauseSaveFailureStaysStopped(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	mon.AddSubscription("plan", nil, true, false, "", nil, nil, false, 0, "", false, nil)
	st.Queue = []types.QueueItem{{ID: "task", PlanCode: "plan", Status: "running"}}
	if err := st.SaveQueue(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st.RegisterTaskCancel("task", cancel)
	_, err := st.DB.Exec(`CREATE TRIGGER reject_pause BEFORE INSERT ON queue WHEN NEW.status='paused' BEGIN SELECT RAISE(ABORT,'test failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/pause/:planCode", PauseSubscription(st, mon))
	w := payRequest(r, "POST", "/pause/plan", `{"paused":true}`)
	if w.Code != 500 || st.Queue[0].Status != "paused" || ctx.Err() == nil {
		t.Fatalf("unsafe failed pause: %d %s %v", w.Code, w.Body, ctx.Err())
	}
	if _, err = st.DB.Exec(`DROP TRIGGER reject_pause`); err != nil {
		t.Fatal(err)
	}
	st.Queue, err = st.DB.ListQueue()
	if err != nil {
		t.Fatal(err)
	}
	if st.Queue[0].Status != "running" {
		t.Fatal("test did not retain pre-pause persisted row")
	}
	reloaded := monitor.New(st)
	reloaded.LoadFromDB()
	saved, err := st.DB.ListQueue()
	if err != nil || st.Queue[0].Status != "paused" || saved[0].Status != "paused" {
		t.Fatal("restart revived old task", err)
	}
}

// 未知订阅不能暂停任何任务。
func TestMissingMonitorPauseLeavesQueueUntouched(t *testing.T) {
	st, mon := newWatchTestMonitor(t)
	st.Queue = []types.QueueItem{{ID: "task", PlanCode: "plan", Status: "running"}}
	r := gin.New()
	r.POST("/pause/:planCode", PauseSubscription(st, mon))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/pause/missing", strings.NewReader(`{"paused":true}`)))
	if w.Code != 404 || st.Queue[0].Status != "running" {
		t.Fatal(w.Code, w.Body)
	}
}
