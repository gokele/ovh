package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ovh-buy/server/internal/types"
)

// 每颗回调按钮都必须在 Telegram 的 64 字节上限内,否则点了没反应。
func assertButtonsFit(t *testing.T, kb map[string]interface{}) {
	t.Helper()
	rows, _ := kb["inline_keyboard"].([][]map[string]interface{})
	for _, row := range rows {
		for _, b := range row {
			if data, ok := b["callback_data"].(string); ok && len(data) > 64 {
				t.Fatalf("callback_data too long (%d): %s", len(data), data)
			}
		}
	}
}

// 删除任务需要两步:第一次只出确认,第二次才删除;全部暂停不碰已下单任务。
func TestTelegramTaskDeleteNeedsConfirmAndPauseAll(t *testing.T) {
	state, mon := newWatchTestMonitor(t)
	state.Accounts = []types.OVHAccount{{ID: "eu", Endpoint: "ovh-eu"}}
	items := []types.QueueItem{
		{ID: "11111111-1111-1111-1111-111111111111", AccountID: "eu", PlanCode: "a", Datacenter: "gra", Status: "running", CreatedAt: types.NowISO()},
		{ID: "22222222-2222-2222-2222-222222222222", AccountID: "eu", PlanCode: "b", Datacenter: "rbx", Status: "running", CreatedAt: types.NowISO()},
		{ID: "33333333-3333-3333-3333-333333333333", AccountID: "eu", PlanCode: "c", Datacenter: "sbg", Status: "completed", CreatedAt: types.NowISO()},
	}
	if err := state.EnqueueItems(items, false); err != nil {
		t.Fatal(err)
	}
	kb := queueKeyboard(state)
	assertButtonsFit(t, kb)
	rows := kb["inline_keyboard"].([][]map[string]interface{})
	var del map[string]interface{}
	_ = json.Unmarshal([]byte(rows[0][1]["callback_data"].(string)), &del)
	if del["a"] != "sx" || del["s"] != "1" {
		t.Fatalf("delete button must be the confirm step: %v", del)
	}
	handleActionCallback(state, mon, map[string]interface{}{"id": "cb"}, del, int64(1), 0)
	if len(state.Queue) != 3 {
		t.Fatal("first tap must not delete")
	}
	delete(del, "s")
	handleActionCallback(state, mon, map[string]interface{}{"id": "cb"}, del, int64(1), 0)
	if len(state.Queue) != 2 {
		t.Fatalf("confirmed delete did not remove the task: %d", len(state.Queue))
	}
	if msg := pauseAllText(state); !strings.Contains(msg, "已暂停 1 个任务") {
		t.Fatal(msg)
	}
	for _, it := range state.Queue {
		if it.Status == "running" {
			t.Fatal("pause all left a task running")
		}
		if it.PlanCode == "c" && it.Status != "completed" {
			t.Fatal("pause all touched a completed order")
		}
	}
	if _, ok := takeAction("nope"); ok {
		t.Fatal("unknown token accepted")
	}
}

// 订阅按钮:暂停/恢复按型号 + 配置精确定位,按钮在长度上限内。
func TestTelegramSubscriptionPauseResume(t *testing.T) {
	state, mon := newWatchTestMonitor(t)
	mon.AddSubscription("24ska01", nil, true, false, "", nil, nil, false, 1, "", []string{"ram-64g-ecc-2133-24ska01", "softraid-2x2000sa-24ska01"}, "")
	kb := subsKeyboard(mon)
	assertButtonsFit(t, kb)
	rows := kb["inline_keyboard"].([][]map[string]interface{})
	var toggle map[string]interface{}
	_ = json.Unmarshal([]byte(rows[0][0]["callback_data"].(string)), &toggle)
	handleActionCallback(state, mon, map[string]interface{}{"id": "cb"}, toggle, int64(1), 0)
	if subs := mon.Snapshot(); len(subs) != 1 || !subs[0].Paused {
		t.Fatal("subscription not paused")
	}
	kb = subsKeyboard(mon)
	rows = kb["inline_keyboard"].([][]map[string]interface{})
	if !strings.HasPrefix(rows[0][0]["text"].(string), "▶️") {
		t.Fatal("paused subscription should offer resume")
	}
}
