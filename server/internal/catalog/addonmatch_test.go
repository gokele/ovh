package catalog

import "testing"

// 真实数据(2026-08 目录):26sk50a-v1 的 storage family 里有两条互为前缀的 addon,
// 一条是纯 2x960NVMe(月费 €0),另一条多编了一段混合盘(月费 €24)。
// 旧实现先标准化再比前缀,StandardizeConfig 把机型后缀吃成粘连残渣:
//
//	softraid-2x960nvme-26sk50a-v1          → softraid-2x960nvmea   (丢了分隔符,落到最低档)
//	softraid-2x960nvme-2x6000sa-26sk50a-v1 → softraid-2x960nvme-2x6000saa (仍有分隔符,抢先命中)
//
// 于是选纯 2x960 配置的用户会被下单到混合盘上,价格也对不上。
func TestMatchAddonsForSegment_PrefixAmbiguity(t *testing.T) {
	addons := []string{
		"softraid-2x960nvme-2x6000sa-26sk50a-v1",
		"softraid-2x960nvme-26sk50a-v1",
		"softraid-2x6000sa-26sk50a-v1",
	}
	cases := []struct {
		seg  string
		want string
	}{
		{"softraid-2x960nvme", "softraid-2x960nvme-26sk50a-v1"},
		{"softraid-2x960nvme-2x6000sa", "softraid-2x960nvme-2x6000sa-26sk50a-v1"},
		{"softraid-2x6000sa", "softraid-2x6000sa-26sk50a-v1"},
	}
	for _, c := range cases {
		got := matchAddonsForSegment(addons, c.seg, StandardizeConfig(c.seg))
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("段 %q → %v, 期望 [%s]", c.seg, got, c.want)
		}
	}
}

// 内存频率对不上时(可用性报 ecc-2933、目录只卖 ecc-3200)必须靠标准化那一档认出来
func TestMatchAddonsForSegment_FrequencyMismatch(t *testing.T) {
	addons := []string{"ram-1024g-ecc-3200-24rise06-v1", "ram-512g-ecc-3200-24rise06-v1"}
	got := matchAddonsForSegment(addons, "ram-1024g-ecc-2933", StandardizeConfig("ram-1024g-ecc-2933"))
	if len(got) != 1 || got[0] != "ram-1024g-ecc-3200-24rise06-v1" {
		t.Errorf("频率不一致时 → %v, 期望 [ram-1024g-ecc-3200-24rise06-v1]", got)
	}
}

// 原始码完全相等优先于任何前缀档
func TestMatchAddonsForSegment_ExactWins(t *testing.T) {
	addons := []string{"ram-64g-ecc-2133-24sk20-us", "ram-64g-ecc-2133"}
	got := matchAddonsForSegment(addons, "ram-64g-ecc-2133", StandardizeConfig("ram-64g-ecc-2133"))
	if len(got) != 1 || got[0] != "ram-64g-ecc-2133" {
		t.Errorf("原始相等应优先 → %v", got)
	}
}

// 美区 addon 带 -us 后缀时仍要能命中(原始前缀档)
func TestMatchAddonsForSegment_USSuffix(t *testing.T) {
	addons := []string{"ram-64g-ecc-2133-24sk20-us", "ram-32g-ecc-2133-24sk20-us"}
	got := matchAddonsForSegment(addons, "ram-64g-ecc-2133", StandardizeConfig("ram-64g-ecc-2133"))
	if len(got) != 1 || got[0] != "ram-64g-ecc-2133-24sk20-us" {
		t.Errorf("美区后缀应命中 → %v", got)
	}
}

// 段在目录里完全没有对应 addon 时必须返回空,不能硬凑一个
func TestMatchAddonsForSegment_NoMatch(t *testing.T) {
	addons := []string{"ram-64g-ecc-2133-24sk20", "softraid-2x450nvme-24sk20"}
	if got := matchAddonsForSegment(addons, "softraid-4x8000sa", StandardizeConfig("softraid-4x8000sa")); len(got) != 0 {
		t.Errorf("无对应 addon 时应返回空 → %v", got)
	}
}
