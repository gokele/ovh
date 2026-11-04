package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/types"
)

func TestDurationOffersAreAccountSpecificAndNeverOrder(t *testing.T) {
	var assigned, deleted, reads int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/auth/time":
			fmt.Fprint(w, time.Now().Unix())
		case r.Method == "POST" && r.URL.Path == "/order/cart":
			fmt.Fprint(w, `{"cartId":"periods"}`)
		case r.Method == "POST" && r.URL.Path == "/order/cart/periods/assign":
			assigned++
			fmt.Fprint(w, `{}`)
		case r.Method == "DELETE" && r.URL.Path == "/order/cart/periods":
			deleted++
			fmt.Fprint(w, `{}`)
		case r.Method == "GET" && (strings.HasSuffix(r.URL.Path, "/eco") || strings.HasSuffix(r.URL.Path, "/vps")):
			reads++
			if assigned < reads {
				t.Error("offers read before assigning cart")
			}
			fmt.Fprint(w, `[{"planCode":"plan","prices":[{"duration":"P1M","pricingMode":"default"},{"duration":"P1Y","pricingMode":"upfront12"},{"duration":"P12M","pricingMode":"default"}]}]`)
		default:
			t.Errorf("duration lookup must not add products or checkout: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	defer api.Close()
	state, _ := newWatchTestMonitor(t)
	state.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: api.URL, Zone: "IE", AppKey: "test", AppSecret: "test", ConsumerKey: "test"}}
	router := gin.New()
	router.GET("/durations", GetOrderDurations(state))
	for _, product := range []string{"eco", "vps"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/durations?account_id=eu&planCode=plan&product="+product, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"durations":["P1M","P12M"]`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, query := range []string{"", "?account_id=missing&planCode=plan", "?account_id=eu&planCode=plan&product=vps&subsidiary=US", "?account_id=eu&planCode=plan&product=../checkout"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/durations"+query, nil))
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if reads != 2 || deleted != 2 || len(state.Queue) != 0 || len(state.History) != 0 {
		t.Fatal("lookup left carts or tasks", reads, deleted)
	}
}
