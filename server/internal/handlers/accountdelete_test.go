package handlers

import (
	"context"
	"testing"

	"github.com/ovh-buy/server/internal/types"
)

// 删账户必须掐掉那个账户正在跑的抢购。
//
// 删任务有四个入口(网页删单条 / 清空队列 / TG /cancel / 删账户级联),
// 前三个都调了 MarkTaskDeleted,只有删账户这条漏了 —— 它直接拿数据库的新列表
// 覆盖 state.Queue 就完事。
//
// 漏掉不会有任何报错,因为队列处理器那条"任务还在不在队列里"的复核
// **兜不住它**:那条复核遍历的是 state.Queue 的快照,而任务已经不在快照里了,
// 循环根本轮不到它。于是 PurchaseServer 会拿着已删账户的凭据,
// 把建车 → 加购 → 配置 → 结账整条链路跑完,一路 401/403。
func TestAccountDeleteCancelsInFlightPurchase(t *testing.T) {
	st, _ := newWatchTestMonitor(t)

	const doomed, survivor = "task-of-deleted-acct", "task-of-other-acct"
	keep := []types.QueueItem{
		{ID: survivor, AccountID: "acc-keep", PlanCode: "24sk602", Datacenter: "rbx", Status: "running"},
	}
	st.Queue = append([]types.QueueItem{
		{ID: doomed, AccountID: "acc-del", PlanCode: "24sk602", Datacenter: "gra", Status: "running"},
	}, keep...)
	// 级联删除已经把 acc-del 的行从库里删了,库里只剩另一个账户的
	if err := st.DB.ReplaceQueue(keep); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}

	// doomed 正跑在 PurchaseServer 里:登记它这一轮的 cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st.RegisterTaskCancel(doomed, cancel)

	reloadAfterAccountDelete(st, "acc-del")

	select {
	case <-ctx.Done():
	default:
		t.Fatal("删账户后,它正在进行的下单没有被取消 —— " +
			"这条协程会拿着已删账户的凭据把结账链路跑完")
	}
	if !st.IsTaskDeleted(doomed) {
		t.Error("被级联删掉的任务没有被标记删除")
	}

	// 别的账户的任务不能受牵连:它还在队列里,更不该被标记成已删
	if st.IsTaskDeleted(survivor) {
		t.Error("误伤了其它账户的任务")
	}
	if len(st.Queue) != 1 || st.Queue[0].ID != survivor {
		t.Errorf("内存队列应只剩另一个账户的那条,实际 %+v", st.Queue)
	}
}
