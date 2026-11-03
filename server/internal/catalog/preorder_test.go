package catalog

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

func TestMonitorCatalogMissRefreshesSoonWithoutRefreshingKnownPlans(t *testing.T) {
	resetCatalogCaches()
	original := fetchSubsidiaryCatalog
	t.Cleanup(func() { fetchSubsidiaryCatalog = original; resetCatalogCaches() })
	const plan = "27sysle02-v1"
	st := testState(t)
	st.Accounts = []types.OVHAccount{{ID: "issue12", Endpoint: "ovh-eu", Zone: "IE"}}
	var calls atomic.Int32
	fetchSubsidiaryCatalog = func(_ *app.State, subsidiary string) (*subsidiaryCatalog, error) {
		calls.Add(1)
		if subsidiary != "IE" {
			return nil, errors.New("wrong subsidiary")
		}
		return &subsidiaryCatalog{plans: map[string]planConfig{
			plan: {addonFamilies: map[string][]string{"memory": {"ram-128g"}}},
		}, fetchedAt: time.Now()}, nil
	}
	for _, tc := range []struct {
		name    string
		present bool
		age     time.Duration
		fetches int32
	}{
		{"recent miss is cached", false, time.Second, 0},
		{"known plan keeps normal cache", true, time.Minute, 0},
		{"missing plan is refreshed after 30 seconds", false, time.Minute, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetCatalogCaches()
			calls.Store(0)
			plans := map[string]planConfig{}
			if tc.present {
				plans[plan] = planConfig{addonFamilies: map[string][]string{"memory": {"ram-128g"}}}
			}
			regionCache["IE"] = &subsidiaryCatalog{plans: plans, fetchedAt: time.Now().Add(-tc.age)}
			var wg sync.WaitGroup
			for i := 0; i < 16; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					families, err := AddonFamiliesForPlan(st, "issue12", plan)
					if tc.present || tc.fetches > 0 {
						if err != nil || len(families["memory"]) != 1 {
							t.Errorf("listed plan was not recovered: families=%v err=%v", families, err)
						}
					} else if !errors.Is(err, ErrPlanNotInCatalog) {
						t.Errorf("recent missing plan must retain its cached result: %v", err)
					}
				}()
			}
			wg.Wait()
			if calls.Load() != tc.fetches {
				t.Fatalf("catalog requests=%d, want %d", calls.Load(), tc.fetches)
			}
		})
	}
}

func TestClassifyPrereleaseKeepsRegionAndProductGuards(t *testing.T) {
	resetCatalogCaches()
	resetAvailProbeCaches()
	originalFetch, originalProbe := fetchSubsidiaryCatalog, probeRegionHasPlan
	t.Cleanup(func() {
		fetchSubsidiaryCatalog, probeRegionHasPlan = originalFetch, originalProbe
		resetCatalogCaches()
		resetAvailProbeCaches()
	})
	fetchSubsidiaryCatalog = func(*app.State, string) (*subsidiaryCatalog, error) {
		return &subsidiaryCatalog{plans: map[string]planConfig{"24sk202": {}}, fetchedAt: time.Now()}, nil
	}
	probeRegionHasPlan = func(region, plan string) (bool, error) {
		if plan == "27skle03-v1-us" {
			return region == "US", nil
		}
		if plan == "27skle99-v1" {
			return false, nil
		}
		return region == "EU", nil
	}
	st := testState(t)
	st.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu", Zone: "IE"}}
	for _, tc := range []struct {
		plan string
		want PlanVerdict
	}{
		{"27skle03-v1", PlanVerdictPrerelease},
		{"27sysstorle01-v1", PlanVerdictPrerelease},
		{"27risele01-v1", PlanVerdictPrerelease},
		{"24sk202", PlanVerdictOK},
		{"27skle03-v1-us", PlanVerdictCrossRegion},
		{"27skle99-v1", PlanVerdictNoSuchPlan},
		{"25scale01-v1", PlanVerdictNotEco},
	} {
		got, _ := ClassifyPlan(st, "eu", tc.plan, "test")
		if got != tc.want {
			t.Errorf("%s: verdict %v, want %v", tc.plan, got, tc.want)
		}
	}
}

func TestPreorderRefreshesCatalogAndSurvivesOutage(t *testing.T) {
	resetCatalogCaches()
	original := fetchSubsidiaryCatalog
	t.Cleanup(func() { fetchSubsidiaryCatalog = original; resetCatalogCaches() })
	st := testState(t)
	st.Accounts = []types.OVHAccount{
		{ID: "eu", Endpoint: "ovh-eu", Zone: "IE"},
		{ID: "us", Endpoint: "ovh-us", Zone: "US"},
	}
	const plan = "27skle03-v1"
	regionCache["IE"] = &subsidiaryCatalog{plans: map[string]planConfig{}, fetchedAt: time.Now().Add(-time.Minute)}
	// A normal reader still uses the two-hour cache.
	if _, err := loadSubsidiaryCatalog(st, "IE"); err != nil {
		t.Fatal(err)
	}
	fetches := 0
	fetchSubsidiaryCatalog = func(_ *app.State, sub string) (*subsidiaryCatalog, error) {
		fetches++
		plans := map[string]planConfig{}
		if sub == "IE" {
			plans[plan] = planConfig{}
		}
		return &subsidiaryCatalog{plans: plans, fetchedAt: time.Now()}, nil
	}
	for i := 0; i < 2; i++ {
		if ready, err := PreorderReady(st, "eu", plan); err != nil || !ready {
			t.Fatalf("listed: %v, %v", ready, err)
		}
	}
	if fetches != 1 {
		t.Fatalf("expected one fresh fetch, got %d", fetches)
	}
	if ready, err := PreorderReady(st, "us", plan); err != nil || ready {
		t.Fatalf("wrong account catalog: %v, %v", ready, err)
	}
	regionCache["US"].fetchedAt = time.Now().Add(-time.Minute)
	fetchSubsidiaryCatalog = func(*app.State, string) (*subsidiaryCatalog, error) { return nil, errors.New("HTTP 429") }
	if ready, _ := PreorderReady(st, "us", plan); ready {
		t.Fatal("catalog outage must not permit an order")
	}
}

func TestMatchPreorderEcoOptionNeverChangesHardware(t *testing.T) {
	for _, tc := range []struct {
		wanted, available string
		match             bool
	}{
		{"ram-64g-ddr4-ecc-2666", "ram-64g-ddr4-ecc-2666-27skle03-v1", true},
		{"softraid-2x480ssd", "softraid-2x480ssd-27skle03-v1", true},
		{"softraid-2x480ssd", "softraid-2x480ssd-2x8000sa-27skle03-v1", false},
		{"ram-64g-ddr4-ecc-2666", "ram-64g-ddr4-ecc-2133-27skle03-v1", false},
		{"ram-64g-ddr4-ecc-2666", "ram-32g-ddr4-ecc-2666-27skle03-v1", false},
	} {
		option, _, _ := MatchPreorderEcoOption([]map[string]interface{}{{"planCode": tc.available}}, tc.wanted)
		if (option != nil) != tc.match {
			t.Errorf("%s -> %s: match=%v", tc.wanted, tc.available, option != nil)
		}
	}
}
