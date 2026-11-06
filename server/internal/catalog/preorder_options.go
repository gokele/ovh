package catalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ovh-buy/server/internal/app"
)

// ResolvePreorderOptions uses catalog product keys when available. The stock feed
// remains the model/configuration source even before a plan or price is published.
func ResolvePreorderOptions(state *app.State, accountID, planCode string, wanted []string) (stock, order []string, err error) {
	var pc planConfig
	account, ok := state.FindAccount(accountID)
	if !ok {
		return nil, nil, fmt.Errorf("下单账户不存在")
	}
	if cat, loadErr := loadSubsidiaryCatalogCached(state, SubsidiaryOfAccount(account), PreorderCheckInterval, true); loadErr == nil {
		pc = cat.plans[planCode]
	}
	selected := map[string]bool{}
	for _, option := range OrderOptions(wanted) {
		if preorderCPUOption(option) {
			continue
		}
		rawWanted := preorderProductCode(option, planCode)
		code, family, product := "", "", rawWanted
		for f, codes := range pc.addonFamilies {
			for _, candidate := range codes {
				raw := pc.addonProducts[candidate]
				if strings.EqualFold(candidate, option) || (strings.EqualFold(candidate, rawWanted+"-"+planCode) && (raw == "" || strings.EqualFold(raw, rawWanted))) {
					code, family = candidate, f
					if raw != "" {
						product = raw
					}
					break
				}
			}
			if code != "" {
				break
			}
		}
		if code == "" {
			// An exact product alias is safe; differing frequencies and extra disks are not.
			for f, codes := range pc.addonFamilies {
				for _, candidate := range codes {
					if raw := pc.addonProducts[candidate]; raw != "" && strings.EqualFold(raw, rawWanted) {
						if code != "" && code != candidate {
							return nil, nil, fmt.Errorf("预订选项 %s 对应多个销售选项，等待明确匹配", option)
						}
						code, family, product = candidate, f, raw
					}
				}
			}
		}
		if cpuFamily(family) {
			continue
		}
		if code == "" {
			code = option
		}
		order = append(order, code)
		selected[family] = true
		// Unknown hardware is preserved as a constraint; never silently drop it.
		if stockFamily(family) || (family == "" && !preorderNetworkOption(option)) {
			stock = append(stock, product)
		}
	}
	var defaults []string
	for family, mandatory := range pc.mandatoryFamilies {
		if mandatory && !selected[family] && !stockFamily(family) {
			code := pc.addonDefaults[family]
			if code != "" && containsStr(pc.addonFamilies[family], code) {
				defaults = append(defaults, code)
			}
		}
	}
	sort.Strings(defaults)
	order = append(order, defaults...)
	return OrderOptions(stock), OrderOptions(order), nil
}

func stockFamily(family string) bool {
	return family == "memory" || family == "storage" || family == "system-storage" || family == "gpu"
}

func cpuFamily(family string) bool {
	return family == "cpu" || family == "processor" || family == "processors"
}
func preorderCPUOption(option string) bool {
	option = strings.ToLower(option)
	return cpuFamily(option) || strings.HasPrefix(option, "cpu-") || strings.HasPrefix(option, "processor-")
}
func preorderNetworkOption(option string) bool {
	option = strings.ToLower(option)
	return strings.HasPrefix(option, "bandwidth-") || strings.HasPrefix(option, "traffic-") || strings.HasPrefix(option, "vrack-")
}

// Catalog SKUs can use a base suffix for a regional plan; only strip this plan's own suffix.
func preorderProductCode(option, plan string) string {
	suffixes := []string{plan}
	if last := strings.LastIndex(plan, "-"); last > 0 {
		region := plan[last+1:]
		if region != "" && strings.Trim(region, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") == "" {
			suffixes = append(suffixes, plan[:last])
		}
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(strings.ToLower(option), "-"+strings.ToLower(suffix)) {
			return option[:len(option)-len(suffix)-1]
		}
	}
	return option
}
