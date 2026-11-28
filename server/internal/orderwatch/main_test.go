package orderwatch

import (
	"os"
	"testing"
)

// 生命周期测试在同一时刻连续调用 RunOnce:节流与新订单等待期在这里关掉,
// 各自的行为由专门的测试临时打开验证。
func TestMain(m *testing.M) {
	listInterval, quietPollInterval, newOrderGrace = 0, 0, 0
	os.Exit(m.Run())
}
