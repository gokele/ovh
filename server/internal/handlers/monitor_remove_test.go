package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/monitor"
)

func TestRemoveMonitorSubscriptionPersists(t *testing.T) {
	for _, tc := range []struct {
		name, optionsKey, remaining string
	}{
		{"first subscription", "", "ram-128g"},
		{"exact configuration", "ram-128g", "ram-64g"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, mon := newWatchTestMonitor(t)
			for _, option := range []string{"ram-64g", "ram-128g"} {
				mon.AddSubscription("nonexistent-model", nil, true, false,
					"", nil, nil, false, 0, "", []string{option}, "")
			}
			mon.SaveToDB()
			router := gin.New()
			router.DELETE("/monitor/subscriptions/:planCode", RemoveSubscription(st, mon))
			path := "/monitor/subscriptions/nonexistent-model"
			if tc.optionsKey != "" {
				path += "?optionsKey=" + url.QueryEscape(tc.optionsKey)
			}
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, httptest.NewRequest(http.MethodDelete, path, nil))
			if resp.Code != http.StatusOK {
				t.Fatalf("first delete: status %d, body %s", resp.Code, resp.Body.String())
			}
			// Reload from SQLite as startup does: deleted subscriptions must stay deleted.
			reloaded := monitor.New(st)
			reloaded.LoadFromDB()
			subs := reloaded.Snapshot()
			if len(subs) != 1 || monitor.OptionsKey(subs[0].Options) != tc.remaining {
				t.Fatalf("wrong subscriptions after reload: %+v", subs)
			}

			// An explicit configuration must not fall through and delete its sibling.
			if tc.optionsKey == "" {
				path += "?optionsKey=ram-64g"
			}
			for _, missingPath := range []string{path, "/monitor/subscriptions/not-subscribed"} {
				resp = httptest.NewRecorder()
				router.ServeHTTP(resp, httptest.NewRequest(http.MethodDelete, missingPath, nil))
				if resp.Code != http.StatusNotFound {
					t.Fatalf("missing subscription: status %d, body %s", resp.Code, resp.Body.String())
				}
			}
			if len(mon.Snapshot()) != 1 {
				t.Fatal("deleting a missing subscription removed its sibling")
			}
		})
	}
}
