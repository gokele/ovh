package logger

import (
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/types"
)

func TestConcurrentLoggingClearAndFlush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.json")
	l := New(path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := 0; i < maxLogs; i++ {
		l.Info("initial", "test")
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				l.Error(fmt.Sprintf("%d:%d", worker, i), "test")
			}
		}(worker)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			if err := l.Clear(); err != nil {
				t.Error(err)
			}
			l.Flush()
		}
	}()
	wg.Wait()
	l.Flush()
	var saved []types.LogEntry
	if err := storage.ReadJSON(path, &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, l.Snapshot()) {
		t.Fatal("persisted logs differ from the latest in-memory entries")
	}
	if err := l.Clear(); err != nil {
		t.Fatal(err)
	}
	l.Flush()
	if err := storage.ReadJSON(path, &saved); err != nil || len(saved) != 0 {
		t.Fatal("cleared logs reappeared", err)
	}
}
