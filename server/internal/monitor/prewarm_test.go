package monitor

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

type monitorWarmRecord struct {
	items  []types.QueueItem
	active func() bool
}
type monitorTestPool struct {
	mu      sync.Mutex
	records map[string]monitorWarmRecord
}

func (p *monitorTestPool) Prepare(owner string, items []types.QueueItem, _ time.Duration, active func() bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records[owner] = monitorWarmRecord{items, active}
	return nil
}
func (*monitorTestPool) Ready(types.QueueItem) bool { return false }
func (p *monitorTestPool) Cancel(owner string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.records, owner)
}
func (p *monitorTestPool) CancelPrefix(prefix string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id := range p.records {
		if strings.HasPrefix(id, prefix) {
			delete(p.records, id)
		}
	}
}
func (*monitorTestPool) Status(string) []app.PrewarmStatus { return nil }
func (*monitorTestPool) Close()                            {}
func (p *monitorTestPool) get(owner string) monitorWarmRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.records[owner]
}

func TestMonitorPreparationFollowsRevisionPauseAndStop(t *testing.T) {
	m := renderTestMonitor(t)
	// Add before attaching the pool, so the setup itself does not launch network work.
	m.AddSubscription("plan", []string{"gra"}, true, false, "", nil, nil, true, 2, "eu", []string{"ram-32g"}, "")
	pool := &monitorTestPool{records: map[string]monitorWarmRecord{}}
	m.state.OrderPrewarm = pool
	m.running = true
	sub := m.subscriptions[0]
	cfg := sub.checkConfig()
	owner := prewarmOwner(sub, cfg)
	m.syncPrewarm()
	record := pool.get(owner)
	if len(record.items) != 2 || record.active == nil || !record.active() || record.items[0].AccountID != "eu" || len(m.state.Queue) != 0 {
		t.Fatal(record)
	}
	key := OptionsKey(cfg.Options)
	if _, err := m.SetPaused("plan", &key, true); err != nil {
		t.Fatal(err)
	}
	if record.active() {
		t.Fatal("paused monitor still permits consumption")
	}
	if _, err := m.SetPaused("plan", &key, false); err != nil {
		t.Fatal(err)
	}
	if record.active() {
		t.Fatal("resume revived an obsolete cart")
	}
	newCfg := sub.checkConfig()
	if prewarmOwner(sub, newCfg) == owner {
		t.Fatal("new revision reused cancelled owner")
	}
	m.syncPrewarm()
	next := pool.get(prewarmOwner(sub, newCfg))
	if next.active == nil || !next.active() {
		t.Fatal("resume did not prepare a new revision")
	}
	m.Stop()
	if next.active() {
		t.Fatal("stopped monitor still permits consumption")
	}
	if recreated := prewarmOwner(&Subscription{PlanCode: sub.PlanCode, CreatedAt: sub.CreatedAt + "-new"}, cfg); recreated == owner {
		t.Fatal("recreated subscription reused a tombstoned owner")
	}
}
