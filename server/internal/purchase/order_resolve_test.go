package purchase

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

// resolveFixture 一个暂停中的结果不明任务,订单接口由本地假 transport 应答(不访问 OVH)。
// orders: 订单号 → 明细扩展(plan.code / dedicated_datacenter / duration)。
func resolveFixture(t *testing.T, endpoint string, orders map[int64][3]string) (*app.State, resolveTarget) {
	t.Helper()
	state := queueCompletionState(t)
	acc := types.OVHAccount{ID: "acc", Name: "acc", Endpoint: endpoint, Zone: "IE", AppKey: "ak", AppSecret: "as", ConsumerKey: "ck"}
	if endpoint == "ovh-us" {
		acc.Zone = "US"
	}
	state.Accounts = []types.OVHAccount{acc}
	item := types.QueueItem{ID: "task-1", AccountID: "acc", PlanCode: "24ska01", Datacenter: "gra", Duration: "P1M", Status: "paused"}
	state.Queue = []types.QueueItem{item}
	if err := state.SaveQueue(); err != nil {
		t.Fatal(err)
	}
	if ok, err := state.DB.ClaimQueueOrder(item); err != nil || !ok {
		t.Fatal("claim", ok, err)
	}
	base, err := state.OVH.ClientForAccount(acc)
	if err != nil {
		t.Fatal(err)
	}
	base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		path = path[strings.Index(path, "/1.0")+4:]
		if r.Method != http.MethodGet {
			t.Errorf("resolver must be read-only, got %s %s", r.Method, path)
		}
		switch {
		case path == "/auth/time":
			return testSaleResponse(r, 200, fmt.Sprint(time.Now().Unix())), nil
		case path == "/me/order":
			ids := []string{}
			for id := range orders {
				ids = append(ids, fmt.Sprint(id))
			}
			return testSaleResponse(r, 200, "["+strings.Join(ids, ",")+"]"), nil
		case strings.HasSuffix(path, "/details"):
			return testSaleResponse(r, 200, "[1]"), nil
		case strings.HasSuffix(path, "/extension"):
			var id int64
			fmt.Sscanf(path, "/me/order/%d/details/1/extension", &id)
			o := orders[id]
			return testSaleResponse(r, 200, fmt.Sprintf(`{"order":{"plan":{"code":%q,"duration":%q},"configurations":[{"label":"dedicated_datacenter","value":%q}]}}`, o[0], o[2], o[1])), nil
		}
		return testSaleResponse(r, 200, `{}`), nil
	})
	return state, resolveTarget{item: item, account: acc, apiDC: "gra", duration: "P1M", sentAt: time.Now()}
}

func taskStatus(state *app.State, id string) string {
	state.QueueMu.Lock()
	defer state.QueueMu.Unlock()
	for _, it := range state.Queue {
		if it.ID == id {
			return it.Status
		}
	}
	return ""
}

func noSleep(time.Duration) {}

// 恰好一笔型号/机房/周期吻合的订单:任务标为已下单,历史记下订单号;另一型号的订单不被误认。
func TestResolverConfirmsSingleMatchingOrder(t *testing.T) {
	resolver = &orderResolver{active: map[string]bool{}, claimed: map[string]bool{}}
	state, target := resolveFixture(t, "ovh-eu", map[int64][3]string{
		555: {"24ska01", "gra", "P1M"},
		556: {"24rise01", "gra", "P1M"},
		557: {"24ska01", "rbx", "P1M"},
	})
	runOrderResolver(state, target, []time.Duration{0}, noSleep)
	if got := taskStatus(state, "task-1"); got != "completed" {
		t.Fatalf("status = %q", got)
	}
	state.HistoryMu.Lock()
	defer state.HistoryMu.Unlock()
	if len(state.History) != 1 || state.History[0].OrderID != "555" || state.History[0].Status != "success" {
		t.Fatalf("history = %+v", state.History)
	}
}

// 两笔都吻合:无法判断是哪一笔,任务保持暂停。
func TestResolverLeavesAmbiguousOrdersToUser(t *testing.T) {
	resolver = &orderResolver{active: map[string]bool{}, claimed: map[string]bool{}}
	state, target := resolveFixture(t, "ovh-eu", map[int64][3]string{
		555: {"24ska01", "gra", "P1M"},
		556: {"24ska01", "gra", "P1M"},
	})
	runOrderResolver(state, target, []time.Duration{0, 0}, noSleep)
	if got := taskStatus(state, "task-1"); got != "paused" {
		t.Fatalf("ambiguous orders must keep the task paused, got %q", got)
	}
}

// 用户已确认没有订单并恢复任务(结账登记被清除):核对结果作废,不把任务改成已下单。
func TestResolverStopsWhenUserResumed(t *testing.T) {
	resolver = &orderResolver{active: map[string]bool{}, claimed: map[string]bool{}}
	state, target := resolveFixture(t, "ovh-eu", map[int64][3]string{555: {"24ska01", "gra", "P1M"}})
	if err := state.ResumeQueueItem("task-1", "running"); err != nil {
		t.Fatal(err)
	}
	runOrderResolver(state, target, []time.Duration{0}, noSleep)
	if got := taskStatus(state, "task-1"); got != "running" {
		t.Fatalf("resumed task was changed to %q", got)
	}
}

// 订单号已记在别的任务名下:不能再记一次;美国站只提示不确认。
func TestResolverSkipsClaimedOrdersAndUSAccounts(t *testing.T) {
	resolver = &orderResolver{active: map[string]bool{}, claimed: map[string]bool{}}
	state, target := resolveFixture(t, "ovh-eu", map[int64][3]string{555: {"24ska01", "gra", "P1M"}})
	state.HistoryMu.Lock()
	state.History = append(state.History, types.PurchaseHistoryEntry{ID: "h", TaskID: "other", AccountID: "acc", OrderID: "555", Status: "success"})
	state.HistoryMu.Unlock()
	runOrderResolver(state, target, []time.Duration{0}, noSleep)
	if got := taskStatus(state, "task-1"); got != "paused" {
		t.Fatalf("order credited to another task was reused: %q", got)
	}

	resolver = &orderResolver{active: map[string]bool{}, claimed: map[string]bool{}}
	us, usTarget := resolveFixture(t, "ovh-us", map[int64][3]string{777: {"24ska01", "gra", "P1M"}})
	runOrderResolver(us, usTarget, []time.Duration{0}, noSleep)
	if got := taskStatus(us, "task-1"); got != "paused" {
		t.Fatalf("US account must not be auto-confirmed without the details extension: %q", got)
	}
}

// 429 后满 1 分钟账户里没有任何新订单:自动恢复;只要出现新订单(哪怕不吻合)就保持暂停。
func TestResolverAutoResumesAfterRateLimitOnlyWithNoNewOrders(t *testing.T) {
	resolver = &orderResolver{active: map[string]bool{}, claimed: map[string]bool{}}
	state, target := resolveFixture(t, "ovh-eu", map[int64][3]string{})
	target.autoResume = true
	runOrderResolver(state, target, []time.Duration{30 * time.Second, 30 * time.Second}, noSleep)
	if got := taskStatus(state, "task-1"); got != "running" {
		t.Fatalf("429 with no new orders should resume, got %q", got)
	}

	resolver = &orderResolver{active: map[string]bool{}, claimed: map[string]bool{}}
	state, target = resolveFixture(t, "ovh-eu", map[int64][3]string{900: {"other-plan", "rbx", "P1M"}})
	target.autoResume = true
	runOrderResolver(state, target, []time.Duration{30 * time.Second, 30 * time.Second}, noSleep)
	if got := taskStatus(state, "task-1"); got != "paused" {
		t.Fatalf("any new order must keep the task paused, got %q", got)
	}

	resolver = &orderResolver{active: map[string]bool{}, claimed: map[string]bool{}}
	state, target = resolveFixture(t, "ovh-eu", map[int64][3]string{})
	runOrderResolver(state, target, []time.Duration{30 * time.Second, 30 * time.Second}, noSleep)
	if got := taskStatus(state, "task-1"); got != "paused" {
		t.Fatalf("non-429 uncertain results must never auto-resume, got %q", got)
	}

	resolver = &orderResolver{active: map[string]bool{}, claimed: map[string]bool{}}
	state, target = resolveFixture(t, "ovh-eu", map[int64][3]string{})
	target.autoResume = true
	runOrderResolver(state, target, []time.Duration{10 * time.Second}, noSleep)
	if got := taskStatus(state, "task-1"); got != "paused" {
		t.Fatalf("must observe for a full minute before resuming, got %q", got)
	}
}
