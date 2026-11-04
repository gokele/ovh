package db

import (
	"testing"

	"github.com/ovh-buy/server/internal/types"
)

func TestCorruptOrderConfigurationIsNotSilentlyDiscarded(t *testing.T) {
	for _, tc := range []struct{ table, field string }{
		{"queue", "options"},
		{"monitor_subscriptions", "options"},
		{"monitor_subscriptions", "datacenters"},
		{"vps_subscriptions", "datacenters"},
	} {
		t.Run(tc.table+"_"+tc.field, func(t *testing.T) {
			database, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if err := database.ReplaceQueue([]types.QueueItem{{ID: "queue", Options: []string{"ram-64g"}}}); err != nil {
				t.Fatal(err)
			}
			if err := database.UpsertMonitorSubscription(types.Subscription{PlanCode: "plan", Options: []string{"ram-64g"}, Datacenters: []string{"gra"}}); err != nil {
				t.Fatal(err)
			}
			if err := database.UpsertVPSSubscription(types.VPSSubscription{ID: "vps", PlanCode: "plan", Datacenters: []string{"gra"}}); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec("UPDATE "+tc.table+" SET "+tc.field+" = ?", `{"not":"an array"}`); err != nil {
				t.Fatal(err)
			}
			switch tc.table {
			case "queue":
				_, err = database.ListQueue()
			case "monitor_subscriptions":
				_, err = database.ListMonitorSubscriptions()
			case "vps_subscriptions":
				_, err = database.ListVPSSubscriptions()
			}
			if err == nil {
				t.Fatal("corrupt selection became empty, allowing a different configuration/datacenter")
			}
		})
	}
}
