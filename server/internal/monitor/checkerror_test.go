package monitor

import (
	"testing"
	"time"
)

// Issue #2 第 3 条:持续失败的型号不得每轮先报"已恢复"再报同样的错。
// 错误状态只在真正拿到库存后清除;beginCheck 不再提前清空。
func TestCheckErrorNotClearedAtRoundStart(t *testing.T) {
	s := &Subscription{PlanCode: "24sk602"}
	s.setCheckError("EU 站点没有 24sk602 的任何可用性记录")

	// 一轮开始:prevErr 带出上一轮错误,但 LastCheckError 必须保留
	prev := s.beginCheck(time.Now().Format(time.RFC3339), "acc1", "EU", "IE")
	if prev == "" {
		t.Fatal("beginCheck 应返回上一轮错误")
	}
	if s.LastCheckError == "" {
		t.Fatal("beginCheck 提前清空了 LastCheckError —— 这正是 issue #2 的根因:" +
			"清空后\"已恢复\"会在查询前被记录,持续失败每轮\"恢复→又失败\"刷屏")
	}

	// 显式清除:只在真正成功拿到库存后调用
	s.clearCheckError()
	if s.LastCheckError != "" {
		t.Fatal("clearCheckError 没清掉")
	}
}
