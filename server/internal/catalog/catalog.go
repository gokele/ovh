package catalog

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

// 返回每个配置组合的可用性 + 匹配到的 API2 options
type ConfigAvailability struct {
	Memory      string            `json:"memory"`
	Storage     string            `json:"storage"`
	Datacenters map[string]string `json:"datacenters"`
	FQN         string            `json:"fqn"`
	Options     []string          `json:"options"`
	// CatalogError 记录拉 /order/catalog/public/eco 时的 OVH 错误。
	// 目录拉不到时 Options 会退化成空数组,调用方必须能区分"目录没拉到"和
	// "这个配置确实没有可匹配 addon",否则会把 OVH 瞬断报成"该机房无可定价配置"。
	CatalogError string `json:"catalogError,omitempty"`
	// CatalogMissing:目录拉到了,但这个 planCode 不在该账户子公司的目录里。
	// 与 CatalogError(网络/OVH 故障,重试有用)必须分开:这种情况重试一万次也没用,
	// 是拿错区的 planCode 了 —— 美区目录的机型一律带 -us/-ca/-eu/-sgp 后缀
	// (US 143 个 plan 全部带后缀,欧区的裸 planCode 在美区目录里根本不存在)。
	CatalogMissing bool `json:"catalogMissing,omitempty"`
	// OptionsNote:目录和 planCode 都在,但这一条 FQN 的内存/存储在该子公司目录里
	// 没有对应 addon(实测 US 35 条、EU/CA 各 18 条)。这类配置在本子公司买不到,
	// 不是"目录挂了",调用方应该照实说而不是报错。
	OptionsNote string `json:"optionsNote,omitempty"`
}

// ErrPlanNotInRegion:该 planCode 在这个账户所属站点没有任何可用性记录。
// OVH 对"不属于本站点的 planCode"返回的是 HTTP 200 + 空数组(实测:欧区 24rise01-v1
// 查 api.us.ovhcloud.com、美区 24rise01-v1-us 查 eu.api.ovh.com,两边都是 []),
// 而真正的缺货是有记录、availability=unavailable(EU 8973 条里 6700 条如此)。
// 两者必须分开报,否则用户看到的都是"空",无从判断是拿错区还是没货。
var ErrPlanNotInRegion = errors.New("该 planCode 在当前站点没有可用性记录")

// ErrConfigNotMatched:plan 在,但指定的 options 组合在它的任何 FQN 里都不存在。
var ErrConfigNotMatched = errors.New("指定配置组合不存在")

// IsAvailableForOrder 判断可用性取值是否真的可以下单。判据统一在 ovh 包
// (app / telegram / catalog 三边都要用同一份,而 app 不能 import catalog),
// 这里保留同名函数只是为了不改动已有调用方。
func IsAvailableForOrder(availability string) bool {
	return ovh.IsAvailableForOrder(availability)
}

// CheckServerAvailabilityWithConfigs 返回每个配置组合的可用性 + 匹配到的 API2 options。
//   - accountID:决定用哪个账户的 OVH client 和 zone 拉 catalog。空 = 默认账户。
//     `/dedicated/server/datacenter/availabilities` 是全局接口,client 走哪个账户无所谓;
//     但 `/order/catalog/public/eco` 必须用对应 subsidiary 拉,否则跨子公司账户的 options 匹配会失败。
//   - monitor 检查 loop 没有"当前账户"概念,直接传 "",意味着只能保证默认账户 + 同 subsidiary 账户准确;
//     quick-order / Telegram 这种已知 account_id 的调用方应该传具体 ID。
func CheckServerAvailabilityWithConfigs(state *app.State, planCode string, accountID string) map[string]*ConfigAvailability {
	client, err := state.OVH.ClientFor(accountID)
	if err != nil {
		return map[string]*ConfigAvailability{}
	}

	// 子公司/站点先算出来:下面所有日志都要带上它,否则"查不到"这种话在三区之间
	// 完全没法定位到底是哪个站点在回空。
	acc, _ := state.FindAccount(accountID)
	subsidiary := SubsidiaryOfAccount(acc)
	region := ovh.SubsidiaryRegion(subsidiary)

	state.Logger.Info(fmt.Sprintf("[配置监控] 查询 %s 的所有配置组合(%s 站点 / 子公司 %s)...", planCode, region, subsidiary), "monitor")

	var availabilities []map[string]interface{}
	q := url.Values{}
	q.Set("planCode", planCode)
	if err := client.Get("/dedicated/server/datacenter/availabilities?"+q.Encode(), &availabilities); err != nil {
		state.Logger.Error(fmt.Sprintf("[配置监控] 获取配置可用性失败: %s", err.Error()), "monitor")
		return map[string]*ConfigAvailability{}
	}

	if len(availabilities) == 0 {
		// 空数组 ≠ 无货:OVH 对"不属于本站点的 planCode"就是回 200 + [](实测三区互查都如此),
		// 真正的缺货是有记录、availability=unavailable。这里必须把区域线索写进日志,
		// 否则跨区订阅(拿欧区 planCode 监控美区账户)会永远显示"取不到可用性"而查不出原因。
		state.Logger.Warn(fmt.Sprintf("[配置监控] %s 站点(子公司 %s)没有 %s 的任何可用性记录 —— "+
			"这不是无货,而是该 planCode 不属于这个站点(美区机型带 -us/-ca/-eu/-sgp 后缀,欧区裸 planCode 在美区查不到)",
			region, subsidiary, planCode), "monitor")
		return map[string]*ConfigAvailability{}
	}

	state.Logger.Info(fmt.Sprintf("[配置监控] OVH API 返回 %d 个配置组合", len(availabilities)), "monitor")

	// addon 匹配用的目录走 2 小时内存缓存(公开数据,不耗账户配额)。
	// 以前这里每次调用都用账户凭据现拉一份 ~10MB 的 eco 目录,而监控是
	// 「每订阅 × 每 5 秒」调一次 —— 几个订阅就能把 OVH 打到 429,
	// 结果是补货那一刻监控恰好最不可靠。
	addonFamilies, addonErr := AddonFamiliesForPlan(state, accountID, planCode)
	catalogErr := ""
	catalogMissing := false
	if addonErr != nil {
		catalogErr = addonErr.Error()
		// "目录里没有这个 planCode" 和 "目录拉取失败" 是两码事:前者是跨区拿错 planCode,
		// 重试无用,得换带本区后缀的机型;后者是 OVH/网络瞬断,重试就好。
		// 调用方(quick_order / telegram)据此给用户不同的提示。
		catalogMissing = errors.Is(addonErr, ErrPlanNotInCatalog)
		if catalogMissing {
			catalogErr = fmt.Sprintf("%s 子公司(%s 站点)的目录里没有 planCode %s —— "+
				"三个站点的目录互不相通,美区机型带 -us/-ca/-eu/-sgp 后缀,请换本区的 planCode",
				subsidiary, region, planCode)
			state.Logger.Warn("[配置监控] "+catalogErr, "monitor")
		} else {
			state.Logger.Error(fmt.Sprintf("[配置监控] 取 %s 的目录 addon 失败(子公司 %s / %s 站点): %s，本次无法匹配 API2 选项",
				planCode, subsidiary, region, catalogErr), "monitor")
		}
	}

	result := map[string]*ConfigAvailability{}
	for _, item := range availabilities {
		memory := getString(item, "memory", "N/A")
		storage := getString(item, "storage", "N/A")
		fqn := getString(item, "fqn", "")
		configKey := fqn

		datacenters := map[string]string{}
		if dcsRaw, ok := item["datacenters"].([]interface{}); ok {
			for _, dcRaw := range dcsRaw {
				dc, ok := dcRaw.(map[string]interface{})
				if !ok {
					continue
				}
				dcName := getString(dc, "datacenter", "")
				availability := getString(dc, "availability", "unknown")
				if dcName != "" {
					datacenters[dcName] = availability
				}
			}
		}

		// 匹配 API2 options
		api2Options := []string{}
		memoryStd := ""
		storageStd := ""
		// 原始段(未标准化)是第一优先的匹配依据,"N/A" 视为没有该段
		rawMemory, rawStorage := "", ""
		if memory != "N/A" {
			rawMemory = memory
			memoryStd = StandardizeConfig(memory)
		}
		if storage != "N/A" {
			rawStorage = storage
			storageStd = StandardizeConfig(storage)
		}

		state.Logger.Debug(fmt.Sprintf("[配置监控] 提取选项: memory=%s (标准化: %s), storage=%s (标准化: %s)",
			memory, memoryStd, storage, storageStd), "monitor")

		// 从缓存的 addonFamilies 里挑与本条 FQN 的内存/存储匹配的 addon planCode。
		// 匹配口径见 matchAddonsForSegment(三段式,绝不用 Contains)。
		wantedSegs, matchedSegs := 0, 0
		matchAddons := func(family, raw, std string) {
			if raw == "" && std == "" {
				return
			}
			wantedSegs++
			hits := matchAddonsForSegment(addonFamilies[family], raw, std)
			if len(hits) == 0 {
				return
			}
			matchedSegs++
			for _, addon := range hits {
				if !contains(api2Options, addon) {
					api2Options = append(api2Options, addon)
				}
			}
		}
		optionsNote := ""
		if len(addonFamilies) > 0 {
			// 原始段优先(避免标准化把机型后缀吃成粘连残渣导致错配),标准化段兜底
			matchAddons("memory", rawMemory, memoryStd)
			matchAddons("storage", rawStorage, storageStd)
			if wantedSegs > 0 && matchedSegs < wantedSegs {
				// 目录在、plan 在,就是这套内存/存储在本子公司目录里没有对应 addon。
				// 实测确实存在(US 35 条、EU/CA 各 18 条,例:24rise04-v1 的 softraid-4x6000sa),
				// 属于"这个配置在本子公司买不到",不能和"目录挂了"混为一谈。
				optionsNote = fmt.Sprintf("该配置(%s / %s)在 %s 子公司的目录里没有对应 addon，本子公司买不到这套配置",
					memory, storage, subsidiary)
				state.Logger.Warn("[配置监控] "+optionsNote, "monitor")
			}
		} else if catalogErr == "" {
			state.Logger.Warn(fmt.Sprintf("[配置监控] %s 子公司的目录里 %s 没有 addonFamilies,无法匹配选项", subsidiary, planCode), "monitor")
		}

		if len(api2Options) > 0 {
			state.Logger.Info(fmt.Sprintf("[配置监控] 成功提取 %d 个API2选项: %v", len(api2Options), api2Options), "monitor")
		}

		result[configKey] = &ConfigAvailability{
			Memory:         memory,
			Storage:        storage,
			Datacenters:    datacenters,
			FQN:            fqn,
			Options:        api2Options,
			CatalogError:   catalogErr,
			CatalogMissing: catalogMissing,
			OptionsNote:    optionsNote,
		}
		state.Logger.Info(fmt.Sprintf("[配置监控] 配置: %s + %s, 数据中心数: %d", memory, storage, len(datacenters)), "monitor")
	}

	state.Logger.Info(fmt.Sprintf("[配置监控] 成功获取 %d 个配置组合的可用性", len(result)), "monitor")
	return result
}

// matchAddonsForSegment 把 availabilities 里的一段 FQN(如 softraid-2x6000sa / ram-32g-ecc-2400)
// 映射成该子公司目录里可下单的 addon planCode。三段式,顺序不能换:
//
//  1. 标准化后完全相等 —— 欧区/加区绝大多数走这条(实测 EU/CA 各 1022 条内存走这条);
//  2. addon 标准化值以「段 + -」开头 —— 美区目录的 addon 都带 -us/-ca/-eu 机房后缀
//     (ram-32g-ecc-2400-24risegame01-ca 标准化后仍是 ram-32g-ca),实测美区 1970 条内存里
//     能走第 1 条的是 0 条,全靠这一条;
//  3. addon 标准化值以「段」开头(无分隔符)—— StandardizeConfig 抹掉机型串后会粘连
//     (ram-256g-ecc-2400-24sk60b → ram-256gb、ram-128g-on-die-...-25risel01-v1-ca →
//     ram-128g-on-diel01-ca),这类只能靠裸前缀兜底。
//
// 绝对不能退回 strings.Contains:softraid-2x6000sa 会同时命中
// hybridsoftraid-2x6000sa-2x512nvme(实测 US 92 条、EU/CA 各 119 条这样误配),
// 而 Options 是 monitor / quick_order / telegram 直接拿去下单的 —— 误配就是买错配置。
func matchAddonsForSegment(addons []string, seg, segStd string) []string {
	if len(addons) == 0 || (seg == "" && segStd == "") {
		return nil
	}
	// 第 1 档:原始码完全相等
	for _, addon := range addons {
		if addon == seg {
			return []string{addon}
		}
	}
	// 第 2 档:原始码以「段-」开头。同一 plan 里存在互为前缀的存储项
	// (softraid-2x960nvme-26sk50a-v1 与 softraid-2x960nvme-2x6000sa-26sk50a-v1),
	// 剩余部分越短越可能只是机型后缀、越长越可能多编了一段别的配置 —— 取最短的那条。
	//
	// 这一档必须放在标准化之前:StandardizeConfig 会把机型后缀吃成粘连残渣
	// (…-26sk50a-v1 → …nvmea),正确项因此丢掉分隔符、反而落到比错误项更低的档,
	// 实测就是这样把 €0 的 2x960NVMe 配成了 €24 的混合盘。
	if seg != "" {
		if best := shortestWithPrefix(addons, seg+"-"); best != "" {
			return []string{best}
		}
	}
	// 第 3 档:标准化后相等。OVH 的 FQN 段和目录 addon 的内存频率经常对不上
	// (可用性报 ram-1024g-ecc-2933、目录只卖 ram-1024g-ecc-3200),只有标准化认得出是同一档。
	if segStd != "" {
		var exact []string
		for _, addon := range addons {
			if StandardizeConfig(addon) == segStd {
				exact = append(exact, addon)
			}
		}
		if len(exact) > 0 {
			return exact
		}
		// 第 4 档:标准化后前缀,同样取剩余最短
		best, bestLen := "", -1
		for _, addon := range addons {
			as := StandardizeConfig(addon)
			if !strings.HasPrefix(as, segStd) {
				continue
			}
			if bestLen < 0 || len(as) < bestLen {
				best, bestLen = addon, len(as)
			}
		}
		if best != "" {
			return []string{best}
		}
	}
	return nil
}

// shortestWithPrefix 返回以 prefix 开头且整体最短的那个 addon(没有则空串)
func shortestWithPrefix(addons []string, prefix string) string {
	best := ""
	for _, addon := range addons {
		if !strings.HasPrefix(addon, prefix) {
			continue
		}
		if best == "" || len(addon) < len(best) {
			best = addon
		}
	}
	return best
}

// 匹配强度分档:数值只用来比大小,越大越可信。
const (
	segScoreEqual     = 4000 // 原样相等
	segScorePrefix    = 2000 // 一方是另一方的 "x-" 前缀(再加上匹配长度做细分)
	segScoreStdEqual  = 1000 // 标准化后相等(会吃掉 -ecc-2400 这种频率差异)
	segScoreStdPrefix = 500  // 标准化后前缀
	segScoreNoMatch   = -1
)

// segmentMatchScore 判断"用户传的 addon planCode"和"availabilities 里的 FQN 段"是不是同一项配置,
// 返回 -1 表示不是,否则返回匹配强度。
//
// 为什么要分档 + 取最高分,而不是"第一个匹配就 break":
//   - 同一个 plan 里存在互为前缀的存储配置(实测 US 112 对、EU/CA 各 61 对,例
//     softraid-36x22000-hdd-sas 与 softraid-36x22000-hdd-sas-2x7680nvme),先撞上短的那条
//     就会把另一套配置的库存当成用户选的那套 —— 抢到的机器和下单的配置对不上;
//   - 但标准化这一档又不能去掉:OVH 的 FQN 段和目录 addon 的内存频率经常对不上
//     (26sk10b-v1 的 FQN 段是 ram-32g-ecc-2133,目录 addon 却是 ram-32g-ecc-2400),
//     只有标准化(抹掉 -ecc-\d+)才认得出是同一档内存。
//
// 三区通用:美区的 -us/-ca/-eu 后缀落在"前缀"这一档,欧区/加区多数落在"相等/标准化相等"。
func segmentMatchScore(optionCode, fqnSeg string) int {
	opt := strings.ToLower(strings.TrimSpace(optionCode))
	seg := strings.ToLower(strings.TrimSpace(fqnSeg))
	if opt == "" || seg == "" {
		return segScoreNoMatch
	}
	if opt == seg {
		return segScoreEqual
	}
	if strings.HasPrefix(opt, seg+"-") {
		return segScorePrefix + len(seg)
	}
	if strings.HasPrefix(seg, opt+"-") {
		return segScorePrefix + len(opt)
	}
	optStd, segStd := StandardizeConfig(opt), StandardizeConfig(seg)
	if optStd != "" && optStd == segStd {
		return segScoreStdEqual
	}
	if optStd != "" && segStd != "" && (strings.HasPrefix(optStd, segStd) || strings.HasPrefix(segStd, optStd)) {
		return segScoreStdPrefix
	}
	return segScoreNoMatch
}

// fqnRelevantOption 只有内存/存储(含系统盘)这两类 addon 会出现在 FQN 里,
// bandwidth / vrack / OS / 许可证不会。把它们一起拿去匹配 FQN 会导致任何机型都匹配不上。
func fqnRelevantOption(opt string) bool {
	o := strings.ToLower(opt)
	if strings.Contains(o, "ram-") || strings.Contains(o, "memory") {
		return true
	}
	for _, kw := range []string{"raid", "disk", "nvme", "ssd", "hdd", "sas", "storage"} {
		if strings.Contains(o, kw) {
			return true
		}
	}
	return false
}

// fqnSegmentsOf 取一条 availability 记录里参与配置匹配的段。
// 用 memory/storage/systemStorage 三个字段而不是切 FQN 字符串:FQN 第一段是 planCode,
// 而美区 planCode 自带 -us/-ca 后缀,按 "." 切完还要再判断哪段是什么,反而容易错。
func fqnSegmentsOf(item map[string]interface{}) []string {
	segs := []string{}
	for _, key := range []string{"memory", "storage", "systemStorage"} {
		if v := getString(item, key, ""); v != "" {
			segs = append(segs, v)
		}
	}
	return segs
}

// CheckServerAvailability 查某机型各机房的可用性（带 options 精确匹配）。
// accountID 决定用哪个账户的 client 查:EU / US / CA 三个 endpoint 各自维护独立的
// /dedicated/server/datacenter/availabilities 库存视图,写死默认账户会让前端切账户后
// 看到的是别的大区的库存。空 = 默认账户。
func CheckServerAvailability(state *app.State, planCode string, options []string, accountID string) (map[string]string, error) {
	client, err := state.OVH.ClientFor(accountID)
	if err != nil {
		return nil, err
	}

	state.Logger.Info(fmt.Sprintf("查询 %s 的可用性...", planCode), "")

	var availabilities []map[string]interface{}
	q := url.Values{}
	q.Set("planCode", planCode)
	if err := client.Get("/dedicated/server/datacenter/availabilities?"+q.Encode(), &availabilities); err != nil {
		state.Logger.Error(fmt.Sprintf("Failed to check availability for %s: %s", planCode, err.Error()), "")
		return nil, err
	}

	state.Logger.Info(fmt.Sprintf("OVH API 返回 %d 个配置组合", len(availabilities)), "")
	if len(availabilities) == 0 {
		// 空数组是"这个 planCode 不属于本站点",不是"全线无货"。以前直接回空 map,
		// 前端/脚本看到的和"所有机房 unavailable"一模一样,跨区拿错 planCode 永远查不出来。
		acc, _ := state.FindAccount(accountID)
		sub := SubsidiaryOfAccount(acc)
		state.Logger.Warn(fmt.Sprintf("%s 站点(子公司 %s)没有 %s 的任何可用性记录", ovh.SubsidiaryRegion(sub), sub, planCode), "")
		return nil, fmt.Errorf("%w：%s 站点(子公司 %s)查不到 %s，"+
			"三个站点的机型目录互不相通(美区机型带 -us/-ca/-eu/-sgp 后缀),请确认 planCode 与账户所在区一致",
			ErrPlanNotInRegion, ovh.SubsidiaryRegion(sub), sub, planCode)
	}

	// 用户提供配置：精确匹配
	if len(options) > 0 {
		state.Logger.Info(fmt.Sprintf("查询 %s 的配置选项可用性: %v", planCode, options), "")
		// 只挑会出现在 FQN 里的 addon(内存/存储/系统盘);带宽、vrack、OS 之类不参与匹配。
		wanted := []string{}
		for _, opt := range options {
			if fqnRelevantOption(opt) {
				wanted = append(wanted, opt)
			} else {
				state.Logger.Debug(fmt.Sprintf("选项 %s 不出现在 FQN 里，跳过配置匹配", opt), "")
			}
		}

		if len(wanted) == 0 {
			// 传进来的 option 全是带宽/vrack/OS 这类不进 FQN 的项:没法用它们挑配置组合,
			// 退回"未指定 options"的默认分支,而不是报"配置不存在"(那是误报)。
			state.Logger.Warn(fmt.Sprintf("%s 的 options %v 里没有内存/存储项，无法定位配置组合，退回默认配置", planCode, options), "")
		} else {
			var matchedConfig map[string]interface{}
			bestScore := 0
			for _, item := range availabilities {
				segs := fqnSegmentsOf(item)
				itemFQN := getString(item, "fqn", "")
				total := 0
				ok := true
				for _, opt := range wanted {
					best := segScoreNoMatch
					for _, seg := range segs {
						if sc := segmentMatchScore(opt, seg); sc > best {
							best = sc
						}
					}
					if best <= segScoreNoMatch {
						ok = false
						break
					}
					total += best
				}
				if !ok {
					continue
				}
				state.Logger.Debug(fmt.Sprintf("候选配置 %s 匹配分 %d (段: %v)", itemFQN, total, segs), "")
				// 取分最高的那条:同一个 plan 里存在互为前缀的存储配置,
				// "第一个匹配就 break" 会挑到粒度更粗的那条,返回的是别的配置的库存。
				if total > bestScore {
					bestScore = total
					matchedConfig = item
				}
			}

			if matchedConfig != nil {
				state.Logger.Info(fmt.Sprintf("✅ 找到匹配配置: %s (匹配分 %d)", getString(matchedConfig, "fqn", ""), bestScore), "")
				result := map[string]string{}
				if dcsRaw, ok := matchedConfig["datacenters"].([]interface{}); ok {
					for _, dcRaw := range dcsRaw {
						dc, ok := dcRaw.(map[string]interface{})
						if !ok {
							continue
						}
						dcName := getString(dc, "datacenter", "")
						availability := getString(dc, "availability", "unknown")
						if dcName == "" {
							continue
						}
						if availability == "" || availability == "unknown" {
							result[dcName] = "unknown"
						} else if availability == "unavailable" {
							result[dcName] = "unavailable"
						} else {
							result[dcName] = availability
						}
					}
				}
				state.Logger.Info(fmt.Sprintf("配置 %s 的可用性: %v", getString(matchedConfig, "fqn", ""), result), "")
				return result, nil
			}

			// 没匹配上要报错而不是回空 map:空 map 会被前端画成"所有机房都没货",
			// 而实际情况是这套配置在本区根本不存在(常见于跨区套用别的区的 addon planCode)。
			fqns := []string{}
			for _, item := range availabilities {
				fqns = append(fqns, getString(item, "fqn", ""))
			}
			acc, _ := state.FindAccount(accountID)
			sub := SubsidiaryOfAccount(acc)
			state.Logger.Warn(fmt.Sprintf("❌ 未找到匹配的配置组合！请求: %v，该 plan 现有组合: %v", options, fqns), "")
			return nil, fmt.Errorf("%w：%s 站点(子公司 %s)的 %s 没有这套配置(%v)，现有组合 %d 种",
				ErrConfigNotMatched, ovh.SubsidiaryRegion(sub), sub, planCode, wanted, len(fqns))
		}
	}

	// 未指定 options：使用第一个
	defaultConfig := availabilities[0]
	defaultFQN := getString(defaultConfig, "fqn", "")
	state.Logger.Info(fmt.Sprintf("使用默认配置: %s", defaultFQN), "")

	result := map[string]string{}
	if dcsRaw, ok := defaultConfig["datacenters"].([]interface{}); ok {
		for _, dcRaw := range dcsRaw {
			dc, ok := dcRaw.(map[string]interface{})
			if !ok {
				continue
			}
			dcName := getString(dc, "datacenter", "")
			availability := getString(dc, "availability", "unknown")
			if dcName == "" {
				continue
			}
			if availability == "" || availability == "unknown" {
				result[dcName] = "unknown"
			} else if availability == "unavailable" {
				result[dcName] = "unavailable"
			} else {
				result[dcName] = availability
			}
		}
	}
	state.Logger.Info(fmt.Sprintf("默认配置 %s 的可用性: %v", defaultFQN, result), "")
	return result, nil
}

// dcCityMap 机房城市三字码 → (中文名, 地区)。
// 取值范围以 dedicated.AvailabilityDatacenterEnum 为准(EU/US/CA 三区 schema 完全一致),
// 实测三个站点的 availabilities 都会同时返回短码(gra/bhs/ynm)和长码
// (eu-west-par-a / ca-east-tor-a / us-east-vin-a)。
// 以前只有一张短码表 + code[:3] 截断,长码全被截成 "eu-"/"ca-"/"us-" 落进"未知",
// 巴黎(eu-west-par-*)和多伦多(ca-east-tor-a)这两个三区都在返回的机房永远显示未知。
var dcCityMap = map[string][2]string{
	// 法国
	"gra": {"格拉沃利讷", "法国"},
	"rbx": {"鲁贝", "法国"},
	"sbg": {"斯特拉斯堡", "法国"},
	"par": {"巴黎", "法国"},
	// 德国:OVH 的德国机房在林堡(LIM1/LIM2),对外常按最近的枢纽法兰克福宣传。
	// 老表把 lim 写成"利马索尔/塞浦路斯"是错的 —— OVH 没有塞浦路斯机房。
	"lim": {"林堡", "德国"},
	"fra": {"法兰克福", "德国"},
	// 英国
	"eri": {"埃里斯", "英国"},
	"lon": {"伦敦", "英国"},
	// 其它欧洲
	"waw": {"华沙", "波兰"},
	"mil": {"米兰", "意大利"},
	// 加拿大
	"bhs": {"博阿尔诺", "加拿大"},
	"tor": {"多伦多", "加拿大"},
	"yyz": {"多伦多", "加拿大"},
	// 美国(只有美区站点会返回这两个)
	"vin": {"弗吉尼亚", "美国东部"},
	"hil": {"俄勒冈", "美国西部"},
	// 亚太(欧区/加区目录里都有这些机型,美区站点也会返回)
	"sgp": {"新加坡", "新加坡"},
	"syd": {"悉尼", "澳大利亚"},
	"ynm": {"孟买", "印度"}, // OVH API 用 ynm,前端显示码是 mum
	"mum": {"孟买", "印度"},
}

// dcCountryMap 枚举里还有国家/大区级的取值(au/ca/de/fr/gb/in/pl/sg/us/eu/default),
// 它们不对应具体城市,只能按国家显示。
var dcCountryMap = map[string][2]string{
	"fr":      {"法国", "法国"},
	"de":      {"德国", "德国"},
	"gb":      {"英国", "英国"},
	"pl":      {"波兰", "波兰"},
	"ca":      {"加拿大", "加拿大"},
	"us":      {"美国", "美国"},
	"sg":      {"新加坡", "新加坡"},
	"au":      {"澳大利亚", "澳大利亚"},
	"in":      {"印度", "印度"},
	"eu":      {"欧洲", "欧洲"},
	"default": {"默认机房", "默认"},
}

// dcRegionMap 没有城市段的长码(eu-west-1-a / ca-east-1-a / ap-south-1-a),
// 只能按大区显示,不硬猜具体城市。
var dcRegionMap = map[string][2]string{
	"eu-west-1":  {"欧洲西部", "欧洲"},
	"eu-central": {"欧洲中部", "欧洲"},
	"eu-south":   {"欧洲南部", "欧洲"},
	"ca-east-1":  {"加拿大东部", "加拿大"},
	"us-east-1":  {"美国东部", "美国东部"},
	"us-west-1":  {"美国西部", "美国西部"},
	"ap-south-1": {"亚太南部", "亚太"},
	"ap-east-1":  {"亚太东部", "亚太"},
}

// lookupDCName 把 OVH 返回的机房码解析成 (中文名, 地区)。
// 三区通用,兼容三种写法:短码 gra、带编号 gra1/sgp02、长码 eu-west-par-a。
func lookupDCName(code string) (string, string, bool) {
	c := strings.ToLower(strings.TrimSpace(code))
	if c == "" {
		return "", "", false
	}
	if v, ok := dcCityMap[c]; ok {
		return v[0], v[1], true
	}
	if v, ok := dcCountryMap[c]; ok {
		return v[0], v[1], true
	}
	segs := strings.Split(c, "-")
	// 从后往前扫城市段:长码的首段是大区(ca-east-tor-a 的 "ca"),
	// 从前往后扫会把多伦多认成"加拿大(国家级)",丢掉城市信息。
	for i := len(segs) - 1; i >= 0; i-- {
		if v, ok := dcCityMap[segs[i]]; ok {
			return v[0], v[1], true
		}
	}
	// 没有城市段的长码:去掉结尾的可用区字母再查大区表(eu-west-1-a → eu-west-1)
	if len(segs) > 1 && len(segs[len(segs)-1]) == 1 {
		if v, ok := dcRegionMap[strings.Join(segs[:len(segs)-1], "-")]; ok {
			return v[0], v[1], true
		}
	}
	if len(segs) >= 2 {
		if v, ok := dcRegionMap[strings.Join(segs[:2], "-")]; ok {
			return v[0], v[1], true
		}
	}
	for i := len(segs) - 1; i >= 0; i-- {
		if v, ok := dcCountryMap[segs[i]]; ok {
			return v[0], v[1], true
		}
	}
	// 带编号的短码:gra1 / bhs5 / sgp02
	if len(c) > 3 {
		if v, ok := dcCityMap[c[:3]]; ok {
			return v[0], v[1], true
		}
	}
	return "", "", false
}

// 多账户:用 accountID 对应账户的 zone 作 ovhSubsidiary(空 = 默认账户),不读全局 state.Config
// (新建账户不会写 kv['config'])。endpoint 与 ovhSubsidiary 一起决定目录范围,
// EU 与 US 的 nichandle.OvhSubsidiaryEnum 取值集合本身就不同,必须跟着账户走。
//
// 第二个返回值是"可用性预拉失败的 plan 数":这些 plan 的机房列会是空的,
// 调用方据此决定要不要用这份结果覆盖缓存。
func LoadServerList(state *app.State, accountID string) ([]types.ServerPlan, int) {
	client, err := state.OVH.ClientFor(accountID)
	if err != nil {
		state.Logger.Error("Failed to load server list: "+err.Error(), "")
		return nil, 0
	}
	acc, _ := state.FindAccount(accountID)
	// 子公司必须大写:三个站点都用 nichandle.OvhSubsidiaryEnum 校验,
	// 传小写 "ie" 直接 400 invalid ovhSubsidiary(EU/US/CA 实测一致),整份机型列表就空了。
	subsidiary := SubsidiaryOfAccount(acc)
	subRegion := ovh.SubsidiaryRegion(subsidiary)
	epRegion := ovh.EndpointRegion(acc.Endpoint)
	if ovh.KnownSubsidiary(subsidiary) && subRegion != epRegion {
		// 子公司与 endpoint 不在同一个站点:目录来自 A 站点、库存来自 B 站点,
		// 拼出来的机型列表每台都会是"零机房"。这种账户配置本身就是错的,直接说清楚,
		// 别让用户对着一份看似正常、实则全空的列表猜。
		state.Logger.Error(fmt.Sprintf("账户配置有误:子公司 %s 属于 %s 站点，而 endpoint %s 属于 %s 站点，"+
			"两者必须一致(EU/US/CA 是三套互不相通的系统)", subsidiary, subRegion, acc.Endpoint, epRegion), "")
		return nil, 0
	}

	// 目录站点由子公司决定(ovh.CatalogBaseURLForSubsidiary),不是由账户 endpoint 决定 ——
	// 这是同一件事的两种说法,但只有前者在账户 zone 为空、按 endpoint 兜底时也成立。
	// 走公开 URL 而不是账户凭据:这是一份 ~10MB 的公开数据,没必要占账户的 API 配额/限流额度。
	catalogResp, err := fetchEcoCatalogRaw(subsidiary)
	if err != nil {
		// 公开站点拉不到(本机出网被限、OVH 瞬断)时,退回账户凭据再拉一次,行为与老版本一致。
		state.Logger.Warn(fmt.Sprintf("公开目录拉取失败(%s/%s): %s，改用账户凭据重试", subRegion, subsidiary, err.Error()), "")
		var fallback map[string]interface{}
		cq := url.Values{}
		cq.Set("ovhSubsidiary", subsidiary)
		if err2 := client.Get("/order/catalog/public/eco?"+cq.Encode(), &fallback); err2 != nil {
			state.Logger.Error("Failed to load server list: "+err2.Error(), "")
			return nil, 0
		}
		catalogResp = fallback
	}

	plans, _ := catalogResp["plans"].([]interface{})
	result := []types.ServerPlan{}

	// 并发预拉所有 plan 的 availabilities（这是循环里唯一的网络 IO）
	// 96 个 plan × 200ms 串行 = 20 秒；改 15 并发 ≈ 1.5 秒
	type availEntry struct {
		availabilities []map[string]interface{}
	}
	availByPlan := make(map[string]*availEntry, len(plans))
	var availMu sync.Mutex
	planCodes := make([]string, 0, len(plans))
	for _, planRaw := range plans {
		if p, ok := planRaw.(map[string]interface{}); ok {
			if pc := getString(p, "planCode", ""); pc != "" {
				planCodes = append(planCodes, pc)
			}
		}
	}
	// 并发从 15 降到 8:96 个 plan 同时打同一 endpoint 最容易吃 OVH 429,
	// 一旦限流就是大批 plan 同时静默变"零机房"。失败再串行重试一次兜底。
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	availFailed := 0
	for _, pc := range planCodes {
		wg.Add(1)
		sem <- struct{}{}
		go func(planCode string) {
			defer wg.Done()
			defer func() { <-sem }()
			var avs []map[string]interface{}
			q := url.Values{}
			q.Set("planCode", planCode)
			err := client.Get("/dedicated/server/datacenter/availabilities?"+q.Encode(), &avs)
			if err != nil {
				// 限流/瞬断退避重试一次,仍失败才计入失败数
				time.Sleep(500 * time.Millisecond)
				err = client.Get("/dedicated/server/datacenter/availabilities?"+q.Encode(), &avs)
			}
			availMu.Lock()
			if err != nil {
				availFailed++
				state.Logger.Warn(fmt.Sprintf("预拉 %s 的可用性失败: %s（该机型的机房列表将为空）", planCode, err.Error()), "")
			}
			availByPlan[planCode] = &availEntry{availabilities: avs}
			availMu.Unlock()
		}(pc)
	}
	wg.Wait()
	if availFailed > 0 {
		state.Logger.Error(fmt.Sprintf("已并发预拉 %d 个 plan 的可用性，其中 %d 个失败，这份结果的机房数据不完整",
			len(planCodes), availFailed), "")
	} else {
		state.Logger.Info(fmt.Sprintf("已并发预拉 %d 个 plan 的可用性", len(planCodes)), "")
	}

	for _, planRaw := range plans {
		plan, ok := planRaw.(map[string]interface{})
		if !ok {
			continue
		}
		planCode := getString(plan, "planCode", "")
		if planCode == "" {
			continue
		}

		// 从预拉结果取（保持串行解析以确保 1:1）
		datacenters := []types.Datacenter{}
		var availabilities []map[string]interface{}
		if entry, ok := availByPlan[planCode]; ok {
			availabilities = entry.availabilities
		}
		for _, item := range availabilities {
			if dcsRaw, ok := item["datacenters"].([]interface{}); ok {
				for _, dcRaw := range dcsRaw {
					dc, ok := dcRaw.(map[string]interface{})
					if !ok {
						continue
					}
					datacenters = append(datacenters, types.Datacenter{
						Datacenter:   getString(dc, "datacenter", ""),
						Availability: getString(dc, "availability", "unknown"),
					})
				}
			}
		}
		// 填充中文名/region
		for i := range datacenters {
			if name, area, ok := lookupDCName(datacenters[i].Datacenter); ok {
				datacenters[i].DCName = name
				datacenters[i].Region = area
			} else {
				datacenters[i].DCName = datacenters[i].Datacenter
				if datacenters[i].DCName == "" {
					datacenters[i].DCName = "未知"
				}
				datacenters[i].Region = "未知"
			}
		}

		serverInfo := types.ServerPlan{
			PlanCode:         planCode,
			Name:             getString(plan, "invoiceName", ""),
			Description:      getString(plan, "description", ""),
			CPU:              "N/A",
			Memory:           "N/A",
			Storage:          "N/A",
			Bandwidth:        "N/A",
			VrackBandwidth:   "N/A",
			Datacenters:      datacenters,
			DefaultOptions:   []types.ServerOption{},
			AvailableOptions: []types.ServerOption{},
		}

		// 特殊系列：SYSLE / SK
		lcPlan := strings.ToLower(planCode)
		if strings.Contains(lcPlan, "sysle") {
			state.Logger.Info(fmt.Sprintf("检测到SYSLE系列服务器: %s", planCode), "")
			switch {
			case strings.Contains(planCode, "011"):
				serverInfo.CPU = "SYSLE 011系列 (入门级服务器CPU)"
			case strings.Contains(planCode, "021"):
				serverInfo.CPU = "SYSLE 021系列 (中端服务器CPU)"
			case strings.Contains(planCode, "031"):
				serverInfo.CPU = "SYSLE 031系列 (高端服务器CPU)"
			default:
				serverInfo.CPU = "SYSLE系列CPU"
			}
			extractCPUFromNames(plan, &serverInfo, state)
		} else if strings.Contains(lcPlan, "sk") {
			state.Logger.Info(fmt.Sprintf("检测到SK系列服务器: %s", planCode), "")
			displayName := getString(plan, "displayName", "")
			invoiceName := getString(plan, "invoiceName", "")
			description := getString(plan, "description", "")
			foundCPU := false
			for _, name := range []string{displayName, invoiceName, description} {
				if name == "" {
					continue
				}
				if strings.Contains(name, "|") {
					parts := strings.SplitN(name, "|", 2)
					if len(parts) > 1 {
						cpuPart := strings.TrimSpace(parts[1])
						lc := strings.ToLower(cpuPart)
						if strings.Contains(lc, "intel") || strings.Contains(lc, "amd") || strings.Contains(lc, "xeon") || strings.Contains(lc, "i7") {
							serverInfo.CPU = cpuPart
							state.Logger.Info(fmt.Sprintf("从名称中提取CPU型号: %s 给 %s", cpuPart, planCode), "")
							foundCPU = true
						}
					}
				}
				if foundCPU {
					break
				}
			}
			if !foundCPU {
				serverInfo.CPU = "SK系列专用CPU"
			}
		}

		if serverInfo.CPU == "N/A" {
			state.Logger.Info(fmt.Sprintf("服务器 %s 无法从API提取CPU信息，尝试从名称提取", planCode), "")
			extractCPUFromNames(plan, &serverInfo, state)
			if serverInfo.CPU == "N/A" {
				switch {
				case strings.Contains(lcPlan, "sysle"):
					serverInfo.CPU = "SYSLE系列专用CPU"
				case strings.Contains(lcPlan, "rise"):
					serverInfo.CPU = "RISE系列专用CPU"
				case strings.Contains(lcPlan, "game"):
					serverInfo.CPU = "GAME系列专用CPU"
				default:
					serverInfo.CPU = "专用服务器CPU"
				}
			}
		}

		if serverInfo.Name == "" {
			serverInfo.Name = getString(plan, "displayName", "")
		}
		if serverInfo.Description == "" {
			serverInfo.Description = getString(plan, "displayName", "")
		}

		// 从 addonFamilies 提取硬件 + 选项
		if afRaw, ok := plan["addonFamilies"].([]interface{}); ok {
			tempAvailable := []types.ServerOption{}
			for _, familyRaw := range afRaw {
				family, ok := familyRaw.(map[string]interface{})
				if !ok {
					continue
				}
				familyName := strings.ToLower(getString(family, "name", ""))
				defaultAddon := getString(family, "default", "")
				addons, _ := family["addons"].([]interface{})

				for _, addonRaw := range addons {
					addonCode, ok := addonRaw.(string)
					if !ok {
						continue
					}
					isDefault := addonCode == defaultAddon
					lcAddon := strings.ToLower(addonCode)
					// 过滤许可证
					if strings.Contains(lcAddon, "windows-server") ||
						strings.Contains(lcAddon, "sql-server") ||
						strings.Contains(lcAddon, "cpanel-license") ||
						strings.Contains(lcAddon, "plesk-") ||
						strings.Contains(lcAddon, "-license-") ||
						strings.HasPrefix(lcAddon, "os-") ||
						strings.Contains(lcAddon, "control-panel") ||
						strings.Contains(lcAddon, "panel") {
						continue
					}
					tempAvailable = append(tempAvailable, types.ServerOption{
						Label:     addonCode,
						Value:     addonCode,
						Family:    familyName,
						IsDefault: isDefault,
					})
					if isDefault {
						serverInfo.DefaultOptions = append(serverInfo.DefaultOptions, types.ServerOption{
							Label: addonCode, Value: addonCode,
						})
					}
				}

				// 硬件信息抽取
				if defaultAddon != "" {
					switch {
					case (strings.Contains(familyName, "cpu") || strings.Contains(familyName, "processor")) && serverInfo.CPU == "N/A":
						serverInfo.CPU = defaultAddon
					case (strings.Contains(familyName, "memory") || strings.Contains(familyName, "ram")) && serverInfo.Memory == "N/A":
						if m := regexp.MustCompile(`(?i)ram-(\d+)g`).FindStringSubmatch(defaultAddon); m != nil {
							serverInfo.Memory = m[1] + " GB"
						} else {
							serverInfo.Memory = defaultAddon
						}
					case (strings.Contains(familyName, "storage") || strings.Contains(familyName, "disk") || strings.Contains(familyName, "drive") ||
						strings.Contains(familyName, "ssd") || strings.Contains(familyName, "hdd")) && serverInfo.Storage == "N/A":
						hybridRe := regexp.MustCompile(`(?i)hybridsoftraid-(\d+)x(\d+)(sa|ssd|hdd)-(\d+)x(\d+)(nvme|ssd|hdd)`)
						if m := hybridRe.FindStringSubmatch(defaultAddon); m != nil {
							serverInfo.Storage = fmt.Sprintf("混合RAID %sx %sGB %s + %sx %sGB %s",
								m[1], m[2], strings.ToUpper(m[3]), m[4], m[5], strings.ToUpper(m[6]))
						} else {
							storRe := regexp.MustCompile(`(?i)(raid|softraid)-(\d+)x(\d+)(ssd|hdd|nvme|sa)`)
							if m := storRe.FindStringSubmatch(defaultAddon); m != nil {
								serverInfo.Storage = fmt.Sprintf("%s %sx %sGB %s",
									strings.ToUpper(m[1]), m[2], m[3], strings.ToUpper(m[4]))
							} else {
								serverInfo.Storage = defaultAddon
							}
						}
					case (strings.Contains(familyName, "bandwidth") || strings.Contains(familyName, "traffic") || strings.Contains(familyName, "network")) && serverInfo.Bandwidth == "N/A":
						serverInfo.Bandwidth = parseBandwidthValue(defaultAddon, &serverInfo)
					}
				}
			}
			if len(tempAvailable) > 0 {
				serverInfo.AvailableOptions = tempAvailable
			}
		}

		// 解析方法 2: 从 plan.details.properties 提取
		if details, ok := plan["details"].(map[string]interface{}); ok {
			if propsRaw, ok := details["properties"].([]interface{}); ok {
				for _, pRaw := range propsRaw {
					prop, ok := pRaw.(map[string]interface{})
					if !ok {
						continue
					}
					propName := strings.ToLower(getString(prop, "name", ""))
					value := getString(prop, "value", "N/A")
					if value == "" || value == "N/A" {
						continue
					}
					switch {
					case (strings.Contains(propName, "cpu") || strings.Contains(propName, "processor")) && serverInfo.CPU == "N/A":
						serverInfo.CPU = value
					case (strings.Contains(propName, "memory") || strings.Contains(propName, "ram")) && serverInfo.Memory == "N/A":
						serverInfo.Memory = value
					case (strings.Contains(propName, "storage") || strings.Contains(propName, "disk") || strings.Contains(propName, "hdd") || strings.Contains(propName, "ssd")) && serverInfo.Storage == "N/A":
						serverInfo.Storage = value
					case strings.Contains(propName, "bandwidth"):
						if strings.Contains(propName, "vrack") || strings.Contains(propName, "private") || strings.Contains(propName, "internal") {
							if serverInfo.VrackBandwidth == "N/A" {
								serverInfo.VrackBandwidth = value
							}
						} else if serverInfo.Bandwidth == "N/A" {
							serverInfo.Bandwidth = value
						}
					}
				}
			}
		}

		// 解析方法 3: 从 plan.product.configurations 提取
		if product, ok := plan["product"].(map[string]interface{}); ok {
			if cfgs, ok := product["configurations"].([]interface{}); ok {
				for _, cRaw := range cfgs {
					cfg, ok := cRaw.(map[string]interface{})
					if !ok {
						continue
					}
					cfgName := strings.ToLower(getString(cfg, "name", ""))
					value := getString(cfg, "value", "")
					if value == "" {
						continue
					}
					switch {
					case (strings.Contains(cfgName, "cpu") || strings.Contains(cfgName, "processor")) && serverInfo.CPU == "N/A":
						serverInfo.CPU = value
					case (strings.Contains(cfgName, "memory") || strings.Contains(cfgName, "ram")) && serverInfo.Memory == "N/A":
						serverInfo.Memory = value
					case (strings.Contains(cfgName, "storage") || strings.Contains(cfgName, "disk") || strings.Contains(cfgName, "hdd") || strings.Contains(cfgName, "ssd")) && serverInfo.Storage == "N/A":
						serverInfo.Storage = value
					case strings.Contains(cfgName, "bandwidth") && serverInfo.Bandwidth == "N/A":
						serverInfo.Bandwidth = value
					}
				}
			}
		}

		// 解析方法 4: 从 plan.description 逗号分割解析
		if desc := getString(plan, "description", ""); desc != "" {
			for _, part := range strings.Split(desc, ",") {
				part = strings.ToLower(strings.TrimSpace(part))
				if part == "" {
					continue
				}
				hasCPU := strings.Contains(part, "cpu") || strings.Contains(part, "core") ||
					strings.Contains(part, "i7") || strings.Contains(part, "i9") ||
					strings.Contains(part, "xeon") || strings.Contains(part, "epyc") || strings.Contains(part, "ryzen")
				if serverInfo.CPU == "N/A" && hasCPU {
					serverInfo.CPU = part
				}
				if serverInfo.Memory == "N/A" && (strings.Contains(part, "ram") || strings.Contains(part, "gb") || strings.Contains(part, "memory")) {
					serverInfo.Memory = part
				}
				if serverInfo.Storage == "N/A" && (strings.Contains(part, "hdd") || strings.Contains(part, "ssd") || strings.Contains(part, "nvme") || strings.Contains(part, "storage") || strings.Contains(part, "disk")) {
					serverInfo.Storage = part
				}
				if serverInfo.Bandwidth == "N/A" && strings.Contains(part, "bandwidth") {
					serverInfo.Bandwidth = part
				}
			}
		}

		// 解析方法 5: 从 plan.pricing.configurations 提取
		if pricing, ok := plan["pricing"].(map[string]interface{}); ok {
			if cfgs, ok := pricing["configurations"].([]interface{}); ok {
				for _, cRaw := range cfgs {
					cfg, ok := cRaw.(map[string]interface{})
					if !ok {
						continue
					}
					cfgName := strings.ToLower(getString(cfg, "name", ""))
					value := getString(cfg, "value", "")
					if value == "" {
						continue
					}
					switch {
					case strings.Contains(cfgName, "processor") && serverInfo.CPU == "N/A":
						serverInfo.CPU = value
					case strings.Contains(cfgName, "memory") && serverInfo.Memory == "N/A":
						serverInfo.Memory = value
					case strings.Contains(cfgName, "storage") && serverInfo.Storage == "N/A":
						serverInfo.Storage = value
					}
				}
			}
		}

		// 从名称提取内存/存储（次要）
		if serverInfo.Memory == "N/A" {
			fullText := serverInfo.Name + " " + serverInfo.Description
			patterns := []string{
				`(\d+)\s*GB\s*RAM`,
				`RAM\s*(\d+)\s*GB`,
				`(\d+)\s*G\s*RAM`,
				`RAM\s*(\d+)\s*G`,
				`(\d+)\s*GB`,
			}
			for _, p := range patterns {
				if m := regexp.MustCompile(`(?i)` + p).FindStringSubmatch(fullText); m != nil {
					serverInfo.Memory = m[1] + " GB"
					break
				}
			}
		}
		if serverInfo.Storage == "N/A" {
			fullText := serverInfo.Name + " " + serverInfo.Description
			patterns := []string{
				`(\d+)\s*[xX]\s*(\d+)\s*GB\s*(SSD|HDD|NVMe)`,
				`(\d+)\s*TB\s*(SSD|HDD|NVMe)`,
				`(\d+)\s*(SSD|HDD|NVMe)`,
			}
			for _, p := range patterns {
				if m := regexp.MustCompile(`(?i)` + p).FindStringSubmatch(fullText); m != nil {
					if len(m) >= 4 && m[3] != "" {
						serverInfo.Storage = fmt.Sprintf("%sx %sGB %s", m[1], m[2], strings.ToUpper(m[3]))
					} else if len(m) >= 3 {
						serverInfo.Storage = fmt.Sprintf("%s %s", m[1], strings.ToUpper(m[2]))
					}
					break
				}
			}
		}

		result = append(result, serverInfo)
	}

	return result, availFailed
}

// parseBandwidthValue 解析 OVH 目录里的带宽字段
// 支持 6 种格式：traffic-Xtb-Y / traffic-Xtb / bandwidth-N（含 Gbps 转换）/ unlimited / guarantee / vrack
func parseBandwidthValue(defaultValue string, sv *types.ServerPlan) string {
	lc := strings.ToLower(defaultValue)

	// 1) traffic-Xtb-Y: X 流量 + Y Mbps
	if m := regexp.MustCompile(`(?i)traffic-(\d+)(tb|gb|mb)-(\d+)`).FindStringSubmatch(defaultValue); m != nil {
		return fmt.Sprintf("%s Mbps / %s %s流量", m[3], m[1], strings.ToUpper(m[2]))
	}
	// 2) traffic-X(tb|gb|mb)$: 仅流量限制
	if m := regexp.MustCompile(`(?i)traffic-(\d+)(tb|gb|mb)$`).FindStringSubmatch(defaultValue); m != nil {
		return fmt.Sprintf("%s %s流量", m[1], strings.ToUpper(m[2]))
	}
	// 3) bandwidth-N: 仅带宽 N Mbps，≥1000 自动转 Gbps
	if m := regexp.MustCompile(`(?i)bandwidth-(\d+)`).FindStringSubmatch(defaultValue); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			if n >= 1000 {
				gbps := float64(n) / 1000.0
				s := strconv.FormatFloat(gbps, 'f', 1, 64)
				s = strings.TrimSuffix(s, ".0")
				return s + " Gbps"
			}
			return strconv.Itoa(n) + " Mbps"
		}
		return m[1] + " Mbps"
	}
	// 4) unlimited 流量（含数字时 → "N Mbps / 无限流量"）
	if strings.Contains(lc, "traffic-unlimited") || strings.Contains(lc, "unlimited") {
		if m := regexp.MustCompile(`(\d+)`).FindStringSubmatch(defaultValue); m != nil {
			return m[1] + " Mbps / 无限流量"
		}
		return "无限流量"
	}
	// 5) guarantee / guaranteed: 保证带宽
	if strings.Contains(lc, "guarantee") || strings.Contains(lc, "guaranteed") {
		if m := regexp.MustCompile(`(\d+)`).FindStringSubmatch(defaultValue); m != nil {
			return m[1] + " Mbps (保证带宽)"
		}
		return "保证带宽"
	}
	// 6) vrack-bandwidth-X: 内部网络带宽（写到 VrackBandwidth 字段）
	if strings.Contains(lc, "vrack") {
		if m := regexp.MustCompile(`(?i)vrack-bandwidth-(\d+)`).FindStringSubmatch(defaultValue); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= 1000 {
				gbps := float64(n) / 1000.0
				s := strconv.FormatFloat(gbps, 'f', 1, 64)
				s = strings.TrimSuffix(s, ".0")
				sv.VrackBandwidth = s + " Gbps"
			} else {
				sv.VrackBandwidth = m[1] + " Mbps"
			}
		}
		return defaultValue
	}
	return defaultValue
}

func extractCPUFromNames(plan map[string]interface{}, info *types.ServerPlan, state *app.State) {
	displayName := getString(plan, "displayName", "")
	invoiceName := getString(plan, "invoiceName", "")
	description := getString(plan, "description", "")
	cpuKeywords := []string{"i7-", "i9-", "i5-", "xeon", "epyc", "ryzen"}
	for _, name := range []string{displayName, invoiceName, description} {
		if name == "" {
			continue
		}
		lcName := strings.ToLower(name)
		for _, kw := range cpuKeywords {
			if strings.Contains(lcName, kw) {
				pos := strings.Index(lcName, kw)
				end := pos + 30
				if end > len(name) {
					end = len(name)
				}
				cpuInfo := strings.TrimSpace(strings.Split(name[pos:end], ",")[0])
				if cpuInfo != "" {
					info.CPU = cpuInfo
					state.Logger.Info(fmt.Sprintf("从关键词中提取CPU型号: %s 给 %s", cpuInfo, info.PlanCode), "")
					return
				}
			}
		}
	}
}

func getString(m map[string]interface{}, key, fallback string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return fallback
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// PassthroughAvailability 用 SDK 直接传请求（用于 sniper 监控）
func PassthroughAvailability(client *ovhsdk.Client, planCode string) ([]map[string]interface{}, error) {
	var out []map[string]interface{}
	q := url.Values{}
	q.Set("planCode", planCode)
	err := client.Get("/dedicated/server/datacenter/availabilities?"+q.Encode(), &out)
	return out, err
}
