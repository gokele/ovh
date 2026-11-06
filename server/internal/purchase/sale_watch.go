package purchase

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/orderterm"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

const salePollInterval = 2 * time.Second

type saleListener struct {
	plan, duration string
	changes        chan struct{}
	last           [32]byte
}

type accountSaleWatch struct {
	cancel    context.CancelFunc
	listeners map[*saleListener]bool
}

// One read-only offer poll per account, regardless of the number of tasks/DCs.
// An offer change wakes tasks; it never authorizes checkout or replaces stock validation.
type saleWatcher struct {
	state    *app.State
	ctx      context.Context
	mu       sync.Mutex
	accounts map[string]*accountSaleWatch
	closed   bool
	wg       sync.WaitGroup
}

func newSaleWatcher(ctx context.Context, state *app.State) *saleWatcher {
	return &saleWatcher{state: state, ctx: ctx, accounts: map[string]*accountSaleWatch{}}
}

func (s *saleWatcher) subscribe(account, plan, duration string) (<-chan struct{}, func()) {
	duration, err := orderterm.Normalize(duration)
	if err != nil {
		return nil, func() {}
	}
	listener := &saleListener{plan: plan, duration: duration, changes: make(chan struct{}, 1)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil {
		return nil, func() {}
	}
	watch := s.accounts[account]
	if watch == nil {
		ctx, cancel := context.WithCancel(s.ctx)
		watch = &accountSaleWatch{cancel: cancel, listeners: map[*saleListener]bool{}}
		s.accounts[account] = watch
		s.wg.Add(1)
		go func() { defer s.wg.Done(); s.run(ctx, account, watch) }()
	}
	watch.listeners[listener] = true
	return listener.changes, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(watch.listeners, listener)
		if len(watch.listeners) == 0 {
			watch.cancel()
			if s.accounts[account] == watch {
				delete(s.accounts, account)
			}
		}
	}
}

func (s *saleWatcher) close() {
	s.mu.Lock()
	s.closed = true
	for _, watch := range s.accounts {
		watch.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *saleWatcher) publish(watch *accountSaleWatch, offers []map[string]interface{}) {
	byPlan := make(map[string]map[string]interface{}, len(offers))
	for _, offer := range offers {
		if plan, ok := offer["planCode"].(string); ok {
			byPlan[plan] = offer
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for listener := range watch.listeners {
		offer := byPlan[listener.plan]
		if _, _, ok := orderterm.Pick(offer["prices"], listener.duration, ""); !ok {
			listener.last = [32]byte{}
			continue
		}
		encoded, _ := json.Marshal(offer["prices"])
		signature := sha256.Sum256(encoded)
		if listener.last == signature {
			continue
		}
		listener.last = signature
		select {
		case listener.changes <- struct{}{}:
		default:
		}
	}
}

func (s *saleWatcher) run(ctx context.Context, account string, watch *accountSaleWatch) {
	probe := saleProbe{state: s.state, account: account}
	defer probe.close()
	failures := 0
	for ctx.Err() == nil {
		pollCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		offers, retryAfter, err := probe.poll(pollCtx)
		cancel()
		delay := salePollInterval
		if err == nil {
			failures = 0
			s.publish(watch, offers)
		} else {
			failures++
			delay = saleRetryDelay(failures, retryAfter)
			if failures == 1 {
				s.state.Logger.Debug(fmt.Sprintf("账户 %s 的预售销售探测失败，%s 后重试（任务保留原有重试）：%s", account, delay.Round(time.Millisecond), err), "purchase")
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// The discovery cart stays empty: no items, configurations, summary or checkout requests.
type saleProbe struct {
	state   *app.State
	account string
	session cartSession
	cart    *preparedCart
}

func (p *saleProbe) close() {
	if p.cart != nil {
		p.cart.cleanup()
		p.cart = nil
	}
}

func (p *saleProbe) poll(ctx context.Context) ([]map[string]interface{}, time.Duration, error) {
	client, err := p.session.clientFor(p.state, p.account)
	if err != nil {
		return nil, 0, err
	}
	client.Timeout = 8 * time.Second // Also bound the SDK's initial clock calibration, which has no context.
	if p.cart != nil && (p.cart.client != client || !time.Now().Before(p.cart.expires)) {
		p.close()
	}
	if p.cart == nil {
		var result struct {
			ID     string `json:"cartId"`
			Expire string `json:"expire"`
		}
		after, err := saleRequest(ctx, client, http.MethodPost, "/order/cart", map[string]interface{}{
			"ovhSubsidiary": orderSubsidiary(p.state, p.session.account, "purchase"),
			"description":   "OVH preorder sale discovery (empty cart)",
		}, &result)
		if err != nil {
			return nil, after, err
		}
		if result.ID == "" || strings.ContainsAny(result.ID, "/?#") {
			return nil, 0, fmt.Errorf("sale discovery cart ID is invalid")
		}
		expires := cartExpires(result.Expire)
		p.cart = &preparedCart{id: result.ID, client: client, expires: expires}
		if !expires.After(time.Now()) {
			p.close()
			return nil, 0, fmt.Errorf("sale discovery cart is expiring")
		}
		after, err = saleRequest(ctx, client, http.MethodPost, "/order/cart/"+result.ID+"/assign", nil, nil)
		if err != nil {
			p.close()
			return nil, after, err
		}
	}
	var offers []map[string]interface{}
	after, err := saleRequest(ctx, client, http.MethodGet, "/order/cart/"+p.cart.id+"/eco", nil, &offers)
	if err != nil && expiredCartError(err) {
		p.close()
	}
	return offers, after, err
}

func saleRequest(ctx context.Context, client *ovhsdk.Client, method, path string, body, result interface{}) (time.Duration, error) {
	req, err := client.NewRequest(method, path, body, true)
	if err != nil {
		return 0, err
	}
	response, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return 0, err
	}
	after := saleRetryAfter(response.Header.Get("Retry-After"), time.Now())
	return after, client.UnmarshalResponse(response, result)
}

func saleRetryAfter(raw string, now time.Time) time.Duration {
	if delay, err := time.ParseDuration(raw + "s"); err == nil && delay > 0 {
		return delay
	}
	if when, err := http.ParseTime(raw); err == nil && when.After(now) {
		return when.Sub(now)
	}
	return 0
}

func saleRetryDelay(failures int, retryAfter time.Duration) time.Duration {
	delay := salePollInterval
	for i := 1; i < failures && delay < time.Minute; i++ {
		delay *= 2
	}
	if delay > time.Minute {
		delay = time.Minute
	}
	delay += time.Duration(rand.Int64N(int64(delay / 4)))
	if retryAfter > delay {
		delay = retryAfter
	}
	return delay
}
