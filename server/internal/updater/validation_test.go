package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFetchLatestRejectsMalformedURL(t *testing.T) {
	t.Setenv("OVH_UPDATE_API", "http://[invalid")
	if _, err := FetchLatest(); err == nil {
		t.Fatal("malformed URL accepted")
	}
}

func TestPrepareRejectsMalformedChecksums(t *testing.T) {
	for _, sum := range []string{"bad", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", "abc123abc123abc123abc123abc123abc123abc123abc123abc123abc123abc123abc123"} {
		t.Run(sum[:3], func(t *testing.T) {
			srv, rel := fakeRelease(t, []byte("new binary"), sum)
			defer srv.Close()
			exe := withFakeExe(t, "old binary")
			if _, _, err := Prepare(rel, nil); err == nil {
				t.Fatal("invalid SHA-256 checksum accepted")
			}
			body, err := os.ReadFile(exe)
			if err != nil || string(body) != "old binary" {
				t.Fatal("existing executable changed", err)
			}
		})
	}
}

func TestPrepareDoesNotReusePredictableTemporaryFile(t *testing.T) {
	srv, rel := fakeRelease(t, []byte("new binary"), "")
	defer srv.Close()
	exe := withFakeExe(t, "old binary")
	oldPath := filepath.Join(filepath.Dir(exe), ".ovh-server-update-probe")
	if err := os.WriteFile(oldPath, []byte("unrelated file"), 0600); err != nil {
		t.Fatal(err)
	}
	tmp, _, err := Prepare(rel, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp)
	if body, err := os.ReadFile(oldPath); err != nil || string(body) != "unrelated file" {
		t.Fatal("update deleted an existing file while probing directory permissions", err)
	}
}
