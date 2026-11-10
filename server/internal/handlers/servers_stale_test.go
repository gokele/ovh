package handlers

import (
	"testing"

	"github.com/ovh-buy/server/internal/types"
)

// 部分型号库存没读到时:新目录照常生效,只有失败的型号沿用旧缓存的机房列表。
func TestMergeStaleStockKeepsFreshCatalog(t *testing.T) {
	cached := []types.ServerPlan{
		{PlanCode: "a", Name: "old-a", Datacenters: []types.Datacenter{{Datacenter: "gra", Availability: "1H-low"}}},
		{PlanCode: "b", Name: "old-b", Datacenters: []types.Datacenter{{Datacenter: "rbx", Availability: "unavailable"}}},
	}
	fresh := []types.ServerPlan{
		{PlanCode: "a", Name: "new-a", StockStale: true},
		{PlanCode: "b", Name: "new-b", Datacenters: []types.Datacenter{{Datacenter: "rbx", Availability: "1H-high"}}},
		{PlanCode: "c", Name: "new-c", StockStale: true},
	}
	out := mergeStaleStock(fresh, cached)
	if out[0].Name != "new-a" || len(out[0].Datacenters) != 1 || out[0].Datacenters[0].Datacenter != "gra" || !out[0].StockStale {
		t.Fatalf("failed plan should keep new metadata and old stock: %+v", out[0])
	}
	if out[1].Datacenters[0].Availability != "1H-high" {
		t.Fatalf("plan with fresh stock must not be overwritten: %+v", out[1])
	}
	if len(out[2].Datacenters) != 0 {
		t.Fatalf("new plan without cache stays empty: %+v", out[2])
	}
}
