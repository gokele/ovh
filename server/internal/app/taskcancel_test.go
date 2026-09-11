package app

import (
	"context"
	"testing"
)

// 删任务必须能中断正在进行的下单。
//
// 以前 DeletedTaskIDs 只是个标记,处理器下一轮才会看;而一轮下单链路有 10 次
// OVH 调用、每次最长 60s。用户在"有货"通知弹出后两秒内点删除,单照样下出去。
// 这里测的是登记 / 标记删除 / 注销三者之间的时序契约。
func TestMarkTaskDeletedCancelsInFlightPurchase(t *testing.T) {
	s := &State{DeletedTaskIDs: map[string]struct{}{}}

	ctx, cancel := context.WithCancel(context.Background())
	s.RegisterTaskCancel("t1", cancel)
	if ctx.Err() != nil {
		t.Fatal("登记不该触发取消")
	}

	s.MarkTaskDeleted("t1")
	if ctx.Err() != context.Canceled {
		t.Fatalf("删任务后 ctx 应已取消,实际 %v", ctx.Err())
	}
	if !s.IsTaskDeleted("t1") {
		t.Fatal("IsTaskDeleted 应为 true")
	}
	// 处理器 defer 里的注销此时是空操作,不能 panic
	s.UnregisterTaskCancel("t1")
}

// 登记之前就被删:MarkTaskDeleted 找不到 cancel,ctx 不会被取消 ——
// 所以处理器登记完必须再查一次 IsTaskDeleted。这个测试把那条契约钉死。
func TestDeleteBeforeRegisterRequiresRecheck(t *testing.T) {
	s := &State{DeletedTaskIDs: map[string]struct{}{}}

	s.MarkTaskDeleted("t2")

	ctx, cancel := context.WithCancel(context.Background())
	s.RegisterTaskCancel("t2", cancel)
	if ctx.Err() != nil {
		t.Fatal("之前的删除标记不该回溯取消后来才登记的 ctx(那是处理器复核的职责)")
	}
	if !s.IsTaskDeleted("t2") {
		t.Fatal("复核必须能看到删除标记,否则这一轮会照跑")
	}
	s.UnregisterTaskCancel("t2")
	if ctx.Err() != context.Canceled {
		t.Fatal("注销必须释放 ctx,否则每轮泄漏一个")
	}
}

// 正常跑完一轮:注销即释放,之后再删也不能 panic。
func TestUnregisterReleasesAndLaterDeleteIsNoop(t *testing.T) {
	s := &State{DeletedTaskIDs: map[string]struct{}{}}
	ctx, cancel := context.WithCancel(context.Background())
	s.RegisterTaskCancel("t3", cancel)
	s.UnregisterTaskCancel("t3")
	if ctx.Err() != context.Canceled {
		t.Fatal("注销后 ctx 应已释放")
	}
	s.MarkTaskDeleted("t3") // 不能 panic
	if !s.IsTaskDeleted("t3") {
		t.Fatal("删除标记仍应写入")
	}
}
