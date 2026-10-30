package monitor

import (
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPauseAndEditInvalidateInFlightCheck(t *testing.T) {
	dir := t.TempDir()
	st := app.NewState(storage.Paths{LogsDir: filepath.Join(dir, "logs")}, nil, logger.New(filepath.Join(dir, "logs.json"), nil), nil)
	m := New(st)
	m.AddSubscription("plan", nil, true, false, "", nil, nil, false, 0, "", false, []string{"ram-32g"})
	sub := m.subscriptions[0]
	cfg := sub.checkConfig()
	if !m.checkCurrent(sub, cfg) {
		t.Fatal("fresh check rejected")
	}
	key := "ram-32g"
	if !m.SetPaused("plan", &key, true) || m.checkCurrent(sub, cfg) {
		t.Fatal("pause did not stop old check")
	}
	if !m.SetPaused("plan", &key, false) || m.checkCurrent(sub, cfg) {
		t.Fatal("resume revived an old check")
	}
	cfg = sub.checkConfig()
	if !m.checkCurrent(sub, cfg) {
		t.Fatal("new check cannot run after resume")
	}
	if err := m.UpdateSubscriptionFields("plan", &key, []string{"rbx"}, true, false, false, 0, "", false, []string{"ram-64g"}); err != nil {
		t.Fatal(err)
	}
	if m.checkCurrent(sub, cfg) {
		t.Fatal("edit did not invalidate old configuration")
	}
	// A stopped check cannot enqueue even one local quick-order request.
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(200) }))
	defer srv.Close()
	st.Port = strings.TrimPrefix(srv.URL, "http://127.0.0.1:")
	m.batchOrder("plan", nil, []notification{{dc: "gra"}}, 1, "eu", false, func() bool { return false })
	if requests.Load() != 0 {
		t.Fatal("stale check enqueued an order")
	}
	// Persisted pause restoration also prevents query requests when invoked directly.
	sub.mu.Lock()
	sub.Paused = true
	sub.mu.Unlock()
	m.CheckAvailabilityChange(sub, "paused")
}

// Stop 必须使已发出的慢请求失效,下一次启动也不能恢复旧配置快照。
func TestGlobalStopInvalidatesInFlightCheck(t *testing.T) {
	dir := t.TempDir()
	st := app.NewState(storage.Paths{}, nil, logger.New(filepath.Join(dir, "test.log"), nil), nil)
	m := New(st)
	m.AddSubscription("plan", nil, true, false, "", nil, nil, false, 0, "", false, nil)
	sub := m.subscriptions[0]
	cfg := sub.checkConfig()
	m.running = true
	if !m.Stop() || m.checkCurrent(sub, cfg) {
		t.Fatal("global stop left old check active")
	}
	m.running = true
	if m.checkCurrent(sub, cfg) {
		t.Fatal("restart revived pre-stop check")
	}
}

func TestNetworkOptionsDoNotHideMatchingHardware(t *testing.T) {
	hardware := []string{"ram-32g-ecc-2666-24sys01-v1-us", "softraid-2x512nvme-24sys01-v1-us"}
	want := append(append([]string(nil), hardware...), "bandwidth-1000-24sys-us", "vrack-bandwidth-500-24sys-us")
	if !configMatchesFilter(want, hardware) {
		t.Fatal("network addons filtered all inventory")
	}
	if configMatchesFilter(append(want, "ram-64g"), hardware) {
		t.Fatal("wrong RAM accepted")
	}
	if configMatchesFilter(append(want, "unknown-addon"), hardware) {
		t.Fatal("unknown addon silently ignored")
	}
	options := monitoringOptions(hardware, want)
	if len(options) != 4 || options[2] != want[2] || options[3] != want[3] {
		t.Fatalf("order lost selected addons: %v", options)
	}
	// Issue #9: KS-STOR 混合盘 + 500Mbps 仍匹配同一硬件配置。
	stor := []string{"ram-16g-ecc-2133-24skstor01-v1", "hybridsoftraid-4x4000sa-1x500nvme-24skstor01-v1"}
	if !configMatchesFilter(append(stor, "bandwidth-500-25sk"), stor) {
		t.Fatal("KS-STOR bandwidth prevented alerts")
	}
}

func TestSnapshotRestockUsesOnlyRealTransitions(t *testing.T) {
	s := &Subscription{History: []HistoryEntry{
		{Timestamp: "2026-10-20T10:00:00Z", ChangeType: "available", OldStatus: nil},
		{Timestamp: "2026-10-21T10:00:00Z", ChangeType: "available", OldStatus: "unavailable"},
		{Timestamp: "2026-10-22T10:00:00Z", ChangeType: "unavailable", OldStatus: "available"},
		{Timestamp: "2026-10-23T10:00:00Z", ChangeType: "available", OldStatus: "price_check_failed"},
	}}
	restored := fromDBSub(toDBSub(s))
	if got := restored.snapshot().LastRestockAt; got != "2026-10-21T10:00:00Z" {
		t.Fatalf("restock=%s", got)
	}
	s.History = s.History[:1]
	if s.snapshot().LastRestockAt != "" {
		t.Fatal("initial stock reported as restock")
	}
}
