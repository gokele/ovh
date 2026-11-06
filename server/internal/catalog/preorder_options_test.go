package catalog

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
)

const preorderOptionsFixture = `{"locale":{"currencyCode":"EUR"},"plans":[{
 "planCode":"27skle03-v1-mum","configurations":[{"name":"dedicated_datacenter","values":["ynm"]}],
 "addonFamilies":[
  {"name":"memory","mandatory":true,"default":"memory-opaque","addons":["memory-opaque","ram-64g-ddr4-ecc-2933-27skle03-v1"]},
  {"name":"storage","mandatory":true,"default":"disk-opaque","addons":["disk-opaque","softraid-2x480ssd-2x8000sa-27skle03-v1"]},
  {"name":"bandwidth","mandatory":true,"default":"traffic-default","addons":["traffic-default"]},
  {"name":"vrack","mandatory":false,"default":"vrack-default","addons":["vrack-default"]},
  {"name":"gpu","mandatory":false,"addons":["gpu-opaque"]}, {"name":"cpu","mandatory":false,"addons":["fixed-cpu-old"]}]
 }],"addons":[
 {"planCode":"memory-opaque","product":"ram-64g-ddr4-ecc-2400"},
 {"planCode":"ram-64g-ddr4-ecc-2933-27skle03-v1","product":"ram-64g-ddr4-ecc-2933"},
 {"planCode":"disk-opaque","product":"softraid-2x480ssd"},
 {"planCode":"softraid-2x480ssd-2x8000sa-27skle03-v1","product":"softraid-2x480ssd-2x8000sa"},
 {"planCode":"traffic-default","product":"traffic-25tb"},
 {"planCode":"gpu-opaque","product":"gpu-1xradeon-rx6700xt-12g"}, {"planCode":"fixed-cpu-old","product":"old-processor"},
 {"planCode":"ram-64g-ddr4-ecc-2666-other","product":"ram-64g-ddr4-ecc-2666"}]}`

func TestPreorderResolvesExactHardwareWithoutCPUOrPrice(t *testing.T) {
	resetCatalogCaches()
	t.Cleanup(resetCatalogCaches)
	payload, err := parseEcoCatalogFull(strings.NewReader(preorderOptionsFixture))
	if err != nil {
		t.Fatal(err)
	}
	st := testState(t)
	st.Accounts = []types.OVHAccount{{ID: "preorder-options", Endpoint: "ovh-eu", Zone: "IE"}}
	regionCache["IE"] = &subsidiaryCatalog{plans: payload.plans, addons: payload.addons, fetchedAt: time.Now()}
	for _, tc := range []struct {
		name                 string
		wanted, stock, order []string
	}{
		{"raw products with regional suffix", []string{"ram-64g-ddr4-ecc-2400", "softraid-2x480ssd"}, []string{"ram-64g-ddr4-ecc-2400", "softraid-2x480ssd"}, []string{"memory-opaque", "disk-opaque", "traffic-default"}},
		{"opaque SKUs map back to stock", []string{"memory-opaque", "disk-opaque"}, []string{"ram-64g-ddr4-ecc-2400", "softraid-2x480ssd"}, []string{"memory-opaque", "disk-opaque", "traffic-default"}},
		{"CPU is not a condition", []string{"CPU-Intel-Xeon-E5-1650v4", "fixed-cpu-old", "ram-64g-ddr4-ecc-2400"}, []string{"ram-64g-ddr4-ecc-2400"}, []string{"memory-opaque", "traffic-default"}},
		{"frequency cannot change", []string{"ram-64g-ddr4-ecc-2666"}, []string{"ram-64g-ddr4-ecc-2666"}, []string{"ram-64g-ddr4-ecc-2666", "traffic-default"}},
		{"capacity cannot change", []string{"ram-32g-ddr4-ecc-2400"}, []string{"ram-32g-ddr4-ecc-2400"}, []string{"ram-32g-ddr4-ecc-2400", "traffic-default"}},
		{"complete disk layout cannot change", []string{"softraid-2x480ssd-2x9000sa"}, []string{"softraid-2x480ssd-2x9000sa"}, []string{"softraid-2x480ssd-2x9000sa", "traffic-default"}},
		{"SKU model suffix remains exact", []string{"ram-64g-ddr4-ecc-2666-27skle04-v1"}, []string{"ram-64g-ddr4-ecc-2666-27skle04-v1"}, []string{"ram-64g-ddr4-ecc-2666-27skle04-v1", "traffic-default"}},
		{"GPU remains a stock condition", []string{"gpu-opaque"}, []string{"gpu-1xradeon-rx6700xt-12g"}, []string{"gpu-opaque", "traffic-default"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stock, order, err := ResolvePreorderOptions(st, "preorder-options", "27skle03-v1-mum", tc.wanted)
			if err != nil || !reflect.DeepEqual(stock, tc.stock) || !reflect.DeepEqual(order, tc.order) {
				t.Fatalf("stock=%v order=%v err=%v", stock, order, err)
			}
		})
	}
}

func TestStockOptionsRemainUsableBeforeSalesCatalogOrDuringOutage(t *testing.T) {
	original := fetchSubsidiaryCatalog
	t.Cleanup(func() { fetchSubsidiaryCatalog = original; resetCatalogCaches() })
	for _, outage := range []bool{false, true} {
		resetCatalogCaches()
		fetchSubsidiaryCatalog = func(*app.State, string) (*subsidiaryCatalog, error) {
			if outage {
				return nil, errors.New("offline")
			}
			return &subsidiaryCatalog{plans: map[string]planConfig{}, fetchedAt: time.Now()}, nil
		}
		st := testState(t)
		st.Accounts = []types.OVHAccount{{ID: "pre", Endpoint: "ovh-eu", Zone: "IE"}}
		wanted := []string{"cpu-unpublished", "ram-32g-ddr4-ecc-2666", "hybridsoftraid-2x8000sa-2x480ssd", "bandwidth-1000"}
		stock, order, err := ResolvePreorderOptions(st, "pre", "27skle03-v1", wanted)
		if err != nil || !reflect.DeepEqual(stock, wanted[1:3]) || !reflect.DeepEqual(order, wanted[1:]) {
			t.Fatalf("outage=%t stock=%v order=%v err=%v", outage, stock, order, err)
		}
	}
}
