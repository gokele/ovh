package telegram

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

// OrderResult 对应 Python: process_telegram_order 返回
type OrderResult struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	TotalOrders   int    `json:"total_orders"`
	CreatedOrders int    `json:"created_orders"`
}

// accountRegionLabel 给用户看的账户归属描述:"名字(子公司 US / US 区)"。
// TG 是唯一没有账户选择器的入口,下单落在哪个账户、哪个区,必须在回复里说清楚。
func accountRegionLabel(acc types.OVHAccount) (string, string) {
	sub := strings.ToUpper(strings.TrimSpace(acc.Zone))
	if sub == "" {
		sub = ovh.DefaultSubsidiaryForEndpoint(acc.Endpoint)
	}
	return sub, fmt.Sprintf("%s(子公司 %s / %s 区)", acc.Name, sub, ovh.SubsidiaryRegion(sub))
}

// emptyAvailabilityReason 解释"可用性查不到"到底是哪一种情况。
//
// 为什么需要它:EU / US / CA 三个站点的目录是彼此独立的系统,同一台机器在不同区
// 是不同的 planCode(实测 FR 与 CA 的 eco 目录 planCode 完全一致 99/99,
// 而 US 的 143 个里只有 28 个和 FR 重合;美区机型带 -us 后缀)。
// 拿欧区 planCode 打美区的 /dedicated/server/datacenter/availabilities,
// OVH 返回的是 HTTP 200 + 空数组 —— 不是错误,就是"没有这个机型"。
// 只回一句"无法获取可用性信息"会让用户以为是抢购程序坏了,而真正该做的是换本区的 planCode。
//
// 用目录判断而不是猜后缀:后缀规则是观察出来的,目录才是权威。
// 目录走 catalog 的 2 小时缓存,不额外消耗账户配额。
// 注意 eco 目录只覆盖 Kimsufi/Rise/Advance 这一档,所以"目录里没有"只用来补充说明,
// 不作为拦截条件 —— 否则会误杀不在 eco 目录里但确实能买的机型。
func emptyAvailabilityReason(state *app.State, accountID, planCode string, acc types.OVHAccount) string {
	sub, label := accountRegionLabel(acc)
	base := fmt.Sprintf("无法获取 %s 的可用性信息(本次使用账户 %s)", planCode, label)
	if _, err := catalog.AddonFamiliesForPlan(state, accountID, planCode); err != nil {
		if strings.Contains(err.Error(), "目录里没有 planCode") {
			return base + fmt.Sprintf(
				"\n\n%s 的目录里没有 %s。OVH 的 EU / US / CA 是三套彼此独立的系统,同一台机器在不同区是不同的 planCode"+
					"(美区机型通常带 -us 后缀),用别区的 planCode 查库存 OVH 只会返回空数组而不是报错。"+
					"\n请改用 %s 区的 planCode,或把 /buy 落到对应区的账户上。", sub, planCode, ovh.SubsidiaryRegion(sub))
		}
		// 目录本身没拉到(网络/429):不能据此断言 planCode 不存在,只说明少了一条判据
		return base + fmt.Sprintf("\n\n(顺带一提:%s 的目录本次也没拉到 —— %s,所以无法确认这个 planCode 是不是属于别的区)", sub, err.Error())
	}
	// planCode 在本区目录里确实存在 → 是真的全区无货/已下架,不是区域搞错了
	return base + fmt.Sprintf("\n\n%s 的目录里有 %s,所以不是区域搞错了,而是这个机型当前在所有机房都没有可售配置。", sub, planCode)
}

// ProcessOrder 对应 Python: process_telegram_order
func ProcessOrder(state *app.State, planCode, datacenter string, quantity int, options []string) OrderResult {
	if quantity < 1 {
		quantity = 1
	}
	// TG /buy 没有账户维度,落默认账户 —— 但必须在这里就把它解析成具体 ID 并写进队列项。
	// 以前 QueueItem.AccountID 留空,下单时才由 purchase 现取默认账户:
	// 中间只要有人改过默认账户(或删掉它),这一单就会用另一个账户、另一个区的凭据去下,
	// 而可用性/目录判断用的还是此刻这个账户的子公司,两边对不上。
	acc, ok := state.FindAccount("")
	if !ok {
		return OrderResult{Success: false, Message: "未配置任何 OVH 账户"}
	}
	accountID := acc.ID
	sub, accLabel := accountRegionLabel(acc)

	availByConfig := catalog.CheckServerAvailabilityWithConfigs(state, planCode, accountID)
	if len(availByConfig) == 0 {
		return OrderResult{Success: false, Message: emptyAvailabilityReason(state, accountID, planCode, acc)}
	}

	// 过滤配置
	type configEntry struct {
		key  string
		data *catalog.ConfigAvailability
	}
	configsToOrder := []configEntry{}
	if len(options) > 0 {
		for k, d := range availByConfig {
			// 检查用户 options 是否被该配置完全覆盖
			if subset(options, d.Options) {
				configsToOrder = append(configsToOrder, configEntry{key: k, data: d})
			}
		}
	} else {
		for k, d := range availByConfig {
			configsToOrder = append(configsToOrder, configEntry{key: k, data: d})
		}
	}
	if len(configsToOrder) == 0 {
		return OrderResult{Success: false, Message: fmt.Sprintf("未找到匹配的配置（指定选项: %v）", options)}
	}

	availableDCs := map[string]struct{}{}
	for _, e := range configsToOrder {
		for dc, status := range e.data.Datacenters {
			// comingSoon 也会走到这里,但它下不了单;用白名单避免为永远买不到的机型建单
			if catalog.IsAvailableForOrder(status) {
				availableDCs[dc] = struct{}{}
			}
		}
	}
	if len(availableDCs) == 0 {
		return OrderResult{Success: false, Message: fmt.Sprintf("%s 在账户 %s 可见的所有机房都无货", planCode, accLabel)}
	}
	dcsToOrder := []string{}
	if datacenter != "" {
		// 用户在 TG 里打的是展示名(比如孟买 mum),OVH 可用性/购物车认的是 API 名(ynm)。
		// 不转换的话 availableDCs 里永远查不到 mum,回一句"指定机房无货"——
		// 而 purchase 那边是转换过的,等于同一台机器在两个环节用了两个名字。
		apiDC := ovh.ConvertDisplayDCToAPIDC(datacenter)
		if _, ok := availableDCs[apiDC]; !ok {
			dcs := make([]string, 0, len(availableDCs))
			for dc := range availableDCs {
				dcs = append(dcs, dc)
			}
			sort.Strings(dcs)
			return OrderResult{Success: false, Message: fmt.Sprintf(
				"指定机房 %s 无货(账户 %s 当前有货的机房: %s)",
				strings.ToUpper(datacenter), accLabel, strings.Join(dcs, ", "))}
		}
		dcsToOrder = append(dcsToOrder, apiDC)
	} else {
		for dc := range availableDCs {
			dcsToOrder = append(dcsToOrder, dc)
		}
	}

	totalOrders := len(configsToOrder) * len(dcsToOrder) * quantity
	ordersToCreate := []types.QueueItem{}
	state.Logger.Info(fmt.Sprintf("[Telegram下单] 账户=%s, 子公司=%s, planCode=%s", accLabel, sub, planCode), "telegram")
	for _, ce := range configsToOrder {
		configOptions := append([]string{}, ce.data.Options...)
		state.Logger.Info(fmt.Sprintf("[Telegram下单] 处理配置: memory=%s, storage=%s, options=%v (数量: %d)",
			ce.data.Memory, ce.data.Storage, configOptions, len(configOptions)), "telegram")
		if len(configOptions) == 0 {
			state.Logger.Warn(fmt.Sprintf("[Telegram下单] ⚠️ 配置选项为空！memory=%s, storage=%s",
				ce.data.Memory, ce.data.Storage), "telegram")
		}
		for _, dc := range dcsToOrder {
			if status, ok := ce.data.Datacenters[dc]; ok && !catalog.IsAvailableForOrder(status) {
				continue
			}
			for i := 0; i < quantity; i++ {
				now := types.NowISO()
				item := types.QueueItem{
					ID:            uuid.NewString(),
					AccountID:     accountID,
					PlanCode:      planCode,
					Datacenter:    dc,
					Options:       append([]string{}, configOptions...),
					Status:        "running",
					CreatedAt:     now,
					UpdatedAt:     now,
					RetryInterval: 30,
					RetryCount:    0,
					LastCheckTime: 0,
					FromTelegram:  true,
				}
				ordersToCreate = append(ordersToCreate, item)
				state.Logger.Debug(fmt.Sprintf("[Telegram下单] 创建订单项: planCode=%s, datacenter=%s, options=%v (ID: %s)",
					planCode, dc, item.Options, item.ID[:8]), "telegram")
			}
		}
	}

	batchSize := 10
	totalBatches := (len(ordersToCreate) + batchSize - 1) / batchSize
	state.Logger.Info(fmt.Sprintf("开始并发创建订单: 总数=%d, 批次大小=%d, 总批次数=%d",
		len(ordersToCreate), batchSize, totalBatches), "telegram")
	created := 0
	var mu sync.Mutex
	for batchIdx := 0; batchIdx < totalBatches; batchIdx++ {
		start := batchIdx * batchSize
		end := start + batchSize
		if end > len(ordersToCreate) {
			end = len(ordersToCreate)
		}
		batch := ordersToCreate[start:end]
		var wg sync.WaitGroup
		for _, item := range batch {
			wg.Add(1)
			go func(it types.QueueItem) {
				defer wg.Done()
				state.QueueMu.Lock()
				state.Queue = append(state.Queue, it)
				state.QueueMu.Unlock()
				mu.Lock()
				created++
				mu.Unlock()
			}(item)
		}
		wg.Wait()
		state.Logger.Info(fmt.Sprintf("批次 %d/%d 完成: 本批次创建 %d 个订单", batchIdx+1, totalBatches, len(batch)), "telegram")
	}
	if created > 0 {
		_ = state.SaveQueue()
		state.Logger.Info(fmt.Sprintf("并发创建订单完成: 共创建 %d/%d 个订单", created, totalOrders), "telegram")
	}
	_ = time.Second
	return OrderResult{
		Success:       true,
		Message:       fmt.Sprintf("已创建 %d/%d 个订单(账户 %s)", created, totalOrders, accLabel),
		TotalOrders:   totalOrders,
		CreatedOrders: created,
	}
}

func subset(needle, haystack []string) bool {
	set := map[string]struct{}{}
	for _, h := range haystack {
		set[h] = struct{}{}
	}
	for _, n := range needle {
		if _, ok := set[n]; !ok {
			return false
		}
	}
	return true
}
