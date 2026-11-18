package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/types"
)

type paymentTransport func(*http.Request) (*http.Response, error)

func (f paymentTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// 默认支付方式:VALID 可用;没有、非 VALID、已过期都判为不可用并说明原因。
func TestDefaultPaymentMethodUsability(t *testing.T) {
	for _, tc := range []struct {
		name, ids, method string
		usable            bool
		reason            string
	}{
		{"valid", `[7]`, `{"paymentMethodId":7,"default":true,"status":"VALID","label":"Visa ****1234","paymentType":"CREDIT_CARD"}`, true, ""},
		{"none", `[]`, ``, false, "没有设置默认支付方式"},
		{"expired status", `[7]`, `{"paymentMethodId":7,"default":true,"status":"EXPIRED"}`, false, "EXPIRED"},
		{"expired date", `[7]`, `{"paymentMethodId":7,"default":true,"status":"VALID","expirationDate":"2020-01-01T00:00:00Z"}`, false, "已过期"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, _ := newWatchTestMonitor(t)
			state.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu", Zone: "IE", AppKey: "ak", AppSecret: "as", ConsumerKey: "ck"}}
			base, err := state.OVH.ClientFor("eu")
			if err != nil {
				t.Fatal(err)
			}
			base.Client.Transport = paymentTransport(func(r *http.Request) (*http.Response, error) {
				body := `{}`
				switch {
				case strings.HasSuffix(r.URL.Path, "/auth/time"):
					body = fmt.Sprint(time.Now().Unix())
				case strings.HasSuffix(r.URL.Path, "/me/payment/method"):
					if r.URL.Query().Get("default") != "true" {
						t.Errorf("must filter on default=true, got %s", r.URL.RawQuery)
					}
					body = tc.ids
				case strings.Contains(r.URL.Path, "/me/payment/method/"):
					body = tc.method
				}
				if r.Method != http.MethodGet {
					t.Errorf("default payment check must be read-only, got %s", r.Method)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			info, err := loadDefaultPayment(context.Background(), state, "eu")
			if err != nil {
				t.Fatal(err)
			}
			if info.Usable != tc.usable || (tc.reason != "" && !strings.Contains(info.Reason, tc.reason)) {
				t.Fatalf("usable=%t reason=%q", info.Usable, info.Reason)
			}
		})
	}
}
