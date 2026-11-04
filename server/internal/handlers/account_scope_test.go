package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/types"
)

func TestRequestKeepsResolvedAccountAndCredentials(t *testing.T) {
	state, _ := newWatchTestMonitor(t)
	state.Accounts = []types.OVHAccount{
		{ID: "eu", Endpoint: "ovh-eu", AppKey: "original", AppSecret: "fixture", ConsumerKey: "fixture", IsDefault: true},
		{ID: "us", Endpoint: "ovh-us", AppKey: "other", AppSecret: "fixture", ConsumerKey: "fixture"},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/operation", nil)
	first, ok := ovhAccountFor(state, c)
	if !ok || first.ID != "eu" {
		t.Fatal("initial account resolution failed")
	}
	state.AccountsMu.Lock()
	state.Accounts[0].IsDefault = false
	state.Accounts[0].AppKey = "changed"
	state.Accounts[1].IsDefault = true
	state.AccountsMu.Unlock()
	client, err := ovhClientFor(state, c)
	if err != nil {
		t.Fatal(err)
	}
	second, ok := ovhAccountFor(state, c)
	if !ok || second != first || client.AppKey != first.AppKey {
		t.Fatal("one request mixed account snapshots or credentials")
	}
}
