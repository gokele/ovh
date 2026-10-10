package handlers

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/numconv"
)

// ============================================================================
// 硬件"中奖"检测
//
// 定义:下单时订购的配置 ≠ 机房实际交付的硬件(且实际更好),社区俗称"中奖"。
//
// 订购配置来源(账单侧,下单那一刻就定死,之后不会变):
//   GET /services/{serviceId}          → resource.product.description = 订购 CPU(如 "Intel Xeon E5-1620v2")
//   GET /services/{serviceId}/options  → 子服务里 ram-32g-ecc-1333 / softraid-2x480ssd 就是订购的内存 / 硬盘
// 实际配置来源(机房侧):
//   GET /dedicated/server/{sn}/specifications/hardware → processorName / memorySize / diskGroups
//
// 实例(ns392029 KS-LE-C):订购 E5-1620v2 + 32GB,实配 E5-1630v4 + 64GB → CPU、内存双中奖。
// 注意 specifications/hardware 里的 description("KS-LE-C - Intel Xeon E5-1620v2")也是订购侧文案,
// 不是实配,不能拿它当实际 CPU。
// ============================================================================

// hwLotteryItem 单项中奖明细(只列出"实际优于订购"的项)
type hwLotteryItem struct {
	Kind    string  `json:"kind"`    // cpu | memory | disk
	Ordered string  `json:"ordered"` // 订购配置展示文案
	Actual  string  `json:"actual"`  // 实际配置展示文案
	// 量化幅度,前端据此显示 "+100%"。0 = 算不出来(前端退回普通文案)
	GainPct float64 `json:"gainPct,omitempty"`
	// 硬盘专属:介质升级(HDD→SSD→NVMe)
	MediaUp bool `json:"mediaUp,omitempty"`
}

// hwLottery 返回给前端的比对结果
type hwLottery struct {
	// Checked=false 表示没拿到订购配置(权限不足 / 老合同没有 options 等),前端不显示任何中奖标识
	Checked  bool            `json:"checked"`
	Won      bool            `json:"won"`
	Tier     int             `json:"tier,omitempty"` // 1=中奖 2=大奖 3=头奖(按中奖项数与翻倍幅度)
	PlanCode string          `json:"planCode,omitempty"`
	PlanName string          `json:"planName,omitempty"`
	Items    []hwLotteryItem `json:"items"`
	Reason   string          `json:"reason,omitempty"`
}

// hwOrderedSpec 从账单接口解析出的订购配置
type hwOrderedSpec struct {
	PlanCode  string
	PlanName  string
	CPU       string // 订购 CPU 文案,可能为空
	MemoryGB  float64
	MemLabel  string // 如 "32GB DDR3 ECC 1333MHz"
	Disks     []hwDiskSpec
	DiskLabel string
}

type hwDiskSpec struct {
	Count  int
	SizeGB float64
	Type   string // nvme | ssd | sa | hdd ...
}

// hwSvcLite /services/{id} 与 /services/{id}/options 共用的精简结构
type hwSvcLite struct {
	Billing struct {
		Plan struct {
			Code        string `json:"code"`
			InvoiceName string `json:"invoiceName"`
		} `json:"plan"`
	} `json:"billing"`
	Resource struct {
		Product struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"product"`
	} `json:"resource"`
}

// 订购配置下单后不会再变,按 serviceName 缓存,避免每次打开概览都多打 3 个 OVH 请求
var hwOrderedSpecCache sync.Map // serviceName -> *hwOrderedSpec

// 中奖结果缓存:机器列表页要给每台机器带 lottery 摘要,不能每次列表刷新都
// N×GET specifications/hardware。详情页算完顺手写入,列表页缓存命中直接用,
// 过期(1h)才补算。硬件抽奖开完奖就定了,1h 的陈旧度没有实际影响。
type hwLotteryEntry struct {
	lot hwLottery
	at  time.Time
}

var hwLotteryCache sync.Map // serviceName -> hwLotteryEntry

const hwLotteryTTL = time.Hour

// hwLotteryFor 取(或补算)一台机器的中奖结果。任何失败都返回 nil:
// 列表页的徽章是锦上添花,不允许它拖慢或报错影响列表本身。
func hwLotteryFor(client *ovhsdk.Client, svc string) *hwLottery {
	if v, ok := hwLotteryCache.Load(svc); ok {
		if e := v.(hwLotteryEntry); time.Since(e.at) < hwLotteryTTL {
			lot := e.lot
			return &lot
		}
	}
	spec, err := hwFetchOrderedSpec(client, svc)
	if err != nil {
		return nil
	}
	var hardware map[string]interface{}
	if err := client.Get("/dedicated/server/"+svc+"/specifications/hardware", &hardware); err != nil {
		return nil
	}
	lot := hwComputeLottery(spec, hardware)
	hwLotteryCache.Store(svc, hwLotteryEntry{lot: lot, at: time.Now()})
	return &lot
}

var (
	hwReRAM  = regexp.MustCompile(`^ram-(\d+)g`)
	hwReDisk = regexp.MustCompile(`(\d+)x(\d+)(nvme|ssd|sas|sa|hdd)`)
	// CPU 名里去掉品牌/系列词,只留型号核心,便于 "Intel Xeon E-2274G" 与 "XeonE-2274G" 判等
	hwReCPUNoise = regexp.MustCompile(`(?i)\(r\)|\(tm\)|intel|xeon|amd|ryzen|epyc|core|processor|cpu`)
	hwReNonAlnum = regexp.MustCompile(`[^a-z0-9]`)
	hwReHasDigit = regexp.MustCompile(`\d`)
)

// hwFetchOrderedSpec 拉订购配置(带缓存)
func hwFetchOrderedSpec(client *ovhsdk.Client, svc string) (*hwOrderedSpec, error) {
	if v, ok := hwOrderedSpecCache.Load(svc); ok {
		return v.(*hwOrderedSpec), nil
	}
	sid, err := serviceIDForDedicated(client, svc)
	if err != nil {
		return nil, err
	}
	var (
		main       hwSvcLite
		opts       []hwSvcLite
		mErr, oErr error
		wg         sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); mErr = client.Get(fmt.Sprintf("/services/%d", sid), &main) }()
	go func() { defer wg.Done(); oErr = client.Get(fmt.Sprintf("/services/%d/options", sid), &opts) }()
	wg.Wait()
	if mErr != nil {
		return nil, mErr
	}
	if oErr != nil {
		return nil, oErr
	}
	spec := hwParseOrderedSpec(main, opts)
	hwOrderedSpecCache.Store(svc, spec)
	return spec, nil
}

// hwParseOrderedSpec 纯函数:账单数据 → 订购配置
func hwParseOrderedSpec(main hwSvcLite, opts []hwSvcLite) *hwOrderedSpec {
	spec := &hwOrderedSpec{
		PlanCode: main.Billing.Plan.Code,
		PlanName: main.Billing.Plan.InvoiceName,
	}
	if d := strings.TrimSpace(main.Resource.Product.Description); hwReHasDigit.MatchString(d) {
		spec.CPU = d
	}
	for _, o := range opts {
		name := strings.ToLower(o.Resource.Product.Name)
		if name == "" {
			name = strings.ToLower(o.Billing.Plan.Code)
		}
		label := o.Billing.Plan.InvoiceName
		if label == "" {
			label = o.Resource.Product.Description
		}
		if m := hwReRAM.FindStringSubmatch(name); m != nil {
			gb, _ := strconv.ParseFloat(m[1], 64)
			spec.MemoryGB = gb
			spec.MemLabel = label
			continue
		}
		if strings.HasPrefix(name, "bandwidth") || strings.HasPrefix(name, "vrack") || strings.HasPrefix(name, "traffic") {
			continue
		}
		if ms := hwReDisk.FindAllStringSubmatch(name, -1); len(ms) > 0 {
			spec.Disks = spec.Disks[:0]
			for _, m := range ms {
				n, _ := strconv.Atoi(m[1])
				sz, _ := strconv.ParseFloat(m[2], 64)
				spec.Disks = append(spec.Disks, hwDiskSpec{Count: n, SizeGB: sz, Type: m[3]})
			}
			spec.DiskLabel = label
		}
	}
	return spec
}

// hwComputeLottery 纯函数:订购配置 vs 实际硬件
func hwComputeLottery(spec *hwOrderedSpec, hardware map[string]interface{}) hwLottery {
	res := hwLottery{Items: []hwLotteryItem{}}
	if spec == nil {
		res.Reason = "订购配置不可用"
		return res
	}
	res.Checked = true
	res.PlanCode = spec.PlanCode
	res.PlanName = spec.PlanName

	// ---- CPU:型号不同即视为中奖(OVH 只会往上替换,不会给更差的) ----
	orderedCPU := spec.CPU
	if orderedCPU == "" {
		// 兜底:hardware.description 形如 "KS-LE-C - Intel Xeon E5-1620v2",最后一段是订购 CPU
		if desc, _ := hardware["description"].(string); desc != "" {
			if i := strings.LastIndex(desc, " - "); i >= 0 {
				orderedCPU = strings.TrimSpace(desc[i+3:])
			}
		}
	}
	actualCPU, _ := hardware["processorName"].(string)
	if orderedCPU != "" && actualCPU != "" && !hwCPUSame(orderedCPU, actualCPU) {
		res.Items = append(res.Items, hwLotteryItem{
			Kind:    "cpu",
			Ordered: orderedCPU,
			Actual:  actualCPU,
		})
	}

	// ---- 内存:实际 > 订购 ----
	if spec.MemoryGB > 0 {
		if actualGB := hwSizeGB(hardware["memorySize"]); actualGB > spec.MemoryGB {
			res.Items = append(res.Items, hwLotteryItem{
				Kind:    "memory",
				Ordered: hwFmtGB(spec.MemoryGB),
				Actual:  hwFmtGB(actualGB),
				GainPct: (actualGB - spec.MemoryGB) / spec.MemoryGB * 100,
			})
		}
	}

	// ---- 硬盘:总容量多 5% 以上,或介质升级(HDD→SSD→NVMe) ----
	if len(spec.Disks) > 0 {
		groups, _ := hardware["diskGroups"].([]interface{})
		var actTotal float64
		actRank := 99
		var actParts []string
		for _, g := range groups {
			gm, _ := g.(map[string]interface{})
			if gm == nil {
				continue
			}
			n, _ := numconv.ToInt64(gm["numberOfDisks"])
			if n <= 0 {
				n = 1
			}
			sz := hwSizeGB(gm["diskSize"])
			typ, _ := gm["diskType"].(string)
			actTotal += float64(n) * sz
			if r := hwDiskRank(typ); r < actRank {
				actRank = r
			}
			actParts = append(actParts, fmt.Sprintf("%d× %s %s", n, strings.ToUpper(typ), hwFmtGB(sz)))
		}
		var ordTotal float64
		ordRank := 99
		var ordParts []string
		for _, d := range spec.Disks {
			ordTotal += float64(d.Count) * d.SizeGB
			if r := hwDiskRank(d.Type); r < ordRank {
				ordRank = r
			}
			ordParts = append(ordParts, fmt.Sprintf("%d× %s %s", d.Count, hwDiskTypeLabel(d.Type), hwFmtGB(d.SizeGB)))
		}
		if len(actParts) > 0 {
			bigger := actTotal > ordTotal*1.05
			// 混合盘(多组)订购时介质比较意义不大,只比单组
			better := len(spec.Disks) == 1 && actRank != 99 && actRank > ordRank
			if bigger || better {
				res.Items = append(res.Items, hwLotteryItem{
					Kind:    "disk",
					Ordered: strings.Join(ordParts, " + "),
					Actual:  strings.Join(actParts, " + "),
					GainPct: func() float64 {
						if ordTotal > 0 && bigger {
							return (actTotal - ordTotal) / ordTotal * 100
						}
						return 0
					}(),
					MediaUp: better,
				})
			}
		}
	}

	res.Won = len(res.Items) > 0
	// 等级:中奖项数为底,任一项翻倍以上 +1,封顶 3。
	// 1=中奖(单项小升) 2=大奖(双项,或单项翻倍) 3=头奖(三项全中 / 多项+翻倍)
	if res.Won {
		res.Tier = len(res.Items)
		for _, it := range res.Items {
			if it.GainPct >= 100 {
				res.Tier++
				break
			}
		}
		if res.Tier > 3 {
			res.Tier = 3
		}
	}
	return res
}


func hwNormCPU(s string) string {
	s = hwReCPUNoise.ReplaceAllString(strings.ToLower(s), "")
	return hwReNonAlnum.ReplaceAllString(s, "")
}

func hwCPUSame(a, b string) bool {
	na, nb := hwNormCPU(a), hwNormCPU(b)
	if na == "" || nb == "" {
		return true // 解析不出型号就不下结论,宁可漏报也不误报
	}
	return na == nb || strings.Contains(na, nb) || strings.Contains(nb, na)
}

// hwSizeGB {value, unit} → GB
func hwSizeGB(v interface{}) float64 {
	m, _ := v.(map[string]interface{})
	if m == nil {
		return 0
	}
	val, _ := numconv.ToFloat64(m["value"])
	switch strings.ToUpper(fmt.Sprint(m["unit"])) {
	case "MB":
		return val / 1024
	case "TB":
		return val * 1024
	case "KB":
		return val / 1024 / 1024
	default:
		return val
	}
}

func hwFmtGB(gb float64) string {
	if gb >= 1024 && math.Mod(gb, 1024) == 0 {
		return fmt.Sprintf("%g TB", gb/1024)
	}
	return fmt.Sprintf("%g GB", math.Round(gb*10)/10)
}

func hwDiskRank(t string) int {
	switch strings.ToLower(t) {
	case "nvme":
		return 2
	case "ssd":
		return 1
	case "":
		return 99
	default: // sa / sas / hdd / sata
		return 0
	}
}

func hwDiskTypeLabel(t string) string {
	switch strings.ToLower(t) {
	case "nvme":
		return "NVME"
	case "ssd":
		return "SSD"
	default:
		return "HDD"
	}
}
