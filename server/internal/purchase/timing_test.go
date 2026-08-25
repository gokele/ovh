package purchase

import (
	"strings"
	"testing"
	"time"
)

func TestTimelineRecordsPhasesInOrder(t *testing.T) {
	tl := newTimeline()
	tl.mark("查库存")
	tl.mark("建购物车")
	tl.mark("下单")

	got := tl.entries()
	if len(got) != 3 {
		t.Fatalf("应该有 3 个阶段,实际 %d", len(got))
	}
	want := []string{"查库存", "建购物车", "下单"}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("第 %d 个阶段应该是 %s,实际 %s", i, w, got[i].Name)
		}
	}
}

// 阶段耗时是"上一个 mark 到这一个 mark",不是"从头到现在" ——
// 搞反的话每个阶段都会包含它前面所有阶段,摘要里的数字加起来会远超总数
func TestTimelinePhasesAreDeltas(t *testing.T) {
	tl := newTimeline()
	time.Sleep(30 * time.Millisecond)
	tl.mark("慢的")
	tl.mark("快的")

	got := tl.entries()
	if got[0].Ms < 20 {
		t.Errorf("第一个阶段应该记到那 30ms,实际 %dms", got[0].Ms)
	}
	if got[1].Ms > 20 {
		t.Errorf("第二个阶段不该把前面的时间算进来,实际 %dms", got[1].Ms)
	}
}

func TestTimelineStringHasTotalAndParts(t *testing.T) {
	tl := newTimeline()
	tl.mark("查库存")
	s := tl.String()
	if !strings.HasPrefix(s, "总 ") || !strings.Contains(s, "查库存") {
		t.Errorf("摘要格式不对: %q", s)
	}
	// 一个 mark 都没有时不该输出半句话
	if (&timeline{}).String() != "" {
		t.Error("没有阶段时摘要应该是空串")
	}
}

// nil timeline 不能 panic:调用点散布在抢购主链路上,
// 哪天有人从别处调 PurchaseServer 而没建 timeline,不该把抢购弄崩
func TestTimelineNilSafe(t *testing.T) {
	var tl *timeline
	tl.mark("x")
	if tl.total() != 0 || tl.String() != "" || tl.entries() != nil {
		t.Error("nil timeline 应该全部返回零值")
	}
}

// key 里必须同时有机型和机房:同一台机器在两个机房是两条独立链路,
// 混成一个 key 的话后写的会盖掉先写的
func TestTimingKeySeparatesDatacenters(t *testing.T) {
	if TimingKey("24sk602", "gra") == TimingKey("24sk602", "rbx") {
		t.Error("不同机房应该是不同的 key")
	}
}

// 用户能订阅任意多个机型 × 机房,不设上限就是内存泄漏
func TestRecordTimingCaps(t *testing.T) {
	lastTimingMu.Lock()
	lastTimings = map[string]lastTiming{}
	lastTimingMu.Unlock()

	for i := 0; i < 600; i++ {
		tl := newTimeline()
		tl.mark("查库存")
		recordTiming(TimingKey("plan", string(rune('a'+i%26))+string(rune(i))), tl, "unavailable")
	}
	if n := len(LastTimings()); n > 500 {
		t.Errorf("最多留 500 条,实际 %d", n)
	}
}
