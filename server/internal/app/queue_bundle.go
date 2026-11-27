package app

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/orderterm"
	"github.com/ovh-buy/server/internal/types"
)

// 整单编辑。
//
// 队列页把同账户、同型号、同配置的任务合成「一单」显示(可跨机房)。以前编辑只改其中一台,
// 改完一单被拆成两行、其余几台还是旧配置;数量也只能一台台加删。这里把一单作为一个整体修改:
//   - 配置(选项、周期、间隔、自动付款、优惠码)应用到这一单所有未下单的任务,原地修改,不新建;
//   - 数量:按机房模式给每个机房「还要抢几台」,多了新增任务、少了删除未下单的任务;
//     合计模式(GroupLimit>0,多机房共享上限)给「合计几台」和要抢的机房;
//   - 已下单的任务一律不动。
//
// 全部在一次落库里完成:要么整单改好,要么什么都没变。

var (
	ErrBundleMixed     = errors.New("这些任务不属于同一单(账户或型号不同),不能一起修改")
	ErrBundleNoTarget  = errors.New("至少要保留一个机房、一台服务器")
	ErrBundleTooLarge  = errors.New("单个机房最多 60 台")
	ErrBundleNoOptions = errors.New("配置无效")
)

const maxBundlePerDC = 60

type BundleUpdate struct {
	IDs           []string
	Options       []string
	Duration      *string
	RetryInterval int
	AutoPay       bool
	Coupon        string
	// Quantities 按机房模式:机房 → 还要抢的台数(未下单任务数)。nil 表示数量不变。
	Quantities map[string]int
	// Total 合计模式:这一单合计要抢的台数(含已下单);Datacenters 为参与的机房。Total<=0 表示不变。
	Total       int
	Datacenters []string
}

type BundleResult struct {
	Updated int `json:"updated"`
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

func dcKey(dc string) string { return strings.ToLower(strings.TrimSpace(dc)) }

// UpdateQueueBundle 原子地修改一整单。
func (s *State) UpdateQueueBundle(u BundleUpdate) (BundleResult, error) {
	var res BundleResult
	if err := s.SaveBlocked("queue"); err != nil {
		return res, err
	}
	if u.Duration != nil {
		d, err := orderterm.Normalize(*u.Duration)
		if err != nil {
			return res, err
		}
		u.Duration = &d
	}
	want := map[string]bool{}
	for _, id := range u.IDs {
		want[id] = true
	}

	s.saveQueueMu.Lock()
	defer s.saveQueueMu.Unlock()
	s.QueueMu.Lock()
	defer s.QueueMu.Unlock()

	next := append([]types.QueueItem(nil), s.Queue...)
	var members []int   // next 下标:本单未下单的任务
	var completed []int // 本单已下单的任务(只用于合计模式计数)
	for i := range next {
		if !want[next[i].ID] {
			continue
		}
		if next[i].Status == "completed" {
			completed = append(completed, i)
			continue
		}
		members = append(members, i)
	}
	if len(members) == 0 && len(completed) == 0 {
		return res, ErrQueueNotFound
	}
	all := append(append([]int(nil), members...), completed...)
	tpl := next[all[0]]
	shared := false
	for _, i := range all {
		it := next[i]
		if it.AccountID != tpl.AccountID || it.PlanCode != tpl.PlanCode || it.Preorder != tpl.Preorder {
			return res, ErrBundleMixed
		}
		if it.GroupLimit > 0 {
			shared = true
		}
	}

	anyRunning := false
	for _, i := range members {
		if next[i].Status == "running" || next[i].Status == "pending" {
			anyRunning = true
		}
	}
	newStatus := "paused"
	if anyRunning {
		newStatus = "running"
	}

	// 目标:每个机房还要多少个未下单任务
	target := map[string]int{}
	group, limit := tpl.OrderGroup, tpl.GroupLimit
	switch {
	case shared && u.Total > 0:
		if len(u.Datacenters) == 0 {
			return res, ErrBundleNoTarget
		}
		if u.Total > maxBundlePerDC {
			return res, ErrBundleTooLarge
		}
		limit = u.Total
		remaining := limit - len(completed)
		if remaining < 0 {
			remaining = 0
		}
		for _, dc := range u.Datacenters {
			target[dcKey(dc)] = remaining
		}
		if group == "" {
			group = uuid.NewString()
		}
	case !shared && u.Quantities != nil:
		total := 0
		for dc, n := range u.Quantities {
			if n < 0 {
				n = 0
			}
			if n > maxBundlePerDC {
				return res, ErrBundleTooLarge
			}
			if n > 0 {
				target[dcKey(dc)] = n
				total += n
			}
		}
		if total == 0 && len(completed) == 0 {
			return res, ErrBundleNoTarget
		}
	default:
		target = nil // 数量不变
	}

	// 1) 配置原地应用到所有未下单任务
	opts := append([]string(nil), u.Options...)
	changed := map[string]bool{}
	for _, i := range members {
		it := &next[i]
		before := groupSignature(*it)
		it.Options = append([]string(nil), opts...)
		if u.Duration != nil {
			it.Duration = *u.Duration
		}
		if u.RetryInterval > 0 {
			it.RetryInterval = u.RetryInterval
		}
		it.AutoPay = u.AutoPay
		it.Coupon = strings.TrimSpace(u.Coupon)
		if shared {
			it.OrderGroup, it.GroupLimit = group, limit
			// 合计数调高后,因「已凑够」暂停的任务重新参与
			if it.StopReason == "groupComplete" && anyRunning {
				it.Status, it.StopReason = "running", ""
			}
		}
		if before != groupSignature(*it) {
			it.FailureCount, it.LastCheckTime, it.OrderPhase = 0, 0, ""
			changed[it.ID] = true
		}
		it.UpdatedAt = types.NowISO()
		res.Updated++
	}

	// 2) 按机房调整数量
	var removedItems []types.QueueItem
	removed := map[string]bool{}
	added := map[string]bool{}
	if target != nil {
		byDC := map[string][]int{}
		for _, i := range members {
			k := dcKey(next[i].Datacenter)
			byDC[k] = append(byDC[k], i)
		}
		// 多出来的:先删暂停的,再删最新建的
		for dc, idxs := range byDC {
			extra := len(idxs) - target[dc]
			if extra <= 0 {
				continue
			}
			sort.SliceStable(idxs, func(a, b int) bool {
				ra, rb := next[idxs[a]].Status == "running", next[idxs[b]].Status == "running"
				if ra != rb {
					return !ra
				}
				return next[idxs[a]].CreatedAt > next[idxs[b]].CreatedAt
			})
			for _, i := range idxs[:extra] {
				removed[next[i].ID] = true
				removedItems = append(removedItems, next[i])
			}
		}
		// 不够的:按本单配置新增
		dcs := make([]string, 0, len(target))
		for dc := range target {
			dcs = append(dcs, dc)
		}
		sort.Strings(dcs)
		for _, dc := range dcs {
			for n := len(byDC[dc]); n < target[dc]; n++ {
				item := types.QueueItem{
					ID: uuid.NewString(), AccountID: tpl.AccountID, PlanCode: tpl.PlanCode, Datacenter: dc,
					Options: append([]string(nil), opts...), Duration: tpl.Duration, Status: newStatus,
					CreatedAt: types.NowISO(), UpdatedAt: types.NowISO(), RetryInterval: tpl.RetryInterval,
					AutoPay: u.AutoPay, Coupon: strings.TrimSpace(u.Coupon), Preorder: tpl.Preorder,
					PreorderWaitStock: tpl.PreorderWaitStock,
				}
				if u.Duration != nil {
					item.Duration = *u.Duration
				}
				if u.RetryInterval > 0 {
					item.RetryInterval = u.RetryInterval
				}
				if shared {
					item.OrderGroup, item.GroupLimit = group, limit
				}
				next = append(next, item)
				added[item.ID] = true
				res.Added++
			}
		}
	}
	if len(removed) > 0 {
		kept := next[:0:0]
		for _, it := range next {
			if removed[it.ID] {
				continue
			}
			kept = append(kept, it)
		}
		next = kept
		res.Removed = len(removed)
		res.Updated -= len(removed)
	}
	if res.Updated < 0 {
		res.Updated = 0
	}

	// 合计数改了:先同步数据库里登记的上限,再写队列;写队列失败时把上限改回去
	var restoreLimit func()
	if shared && limit != tpl.GroupLimit {
		prev, had, err := s.DB.SetOrderGroupLimit(group, limit)
		if err != nil {
			return BundleResult{}, err
		}
		if had {
			restoreLimit = func() { _, _, _ = s.DB.SetOrderGroupLimit(group, prev) }
		}
	}
	if err := s.DB.ReplaceQueue(next); err != nil {
		if restoreLimit != nil {
			restoreLimit()
		}
		return BundleResult{}, err
	}
	s.Queue = next

	// 落库之后再中断/删除:失败时内存与磁盘都保持原样
	keptDCs := map[string]bool{}
	for _, it := range next {
		if want[it.ID] || added[it.ID] {
			keptDCs[dcKey(it.Datacenter)] = true
		}
	}
	for _, it := range removedItems {
		s.MarkTaskDeleted(it.ID)
		if !keptDCs[dcKey(it.Datacenter)] {
			// 这个机房整个不抢了:与删除任务一致,挡住监控在这个机房自动建单
			if err := s.SetAutomaticOrderBlockedLocked(it, true); err != nil {
				s.Logger.Warn("整单编辑:自动建单停止记录未能写入: "+err.Error(), "queue")
			}
		}
	}
	for _, it := range next {
		if it.Status != "running" || (!want[it.ID] && !added[it.ID]) {
			continue
		}
		if err := s.SetAutomaticOrderBlockedLocked(it, false); err != nil {
			s.Logger.Warn("整单编辑:自动建单停止记录未能更新: "+err.Error(), "queue")
		}
		if changed[it.ID] {
			s.UnregisterTaskCancel(it.ID) // 中断旧配置那一轮(结账发出后不受影响)
		}
	}
	s.WakeQueue()
	return res, nil
}

// groupSignature 决定下单行为的字段;变了才中断本轮、清零失败计数。
func groupSignature(it types.QueueItem) string {
	opts := append([]string(nil), it.Options...)
	sort.Strings(opts)
	return strings.Join([]string{strings.Join(opts, ","), it.Duration, it.Coupon, boolStr(it.AutoPay), it.OrderGroup, strconv.Itoa(it.GroupLimit), it.Status}, "|")
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
