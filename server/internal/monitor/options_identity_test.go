package monitor

import (
	"path/filepath"
	"testing"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/storage"
)

// 回归:订阅身份 = planCode + 配置组合(2026-10-12 用户报障:
// 同型号只差内存的第二条订阅把第一条覆盖了)。
// - 同 planCode 不同 options → 两条独立订阅
// - 同 planCode 同 options    → 就地更新,不新增
func TestSubscriptionIdentityByOptions(t *testing.T) {
	dir := t.TempDir()
	st := app.NewState(storage.Paths{LogsDir: filepath.Join(dir, "logs")}, nil, logger.New(filepath.Join(dir, "logs.json"), nil), nil)
	m := New(st)

	m.AddSubscription("26sk50a-v1", nil, true, false, "SK-A", nil, nil, false, 0, "", nil, "") // 盯全部
	m.AddSubscription("26sk50a-v1", []string{"ram-32g-ecc-1333"}, true, false, "SK-A", nil, nil, false, 0, "", []string{"ram-32g-ecc-1333"}, "")
	m.AddSubscription("26sk50a-v1", []string{"ram-64g-ecc-1333"}, true, false, "SK-A", nil, nil, false, 0, "", []string{"ram-64g-ecc-1333"}, "")

	if got := len(m.Snapshot()); got != 3 {
		t.Fatalf("同型号不同配置应为 3 条订阅(盯全部/32G/64G),实际 %d", got)
	}

	// 相同组合再添加 → 仍 3 条,就地更新
	m.AddSubscription("26sk50a-v1", []string{"ram-32g-ecc-1333"}, false, true, "SK-A", nil, nil, false, 0, "", []string{"ram-32g-ecc-1333"}, "")
	if got := len(m.Snapshot()); got != 3 {
		t.Fatalf("同组合重复添加不应新增,实际 %d 条", got)
	}

	// 精确删除:只删 32G 那条
	if !m.RemoveSubscriptionExact("26sk50a-v1", OptionsKey([]string{"ram-32g-ecc-1333"})) {
		t.Fatal("精确删除应命中")
	}
	if got := len(m.Snapshot()); got != 2 {
		t.Fatalf("精确删除后应剩 2 条,实际 %d", got)
	}
}
