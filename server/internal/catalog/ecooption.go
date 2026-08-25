package catalog

import "strings"

// MatchEcoOption 把用户/FQN 给的 addon 码匹配到 OVH /order/cart/{id}/eco/options
// 返回的那批可选项上,返回命中的选项、它的完整 planCode、以及命中档次(日志用)。
//
// **这个函数直接决定账单**:选错 addon 就是下单到别的配置、付另一个价钱
// (实测 26sk50a-v1 上,纯 2x960NVMe 是 €0/月,而多编了一套混合盘的那条是 €24/月)。
//
// 以前询价(price)和抢购(purchase)各存一份逐字复制的实现,两处一旦分裂就会出现
// "询价报 €70、实际扣 €94"这种最难查的问题 —— 所以收敛到这里一份。
//
// 分四档、同档取最优,而不是"第一个匹配就用":
//   - 同一个 plan 里存在互为前缀的存储配置(softraid-2x960nvme 与
//     softraid-2x960nvme-2x6000sa),先撞上哪条取决于 OVH 返回顺序;
//   - 原始码档必须排在标准化档前面:StandardizeConfig 会把机型后缀吃成粘连残渣
//     (…-26sk50a-v1 → …nvmea),正确项因此丢掉分隔符、反而落到比错误项更低的档。

func MatchEcoOption(opts []map[string]interface{}, wanted string) (map[string]interface{}, string, string) {
	w := strings.ToLower(strings.TrimSpace(wanted))
	if w == "" {
		return nil, "", ""
	}
	wStd := StandardizeConfig(w)

	type cand struct {
		opt      map[string]interface{}
		code     string
		strength int // 同档内:命中的公共前缀越长越可信
		size     int // 同档同强度:整体越短越可信(多出来的段=多编了一套配置)
	}
	tierNames := [4]string{"原样相等", "原始码前缀", "标准化相等", "标准化前缀"}
	var tiers [4]*cand

	for _, o := range opts {
		code, _ := o["planCode"].(string)
		c := strings.ToLower(strings.TrimSpace(code))
		if c == "" {
			continue
		}
		cStd := StandardizeConfig(c)
		tier, strength := -1, 0
		switch {
		case c == w:
			tier, strength = 0, len(c)
		case strings.HasPrefix(c, w+"-"):
			tier, strength = 1, len(w)
		case strings.HasPrefix(w, c+"-"):
			tier, strength = 1, len(c)
		case cStd != "" && cStd == wStd:
			tier, strength = 2, len(cStd)
		case cStd != "" && wStd != "" && strings.HasPrefix(cStd, wStd):
			tier, strength = 3, len(wStd)
		case cStd != "" && wStd != "" && strings.HasPrefix(wStd, cStd):
			tier, strength = 3, len(cStd)
		}
		if tier < 0 {
			continue
		}
		cur := tiers[tier]
		if cur == nil || strength > cur.strength || (strength == cur.strength && len(c) < cur.size) {
			tiers[tier] = &cand{opt: o, code: code, strength: strength, size: len(c)}
		}
	}
	for i, t := range tiers {
		if t != nil {
			return t.opt, t.code, tierNames[i]
		}
	}
	return nil, "", ""
}
