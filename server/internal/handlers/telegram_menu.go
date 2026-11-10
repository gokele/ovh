package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/notify"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// Telegram 菜单与列表按钮。
//
// 所有按钮都经过 handleTelegramCallback 的授权与限流;callback_data 有 64 字节上限,
// 订阅的型号 + 配置键可能很长,所以用 30 分钟有效的短令牌指向实际目标。
// 删除类操作一律两步:第一次点按钮只换成「确认删除」,第二次才执行。

// tgAction 短令牌背后的实际操作目标。
type tgAction struct {
	kind       string // sub-pause / sub-resume / sub-delete / task-delete
	planCode   string
	optionsKey string
	taskID     string
	expires    time.Time
}

var (
	tgActionMu sync.Mutex
	tgActions  = map[string]tgAction{}
)

const tgActionTTL = 30 * time.Minute

// putAction 保存操作并返回 8 字节十六进制令牌;顺手清理过期令牌。
func putAction(a tgAction) string {
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	tok := hex.EncodeToString(raw[:])
	a.expires = time.Now().Add(tgActionTTL)
	tgActionMu.Lock()
	defer tgActionMu.Unlock()
	for k, v := range tgActions {
		if time.Now().After(v.expires) {
			delete(tgActions, k)
		}
	}
	tgActions[tok] = a
	return tok
}

// takeAction 读取令牌;过期或不存在返回 false。
func takeAction(tok string) (tgAction, bool) {
	tgActionMu.Lock()
	defer tgActionMu.Unlock()
	a, ok := tgActions[tok]
	if !ok || time.Now().After(a.expires) {
		delete(tgActions, tok)
		return tgAction{}, false
	}
	return a, true
}

// cbButton 生成一颗 callback 按钮。
func cbButton(text string, data map[string]string) map[string]interface{} {
	raw, _ := json.Marshal(data)
	return map[string]interface{}{"text": text, "callback_data": string(raw)}
}

// keyboard 包装 inline_keyboard。
func keyboard(rows [][]map[string]interface{}) map[string]interface{} {
	if len(rows) == 0 {
		return nil
	}
	return map[string]interface{}{"inline_keyboard": rows}
}

// menuBack 每个菜单页底部的返回按钮。
func menuBack() []map[string]interface{} {
	return []map[string]interface{}{cbButton("« 菜单", map[string]string{"a": "m", "c": "home"})}
}

// menuText 菜单首页说明。
func menuText(mon *monitor.Monitor) string {
	running := "已停止"
	if mon != nil && mon.Running() {
		running = "运行中"
	}
	return "🤖 OVH 抢购助手\n\n监控：" + running + "\n点下面的按钮查看或操作；发 /help 看下单格式。"
}

// menuKeyboard 菜单首页按钮:常用查看 + 监控开关。
func menuKeyboard(mon *monitor.Monitor) map[string]interface{} {
	toggle := cbButton("▶️ 启动监控", map[string]string{"a": "m", "c": "mon-on"})
	if mon != nil && mon.Running() {
		toggle = cbButton("⏹ 停止监控", map[string]string{"a": "m", "c": "mon-off"})
	}
	return keyboard([][]map[string]interface{}{
		{cbButton("📊 总览", map[string]string{"a": "m", "c": "status"}), cbButton("🛒 任务", map[string]string{"a": "m", "c": "queue"})},
		{cbButton("🔔 订阅", map[string]string{"a": "m", "c": "subs"}), cbButton("📜 最近订单", map[string]string{"a": "m", "c": "recent"})},
		{cbButton("⏱ 抢购速度", map[string]string{"a": "m", "c": "speed"}), toggle},
	})
}

// sendMenu 发出菜单首页。
func sendMenu(state *app.State, mon *monitor.Monitor, chatID interface{}, messageID int64) {
	telegram.SendKeyboard(state, chatID, messageID, menuText(mon), menuKeyboard(mon))
}

// menuPage 菜单各页的文字与按钮;页面内的操作按钮在下方,底部统一「« 菜单」。
func menuPage(state *app.State, mon *monitor.Monitor, page string) (string, map[string]interface{}) {
	switch page {
	case "status":
		return statusText(state, mon), keyboard([][]map[string]interface{}{menuBack()})
	case "queue":
		return queueText(state), withBack(queueKeyboard(state))
	case "subs":
		return subsText(state, mon), withBack(subsKeyboard(mon))
	case "recent":
		return recentText(state), withBack(recentKeyboard(state))
	case "speed":
		return speedText(state), keyboard([][]map[string]interface{}{menuBack()})
	default:
		return menuText(mon), menuKeyboard(mon)
	}
}

// withBack 在已有键盘底部追加返回按钮。
func withBack(kb map[string]interface{}) map[string]interface{} {
	var rows [][]map[string]interface{}
	if kb != nil {
		rows, _ = kb["inline_keyboard"].([][]map[string]interface{})
	}
	return keyboard(append(rows, menuBack()))
}

// handleMenuCallback 菜单翻页与监控开关;原地编辑同一条消息。
func handleMenuCallback(state *app.State, mon *monitor.Monitor, cb, obj map[string]interface{}, chatID interface{}, messageID int64) {
	page := strOr(obj, "c")
	answer := ""
	switch page {
	case "mon-on":
		answer = monitorToggleText(state, mon, true)
		page = "home"
	case "mon-off":
		answer = monitorToggleText(state, mon, false)
		page = "home"
	}
	telegram.AnswerCallback(state, idToString(cb["id"]), answer, false)
	text, kb := menuPage(state, mon, page)
	if answer != "" {
		text = answer + "\n\n" + text
	}
	telegram.EditMessage(state, chatID, messageID, clampReply(text), kb)
}

// monitorToggleText 与网页「启动/停止监控」同一语义:启动前验证通知通道;停止会暂停关联抢购任务并持久化。
func monitorToggleText(state *app.State, mon *monitor.Monitor, on bool) string {
	if mon == nil {
		return "监控未初始化。"
	}
	if on {
		if ok, reason := notify.AnyAvailable(state, true); !ok {
			return "无法启动监控：通知通道不可用（" + reason + "）"
		}
		if mon.Start() {
			state.Logger.Info("Telegram 启动服务器监控", "telegram")
			return "✅ 监控已启动"
		}
		if mon.Running() {
			return "监控已在运行中"
		}
		return "⚠️ 监控未能启动，请检查数据库和服务日志"
	}
	stopped, paused, err := mon.StopByUser()
	if err != nil {
		return "⚠️ 监控已停止，但未能完整保存停止状态：" + err.Error()
	}
	state.Logger.Info("Telegram 停止服务器监控", "telegram")
	msg := "⏹ 监控未运行"
	if stopped {
		msg = "⏹ 监控已停止"
	}
	if paused > 0 {
		msg += fmt.Sprintf("，并暂停了 %d 个关联抢购任务（恢复监控不会自动恢复它们）", paused)
	}
	return msg
}

// monitorText /monitor on|off 命令。
func monitorText(state *app.State, mon *monitor.Monitor, args []string) string {
	if len(args) == 0 {
		return menuText(mon) + "\n\n用法：/monitor on 或 /monitor off"
	}
	switch strings.ToLower(args[0]) {
	case "on", "start", "开", "启动":
		return monitorToggleText(state, mon, true)
	case "off", "stop", "关", "停止":
		return monitorToggleText(state, mon, false)
	}
	return "用法：/monitor on 或 /monitor off"
}

// subsKeyboard 每条订阅一行:暂停/恢复 + 删除(两步确认)。
func subsKeyboard(mon *monitor.Monitor) map[string]interface{} {
	if mon == nil {
		return nil
	}
	var rows [][]map[string]interface{}
	for i, s := range mon.Snapshot() {
		if i >= tgMaxListItems {
			break
		}
		key := monitor.OptionsKey(s.Options)
		label := fmt.Sprintf("%d. %s", i+1, s.PlanCode)
		toggle := cbButton("⏸ 暂停 "+label, map[string]string{"a": "sx", "t": putAction(tgAction{kind: "sub-pause", planCode: s.PlanCode, optionsKey: key})})
		if s.Paused {
			toggle = cbButton("▶️ 恢复 "+label, map[string]string{"a": "sx", "t": putAction(tgAction{kind: "sub-resume", planCode: s.PlanCode, optionsKey: key})})
		}
		del := cbButton("🗑", map[string]string{"a": "sx", "t": putAction(tgAction{kind: "sub-delete", planCode: s.PlanCode, optionsKey: key}), "s": "1"})
		rows = append(rows, []map[string]interface{}{toggle, del})
	}
	return keyboard(rows)
}

// recentKeyboard 未付款订单给出「打开订单页」链接按钮(需登录 OVH,不含凭证)。
func recentKeyboard(state *app.State) map[string]interface{} {
	state.HistoryMu.Lock()
	recent := append([]types.PurchaseHistoryEntry(nil), state.History...)
	state.HistoryMu.Unlock()
	var rows [][]map[string]interface{}
	for i := len(recent) - 1; i >= 0 && len(rows) < 5; i-- {
		h := recent[i]
		if h.Status != "success" || h.OrderID == "" || h.OrderStatus == "delivered" || h.OrderStatus == "cancelled" {
			continue
		}
		acc, _ := state.FindAccount(h.AccountID)
		link := ovh.ManagerOrderURL(acc.Endpoint, h.OrderID)
		if link == "" {
			continue
		}
		rows = append(rows, []map[string]interface{}{{"text": "💳 订单 " + h.OrderID + " · " + h.PlanCode, "url": link}})
	}
	return keyboard(rows)
}

// speedText 最近抢到的订单的关键耗时:发现库存→发出结账、本轮链路、等了多久。
func speedText(state *app.State) string {
	state.HistoryMu.Lock()
	all := append([]types.PurchaseHistoryEntry(nil), state.History...)
	state.HistoryMu.Unlock()
	var b strings.Builder
	b.WriteString("⏱ 抢购速度（最近抢到的订单）\n\n")
	n := 0
	for i := len(all) - 1; i >= 0 && n < 10; i-- {
		h := all[i]
		if h.Status != "success" || h.OrderID == "" {
			continue
		}
		n++
		b.WriteString(fmt.Sprintf("✅ %s @ %s · 订单 %s\n", h.PlanCode, strings.ToUpper(orAny(h.Datacenter)), h.OrderID))
		var parts []string
		if h.DetectionToWriteUs > 0 {
			p := "发现→发出 " + fmtMicros(h.DetectionToWriteUs)
			if h.Prewarmed {
				p += "（预装）"
			}
			parts = append(parts, p)
		}
		if h.TotalMs > 0 {
			parts = append(parts, fmt.Sprintf("链路 %dms", h.TotalMs))
		}
		if h.WaitMs > 0 {
			parts = append(parts, "等了 "+fmtWait(time.Duration(h.WaitMs)*time.Millisecond))
		}
		if len(parts) == 0 {
			parts = append(parts, "（旧版本记录，没有耗时数据）")
		}
		b.WriteString("    " + strings.Join(parts, " · ") + "\n")
	}
	if n == 0 {
		return "⏱ 还没有抢到的订单，暂无耗时数据。\n\n抢到后这里会显示「发现库存 → 发出结账」的耗时。"
	}
	return b.String()
}

// fmtMicros 亚毫秒保留两位小数。
func fmtMicros(us int64) string {
	if us < 1000 {
		return fmt.Sprintf("%.2fms", float64(us)/1000)
	}
	return fmt.Sprintf("%dms", us/1000)
}

// fmtWait 等待时长的简写。
func fmtWait(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d 分", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时 %d 分", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%d 天 %d 小时", int(d.Hours())/24, int(d.Hours())%24)
	}
}

// handleActionCallback 订阅暂停/恢复/删除与任务删除;删除需第二次确认。
func handleActionCallback(state *app.State, mon *monitor.Monitor, cb, obj map[string]interface{}, chatID interface{}, messageID int64) {
	tok := strOr(obj, "t")
	act, ok := takeAction(tok)
	if !ok {
		telegram.AnswerCallback(state, idToString(cb["id"]), "按钮已过期，请重新打开列表", true)
		return
	}
	// 删除第一步:只把消息换成确认按钮,不执行
	if strOr(obj, "s") == "1" {
		target := act.planCode
		if act.kind == "task-delete" {
			target = taskLabel(state, act.taskID)
		}
		telegram.AnswerCallback(state, idToString(cb["id"]), "", false)
		telegram.EditMessage(state, chatID, messageID, "确认删除 "+target+"？此操作不能撤销。", keyboard([][]map[string]interface{}{{
			cbButton("确认删除", map[string]string{"a": "sx", "t": tok}),
			cbButton("取消", map[string]string{"a": "m", "c": pageFor(act.kind)}),
		}}))
		return
	}
	answer := runAction(state, mon, act)
	telegram.AnswerCallback(state, idToString(cb["id"]), answer, false)
	text, kb := menuPage(state, mon, pageFor(act.kind))
	telegram.EditMessage(state, chatID, messageID, clampReply("✅ "+answer+"\n\n"+text), kb)
}

// pageFor 操作完成后回到哪一页。
func pageFor(kind string) string {
	if strings.HasPrefix(kind, "task") {
		return "queue"
	}
	return "subs"
}

// taskLabel 任务的可读名称。
func taskLabel(state *app.State, id string) string {
	state.QueueMu.Lock()
	defer state.QueueMu.Unlock()
	for _, it := range state.Queue {
		if it.ID == id {
			return it.PlanCode + "@" + strings.ToUpper(orAny(it.Datacenter))
		}
	}
	return "该任务"
}

// runAction 执行令牌对应的操作;语义与网页一致(暂停监控联动暂停关联任务,恢复不自动恢复任务)。
func runAction(state *app.State, mon *monitor.Monitor, act tgAction) string {
	switch act.kind {
	case "sub-pause", "sub-resume":
		if mon == nil {
			return "监控未初始化"
		}
		key := act.optionsKey
		paused, err := mon.SetPaused(act.planCode, &key, act.kind == "sub-pause")
		if err != nil {
			return "操作失败：" + err.Error()
		}
		if _, err := persistMonitorChanges(mon); err != nil {
			return "保存失败，监控已停止：" + err.Error()
		}
		if act.kind == "sub-resume" {
			return "已恢复监控 " + act.planCode + "（之前暂停的抢购任务需在 /queue 手动恢复）"
		}
		msg := "已暂停监控 " + act.planCode
		if paused > 0 {
			msg += fmt.Sprintf("，并暂停了 %d 个关联任务", paused)
		}
		return msg
	case "sub-delete":
		if mon == nil || !mon.RemoveSubscriptionExact(act.planCode, act.optionsKey) {
			return "订阅已不存在"
		}
		if _, err := persistMonitorChanges(mon); err != nil {
			return "保存失败，监控已停止：" + err.Error()
		}
		return "已删除订阅 " + act.planCode
	case "task-delete":
		return deleteQueueTask(state, act.taskID)
	}
	return "未知操作"
}

// deleteQueueTask 与 /cancel 相同:写停止记录、取消进行中的准备、移除并落库。
func deleteQueueTask(state *app.State, id string) string {
	state.QueueMu.Lock()
	idx := -1
	for i, it := range state.Queue {
		if it.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		state.QueueMu.Unlock()
		return "任务已不存在"
	}
	it := state.Queue[idx]
	if it.Status == "completed" {
		state.QueueMu.Unlock()
		return "已下单的任务不能删除，请在控制台查看"
	}
	blockErr := state.SetAutomaticOrderBlockedLocked(it, true)
	state.MarkTaskDeleted(it.ID)
	state.Queue = append(state.Queue[:idx], state.Queue[idx+1:]...)
	state.QueueMu.Unlock()
	if err := errors.Join(blockErr, state.SaveQueue()); err != nil {
		return "已移除，但未能写入数据库，重启后会重新出现：" + err.Error()
	}
	state.Logger.Info("Telegram 删除任务 "+id, "telegram")
	return "已删除 " + it.PlanCode + "@" + strings.ToUpper(orAny(it.Datacenter))
}

// pauseAllText 暂停全部运行中/待执行任务(写停止记录);已发出的结账不会因此撤回。
func pauseAllText(state *app.State) string {
	var ids []string
	state.QueueMu.Lock()
	for _, it := range state.Queue {
		if it.Status == "running" || it.Status == "pending" {
			ids = append(ids, it.ID)
		}
	}
	state.QueueMu.Unlock()
	if len(ids) == 0 {
		return "没有运行中的任务。"
	}
	failed := 0
	for _, id := range ids {
		if _, err := pauseQueueTask(state, id); err != nil {
			failed++
		}
	}
	msg := fmt.Sprintf("⏸ 已暂停 %d 个任务", len(ids)-failed)
	if failed > 0 {
		msg += fmt.Sprintf("，%d 个未能暂停（可能状态已变化）", failed)
	}
	return msg + "。已发出的结账请求不受影响；需要时在 /queue 逐个恢复。"
}
