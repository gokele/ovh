package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/types"
)

type queuePreorderTransport func(*http.Request) (*http.Response, error)

func (f queuePreorderTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAddQueuePreorderDoesNotBypassValidation(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = queuePreorderTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"plans":[]}`
		if strings.Contains(r.URL.Path, "/availabilities") {
			plan := r.URL.Query().Get("planCode")
			has := (plan == "27skle03-v1" || plan == "25scale01-v1") && r.URL.Host == "eu.api.ovh.com"
			if plan == "27skle03-v1-us" {
				has = r.URL.Host == "api.us.ovhcloud.com"
			}
			body = `[]`
			if has {
				body = `[{"planCode":"` + plan + `"}]`
			}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	st, _ := newWatchTestMonitor(t)
	st.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu", Zone: "FI"}}
	router := gin.New()
	router.POST("/queue", AddQueueItem(st))
	for _, tc := range []struct {
		plan     string
		preorder bool
		status   int
	}{
		{"27skle03-v1", false, http.StatusBadRequest},
		{"27skle03-v1", true, http.StatusOK},
		{"27skle03-v1-us", true, http.StatusBadRequest},
		{"27skle99-v1", true, http.StatusBadRequest},
		{"25scale01-v1", true, http.StatusBadRequest},
	} {
		body := fmt.Sprintf(`{"account_id":"eu","planCode":%q,"datacenter":"gra","preorder":%t,"coupon":" Save10 "}`, tc.plan, tc.preorder)
		request := httptest.NewRequest(http.MethodPost, "/queue", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("%s preorder=%v: %d, %s", tc.plan, tc.preorder, response.Code, response.Body.String())
		}
	}
	items, err := st.DB.ListQueue()
	if err != nil || len(items) != 1 || !items[0].Preorder || items[0].Coupon != "Save10" {
		t.Fatalf("preorder was not persisted: %+v %v", items, err)
	}
}
