package vps

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ovh-buy/server/internal/ovh"
)

// VPS 型号列表。
//
// 为什么必须从 OVH 实时目录取,而不是在代码里写死:
// 型号是**会整代下架**的。实测 2026-08 时,写死在前端的 vps-2025-model1..6 已经全部
// 丢掉了 order-funnel:show / website:show 两个标记(即 OVH 不再对外售卖,目录里留着
// 只为老客户续费),同一时刻 vps-2027 在 6-7 个机房有货。
//
// 后果不是"少一个新型号可选",而是补货监控整体失效:订阅一个停售型号,
// 库存接口老老实实返回 11 个机房、全部无货,永远不会跳变,也就永远不发通知。
// 症状("一直没货")和"这台机器确实抢手"长得一模一样,能静默几个月没人发现。
// 写死一次就意味着每次 OVH 换代都要发个版本,而中间那段时间用户是在盯着空气。

// Model 一个可下单的 VPS 型号
type Model struct {
	PlanCode string `json:"planCode"`
	// Name OVH 自己的商品名,如 "VPS-2 2027"。不自己拼:LZ(本地区域)之类的
	// 变体名字里带信息,自己拼会拼丢
	Name string `json:"name"`
	// Generation 代次,如 "2027"。前端拿它分组
	Generation string `json:"generation"`
	// Price 月付价格,已经是 OVH 格式化好的("€ 8.49" / "$5.35 USD"),
	// 不自己做货币换算 —— 三个站点币种不同,自己格式化只会格错
	Price string `json:"price,omitempty"`
	// Location 变体所在地。US 站点会把欧洲/加拿大机房的 VPS 以 -eu / -ca 后缀
	// 单独作为商品卖,同名不同货
	Location string `json:"location,omitempty"`
	// Datacenters 这个型号能装在哪些机房。三个站点取值完全不同 ——
	// US 的 vps-xxx 只有 US-EAST-VA / US-WEST-OR,欧洲机房要买 -eu 后缀的另一个商品
	Datacenters []string `json:"datacenters,omitempty"`
	// OSChoices 下单时可选的系统。VPS 和独服不同:系统是下单时就要定的配置项
	OSChoices []string `json:"osChoices,omitempty"`
}

type modelsCacheEntry struct {
	models    []Model
	fetchedAt time.Time
	err       error
}

const modelsCacheTTL = 2 * time.Hour

// 失败也缓存一小会:OVH 抽风时不要每次刷新页面都去撞一次
const modelsCacheFailTTL = 2 * time.Minute

var (
	modelsMu    sync.Mutex
	modelsCache = map[string]modelsCacheEntry{}
)

// Models 某子公司当前可下单的 VPS 型号。
// 走公开目录,不带凭据、不占账户配额。
func Models(subsidiary string) ([]Model, error) {
	sub := NormalizeSubsidiary(subsidiary)
	if sub == "" {
		return nil, fmt.Errorf("缺少 ovhSubsidiary:VPS 目录必须按子公司查(它决定连哪个站点)")
	}
	if !ovh.KnownSubsidiary(sub) {
		return nil, fmt.Errorf("未知的 OVH 子公司 %q", sub)
	}

	modelsMu.Lock()
	if c, ok := modelsCache[sub]; ok {
		fresh := c.err == nil && time.Since(c.fetchedAt) < modelsCacheTTL
		failFresh := c.err != nil && time.Since(c.fetchedAt) < modelsCacheFailTTL
		if fresh || failFresh {
			modelsMu.Unlock()
			return c.models, c.err
		}
	}
	modelsMu.Unlock()

	models, err := fetchModels(sub)

	modelsMu.Lock()
	// 拉失败时保留上一次的好结果:一份两小时前的型号列表,
	// 也远比让用户对着一个空下拉框强
	if err != nil {
		if c, ok := modelsCache[sub]; ok && c.err == nil && len(c.models) > 0 {
			modelsCache[sub] = modelsCacheEntry{models: c.models, fetchedAt: time.Now(), err: nil}
			stale := c.models
			modelsMu.Unlock()
			return stale, nil
		}
	}
	modelsCache[sub] = modelsCacheEntry{models: models, fetchedAt: time.Now(), err: err}
	modelsMu.Unlock()
	return models, err
}

// vpsCatalogURL 公开 VPS 目录。站点由子公司决定 —— 拿 CA 的子公司去 eu.api.ovh.com
// 是 400 invalid ovhSubsidiary,不是空目录。
func vpsCatalogURL(subsidiary string) string {
	q := url.Values{}
	q.Set("ovhSubsidiary", subsidiary)
	return ovh.CatalogBaseURLForSubsidiary(subsidiary) + "/v1/order/catalog/public/vps?" + q.Encode()
}

// catalogPlan 只取需要的字段。整份目录 200+ 个套餐,全解出来没意义。
type catalogPlan struct {
	PlanCode    string `json:"planCode"`
	InvoiceName string `json:"invoiceName"`
	Blobs       struct {
		Tags       []string `json:"tags"`
		Commercial struct {
			Line string `json:"line"`
		} `json:"commercial"`
	} `json:"blobs"`
	Pricings []struct {
		Description    string `json:"description"`
		FormattedPrice string `json:"formattedPrice"`
		IntervalUnit   string `json:"intervalUnit"`
	} `json:"pricings"`
	Configurations []struct {
		Name   string   `json:"name"`
		Values []string `json:"values"`
	} `json:"configurations"`
}

func fetchModels(subsidiary string) ([]Model, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodGet, vpsCatalogURL(subsidiary), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取 %s 的 VPS 目录失败: %w", subsidiary, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("拉取 %s 的 VPS 目录失败(%s 站点): HTTP %d %s",
			subsidiary, ovh.SubsidiaryRegion(subsidiary), resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var doc struct {
		Plans []catalogPlan `json:"plans"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("解析 %s 的 VPS 目录失败: %w", subsidiary, err)
	}

	out := make([]Model, 0, 16)
	for _, p := range doc.Plans {
		// order-funnel:show 是 OVH 自己的"现在还卖不卖"标记。
		// 用它而不是拿 planCode 猜代次:猜的话每次 OVH 换代都得改代码,
		// 而且分不出"下架了"和"我这条正则没覆盖到"。
		if !hasTag(p.Blobs.Tags, "order-funnel:show") {
			continue
		}
		out = append(out, Model{
			PlanCode:    p.PlanCode,
			Name:        strings.TrimSpace(p.InvoiceName),
			Generation:  p.Blobs.Commercial.Line,
			Price:       monthlyPrice(p),
			Location:    locationOf(p.PlanCode),
			Datacenters: configValues(p, "vps_datacenter"),
			OSChoices:   configValues(p, "vps_os"),
		})
	}

	sortModels(out)
	return out, nil
}

// configValues 取某个配置项的合法取值。
// 直接从目录读而不是在代码里维护一张表:机房和系统列表 OVH 随时会改,
// 写死一次就意味着每次变动都要发版,而中间那段时间用户会被挡在门外。
func configValues(p catalogPlan, name string) []string {
	for _, c := range p.Configurations {
		if c.Name == name {
			return c.Values
		}
	}
	return nil
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

func monthlyPrice(p catalogPlan) string {
	for _, pr := range p.Pricings {
		if pr.IntervalUnit == "month" && pr.Description == "Monthly fees" {
			return pr.FormattedPrice
		}
	}
	return ""
}

// locationOf US 站点把欧洲/加拿大机房的 VPS 以 -eu / -ca 后缀单独作为商品卖。
// 同名不同货,不标出来用户会以为是重复项。
func locationOf(planCode string) string {
	switch {
	case strings.HasSuffix(planCode, "-eu"):
		return "欧洲机房"
	case strings.HasSuffix(planCode, "-ca"):
		return "加拿大机房"
	default:
		return ""
	}
}

// sortModels 新代次在前,同代按型号序号升序。
// 用户要找的几乎总是"最新那代里的某一档",让它排在最上面。
func sortModels(m []Model) {
	sort.SliceStable(m, func(i, j int) bool {
		gi, gj := genNum(m[i].Generation), genNum(m[j].Generation)
		if gi != gj {
			return gi > gj
		}
		ni, nj := modelNum(m[i].PlanCode), modelNum(m[j].PlanCode)
		if ni != nj {
			return ni < nj
		}
		return m[i].PlanCode < m[j].PlanCode
	})
}

func genNum(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// modelNum 从 vps-2027-model12.LZ-eu 里抠出 12。抠不出来排到最后。
func modelNum(planCode string) int {
	i := strings.Index(planCode, "model")
	if i < 0 {
		return 1 << 30
	}
	rest := planCode[i+len("model"):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 1 << 30
	}
	n, _ := strconv.Atoi(rest[:end])
	return n
}

// IsOrderable 这个 planCode 现在还卖不卖。
// 给订阅体检用:盯着一个停售型号的订阅永远不会响,而用户看到的只是"一直没货"。
func IsOrderable(subsidiary, planCode string) (bool, error) {
	models, err := Models(subsidiary)
	if err != nil {
		return false, err
	}
	for _, m := range models {
		if strings.EqualFold(m.PlanCode, planCode) {
			return true, nil
		}
	}
	return false, nil
}
