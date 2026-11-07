package purchase

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/types"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

type ecoMetadata struct {
	options []map[string]interface{}
	expires time.Time
}

func optionCacheKey(item *types.QueueItem, account types.OVHAccount) string {
	return preparationKey(types.QueueItem{PlanCode: item.PlanCode, Datacenter: item.Datacenter}, account)
}

// Metadata is immutable, scoped to the exact account setup/model/DC, and short-lived.
// It never caches stock, a checkout result, or the availability of a coupon.
func ecoOptions(ctx context.Context, state *app.State, client *ovhsdk.Client, item *types.QueueItem, account types.OVHAccount, cartID string, wanted []string) ([]map[string]interface{}, bool, error) {
	if signal, ok := ctx.Value(saleSignalKey{}).(saleSnapshot); item.Preorder && ok && signal.fresh(account, item.PlanCode) && signal.options != nil {
		return signal.options, false, nil
	}
	pool, _ := state.OrderPrewarm.(*CartPool)
	key := optionCacheKey(item, account)
	if pool != nil {
		pool.mu.Lock()
		entry := pool.metadata[key]
		pool.mu.Unlock()
		valid := time.Now().Before(entry.expires)
		for _, option := range wanted {
			match, _, _ := catalog.MatchEcoOption(entry.options, option)
			if item.Preorder {
				match, _, _ = catalog.MatchPreorderEcoOption(entry.options, option)
			}
			if match == nil {
				valid = false
				break
			}
		}
		if valid {
			return entry.options, true, nil
		}
	}
	var options []map[string]interface{}
	err := client.GetWithContext(ctx, fmt.Sprintf("/order/cart/%s/eco/options?planCode=%s", cartID, url.QueryEscape(item.PlanCode)), &options)
	if err == nil && len(options) > 0 && pool != nil {
		pool.mu.Lock()
		if len(pool.metadata) >= 256 {
			for old := range pool.metadata {
				delete(pool.metadata, old)
				break
			}
		}
		pool.metadata[key] = ecoMetadata{options: options, expires: time.Now().Add(5 * time.Minute)}
		pool.mu.Unlock()
	}
	return options, false, err
}

func invalidateEcoOptions(state *app.State, item *types.QueueItem, account types.OVHAccount) {
	if pool, ok := state.OrderPrewarm.(*CartPool); ok {
		pool.mu.Lock()
		delete(pool.metadata, optionCacheKey(item, account))
		pool.mu.Unlock()
	}
}
