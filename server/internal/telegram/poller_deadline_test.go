package telegram

import (
	"sync/atomic"
	"testing"
	"time"
)

// 一条慢 update 不能拖住后面的按钮点击:超时后放行,慢的那条在后台继续执行完。
func TestHandleWithDeadlineDoesNotBlockLaterUpdates(t *testing.T) {
	old := handleSlowAfter
	handleSlowAfter = 50 * time.Millisecond
	defer func() { handleSlowAfter = old }()

	release := make(chan struct{})
	var finished atomic.Bool
	p := &Poller{state: newAccTestState(t), handle: func(up map[string]interface{}) {
		if up["slow"] == true {
			<-release
			finished.Store(true)
		}
	}}

	start := time.Now()
	p.handleWithDeadline(map[string]interface{}{"slow": true}, 1)
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("slow update blocked the loop for %s", waited)
	}
	p.handleWithDeadline(map[string]interface{}{"slow": false}, 2) // 后续 update 照常处理
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for !finished.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !finished.Load() {
		t.Fatal("slow update must still finish in the background")
	}
}
