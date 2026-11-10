package handlers

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// queueKeyboard /queue 的每个任务一行:序号对应文字列表,运行中给「暂停」,已暂停给「恢复」。
// 删除仍只走 /cancel:按钮误触不能丢掉任务。
func queueKeyboard(state *app.State) map[string]interface{} {
	items := activeQueueItems(state)
	rows := [][]map[string]interface{}{}
	for i, it := range items {
		if i >= tgMaxListItems {
			break
		}
		label := fmt.Sprintf("%d. %s@%s", i+1, it.PlanCode, strings.ToUpper(orAny(it.Datacenter)))
		del := cbButton("🗑", map[string]string{"a": "sx", "t": putAction(tgAction{kind: "task-delete", taskID: it.ID}), "s": "1"})
		switch it.Status {
		case "running", "pending":
			rows = append(rows, []map[string]interface{}{telegram.TaskButton("⏸ 暂停 "+label, it.ID, telegram.TaskPause), del})
		case "paused":
			rows = append(rows, []map[string]interface{}{telegram.TaskButton("▶️ 恢复 "+label, it.ID, telegram.TaskResume), del})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	if len(rows) > 1 {
		rows = append(rows, []map[string]interface{}{cbButton("⏸ 全部暂停", map[string]string{"a": "qa"})})
	}
	return map[string]interface{}{"inline_keyboard": rows}
}

func activeQueueItems(state *app.State) []types.QueueItem {
	state.QueueMu.Lock()
	defer state.QueueMu.Unlock()
	items := make([]types.QueueItem, 0, len(state.Queue))
	for _, it := range state.Queue {
		if it.Status == "running" || it.Status == "pending" || it.Status == "paused" {
			items = append(items, it)
		}
	}
	return items
}

// pauseQueueTask 与控制台暂停一致:写停止记录、取消进行中的准备、落库。
// 已发出的结账请求不会因暂停而撤回。
func pauseQueueTask(state *app.State, id string) (types.QueueItem, error) {
	state.QueueMu.Lock()
	var found *types.QueueItem
	var blockErr error
	for i := range state.Queue {
		if state.Queue[i].ID != id {
			continue
		}
		if state.Queue[i].Status != "running" && state.Queue[i].Status != "pending" {
			state.QueueMu.Unlock()
			return state.Queue[i], fmt.Errorf("任务当前状态为 %s，无需暂停", state.Queue[i].Status)
		}
		blockErr = state.SetAutomaticOrderBlockedLocked(state.Queue[i], true)
		state.Queue[i].Status = "paused"
		state.Queue[i].UpdatedAt = types.NowISO()
		state.UnregisterTaskCancel(id)
		copy := state.Queue[i]
		found = &copy
		break
	}
	state.QueueMu.Unlock()
	if found == nil {
		return types.QueueItem{}, app.ErrQueueNotFound
	}
	if err := errors.Join(blockErr, state.SaveQueue()); err != nil {
		return *found, fmt.Errorf("已在本次运行中暂停，但没能写进数据库，重启后会恢复原状态：%w", err)
	}
	return *found, nil
}

// handleTaskCallback 处理 {"a":"qt"} 按钮。调用方已完成授权与限流。
func handleTaskCallback(state *app.State, cb map[string]interface{}, obj map[string]interface{}, chatID interface{}, messageID int64) {
	id := strOr(obj, "i")
	op := strOr(obj, "o")
	answer := ""
	switch op {
	case telegram.TaskPause:
		if it, err := pauseQueueTask(state, id); err != nil {
			answer = "暂停失败：" + err.Error()
		} else {
			answer = "已暂停 " + it.PlanCode + "@" + strings.ToUpper(orAny(it.Datacenter))
			state.Logger.Info("Telegram 暂停任务 "+id, "telegram")
		}
	case telegram.TaskResume:
		if err := state.RestoreCompletedQueue(); err != nil {
			answer = "无法核对已下单记录，未恢复：" + err.Error()
		} else if err := state.ResumeQueueItem(id, "running"); err != nil {
			switch {
			case errors.Is(err, app.ErrQueueAlreadyOrdered):
				answer = "这个任务已经下过单，不能再次启动"
			case errors.Is(err, app.ErrQueueNotFound):
				answer = "任务已不存在"
			default:
				answer = "恢复失败：" + err.Error()
			}
		} else {
			answer = "已恢复运行"
			state.Logger.Info("Telegram 恢复任务 "+id, "telegram")
		}
	default:
		answer = "未知操作"
	}
	telegram.AnswerCallback(state, idToString(cb["id"]), answer, false)
	// 按钮操作后原地刷新任务列表,不刷屏。
	telegram.EditMessage(state, chatID, messageID, clampReply(queueText(state)+"\n\n✅ "+answer), queueKeyboard(state))
}
