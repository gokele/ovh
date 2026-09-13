package telegram

import (
	"strings"
	"testing"
)

// 位置无关的解析。
//
// 这些写法以前全都**静默**失效:任务照样建、照样下单,只是机房/数量/配置
// 悄悄变回默认值,用户拿到的是基础配置的机器。没有报错、日志里也看不出异常 ——
// 用户唯一的反馈是"TG 上下单总是选不了配置"。
//
// 三个根因:配置必须带逗号才认;switch 只写了 case 1 / case 2,
// 三个参数直接掉进没有 default 的缝里;机房要求全小写。
func TestParseOrderMessageIsPositionIndependent(t *testing.T) {
	cases := []struct {
		in   string
		dc   string
		qty  int
		opts string // 逗号连接,便于比对
	}{
		// —— 以前会丢东西的写法 ——
		{"24ska01 gra softraid-2x960ssd", "gra", 1, "softraid-2x960ssd"},
		{"24ska01 gra 2 softraid-2x960ssd", "gra", 2, "softraid-2x960ssd"},
		{"24ska01 softraid-2x960ssd", "", 1, "softraid-2x960ssd"},
		{"24ska01 GRA 2", "gra", 2, ""},
		{"24ska01 Gra", "gra", 1, ""},
		// 空格分隔的多个配置(以前只认逗号)
		{"24ska01 gra 2 ram-64g softraid-2x960ssd", "gra", 2, "ram-64g,softraid-2x960ssd"},
		// —— 以前就能用的写法,不能改坏 ——
		{"24ska01", "", 1, ""},
		{"24ska01 bhs", "bhs", 1, ""},
		{"24ska01 2", "", 2, ""},
		{"24ska01 ram-64g,softraid-2x960ssd", "", 1, "ram-64g,softraid-2x960ssd"},
		{"24ska01 gra 2 ram-64g,softraid-2x960ssd", "gra", 2, "ram-64g,softraid-2x960ssd"},
		{"24ska01 2 gra", "gra", 2, ""},
		// 顺序任意
		{"24ska01 softraid-2x960ssd gra 2", "gra", 2, "softraid-2x960ssd"},
	}
	for _, c := range cases {
		got := ParseOrderMessage(c.in)
		if got == nil {
			t.Errorf("%q 解析失败", c.in)
			continue
		}
		opts := strings.Join(got.Options, ",")
		if got.Datacenter != c.dc || got.Quantity != c.qty || opts != c.opts {
			t.Errorf("%q\n  得到 dc=%q qty=%d opts=%q\n  期望 dc=%q qty=%d opts=%q",
				c.in, got.Datacenter, got.Quantity, opts, c.dc, c.qty, c.opts)
		}
	}
}

// 配置项绝不能被当成机房吃掉。
// addon planCode 一律带连字符和数字,和 3~4 纯字母的机房码不会撞 ——
// 撞了的话用户点"确认下单"时看到的机房是对的、配置却少了一项。
func TestParseOrderMessageOptionsAreNotMistakenForDatacenter(t *testing.T) {
	got := ParseOrderMessage("24ska01 ram-64g softraid-2x960ssd bandwidth-500-unguaranteed")
	if got.Datacenter != "" {
		t.Errorf("把配置当成了机房: %q", got.Datacenter)
	}
	if len(got.Options) != 3 {
		t.Errorf("应有 3 项配置,实际 %v", got.Options)
	}
}

// 裸 @ 是打漏了账户名,丢掉即可 —— 但绝不能落进 options。
// 发给 OVH 只会换来一个用户看不懂的 400。
func TestParseOrderMessageBareAtNeverBecomesOption(t *testing.T) {
	got := ParseOrderMessage("24sk602 @ gra")
	if got.AccountRef != "" {
		t.Errorf("裸 @ 不该被当成账户引用: %q", got.AccountRef)
	}
	for _, o := range got.Options {
		if strings.HasPrefix(o, "@") {
			t.Errorf("@ 落进了 options: %v", got.Options)
		}
	}
	if got.Datacenter != "gra" {
		t.Errorf("裸 @ 影响了后面的解析: dc=%q", got.Datacenter)
	}
}

// 数量只认第一个纯数字。第二个数字多半是配置里的型号被空格劈开了,
// 拿它当数量会把"抢 2 台"变成"抢 960 台"(虽然会被 clamp,但仍然不是用户要的)。
func TestParseOrderMessageOnlyFirstNumberIsQuantity(t *testing.T) {
	got := ParseOrderMessage("24ska01 gra 2 960")
	if got.Quantity != 2 {
		t.Errorf("数量应为 2,实际 %d", got.Quantity)
	}
	if strings.Join(got.Options, ",") != "960" {
		t.Errorf("第二个数字应落进 options,实际 %v", got.Options)
	}
}

// 中文输入法打出来的是全角标点。用户在手机上发 `24ska01 gra 2 ram-64g，softraid-2x960ssd`
// 时,以前会把整串配置当成**一个** addon planCode ——
// 匹配不上任何东西,而且不报错:单照下,只是配置悄悄没了。
// 这正是"TG 上下单总是选不了配置"的另一半原因。
func TestParseOrderMessageAcceptsFullWidthPunctuation(t *testing.T) {
	cases := []string{
		"24ska01 gra 2 ram-64g，softraid-2x960ssd", // 全角逗号
		"24ska01 gra 2 ram-64g、softraid-2x960ssd", // 顿号
		"24ska01　gra　2　ram-64g，softraid-2x960ssd", // 全角空格 + 全角逗号
	}
	for _, in := range cases {
		got := ParseOrderMessage(in)
		if got == nil {
			t.Errorf("%q 解析失败", in)
			continue
		}
		if got.Datacenter != "gra" || got.Quantity != 2 {
			t.Errorf("%q → dc=%q qty=%d,期望 gra/2", in, got.Datacenter, got.Quantity)
		}
		if strings.Join(got.Options, ",") != "ram-64g,softraid-2x960ssd" {
			t.Errorf("%q → opts=%v,期望两项拆开", in, got.Options)
		}
	}
}

// 数量写成 -1 / 3.5 这种:意图是数量、只是写得不合法,不能当成配置项发给 OVH
// (换回来的是一句用户看不懂的英文报错)。
func TestParseOrderMessageMalformedQuantityIsNotAnOption(t *testing.T) {
	for _, in := range []string{"24ska01 gra -1", "24ska01 gra +2", "24ska01 gra 3.5"} {
		got := ParseOrderMessage(in)
		if len(got.Options) != 0 {
			t.Errorf("%q → 写坏的数量落进了 options: %v", in, got.Options)
		}
		if got.Datacenter != "gra" {
			t.Errorf("%q → 机房被带偏: %q", in, got.Datacenter)
		}
	}
}
