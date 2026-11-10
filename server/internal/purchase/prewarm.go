package purchase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/orderterm"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/price"
	"github.com/ovh-buy/server/internal/types"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

const maxPrewarmCarts = 128

type warmEntry struct {
	owner, key   string
	item         types.QueueItem
	account      types.OVHAccount
	lease, retry time.Time
	active       func() bool
	cancel       context.CancelFunc
	cart         *preparedCart
	building     bool
	consumed     bool
	standby      bool
	err          string
}

// CartPool holds only unsubmitted carts; each prepared checkout can be taken once.
type CartPool struct {
	state     *app.State
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	entries   map[string][]*warmEntry
	cancelled map[string]time.Time
	metadata  map[string]ecoMetadata
	closed    bool
	workers   chan struct{}
	wg        sync.WaitGroup
}

func NewCartPool(state *app.State) *CartPool {
	ctx, cancel := context.WithCancel(context.Background())
	p := &CartPool{state: state, ctx: ctx, cancel: cancel, entries: map[string][]*warmEntry{}, cancelled: map[string]time.Time{}, metadata: map[string]ecoMetadata{}, workers: make(chan struct{}, 8)}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.maintain()
			}
		}
	}()
	return p
}

func preparationKey(item types.QueueItem, account types.OVHAccount) string {
	options := append([]string(nil), item.Options...)
	sort.Strings(options)
	data, _ := json.Marshal([]interface{}{account.ID, account.Endpoint, account.Zone, account.AppKey, account.AppSecret, account.ConsumerKey, account.ProxyURL, account.Fingerprint, item.PlanCode, normalizedDuration(item.Duration), ovh.ConvertDisplayDCToAPIDC(item.Datacenter), options, strings.TrimSpace(item.Coupon), item.Preorder})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (p *CartPool) Prepare(owner string, items []types.QueueItem, lease time.Duration, active func() bool) error {
	if owner == "" || len(owner) > 200 || len(items) == 0 || len(items) > types.MaxOrderFanout || lease < 0 || lease > 45*time.Minute {
		return fmt.Errorf("invalid preparation request")
	}
	if active != nil && !active() {
		return fmt.Errorf("preparation is no longer active")
	}
	next := make([]*warmEntry, 0, len(items))
	for _, item := range items {
		duration, err := orderterm.Normalize(item.Duration)
		if err != nil {
			return err
		}
		item.Duration = duration
		account, ok := p.state.FindAccount(item.AccountID)
		if !ok || item.AccountID == "" || strings.TrimSpace(item.PlanCode) == "" || strings.TrimSpace(item.Datacenter) == "" {
			return fmt.Errorf("preparation requires an account, model and datacenter")
		}
		item.Options = append([]string(nil), item.Options...)
		item.Datacenter = ovh.ConvertDisplayDCToAPIDC(item.Datacenter)
		entry := &warmEntry{owner: owner, key: preparationKey(item, account), item: item, account: account, active: active, standby: lease > 2*time.Minute && strings.HasPrefix(owner, "web:")}
		if lease > 0 {
			entry.lease = time.Now().Add(lease)
		}
		next = append(next, entry)
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("preparation pool is closed")
	}
	if until, ok := p.cancelled[owner]; ok && time.Now().Before(until) {
		p.mu.Unlock()
		return fmt.Errorf("preparation was cancelled")
	}
	total := len(next)
	for id, list := range p.entries {
		if id != owner {
			total += len(list)
		}
	}
	if total > maxPrewarmCarts {
		p.mu.Unlock()
		return fmt.Errorf("preparation pool is full")
	}
	previous := p.entries[owner]
	var discard []*preparedCart
	for i, entry := range next {
		for j, old := range previous {
			if old != nil && !old.consumed && old.key == entry.key {
				if old.standby {
					entry.standby = true
					if entry.lease.Before(old.lease) {
						entry.lease = old.lease
					}
				}
				old.lease, old.active = entry.lease, active
				old.standby = entry.standby
				next[i] = old
				previous[j] = nil
				break
			}
		}
	}
	for _, old := range previous {
		if old != nil {
			if old.cancel != nil {
				old.cancel()
			}
			if old.cart != nil {
				discard = append(discard, old.cart)
			}
		}
	}
	p.entries[owner] = next
	for _, entry := range next {
		p.startLocked(entry)
	}
	p.mu.Unlock()
	go cleanupPreparedCarts(discard)
	return nil
}

func (p *CartPool) startLocked(entry *warmEntry) {
	if p.closed || entry.building || entry.consumed || (entry.cart != nil && time.Until(entry.cart.expires) > 30*time.Second) || time.Now().Before(entry.retry) {
		return
	}
	ctx, cancel := context.WithTimeout(p.ctx, 90*time.Second)
	entry.cancel, entry.building = cancel, true
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer cancel()
		defer func() {
			p.mu.Lock()
			entry.building = false
			if ctx.Err() != nil {
				entry.err = ctx.Err().Error()
				entry.retry = time.Now().Add(30 * time.Second)
				if entry.item.Preorder {
					entry.retry = time.Now().Add(catalog.PreorderCheckInterval)
				}
			}
			p.mu.Unlock()
		}()
		select {
		case p.workers <- struct{}{}:
			defer func() { <-p.workers }()
		case <-ctx.Done():
			return
		}
		cart, err := p.assemble(ctx, entry)
		p.mu.Lock()
		present := false
		for _, live := range p.entries[entry.owner] {
			if live == entry {
				present = true
				break
			}
		}
		var previous *preparedCart
		if present && !p.closed && ctx.Err() == nil {
			if err != nil {
				entry.err = ovh.Explain(err)
				entry.retry = time.Now().Add(30 * time.Second)
				if entry.item.Preorder {
					entry.retry = time.Now().Add(catalog.PreorderCheckInterval)
				}
			} else {
				previous, entry.cart = entry.cart, cart
				cart = nil
				entry.err = ""
			}
		}
		p.mu.Unlock()
		if cart != nil {
			cart.cleanup()
		}
		if previous != nil {
			previous.cleanup()
		}
	}()
}

func (p *CartPool) assemble(ctx context.Context, entry *warmEntry) (*preparedCart, error) {
	if !p.valid(entry) {
		return nil, fmt.Errorf("preparation is no longer active")
	}
	item := entry.item
	item.ID = "prewarm:" + entry.key
	client, err := p.state.OVH.IsolatedClientForAccount(entry.account)
	if err != nil {
		return nil, err
	}
	options := append([]string(nil), item.Options...)
	wantedPreorder := options
	if item.Preorder {
		wantedPreorder, options, err = catalog.ResolvePreorderOptions(p.state, item.AccountID, item.PlanCode, item.Options)
		if err != nil {
			return nil, err
		}
	}
	preorderOrder := append([]string(nil), options...)
	fqn := ""
	stockAvailable := false
	{
		observation, err := p.state.StockSignals.Fetch(ctx, item.AccountID, item.PlanCode, func() ([]map[string]interface{}, error) {
			return ovh.GetAvailabilities(ctx, client, item.PlanCode)
		})
		if err != nil {
			return nil, err
		}
		variants := observation.Variants
		wanted := fqnRelevantOptions(variants, item.Options)
		if item.Preorder {
			wanted = wantedPreorder
		}
		for _, variant := range variants {
			value, _ := variant["fqn"].(string)
			matches := fqnCoversOptions(value, wanted)
			if item.Preorder {
				matches = preorderFQNCoversOptions(value, wanted) && strings.EqualFold(strings.SplitN(value, ".", 2)[0], item.PlanCode)
			}
			if value == "" || !matches {
				continue
			}
			dcs, _ := variant["datacenters"].([]interface{})
			for _, raw := range dcs {
				dc, _ := raw.(map[string]interface{})
				if dc["datacenter"] == item.Datacenter {
					fqn = value
					availability, _ := dc["availability"].(string)
					stockAvailable = catalog.IsAvailableForOrder(availability)
					break
				}
			}
			if fqn != "" {
				break
			}
		}
	}
	if fqn == "" {
		return nil, fmt.Errorf("selected hardware/datacenter is not present in the stock feed yet")
	}
	if item.Preorder {
		options = preorderConfigurationOptions(fqn, wantedPreorder, options)
	} else {
		options = configurationOptions(fqn, options)
	}
	cart, out := prepareCart(ctx, p.state, &item, entry.account, client, options, item.Datacenter, fqn, newTimeline(), "", true)
	if cart == nil {
		if out.Reason == "" {
			out.Reason = "cart preparation failed"
		}
		return nil, fmt.Errorf("%s", out.Reason)
	}
	cart.stockOptions = append([]string(nil), wantedPreorder...)
	cart.orderOptions = preorderOrder
	var summary struct {
		Prices struct {
			WithTax struct {
				Value    *float64 `json:"value"`
				Currency string   `json:"currencyCode"`
			} `json:"withTax"`
		} `json:"prices"`
	}
	err = client.GetWithContext(ctx, "/order/cart/"+cart.id+"/summary", &summary)
	estimated := false
	if err != nil {
		// A stock-related summary rejection must not discard a fully assembled standby cart.
		var apiErr *ovhsdk.APIError
		stockRejection := false
		if errors.As(err, &apiErr) && (apiErr.Code == 400 || apiErr.Code == 409) {
			message := strings.ToLower(apiErr.Message)
			stockRejection = strings.Contains(message, "not available") || strings.Contains(message, "out of stock") || strings.Contains(message, "no stock")
		}
		if !stockAvailable && stockRejection && ctx.Err() == nil {
			quote, quoteErr := catalog.PriceForOptions(p.state, item.AccountID, item.PlanCode, cart.options)
			if quoteErr == nil && !quote.Partial && cart.quote.PricingMode == "default" && cart.duration == "P1M" {
				amount := quote.Monthly + quote.Install
				summary.Prices.WithTax.Value = &amount
				summary.Prices.WithTax.Currency = quote.Currency
				estimated = true
			}
		}
		if !estimated {
			cart.cleanup()
			return nil, err
		}
	}
	value := summary.Prices.WithTax.Value
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value <= 0 || summary.Prices.WithTax.Currency == "" {
		cart.cleanup()
		return nil, fmt.Errorf("cart has no confirmed price")
	}
	if !p.valid(entry) {
		cart.cleanup()
		return nil, context.Canceled
	}
	cart.quote.Prices = map[string]interface{}{"withTax": *value, "currencyCode": summary.Prices.WithTax.Currency}
	if estimated {
		cart.quote.Prices["estimated"] = true
	}
	cart.quote.Items = []map[string]interface{}{}
	return cart, nil
}

func (p *CartPool) valid(entry *warmEntry) bool {
	p.mu.Lock()
	active := entry.active
	p.mu.Unlock()
	if active != nil && !active() {
		return false
	}
	account, ok := p.state.FindAccount(entry.account.ID)
	return ok && preparationKey(entry.item, account) == entry.key
}

func (p *CartPool) matching(item types.QueueItem, take bool) *preparedCart {
	account, ok := p.state.FindAccount(item.AccountID)
	if !ok {
		return nil
	}
	key := preparationKey(item, account)
	p.mu.Lock()
	var candidates []*warmEntry
	for _, list := range p.entries {
		for _, entry := range list {
			if entry.key == key && entry.cart != nil {
				candidates = append(candidates, entry)
			}
		}
	}
	p.mu.Unlock()
	for _, entry := range candidates {
		if !p.valid(entry) {
			continue
		}
		p.mu.Lock()
		if entry.cart == nil || entry.cart.duration == "" || !orderterm.Same(entry.cart.duration, item.Duration) || !time.Now().Before(entry.cart.expires) || (!entry.lease.IsZero() && !time.Now().Before(entry.lease)) {
			p.mu.Unlock()
			continue
		}
		present := false
		for _, live := range p.entries[entry.owner] {
			if live == entry {
				present = true
				break
			}
		}
		if !present {
			p.mu.Unlock()
			continue
		}
		cart := entry.cart
		if take {
			entry.cart = nil
			entry.consumed = true
			list := p.entries[entry.owner]
			for i, live := range list {
				if live == entry {
					p.entries[entry.owner] = append(list[:i:i], list[i+1:]...)
					break
				}
			}
			if len(p.entries[entry.owner]) == 0 {
				delete(p.entries, entry.owner)
			}
		}
		p.mu.Unlock()
		return cart
	}
	return nil
}

func (p *CartPool) Ready(item types.QueueItem) bool         { return p.matching(item, false) != nil }
func (p *CartPool) Take(item types.QueueItem) *preparedCart { return p.matching(item, true) }

func (p *CartPool) Quote(item types.QueueItem) *price.PriceInfo {
	if cart := p.matching(item, false); cart != nil {
		return cart.quote
	}
	return nil
}

func (p *CartPool) Cancel(owner string) {
	p.mu.Lock()
	carts := p.cancelLocked(owner, true)
	p.mu.Unlock()
	go cleanupPreparedCarts(carts)
}

func (p *CartPool) cancelLocked(owner string, tombstone bool) []*preparedCart {
	entries := p.entries[owner]
	delete(p.entries, owner)
	if tombstone {
		if len(p.cancelled) >= 4096 {
			for key := range p.cancelled {
				delete(p.cancelled, key)
				break
			}
		}
		p.cancelled[owner] = time.Now().Add(10 * time.Minute)
	}
	var carts []*preparedCart
	for _, entry := range entries {
		if entry.cancel != nil {
			entry.cancel()
		}
		if entry.cart != nil {
			carts = append(carts, entry.cart)
			entry.cart = nil
		}
	}
	return carts
}

func (p *CartPool) cancelCurrent(entry *warmEntry) {
	p.mu.Lock()
	var carts []*preparedCart
	for _, live := range p.entries[entry.owner] {
		if live == entry {
			carts = p.cancelLocked(entry.owner, false)
			break
		}
	}
	p.mu.Unlock()
	go cleanupPreparedCarts(carts)
}

func (p *CartPool) CancelPrefix(prefix string) {
	p.mu.Lock()
	var ids []string
	for owner := range p.entries {
		if strings.HasPrefix(owner, prefix) {
			ids = append(ids, owner)
		}
	}
	p.mu.Unlock()
	for _, id := range ids {
		p.Cancel(id)
	}
}

func (p *CartPool) maintain() {
	p.mu.Lock()
	var entries []*warmEntry
	for _, list := range p.entries {
		entries = append(entries, list...)
	}
	for owner, until := range p.cancelled {
		if time.Now().After(until) {
			delete(p.cancelled, owner)
		}
	}
	p.mu.Unlock()
	for _, entry := range entries {
		if !p.valid(entry) {
			p.cancelCurrent(entry)
			continue
		}
		p.mu.Lock()
		if !entry.lease.IsZero() && time.Now().After(entry.lease) {
			p.mu.Unlock()
			p.cancelCurrent(entry)
			continue
		}
		if entry.cart != nil && !time.Now().Before(entry.cart.expires) {
			cart := entry.cart
			entry.cart = nil
			go cart.cleanup()
		}
		present := false
		for _, live := range p.entries[entry.owner] {
			if live == entry {
				present = true
				break
			}
		}
		if present {
			p.startLocked(entry)
		}
		p.mu.Unlock()
	}
}

func (p *CartPool) Status(owner string) []app.PrewarmStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := []app.PrewarmStatus{}
	for id, list := range p.entries {
		if owner != "" && id != owner {
			continue
		}
		status := app.PrewarmStatus{ID: id, State: "preparing", Datacenters: []string{}}
		dcs := map[string]bool{}
		for _, entry := range list {
			status.AccountID, status.PlanCode = entry.item.AccountID, entry.item.PlanCode
			status.Duration = entry.item.Duration
			status.Standby = entry.standby
			status.Total++
			if !dcs[entry.item.Datacenter] {
				dcs[entry.item.Datacenter] = true
				status.Datacenters = append(status.Datacenters, entry.item.Datacenter)
			}
			if entry.cart != nil && time.Now().Before(entry.cart.expires) {
				status.Ready++
				if entry.cart.quote.Prices["estimated"] == true {
					status.Estimated = true
				}
			}
			if !entry.lease.IsZero() {
				status.ExpiresAt = entry.lease.UTC().Format(time.RFC3339)
			}
			if entry.err != "" {
				status.Error = entry.err
			}
		}
		if status.Ready == status.Total {
			status.State = "ready"
		} else if status.Error != "" && status.Ready == 0 {
			status.State = "waiting"
		}
		result = append(result, status)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (p *CartPool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.cancel()
	var carts []*preparedCart
	for _, list := range p.entries {
		for _, entry := range list {
			if entry.cancel != nil {
				entry.cancel()
			}
			if entry.cart != nil {
				carts = append(carts, entry.cart)
			}
		}
	}
	p.entries = map[string][]*warmEntry{}
	p.mu.Unlock()
	p.wg.Wait()
	cleanupPreparedCarts(carts)
}

func cleanupPreparedCarts(carts []*preparedCart) {
	if len(carts) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, cart := range carts {
		wg.Add(1)
		go func(cart *preparedCart) {
			defer wg.Done()
			_ = cart.client.DeleteWithContext(ctx, "/order/cart/"+cart.id, nil)
		}(cart)
	}
	wg.Wait()
}

func normalizedDuration(raw string) string { d, _ := orderterm.Normalize(raw); return d }
