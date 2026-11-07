package purchase

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ovh-buy/server/internal/numconv"
	"github.com/ovh-buy/server/internal/orderterm"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

type saleSignalKey struct{}

// Immutable account-scoped offers; a signal never changes the saved hardware.
type saleSnapshot struct {
	account     types.OVHAccount
	plan        string
	base        map[string]interface{}
	options     []map[string]interface{}
	at, retryAt time.Time
}

const saleQuoteMaxAge = 10 * time.Second

func (s saleSnapshot) fresh(account types.OVHAccount, plan string) bool {
	return s.plan == plan && s.account.ID == account.ID && s.account.Zone == account.Zone && ovh.SameClientConfiguration(s.account, account) &&
		!s.at.IsZero() && time.Since(s.at) >= 0 && time.Since(s.at) < saleQuoteMaxAge
}

func salePrice(raw interface{}, period, preferredMode string) (string, bool) {
	duration, mode, ok := orderterm.Pick(raw, period, preferredMode)
	if !ok {
		return "", false
	}
	rows, _ := raw.([]interface{})
	for _, row := range rows {
		p, _ := row.(map[string]interface{})
		if p["duration"] != duration || p["pricingMode"] != mode {
			continue
		}
		price, _ := p["price"].(map[string]interface{})
		value, numeric := numconv.ToFloat64(price["value"])
		currency, _ := price["currencyCode"].(string)
		if numeric && value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) && currency != "" {
			return currency, true
		}
	}
	return "", false
}

func (s saleSnapshot) ready(period, mode string, selected []string, selectedModes ...map[string]string) bool {
	if _, actual, ok := orderterm.Pick(s.base["prices"], period, mode); !ok || (mode != "" && actual != mode) {
		return false
	}
	currency, ok := salePrice(s.base["prices"], period, mode)
	if !ok {
		return false
	}
	for _, code := range selected {
		found := false
		for _, option := range s.options {
			pc, _ := option["planCode"].(string)
			if !strings.EqualFold(code, pc) {
				continue
			}
			wantedMode := mode
			if len(selectedModes) > 0 {
				wantedMode = selectedModes[0][code]
			}
			_, actual, termOK := orderterm.Pick(option["prices"], period, wantedMode)
			c, valid := salePrice(option["prices"], period, wantedMode)
			valid = valid && termOK && (wantedMode == "" || actual == wantedMode)
			found = valid && c == currency
			break
		}
		if !found {
			return false
		}
	}
	return true
}

func readSaleSnapshot(ctx context.Context, client *ovhsdk.Client, account types.OVHAccount, cartID, plan string) (saleSnapshot, error) {
	snapshot := saleSnapshot{account: account, plan: plan}
	var offers []map[string]interface{}
	after, err := saleRequest(ctx, client, http.MethodGet, "/order/cart/"+cartID+"/eco", nil, &offers)
	if err != nil {
		snapshot.retryAt = time.Now().Add(after)
		return snapshot, err
	}
	for _, offer := range offers {
		if offer["planCode"] == plan {
			snapshot.base = offer
			break
		}
	}
	if snapshot.base == nil {
		snapshot.at = time.Now()
		return snapshot, nil
	}
	after, err = saleRequest(ctx, client, http.MethodGet, "/order/cart/"+cartID+"/eco/options?planCode="+url.QueryEscape(plan), nil, &snapshot.options)
	if err != nil {
		snapshot.retryAt = time.Now().Add(after)
		return snapshot, err
	}
	snapshot.at = time.Now()
	return snapshot, nil
}

func preorderSaleQuote(ctx context.Context, session *cartSession, cart *preparedCart, item *types.QueueItem) (saleSnapshot, Outcome) {
	snapshot, _ := ctx.Value(saleSignalKey{}).(saleSnapshot)
	if snapshot.retryAt.After(time.Now()) {
		return snapshot, Outcome{WaitingForCatalog: true, Reason: "销售接口限流，等待重试", RetryAfter: time.Until(snapshot.retryAt)}
	}
	if !snapshot.fresh(session.account, item.PlanCode) {
		if session.sale.fresh(session.account, item.PlanCode) && session.sale.ready(cart.duration, cart.quote.PricingMode, cart.options, cart.modes) {
			snapshot = session.sale
		}
	}
	if !snapshot.fresh(session.account, item.PlanCode) {
		if session.sale.retryAt.After(time.Now()) {
			return session.sale, Outcome{WaitingForCatalog: true, Reason: "销售接口限流，等待重试", RetryAfter: time.Until(session.sale.retryAt)}
		}
		var err error
		quoteCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		snapshot, err = readSaleSnapshot(quoteCtx, session.client, session.account, cart.id, item.PlanCode)
		cancel()
		session.sale = snapshot
		if err != nil {
			if ctx.Err() != nil {
				return snapshot, Outcome{Cancelled: true}
			}
			return snapshot, Outcome{WaitingForCatalog: true, Reason: fmt.Sprintf("等待主机与所选配置报价: %s", ovh.Explain(err)), RetryAfter: time.Until(snapshot.retryAt)}
		}
	}
	if !snapshot.ready(cart.duration, cart.quote.PricingMode, cart.options, cart.modes) {
		rebuild := false
		if _, known := salePrice(snapshot.base["prices"], cart.duration, ""); known {
			_, mode, _ := orderterm.Pick(snapshot.base["prices"], cart.duration, cart.quote.PricingMode)
			rebuild = mode != cart.quote.PricingMode
		}
		for _, option := range snapshot.options {
			code, _ := option["planCode"].(string)
			if wanted := cart.modes[code]; wanted != "" {
				if _, known := salePrice(option["prices"], cart.duration, ""); known {
					_, mode, _ := orderterm.Pick(option["prices"], cart.duration, wanted)
					rebuild = rebuild || mode != wanted
				}
			}
		}
		return snapshot, Outcome{WaitingForCatalog: true, RetryImmediately: rebuild, Reason: "等待主机与全部所选配置的价格和购买周期开放"}
	}
	return snapshot, Outcome{}
}
