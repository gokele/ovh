package purchase

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/notify"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/types"
)

// 结账结果不明的通知合并。
//
// 同配置合并抢购时,有货那一刻一组任务同时结账;OVH 限流(429)或网络抖动时
// 它们几乎同时落到「结果不明」,随后的自动核对也几乎同时结束。逐条发的话
// 手机上一下子十几条一模一样的消息,真正要处理的那句反而看不到。
// 同一种通知在 noticeWindow 内只发一条:只有一台时原样发,多台时列出每个型号@机房和台数。
// 核实到订单、下单成功仍逐条发 —— 每条都带订单号和付款链接,合并会丢信息。

type noticeKind string

const (
	noticeUncertain   noticeKind = "uncertain"   // 结账结果不明确,任务已暂停
	noticeUnresolved  noticeKind = "unresolved"  // 自动核对结束仍无法确认
	noticeAutoResumed noticeKind = "autoResumed" // 429 后观察期内无新订单,已自动恢复
)

const (
	noticeWindow     = 3 * time.Second
	noticeMaxButtons = 6
)

type pendingNotice struct {
	state      *app.State
	item       types.QueueItem
	text       string
	markup     map[string]interface{}
	candidates []string
}

type noticeBatcher struct {
	mu      sync.Mutex
	window  time.Duration
	pending map[noticeKind][]pendingNotice
	// send 为空时用通知通道广播;测试里替换
	send func(state *app.State, text string, markup map[string]interface{})
}

var notices = &noticeBatcher{window: noticeWindow, pending: map[noticeKind][]pendingNotice{}}

// add 登记一条通知;同一种的第一条到来时开始计时,窗口结束后合并发出。
// window<=0 时立即发送(测试用:不让延迟的通知跑到测试结束之后)。
func (b *noticeBatcher) add(state *app.State, kind noticeKind, n pendingNotice) {
	n.state = state
	b.mu.Lock()
	first := len(b.pending[kind]) == 0
	b.pending[kind] = append(b.pending[kind], n)
	window := b.window
	b.mu.Unlock()
	if window <= 0 {
		b.flush(kind)
		return
	}
	if first {
		time.AfterFunc(window, func() { b.flush(kind) })
	}
}

func (b *noticeBatcher) flush(kind noticeKind) {
	b.mu.Lock()
	batch := b.pending[kind]
	delete(b.pending, kind)
	send := b.send
	b.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	if send == nil {
		send = func(state *app.State, text string, markup map[string]interface{}) {
			if state != nil {
				notify.Broadcast(state, text, markup)
			}
		}
	}
	state := batch[len(batch)-1].state
	if len(batch) == 1 {
		send(state, batch[0].text, batch[0].markup)
		return
	}
	text, markup := mergeNotices(kind, batch)
	send(state, text, markup)
}

// mergeNotices 多台合成一条:标题带台数,逐行列出型号@机房 ×台数。
func mergeNotices(kind noticeKind, batch []pendingNotice) (string, map[string]interface{}) {
	counts := map[string]int{}
	var order []string
	for _, n := range batch {
		k := n.item.PlanCode + "@" + strings.ToUpper(n.item.Datacenter)
		if counts[k] == 0 {
			order = append(order, k)
		}
		counts[k]++
	}
	var lines strings.Builder
	for _, k := range order {
		if counts[k] > 1 {
			lines.WriteString(fmt.Sprintf("· %s ×%d\n", k, counts[k]))
		} else {
			lines.WriteString("· " + k + "\n")
		}
	}
	var b strings.Builder
	withButtons := len(batch) <= noticeMaxButtons
	resumeHint := "- 没有：点下面的按钮恢复对应任务"
	if !withButtons {
		resumeHint = "- 没有：到网页的抢购队列恢复这些任务"
	}
	switch kind {
	case noticeUncertain:
		b.WriteString(fmt.Sprintf("⚠️ %d 台抢购结账结果不明确，无法确认订单是否已创建\n\n", len(batch)))
		b.WriteString(lines.String())
		b.WriteString("\n这些任务已暂停，正在自动核对 OVH 账户里的新订单（约 5 分钟），核实后会再通知。\n" +
			"也可以直接到 OVH 订单页查看：\n- 有：等付款即可，任务不需要恢复\n" + resumeHint)
	case noticeUnresolved:
		b.WriteString(fmt.Sprintf("⚠️ %d 台的结账结果仍无法自动确认\n\n", len(batch)))
		b.WriteString(lines.String())
		seen := map[string]bool{}
		var ids []string
		for _, n := range batch {
			for _, id := range n.candidates {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
		sort.Strings(ids)
		if len(ids) > 0 {
			if len(ids) > 8 {
				ids = append(ids[:8], "…")
			}
			b.WriteString("\n账户里有新订单，但无法逐一对应到这些任务。\n新订单号：" + strings.Join(ids, "、") + "\n")
		} else {
			b.WriteString("\n结账后几分钟内，账户里没有出现新的订单。\n")
		}
		b.WriteString("\n这些任务保持暂停。请到 OVH 订单页确认：\n- 有订单：去付款，对应任务不用恢复\n" + resumeHint)
	case noticeAutoResumed:
		b.WriteString(fmt.Sprintf("🔁 %d 台结账时被 OVH 限流（429）。\n结账后 1 分钟内账户里没有任何新订单，确认未下单，任务已自动恢复继续抢购。\n\n", len(batch)))
		b.WriteString(lines.String())
		return strings.TrimRight(b.String(), "\n"), nil
	}
	if !withButtons || kind == noticeAutoResumed {
		return b.String(), nil
	}
	// 每台一颗恢复按钮;同一型号@机房有多台时编号区分
	seq := map[string]int{}
	var rows [][]map[string]interface{}
	for _, n := range batch {
		k := n.item.PlanCode + "@" + strings.ToUpper(n.item.Datacenter)
		seq[k]++
		label := "恢复 " + k
		if counts[k] > 1 {
			label = fmt.Sprintf("%s #%d", label, seq[k])
		}
		rows = append(rows, []map[string]interface{}{telegram.TaskButton(label, n.item.ID, telegram.TaskResume)})
	}
	return b.String(), map[string]interface{}{"inline_keyboard": rows}
}
