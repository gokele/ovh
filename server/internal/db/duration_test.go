package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ovh-buy/server/internal/types"
)

func TestDatabasePathsWithURICharactersRemainIsolated(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"orders", "orders#annual一年%"} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		database, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		var previous string
		if found, err := database.GetKV("period-test", &previous); found || err != nil {
			t.Fatal("different directories shared a database", previous, err)
		}
		if err := database.SetKV("period-test", name); err != nil {
			t.Fatal(err)
		}
		_ = database.Close()
		if _, err := os.Stat(filepath.Join(dir, "sniper.db")); err != nil {
			t.Fatal("database written outside selected directory", err)
		}
	}
}

func TestPurchasePeriodsSurviveRestartAndUpsert(t *testing.T) {
	dir := t.TempDir()
	database, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceQueue([]types.QueueItem{{ID: "annual", PlanCode: "plan", Duration: "P12M", CreatedAt: "now"}, {ID: "legacy", PlanCode: "plan", CreatedAt: "now"}}); err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceHistory([]types.PurchaseHistoryEntry{{ID: "history", PlanCode: "plan", Duration: "P24M", PurchaseTime: "now"}}); err != nil {
		t.Fatal(err)
	}
	sub := types.Subscription{PlanCode: "plan", Duration: "P3M", CreatedAt: "now"}
	if err := database.UpsertMonitorSubscription(sub); err != nil {
		t.Fatal(err)
	}
	sub.Duration = "P12M"
	if err := database.UpsertMonitorSubscription(sub); err != nil {
		t.Fatal(err)
	}
	vps := types.VPSSubscription{ID: "vps", PlanCode: "vps", Duration: "P6M", CreatedAt: "now"}
	if err := database.UpsertVPSSubscription(vps); err != nil {
		t.Fatal(err)
	}
	vps.Duration = "P24M"
	if err := database.UpsertVPSSubscription(vps); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()
	database, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	items, err := database.ListQueue()
	if err != nil || len(items) != 2 || items[0].Duration != "P12M" || items[1].Duration != "" {
		t.Fatal(items, err)
	}
	history, err := database.ListHistory()
	if err != nil || len(history) != 1 || history[0].Duration != "P24M" {
		t.Fatal(history, err)
	}
	subs, err := database.ListMonitorSubscriptions()
	if err != nil || len(subs) != 1 || subs[0].Duration != "P12M" {
		t.Fatal(subs, err)
	}
	vpsSubs, err := database.ListVPSSubscriptions()
	if err != nil || len(vpsSubs) != 1 || vpsSubs[0].Duration != "P24M" {
		t.Fatal(vpsSubs, err)
	}
}

func TestDurationMigrationPreservesExistingTasks(t *testing.T) {
	dir := t.TempDir()
	database, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceQueue([]types.QueueItem{{ID: "old", PlanCode: "plan", CreatedAt: "now"}}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"queue", "history", "monitor_subscriptions", "vps_subscriptions"} {
		if _, err := database.Exec("ALTER TABLE " + table + " DROP COLUMN duration"); err != nil {
			t.Fatal(err)
		}
	}
	_ = database.Close()
	database, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	items, err := database.ListQueue()
	if err != nil || len(items) != 1 || items[0].ID != "old" || items[0].Duration != "" {
		t.Fatal(items, err)
	}
}
