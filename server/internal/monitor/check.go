package monitor

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

// notification 单次状态变化通知（内部）
type notification struct {
	dc               string
	status           string
	oldStatus        string
	hasOld           bool
	statusKey        string
	changeType       string
	priceCheckFailed bool
	priceCheckError  string
	configTraceID    string
	traceID          string
	detectedTime     string
	durationText     string
}

func (n notification) oldStatusJSON() interface{} {
	if !n.hasOld {
		return nil
	}
	return n.oldStatus
}

// —— 可用性查询账户的区域选取 ——
//
// 为什么需要选账户:OVH 的 EU / US / CA 是三套彼此独立的系统,
// /dedicated/server/datacenter/availabilities 在三个站点各维护一份库存视图。
// 拿不属于本站点的 planCode 去查,OVH 返回的是 **HTTP 200 + 空数组**,不是错误。
// 实测(未鉴权公开调用,三站点同一条 query):
//
//	24sk102-us  : EU 200 []      US 200 2条    CA 200 []
//	24adv01-v3  : EU 200 32条    US 200 []     CA 200 32条
//	24sk202-sgp : EU 200 4条     US 200 4条    CA 200 4条
//
// 也就是说:美区目录的 -us/-eu/-ca 后缀机型只有 US 站点查得到,
// 欧区目录的机型只有 EU/CA 站点查得到(EU 与 CA 的库存视图实测逐条一致),
// 少数亚太机型三边都有。
//
// 订阅没设 AutoOrderAccountID(只通知不下单)时,以前一律落默认账户 ——
// 默认账户是欧区、订阅的是美区 planCode,就永远返回空数组、永远不告警、也不报错。
// 现在改成:按 planCode 在各账户子公司公开目录里的归属自动挑一个区域正确的账户。

// checkAccountTTL 查询账户选取结果的缓存时长。
// 目录本身已有 2 小时缓存,这里再缓一层是为了不让「每订阅 × 每 5 秒」的检查
// 一轮轮地把所有账户遍历一遍、并反复刷同样的日志。
const checkAccountTTL = 10 * time.Minute

// transientChoiceTTL 「这轮没判出来」这类结论的缓存时长。
// 短是因为它是失败态:OVH 抖一下就该尽快重试,但也不能每 5 秒重试一次把目录接口打爆。
const transientChoiceTTL = 60 * time.Second

// queryAccountChoice 一次选取的结果
type queryAccountChoice struct {
	accountID  string // 用来查可用性(以及验价)的账户;"" = 默认账户
	region     string // EU / US / CA
	subsidiary string // ovhSubsidiary
	// degradeReason 非空 = 没能选到区域正确的账户,给用户看的中文说明。
	degradeReason string
	// definitive 为 true 表示「目录确实拉到了,而且里面没有这个 planCode」——
	// 这时再去打 OVH 只会白拿一个空数组,直接跳过。
	// 目录拉取失败时是 false:不能凭一次网络抖动就断言机型不存在。
	definitive bool
	at         time.Time
	// ttl 这条结论的缓存时长;0 = 用 checkAccountTTL。
	// "暂时判不出"的结论只缓很短一会儿,不能和确定性结论同寿命。
	ttl time.Duration
}

// effectiveTTL 这条结论能被复用多久。
func (c queryAccountChoice) effectiveTTL() time.Duration {
	if c.ttl > 0 {
		return c.ttl
	}
	return checkAccountTTL
}

var (
	queryAccountMu    sync.Mutex
	queryAccountCache = map[string]queryAccountChoice{} // planCode + 账户指纹 → 选取结果
)

// accountsFingerprint 当前账户集合的指纹(id/endpoint/zone)。
// 拼进缓存 key,这样新增/删除/改区一个账户后旧结果自动失效,
// 不需要账户 CRUD 那边记得来调一个 Invalidate —— 那种跨包的"记得调"迟早会漏。
func (m *Monitor) accountsFingerprint() string {
	m.state.AccountsMu.RLock()
	parts := make([]string, 0, len(m.state.Accounts))
	for _, a := range m.state.Accounts {
		parts = append(parts, a.ID+":"+strings.ToLower(a.Endpoint)+":"+strings.ToUpper(a.Zone))
	}
	m.state.AccountsMu.RUnlock()
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// planInAccountCatalog 判断 planCode 是否出现在该账户子公司的公开目录里。
// 返回 (在目录里, 结论是否确定)。走 catalog 的 2 小时内存缓存,
// 不消耗账户 API 配额,也不会在热路径上现拉整份 10MB 目录。
//
// 必须把 catalog.ErrPlanNotInCatalog 和其它错误分开:前者是确定性结论
// (机型不属于这个大区,重试一万次也一样),后者只是目录暂时拉不到 ——
// 凭一次网络抖动就断言"机型不存在"会把正常订阅误停掉。
func (m *Monitor) planInAccountCatalog(accountID, planCode string) (inCatalog bool, definitive bool) {
	_, err := catalog.AddonFamiliesForPlan(m.state, accountID, planCode)
	switch {
	case err == nil:
		return true, true
	case errors.Is(err, catalog.ErrPlanNotInCatalog):
		// 注意:"不在 eco 目录"**不等于**"不属于这个大区"。
		// 实测 EU 站点 availabilities 有 244 个 planCode,eco 目录只有 99 个 ——
		// Scale / HCI / SDS / High-Grade 整条产品线都不在目录里却有真实库存。
		// 所以这里只当作"这个大区大概率不是它的归属",绝不作为确定性结论,
		// 真正的归属判定交给 catalog.RegionOfPlan(打可用性接口)。
		return false, false
	default:
		m.state.Logger.Debug(fmt.Sprintf("[monitor/region] 探测 %s 是否属于账户 %s 的目录失败(不据此判定): %s",
			planCode, accountID, err.Error()), "monitor")
		return false, false
	}
}

// probePlanRegionAccount 目录判不出归属时,用公开的可用性接口逐大区探一次,
// 命中的大区里挑一个账户来查。这条路径能覆盖 eco 目录里没有的机型(Scale/HCI 等)。
// 返回 (选中的账户, 是否命中, 三区都确认没有记录)。
// 第三个返回值才是"确定性结论"的唯一来源:只有所有候选大区都探测成功、且都没有记录,
// 才敢说这个 planCode 在已配置账户覆盖范围内不存在。探测本身失败一律不下结论。
func (m *Monitor) probePlanRegionAccount(planCode string, candidates []types.OVHAccount) (queryAccountChoice, bool, bool) {
	regions := make([]string, 0, 3)
	for _, acc := range candidates {
		r, _ := accountRegionInfo(acc)
		regions = append(regions, r)
	}
	region, err := catalog.RegionOfPlan(m.state, planCode, regions)
	if err != nil {
		return queryAccountChoice{}, false, false
	}
	if region == "" {
		return queryAccountChoice{}, false, true // 探测都成功了,确实哪个区都没有
	}
	for _, acc := range candidates {
		r, subsidiary := accountRegionInfo(acc)
		if r != region {
			continue
		}
		m.state.Logger.Info(fmt.Sprintf("[monitor/region] %s 未出现在 eco 目录,但 %s 站点的可用性接口有记录,用账户「%s」查询",
			planCode, region, acc.Name), "monitor")
		return queryAccountChoice{accountID: acc.ID, region: region, subsidiary: subsidiary, at: time.Now()}, true, false
	}
	return queryAccountChoice{}, false, false
}

// explainEmptyAvailability 查询账户明明选出来了、可用性接口却返回空时,给一个说得清的原因。
//
// 为什么要多探这一下:OVH 三个站点互不相通,拿别站点的 planCode 去查是 HTTP 200 + 空数组
// (实测 24sk102-us 在 EU/CA 都是 0 条、US 是 2 条),和"这台机器真的下架了"长得一模一样。
// 只有把"别的大区能查到记录"这件事查出来,用户才知道该去改订阅的账户而不是干等补货。
//
// 探测走 catalog.RegionOfPlan(公开可用性接口 + 10 分钟缓存,不消耗账户配额),
// 候选大区只取已配置账户覆盖到的那些 —— 本项目不另外维护大区枚举,
// 想覆盖"你压根没有该大区账户"的情况需要 ovh 包提供全量大区列表(见 needsOther)。
func (m *Monitor) explainEmptyAvailability(planCode string, choice queryAccountChoice) string {
	generic := fmt.Sprintf("已用 %s 站点(子公司 %s)查询,OVH 未返回 %s 的任何配置组合(机型可能已下架,或 OVH 接口暂时异常)",
		choice.region, choice.subsidiary, planCode)

	// 本轮用的大区排第一:它刚刚已经被证实是空的,让 RegionOfPlan 先确认这一点,
	// 命中别的大区才说明是区域错配。
	regions := []string{choice.region}
	m.state.AccountsMu.RLock()
	for _, a := range m.state.Accounts {
		r, _ := accountRegionInfo(a)
		regions = append(regions, r)
	}
	m.state.AccountsMu.RUnlock()

	region, err := catalog.RegionOfPlan(m.state, planCode, regions)
	if err != nil || region == "" || region == choice.region {
		// 探测失败 / 哪个区都没有 / 就在本区(那就是真的没数据)—— 都不改口径,给通用说明
		return generic
	}
	return fmt.Sprintf(
		"机型 %s 的库存记录只在 %s 站点查得到,而本轮用的是 %s 站点(子公司 %s)的账户。"+
			"OVH 的 EU/US/CA 三套系统互不相通,这条订阅在当前账户下既查不到库存也下不了单,"+
			"请把订阅的下单账户改成 %s 大区的账户",
		planCode, region, choice.region, choice.subsidiary, region)
}

// accountRegionInfo 取账户的大区与子公司。子公司口径统一走 catalog.SubsidiaryOfAccount,
// 大区归属统一走 ovh.SubsidiaryRegion —— 全项目就这一份权威表,这里不另立映射。
func accountRegionInfo(acc types.OVHAccount) (region, subsidiary string) {
	subsidiary = catalog.SubsidiaryOfAccount(acc)
	return ovh.SubsidiaryRegion(subsidiary), subsidiary
}

// resolveQueryAccount 决定这条订阅该用哪个账户去查可用性 / 验价。
//
// 优先级:
//  1. 订阅显式指定了 AutoOrderAccountID → 用它。验价账户和下单账户必须是同一个,
//     否则「A 账户验得出价、B 账户下不了单」。账户不存在时只能报错,不能悄悄改用默认账户。
//  2. 否则按 planCode 在各账户子公司目录里的归属挑:默认账户优先(保持既有行为),
//     其次按账户顺序找第一个目录里有这个 plan 的。
//  3. 一个都没有 → 用可用性接口逐大区探,再不行落默认账户并给出明确的降级说明。
//
// 参数是 (planCode, autoOrderAccountID) 而不是 *Subscription:这两个值必须由调用方
// 在持订阅锁时取好(见 Subscription 的并发约定),而且 PreflightRegion 手里根本没有订阅对象。
func (m *Monitor) resolveQueryAccount(planCode, autoOrderAccountID string) queryAccountChoice {
	// 缓存 key 里带上 autoOrderAccountID 和账户指纹:
	// 订阅改了下单账户 → key 变 → 立刻生效;账户增删改区 → 指纹变 → 立刻失效。
	// 所以显式账户这条路径同样可以吃缓存,不必像以前那样每订阅每 5 秒重算一遍
	// (那条路径里的 planInAccountCatalog 在目录缓存过期时会现拉一份整目录)。
	cacheKey := planCode + "@" + autoOrderAccountID + "@" + m.accountsFingerprint()
	queryAccountMu.Lock()
	if c, ok := queryAccountCache[cacheKey]; ok && time.Since(c.at) < c.effectiveTTL() {
		queryAccountMu.Unlock()
		return c
	}
	queryAccountMu.Unlock()

	cache := func(c queryAccountChoice) queryAccountChoice {
		c.at = time.Now()
		queryAccountMu.Lock()
		queryAccountCache[cacheKey] = c
		queryAccountMu.Unlock()
		return c
	}

	if autoOrderAccountID != "" {
		acc, ok := m.state.FindAccount(autoOrderAccountID)
		if !ok {
			return cache(queryAccountChoice{
				degradeReason: fmt.Sprintf("订阅指定的下单账户 %s 已不存在,无法确定该用哪个大区查询库存,请重新选择账户",
					autoOrderAccountID),
			})
		}
		region, subsidiary := accountRegionInfo(acc)
		// 这里**不再**用 eco 目录判"机型是不是属于这个账户的大区":
		// 实测 EU 站点 availabilities 有 244 个 planCode、eco 目录只有 99 个,
		// Scale/HCI/SDS/High-Grade 整条线不在目录里却有真实库存 ——
		// 按目录判会把这些订阅打上"区域错配"的假警报。
		// 真正的错配留到查出空结果时由 explainEmptyAvailability 用可用性接口坐实。
		return cache(queryAccountChoice{accountID: acc.ID, region: region, subsidiary: subsidiary})
	}

	m.state.AccountsMu.RLock()
	accounts := make([]types.OVHAccount, len(m.state.Accounts))
	copy(accounts, m.state.Accounts)
	m.state.AccountsMu.RUnlock()

	if len(accounts) == 0 {
		return cache(queryAccountChoice{degradeReason: "未配置任何 OVH 账户,无法查询库存"})
	}

	// 候选顺序:默认账户排最前,保证「plan 在默认账户大区里」时行为与以前完全一致
	candidates := make([]types.OVHAccount, 0, len(accounts))
	for _, a := range accounts {
		if a.IsDefault {
			candidates = append(candidates, a)
		}
	}
	for _, a := range accounts {
		if !a.IsDefault {
			candidates = append(candidates, a)
		}
	}

	triedRegions := map[string]struct{}{}
	for _, acc := range candidates {
		region, subsidiary := accountRegionInfo(acc)
		triedRegions[region] = struct{}{}
		// planInAccountCatalog 只有"确实在目录里"时才返回 (true, true),
		// 其余情况(不在目录 / 拉取失败)都是不确定 —— 见该函数上的说明。
		// 所以这里只把它当作"快速命中"的捷径,判不出来一律交给下面的可用性探测。
		if in, definitive := m.planInAccountCatalog(acc.ID, planCode); !definitive || !in {
			continue
		}
		m.state.Logger.Info(fmt.Sprintf("[monitor/region] %s 归属 %s 站点(子公司 %s),用账户「%s」查询库存",
			planCode, region, subsidiary, acc.Name), "monitor")
		return cache(queryAccountChoice{accountID: acc.ID, region: region, subsidiary: subsidiary})
	}

	// 目录里找不到 —— 先用公开的可用性接口逐大区探一次再下结论。
	// eco 目录只覆盖能用 /order/cart/eco 下单的机型,而监控要覆盖的机型远不止这些。
	c, hit, probedAbsent := m.probePlanRegionAccount(planCode, candidates)
	if hit {
		return cache(c)
	}
	// 确定性结论只认探测结果:三区都探成功且都没有记录,才允许停止每 5 秒白打 OVH
	allDefinitive := probedAbsent

	// 没有任何账户的大区目录里有这个 plan
	regions := make([]string, 0, len(triedRegions))
	for r := range triedRegions {
		regions = append(regions, r)
	}
	sort.Strings(regions)
	choice := queryAccountChoice{}
	if allDefinitive {
		choice.definitive = true
		choice.degradeReason = fmt.Sprintf(
			"机型 %s 在已配置账户覆盖的大区(%s)里既不在目录、可用性接口也查不到任何记录。"+
				"OVH 的 EU/US/CA 三个站点互不相通(带 -us/-eu/-ca 后缀的机型只在对应站点出售),"+
				"请确认 planCode 是否拼写正确,或添加对应大区的账户",
			planCode, strings.Join(regions, "/"))
	} else {
		// 目录 / 探测没跑成,不能下确定性结论:退回默认账户按老路走一次,过一小会儿再判。
		// 这里必须用短 TTL —— 以前和成功结论一样缓 10 分钟,等于让一次网络抖动
		// 把"暂时判不出"钉死 10 分钟,注释写着"下轮再判"其实下轮读的还是这条缓存。
		choice.ttl = transientChoiceTTL
		choice.degradeReason = fmt.Sprintf(
			"暂时无法确定机型 %s 属于哪个大区(公开目录/可用性探测暂时失败),本轮退回默认账户查询", planCode)
	}
	return cache(choice)
}

// PreflightRegion 添加订阅时的区域预检:告诉调用方这个 planCode 会由哪个大区的账户来查,
// 以及有没有明显的区域错配。返回的 warning 非空时只是提示,不阻止订阅 ——
// 目录有 2 小时缓存、OVH 也会临时抖动,不能凭一次探测就把用户挡在门外。
//
// autoOrderAccountID 传订阅要用的下单账户(可为空),口径与真正检查时完全一致。
func (m *Monitor) PreflightRegion(planCode, autoOrderAccountID string) (region, subsidiary, warning string) {
	c := m.resolveQueryAccount(planCode, autoOrderAccountID)
	return c.region, c.subsidiary, c.degradeReason
}

// CheckAvailabilityChange 对应 Python: check_availability_change
//
// 并发:同一条订阅同一时刻只会有一个 goroutine 在跑这里(loop 每轮一个,轮间 wg.Wait),
// 但 HTTP 侧随时可能读同一条订阅。所以本函数对 sub 的所有读写都走 types.go 里的带锁方法,
// 中间态(lastStatus)先在本地副本上算,算完一次性写回。
func (m *Monitor) CheckAvailabilityChange(sub *Subscription, traceID string) {
	planCode := sub.PlanCode // 创建后不变,可以裸读

	// 订阅配置在本轮开始时取一份快照,后面统一用它 ——
	// 边跑边读 sub.X 既有竞争,也会让一轮检查横跨用户改配置的时刻。
	cfg := sub.checkConfig()

	// 查询账户必须按 planCode 所属大区选,不能一律落默认账户:
	// 三个站点的库存视图独立,查错站点是 HTTP 200 + 空数组而不是报错(见 resolveQueryAccount 上的实测)。
	choice := m.resolveQueryAccount(planCode, cfg.AutoOrderAccountID)

	// beginCheck 写入本轮诊断信息并返回上一轮的错误原因,用来给日志去重:
	// 检查每 5 秒跑一轮,同一条区域错配如果每轮都 Warn 一次,一天能刷出一万七千条一模一样的日志。
	prevErr := sub.beginCheck(m.nowBeijing().Format(time.RFC3339Nano),
		choice.accountID, choice.region, choice.subsidiary)

	if choice.degradeReason != "" && choice.degradeReason != prevErr {
		m.state.Logger.Warn(fmt.Sprintf("[monitor/region] 订阅 %s: %s", planCode, choice.degradeReason), "monitor")
	}
	if choice.degradeReason != "" {
		// 选账户这一步就出了问题(账户被删 / 判不出归属),不管后面查不查得到数据,
		// 都必须让用户在 region_issues 里看见 —— 以前只在"查出来是空"时才写进 LastCheckError,
		// 于是"订阅指定的下单账户已被删除、现在拿默认账户在查"这种事,
		// 只要默认账户碰巧有数据就完全无声无息。
		sub.setCheckError(choice.degradeReason)
	}
	if choice.definitive && choice.accountID == "" {
		// 可用性接口已经逐区确认过:已配置账户覆盖的大区里根本没有这个机型的记录。
		// 再打 OVH 也只会拿到空数组,每 5 秒白跑一次。原因已写进订阅状态(前端 region_issues 可见),
		// 不是静默跳过;而且这个结论 10 分钟后会自动重探,机型上架后能自愈。
		return
	}
	if prevErr != "" && choice.degradeReason == "" {
		m.state.Logger.Info(fmt.Sprintf("[monitor/region] 订阅 %s 的区域问题已恢复,改用 %s 站点(子公司 %s)查询",
			planCode, choice.region, choice.subsidiary), "monitor")
	}

	// 监控用选中账户的 subsidiary 拉 catalog,这样跨子公司 multi-account
	// 触发 auto-order 时,options 匹配能命中目标账户独有的项。
	currentAvailability := catalog.CheckServerAvailabilityWithConfigs(m.state, planCode, choice.accountID)
	if len(currentAvailability) == 0 {
		// 区分两种"空":选到了区域正确的账户还是空 → OVH 侧问题;
		// 用的账户其实在别的大区 → 区域配错,必须说清楚,不能只丢一句"无法获取"。
		reason := choice.degradeReason
		if reason == "" {
			reason = m.explainEmptyAvailability(planCode, choice)
		}
		sub.setCheckError(reason)
		if reason != prevErr {
			m.state.Logger.Warn(fmt.Sprintf("无法获取 %s 的可用性信息: %s", planCode, reason), "monitor")
		}
		return
	}

	// 状态先在副本上推进,循环结束一次性写回:HTTP 侧读到的要么是上轮的完整状态、
	// 要么是本轮的完整状态,不会是推进到一半的中间态。
	lastStatus := sub.statusSnapshot()
	monitoredDCs := cfg.Datacenters

	m.state.Logger.Info(fmt.Sprintf("订阅 %s - 监控数据中心: %v", planCode, monitoredDCs), "monitor")
	m.state.Logger.Info(fmt.Sprintf("订阅 %s - 当前发现 %d 个配置组合", planCode, len(currentAvailability)), "monitor")

	for configKey, configData := range currentAvailability {
		memory := configData.Memory
		storage := configData.Storage
		configDisplay := memory + " + " + storage

		configTraceID := uuid.NewString()
		m.state.Logger.Info(fmt.Sprintf("检查配置: %s [config-trace:%s]", configDisplay, configTraceID), "monitor")

		configInfo := map[string]interface{}{
			"memory":  memory,
			"storage": storage,
			"display": configDisplay,
			"options": configData.Options,
		}
		// 询价必须走上面选中的那个账户:验价账户和下单账户不一致时,
		// endpoint 与 ovhSubsidiary 都会错(A 账户买得到 B 账户买不到,反之亦然)。
		// 订阅指定了 auto_order 账户时 choice.accountID 就是它,两者天然一致;
		// 没指定时用的是区域正确的那个账户 —— 以前这里传空串会落到默认账户,
		// 美区机型就会拿欧区购物车去验价,币种和可售性全错。
		// 账户 ID 只加在传给询价/通知的副本上,不污染写进订阅历史的 configInfo。
		priceCfg := copyMap(configInfo)
		priceCfg["account_id"] = choice.accountID

		type dcStatus struct {
			status    string
			statusKey string
			oldStatus string
			hasOld    bool
			orderable bool
		}
		dcStatusMap := map[string]dcStatus{}
		priceCheckTasks := []string{}
		for dc, status := range configData.Datacenters {
			if len(monitoredDCs) > 0 && !containsString(monitoredDCs, dc) {
				continue
			}
			statusKey := dc + "|" + configKey
			old, hasOld := lastStatus[statusKey]
			// 白名单判定:只有 \d+H 家族算有货。以前 `!= "unavailable"` 把 comingSoon
			// (即将上线、尚未开售)和 unknown 都当成有货,会对永远下不了单的机型
			// 反复验价、反复发"有货但价格校验失败"的骚扰通知。
			orderable := catalog.IsAvailableForOrder(status)
			dcStatusMap[dc] = dcStatus{status: status, statusKey: statusKey, oldStatus: old, hasOld: hasOld, orderable: orderable}
			if orderable {
				priceCheckTasks = append(priceCheckTasks, dc)
			}
		}

		// 并发价格校验
		priceCheckResults := map[string][2]interface{}{}
		if len(priceCheckTasks) > 0 {
			var pcMu sync.Mutex
			var wg sync.WaitGroup
			workers := len(priceCheckTasks)
			if workers > 10 {
				workers = 10
			}
			sem := make(chan struct{}, workers)
			for _, dc := range priceCheckTasks {
				wg.Add(1)
				sem <- struct{}{}
				go func(dc string) {
					defer wg.Done()
					defer func() { <-sem }()
					ok, errMsg := m.verifyPriceAvailable(planCode, dc, priceCfg)
					pcMu.Lock()
					priceCheckResults[dc] = [2]interface{}{ok, errMsg}
					pcMu.Unlock()
					if ok {
						m.state.Logger.Info(fmt.Sprintf("%s@%s [%s] 价格校验通过 [config-trace:%s]",
							planCode, dc, configDisplay, configTraceID), "monitor")
					} else {
						m.state.Logger.Info(fmt.Sprintf("%s@%s [%s] 价格校验失败，原因: %s [config-trace:%s]",
							planCode, dc, configDisplay, errMsg, configTraceID), "monitor")
					}
				}(dc)
			}
			wg.Wait()
		}

		notifications := []notification{}

		for dc, ds := range dcStatusMap {
			// 状态机(以及落进 lastStatus 的值)只认 available / unavailable /
			// price_check_failed 三种规范化状态,不可下单的原始值(unavailable /
			// unknown / comingSoon)统一折成 unavailable
			actualStatus := "unavailable"
			priceCheckFailed := false
			priceCheckError := ""

			if ds.orderable {
				if v, ok := priceCheckResults[dc]; ok {
					okBool, _ := v[0].(bool)
					errStr, _ := v[1].(string)
					if !okBool {
						actualStatus = "price_check_failed"
						priceCheckFailed = true
						priceCheckError = errStr
						m.state.Logger.Info(fmt.Sprintf("%s@%s [%s] 可用性显示有货但价格校验失败，原因: %s，标记为price_check_failed",
							planCode, dc, configDisplay, errStr), "monitor")
					} else {
						actualStatus = "available"
						m.state.Logger.Info(fmt.Sprintf("%s@%s [%s] 可用性有货且价格校验通过，确认有货",
							planCode, dc, configDisplay), "monitor")
					}
				} else {
					actualStatus = "price_check_failed"
					priceCheckFailed = true
					priceCheckError = "价格校验未执行"
				}
			}

			statusChanged := false
			changeType := ""

			if !ds.hasOld {
				if actualStatus == "price_check_failed" {
					m.state.Logger.Info(fmt.Sprintf("首次检查: %s@%s [%s] 可用性有货但价格校验失败，发送通知",
						planCode, dc, configDisplay), "monitor")
					if cfg.NotifyAvailable {
						statusChanged = true
						changeType = "price_check_failed"
					}
				} else if actualStatus == "unavailable" {
					m.state.Logger.Info(fmt.Sprintf("首次检查: %s@%s [%s] 无货", planCode, dc, configDisplay), "monitor")
					if cfg.NotifyUnavailable {
						statusChanged = true
						changeType = "unavailable"
					}
				} else {
					m.state.Logger.Info(fmt.Sprintf("首次检查: %s@%s [%s] 有货（价格校验通过），发送通知",
						planCode, dc, configDisplay), "monitor")
					if cfg.NotifyAvailable {
						statusChanged = true
						changeType = "available"
					}
				}
			} else if ds.oldStatus == "unavailable" && actualStatus == "available" {
				if cfg.NotifyAvailable {
					statusChanged = true
					changeType = "available"
					m.state.Logger.Info(fmt.Sprintf("%s@%s [%s] 从无货变有货（价格校验通过）",
						planCode, dc, configDisplay), "monitor")
				}
			} else if ds.oldStatus == "unavailable" && actualStatus == "price_check_failed" {
				m.state.Logger.Info(fmt.Sprintf("%s@%s [%s] 从无货变可用性有货但价格校验失败，发送通知",
					planCode, dc, configDisplay), "monitor")
				if cfg.NotifyAvailable {
					statusChanged = true
					changeType = "price_check_failed"
				}
			} else if ds.oldStatus == "price_check_failed" && actualStatus == "available" {
				if cfg.NotifyAvailable {
					statusChanged = true
					changeType = "available"
					m.state.Logger.Info(fmt.Sprintf("%s@%s [%s] 从价格校验失败变有货（价格校验通过）",
						planCode, dc, configDisplay), "monitor")
				}
			} else if ds.oldStatus == "price_check_failed" && actualStatus == "unavailable" {
				if cfg.NotifyUnavailable {
					statusChanged = true
					changeType = "unavailable"
					m.state.Logger.Info(fmt.Sprintf("%s@%s [%s] 从价格校验失败变无货",
						planCode, dc, configDisplay), "monitor")
				}
			} else if ds.oldStatus == "available" && actualStatus == "unavailable" {
				if cfg.NotifyUnavailable {
					statusChanged = true
					changeType = "unavailable"
					m.state.Logger.Info(fmt.Sprintf("%s@%s [%s] 从有货变无货", planCode, dc, configDisplay), "monitor")
				}
			} else if ds.oldStatus == "available" && actualStatus == "price_check_failed" {
				m.state.Logger.Info(fmt.Sprintf("%s@%s [%s] 从有货变可用性有货但价格校验失败，发送通知",
					planCode, dc, configDisplay), "monitor")
				if cfg.NotifyAvailable {
					statusChanged = true
					changeType = "price_check_failed"
				}
			}

			if statusChanged {
				detectedTime := m.nowBeijing().Format(time.RFC3339Nano)
				n := notification{
					dc:               dc,
					status:           actualStatus,
					oldStatus:        ds.oldStatus,
					hasOld:           ds.hasOld,
					statusKey:        ds.statusKey,
					changeType:       changeType,
					priceCheckFailed: priceCheckFailed,
					priceCheckError:  priceCheckError,
					configTraceID:    configTraceID,
					traceID:          traceID,
					detectedTime:     detectedTime,
				}
				if changeType == "available" && ds.oldStatus == "unavailable" {
					n.durationText = m.calcDuration(sub, dc, configDisplay, []string{"unavailable", "price_check_failed"})
				}
				notifications = append(notifications, n)
			}

			lastStatus[ds.statusKey] = actualStatus
		}

		// 价格查询（同一配置只查一次）
		var priceText string
		var priceFetchError string
		hasAvail := false
		for _, n := range notifications {
			if n.changeType == "available" {
				hasAvail = true
				break
			}
		}
		if hasAvail {
			firstDC := ""
			for _, n := range notifications {
				if n.changeType == "available" && n.status != "unavailable" {
					firstDC = n.dc
					break
				}
			}
			if firstDC != "" {
				priceText, priceFetchError = m.getPriceWithTimeout(planCode, firstDC, priceCfg, 30*time.Second)
				if priceText != "" {
					m.state.Logger.Debug(fmt.Sprintf("配置 %s 价格获取成功: %s，将在所有通知中复用", configDisplay, priceText), "monitor")
				} else {
					m.state.Logger.Warn(fmt.Sprintf("配置 %s 价格获取失败，通知中不包含价格信息", configDisplay), "monitor")
					if priceFetchError == "" {
						priceFetchError = "价格接口未返回结果"
					}
				}
			}
		}

		// 分类
		var availables, unavailables, priceFailed []notification
		for _, n := range notifications {
			switch n.changeType {
			case "available":
				availables = append(availables, n)
			case "unavailable":
				unavailables = append(unavailables, n)
			case "price_check_failed":
				priceFailed = append(priceFailed, n)
			}
		}

		// 自动下单
		orderTargets := []notification{}
		for _, n := range availables {
			if !n.priceCheckFailed && (!n.hasOld || n.oldStatus == "unavailable") {
				orderTargets = append(orderTargets, n)
			}
		}
		// 触发条件:订阅勾了 AutoOrder + 指定了某个账户。
		// 没账户(AutoOrderAccountID="") → 只发可用通知,不下单(用户明确决定)。
		if len(orderTargets) > 0 && cfg.AutoOrder {
			switch {
			case cfg.AutoOrderAccountID == "":
				m.state.Logger.Info(fmt.Sprintf("[monitor] %s 触发 auto_order 但未指定账户,只通知不下单", planCode), "monitor")
			case choice.accountID != cfg.AutoOrderAccountID:
				// 查询/验价账户与下单账户不是同一个 —— 只可能发生在下单账户已被删除、
				// 本轮退回默认账户查询的时候。这种单子照发出去,买到的会是"另一个账户看到的价格
				// 和可售性",甚至直接被 quick-order 以账户不存在拒掉。宁可不下单,只通知。
				m.state.Logger.Warn(fmt.Sprintf(
					"[monitor] %s 跳过自动下单:验价用的是账户 %q,而订阅指定的下单账户是 %q,两者不一致(下单账户可能已被删除)",
					planCode, choice.accountID, cfg.AutoOrderAccountID), "monitor")
			default:
				m.batchOrder(planCode, configInfo, orderTargets, cfg.Quantity, cfg.AutoOrderAccountID)
			}
		}

		// 发送有货通知
		if len(availables) > 0 {
			m.state.Logger.Info(fmt.Sprintf("准备发送汇总提醒: %s [%s] - %d个机房有货",
				planCode, configDisplay, len(availables)), "monitor")
			configInfoWithPrice := copyMap(priceCfg)
			if priceText != "" {
				configInfoWithPrice["cached_price"] = priceText
			}
			availDCs := make([]map[string]interface{}, 0, len(availables))
			for _, n := range availables {
				dcInfo := map[string]interface{}{"dc": n.dc, "status": n.status}
				if n.durationText != "" {
					dcInfo["duration_text"] = n.durationText
				}
				if n.detectedTime != "" {
					dcInfo["detected_time"] = n.detectedTime
				}
				availDCs = append(availDCs, dcInfo)
			}
			configTraceForNotif := ""
			if len(availables) > 0 {
				configTraceForNotif = availables[0].configTraceID
			}
			errIfNoPrice := ""
			if priceText == "" {
				errIfNoPrice = priceFetchError
			}
			m.SendAvailabilityAlertGrouped(planCode, availDCs, configInfoWithPrice, cfg.ServerName,
				errIfNoPrice, traceID, configTraceForNotif)

			entries := make([]HistoryEntry, 0, len(availables))
			for _, n := range availables {
				entries = append(entries, HistoryEntry{
					Timestamp:  m.nowBeijing().Format(time.RFC3339Nano),
					Datacenter: n.dc,
					Status:     n.status,
					ChangeType: n.changeType,
					OldStatus:  n.oldStatusJSON(),
					Config:     configInfo,
				})
			}
			sub.appendHistory(entries, maxHistorySize)
		}

		// 价格校验失败通知
		for _, n := range priceFailed {
			m.state.Logger.Info(fmt.Sprintf("准备发送价格校验失败提醒: %s@%s [%s] - 可用性有货但价格校验失败",
				planCode, n.dc, configDisplay), "monitor")
			priceTextFailed := m.GetPriceInfoText(planCode, n.dc, priceCfg)
			configInfoFailed := copyMap(priceCfg)
			if priceTextFailed != "" {
				configInfoFailed["cached_price"] = priceTextFailed
				configInfoFailed["price_check_error"] = n.priceCheckError
			}
			m.SendAvailabilityAlert(planCode, n.dc, "unavailable", "price_check_failed",
				configInfoFailed, cfg.ServerName, "", n.priceCheckError, traceID, n.configTraceID, n.detectedTime)
			sub.appendHistory([]HistoryEntry{{
				Timestamp:  m.nowBeijing().Format(time.RFC3339Nano),
				Datacenter: n.dc,
				Status:     "price_check_failed",
				ChangeType: "price_check_failed",
				OldStatus:  n.oldStatusJSON(),
				Config:     configInfo,
			}}, maxHistorySize)
		}

		// 下架聚合通知
		if len(unavailables) > 0 {
			m.state.Logger.Info(fmt.Sprintf("准备发送聚合下架提醒: %s [%s] - %d个机房",
				planCode, configDisplay, len(unavailables)), "monitor")
			unavailDCs := make([]map[string]interface{}, 0, len(unavailables))
			for _, n := range unavailables {
				dcInfo := map[string]interface{}{"dc": n.dc, "status": n.status}
				isBecame := n.changeType == "unavailable" && n.hasOld && n.oldStatus != "unavailable"
				if isBecame {
					if d := m.calcDuration(sub, n.dc, configDisplay, []string{"available"}); d != "" {
						dcInfo["duration_text"] = d
					}
				}
				unavailDCs = append(unavailDCs, dcInfo)
			}
			configTraceForNotif := ""
			if len(unavailables) > 0 {
				configTraceForNotif = unavailables[0].configTraceID
			}
			m.SendUnavailableAlertGrouped(planCode, unavailDCs, configInfo, cfg.ServerName,
				traceID, configTraceForNotif)
			entries := make([]HistoryEntry, 0, len(unavailables))
			for _, n := range unavailables {
				entries = append(entries, HistoryEntry{
					Timestamp:  m.nowBeijing().Format(time.RFC3339Nano),
					Datacenter: n.dc,
					Status:     n.status,
					ChangeType: n.changeType,
					OldStatus:  n.oldStatusJSON(),
					Config:     configInfo,
				})
			}
			sub.appendHistory(entries, maxHistorySize)
		}
	}

	// 这里以前会给"本轮没监控的机房"也写一条 lastStatus,而且写的是 OVH 原始可用性值
	// (24H / 1H-low / comingSoon)。这两点都有害:
	//   - 原始值不在状态机认识的 available / unavailable / price_check_failed 里,
	//     后续所有分支都不命中,状态变化会被静默吞掉;
	//   - 这些机房根本没做过价格校验,凭空写一条状态,等于让用户之后把它加进监控时
	//     "首次检查"分支失效,当次有货既不通知也不触发 auto-order。
	// 监控范围内的机房在上面的主循环里已经写过规范化状态了,所以这里不再补写。
	sub.replaceLastStatus(lastStatus)
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// calcDuration 计算最近一次相反状态到现在的历时
func (m *Monitor) calcDuration(sub *Subscription, dc, configDisplay string, targetChangeTypes []string) string {
	// 取副本再倒着找:检查 goroutine 之外还有 HTTP 侧在读同一条订阅,
	// 而 History 正被本轮 appendHistory 追加,直接遍历真身就是竞争。
	history := sub.historySnapshot()
	var lastTS string
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		if entry.Datacenter != dc {
			continue
		}
		matched := false
		for _, t := range targetChangeTypes {
			if entry.ChangeType == t {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if configDisplay != "" && entry.Config != nil {
			if d, ok := entry.Config["display"].(string); ok && d != configDisplay {
				continue
			}
		}
		lastTS = entry.Timestamp
		if lastTS != "" {
			break
		}
	}
	if lastTS == "" {
		return ""
	}
	startDT, err := time.Parse(time.RFC3339Nano, lastTS)
	if err != nil {
		startDT, err = time.Parse(time.RFC3339, lastTS)
		if err != nil {
			return ""
		}
	}
	delta := m.nowBeijing().Sub(startDT)
	totalSec := int(delta.Seconds())
	if totalSec < 0 {
		totalSec = 0
	}
	days := totalSec / 86400
	rem := totalSec % 86400
	hours := rem / 3600
	minutes := (rem % 3600) / 60
	seconds := rem % 60
	if days > 0 {
		return fmt.Sprintf("历时 %d天%d小时%d分%d秒", days, hours, minutes, seconds)
	}
	if hours > 0 {
		return fmt.Sprintf("历时 %d小时%d分%d秒", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("历时 %d分%d秒", minutes, seconds)
	}
	return fmt.Sprintf("历时 %d秒", seconds)
}
