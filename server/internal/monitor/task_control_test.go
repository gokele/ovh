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
