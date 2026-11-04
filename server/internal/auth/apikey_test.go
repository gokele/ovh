package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestAPIKeyIsRandomPersistentAndConcurrent(t *testing.T) {
	dir := t.TempDir()
	const n = 16
	var wg sync.WaitGroup
	keys := make([]string, n)
	errs := make([]error, n)
	for i := range keys {
		wg.Add(1)
		go func(i int) { defer wg.Done(); keys[i], _, errs[i] = ResolveAPIKey(dir, "") }(i)
	}
	wg.Wait()
	for i, key := range keys {
		if errs[i] != nil || len(key) != 64 || key != keys[0] {
			t.Fatalf("startup %d did not obtain the same persistent key: %v", i, errs[i])
		}
	}
	key, generated, err := ResolveAPIKey(dir, "")
	if err != nil || generated || key != keys[0] {
		t.Fatal("restart changed the key")
	}
	other, _, err := ResolveAPIKey(t.TempDir(), "")
	if err != nil || other == key {
		t.Fatal("independent installations must have independent keys")
	}
	info, err := os.Stat(filepath.Join(dir, ".api-secret"))
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatal("generated key must be owner-only")
	}
}

func TestConfiguredAPIKeyIsPreservedAndStorageErrorsFailClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	key, generated, err := ResolveAPIKey(path, "configured-key")
	if err != nil || generated || key != "configured-key" {
		t.Fatal("configured key was changed")
	}
	if key, _, err := ResolveAPIKey(path, ""); err == nil || key != "" {
		t.Fatal("storage failure fell back to an insecure key")
	}
}
