package handlers

import (
	"encoding/json"
	"testing"

	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// /queue 按钮:暂停写停止记录并落库;恢复走与控制台相同的路径,已下单任务不能复活。
func TestTelegramTaskButtonsPauseAndResume(t *testing.T) {
	state, _ := newWatchTestMonitor(t)
	state.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu"}}
	running := types.QueueItem{ID: "11111111-2222-3333-4444-555555555555", AccountID: "eu", PlanCode: "plan", Datacenter: "gra", Status: "running", CreatedAt: types.NowISO()}
	done := types.QueueItem{ID: "99999999-2222-3333-4444-555555555555", AccountID: "eu", PlanCode: "plan", Datacenter: "fra", Status: "completed", CreatedAt: types.NowISO()}
	if err := state.EnqueueItems([]types.QueueItem{running, done}, false); err != nil {
		t.Fatal(err)
	}
	kb := queueKeyboard(state)
	rows, _ := kb["inline_keyboard"].([][]map[string]interface{})
	if len(rows) != 1 {
		t.Fatalf("completed task must not get a button: %v", rows)
	}
	data, _ := rows[0][0]["callback_data"].(string)
	if len(data) > 64 {
		t.Fatalf("callback_data %d bytes exceeds Telegram limit", len(data))
	}
	var obj map[string]interface{}
	_ = json.Unmarshal([]byte(data), &obj)
	if obj["a"] != "qt" || obj["o"] != telegram.TaskPause || obj["i"] != running.ID {
		t.Fatal("unexpected button", obj)
	}
	handleTaskCallback(state, map[string]interface{}{"id": "cb"}, obj, int64(1), 0)
	rowsDB, _ := state.DB.ListQueue()
	for _, r := range rowsDB {
		if r.ID == running.ID && r.Status != "paused" {
			t.Fatal("pause not persisted", r.Status)
		}
	}
	handleTaskCallback(state, map[string]interface{}{"id": "cb"}, map[string]interface{}{"a": "qt", "i": running.ID, "o": telegram.TaskResume}, int64(1), 0)
	handleTaskCallback(state, map[string]interface{}{"id": "cb"}, map[string]interface{}{"a": "qt", "i": done.ID, "o": telegram.TaskResume}, int64(1), 0)
	state.QueueMu.Lock()
	defer state.QueueMu.Unlock()
	for _, it := range state.Queue {
		if it.ID == running.ID && it.Status != "running" {
			t.Fatal("resume failed", it.Status)
		}
		if it.ID == done.ID && it.Status != "completed" {
			t.Fatal("completed task was revived by a button", it.Status)
		}
	}
}
