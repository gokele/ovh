package types

import "strings"

// SplitList 把用户手打的一串"用逗号分隔"的值切开。
//
// 为什么不能直接 strings.Split(s, ","):这些输入框里的内容全是人打的,
// 而中文输入法默认打出来的是**全角**标点。用户输入 `gra，bhs`,
// 半角切法会得到一个词 "gra，bhs" —— 它当然匹配不上任何机房,
// 而且不会有任何报错:订阅照样建,只是永远不触发。
//
// 踩点最深的是 Telegram 管理员白名单:那串 chat ID 打成全角逗号的话,
// 整条白名单失效,用户会把自己锁在机器人外面,且完全不知道为什么。
//
// 认这些分隔符:半角/全角逗号、顿号、半角/全角分号、换行、制表符。
// 不按空格切 —— 空格更可能是用户在逗号后面顺手敲的,TrimSpace 处理掉即可。
func SplitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ',', '，', '、', ';', '；', '\n', '\r', '\t':
			return true
		}
		return false
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		// TrimSpace 按 unicode.IsSpace 判断,全角空格 U+3000 和不换行空格 U+00A0
		// (从网页复制粘贴时很常见)都会被去掉
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
