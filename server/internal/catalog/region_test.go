package catalog

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/ovh"
)

// 取自真实目录(2026-08 抓的 /order/catalog/public/eco):
// US 子公司的 plan 只有 united_states;欧区是 canada/europe,亚太机型归 canada。
const usCatalogFixture = `{"plans":[
 {"planCode":"24sk202-us","configurations":[
   {"name":"dedicated_datacenter","values":["vin","hil"]},
   {"name":"dedicated_os","values":["none_64.en"]},
   {"name":"region","values":["united_states"]}]},
 {"planCode":"24sk202-eu","configurations":[
   {"name":"dedicated_datacenter","values":["fra","gra","lon","rbx","sbg","waw"]},
   {"name":"region","values":["united_states"]}]}]}`

const euCatalogFixture = `{"plans":[
 {"planCode":"24adv01-v3","configurations":[
   {"name":"dedicated_datacenter","values":["bhs","fra","gra","lon","rbx","sbg","waw"]},
   {"name":"region","values":["canada","europe"]}]},
 {"planCode":"24adv01-v3-sgp","configurations":[
   {"name":"dedicated_datacenter","values":["sgp"]},
   {"name":"region","values":["canada"]}]},
 {"planCode":"noRegionPlan","configurations":[
   {"name":"dedicated_datacenter","values":["gra"]}]}]}`

func TestParseEcoCatalog(t *testing.T) {
	plans, err := parseEcoCatalog(strings.NewReader(usCatalogFixture))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("plan 数 = %d, 期望 2", len(plans))
	}
	pc := plans["24sk202-us"]
	if len(pc.regions) != 1 || pc.regions[0] != "united_states" {
		t.Errorf("24sk202-us 的 region = %v, 期望 [united_states]", pc.regions)
	}
	if len(pc.datacenters) != 2 {
		t.Errorf("24sk202-us 的机房 = %v, 期望 2 个", pc.datacenters)
	}
}

func TestPickRegion(t *testing.T) {
	us, _ := parseEcoCatalog(strings.NewReader(usCatalogFixture))
	eu, _ := parseEcoCatalog(strings.NewReader(euCatalogFixture))

	cases := []struct {
		name  string
		plans map[string]planConfig
		plan  string
		dc    string
		want  string
	}{
		// 美区:不管机房在美国还是欧洲,都必须是 united_states。
		// 老代码对 vin/hil 发 "usa"、对 gra 发 "europe",两者在美区目录里都不存在
		{"美区-美国机房", us, "24sk202-us", "vin", "united_states"},
		{"美区-欧洲机房", us, "24sk202-eu", "gra", "united_states"},
		// 欧区:多候选时按机房挑
		{"欧区-欧洲机房", eu, "24adv01-v3", "gra", "europe"},
		{"欧区-加拿大机房", eu, "24adv01-v3", "bhs", "canada"},
		// 亚太机型在目录里归 canada,不是 "apac"
		{"欧区-新加坡机型", eu, "24adv01-v3-sgp", "sgp", "canada"},
		// 没有 region 配置项的 plan 不应该发这一项
		{"无 region 配置", eu, "noRegionPlan", "gra", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pickRegion(c.plans[c.plan], c.dc)
			if got != c.want {
				t.Errorf("pickRegion(%s@%s) = %q, 期望 %q", c.plan, c.dc, got, c.want)
			}
		})
	}
}

func TestRegionBucketForDC(t *testing.T) {
	for dc, want := range map[string]string{
		"gra": "europe", "rbx": "europe", "sbg": "europe", "fra": "europe",
		"eu-west-par-a": "europe",
		"bhs":           "canada", "yyz": "canada",
		"sgp": "canada", "syd": "canada", "ynm": "canada", // 亚太归 canada 桶
		"vin": "united_states", "hil": "united_states",
	} {
		if got := regionBucketForDC(dc); got != want {
			t.Errorf("regionBucketForDC(%q) = %q, 期望 %q", dc, got, want)
		}
	}
}

// 静态兜底(目录拉不到时用)的取值必须也是目录里真实存在的字符串
func TestRegionForDCInSubsidiaryFallback(t *testing.T) {
	if got := ovh.RegionForDCInSubsidiary("gra", "US"); got != "united_states" {
		t.Errorf("US 子公司的欧洲机房 = %q, 期望 united_states", got)
	}
	if got := ovh.RegionForDCInSubsidiary("vin", "US"); got != "united_states" {
		t.Errorf("US 子公司的美国机房 = %q, 期望 united_states", got)
	}
	if got := ovh.RegionForDCInSubsidiary("gra", "IE"); got != "europe" {
		t.Errorf("欧区欧洲机房 = %q, 期望 europe", got)
	}
	if got := ovh.RegionForDCInSubsidiary("sgp", "IE"); got != "canada" {
		t.Errorf("欧区新加坡机房 = %q, 期望 canada(目录如此)", got)
	}
	for _, bad := range []string{"usa", "apac"} {
		for _, dc := range []string{"vin", "hil", "sgp", "syd", "ynm", "gra"} {
			for _, sub := range []string{"US", "IE", "FR", "CA"} {
				if got := ovh.RegionForDCInSubsidiary(dc, sub); got == bad {
					t.Errorf("%s@%s 返回了目录里不存在的 %q", dc, sub, bad)
				}
			}
		}
	}
}

// 联网校验:直接打官方公开目录,确认我们对 region 的理解没有过期。
// go test -short 会跳过。
func TestLiveCatalogRegions(t *testing.T) {
	if testing.Short() {
		t.Skip("联网测试,-short 跳过")
	}
	client := &http.Client{Timeout: 90 * time.Second}
	for _, tc := range []struct {
		subsidiary string
		wantOnly   string // 该子公司下所有 plan 的 region 都应只含这个值(空=不校验)
	}{
		{"US", "united_states"},
		{"IE", ""},
	} {
		resp, err := client.Get(ovh.CatalogBaseURLForSubsidiary(tc.subsidiary) +
			"/v1/order/catalog/public/eco?ovhSubsidiary=" + tc.subsidiary)
		if err != nil {
			t.Skipf("拉取 %s 目录失败(网络问题): %v", tc.subsidiary, err)
		}
		plans, err := parseEcoCatalog(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("解析 %s 目录失败: %v", tc.subsidiary, err)
		}
		if len(plans) == 0 {
			t.Fatalf("%s 目录里一个 plan 都没有", tc.subsidiary)
		}
		seen := map[string]bool{}
		for _, pc := range plans {
			for _, r := range pc.regions {
				seen[r] = true
			}
		}
		t.Logf("%s 目录 %d 个 plan,region 取值集合 = %v", tc.subsidiary, len(plans), keysOf(seen))
		if tc.wantOnly != "" {
			for r := range seen {
				if r != tc.wantOnly {
					t.Errorf("%s 子公司出现了预期外的 region %q(期望只有 %q)", tc.subsidiary, r, tc.wantOnly)
				}
			}
		}
		// 任何子公司都不该出现我们曾经硬编码的这两个值
		for _, bad := range []string{"usa", "apac"} {
			if seen[bad] {
				t.Errorf("%s 子公司目录里出现了 %q —— 与本次修复的前提冲突", tc.subsidiary, bad)
			}
		}

		// 穷举:每个 plan × 每个机房,我们挑出来的 region 必须在官方允许值里。
		// 这是整条下单链路上最容易悄悄错的一步 —— 错了就是每单都卡在 configuration。
		checked := 0
		for planCode, pc := range plans {
			for _, dc := range pc.datacenters {
				got := pickRegion(pc, dc)
				if len(pc.regions) == 0 {
					if got != "" {
						t.Errorf("%s %s@%s: plan 没有 region 配置却挑出了 %q", tc.subsidiary, planCode, dc, got)
					}
					continue
				}
				ok := false
				for _, r := range pc.regions {
					if r == got {
						ok = true
						break
					}
				}
				if !ok {
					t.Errorf("%s %s@%s: 挑出 %q,不在官方允许值 %v 里", tc.subsidiary, planCode, dc, got, pc.regions)
				}
				checked++
			}
		}
		t.Logf("%s: 穷举校验 %d 个 (plan, 机房) 组合全部通过", tc.subsidiary, checked)
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
