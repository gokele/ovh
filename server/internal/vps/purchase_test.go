package vps

import "testing"

func labels(cfgs []kv) map[string]string {
	m := map[string]string{}
	for _, c := range cfgs {
		m[c.label] = c.value
	}
	return m
}

// US 站点只有一个 region 取值,直接用,不要拿机房去猜
func TestBuildVPSConfig_US只有一个region(t *testing.T) {
	req := []requiredItem{
		{Label: "vps_datacenter", Required: true, AllowedValues: []string{"US-EAST-VA", "US-WEST-OR"}},
		{Label: "region", AllowedValues: []string{"united_states"}},
	}
	got := labels(buildVPSConfig(req, "US-EAST-VA", ""))
	if got["region"] != "united_states" {
		t.Errorf("region 应该是 united_states,实际 %q", got["region"])
	}
	if got["vps_datacenter"] != "US-EAST-VA" {
		t.Errorf("机房不对: %q", got["vps_datacenter"])
	}
}

// EU/CA 站点给两个 region,必须按机房挑对那个。
// 挑错的代价是整单失败 —— BHS 是加拿大,GRA 是欧洲。
func TestBuildVPSConfig_EU按机房挑region(t *testing.T) {
	req := []requiredItem{
		{Label: "vps_datacenter", Required: true, AllowedValues: []string{"GRA", "BHS", "SGP"}},
		{Label: "region", AllowedValues: []string{"canada", "europe"}},
	}
	cases := map[string]string{
		"GRA": "europe",
		"SBG": "europe",
		"UK":  "europe",
		"BHS": "canada",
		"SGP": "canada",
		"SYD": "canada",
		"YNM": "canada",
	}
	for dc, want := range cases {
		got := labels(buildVPSConfig(req, dc, ""))
		if got["region"] != want {
			t.Errorf("机房 %s 应该配 region=%s,实际 %q", dc, want, got["region"])
		}
	}
}

// 机房认不出来时宁可不提交 region:它不是必填项,
// 让 OVH 用默认值,总比提交一个错的把整单打掉强
func TestBuildVPSConfig_未知机房不猜region(t *testing.T) {
	req := []requiredItem{
		{Label: "vps_datacenter", Required: true},
		{Label: "region", AllowedValues: []string{"canada", "europe"}},
	}
	got := labels(buildVPSConfig(req, "MARS-1", ""))
	if _, ok := got["region"]; ok {
		t.Errorf("认不出的机房不该硬猜 region,却提交了 %q", got["region"])
	}
	if got["vps_datacenter"] != "MARS-1" {
		t.Error("机房还是要照实提交,让 OVH 给出准确报错")
	}
}

// 购物车没列出来的配置项一个都不能提交:多一个不认识的 label 就是 400
func TestBuildVPSConfig_不提交购物车没要的项(t *testing.T) {
	req := []requiredItem{{Label: "vps_datacenter", Required: true}}
	got := labels(buildVPSConfig(req, "GRA", "Ubuntu 24.04"))
	if _, ok := got["region"]; ok {
		t.Error("购物车没要 region 就不该提交")
	}
	// vps_os 购物车没列,但用户明确选了 —— 允许提交(OVH 会自己校验)
	if got["vps_os"] != "Ubuntu 24.04" {
		t.Errorf("用户选了系统就该提交,实际 %q", got["vps_os"])
	}
}

// 用户没选系统就不提交,让 OVH 用默认镜像
func TestBuildVPSConfig_没选系统就不提交(t *testing.T) {
	req := []requiredItem{
		{Label: "vps_datacenter", Required: true},
		{Label: "vps_os", AllowedValues: []string{"Ubuntu 24.04", "Debian 12"}},
	}
	got := labels(buildVPSConfig(req, "GRA", ""))
	if _, ok := got["vps_os"]; ok {
		t.Error("没选系统不该提交 vps_os")
	}
}

// 完全拉不到必需配置时也得把机房交上去 —— 那是唯一的必填项
func TestBuildVPSConfig_拉不到必需配置时兜底(t *testing.T) {
	got := labels(buildVPSConfig(nil, "GRA", ""))
	if got["vps_datacenter"] != "GRA" {
		t.Errorf("兜底时机房必须提交,实际 %v", got)
	}
}

// 这张表是从 OVH 自己的目录里读出来的(US 站点 -ca 变体列的正好是这四个机房),
// 改动它等于改变下单去哪个大洲,必须有测试盯着
func TestDCRegion表与OVH目录一致(t *testing.T) {
	canada := []string{"BHS", "SGP", "SYD", "YNM"}
	for _, dc := range canada {
		if dcRegion[dc] != "canada" {
			t.Errorf("%s 应该属于 canada,实际 %q", dc, dcRegion[dc])
		}
	}
	europe := []string{"DE", "EU-SOUTH-MIL", "EU-WEST-RBX", "GRA", "SBG", "UK", "WAW"}
	for _, dc := range europe {
		if dcRegion[dc] != "europe" {
			t.Errorf("%s 应该属于 europe,实际 %q", dc, dcRegion[dc])
		}
	}
	for _, dc := range []string{"US-EAST-VA", "US-WEST-OR"} {
		if dcRegion[dc] != "united_states" {
			t.Errorf("%s 应该属于 united_states,实际 %q", dc, dcRegion[dc])
		}
	}
}
