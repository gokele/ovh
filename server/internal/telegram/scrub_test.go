package telegram

import (
	"strings"
	"testing"
)

// Bot Token 绝不能进日志。
//
// Token 能冒充你发通知、能读群消息、能通过按钮回调触发下单 —— config.go
// 专门为它做了加密落库,而日志是明文的、还能通过 GET /api/logs 在前端读到。
// 一条泄漏就把加密那层绕过去了。
//
// 泄漏路径不是"有人手写了 Logger.Info(token)",而是**错误信息**:
// Go 的 http 错误会把完整 URL 带进 Error(),而 URL 里就有 token:
//
//	Post "https://api.telegram.org/bot<完整TOKEN>/sendMessage": dial tcp ...
//
// 这类错误在网络差的部署里是常态,所以每一条往日志走的错误都必须过 scrub。
func TestScrubRemovesBotToken(t *testing.T) {
	// 故意用一串短的假 token:仓库的提交钩子按 [0-9]{8,10}:[A-Za-z0-9_-]{35}
	// 扫机密,用真实长度的样例会被它拦下(实测拦过一次)。
	// 而 scrub 的正则是 /bot[0-9]+:[A-Za-z0-9_-]+ —— 不要求 35 位,
	// 所以短样例照样能验证替换逻辑。
	const token = "123:FAKE_NOT_A_REAL_TOKEN"

	cases := []string{
		`Post "https://api.telegram.org/bot` + token + `/sendMessage": dial tcp 149.154.167.220:443: i/o timeout`,
		`Get "https://api.telegram.org/bot` + token + `/getUpdates?offset=12": context deadline exceeded`,
		`https://api.telegram.org/bot` + token + `/answerCallbackQuery`,
	}
	for _, in := range cases {
		out := scrub(in)
		if strings.Contains(out, token) {
			t.Errorf("完整 token 泄漏了:\n  输入: %s\n  输出: %s", in, out)
		}
		// 部分泄漏同样危险:token 的前半段(bot id)加上一次暴力枚举就够用了
		if strings.Contains(out, strings.Split(token, ":")[1]) {
			t.Errorf("token 的密钥段泄漏了: %s", out)
		}
		if !strings.Contains(out, "/bot***") {
			t.Errorf("没有替换成占位符,可能是正则没匹配上: %s", out)
		}
	}

	// 不含 token 的错误要原样保留 —— 过度脱敏会把有用的排查信息也抹掉
	plain := "dial tcp: lookup api.telegram.org: no such host"
	if scrub(plain) != plain {
		t.Errorf("不含 token 的信息被改动了: %q", scrub(plain))
	}
}
