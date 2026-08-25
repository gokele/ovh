package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

// ErrPlanNotInCatalog:目录拉到了,但这个 planCode 不在该子公司的目录里。
// 与"目录拉取失败"必须分开:三个站点的目录互不相通(美区 143 个 plan 全部带
// -us/-ca/-eu/-sgp 后缀),拿错区的 planCode 重试多少次都不会变,得换 planCode。
var ErrPlanNotInCatalog = errors.New("该子公司的目录里没有这个 planCode")

// SubsidiaryOfAccount 统一的"这个账户该用哪个 ovhSubsidiary"口径:
// 取账户 zone(去空格大写,OVH 只认大写枚举),为空时按 endpoint 推同大区的默认子公司。
// 全项目只留这一处推导,免得各文件各写一遍、彼此漂移。
func SubsidiaryOfAccount(acc types.OVHAccount) string {
	sub := strings.ToUpper(strings.TrimSpace(acc.Zone))
	if sub == "" {
		sub = ovh.DefaultSubsidiaryForEndpoint(acc.Endpoint)
	}
	return sub
}

// ecoCatalogURL 公开 eco 目录的 URL。站点由子公司决定:EU/US/CA 三套系统各管各的子公司,
// 拿 CA 的子公司去 eu.api.ovh.com(或反过来)是 400 invalid ovhSubsidiary,不是空目录。
func ecoCatalogURL(subsidiary string) string {
	q := url.Values{}
	q.Set("ovhSubsidiary", subsidiary)
	return ovh.CatalogBaseURLForSubsidiary(subsidiary) + "/v1/order/catalog/public/eco?" + q.Encode()
}

// fetchEcoCatalogBody 拉公开目录并把 body 交给 parse 处理(不带凭据,不占账户配额)。
func fetchEcoCatalogBody(subsidiary string, parse func(io.Reader) error) error {
	httpClient := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodGet, ecoCatalogURL(subsidiary), nil)
	if err != nil {
		return err
	}
	req.Header.Set("accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("拉取 %s 目录失败: %w", subsidiary, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		// 400 基本只有一个原因:子公司不属于这个站点、或者没大写。原样带上 OVH 的回话,
		// 免得三个区之间排查时只看到一个光秃秃的 400。
		return fmt.Errorf("拉取 %s 目录失败(%s 站点): HTTP %d %s",
			subsidiary, ovh.SubsidiaryRegion(subsidiary), resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return parse(resp.Body)
}

// fetchEcoCatalogRaw 拉整份公开目录的原始结构。
// LoadServerList 需要 plan 的全部字段(invoiceName / details / pricing ...),
// 用不了 loadSubsidiaryCatalog 那份只留 region+addon 的精简缓存。
func fetchEcoCatalogRaw(subsidiary string) (map[string]interface{}, error) {
	var out map[string]interface{}
	err := fetchEcoCatalogBody(subsidiary, func(r io.Reader) error {
		return json.NewDecoder(r).Decode(&out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// region 配置的合法取值由 OVH 目录按 (子公司, planCode) 给出,不是按机房推的。
// 实测三个子公司的 /order/catalog/public/eco:
//
//	US  : 全部 143 个 plan 的 region 都只有 ["united_states"] —— 哪怕机房是 gra/fra 这些欧洲机房
//	IE/FR/CA/SG: 只有 ["canada"] / ["europe"] 两种,且 -mum/-sgp/-syd 后缀的 plan 是 ["canada"]
//
// 也就是说同一个 gra 机房,US 账户要发 united_states、EU 账户要发 europe。
// 老代码写死一张"机房→区域"表(usa/apac 这些值在任何子公司的目录里都不存在),
// 美区每一单都会卡在 region 这步,APAC 机型在欧区也一样。
//
// requiredConfiguration 只返回 label/required/type,不带 allowedValues,
// 所以唯一权威来源就是目录里的 configurations[].values。

const (
	regionCacheTTL = 2 * time.Hour
	// regionCacheFailTTL 目录拉取失败后的负缓存时长。
	// 目录单份 12MB,监控是「每订阅 5 秒一轮 × maxWorkers 并发」,一旦 OVH 侧不可用
	// (429 最常见),没有负缓存就会每一轮每一个订阅都重发一次完整请求:自己把自己
	// 打进更深的 429,429 又让目录永远拉不回来 —— 自锁。失败后静默 30 秒再试,
	// 30 秒 ≈ 6 个监控轮次,既止住风暴又不会明显推迟恢复。
	regionCacheFailTTL = 30 * time.Second
)

type planConfig struct {
	regions     []string
	datacenters []string
	// addonFamilies: family 名(memory / storage / bandwidth ...) → 该 family 下的 addon planCode。
	// 监控和快速下单要用它把 availabilities 里的 FQN 段(ram-64g-ecc-2133)映射成
	// 下单用的 addon planCode(ram-64g-ecc-2133-24sk20-us)。
	addonFamilies map[string][]string
}

type subsidiaryCatalog struct {
	plans     map[string]planConfig
	fetchedAt time.Time
}

// catalogFailure 负缓存条目:记住"上次拉取失败"这件事,免得失败期间被反复重试。
type catalogFailure struct {
	err error
	at  time.Time
}

// catalogCall 自己实现的 singleflight:同一子公司同时只允许一次在途拉取,
// 其余调用方等这一次的结果。不引 golang.org/x/sync 是为了不给这个模块加新依赖。
type catalogCall struct {
	done chan struct{}
	cat  *subsidiaryCatalog
	err  error
}

var (
	// 一把普通互斥锁同时保护三张表:命中缓存的路径只是几次 map 读,
	// 用 RWMutex 换来的并发度还不够抵消 in-flight 表必须写锁的开销。
	regionCacheMu   sync.Mutex
	regionCache     = map[string]*subsidiaryCatalog{} // key: 大写子公司
	regionCacheFail = map[string]catalogFailure{}     // 失败负缓存
	regionCacheCall = map[string]*catalogCall{}       // 在途拉取(singleflight)
)

// RegionForPlan 返回该 (账户子公司, planCode, 机房) 下应该提交的 region 值。
// 返回空串表示该 plan 不需要 region 配置(目录里没有这项)。
// 拿不到目录时返回 error,调用方可以退回 ovh.RegionForDC 的静态兜底。
func RegionForPlan(state *app.State, accountID, planCode, apiDC string) (string, error) {
	acc, _ := state.FindAccount(accountID)
	subsidiary := SubsidiaryOfAccount(acc)
	cat, err := loadSubsidiaryCatalog(state, subsidiary)
	if err != nil {
		return "", err
	}
	pc, ok := cat.plans[planCode]
	if !ok {
		return "", fmt.Errorf("%w: %s(子公司 %s / %s 站点)", ErrPlanNotInCatalog, planCode, subsidiary, ovh.SubsidiaryRegion(subsidiary))
	}
	return pickRegion(pc, apiDC), nil
}

// pickRegion 从该 plan 的合法 region 取值里挑一个。纯函数,便于测试。
// 返回空串 = 该 plan 没有 region 配置项,不要发这一项。
func pickRegion(pc planConfig, apiDC string) string {
	switch len(pc.regions) {
	case 0:
		return ""
	case 1:
		return pc.regions[0]
	}
	// 多个候选(欧区常见 canada+europe):按机房归属挑。
	// 注意 OVH 把亚太机房(sgp/syd/ynm)归在 canada 这个桶里,不是"apac"。
	want := regionBucketForDC(apiDC)
	for _, r := range pc.regions {
		if strings.EqualFold(r, want) {
			return r
		}
	}
	return pc.regions[0]
}

// parseEcoCatalog 从目录 JSON 里只挑出 region / 机房两项配置。纯函数,便于测试。
func parseEcoCatalog(r io.Reader) (map[string]planConfig, error) {
	var payload struct {
		Plans []struct {
			PlanCode       string `json:"planCode"`
			Configurations []struct {
				Name   string   `json:"name"`
				Values []string `json:"values"`
			} `json:"configurations"`
			AddonFamilies []struct {
				Name   string   `json:"name"`
				Addons []string `json:"addons"`
			} `json:"addonFamilies"`
		} `json:"plans"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, err
	}
	out := make(map[string]planConfig, len(payload.Plans))
	for _, p := range payload.Plans {
		var pc planConfig
		for _, cfg := range p.Configurations {
			switch cfg.Name {
			case "region":
				pc.regions = cfg.Values
			case "dedicated_datacenter":
				pc.datacenters = cfg.Values
			}
		}
		if len(p.AddonFamilies) > 0 {
			pc.addonFamilies = make(map[string][]string, len(p.AddonFamilies))
			for _, f := range p.AddonFamilies {
				pc.addonFamilies[strings.ToLower(f.Name)] = f.Addons
			}
		}
		out[p.PlanCode] = pc
	}
	return out, nil
}

// AddonFamiliesForPlan 取该 (账户子公司, planCode) 的 addon family → addon planCode 列表。
// 走的是 2 小时内存缓存的公开目录:以前每次可用性检查都用账户凭据现拉一份 10MB 目录,
// 监控按「每订阅 × 每 5 秒」的频率跑,几个订阅就能把 OVH 打到 429 —— 恰好在补货那一刻失灵。
func AddonFamiliesForPlan(state *app.State, accountID, planCode string) (map[string][]string, error) {
	acc, _ := state.FindAccount(accountID)
	subsidiary := SubsidiaryOfAccount(acc)
	cat, err := loadSubsidiaryCatalog(state, subsidiary)
	if err != nil {
		return nil, err
	}
	pc, ok := cat.plans[planCode]
	if !ok {
		return nil, fmt.Errorf("%w: %s(子公司 %s / %s 站点)", ErrPlanNotInCatalog, planCode, subsidiary, ovh.SubsidiaryRegion(subsidiary))
	}
	return pc.addonFamilies, nil
}

// regionBucketForDC 机房归属的 region 桶(只在 plan 有多个候选时用来消歧)。
//
// 必须先把机房码规范化:三个站点的 availabilities 都会返回长码
// (ca-east-tor-a / us-east-vin-a / ap-south-mum-a),而前端的机房芯片就是从
// availabilities 来的,这些长码会原样进到下单链路。老实现只按短码前缀判断,
// 长码一律落进 europe 桶 —— 多伦多/温特山/孟买的多候选机型会挑错 region。
func regionBucketForDC(dc string) string {
	d := normalizeDCCity(dc)
	switch d {
	case "bhs", "tor", "yyz", "sgp", "syd", "ynm", "mum":
		// 加拿大 + 亚太都落 canada 桶(目录实测如此)
		return "canada"
	case "vin", "hil":
		return "united_states"
	}
	// 没有城市段的长码(ca-east-1-a / us-west-1-a / ap-south-1-a)按大区前缀兜底
	switch {
	case strings.HasPrefix(d, "ca-"), strings.HasPrefix(d, "ap-"):
		return "canada"
	case strings.HasPrefix(d, "us-"):
		return "united_states"
	default:
		return "europe"
	}
}

// normalizeDCCity 把机房码归一成城市三字码:
// 短码原样(gra)、带编号取前三位(bhs5→bhs)、长码取城市段(ca-east-tor-a→tor)。
// 认不出来时原样返回小写串,交给调用方按大区前缀兜底。
func normalizeDCCity(dc string) string {
	d := strings.ToLower(strings.TrimSpace(dc))
	if d == "" {
		return ""
	}
	if _, ok := dcCityMap[d]; ok {
		return d
	}
	segs := strings.Split(d, "-")
	// 从后往前扫:长码首段是大区(ca-east-tor-a 的 ca),从前往后会先撞上大区码
	for i := len(segs) - 1; i >= 0; i-- {
		if _, ok := dcCityMap[segs[i]]; ok {
			return segs[i]
		}
	}
	if len(d) > 3 {
		if _, ok := dcCityMap[d[:3]]; ok {
			return d[:3]
		}
	}
	return d
}

// loadSubsidiaryCatalog 取某子公司的目录(只保留 region/机房两项配置),带 2 小时内存缓存。
// 目录是公开数据(前端浏览页也直接打这个端点),不需要凭据,所以这里走 HTTP 而不是 SDK ——
// 避免为了读一张配置表去占用账户的 API 配额。
//
// 三层防护,都是为了「目录不可用时不要把自己打死」:
//  1. 正缓存 2 小时(区域/机房配置几乎不变);
//  2. singleflight:同一子公司同时只发一次请求,不管多少个监控 worker 同时进来;
//  3. 负缓存 30 秒 + 过期正缓存兜底:拉不到时优先返回上一份(过期的)目录,
//     因为一份两小时前的区域配置远好过没有 —— 后者会让下单退回静态兜底。
func loadSubsidiaryCatalog(state *app.State, subsidiary string) (*subsidiaryCatalog, error) {
	// 缓存 key 统一大写:OVH 只认大写子公司,调用方要是传了小写,
	// 既会多拉一份缓存,请求本身也会 400。
	subsidiary = strings.ToUpper(strings.TrimSpace(subsidiary))
	if subsidiary == "" {
		return nil, fmt.Errorf("缺少 ovhSubsidiary,无法确定要拉哪个站点的目录")
	}

	regionCacheMu.Lock()
	if c, ok := regionCache[subsidiary]; ok && time.Since(c.fetchedAt) < regionCacheTTL {
		regionCacheMu.Unlock()
		return c, nil
	}
	// 刚失败过:直接复用上一次的结论,不再打 OVH
	if f, ok := regionCacheFail[subsidiary]; ok && time.Since(f.at) < regionCacheFailTTL {
		stale := regionCache[subsidiary]
		regionCacheMu.Unlock()
		if stale != nil {
			return stale, nil
		}
		return nil, f.err
	}
	// 已经有人在拉了:等他的结果,不要再发一份 12MB 的请求
	if call, ok := regionCacheCall[subsidiary]; ok {
		regionCacheMu.Unlock()
		<-call.done
		return call.cat, call.err
	}
	call := &catalogCall{done: make(chan struct{})}
	regionCacheCall[subsidiary] = call
	regionCacheMu.Unlock()

	cat, err := fetchSubsidiaryCatalog(state, subsidiary)

	regionCacheMu.Lock()
	delete(regionCacheCall, subsidiary)
	if err == nil {
		regionCache[subsidiary] = cat
		delete(regionCacheFail, subsidiary)
	} else {
		regionCacheFail[subsidiary] = catalogFailure{err: err, at: time.Now()}
		if stale := regionCache[subsidiary]; stale != nil {
			// 过期的旧目录仍然可用:机型的 region/机房/addon 配置不会在两小时里改掉,
			// 拿它继续跑,总比退回静态兜底(甚至让监控停摆)强。
			state.Logger.Warn(fmt.Sprintf("[region] 刷新 %s 目录失败(%s),继续使用 %s 前缓存的那份",
				subsidiary, err.Error(), time.Since(stale.fetchedAt).Truncate(time.Second)), "purchase")
			cat, err = stale, nil
		}
	}
	regionCacheMu.Unlock()

	call.cat, call.err = cat, err
	close(call.done)
	return cat, err
}

// fetchSubsidiaryCatalog 真正去拉一份目录并裁剪成内存里那份精简结构。
// 拆出来是为了让 loadSubsidiaryCatalog 里只剩缓存/并发控制的逻辑;
// 写成变量是为了让测试能换成假实现,用调用次数直接验证 singleflight 和负缓存
// (这层的价值全在"到底发了几次请求",用真实 HTTP 是测不出来的)。
var fetchSubsidiaryCatalog = func(state *app.State, subsidiary string) (*subsidiaryCatalog, error) {
	// 目录单份 12MB 左右,只留 region/机房/addon 三项,不把整份 JSON 留在内存里
	var plans map[string]planConfig
	if err := fetchEcoCatalogBody(subsidiary, func(r io.Reader) error {
		p, err := parseEcoCatalog(r)
		if err != nil {
			return fmt.Errorf("解析 %s 目录失败: %w", subsidiary, err)
		}
		plans = p
		return nil
	}); err != nil {
		return nil, err
	}
	parsed := &subsidiaryCatalog{plans: plans, fetchedAt: time.Now()}
	state.Logger.Info(fmt.Sprintf("[region] 已缓存 %s 目录的区域配置(%d 个 plan)", subsidiary, len(parsed.plans)), "purchase")
	return parsed, nil
}

// ResolveRegion 给下单/询价用的统一入口:先查目录,失败退回静态兜底。
// 返回 (region, 来源说明),region 为空表示不需要发 region 配置项。
func ResolveRegion(state *app.State, accountID, planCode, apiDC string) (string, string) {
	if r, err := RegionForPlan(state, accountID, planCode, apiDC); err == nil {
		return r, "catalog"
	} else {
		state.Logger.Warn(fmt.Sprintf("[region] 从目录解析 %s@%s 的区域失败(%s),退回静态兜底",
			planCode, apiDC, err.Error()), "purchase")
	}
	acc, _ := state.FindAccount(accountID)
	// 兜底也必须用归一化后的子公司:acc.Zone 原样传下去时,zone 为空的美区账户
	// (建号不填 zone 很常见)会被当成非 US 子公司,gra/fra 这些美区也在卖的机房
	// 就会算出 europe —— 而美区目录里 143 个 plan 的 region 只有 united_states,
	// 每一单都卡在 configuration。SubsidiaryOfAccount 会按 endpoint 补出 US。
	return FallbackRegion(apiDC, SubsidiaryOfAccount(acc)), "fallback"
}

// FallbackRegion 目录拉不到时的静态兜底:(机房, 子公司) → region。
// 权威解析永远是目录(RegionForPlan),这里只是网络故障时的最后一道。
//
// 在 ovh.RegionForDCInSubsidiary 之上补两件它做不到的事:
//  1. 归一化机房码 —— 三区的 availabilities 都会返回长码(ca-east-tor-a /
//     eu-west-par-a / us-east-vin-a),而前端机房芯片就是从 availabilities 来的,
//     长码会原样进下单链路;那边只按短码前缀匹配,长码一律落空串。
//  2. tor(多伦多)这类枚举里有、静态表里没有的城市,用目录实测的 region 桶补齐。
func FallbackRegion(apiDC, subsidiary string) string {
	dc := normalizeDCCity(apiDC)
	if dc == "" && !strings.EqualFold(strings.TrimSpace(subsidiary), "US") {
		// 连机房都没有就别猜:非 US 子公司有 canada/europe 两种取值,猜错等于提交一个
		// 该 plan 不接受的配置。返回空串,让调用方去问购物车的 requiredConfiguration。
		return ""
	}
	if r := ovh.RegionForDCInSubsidiary(dc, subsidiary); r != "" {
		return r
	}
	// 到这里只可能是非 US 子公司(US 子公司那条分支恒定返回 united_states)。
	// 非 US 子公司的目录里只有 canada / europe 两种取值,所以 united_states 桶
	// (vin/hil)必须继续返回空串,让调用方去问购物车的 requiredConfiguration,
	// 而不是提交一个该子公司根本没有的取值。
	if b := regionBucketForDC(apiDC); b != "united_states" {
		return b
	}
	return ""
}

// NormalizeDCCity 把 OVH 返回的机房码归一成城市三字码(gra1→gra、ca-east-tor-a→tor)。
// 导出给其它包用,免得各处再各写一份长码解析。
func NormalizeDCCity(dc string) string { return normalizeDCCity(dc) }

// DatacentersForPlan 取该 (账户子公司, planCode) 在目录里可选的机房列表(目录给的原始顺序)。
// 走的是同一份 2 小时缓存,不额外打 OVH。
func DatacentersForPlan(state *app.State, accountID, planCode string) ([]string, error) {
	acc, _ := state.FindAccount(accountID)
	subsidiary := SubsidiaryOfAccount(acc)
	cat, err := loadSubsidiaryCatalog(state, subsidiary)
	if err != nil {
		return nil, err
	}
	pc, ok := cat.plans[planCode]
	if !ok {
		return nil, fmt.Errorf("%w: %s(子公司 %s / %s 站点)", ErrPlanNotInCatalog, planCode, subsidiary, ovh.SubsidiaryRegion(subsidiary))
	}
	return append([]string(nil), pc.datacenters...), nil
}

// PreferredDatacenterForPlan 调用方没指定机房时,给这个 plan 挑一个"它确实在卖"的机房。
// 取不到目录(或该 plan 不在 eco 目录里,例如 Scale/HCI 那些机型)时返回空串,
// 由调用方退回大区默认机房 —— 不能因为挑不出来就不查。
//
// 为什么不能按大区写死一个默认机房(2026-08 实测三份公开 eco 目录):
//
//	US 143 个 plan 里含 vin 的只有 33 个 —— 42 个只卖欧洲机房、36 个只卖 bhs、32 个只卖 sgp/syd/ynm
//	EU/CA 99 个 plan 里含 gra 的 47 个、含 bhs 的 42 个,51 个只卖 sgp/syd/ynm
//
// 写死大区默认机房等于让一大半机型拿一个自己根本不卖的机房去询价(cart 直接报配置不可订购)。
func PreferredDatacenterForPlan(state *app.State, accountID, planCode string) string {
	if planCode == "" {
		return ""
	}
	dcs, err := DatacentersForPlan(state, accountID, planCode)
	if err != nil || len(dcs) == 0 {
		return ""
	}
	acc, _ := state.FindAccount(accountID)
	return pickDatacenter(dcs, regionBucketForSubsidiary(SubsidiaryOfAccount(acc)))
}

// pickDatacenter 在目录列出的机房里挑一个。纯函数,便于测试。
// 目录给的顺序不是字母序,而是 OVH 自己的主次顺序(欧洲机房在前、bhs 在后),
// 所以"第一个"本身就是个合理的缺省;但要先照顾账户所在大区 ——
// 一个加区账户对 [fra gra lon rbx sbg waw bhs] 这种 plan 应该默认 bhs 而不是 fra。
// preferredDCByBucket 各大区的主力机房。同一大区有多个可选机房时优先取它,
// 取不到再退回目录列表里第一个同区机房。
//
// 为什么要这一层:OVH 目录里的机房顺序是它自己的顺序(欧区把 fra 排在 gra 前面),
// 直接取"第一个同区机房"会让欧区的缺省询价机房从格拉沃利讷变成法兰克福 ——
// 功能上没错,但对老用户是无谓的观感变化,而 gra 在这些 plan 的机房列表里本来就有。
var preferredDCByBucket = map[string][]string{
	"europe":        {"gra", "rbx", "sbg"},
	"canada":        {"bhs"},
	"united_states": {"vin", "hil"},
}

func pickDatacenter(dcs []string, wantBucket string) string {
	if len(dcs) == 0 {
		return ""
	}
	if wantBucket != "" {
		// 先看主力机房在不在该 plan 的可选列表里
		for _, want := range preferredDCByBucket[wantBucket] {
			for _, dc := range dcs {
				if normalizeDCCity(dc) == want {
					return dc
				}
			}
		}
		// 主力机房不卖这个机型(实测欧区 99 个 plan 里有 52 个不卖 gra),
		// 退回目录里第一个同区机房
		for _, dc := range dcs {
			if regionBucketForDC(dc) == wantBucket {
				return dc
			}
		}
	}
	return dcs[0]
}

// regionBucketForSubsidiary 子公司 → 它自己那档 region 桶,只用来在多个可选机房里
// 优先挑同区的那个。子公司归属仍然取自 ovh.SubsidiaryRegion 这张唯一权威表,
// 这里只是把 EU/US/CA 翻译成目录里的 region 字符串。
func regionBucketForSubsidiary(subsidiary string) string {
	switch ovh.SubsidiaryRegion(subsidiary) {
	case "US":
		return "united_states"
	case "CA":
		return "canada"
	default:
		return "europe"
	}
}

// WarmRegionCache 启动时后台预热各账户子公司的区域配置。
// 首次解析要拉一份 10MB 的目录(实测 2-7 秒),放在抢购那一单的链路里等于白白让掉先手;
// 启动时先拉好,后续下单直接命中内存。失败不影响功能(下单时会自己重试或走静态兜底)。
func WarmRegionCache(state *app.State) {
	state.AccountsMu.RLock()
	subs := map[string]struct{}{}
	for _, a := range state.Accounts {
		subs[SubsidiaryOfAccount(a)] = struct{}{}
	}
	state.AccountsMu.RUnlock()

	for s := range subs {
		if _, err := loadSubsidiaryCatalog(state, s); err != nil {
			state.Logger.Warn(fmt.Sprintf("[region] 预热 %s 目录失败(下单时会重试): %s", s, err.Error()), "purchase")
		}
	}
}
