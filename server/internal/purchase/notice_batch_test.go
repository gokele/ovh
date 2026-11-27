package purchase

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

type sent struct {
	text   string
	markup map[string]interface{}
}

func testBatcher() (*noticeBatcher, func() []sent) {
	var mu sync.Mutex
	var out []sent
	b := &noticeBatcher{window: 50 * time.Millisecond, pending: map[noticeKind][]pendingNotice{}}
	b.send = func(_ *app.State, text string, markup map[string]interface{}) {
		mu.Lock()
		out = append(out, sent{text, markup})
		mu.Unlock()
	}
	return b, func() []sent {
		time.Sleep(120 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		return append([]sent(nil), out...)
	}
}

func notice(id, dc string) pendingNotice {
	return pendingNotice{item: types.QueueItem{ID: id, PlanCode: "plan", Datacenter: dc}, text: "single " + id, markup: map[string]interface{}{"k": id}}
}

func TestSingleNoticeIsSentUnchanged(t *testing.T) {
	b, wait := testBatcher()
	b.add(nil, noticeUncertain, notice("a", "fra"))
	got := wait()
	if len(got) != 1 || got[0].text != "single a" || got[0].markup["k"] != "a" {
		t.Fatalf("single notice changed: %+v", got)
	}
}

func TestBurstIsMergedPerKindWithResumeButtons(t *testing.T) {
	b, wait := testBatcher()
	for _, n := range []pendingNotice{notice("a", "fra"), notice("b", "fra"), notice("c", "gra")} {
		b.add(nil, noticeUncertain, n)
	}
	b.add(nil, noticeAutoResumed, notice("d", "rbx"))
	got := wait()
	if len(got) != 2 {
		t.Fatalf("expected one merged message per kind, got %d: %+v", len(got), got)
	}
	var merged sent
	for _, s := range got {
		if strings.Contains(s.text, "3 台") {
			merged = s
		}
	}
	if !strings.Contains(merged.text, "plan@FRA ×2") || !strings.Contains(merged.text, "plan@GRA") {
		t.Fatalf("merged text missing lines: %q", merged.text)
	}
	rows, _ := merged.markup["inline_keyboard"].([][]map[string]interface{})
	if len(rows) != 3 {
		t.Fatalf("expected a resume button per task, got %d", len(rows))
	}
	if !strings.Contains(rows[0][0]["text"].(string), "#1") || !strings.Contains(rows[1][0]["text"].(string), "#2") {
		t.Fatalf("same plan@dc buttons must be numbered: %v", rows)
	}
}

func TestLargeBurstPointsToQueuePageInsteadOfButtons(t *testing.T) {
	b, wait := testBatcher()
	for i := 0; i < noticeMaxButtons+2; i++ {
		b.add(nil, noticeUnresolved, pendingNotice{item: types.QueueItem{ID: string(rune('a' + i)), PlanCode: "plan", Datacenter: "fra"}, candidates: []string{"111", "222"}})
	}
	got := wait()
	if len(got) != 1 || got[0].markup != nil || !strings.Contains(got[0].text, "抢购队列") || !strings.Contains(got[0].text, "111、222") {
		t.Fatalf("large burst not merged as expected: %+v", got)
	}
}
