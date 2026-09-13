package types

import (
	"strings"
	"testing"
)

// 这些输入全都来自"人在手机上打字",不是构造出来的极端情况。
// 以前只切半角逗号,下面前四条各自都会静默产出一个匹配不上任何东西的值。
func TestSplitListHandlesRealWorldTyping(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"半角逗号", "gra,bhs", []string{"gra", "bhs"}},
		{"全角逗号(中文输入法默认)", "gra，bhs", []string{"gra", "bhs"}},
		{"顿号", "gra、bhs", []string{"gra", "bhs"}},
		{"全角分号", "gra；bhs", []string{"gra", "bhs"}},
		{"逗号后带空格", "gra, bhs", []string{"gra", "bhs"}},
		{"全角空格", "gra,　bhs", []string{"gra", "bhs"}},
		{"不换行空格(网页复制)", "gra, bhs", []string{"gra", "bhs"}},
		{"换行粘贴", "gra\nbhs\n", []string{"gra", "bhs"}},
		{"半全角混用", "gra，bhs,waw、sbg", []string{"gra", "bhs", "waw", "sbg"}},
		{"多余分隔符", ",,gra,,bhs,,", []string{"gra", "bhs"}},
		{"首尾空白", "  gra , bhs  ", []string{"gra", "bhs"}},
		{"单个值", "gra", []string{"gra"}},
		{"空串", "", nil},
		{"只有分隔符", "，、,", nil},
		{"只有空白", "   　 ", nil},
	}
	for _, c := range cases {
		got := SplitList(c.in)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: SplitList(%q) = %v,期望 %v", c.name, c.in, got, c.want)
		}
	}
}

// 值本身带连字符和数字(addon planCode 就长这样)不能被切碎
func TestSplitListKeepsPlanCodesIntact(t *testing.T) {
	got := SplitList("ram-64g-noecc-2133，softraid-2x960ssd")
	want := []string{"ram-64g-noecc-2133", "softraid-2x960ssd"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("得到 %v,期望 %v", got, want)
	}
}
