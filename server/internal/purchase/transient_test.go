package purchase

import (
	"errors"
	"fmt"
	"net"
	"testing"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// 429 曾经被算成一次"真正的下单失败尝试"。补货那一刻所有人都在打同一个接口,
// OVH 限流是常态 —— MaxRetries=5 / retryInterval=10s 的任务会在不到一分钟里
// 被自己判死,而那一分钟正是唯一有货的窗口。
func TestTransientDoesNotBurnRetryBudget(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		transient bool
	}{
		{"429 限流", &ovhsdk.APIError{Code: 429, Message: "Too many requests"}, true},
		{"408 超时", &ovhsdk.APIError{Code: 408}, true},
		{"500 OVH 自己挂了", &ovhsdk.APIError{Code: 500}, true},
		{"502 网关", &ovhsdk.APIError{Code: 502}, true},
		{"503 不可用", &ovhsdk.APIError{Code: 503}, true},
		{"504 网关超时", &ovhsdk.APIError{Code: 504}, true},

		{"400 参数错", &ovhsdk.APIError{Code: 400, Message: "Invalid planCode"}, false},
		{"401 凭据错", &ovhsdk.APIError{Code: 401}, false},
		{"403 无权限", &ovhsdk.APIError{Code: 403}, false},
		{"404 机型不在本区", &ovhsdk.APIError{Code: 404}, false},
		{"409 冲突", &ovhsdk.APIError{Code: 409}, false},

		{"连接被重置", errors.New("read tcp 1.2.3.4:443: connection reset by peer"), true},
		{"DNS 解析不了", errors.New("dial tcp: lookup eu.api.ovh.com: no such host"), true},
		{"IO 超时", errors.New("net/http: request canceled (Client.Timeout exceeded)"), true},
		{"EOF", errors.New("unexpected EOF"), true},

		{"业务拒绝", errors.New("this plan is not available in datacenter rbx"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsTransient(c.err); got != c.transient {
				t.Fatalf("IsTransient(%v) = %v, 期望 %v", c.err, got, c.transient)
			}
			// Attempted 是 FailureCount 的开关:transient 必须不计数
			if got := attemptOutcome(c.err).Attempted; got == c.transient && c.err != nil {
				t.Fatalf("attemptOutcome(%v).Attempted = %v,与 transient=%v 矛盾", c.err, got, c.transient)
			}
		})
	}
}

// 包了一层的错误也要认得出来 —— 代码里到处是 fmt.Errorf("...: %w", err)
func TestTransientThroughWrappedError(t *testing.T) {
	wrapped := fmt.Errorf("加购基础商品失败: %w", &ovhsdk.APIError{Code: 429})
	if !IsTransient(wrapped) {
		t.Fatal("包了一层的 429 应当仍被认成 transient")
	}
}

// net.Error 的 Timeout() 路径
type fakeTimeout struct{}

func (fakeTimeout) Error() string   { return "some opaque failure" }
func (fakeTimeout) Timeout() bool   { return true }
func (fakeTimeout) Temporary() bool { return true }

func TestTransientNetTimeout(t *testing.T) {
	var e net.Error = fakeTimeout{}
	if !IsTransient(e) {
		t.Fatal("net.Error.Timeout()==true 应当算 transient")
	}
}

// POST 已发出后,未知传输/JSON 错误和重定向必须走暂停/对账,不能直接重试。
func TestCheckoutUncertaintyIsConservative(t *testing.T) {
	for _, code := range []int{307, 408, 429, 500, 503} {
		if !checkoutUncertain(&ovhsdk.APIError{Code: code}) {
			t.Fatal("uncertain checkout classified as rejected", code)
		}
	}
	for _, code := range []int{400, 401, 403, 404, 409} {
		if checkoutUncertain(&ovhsdk.APIError{Code: code}) {
			t.Fatal("explicit rejection classified as ambiguous", code)
		}
	}
	if !checkoutUncertain(errors.New("unexpected response body")) {
		t.Fatal("unknown checkout error can be retried")
	}
}
