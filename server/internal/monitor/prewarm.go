package monitor

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"time"

	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/types"
)

func prewarmOwner(sub *Subscription, cfg subCheckConfig) string {
	sum := sha256.Sum256([]byte(sub.PlanCode + "\x00" + sub.CreatedAt + "\x00" + OptionsKey(cfg.Options)))
	return fmt.Sprintf("monitor:%x:%d", sum[:12], cfg.Revision)
}

func (m *Monitor) prepareAlertTargets(sub *Subscription, cfg subCheckConfig, accountID string, options, dcs []string) {
	if m.state.OrderPrewarm == nil || !cfg.NotifyAvailable || !m.checkCurrent(sub, cfg) || accountID == "" || len(dcs) == 0 {
		return
	}
	items := make([]types.QueueItem, 0, len(dcs))
	for _, dc := range dcs {
		if len(items) >= types.MaxOrderFanout {
			break
		}
		items = append(items, types.QueueItem{AccountID: accountID, PlanCode: sub.PlanCode, Duration: cfg.Duration, Datacenter: dc, Options: append([]string(nil), options...)})
	}
	key := sha256.Sum256([]byte(OptionsKey(options)))
	_ = m.state.OrderPrewarm.Prepare(fmt.Sprintf("notify:%s:%x", prewarmOwner(sub, cfg), key[:8]), items, 2*time.Minute, func() bool { return m.checkCurrent(sub, cfg) })
}

func (m *Monitor) syncPrewarm() {
	pool := m.state.OrderPrewarm
	if pool == nil {
		return
	}
	m.subsMu.Lock()
	running := m.running
	subs := append([]*Subscription(nil), m.subscriptions...)
	m.subsMu.Unlock()
	// Pool liveness checks remove old revisions; a stale snapshot must not cancel a newer owner.
	if !running {
		return
	}
	for _, sub := range subs {
		cfg := sub.checkConfig()
		if !cfg.AutoOrder || cfg.Paused || cfg.AutoOrderAccountID == "" || !m.checkCurrent(sub, cfg) {
			continue
		}
		if len(cfg.Options) > 0 && len(cfg.Datacenters) > 0 {
			m.prepareMonitorTargets(sub, cfg, nil)
			continue
		}
		availability := catalog.CheckServerAvailabilityWithConfigs(m.state, sub.PlanCode, cfg.AutoOrderAccountID)
		m.prepareMonitorTargets(sub, cfg, availability)
	}
}

func (m *Monitor) prepareMonitorTargets(sub *Subscription, cfg subCheckConfig, availability map[string]*catalog.ConfigAvailability) {
	if m.state.OrderPrewarm == nil || !cfg.AutoOrder || cfg.Paused || cfg.AutoOrderAccountID == "" || !m.checkCurrent(sub, cfg) {
		return
	}
	var items []types.QueueItem
	quantity := cfg.Quantity
	if quantity < 1 {
		quantity = 1
	}
	if quantity > types.MaxOrderQuantity {
		quantity = types.MaxOrderQuantity
	}
	appendTargets := func(dcs, options []string) {
		ordered := append([]string(nil), dcs...)
		sort.Strings(ordered)
		for _, dc := range ordered {
			for n := 0; n < quantity && len(items) < types.MaxOrderFanout; n++ {
				items = append(items, types.QueueItem{AccountID: cfg.AutoOrderAccountID, PlanCode: sub.PlanCode, Duration: cfg.Duration, Datacenter: dc, Options: append([]string(nil), options...)})
			}
		}
	}
	if availability == nil {
		appendTargets(cfg.Datacenters, cfg.Options)
	} else {
		keys := make([]string, 0, len(availability))
		for key := range availability {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			config := availability[key]
			if config.AwaitingCatalog(sub.PlanCode) || !configMatchesFilter(cfg.Options, config.Options) {
				continue
			}
			var dcs []string
			for dc := range config.Datacenters {
				if len(cfg.Datacenters) == 0 || containsString(cfg.Datacenters, dc) {
					dcs = append(dcs, dc)
				}
			}
			appendTargets(dcs, monitoringOptions(config.Options, cfg.Options))
		}
	}
	if len(items) > 0 {
		_ = m.state.OrderPrewarm.Prepare(prewarmOwner(sub, cfg), items, 0, func() bool { return m.checkCurrent(sub, cfg) && cfg.AutoOrder })
	}
}
