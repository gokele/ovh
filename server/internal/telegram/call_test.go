package telegram

import (
	"strings"
	"testing"
)

// 截断必须按字符,不能按字节。
//
// 这些消息基本都是中文:一个汉字 3 字节。按字节切会把一个字劈成两半,
// Telegram 收到的是半个 UTF-8 序列,发出去就是乱码 —— 而"乱码"和"消息丢了"
// 一样都是用户看不懂的失败,只是更难查。
func TestTruncateRunesCutsByCharacterNotByte(t *testing.T) {
	cn := strings.Repeat("中", 100) // 100 字符 / 300 字节
	got := truncateRunes(cn, 10)
	if n := len([]rune(got)); n != 10 {
		t.Fatalf("应截成 10 个字符,实际 %d", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatal("截断后要有省略号,否则用户不知道内容没说完")
	}
	// 关键:结果必须是合法 UTF-8,不能有半个字
	if !isValidUTF8(got) {
		t.Fatal("截断产生了非法 UTF-8 —— 按字节切的典型症状")
	}

	// 没超长的原样返回,不该平白加省略号
	if got := truncateRunes("短消息", 100); got != "短消息" {
		t.Fatalf("没超长不该改动,实际 %q", got)
	}
	// 边界:正好等于上限
	if got := truncateRunes("12345", 5); got != "12345" {
		t.Fatalf("正好等于上限不该截,实际 %q", got)
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == 0xFFFD { // RuneError
			return false
		}
	}
	return true
}

// 两个上限必须和官方文档一致。
//
// 写错的后果都是静默的:
//   - sendMessage 超 4096 → Telegram 400,消息根本发不出去。走这条路的是
//     补货通知,用户会直接错过一波货,而且不知道曾经有过货。
//   - answerCallbackQuery 超 200 → 整个应答被拒,表现是用户点了按钮、
//     转圈半天没有任何提示,而按钮其实已经生效了。
func TestTelegramLimitsMatchOfficialDocs(t *testing.T) {
	// https://core.telegram.org/bots/api#sendmessage
	if MaxMessageRunes != 4096 {
		t.Errorf("sendMessage 上限应为 4096 字符,当前 %d", MaxMessageRunes)
	}
	// https://core.telegram.org/bots/api#answercallbackquery —— "0-200 characters"
	if MaxCallbackAnswerRunes != 200 {
		t.Errorf("answerCallbackQuery 上限应为 200 字符,当前 %d", MaxCallbackAnswerRunes)
	}
	// getUpdates 的 limit 官方是 1-100
	if PollLimit < 1 || PollLimit > 100 {
		t.Errorf("getUpdates 的 limit 必须在 1-100,当前 %d —— 超出会被 Telegram 拒绝", PollLimit)
	}
}
