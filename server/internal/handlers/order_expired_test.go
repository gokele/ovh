package handlers

import (
	"testing"
	"time"
)

func TestEffectiveOrderStatusMarksLapsedUnpaidOrdersExpired(t *testing.T) {
	now := time.Date(2026, 11, 27, 3, 0, 0, 0, time.UTC)
	cases := []struct {
		status string
		exp    interface{}
		want   string
	}{
		{"notPaid", "2026-11-26T19:26:22+01:00", "expired"}, // 期限已过:OVH 仍报 notPaid
		{"notPaid", "2026-11-27T19:26:22+01:00", "notPaid"}, // 还在付款期内
		{"notPaid", "", "notPaid"},                          // 没有期限不猜
		{"notPaid", nil, "notPaid"},
		{"notPaid", "garbage", "notPaid"},
		{"delivered", "2026-11-26T19:26:22+01:00", "delivered"}, // 已付款的不受期限影响
		{"cancelled", "2026-11-26T19:26:22+01:00", "cancelled"},
	}
	for _, c := range cases {
		if got := effectiveOrderStatus(c.status, c.exp, now); got != c.want {
			t.Errorf("effectiveOrderStatus(%q, %v) = %q, want %q", c.status, c.exp, got, c.want)
		}
	}
}
