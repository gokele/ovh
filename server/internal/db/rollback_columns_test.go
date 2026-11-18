package db

import (
	"testing"
)

// 新版本加列后回退到旧版本:旧结构体没有新列对应的字段,读取不能整条失败。
// 这里反过来模拟——给当前库的每张主表加一列当前结构体不认识的列。
func TestReadsIgnoreColumnsAddedByNewerVersion(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, table := range []string{"history", "queue", "monitor_subscriptions", "ovh_accounts"} {
		if _, err := database.Exec(`ALTER TABLE ` + table + ` ADD COLUMN future_col_from_newer_release TEXT NOT NULL DEFAULT 'x'`); err != nil {
			t.Fatalf("add column to %s: %v", table, err)
		}
	}
	if _, err := database.Exec(`INSERT INTO history (id, plan_code, datacenter, status, purchase_time) VALUES ('h1', 'p', 'gra', 'success', '2026-11-18T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	hist, err := database.ListHistory()
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(hist) != 1 || hist[0].ID != "h1" {
		t.Fatalf("history row lost: %+v", hist)
	}
	if _, err := database.ListQueue(); err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if _, err := database.ListMonitorSubscriptions(); err != nil {
		t.Fatalf("ListMonitorSubscriptions: %v", err)
	}
	if _, err := database.ListAccounts(); err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
}
