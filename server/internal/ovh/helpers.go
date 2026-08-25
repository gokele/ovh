package ovh

import "regexp"

import "strings"

// ConvertDisplayDCToAPIDC 将前端显示的数据中心代码转换为 OVH API 代码
func ConvertDisplayDCToAPIDC(datacenter string) string {
	if datacenter == "" {
		return "gra"
	}
	dcMap := map[string]string{
		"mum": "ynm", // 孟买：前端 mum，OVH API 用 ynm
	}
	lower := strings.ToLower(datacenter)
	if v, ok := dcMap[lower]; ok {
		return v
	}
	return lower
}

// RegionForDCInSubsidiary 静态兜底:目录拉不到时用它猜 region。
//
// region 的合法取值由 (子公司, planCode) 决定,不是按机房推的 —— 权威解析在
// catalog.ResolveRegion,这里只是网络故障时的最后一道兜底,取值必须与目录里
// 真实出现过的字符串完全一致:
//
//	US 子公司        → united_states(该子公司下所有机房都是它,包括欧洲机房)
//	其它子公司       → canada / europe 两种;
//	                   亚太机房(sgp/syd/ynm)在目录里归的是 canada,不是"apac"
//
// 老实现返回过 "usa" 和 "apac",这两个字符串在任何子公司的目录里都不存在,
// 结果是美区每一单、以及欧区的亚太机型,都会卡在 region 配置这一步。
func RegionForDCInSubsidiary(dc, subsidiary string) string {
	if strings.EqualFold(strings.TrimSpace(subsidiary), "US") {
		return "united_states"
	}
	d := strings.ToLower(dc)
	canada := []string{"bhs", "yyz", "sgp", "syd", "ynm", "mum"}
	for _, p := range canada {
		if strings.HasPrefix(d, p) {
			return "canada"
		}
	}
	eu := []string{"gra", "rbx", "sbg", "eri", "lim", "waw", "par", "fra", "lon", "eu-west", "eu-central", "eu-south"}
	for _, p := range eu {
		if strings.HasPrefix(d, p) {
			return "europe"
		}
	}
	if strings.HasPrefix(d, "vin") || strings.HasPrefix(d, "hil") {
		// 美国机房但账户不在 US 子公司:目录里不会有这种组合,给空让调用方跳过 region
		return ""
	}
	return ""
}

// OVH 的三个站点是彼此独立的系统:目录、价格、库存、购物车、账户全都不互通,
// 每个子公司只属于其中一个站点,查错站点直接 400/404。
//
// 归属取自各站点自己的 nichandle.OvhSubsidiaryEnum(/1.0/me.json),并逐个用
// /v1/order/catalog/public/eco?ovhSubsidiary=X 实测过:
//
//	EU(eu.api.ovh.com)      : CZ DE ES EU FI FR GB IE IT LT MA NL PL PT SN TN
//	CA(ca.api.ovh.com)      : ASIA AU CA IN QC SG WE WS
//	US(api.us.ovhcloud.com) : US
//
// 特别注意两组容易搞反的:
//   - MA(摩洛哥,MAD) / TN(突尼斯,TND) / SN(塞内加尔,XOF) 属于 **EU**,不是 CA
//   - WE / WS(两个 USD 计价的子公司)属于 **CA**,不是 EU
//
// 这张表是全项目唯一的权威来源:endpoint 推导、目录站点、VPS 可用性站点都从它派生,
// 以前三处各写一份,彼此矛盾(VPS 把 MA/TN/SN 打到 CA、建账户把 WS/WE 落到 ovh-eu)。
var subsidiaryRegions = map[string]string{
	// EU
	"CZ": "EU", "DE": "EU", "ES": "EU", "EU": "EU", "FI": "EU", "FR": "EU",
	"GB": "EU", "IE": "EU", "IT": "EU", "LT": "EU", "MA": "EU", "NL": "EU",
	"PL": "EU", "PT": "EU", "SN": "EU", "TN": "EU",
	// CA
	"ASIA": "CA", "AU": "CA", "CA": "CA", "IN": "CA", "QC": "CA",
	"SG": "CA", "WE": "CA", "WS": "CA",
	// US
	"US": "US",
}

// SubsidiaryRegion 返回子公司所属的大区(EU / US / CA)。未知子公司按 EU 处理
// (EU 覆盖面最广,且是 OVH 的默认站点)。
func SubsidiaryRegion(sub string) string {
	if r, ok := subsidiaryRegions[strings.ToUpper(strings.TrimSpace(sub))]; ok {
		return r
	}
	return "EU"
}

// KnownSubsidiary 该子公司是否在已知归属表里
func KnownSubsidiary(sub string) bool {
	_, ok := subsidiaryRegions[strings.ToUpper(strings.TrimSpace(sub))]
	return ok
}

// APIBaseURLForRegion 大区 → REST API base URL(不带 /1.0 或 /v1)
func APIBaseURLForRegion(region string) string {
	switch strings.ToUpper(region) {
	case "US":
		return "https://api.us.ovhcloud.com"
	case "CA":
		return "https://ca.api.ovh.com"
	default:
		return "https://eu.api.ovh.com"
	}
}

// EndpointForSubsidiary 子公司 → go-ovh 的 endpoint 名
func EndpointForSubsidiary(sub string) string {
	switch SubsidiaryRegion(sub) {
	case "US":
		return "ovh-us"
	case "CA":
		return "ovh-ca"
	default:
		return "ovh-eu"
	}
}

// CatalogBaseURLForSubsidiary 子公司 → 公开目录所在站点。
// 同一份 catalog 只能从对应站点查,跨站点 400/404。
func CatalogBaseURLForSubsidiary(sub string) string {
	return APIBaseURLForRegion(SubsidiaryRegion(sub))
}

// orderableAvailabilityRe 匹配 dedicated.AvailabilityEnum 里真正能下单的取值。
// 官方枚举 = ['120H','1440H','1H-high','1H-low','2160H','240H','24H','480H','720H','72H',
// 'comingSoon','unavailable','unknown']:只有 \d+H(-high|-low)? 家族是"多久能交付"的承诺,
// comingSoon 是"即将上线但还没开卖",unknown 是"OVH 没给状态",这两个都下不了单。
//
// 放在 ovh 包是为了让 app / catalog / telegram 都能用同一份判据 ——
// catalog 依赖 app,app 不能反过来 import catalog,只有最底层的 ovh 包对三者都可见。
var orderableAvailabilityRe = regexp.MustCompile(`^\d+H(-high|-low)?$`)

// IsAvailableForOrder 用白名单判断某个可用性取值是否真的可以下单。
// 全项目统一走这个函数,不要再写 `!= "unavailable"` 这种黑名单 —— 那会把 comingSoon 当成有货。
func IsAvailableForOrder(availability string) bool {
	return orderableAvailabilityRe.MatchString(availability)
}

// DefaultSubsidiaryForEndpoint 账户没填 zone 时,按 endpoint 推一个同大区的默认子公司。
// 以前一律回落 "IE",对美区账户意味着拿欧洲子公司的目录和价格去下单,必然失败。
func DefaultSubsidiaryForEndpoint(endpoint string) string {
	switch strings.ToLower(strings.TrimSpace(endpoint)) {
	case "ovh-us":
		return "US"
	case "ovh-ca", "kimsufi-ca", "soyoustart-ca":
		return "CA"
	default:
		return "IE"
	}
}

// EndpointRegion 把 go-ovh 的 endpoint 名归到 EU / US / CA 三个大区。
// 同大区内的品牌别名(ovh-eu / kimsufi-eu / soyoustart-eu)共用同一套目录站点。
func EndpointRegion(endpoint string) string {
	e := strings.ToLower(strings.TrimSpace(endpoint))
	switch {
	case e == "ovh-us":
		return "US"
	case strings.HasSuffix(e, "-ca"):
		return "CA"
	default:
		return "EU"
	}
}
