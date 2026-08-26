package monitor

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/logger"
)

// 回归测试:检查 goroutine 与 HTTP 侧并发读写同一条 *Subscription。
//
// 背景(本轮修的 bug):loop 每轮复制出去的是 []*Subscription —— 复制的只有指针,
// subsMu 保护得了切片,保护不了指针指向的结构体。检查 goroutine 过去在锁外直接写
// sub.LastCheckAt / LastCheckError / LastStatus / History,而 Status()/Snapshot()/toDBSub
// 在持 subsMu 时读同一批字段,`go test -race` 会稳定报 DATA RACE。
//
// 这个测试不打网络、不碰 OVH:它只模拟"一个写者按 check.go 的写序推进状态 +
// 多个读者按 handler 的读法读"这一对并发关系,靠 -race 检测器判定。
// 跑法:cd server && go test -race ./internal/monitor/
func TestSubscriptionConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	state := &app.State{
		Logger: logger.New(filepath.Join(dir, "logs.json"),
			slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))),
	}
	m := New(state)
	m.AddSubscription("24sk102-us", []string{"bhs", "vin"}, true, true, "SK102", nil, nil, false, 0, "", false)
	m.AddSubscription("24adv01-v3", nil, true, false, "ADV1", nil, nil, true, 2, "acc-eu", false)

	subs := func() []*Subscription {
		m.subsMu.Lock()
		defer m.subsMu.Unlock()
		out := make([]*Subscription, len(m.subscriptions))
		copy(out, m.subscriptions)
		return out
	}()

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// 写者:完全照 check.go 的写序来(beginCheck → 状态推进 → 追加历史 → 写回状态)
	for _, sub := range subs {
		wg.Add(1)
		go func(s *Subscription) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				prev := s.beginCheck(time.Now().Format(time.RFC3339Nano), "acc-eu", "EU", "IE")
				_ = prev
				cfg := s.checkConfig()
				_ = cfg
				status := s.statusSnapshot()
				status["gra|ram-64g"] = "available"
				s.appendHistory([]HistoryEntry{{
					Timestamp:  time.Now().Format(time.RFC3339Nano),
					Datacenter: "gra",
					Status:     "available",
					ChangeType: "available",
					Config:     map[string]interface{}{"display": "64G + 2x960"},
				}}, maxHistorySize)
				_ = s.historySnapshot()
				s.replaceLastStatus(status)
				if i%3 == 0 {
					s.setCheckError("机型只在 US 站点查得到")
				}
			}
		}(sub)
	}

	// 读者:照 handler 的读法(Status / Snapshot / Marshal / 落库转换)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := json.Marshal(m.Status()); err != nil {
					t.Error(err)
					return
				}
				for _, s := range m.Snapshot() {
					if _, err := json.Marshal(s); err != nil {
						t.Error(err)
						return
					}
					_ = toDBSub(s)
				}
				if s := m.FindSubscription("24sk102-us"); s != nil {
					_ = len(s.History)
				}
			}
		}()
	}

	// 改配置的一方(用户在 UI 上改订阅):走 AddSubscription 的更新分支
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			m.AddSubscription("24sk102-us", []string{"bhs"}, i%2 == 0, true, "SK102", nil, nil, true, 1, "acc-us", false)
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// Snapshot / FindSubscription 必须返回深拷贝:调用方(handler)拿到的对象再怎么被
// 别处修改,都不能影响真实订阅,反过来检查 goroutine 继续写真身也不能污染已经发出去的响应。
func TestSnapshotIsDeepCopy(t *testing.T) {
	dir := t.TempDir()
	state := &app.State{
		Logger: logger.New(filepath.Join(dir, "logs.json"),
			slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))),
	}
	m := New(state)
	m.AddSubscription("24adv01-v3", []string{"gra"}, true, false, "ADV1", nil, nil, false, 0, "", false)

	live := m.subscriptions[0]
	live.replaceLastStatus(map[string]string{"gra|x": "available"})
	live.appendHistory([]HistoryEntry{{Timestamp: "t0", Datacenter: "gra"}}, maxHistorySize)

	cp := m.Snapshot()[0]
	cp.LastStatus["gra|x"] = "unavailable"
	cp.History[0].Datacenter = "rbx"
	cp.Datacenters[0] = "bhs"

	if got := live.statusSnapshot()["gra|x"]; got != "available" {
		t.Errorf("改副本污染了真身 LastStatus: %s", got)
	}
	if got := live.historySnapshot()[0].Datacenter; got != "gra" {
		t.Errorf("改副本污染了真身 History: %s", got)
	}
	if got := live.checkConfig().Datacenters[0]; got != "gra" {
		t.Errorf("改副本污染了真身 Datacenters: %s", got)
	}

	// 真身继续推进,已经拿到手的副本不受影响
	live.appendHistory([]HistoryEntry{{Timestamp: "t1", Datacenter: "sbg"}}, maxHistorySize)
	if len(cp.History) != 1 {
		t.Errorf("副本被真身的后续写入影响了: %d 条", len(cp.History))
	}
}
