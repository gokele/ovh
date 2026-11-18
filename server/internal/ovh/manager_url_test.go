package ovh

import "testing"

// 三个大区的控制面板互不认对方的订单号,发错区用户只会看到"订单不存在"。
func TestManagerOrderURLPerRegion(t *testing.T) {
	cases := []struct{ endpoint, want string }{
		{"ovh-eu", "https://manager.eu.ovhcloud.com/#/dedicated/billing/orders/123"},
		{"ovh-us", "https://manager.us.ovhcloud.com/#/dedicated/billing/orders/123"},
		{"ovh-ca", "https://manager.ca.ovhcloud.com/#/dedicated/billing/orders/123"},
		{"", "https://manager.eu.ovhcloud.com/#/dedicated/billing/orders/123"},
	}
	for _, c := range cases {
		if got := ManagerOrderURL(c.endpoint, "123"); got != c.want {
			t.Fatalf("ManagerOrderURL(%q) = %q, 期望 %q", c.endpoint, got, c.want)
		}
	}
}

// 没有订单号时返回空,让调用方去说人话,而不是拼出一个打不开的链接
func TestManagerOrderURLEmptyID(t *testing.T) {
	if got := ManagerOrderURL("ovh-eu", ""); got != "" {
		t.Fatalf("空订单号应返回空串,实际 %q", got)
	}
}

func TestOrderLinkPrefersOfficialCheckoutURL(t *testing.T) {
	official := "https://www.ovh.com/cgi-bin/order/display-order.cgi?orderId=263756529&orderPassword=abcd"
	if got := OrderLink("ovh-eu", "263756529", official); got != official {
		t.Fatalf("official checkout url must be used: %s", got)
	}
	// 缺失、非 https、非 OVH 域名时退回控制面板深链
	for _, bad := range []string{"", "http://www.ovh.com/cgi-bin/order/display-order.cgi?orderId=1", "https://evil.example/ovh.com", "not a url"} {
		if got := OrderLink("ovh-eu", "263756529", bad); got != "https://manager.eu.ovhcloud.com/#/dedicated/billing/orders/263756529" {
			t.Fatalf("fallback expected for %q, got %s", bad, got)
		}
	}
	if got := OrderLink("ovh-us", "9", "https://www.ovh.com/cgi-bin/order/display-order.cgi?orderId=9&orderPassword=x"); got == "" || got[:8] != "https://" {
		t.Fatalf("us official url: %s", got)
	}
}
