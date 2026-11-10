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

func TestCartRequiresEveryExpectedHardwareSKU(t *testing.T) {
	for _, tc := range []struct {
		name, base, memory, storage string
		extraCode, quantity         string
		missing, extra, reject      bool
	}{
		{name: "complete", base: "27skle03-v1", memory: "ram-64g-ecc-2666-27skle03-v1", storage: "softraid-2x480ssd-27skle03-v1"},
		{name: "base substituted", base: "27skle03-v2", memory: "ram-64g-ecc-2666-27skle03-v1", storage: "softraid-2x480ssd-27skle03-v1", reject: true},
		{name: "memory substituted", base: "27skle03-v1", memory: "ram-64g-ecc-2133-27skle03-v1", storage: "softraid-2x480ssd-27skle03-v1", reject: true},
		{name: "mixed disk substituted", base: "27skle03-v1", memory: "ram-64g-ecc-2666-27skle03-v1", storage: "softraid-2x480ssd-2x8000sa-27skle03-v1", reject: true},
		{name: "addon silently omitted", base: "27skle03-v1", missing: true, reject: true},
		{name: "unexpected extra hardware", base: "27skle03-v1", memory: "ram-64g-ecc-2666-27skle03-v1", storage: "softraid-2x480ssd-27skle03-v1", extra: true, reject: true},
		{name: "CPU follows model", base: "27skle03-v1", memory: "ram-64g-ecc-2666-27skle03-v1", storage: "softraid-2x480ssd-27skle03-v1", extraCode: "cpu-provider-default"},
		{name: "implicit network default", base: "27skle03-v1", memory: "ram-64g-ecc-2666-27skle03-v1", storage: "softraid-2x480ssd-27skle03-v1", extraCode: "bandwidth-provider-default"},
		{name: "duplicate host", base: "27skle03-v1", memory: "ram-64g-ecc-2666-27skle03-v1", storage: "softraid-2x480ssd-27skle03-v1", extraCode: "27skle03-v1", reject: true},
		{name: "duplicate memory", base: "27skle03-v1", memory: "ram-64g-ecc-2666-27skle03-v1", storage: "softraid-2x480ssd-27skle03-v1", extraCode: "ram-64g-ecc-2666-27skle03-v1", reject: true},
		{name: "quantity one", base: "27skle03-v1", memory: "ram-64g-ecc-2666-27skle03-v1", storage: "softraid-2x480ssd-27skle03-v1", quantity: "1"},
		{name: "quantity two", base: "27skle03-v1", memory: "ram-64g-ecc-2666-27skle03-v1", storage: "softraid-2x480ssd-27skle03-v1", quantity: "2", reject: true},
		{name: "quantity zero", base: "27skle03-v1", memory: "ram-64g-ecc-2666-27skle03-v1", storage: "softraid-2x480ssd-27skle03-v1", quantity: "0", reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("verification mutated cart")
					w.WriteHeader(400)
					return
				}
				switch r.URL.Path {
				case "/auth/time":
					fmt.Fprint(w, time.Now().Unix())
				case "/order/cart/test":
					fmt.Fprint(w, `{"items":[1]}`)
				case "/order/cart/test/item/1":
					options := "[2,3]"
					if tc.missing {
						options = "[]"
					}
					if tc.extra || tc.extraCode != "" {
						options = "[2,3,4]"
					}
					quantity := ""
					if tc.quantity != "" {
						quantity = `,"quantity":` + tc.quantity
					}
					fmt.Fprintf(w, `{"itemId":1,"duration":"P1M","settings":{"planCode":%q%s},"options":%s}`, tc.base, quantity, options)
				case "/order/cart/test/item/2":
					fmt.Fprintf(w, `{"itemId":2,"duration":"P1M","settings":{"planCode":%q}}`, tc.memory)
				case "/order/cart/test/item/3":
					fmt.Fprintf(w, `{"itemId":3,"duration":"P1M","settings":{"planCode":%q}}`, tc.storage)
				case "/order/cart/test/item/4":
					code := tc.extraCode
					if code == "" {
						code = "unselected-extra-hardware"
					}
					fmt.Fprintf(w, `{"itemId":4,"duration":"P1M","settings":{"planCode":%q}}`, code)
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
			err = VerifyCart(context.Background(), client, "test", 1, "P1M", "27skle03-v1", "ram-64g-ecc-2666-27skle03-v1", "softraid-2x480ssd-27skle03-v1")
			if (err != nil) != tc.reject || (tc.reject && !errors.Is(err, ErrCartConfiguration)) {
				t.Fatal(err)
			}
		})
	}
}

// 同一层的购物车项目并行读取:三个各慢 150ms 的项目不能串行累加到 450ms。
func TestCartItemsAreReadInParallel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/time":
			fmt.Fprint(w, time.Now().Unix())
		case "/order/cart/fast":
			fmt.Fprint(w, `{"items":[1,2,3]}`)
		default:
			time.Sleep(150 * time.Millisecond)
			id := strings.TrimPrefix(r.URL.Path, "/order/cart/fast/item/")
			plan := map[string]string{"1": "plan", "2": "ram", "3": "disk"}[id]
			fmt.Fprintf(w, `{"itemId":%s,"duration":"P1M","settings":{"planCode":%q,"quantity":1}}`, id, plan)
		}
	}))
	defer server.Close()
	client, err := ovhsdk.NewClient(server.URL, "ak", "as", "ck")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := VerifyCart(context.Background(), client, "fast", 1, "P1M", "plan", "ram", "disk"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 350*time.Millisecond {
		t.Fatalf("item reads ran serially: %s", elapsed)
	}
}
