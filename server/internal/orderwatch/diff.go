package orderwatch

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

// FollowUpStep 对应 OVH GET /me/order/{orderId}/followUp 返回的单步交付进度。
type FollowUpStep struct {
	Step    string          `json:"step"`
	Status  string          `json:"status"`
	History []FollowUpEvent `json:"history"`
}

// FollowUpEvent 对应单步里的历史时间线事件。
type FollowUpEvent struct {
	Step        string `json:"-"`
	Date        string `json:"date"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// StepChange 记录交付大阶段的变化。
type StepChange struct {
	Step      string
	OldStatus string
	NewStatus string
}

// OrderMeta 订单基础信息。
type OrderMeta struct {
	OrderID        string
	Date           string
	ExpirationDate string
	URL            string
	PriceText      string
	Summary        string
}

var canonicalSteps = []string{"VALIDATING", "VALIDATED", "DELIVERING", "AVAILABLE"}

func isTerminalStatus(status string) bool {
	return status == "delivered" || status == "cancelled" || status == "expired"
}

func parseOVHTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	if t, ok := types.ParseTS(raw); ok {
		return t, true
	}
	return time.Time{}, false
}

// effectiveStatus 对已过付款期限但 OVH 仍返回 notPaid 的订单换算为 expired。
func effectiveStatus(rawStatus, expirationDate string, now time.Time) string {
	s := strings.TrimSpace(rawStatus)
	if s != "notPaid" || strings.TrimSpace(expirationDate) == "" {
		return s
	}
	if exp, ok := parseOVHTime(expirationDate); ok && !exp.After(now) {
		return "expired"
	}
	return s
}

func eventKey(e FollowUpEvent) string {
	return strings.TrimSpace(e.Step) + "|" + strings.TrimSpace(e.Label) + "|" + strings.TrimSpace(e.Date) + "|" + strings.TrimSpace(e.Description)
}

func extractStepsAndEvents(steps []FollowUpStep) (map[string]string, []FollowUpEvent) {
	stepMap := make(map[string]string, len(steps))
	var events []FollowUpEvent
	for _, s := range steps {
		stepName := strings.ToUpper(strings.TrimSpace(s.Step))
		stepStatus := strings.ToUpper(strings.TrimSpace(s.Status))
		if stepName != "" && stepStatus != "" {
			stepMap[stepName] = stepStatus
		}
		for _, h := range s.History {
			events = append(events, FollowUpEvent{
				Step:        stepName,
				Date:        strings.TrimSpace(h.Date),
				Label:       strings.TrimSpace(h.Label),
				Description: strings.TrimSpace(h.Description),
			})
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		ti, oki := parseOVHTime(events[i].Date)
		tj, okj := parseOVHTime(events[j].Date)
		if oki && okj && !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return false
	})
	return stepMap, events
}

func hasSignificantFollowUp(steps map[string]string, events []FollowUpEvent) bool {
	for _, e := range events {
		if e.Label != "" && e.Label != "ORDER_STARTED" {
			return true
		}
	}
	for _, st := range []string{"VALIDATED", "DELIVERING", "AVAILABLE"} {
		if v := steps[st]; v != "" && v != "TODO" {
			return true
		}
	}
	if v := steps["VALIDATING"]; v == "DONE" || v == "ERROR" {
		return true
	}
	return false
}

func diffOrder(
	prev db.OrderWatchSnapshot,
	effStatus string,
	freshSteps map[string]string,
	freshEvents []FollowUpEvent,
	followUpOK bool,
) (next db.OrderWatchSnapshot, statusChanged bool, newEvents []FollowUpEvent, stepChanges []StepChange) {
	next = prev
	next.Steps = make(map[string]string, len(prev.Steps))
	for k, v := range prev.Steps {
		next.Steps[k] = v
	}
	next.Events = append([]string(nil), prev.Events...)

	if effStatus == "unknown" && prev.Status != "" {
		next.Status = prev.Status
	} else if effStatus != "" {
		next.Status = effStatus
	}
	statusChanged = effStatus != "" && effStatus != "unknown" && effStatus != prev.Status

	seenEvents := make(map[string]bool, len(prev.Events))
	for _, k := range prev.Events {
		seenEvents[k] = true
	}

	if followUpOK {
		for _, e := range freshEvents {
			k := eventKey(e)
			if !seenEvents[k] {
				seenEvents[k] = true
				next.Events = append(next.Events, k)
				newEvents = append(newEvents, e)
			}
		}

		visited := make(map[string]bool, len(canonicalSteps)+len(freshSteps))
		recordStep := func(st string) {
			if visited[st] {
				return
			}
			visited[st] = true
			ns := freshSteps[st]
			if ns == "" {
				return
			}
			os := prev.Steps[st]
			next.Steps[st] = ns
			if ns != os && ns != "TODO" {
				stepChanges = append(stepChanges, StepChange{
					Step:      st,
					OldStatus: os,
					NewStatus: ns,
				})
			}
		}
		for _, st := range canonicalSteps {
			recordStep(st)
		}
		var extras []string
		for st := range freshSteps {
			if !visited[st] {
				extras = append(extras, st)
			}
		}
		sort.Strings(extras)
		for _, st := range extras {
			recordStep(st)
		}
	}

	next.Terminal = isTerminalStatus(next.Status)
	return next, statusChanged, newEvents, stepChanges
}

func statusCN(s string) string {
	switch s {
	case "notPaid":
		return "待付款"
	case "checking":
		return "核验中"
	case "delivering":
		return "交付中"
	case "delivered":
		return "已交付"
	case "cancelling":
		return "取消中"
	case "cancelled":
		return "已取消"
	case "expired":
		return "已过期未付款"
	case "documentsRequested":
		return "需补交资料"
	case "unknown", "":
		return "状态未知"
	default:
		return s
	}
}

func stepCN(s string) string {
	switch s {
	case "VALIDATING":
		return "核验订单"
	case "VALIDATED":
		return "订单已确认"
	case "DELIVERING":
		return "交付中"
	case "AVAILABLE":
		return "可以使用"
	default:
		return s
	}
}

func stepStatusCN(s string) string {
	switch s {
	case "TODO":
		return "未开始"
	case "DOING":
		return "进行中"
	case "DONE":
		return "已完成"
	case "ERROR":
		return "出错"
	default:
		return s
	}
}

func labelCN(l string) string {
	switch l {
	case "DELIVERY":
		return "开始交付"
	case "FRAUD_CHECK":
		return "风控审核"
	case "FRAUD_DOCS_REQUESTED":
		return "要求补交资料"
	case "FRAUD_MANUAL_REVIEW":
		return "人工审核"
	case "FRAUD_REFUSED":
		return "审核未通过"
	case "INVOICE_IN_PROGRESS":
		return "生成发票中"
	case "INVOICE_SENT":
		return "发票已发送"
	case "ORDER_ACCEPTED":
		return "订单已接受"
	case "ORDER_STARTED":
		return "订单已开始处理"
	case "PAYMENT_CONFIRMED":
		return "付款已确认"
	case "PAYMENT_INITIATED":
		return "付款已发起"
	case "PAYMENT_RECEIVED":
		return "已收到付款"
	case "REGISTERED_PAYMENT_INITIATED":
		return "已用绑定方式发起付款"
	default:
		if l == "" {
			return "进度更新"
		}
		return l
	}
}

func hasEventLabel(events []FollowUpEvent, labels ...string) bool {
	for _, e := range events {
		for _, l := range labels {
			if e.Label == l {
				return true
			}
		}
	}
	return false
}

func hasStepStatus(changes []StepChange, step, status string) bool {
	for _, sc := range changes {
		if sc.Step == step && sc.NewStatus == status {
			return true
		}
	}
	return false
}

func anyStepError(changes []StepChange) bool {
	for _, sc := range changes {
		if sc.NewStatus == "ERROR" {
			return true
		}
	}
	return false
}

func headlineForUpdate(
	isNew bool,
	nextStatus string,
	statusChanged bool,
	newEvents []FollowUpEvent,
	stepChanges []StepChange,
) string {
	if isNew {
		return "🆕 OVH 订单更新：已下单（" + statusCN(nextStatus) + "）"
	}
	switch {
	case (statusChanged && nextStatus == "delivered") || hasStepStatus(stepChanges, "AVAILABLE", "DONE"):
		return "✅ OVH 订单已交付"
	case statusChanged && nextStatus == "cancelled":
		return "❌ OVH 订单已取消"
	case statusChanged && nextStatus == "cancelling":
		return "⚠️ OVH 订单取消中"
	case statusChanged && nextStatus == "expired":
		return "⏰ OVH 订单已过期未付款"
	case (statusChanged && nextStatus == "documentsRequested") || hasEventLabel(newEvents, "FRAUD_DOCS_REQUESTED"):
		return "📄 OVH 订单需补交资料"
	case hasEventLabel(newEvents, "FRAUD_REFUSED") || anyStepError(stepChanges):
		return "⚠️ OVH 订单处理异常"
	case (statusChanged && nextStatus == "delivering") || hasEventLabel(newEvents, "DELIVERY") || hasStepStatus(stepChanges, "DELIVERING", "DOING"):
		return "🚀 OVH 订单开始交付"
	case hasStepStatus(stepChanges, "VALIDATED", "DONE") || hasEventLabel(newEvents, "ORDER_ACCEPTED"):
		return "✅ OVH 订单已验证通过"
	case hasEventLabel(newEvents, "PAYMENT_CONFIRMED", "PAYMENT_RECEIVED", "PAYMENT_INITIATED", "REGISTERED_PAYMENT_INITIATED") || (statusChanged && nextStatus == "checking"):
		if nextStatus == "checking" {
			return "💳 OVH 订单已付款（核验中）"
		}
		return "💳 OVH 订单已付款"
	default:
		return "📦 OVH 订单进度更新"
	}
}

func formatOrderNotification(
	acc types.OVHAccount,
	isNew bool,
	prevStatus string,
	next db.OrderWatchSnapshot,
	statusChanged bool,
	newEvents []FollowUpEvent,
	stepChanges []StepChange,
) string {
	var b strings.Builder
	b.WriteString(headlineForUpdate(isNew, next.Status, statusChanged, newEvents, stepChanges))
	b.WriteString("\n\n")
	b.WriteString("订单号: " + next.OrderID + "\n")
	if strings.TrimSpace(acc.Name) != "" {
		b.WriteString("账户: " + strings.TrimSpace(acc.Name) + "\n")
	}
	if strings.TrimSpace(next.Summary) != "" {
		b.WriteString("商品: " + strings.TrimSpace(next.Summary) + "\n")
	}
	if strings.TrimSpace(next.PriceText) != "" {
		b.WriteString("金额: " + strings.TrimSpace(next.PriceText) + "\n")
	}
	if statusChanged && prevStatus != "" && prevStatus != "unknown" {
		b.WriteString(fmt.Sprintf("状态: %s → %s\n", statusCN(prevStatus), statusCN(next.Status)))
	} else if next.Status != "" {
		b.WriteString("状态: " + statusCN(next.Status) + "\n")
	}
	if len(stepChanges) > 0 {
		parts := make([]string, 0, len(stepChanges))
		for _, sc := range stepChanges {
			parts = append(parts, fmt.Sprintf("%s（%s）", stepCN(sc.Step), stepStatusCN(sc.NewStatus)))
		}
		b.WriteString("阶段: " + strings.Join(parts, "、") + "\n")
	}
	if len(newEvents) > 0 {
		b.WriteString("进展:\n")
		for _, e := range newEvents {
			lbl := labelCN(e.Label)
			line := "• " + lbl
			if e.Description != "" && e.Description != e.Label && e.Description != lbl {
				line += " — " + e.Description
			}
			if ts, ok := parseOVHTime(e.Date); ok {
				line += "（" + ts.Local().Format("2006-01-02 15:04:05") + "）"
			}
			b.WriteString(line + "\n")
		}
	}
	if link := ovh.OrderLink(acc.Endpoint, next.OrderID, next.OrderURL); link != "" {
		b.WriteString("链接: " + link)
	}
	return strings.TrimRight(b.String(), "\n")
}
