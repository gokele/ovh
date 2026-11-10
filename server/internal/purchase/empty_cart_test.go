package purchase

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPreorderReusesOnlyConfirmedEmptyAssignedCart(t *testing.T) {
	for _, tc := range []struct {
		name, emptyResponse string
		status              int
		keep, expire        bool
	}{
		{name: "empty", status: 400, keep: true},
		{name: "expired empty", status: 400, keep: true, expire: true},
		{name: "partial item", status: 400, emptyResponse: `{"items":[99]}`},
		{name: "missing item list", status: 400, emptyResponse: `{}`},
		{name: "unknown item list", status: 400, emptyResponse: `{"items":null}`},
		{name: "read only cart", status: 400, emptyResponse: `{"items":[],"readOnly":true}`},
		{name: "ambiguous base response", status: 503},
		{name: "rate limited", status: 429},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, carts, checkouts, stock := hotFixture(t)
			stock.Store(true) // A prerelease can show 1H stock before the cart API opens.
			base, err := state.OVH.ClientFor("hot-eu")
			if err != nil {
				t.Fatal(err)
			}
			original := base.Client.Transport
			var open atomic.Bool
			var assigns atomic.Int32
			base.Client.Transport = preorderTransport(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/assign") {
					assigns.Add(1)
				}
				status, body := 200, ""
				if !open.Load() {
					switch {
					case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/eco"):
						status, body = tc.status, `{"message":"not open yet"}`
					case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/eco"):
						body = `[]`
					case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/order/cart/cart-"):
						body = tc.emptyResponse
					}
				}
				if body != "" {
					return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				}
				return original.RoundTrip(r)
			})
			item := poolItem()
			item.Preorder = true
			session := &cartSession{}
			defer session.discard()
			ctx := context.WithValue(context.Background(), cartSessionKey{}, session)
			out := PurchaseServer(ctx, state, &item)
			if checkouts.Load() != 0 || session.cart != nil || (session.empty != nil) != tc.keep {
				t.Fatal("unconfirmed/partial cart was retained", out, session.empty)
			}
			if tc.keep && !out.WaitingForCatalog {
				t.Fatal(out)
			}
			if tc.expire {
				session.empty.expires = time.Now().Add(-time.Second)
			}
			open.Store(true)
			out = PurchaseServer(ctx, state, &item)
			if !out.Success || checkouts.Load() != 1 || session.empty != nil {
				t.Fatal("preorder did not complete safely", out, checkouts.Load())
			}
			want := int32(2)
			if tc.keep && !tc.expire {
				want = 1
			}
			if carts.Load() != want || assigns.Load() != want {
				t.Fatal("invalid seed reuse or repeated create/assign", carts.Load(), assigns.Load(), want)
			}
		})
	}
}
