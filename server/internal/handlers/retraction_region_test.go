package handlers

import "testing"

// 撤单接口不是三个大区都有。
//
// 实测三份官方 schema:
//
//	eu.api.ovh.com        POST /me/order/{id}/retraction  ✓
//	ca.api.ovh.com        POST /me/order/{id}/retraction  ✓
//	api.us.ovhcloud.com   整个接口不存在,只有只读的 /me/refund
//
// 而**三个区的 billing.Order 都带 retractionDate 字段**。这是最容易踩的地方:
// 只看 retractionDate 有没有的话,美区账户完全可能查到一个撤回截止日、
// 界面上显示出"可撤单"按钮,用户点下去收到一个 404。
//
// 一个点了必然失败的退款按钮,比没有按钮更糟 —— 用户会以为自己申请了。
func TestRetractionRegionGate(t *testing.T) {
	cases := []struct {
		endpoint  string
		supported bool
		why       string
	}{
		{"ovh-eu", true, "欧区有 retraction 接口"},
		{"ovh-ca", true, "加区有 retraction 接口"},
		{"ovh-us", false, "美区 API 根本没有这个接口"},
		{"kimsufi-eu", true, "kimsufi 欧区走 EU 那套"},
		{"soyoustart-eu", true, "soyoustart 欧区走 EU 那套"},
		{"kimsufi-ca", true, "kimsufi 加区走 CA 那套"},
		{"soyoustart-ca", true, "soyoustart 加区走 CA 那套"},
		{"", true, "认不出的 endpoint 按 EU 处理(EndpointRegion 的既有语义)"},
	}
	for _, c := range cases {
		if got := retractionSupportedRegion(c.endpoint); got != c.supported {
			t.Errorf("retractionSupportedRegion(%q) = %v, 期望 %v —— %s",
				c.endpoint, got, c.supported, c.why)
		}
	}
}

// 撤单理由是 OVH 的必填枚举,传别的值它直接 400。
// 前端下拉照这份渲染,所以这份必须和 billing.order.RetractionReasonEnum 完全一致。
func TestRetractionReasonsMatchOVHEnum(t *testing.T) {
	// 来自 eu.api.ovh.com/1.0/me.json 的 billing.order.RetractionReasonEnum
	want := map[string]bool{
		"competitor": true, "difficulty": true, "expensive": true,
		"other": true, "performance": true, "reliability": true, "unused": true,
	}
	got := map[string]bool{}
	for _, r := range retractionReasons {
		v, _ := r["value"].(string)
		if v == "" {
			t.Fatalf("有一项缺 value: %+v", r)
		}
		if !want[v] {
			t.Errorf("%q 不在 OVH 的枚举里,提交会被 400 拒绝", v)
		}
		if lbl, _ := r["label"].(string); lbl == "" {
			t.Errorf("%q 缺中文说明", v)
		}
		got[v] = true
	}
	for v := range want {
		if !got[v] {
			t.Errorf("少了 OVH 支持的理由 %q —— 用户会因此选不到真实原因", v)
		}
	}

	// 校验函数要认全集、也要挡住枚举外的值
	for v := range want {
		if !isValidRetractionReason(v) {
			t.Errorf("isValidRetractionReason(%q) 应为 true", v)
		}
	}
	for _, bad := range []string{"", "REFUND", "expensive ", "太贵了", "unknown"} {
		if isValidRetractionReason(bad) {
			t.Errorf("isValidRetractionReason(%q) 应为 false —— 直接放过去会被 OVH 400", bad)
		}
	}
}
