package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/types"
)

func TestOrderRequestsRejectUnrecognizedConstraintsBeforeAnySideEffects(t *testing.T) {
	state, _ := newWatchTestMonitor(t)
	state.Accounts = []types.OVHAccount{{ID: "test", Endpoint: "ovh-eu", Zone: "IE", AppKey: "test", AppSecret: "test", ConsumerKey: "test"}}
	state.Queue = []types.QueueItem{{ID: "existing", AccountID: "test", PlanCode: "27skle02-v1", Datacenter: "gra", Status: "paused", RetryInterval: 60}}
	before := state.Queue[0]
	prewarmer := &testPrewarmer{}
	state.OrderPrewarm = prewarmer
	original := http.DefaultTransport
	http.DefaultTransport = queuePreorderTransport(func(*http.Request) (*http.Response, error) {
		t.Error("invalid parameters reached an OVH API")
		return nil, io.ErrUnexpectedEOF
	})
	defer func() { http.DefaultTransport = original }()
	router := gin.New()
	router.POST("/queue", AddQueueItem(state))
	router.PUT("/queue/:id", UpdateQueueItem(state))
	router.POST("/quick-order", QuickOrder(state))
	router.POST("/prewarm", PrepareOrderCart(state))
	for _, route := range []struct {
		method, path string
		body         map[string]interface{}
	}{
		{"POST", "/queue", map[string]interface{}{"account_id": "test", "planCode": "27skle02-v1", "datacenter": "gra", "preorder": true}},
		{"PUT", "/queue/existing", map[string]interface{}{"datacenter": "gra", "retryInterval": 60}},
		{"POST", "/quick-order", map[string]interface{}{"account_id": "test", "planCode": "27skle02-v1", "datacenter": "gra", "options": []string{"ram-32g-ecc-2400"}}},
		{"POST", "/prewarm", map[string]interface{}{"id": "guard-request", "account_id": "test", "planCode": "27skle02-v1", "datacenters": []string{"gra"}, "quantity": 1}},
	} {
		for _, field := range []string{"hwRam", "hwDisk", "allowFreqDrift", "maxPrice", "autoPya"} {
			t.Run(route.method+route.path+"/"+field, func(t *testing.T) {
				body := map[string]interface{}{field: "unsupported"}
				for name, value := range route.body {
					body[name] = value
				}
				raw, _ := json.Marshal(body)
				request := httptest.NewRequest(route.method, route.path, bytes.NewReader(raw))
				request.Header.Set("Content-Type", "application/json")
				record := httptest.NewRecorder()
				router.ServeHTTP(record, request)
				if record.Code != http.StatusBadRequest {
					t.Fatalf("ignored %s: %d %s", field, record.Code, record.Body.String())
				}
			})
		}
	}
	if len(state.Queue) != 1 || !reflect.DeepEqual(state.Queue[0], before) || len(prewarmer.items) != 0 {
		t.Fatal("rejected parameters changed queue or prepared a cart")
	}
}

func TestOrderJSONRequiresOneValueAndPreservesValidation(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"planCode":"27skle02-v1"}`, true},
		{"{\"planCode\":\"27skle02-v1\"}\n\t", true},
		{`{"planCode":"27skle02-v1","maxPrice":1}`, false},
		{`{"planCode":"27skle02-v1"} {}`, false},
		{`{"planCode":"27skle02-v1"} trailing`, false},
		{`{}`, false},
		{``, false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			var value struct {
				PlanCode string `json:"planCode" binding:"required"`
			}
			if err := bindOrderJSON(context, &value); (err == nil) != tc.ok {
				t.Fatalf("accepted=%v err=%v", err == nil, err)
			}
		})
	}
}
