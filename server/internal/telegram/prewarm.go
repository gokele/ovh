package telegram

import (
	"sort"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

// PrewarmOrder prepares only; the confirmation handler remains the sole enqueue point.
func PrewarmOrder(state *app.State, owner, accountID, plan, dc string, quantity int, options []string, lease time.Duration, duration string) {
	if state.OrderPrewarm == nil || accountID == "" {
		return
	}
	if quantity < 1 {
		quantity = 1
	}
	if quantity > types.MaxOrderQuantity {
		quantity = types.MaxOrderQuantity
	}
	availability := catalog.CheckServerAvailabilityWithConfigs(state, plan, accountID)
	keys := make([]string, 0, len(availability))
	for key := range availability {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var items []types.QueueItem
	for _, key := range keys {
		config := availability[key]
		if config.AwaitingCatalog(plan) || !subset(options, config.Options) {
			continue
		}
		var dcs []string
		for candidate := range config.Datacenters {
			if dc == "" || candidate == ovh.ConvertDisplayDCToAPIDC(dc) {
				dcs = append(dcs, candidate)
			}
		}
		sort.Strings(dcs)
		for _, candidate := range dcs {
			for n := 0; n < quantity && len(items) < types.MaxOrderFanout; n++ {
				items = append(items, types.QueueItem{AccountID: accountID, PlanCode: plan, Duration: duration, Datacenter: candidate, Options: append([]string(nil), config.Options...)})
			}
		}
	}
	if len(items) > 0 {
		_ = state.OrderPrewarm.Prepare(owner, items, lease, nil)
	}
}
