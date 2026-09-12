package handlers

import "testing"

// 订单号在 JSON 里可能是三种类型,都要认。
// 走映射缓存那条路时是 int64/float64,而 encoding/json 默认把数字解成 float64 ——
// 只认 int64 的话映射命中了也取不出订单号,静默退化成"没找到"。
func TestToOrderIDAcceptsJSONNumberForms(t *testing.T) {
	cases := []struct {
		in   interface{}
		want int64
		ok   bool
	}{
		{int64(108523471), 108523471, true},
		{float64(108523471), 108523471, true}, // encoding/json 的默认形态
		{"108523471", 108523471, true},
		{"", 0, false},
		{"abc", 0, false},
		{nil, 0, false},
		{true, 0, false},
	}
	for _, c := range cases {
		got, ok := toOrderID(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("toOrderID(%#v) = (%d, %v), 期望 (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
