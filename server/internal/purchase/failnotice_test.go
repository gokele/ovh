package purchase

import (
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/types"
)

// 一条 /buy 最多扇出 60 个任务(MaxOrderFanout)。跨区 planCode 这类确定性失败
// 是每个任务第一轮就判的 —— 不合并的话用户几秒内被刷 60 条一模一样的消息,
// 真正要看的那条原因反而被淹了。
func TestFailNoticeCollapsesBurst(t *testing.T) {
	f := &failNotifier{seen: map[string]*failGroup{}}
	const reason = "确定性失败，重试无用：跨区 planCode"

	sent := 0
	for _, dc := range []string{"gra", "rbx", "sbg", "waw", "bhs", "gra", "rbx"} {
		if _, ok := f.take(&types.QueueItem{PlanCode: "24sk602", Datacenter: dc}, reason, true); ok {
			sent++
		}
	}
	if sent != 1 {
		t.Fatalf("同一批失败应只发 1 条,实际 %d 条", sent)
	}

	// 不同型号是另一件事,要单独通知
	if _, ok := f.take(&types.QueueItem{PlanCode: "24ska01", Datacenter: "gra"}, reason, true); !ok {
		t.Fatal("不同型号应各自通知")
	}
	// 同型号但不同原因,也是另一件事
	if _, ok := f.take(&types.QueueItem{PlanCode: "24sk602", Datacenter: "gra"}, "连续 5 次下单尝试均失败，停止重试", false); !ok {
		t.Fatal("不同原因应各自通知")
	}
}

// 窗口过后必须能再发:半分钟后又一批真的失败了,那是新事件,不能被吞掉。
func TestFailNoticeReopensAfterWindow(t *testing.T) {
	f := &failNotifier{seen: map[string]*failGroup{}}
	const reason = "连续 5 次下单尝试均失败，停止重试"
	item := &types.QueueItem{PlanCode: "24sk602", Datacenter: "gra"}

	if _, ok := f.take(item, reason, false); !ok {
		t.Fatal("第一条必须发")
	}
	if _, ok := f.take(item, reason, false); ok {
		t.Fatal("窗口内第二条不该发")
	}

	// 把这一组的起始时间推到窗口之外，模拟时间流逝
	for _, g := range f.seen {
		g.first = time.Now().Add(-failNoticeWindow - time.Second)
	}
	if _, ok := f.take(item, reason, false); !ok {
		t.Fatal("窗口过后应能再次通知 —— 那是一次新的失败,吞掉就等于又回到了静默")
	}
}

// 消息本身要能分清两种终止:Fatal 改了才有用,用尽重试可以直接重开。
func TestFailMessageDistinguishesFatal(t *testing.T) {
	item := &types.QueueItem{PlanCode: "24sk602", Datacenter: "gra"}

	fatal := buildTaskFailedMessage(item, "跨区 planCode", true)
	if !contains(fatal, "重试也不会变") || !contains(fatal, "三个大区") {
		t.Fatalf("Fatal 消息要说明白「改了才有用」:\n%s", fatal)
	}
	exhausted := buildTaskFailedMessage(item, "连续 5 次失败", false)
	if !contains(exhausted, "重新下一单") {
		t.Fatalf("用尽重试的消息要给出下一步:\n%s", exhausted)
	}
	// 两种都要带上型号和机房,否则多任务时看不出说的是哪一单
	for _, m := range []string{fatal, exhausted} {
		if !contains(m, "24sk602") || !contains(m, "GRA") {
			t.Fatalf("消息里必须有型号和机房:\n%s", m)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
