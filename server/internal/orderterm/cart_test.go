package orderterm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

func TestCartPeriodsIncludeImplicitAndNestedOptions(t *testing.T) {
	for _, tc := range []struct {
		name, ids, first, second, third string
		reject                          bool
		status                          int
	}{
		{name: "annual aliases", ids: `[1,2]`, first: `"P1Y"`, second: `"P12M"`, third: `"P1Y"`},
		{name: "monthly host", ids: `[1,2]`, first: `"P1M"`, second: `"P12M"`, third: `"P12M"`, reject: true},
		{name: "monthly addon", ids: `[1,2]`, first: `"P12M"`, second: `"P1M"`, third: `"P12M"`, reject: true},
		{name: "implicit monthly nested addon", ids: `[1]`, first: `"P12M"`, second: `"P12M"`, third: `"P1M"`, reject: true},
		{name: "missing duration", ids: `[1,2]`, first: `null`, second: `"P12M"`, third: `"P12M"`, reject: true},
		{name: "empty duration", ids: `[1,2]`, first: `"P12M"`, second: `""`, third: `"P12M"`, reject: true},
		{name: "empty cart", ids: `[]`, reject: true},
		{name: "missing host", ids: `[2]`, second: `"P12M"`, third: `"P12M"`, reject: true},
		{name: "read failure", ids: `[1]`, status: 503, reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("verification must never mutate cart")
					w.WriteHeader(400)
					return
				}
				switch r.URL.Path {
				case "/auth/time":
					fmt.Fprint(w, time.Now().Unix())
				case "/order/cart/test":
					fmt.Fprintf(w, `{"items":%s}`, tc.ids)
				case "/order/cart/test/item/1":
					if tc.status != 0 {
						w.WriteHeader(tc.status)
						fmt.Fprint(w, `{"message":"unavailable"}`)
						return
					}
					fmt.Fprintf(w, `{"itemId":1,"duration":%s,"options":[2]}`, tc.first)
				case "/order/cart/test/item/2":
					fmt.Fprintf(w, `{"itemId":2,"duration":%s,"options":[3]}`, tc.second)
				case "/order/cart/test/item/3":
					fmt.Fprintf(w, `{"itemId":3,"duration":%s,"options":[]}`, tc.third)
				default:
					t.Error("unexpected request", r.URL)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			client, err := ovhsdk.NewClient(server.URL, "test", "test", "test")
			if err != nil {
				t.Fatal(err)
			}
			err = VerifyCart(context.Background(), client, "test", 1, "P12M")
			if (err != nil) != tc.reject {
				t.Fatal(err)
			}
			if tc.reject && tc.status == 0 && !errors.Is(err, ErrCartPeriod) {
				t.Fatal("unsafe cart was not identified", err)
			}
			if tc.name == "read failure" && (err == nil || !strings.Contains(err.Error(), "unavailable")) {
				t.Fatal(err)
			}
		})
	}
}
