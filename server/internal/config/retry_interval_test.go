package config

import (
	"testing"

	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/types"
)

// 重试间隔为 0 时处理器会每秒重试一次(就绪判断 now-last >= 0 恒真)。
// 这个值可能来自三处:老配置没有这个字段、前端留空提交、旧库里的任务行。
// 前两处在这里兜,第三处在处理器里兜(ClampRetryInterval 是同一个函数)。
func TestRetryIntervalDefaultsAndClamp(t *testing.T) {
	// 纯函数:0/负数退默认,越界夹回
	cases := []struct{ in, fb, want int }{
		{0, 60, 60},
		{-5, 60, 60},
		{30, 60, 30},
		{0, 2, 2},
		{99999999, 60, types.MaxRetryInterval},
	}
	for _, c := range cases {
		if got := types.ClampRetryInterval(c.in, c.fb); got != c.want {
			t.Fatalf("Clamp(%d, %d) = %d, 期望 %d", c.in, c.fb, got, c.want)
		}
	}

	// 老配置(没有这两个字段)加载后必须给出明确的默认值,而不是 0
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// 模拟升级前存下的配置:只有旧字段
	if err := database.SetKV("config", types.Config{Endpoint: "ovh-eu", Zone: "IE"}); err != nil {
		t.Fatal(err)
	}
	s := New(database)
	if got := s.RetryInterval(); got != types.DefaultTaskRetryInterval {
		t.Fatalf("老配置的默认间隔应为 %d,实际 %d —— 为 0 的话 TG/网页新建的任务会每秒重试",
			types.DefaultTaskRetryInterval, got)
	}
	if got := s.QuickOrderRetryInterval(); got != types.DefaultQuickRetryInterval {
		t.Fatalf("老配置的自动下单间隔应为 %d,实际 %d", types.DefaultQuickRetryInterval, got)
	}

	// 用户改了默认值,Get/Set 往返后 getter 要给新值
	cfg := s.Get()
	cfg.DefaultRetryInterval = 45
	if err := s.Set(cfg); err != nil {
		t.Fatal(err)
	}
	if got := New(database).RetryInterval(); got != 45 {
		t.Fatalf("重新加载后应为 45,实际 %d", got)
	}
}
