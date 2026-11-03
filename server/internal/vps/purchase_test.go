package vps

import (
	"encoding/json"
	"testing"
)

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

// 用户没选系统也必须提交 vps_os:checkout 缺它就 400,不存在"OVH 默认镜像"
func TestBuildVPSConfig_没选系统就挑一个(t *testing.T) {
	req := []requiredItem{
		{Label: "vps_datacenter", Required: true},
		{Label: "vps_os", AllowedValues: []string{"Ubuntu 24.04", "Debian 12"}},
	}
	got := labels(buildVPSConfig(req, "GRA", ""))
	if got["vps_os"] != "Debian 12" {
		t.Errorf("没选系统应按偏好挑 Debian 12,实际 %q", got["vps_os"])
	}
	req[1].AllowedValues = []string{"Fedora 44", "Rocky Linux 9"}
	got = labels(buildVPSConfig(req, "GRA", ""))
	if got["vps_os"] != "Fedora 44" {
		t.Errorf("偏好都没有时应取第一个,实际 %q", got["vps_os"])
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

// 2027 系列的 automatedBackup / storage 是必选家族:每个家族挑同计价方式下最便宜的,
// os 家族交给 vps_os,非必选的(snapshot)一个都不加
func TestPickMandatoryAddons(t *testing.T) {
	var opts []vpsOption
	if err := json.Unmarshal([]byte(`[
	 {"family":"automatedBackup","mandatory":true,"planCode":"backup-7","prices":[
	  {"duration":"P1M","pricingMode":"default","capacities":["renew"],"price":{"value":1.1}}]},
	 {"family":"automatedBackup","mandatory":true,"planCode":"backup-1","prices":[
	  {"duration":"P0D","pricingMode":"default","capacities":["installation"],"price":{"value":0}},
	  {"duration":"P1M","pricingMode":"default","capacities":["renew"],"price":{"value":0.35}}]},
	 {"family":"storage","mandatory":true,"planCode":"storage-remote","prices":[
	  {"duration":"P1M","pricingMode":"default","capacities":["renew"],"price":{"value":0}}]},
	 {"family":"os","mandatory":true,"planCode":"option-linux","prices":[
	  {"duration":"P1M","pricingMode":"default","capacities":["renew"],"price":{"value":0}}]},
	 {"family":"snapshot","mandatory":false,"planCode":"snap","prices":[
	  {"duration":"P1M","pricingMode":"default","capacities":["renew"],"price":{"value":0.3}}]}
	]`), &opts); err != nil {
		t.Fatal(err)
	}
	picks, missing := pickMandatoryAddons(opts, "P1M", "default")
	if len(missing) != 0 {
		t.Fatalf("不该有缺失家族: %v", missing)
	}
	got := map[string]string{}
	for _, p := range picks {
		got[p.family] = p.planCode
	}
	want := map[string]string{"automatedBackup": "backup-1", "storage": "storage-remote"}
	if len(got) != len(want) || got["automatedBackup"] != "backup-1" || got["storage"] != "storage-remote" {
		t.Errorf("挑选结果不对: %v", got)
	}

	// 计价方式对不上 → 报缺失,而不是硬塞一个会被 checkout 拒的
	_, missing = pickMandatoryAddons(opts, "P1Y", "upfront12")
	if len(missing) != 2 {
		t.Errorf("P1Y 下两个必选家族都该缺失,实际 %v", missing)
	}
}
