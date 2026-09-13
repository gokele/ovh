package handlers

import "testing"

// 一台机器通常有好几个救援启动项(不同内核、pro / customer)。
// 挑错的后果不是报错,而是进了一个**不会把密码发到你邮箱**的救援系统 ——
// 机器重启了、SSH 也起来了,但你没有密码,只能再折腾一轮。
// 所以优先认 kernel 里带 customer 的那个(OVH 现在的标准客户救援系统)。
func TestPickRescueBootPrefersCustomer(t *testing.T) {
	boots := []map[string]interface{}{
		{"bootId": float64(1122), "kernel": "rescue64-pro"},
		{"bootId": float64(1156), "kernel": "rescue-customer"},
		{"bootId": float64(1199), "kernel": "rescue32-pro"},
	}
	id, kernel, ok := pickRescueBoot(boots)
	if !ok {
		t.Fatal("有候选项却没挑出来")
	}
	if id != 1156 {
		t.Errorf("应当挑 customer 那个(1156),实际 %d(%s)", id, kernel)
	}
}

// 没有 customer 时退回第一个,不能因为挑不到"最优"就整个失败 ——
// 失败的表现是"这台机器不支持救援",而实际上它支持。
func TestPickRescueBootFallsBackToFirst(t *testing.T) {
	boots := []map[string]interface{}{
		{"bootId": float64(900), "kernel": "rescue64-pro"},
		{"bootId": float64(901), "kernel": "rescue32-pro"},
	}
	id, _, ok := pickRescueBoot(boots)
	if !ok || id != 900 {
		t.Errorf("应当退回第一个(900),实际 %d ok=%v", id, ok)
	}
}

// 空列表要明确返回"没有",让调用方给出"这台机器没有救援启动项"的说法,
// 而不是发一个 bootId=0 出去(那是个不存在的启动项)
func TestPickRescueBootEmpty(t *testing.T) {
	if _, _, ok := pickRescueBoot(nil); ok {
		t.Error("空列表不该返回 ok")
	}
	if id, _, ok := pickRescueBoot([]map[string]interface{}{{"kernel": "x"}}); ok {
		t.Errorf("bootId 缺失时不该返回 ok,实际 id=%d", id)
	}
}
