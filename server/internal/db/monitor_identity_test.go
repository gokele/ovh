package db

import (
	"github.com/ovh-buy/server/internal/types"
	"testing"
)

func TestMonitorIdentityMigrationPreservesLegacyAndAllowsConfigurations(t *testing.T) {
	dir := t.TempDir()
	database, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the legacy one-row-per-model table, including noncanonical option order.
	if _, err = database.Exec(`DROP TABLE monitor_subscriptions;
 CREATE TABLE monitor_subscriptions (
 plan_code TEXT PRIMARY KEY, created_at TEXT NOT NULL,
 datacenters TEXT NOT NULL DEFAULT '[]',notify_available INTEGER NOT NULL DEFAULT 1,
 notify_unavailable INTEGER NOT NULL DEFAULT 0,last_status TEXT NOT NULL DEFAULT '{}',
 history TEXT NOT NULL DEFAULT '[]',server_name TEXT NOT NULL DEFAULT '',auto_order INTEGER NOT NULL DEFAULT 0,
 quantity INTEGER NOT NULL DEFAULT 1, auto_order_account_id TEXT NOT NULL DEFAULT '',auto_pay INTEGER NOT NULL DEFAULT 0,
 paused INTEGER NOT NULL DEFAULT 0,options TEXT NOT NULL DEFAULT '[]');
 INSERT INTO monitor_subscriptions(plan_code,created_at,paused,options,last_status) VALUES('plan','2026-10-23',1,'["storage","ram-32g"]','{"gra":"unavailable"}');`); err != nil {
		t.Fatal(err)
	}
	database.Close()
	database, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	got, err := database.ListMonitorSubscriptions()
	if err != nil || len(got) != 1 || !got[0].Paused || got[0].LastStatus["gra"] != "unavailable" {
		t.Fatalf("migration lost legacy state: %+v %v", got, err)
	}
	// Upsert in reversed option order must update the same configuration.
	sub := got[0]
	sub.Options = []string{"storage", "ram-32g"}
	sub.Datacenters = []string{"rbx"}
	if err = database.UpsertMonitorSubscription(sub); err != nil {
		t.Fatal(err)
	}
	if err = database.UpsertMonitorSubscription(types.Subscription{PlanCode: "plan", CreatedAt: "2026-10-23", Options: []string{"ram-64g"}}); err != nil {
		t.Fatal(err)
	}
	got, err = database.ListMonitorSubscriptions()
	if err != nil || len(got) != 2 {
		t.Fatalf("configuration identity incorrect: %+v %v", got, err)
	}
	for _, s := range got {
		if s.Paused && (len(s.Datacenters) != 1 || s.Datacenters[0] != "rbx") {
			t.Fatalf("paused upsert lost changes: %+v", s)
		}
	}
	// A second startup must not rebuild or discard either sibling.
	database.Close()
	database, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	got, err = database.ListMonitorSubscriptions()
	if err != nil || len(got) != 2 {
		t.Fatalf("identity migration not idempotent: %+v %v", got, err)
	}
}
