package config

import (
	"testing"

	"github.com/ovh-buy/server/internal/db"
)

func TestFailedSaveKeepsActiveConfiguration(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := New(database)
	before := store.Get()
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	next := before
	next.NotifyWebhookURL = "https://example.invalid/changed"
	if err := store.Set(next); err == nil {
		t.Fatal("closed database accepted configuration")
	}
	if got := store.Get(); got != before {
		t.Fatal("failed save changed the running configuration")
	}
}

func TestUnreadableConfigurationCannotBeOverwritten(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	original := map[string]interface{}{"defaultRetryInterval": "invalid", "tgToken": "preserve-me"}
	if err := database.SetKV(kvConfigKey, original); err != nil {
		t.Fatal(err)
	}
	store := New(database)
	if err := store.Set(store.Get()); err == nil {
		t.Fatal("default values overwrote unreadable configuration")
	}
	var preserved map[string]interface{}
	if _, err := database.GetKV(kvConfigKey, &preserved); err != nil {
		t.Fatal(err)
	}
	if preserved["tgToken"] != original["tgToken"] {
		t.Fatal("original configuration was lost")
	}
}
