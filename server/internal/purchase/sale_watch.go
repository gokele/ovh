package purchase

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/orderterm"
	"github.com/ovh-buy/server/internal/types"
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
	account   types.OVHAccount
	latest    map[string]saleSnapshot
	retryAt   time.Time
}

// One read-only offer poll per account, regardless of the number of tasks/DCs.
// Base and addon changes wake tasks; the purchase path verifies the complete configuration.
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
		watch = &accountSaleWatch{cancel: cancel, listeners: map[*saleListener]bool{}, latest: map[string]saleSnapshot{}}
		s.accounts[account] = watch
		s.wg.Add(1)
		go func() { defer s.wg.Done(); s.run(ctx, account, watch) }()
	}
	watch.listeners[listener] = true
	if snapshot := watch.latest[plan]; snapshot.fresh(watch.account, plan) {
		if _, ok := salePrice(snapshot.base["prices"], duration, ""); ok {
			listener.changes <- struct{}{}
		}
	}
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

func (s *saleWatcher) publish(watch *accountSaleWatch, offers []map[string]interface{}, addons ...map[string][]map[string]interface{}) {
	var options map[string][]map[string]interface{}
	if len(addons) > 0 {
		options = addons[0]
	}
	s.publishSelected(watch, offers, options, "")
}

func (s *saleWatcher) publishSelected(watch *accountSaleWatch, offers []map[string]interface{}, addons map[string][]map[string]interface{}, onlyPlan string) {
	byPlan := make(map[string]map[string]interface{}, len(offers))
	for _, offer := range offers {
		if plan, ok := offer["planCode"].(string); ok {
			byPlan[plan] = offer
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if watch.latest == nil {
		watch.latest = map[string]saleSnapshot{}
	}
	for listener := range watch.listeners {
		if onlyPlan != "" && listener.plan != onlyPlan {
			continue
		}
		offer := byPlan[listener.plan]
		var options []map[string]interface{}
		if addons != nil {
			options = addons[listener.plan]
		}
		watch.latest[listener.plan] = saleSnapshot{account: watch.account, plan: listener.plan, base: offer, options: options, at: time.Now()}
		if _, ok := salePrice(offer["prices"], listener.duration, ""); !ok {
			listener.last = [32]byte{}
			continue
		}
		encoded, _ := json.Marshal([]interface{}{offer["prices"], options})
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

// publishBaseOpen 只处理「上一轮所选周期没有价格、这一轮有了」的型号:记录不含选项的快照并唤醒。
// 已开售型号不受影响,避免把已有的选项报价覆盖成空,也不会每轮重复唤醒。
// 不含选项的快照 ready() 恒为 false,所以这一步只会建车,不会结账。
func (s *saleWatcher) publishBaseOpen(watch *accountSaleWatch, offers []map[string]interface{}) {
	byPlan := make(map[string]map[string]interface{}, len(offers))
	for _, offer := range offers {
		if plan, ok := offer["planCode"].(string); ok {
			byPlan[plan] = offer
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if watch.latest == nil {
		watch.latest = map[string]saleSnapshot{}
	}
	for listener := range watch.listeners {
		offer := byPlan[listener.plan]
		if _, open := salePrice(offer["prices"], listener.duration, ""); !open {
			continue
		}
		previous := watch.latest[listener.plan]
		if _, wasOpen := salePrice(previous.base["prices"], listener.duration, ""); wasOpen {
			continue
		}
		watch.latest[listener.plan] = saleSnapshot{account: watch.account, plan: listener.plan, base: offer, at: time.Now()}
		select {
		case listener.changes <- struct{}{}:
		default:
		}
	}
}

func (s *saleWatcher) latest(account, plan string) saleSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if watch := s.accounts[account]; watch != nil {
		snapshot := watch.latest[plan]
		snapshot.retryAt = watch.retryAt
		return snapshot
	}
	return saleSnapshot{}
}

func (s *saleWatcher) pollOptions(ctx context.Context, probe *saleProbe, watch *accountSaleWatch, offers []map[string]interface{}) (map[string][]map[string]interface{}, time.Duration, error) {
	s.mu.Lock()
	plans := map[string]bool{}
	for listener := range watch.listeners {
		for _, offer := range offers {
			if offer["planCode"] == listener.plan {
				if _, ok := salePrice(offer["prices"], listener.duration, ""); ok {
					plans[listener.plan] = true
				}
				break
			}
		}
	}
	s.mu.Unlock()
	result := map[string][]map[string]interface{}{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	var retryAfter time.Duration
	limit := make(chan struct{}, 4)
	// go-ovh NewRequest mutates http.Client.Timeout. Finish signing before any parallel Do calls.
	requests := map[string]*http.Request{}
	for plan := range plans {
		req, err := probe.session.client.NewRequest(http.MethodGet, "/order/cart/"+probe.cart.id+"/eco/options?planCode="+url.QueryEscape(plan), nil, true)
		if err != nil {
			return result, 0, err
		}
		requests[plan] = req
	}
	for plan, req := range requests {
		wg.Add(1)
		go func(plan string, req *http.Request) {
			defer wg.Done()
			select {
			case limit <- struct{}{}:
				defer func() { <-limit }()
			case <-ctx.Done():
				return
			}
			var options []map[string]interface{}
			after, err := sendSaleRequest(ctx, probe.session.client, req, &options)
			mu.Lock()
			result[plan] = options
			if err != nil {
				result[plan] = nil
				var apiErr *ovhsdk.APIError
				limited := after > 0 || (errors.As(err, &apiErr) && apiErr.Code == http.StatusTooManyRequests)
				if limited {
					if firstErr == nil {
						firstErr = err
					}
					if after > retryAfter {
						retryAfter = after
					}
					s.mu.Lock()
					watch.retryAt = time.Now().Add(saleRetryDelay(1, after))
					s.mu.Unlock()
				}
			}
			mu.Unlock()
			// Publish each model independently: a slow or unopened addon API must not delay another model.
			if err == nil {
				s.publishSelected(watch, offers, map[string][]map[string]interface{}{plan: options}, plan)
			} else {
				s.publishSelected(watch, offers, map[string][]map[string]interface{}{plan: nil}, plan)
			}
		}(plan, req)
	}
	wg.Wait()
	if ctx.Err() != nil && firstErr == nil {
		firstErr = ctx.Err()
	}
	return result, retryAfter, firstErr
}

func (s *saleWatcher) run(ctx context.Context, account string, watch *accountSaleWatch) {
	probe := saleProbe{state: s.state, account: account}
	defer probe.close()
	failures := 0
	for ctx.Err() == nil {
		pollCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		offers, retryAfter, err := probe.poll(pollCtx)
		var options map[string][]map[string]interface{}
		if err == nil {
			s.mu.Lock()
			watch.account = probe.session.account
			s.mu.Unlock()
			// 基础商品刚开售就先唤醒任务去建车(加基础商品、机房/系统/区域配置),
			// 与下面的选项报价读取并行,而不是等全部选项价格回来再串行建车。
			s.publishBaseOpen(watch, offers)
			options, retryAfter, err = s.pollOptions(pollCtx, &probe, watch, offers)
		}
		cancel()
		delay := salePollInterval
		if err == nil {
			failures = 0
			s.mu.Lock()
			watch.account = probe.session.account
			watch.retryAt = time.Time{}
			s.mu.Unlock()
			s.publish(watch, offers, options)
		} else {
			failures++
			delay = saleRetryDelay(failures, retryAfter)
			s.mu.Lock()
			watch.retryAt = time.Now().Add(delay)
			s.mu.Unlock()
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
	return sendSaleRequest(ctx, client, req, result)
}

func sendSaleRequest(ctx context.Context, client *ovhsdk.Client, req *http.Request, result interface{}) (time.Duration, error) {
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
