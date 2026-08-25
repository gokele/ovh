package catalog

import "testing"

// 构造 /eco/options 的返回形状
func opts(codes ...string) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(codes))
	for _, c := range codes {
		out = append(out, map[string]interface{}{"planCode": c, "duration": "P1M"})
	}
	return out
}

// 这条是整个下单链路上唯一直接决定账单的匹配。
// 真实数据:26sk50a-v1 的 storage 选项里有两条互为前缀的,
// 纯 2x960NVMe 月费 €0,多编了一套混合盘的那条 €24 —— 选错就是每月多付 €24。
func TestMatchEcoOption_前缀歧义必须选最短(t *testing.T) {
	list := opts(
		"softraid-2x960nvme-2x6000sa-26sk50a-v1", // €24 —— 错误项
		"softraid-2x960nvme-26sk50a-v1",          // €0  —— 正确项
		"softraid-2x6000sa-26sk50a-v1",
	)
	for _, name := range []string{"正序", "逆序"} {
		in := list
		if name == "逆序" {
			in = []map[string]interface{}{list[2], list[1], list[0]}
		}
		_, code, tier := MatchEcoOption(in, "softraid-2x960nvme")
		if code != "softraid-2x960nvme-26sk50a-v1" {
			t.Errorf("%s输入: 命中 %q(档次 %s), 期望 softraid-2x960nvme-26sk50a-v1", name, code, tier)
		}
	}
}

// OVH 返回顺序不该影响结果 —— 以前是"第一个匹配就用",结果取决于接口返回顺序
func TestMatchEcoOption_与返回顺序无关(t *testing.T) {
	a := opts("ram-64g-ecc-2133-24sk20-us", "ram-32g-ecc-2133-24sk20-us")
	b := opts("ram-32g-ecc-2133-24sk20-us", "ram-64g-ecc-2133-24sk20-us")
	_, c1, _ := MatchEcoOption(a, "ram-64g-ecc-2133")
	_, c2, _ := MatchEcoOption(b, "ram-64g-ecc-2133")
	if c1 != c2 || c1 != "ram-64g-ecc-2133-24sk20-us" {
		t.Errorf("顺序影响了结果: %q vs %q", c1, c2)
	}
}

// 原样相等优先于任何前缀档
func TestMatchEcoOption_原样相等优先(t *testing.T) {
	list := opts("ram-64g-ecc-2133-24sk20-us", "ram-64g-ecc-2133")
	_, code, tier := MatchEcoOption(list, "ram-64g-ecc-2133")
	if code != "ram-64g-ecc-2133" || tier != "原样相等" {
		t.Errorf("命中 %q(档次 %s), 期望原样相等命中 ram-64g-ecc-2133", code, tier)
	}
}

// 内存频率对不上时(可用性报 ecc-2933、目录只卖 ecc-3200)要靠标准化档认出来,
// 否则整单会因为"配不出这套内存"被取消
func TestMatchEcoOption_频率不一致靠标准化兜底(t *testing.T) {
	list := opts("ram-1024g-ecc-3200-24rise06-v1", "ram-512g-ecc-3200-24rise06-v1")
	_, code, _ := MatchEcoOption(list, "ram-1024g-ecc-2933")
	if code != "ram-1024g-ecc-3200-24rise06-v1" {
		t.Errorf("命中 %q, 期望 ram-1024g-ecc-3200-24rise06-v1", code)
	}
}

// 标准化兜底不能跨容量乱配:32g 绝不能配到 64g 上
func TestMatchEcoOption_不跨容量错配(t *testing.T) {
	list := opts("ram-64g-ecc-3200-24rise06-v1")
	if _, code, _ := MatchEcoOption(list, "ram-32g-ecc-3200"); code != "" {
		t.Errorf("32G 不该匹配到 %q", code)
	}
}

// 目录里确实没有这一项时必须返回空,而不是硬凑一个 —— 硬凑就是下错配置
func TestMatchEcoOption_无匹配返回空(t *testing.T) {
	list := opts("ram-64g-ecc-2133-24sk20", "softraid-2x450nvme-24sk20")
	if _, code, _ := MatchEcoOption(list, "softraid-4x8000sa"); code != "" {
		t.Errorf("不该匹配到 %q", code)
	}
}

// 空输入不能 panic
func TestMatchEcoOption_边界(t *testing.T) {
	if _, c, _ := MatchEcoOption(nil, "x"); c != "" {
		t.Error("nil 选项列表应返回空")
	}
	if _, c, _ := MatchEcoOption(opts("a"), ""); c != "" {
		t.Error("空 wanted 应返回空")
	}
	if _, c, _ := MatchEcoOption(opts("", "  "), "a"); c != "" {
		t.Error("空 planCode 应被跳过")
	}
}
