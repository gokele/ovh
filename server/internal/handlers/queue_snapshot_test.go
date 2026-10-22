package handlers

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

type lockCheckingWriter struct {
	*httptest.ResponseRecorder
	mu       *sync.Mutex
	unlocked bool
}

func (w *lockCheckingWriter) Write(p []byte) (int, error) {
	w.unlocked = w.mu.TryLock()
	if w.unlocked {
		w.mu.Unlock()
	}
	return w.ResponseRecorder.Write(p)
}

func TestQueueSnapshotsReleaseLockBeforeResponse(t *testing.T) {
	st, _ := newWatchTestMonitor(t)
	for _, tc := range []struct {
		name string
		mu   *sync.Mutex
		h    gin.HandlerFunc
	}{
		{"queue", &st.QueueMu, GetQueue(st)},
		{"history", &st.HistoryMu, GetPurchaseHistory(st)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/", tc.h)
			w := &lockCheckingWriter{ResponseRecorder: httptest.NewRecorder(), mu: tc.mu}
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			if !w.unlocked {
				t.Fatal("a slow client would block queue/history mutations")
			}
			if w.Body.String() != "[]" {
				t.Fatalf("empty response must remain an array: %s", w.Body.String())
			}
		})
	}
}
