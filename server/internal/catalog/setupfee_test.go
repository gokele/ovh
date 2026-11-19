package catalog

import (
	"errors"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/types"
)

// 配置安装费 = 型号自身 + 每个附加选项的安装费(不含税 / 税额分开);目录里没有的选项不计入,记 skipped。
func TestConfigSetupFee(t *testing.T) {
	resetCatalogCaches()
	t.Cleanup(resetCatalogCaches)
	st := testState(t)
	st.Accounts = []types.OVHAccount{{ID: "fee", Endpoint: "ovh-eu", Zone: "IE"}}
	plan := []catalogPricing{
		{Capacities: []string{"renew"}, IntervalUnit: "month", Interval: 1, Mode: "default", Price: 1599000000, Tax: 367770000},
		{Capacities: []string{"installation"}, IntervalUnit: "none", Interval: 1, Mode: "default", Price: 4999000000, Tax: 1149770000},
	}
	disk := []catalogPricing{
		{Capacities: []string{"renew"}, IntervalUnit: "month", Interval: 1, Mode: "default", Price: 1000000000, Tax: 230000000},
		{Capacities: []string{"installation"}, IntervalUnit: "none", Interval: 1, Mode: "default", Price: 2000000000, Tax: 460000000},
	}
	ram := []catalogPricing{{Capacities: []string{"renew"}, IntervalUnit: "month", Interval: 1, Mode: "default", Price: 500000000}}
	regionCache["IE"] = &subsidiaryCatalog{
		plans: map[string]planConfig{"ks-le-3": {monthly: monthlyOf(plan), install: installOf(plan)}},
		addons: map[string]planConfig{
			"softraid-2x8000sa": {monthly: monthlyOf(disk), install: installOf(disk)},
			"ram-64g":           {monthly: monthlyOf(ram), install: installOf(ram)},
		},
		currency: "EUR", fetchedAt: time.Now(),
	}
	fee, cur, skipped, err := ConfigSetupFee(st, "fee", "ks-le-3", []string{"softraid-2x8000sa", "ram-64g", "ip-block-x"})
	if err != nil || cur != "EUR" || !fee.OK || skipped != 1 {
		t.Fatalf("fee=%+v cur=%s skipped=%d err=%v", fee, cur, skipped, err)
	}
	if got := round(fee.Price); got != 69.99 {
		t.Fatalf("price %v", got)
	}
	if got := round(fee.Tax); got != 16.1 {
		t.Fatalf("tax %v", got)
	}
	if _, _, _, err := ConfigSetupFee(st, "fee", "missing", nil); !errors.Is(err, ErrPlanNotInCatalog) {
		t.Fatalf("missing plan err=%v", err)
	}
}

func round(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }
