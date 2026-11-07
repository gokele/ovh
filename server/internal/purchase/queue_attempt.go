package purchase

import (
	"context"
	"fmt"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/notify"
	"github.com/ovh-buy/server/internal/types"
)

// runQueueAttempt 只在内存登记尝试;结果返回后才持久化终态、历史和通知。
func runQueueAttempt(ctx context.Context, state *app.State, it types.QueueItem, purchase func(context.Context, *app.State, *types.QueueItem) Outcome) (outcome Outcome) {
	defer func() {
		phase := ""
		switch {
		case outcome.WaitingForQuota:
			phase = "waitingQuota"
		case outcome.WaitingForCatalog:
			phase = "waitingQuote"
			if session, _ := ctx.Value(cartSessionKey{}).(*cartSession); session != nil {
				if session.empty != nil {
					phase = "waitingBase"
				} else if session.partial != nil {
					phase = "waitingOptions"
				}
			}
		case outcome.WaitingForStock:
			phase = "waitingStock"
		}
		if phase != "" {
			setOrderPhase(state, it.ID, phase)
		}
	}()

	if ctx.Err() != nil || state.IsTaskDeleted(it.ID) {
		return outcome
	}

	// 更新检查时间、重试次数
	state.QueueMu.Lock()
	var current *types.QueueItem
	for i := range state.Queue {
		if state.Queue[i].ID == it.ID {
			current = &state.Queue[i]
			break
		}
	}
	if current == nil {
		state.QueueMu.Unlock()
		return outcome
	}
	// 快照后用户可能已暂停/删除:不复核会照走完整下单链路(结账=真实扣款)
	if current.Status != "running" {
		state.QueueMu.Unlock()
		return outcome
	}
	// 自愈:间隔为 0 的老任务把有效值写回去,界面上就不再显示"0 秒后",
	// 下一次 SaveQueue 顺带落库,以后也不用每轮再兜底
	if current.RetryInterval <= 0 {
		current.RetryInterval = state.Config.RetryInterval()
	}
	current.LastCheckTime = float64(time.Now().UnixNano()) / 1e9
	current.RetryCount++
	current.UpdatedAt = types.NowISO()
	finalRetry := current.RetryCount
	snapshot := *current
	state.QueueMu.Unlock()

	// worker 的生命周期取消已登记;在发起请求前再次检查暂停或删除。
	if ctx.Err() != nil || state.IsTaskDeleted(it.ID) {
		return outcome
	}

	outcome = purchase(ctx, state, &snapshot)
	if outcome.GroupStopped != "" {
		state.QueueMu.Lock()
		for i := range state.Queue {
			if state.Queue[i].ID == it.ID {
				state.Queue[i].Status = "paused"
				state.Queue[i].StopReason = outcome.GroupStopped
				state.Queue[i].UpdatedAt = types.NowISO()
				break
			}
		}
		state.QueueMu.Unlock()
		_ = state.SaveQueue()
		return outcome
	}
	if outcome.PauseTask {
		// 结账不确定:暂停而不是重试(重试可能买两台)。不计失败次数
		state.QueueMu.Lock()
		for i := range state.Queue {
			if state.Queue[i].ID == it.ID {
				state.Queue[i].Status = "paused"
				state.Queue[i].UpdatedAt = types.NowISO()
				break
			}
		}
		state.QueueMu.Unlock()
		if outcome.Reason != "" {
			recordFailureStatus(state, &snapshot, outcome.Reason, "uncertain")
		}
		_ = state.SaveQueue()
		state.Logger.Info("任务已暂停(结账结果不确定,等人工确认): "+it.PlanCode, "queue")
		return outcome
	}
	if outcome.Cancelled {
		// 用户删除或暂停,不计失败,不覆盖用户选择的队列状态。
		return outcome
	}
	if outcome.WaitingForCatalog || outcome.WaitingForQuota {
		return outcome
	}
	if outcome.Success {
		state.QueueMu.Lock()
		for i := range state.Queue {
			if state.Queue[i].ID == it.ID {
				state.Queue[i].Status = "completed"
				state.Queue[i].UpdatedAt = types.NowISO()
				break
			}
		}
		state.QueueMu.Unlock()
		// 成功即落库,不等轮末:checkout 成功到 SaveQueue 之间崩溃的话,
		// 重启后任务复活再买一台(真金白银)。失败轮不落 —— 高频且可重建
		_ = state.SaveQueue()
		if finalRetry == 1 {
			state.Logger.Info("首次尝试购买成功: "+it.PlanCode, "queue")
		} else {
			state.Logger.Info("重试购买成功: "+it.PlanCode, "queue")
		}
		return outcome
	}

	// 失败路径:先把"真的提交过并失败"计进 FailureCount(无货轮次不计),
	// MaxRetries 封顶用的是它。
	failureCount := snapshot.FailureCount
	if outcome.Attempted {
		state.QueueMu.Lock()
		for i := range state.Queue {
			if state.Queue[i].ID == it.ID {
				state.Queue[i].FailureCount++
				failureCount = state.Queue[i].FailureCount
				break
			}
		}
		state.QueueMu.Unlock()
	}

	// 以前这里只打一行日志就完了:任务状态永远停在 running,
	// MaxRetries 全项目只写不读,于是跨区 planCode / 账户被删这类
	// **重试一万次也不会变**的任务会按 retryInterval 永远刷下去,
	// 而且每一轮都往 history 写一条失败记录,把抢购历史刷满。
	// 现在两道闸:确定性失败直接终止,其余按 MaxRetries 封顶。
	if finalRetry == 1 {
		state.Logger.Info("首次尝试购买失败或服务器暂无货: "+it.PlanCode, "queue")
	} else {
		state.Logger.Info("重试购买失败或服务器仍无货: "+it.PlanCode, "queue")
	}

	stopReason := ""
	switch {
	case outcome.Fatal:
		// 确定性失败(跨区机型 / 非 Eco 机型 / planCode 不存在 / 账户已删):
		// 换个时间点重试必然还是同一个结果,再重试只是白刷 OVH 和 history。
		// 失败原因 PurchaseServer 已经写进 history 了,这里只负责终止任务。
		stopReason = "确定性失败，重试无用：" + outcome.Reason
	case snapshot.MaxRetries > 0 && failureCount >= snapshot.MaxRetries:
		// MaxRetries 封顶的是**真正提交并失败的次数**,不是检查轮次。
		// 抢购的常态就是绝大多数轮次都无货(那些轮次 Attempted=false,不计数),
		// 拿轮次封顶会让任务在还没真正试过几次时就被判死。
		// 0 / 缺省 = 不封顶,保持 /api/queue 入队任务"一直抢到有货为止"的语义。
		stopReason = fmt.Sprintf("连续 %d 次下单尝试均失败，停止重试", snapshot.MaxRetries)
	}
	if stopReason == "" {
		return outcome
	}
	if !outcome.Fatal {
		// 确定性失败的原因 PurchaseServer 已经写进 history 了;
		// 用尽 MaxRetries 这条没人写过,补一条,否则任务会莫名其妙停在 failed
		// 而历史里一个字都没有。
		recordFailure(state, &snapshot, stopReason)
	}
	state.QueueMu.Lock()
	for i := range state.Queue {
		if state.Queue[i].ID == it.ID {
			state.Queue[i].Status = "failed"
			state.Queue[i].UpdatedAt = types.NowISO()
			break
		}
	}
	state.QueueMu.Unlock()
	// 任务死了必须告诉用户。
	//
	// 以前这里只写日志:成功会 Broadcast 一条通知,失败终止却完全静默 ——
	// 而下单时承诺的是"系统会一直重试到抢到为止"。跨区 planCode、
	// 账户被删这类 Fatal 判定通常发生在第一轮,用户那边看到的是
	// "已加入队列"之后再无音讯,以为还在抢,实际早就停了。
	// 越是确定性失败越要说:它永远不会自己好。
	if msg, ok := failNotice.take(&snapshot, stopReason, outcome.Fatal); ok {
		notify.Broadcast(state, msg, nil)
	}
	state.Logger.Warn(fmt.Sprintf("任务 %s (%s @ %s) 置为 failed：%s",
		it.ID, it.PlanCode, it.Datacenter, stopReason), "queue")
	return outcome
}

func setOrderPhase(state *app.State, id, phase string) {
	state.QueueMu.Lock()
	defer state.QueueMu.Unlock()
	for i := range state.Queue {
		if state.Queue[i].ID == id && state.Queue[i].Status == "running" {
			state.Queue[i].OrderPhase = phase
			break
		}
	}
}
