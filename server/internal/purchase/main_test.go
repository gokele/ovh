package purchase

import (
	"os"
	"testing"
)

// 测试里结果不明的通知立即发出,避免 3 秒后的延迟发送跑到触发它的测试结束之后。
// 合并行为本身由 notice_batch_test.go 用独立的 batcher 覆盖。
func TestMain(m *testing.M) {
	notices.window = 0
	os.Exit(m.Run())
}
