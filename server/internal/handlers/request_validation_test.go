package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/types"
)

func TestOptionalJSONRejectsPartiallyDecodedMutation(t *testing.T) {
	for _, tc := range []struct {
		body            string
		status, actions int
	}{
		{`{"confirm":true,"count":"invalid"}`, 400, 0},
		{`{"confirm":"invalid","count":1}`, 400, 0},
		{`{"confirm":true,`, 400, 0},
		{`{"confirm":true,"count":1}`, 200, 1},
		{"", 200, 1},
	} {
		actions := 0
		router := gin.New()
		router.POST("/operation", func(c *gin.Context) {
			var body struct {
				Confirm bool `json:"confirm"`
				Count   int  `json:"count"`
			}
			if !bindOptionalJSON(c, &body) {
				return
			}
			actions++
			c.Status(200)
		})
		request := httptest.NewRequest(http.MethodPost, "/operation", strings.NewReader(tc.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != tc.status || actions != tc.actions {
			t.Fatalf("body %q status=%d actions=%d", tc.body, response.Code, actions)
		}
	}
}

func TestSubscriptionRemovalDoesNotReportSuccessWhenSavingFails(t *testing.T) {
	for _, kind := range []string{"dedicated", "vps"} {
		t.Run(kind, func(t *testing.T) {
			state, mon := newWatchTestMonitor(t)
			router := gin.New()
			if kind == "dedicated" {
				mon.AddSubscription("fixture", nil, true, false, "", nil, nil, false, 0, "", nil, "")
				router.DELETE("/subscriptions/:planCode", RemoveSubscription(state, mon))
			} else {
				state.VPSSubscriptions = []types.VPSSubscription{{ID: "fixture", PlanCode: "fixture"}}
				router.DELETE("/subscriptions/:subscription_id", RemoveVPSSubscription(state))
			}
			_ = state.DB.Close()
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/subscriptions/fixture", nil))
			if response.Code != 500 {
				t.Fatalf("failed persistence returned %d: %s", response.Code, response.Body.String())
			}
		})
	}
}
