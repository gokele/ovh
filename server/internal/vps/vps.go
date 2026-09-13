package vps

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
	"github.com/ovh-buy/server/internal/notify"
	"github.com/ovh-buy/server/internal/numconv"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
)

var (
	runningMu sync.Mutex
	running   bool
	// generation 循环代际号,每次 Start 递增。
	// 只翻 running 布尔的话,"停止后立刻启动"会让旧循环活下来:
	// 它下一个检查点看到 running 又是 true,不退出 → 两个循环并存 →
	// 同一次补货下两次单。VPS 侧界面就一个 停止/启动 按钮,点两下即可复现,
	// 而 VPS 循环退出延迟很大(每订阅一次 10 秒超时的 HTTP + 下单往返)。
	generation int64

	// TG 健康检查节流。loop 每 5 分钟 verify 一次,失败自停。
	tgCheckMu   sync.Mutex
	lastTGCheck time.Time
)

const tgRecheckInterval = 5 * time.Minute

// 配置类失败(子公司串区 / planCode 不在该区目录)每轮都会以同样方式复现,
// 按 60s 一轮刷屏没有信息量:同一订阅同一原因只记一次,原因变了或恢复后再记。
var (
	lastCheckErrMu sync.Mutex
	lastCheckErr   = map[string]string{}
)

// logCheckFailure 记录一次可用性检查失败,并做同因去重。
func logCheckFailure(state *app.State, subID, planCode, subsidiary string, err error) {
	msg := fmt.Sprintf("VPS 可用性检查失败 [%s @ %s]: %s", planCode, subsidiary, err.Error())
	lastCheckErrMu.Lock()
	same := lastCheckErr[subID] == msg
	lastCheckErr[subID] = msg
	lastCheckErrMu.Unlock()
	if same {
		return
	}
	var ce *CheckError
	if errors.As(err, &ce) && ce.Permanent() {
		// 配置问题:不会自愈,提到 Error 让用户在日志里一眼看到
		state.Logger.Error(msg+" (该订阅将持续失败,请修正子公司或套餐后重新订阅)", "vps_monitor")
		return
	}
	state.Logger.Warn(msg, "vps_monitor")
}

// clearCheckFailure 恢复正常后清掉去重记录,下次再失败仍会记一条。
func clearCheckFailure(subID string) {
	lastCheckErrMu.Lock()
	delete(lastCheckErr, subID)
	lastCheckErrMu.Unlock()
}

// checkNotifyOrStop 节流后体检所有通知通道,全挂才 Stop()。
// 返回 true=继续 loop,false=已自停。
func checkNotifyOrStop(state *app.State) bool {
	tgCheckMu.Lock()
	due := time.Since(lastTGCheck) >= tgRecheckInterval
	tgCheckMu.Unlock()
	if !due {
		return true
	}
	// 与服务器监控同一口径:所有通道都不可用才停,只挂一条不影响
	ok, reason := notify.AnyAvailable(state, true)
	tgCheckMu.Lock()
	lastTGCheck = time.Now()
	tgCheckMu.Unlock()
	if !ok {
		state.Logger.Error("所有通知通道都不可用,自动停止 VPS 监控("+reason+")", "vps_monitor")
		Stop(state)
		return false
	}
	return true
}

// vpsAPIBaseURL 把 OVH subsidiary 映射到对应区域的 base URL。
// VPS 可用性接口是 public 的,但必须连对站点才能查到该 subsidiary 的 VPS。
//
// 归属表统一在 ovh 包。这里以前自己写了一份,把 MA(摩洛哥)/TN(突尼斯)/SN(塞内加尔)
// 路由到了加区 —— 实测这三个子公司在 ca.api.ovh.com 直接 400,它们属于 EU 站点,
// 于是这三个子公司的 VPS 补货监控一直在查错站点(表现为恒无货)。
func vpsAPIBaseURL(subsidiary string) string {
	return ovh.CatalogBaseURLForSubsidiary(subsidiary)
}

// NormalizeSubsidiary 统一大小写并去空白。
// OVH 的 nichandle.OvhSubsidiaryEnum 只认大写:实测
// eu.api.ovh.com/v1/vps/order/rule/datacenter?ovhSubsidiary=ie → 400
// "Parameter ovhSubsidiary isn't formatted correctly",传 IE 才 200。
// 顺带保证"ie"和"IE"不会被当成两个不同订阅重复建。
func NormalizeSubsidiary(sub string) string {
	return strings.ToUpper(strings.TrimSpace(sub))
}

// DefaultSubsidiary 订阅/手动查询没给子公司时的兜底。
//
// 以前一律写死 "IE"。三个站点的库存彼此独立:美区账户按 IE 查到的是欧洲机房的货,
// 补货通知发了也买不到。所以先跟着自动下单账户的 zone / endpoint 走,
// 没有账户才回落到 ovh 包的默认值(仍是 IE,但那是"没有任何线索"时的选择)。
func DefaultSubsidiary(state *app.State, accountID string) string {
	if acc, ok := state.FindAccount(accountID); ok {
		if zone := NormalizeSubsidiary(acc.Zone); ovh.KnownSubsidiary(zone) {
			return zone
		}
		return ovh.DefaultSubsidiaryForEndpoint(acc.Endpoint)
	}
	return ovh.DefaultSubsidiaryForEndpoint("")
}

// RegionLabel 大区代号 → 中文站点说明,只用于错误文案。
func RegionLabel(region string) string {
	switch region {
	case "US":
		return "US 区(api.us.ovhcloud.com)"
	case "CA":
		return "CA 区(ca.api.ovh.com)"
	default:
		return "EU 区(eu.api.ovh.com)"
	}
}

// CheckError 区分"配置错了,重试也没用"和"临时故障,下轮再试"。
// 前者(区域 / planCode 不匹配)要把中文原因原样透给用户 —— 这类请求
// 每轮都会以同样的方式失败,静默返回空数据只会表现为"这个套餐永远无货"。
type CheckError struct {
	Msg       string
	Retryable bool
}

func (e *CheckError) Error() string { return e.Msg }

// Permanent 该错误是不是配置问题(重试无用)
func (e *CheckError) Permanent() bool { return !e.Retryable }

// bodySnippet 截取 OVH 的错误响应,拼进中文提示里方便定位。
func bodySnippet(b []byte) string {
	t := strings.TrimSpace(strings.NewReplacer("\n", " ", "\r", " ").Replace(string(b)))
	if len(t) > 200 {
		t = t[:200] + "..."
	}
	return t
}

// CheckVPSDCAvailability 查某个 planCode 在该子公司下各机房的库存。
//
// /vps/order/rule/datacenter 三个区都有(EU/CA 正式,US 标 BETA),但它们是三套独立系统:
//
//   - 子公司决定站点:EU 站点传 US/CA、CA 站点传 IE/MA 都会 400
//     "Parameter ovhSubsidiary isn't formatted correctly"(实测)。
//   - planCode 也分区:EU / CA 目录里只有 vps-2025-modelN 这种无后缀码;
//     US 目录额外有 -eu / -ca 后缀码(vps-2025-model1-eu 才是"美国账户买欧洲机房"),
//     而无后缀码在美区指的是美国机房(us-east-vin / us-west-hil)。
//     拿错区的码过来 OVH 回 404 "plan not found"(实测 eu + vps-2025-model1-eu → 404),
//     不是空数组,所以不带原因返回 nil 会让用户完全看不出是配错了区。
//
// 还有一个坑:US 站点**不**校验 ovhSubsidiary —— 传 IE/FR 一样 200,返回的却是美国机房。
// 串区在美区不会报错,只能在本地先用 ovh.KnownSubsidiary / SubsidiaryRegion 兜住。
// pickOSStatus 按订阅勾选的系统挑库存字段。
//
// 两个都勾(或都不勾)时用通用 status;只勾一个时读对应的 linuxStatus / windowsStatus。
// 字段缺失时回落到 status —— 宁可按通用库存报,也不要因为字段没有就当成无货,
// 那会让监控静默失效。
// vpsStockAvailable 白名单:vps.order.rule.DatacenterStatusEnum 三区一致 =
// [available, out-of-stock, out-of-stock-preorder-allowed],只有 available 能下单。
// 用白名单而不是排除法 —— OVH 加新取值时,未知值一律按"无货"处理才安全。
func vpsStockAvailable(status string) bool { return status == "available" }

func pickOSStatus(dc map[string]interface{}, linux, windows bool) string {
	get := func(k string) string {
		v, _ := dc[k].(string)
		return v
	}
	general := get("status")
	switch {
	case linux && !windows:
		if s := get("linuxStatus"); s != "" {
			return s
		}
	case windows && !linux:
		if s := get("windowsStatus"); s != "" {
			return s
		}
	}
	return general
}

// CheckVPSDCAvailability 查某机型在某子公司的各机房库存。
// osFilter 非空时作为 os query 参数带给 OVH(订阅里选的安装系统)。
func CheckVPSDCAvailability(state *app.State, planCode, ovhSubsidiary string) (map[string]interface{}, error) {
	return checkVPSDCAvailabilityOS(state, planCode, ovhSubsidiary, "", "")
}

// accountID 决定这次查询从哪个出口发出去。
// VPS 的可用性轮询是每轮都打的高频路径 —— 不跟着账户的代理走的话,
// 即便每个账户都配了代理,这些请求仍然从本机真实 IP 发出,出口隔离漏了一半。
func checkVPSDCAvailabilityOS(state *app.State, planCode, ovhSubsidiary, osFilter, accountID string) (map[string]interface{}, error) {
	sub := NormalizeSubsidiary(ovhSubsidiary)
	planCode = strings.TrimSpace(planCode)
	if sub == "" {
		return nil, &CheckError{Msg: "缺少 ovhSubsidiary:VPS 库存必须按子公司查(它决定连哪个站点),不能省略"}
	}
	if !ovh.KnownSubsidiary(sub) {
		return nil, &CheckError{Msg: fmt.Sprintf(
			"未知的 OVH 子公司 %q,无法判断它属于 EU / US / CA 哪个站点。可用值:EU 区 CZ DE ES EU FI FR GB IE IT LT MA NL PL PT SN TN;CA 区 ASIA AU CA IN QC SG WE WS;US 区 US", sub)}
	}
	if planCode == "" {
		return nil, &CheckError{Msg: "缺少 planCode"}
	}

	// 多账户:base URL 跟着订阅的 ovhSubsidiary 走,
	// 不再读旧的 state.Config(新建账户不写 kv['config'],它永远是空)
	region := ovh.SubsidiaryRegion(sub)
	baseURL := vpsAPIBaseURL(sub)
	u := baseURL + "/v1/vps/order/rule/datacenter"
	params := url.Values{}
	params.Set("ovhSubsidiary", sub)
	params.Set("planCode", planCode)
	if os := strings.TrimSpace(osFilter); os != "" {
		// schema: GET /vps/order/rule/datacenter 有可选 query os
		// ("VPS OS selection in order api")。订阅选了系统就带上,
		// 让 OVH 直接按目标系统算库存,而不是拿通用库存去猜
		params.Set("os", os)
	}
	fullURL := u + "?" + params.Encode()

	state.Logger.Info(fmt.Sprintf("检查VPS可用性: %s (subsidiary: %s, 站点: %s)", planCode, sub, RegionLabel(region)), "vps_monitor")

	req, _ := http.NewRequest(http.MethodGet, fullURL, nil)
	req.Header.Set("accept", "application/json")
	client, cerr := state.HTTPClientFor(accountID, 10*time.Second)
	if cerr != nil {
		// 配了代理却建不出来 —— 不退回直连。退回的表现是一切正常、
		// 隔离却已经没了,而用户无从察觉。
		return nil, &CheckError{Msg: "出站代理配置有问题: " + cerr.Error(), Retryable: false}
	}
	resp, err := client.Do(req)
	if err != nil {
		// 网络抖动:下一轮还有机会
		return nil, &CheckError{Msg: fmt.Sprintf("请求 %s 失败: %s", RegionLabel(region), err.Error()), Retryable: true}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == http.StatusNotFound:
		// OVH: {"class":"Client::NotFound","message":"not found: plan not found"}
		return nil, &CheckError{Msg: fmt.Sprintf(
			"套餐 %s 不在 %s 的 VPS 目录里(子公司 %s)。三个站点目录不互通:US 目录里的 -eu / -ca 后缀套餐只能在 US 子公司下查,EU / CA 站点只认无后缀套餐(如 vps-2025-model1)。",
			planCode, RegionLabel(region), sub)}
	case resp.StatusCode == http.StatusBadRequest:
		return nil, &CheckError{Msg: fmt.Sprintf(
			"%s 拒绝了这次查询(子公司 %s / 套餐 %s):%s。多半是子公司与站点不匹配。",
			RegionLabel(region), sub, planCode, bodySnippet(body))}
	case resp.StatusCode != http.StatusOK:
		return nil, &CheckError{Msg: fmt.Sprintf(
			"%s 返回 HTTP %d:%s", RegionLabel(region), resp.StatusCode, bodySnippet(body)), Retryable: true}
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, &CheckError{Msg: fmt.Sprintf("%s 返回的不是合法 JSON: %s", RegionLabel(region), bodySnippet(body)), Retryable: true}
	}
	if _, ok := data["datacenters"]; !ok {
		// 200 但没有 datacenters 字段:结构变了,别当成"无货"吞掉
		return nil, &CheckError{Msg: fmt.Sprintf("%s 返回里没有 datacenters 字段: %s", RegionLabel(region), bodySnippet(body)), Retryable: true}
	}
	state.Logger.Info("VPS "+planCode+" 数据中心信息获取成功 ("+sub+")", "vps_monitor")
	return data, nil
}

// cloneSubs 深拷贝订阅快照。
//
// 以前这里是 copy(subs, state.VPSSubscriptions) —— 结构体是复制了,但 LastStatus 是
// map、History/Datacenters 是 slice,复制出来的还是同一个底层对象。检查循环随后在
// **锁外**往 lastStatus[code] 写状态,而 SaveSubscriptions 那边正拿着同一个 map 做
// JSON 序列化。Go 的 map 并发读写不是数据竞争那么客气,是 runtime 直接
// `fatal error: concurrent map read and map write` 把整个进程打死 —— recover 也拦不住。
// 抢购工具半夜静默退出就没有然后了。
//
// 所以快照必须连引用类型一起复制:循环里怎么改都只动自己那一份,
// 改完再在锁内按 ID 合并回去。
func cloneSubs(src []types.VPSSubscription) []types.VPSSubscription {
	out := make([]types.VPSSubscription, len(src))
	copy(out, src)
	for i := range out {
		if src[i].LastStatus != nil {
			m := make(map[string]string, len(src[i].LastStatus))
			for k, v := range src[i].LastStatus {
				m[k] = v
			}
			out[i].LastStatus = m
		}
		if src[i].Datacenters != nil {
			d := make([]string, len(src[i].Datacenters))
			copy(d, src[i].Datacenters)
			out[i].Datacenters = d
		}
		if src[i].History != nil {
			h := make([]map[string]interface{}, len(src[i].History))
			copy(h, src[i].History) // 单条 history 写入后不再改,浅拷贝到这层就够
			out[i].History = h
		}
	}
	return out
}

// SaveSubscriptions 把订阅 + check_interval 写回 SQLite
// saveSubsMu 保存串行化,同 monitor.saveMu。
// VPS 这边尤其确凿:后台监控循环每轮都调 SaveSubscriptions,
// 而 handler 有 5 处也在调,后台和用户点击必然并发。
var saveSubsMu sync.Mutex

func SaveSubscriptions(state *app.State) error {
	if err := state.SaveBlocked("vps_subscriptions"); err != nil {
		state.Logger.Error(err.Error(), "vps")
		return err
	}
	saveSubsMu.Lock()
	defer saveSubsMu.Unlock()
	state.VPSSubsMu.Lock()
	// 同样要深拷贝:ReplaceVPSSubscriptions 会在锁外把 LastStatus 序列化成 JSON,
	// 浅拷贝的话它读的是和内存里同一个 map。
	subs := cloneSubs(state.VPSSubscriptions)
	interval := state.VPSCheckInterval
	state.VPSSubsMu.Unlock()
	if err := state.DB.ReplaceVPSSubscriptions(subs); err != nil {
		state.Logger.Error("保存VPS订阅时出错: "+err.Error(), "")
		return err
	}
	if err := state.DB.SetKV("vps_check_interval", interval); err != nil {
		state.Logger.Error("保存VPS检查间隔时出错: "+err.Error(), "")
		return err
	}
	state.Logger.Info(fmt.Sprintf("已保存 %d 个VPS订阅", len(subs)), "")
	return nil
}

var vpsModelMap = map[string]string{
	"vps-2025-model1": "VPS-1",
	"vps-2025-model2": "VPS-2",
	"vps-2025-model3": "VPS-3",
	"vps-2025-model4": "VPS-4",
	"vps-2025-model5": "VPS-5",
	"vps-2025-model6": "VPS-6",
}

// vpsPlanDisplay 通知里显示的套餐名。
// 美区目录的套餐码带 -eu / -ca 后缀(vps-2025-model1-eu = 美国账户买欧洲机房),
// 直接查表查不到,美区订阅的通知就只能显示裸 planCode。
func vpsPlanDisplay(planCode string) string {
	if name, ok := vpsModelMap[planCode]; ok {
		return name
	}
	for _, suffix := range []string{"-eu", "-ca"} {
		if strings.HasSuffix(planCode, suffix) {
			if name, ok := vpsModelMap[strings.TrimSuffix(planCode, suffix)]; ok {
				return name + "(" + strings.ToUpper(strings.TrimPrefix(suffix, "-")) + " 机房)"
			}
		}
	}
	return planCode
}

var statusMap = map[string]string{
	"available":                     "现货",
	"out-of-stock":                  "无货",
	"out-of-stock-preorder-allowed": "缺货（可预订）",
	"unavailable":                   "不可用",
	"unknown":                       "未知",
}

// 必须带上子公司:同一个 planCode 在 EU / US / CA 三个站点是三份互不相干的库存,
// 不写子公司的话,同时监控 IE 和 US 的用户收到的两条通知长得一模一样,分不清该去哪买。
func SendSummaryNotification(state *app.State, planCode, ovhSubsidiary string, dcs []map[string]interface{}, changeType string) bool {
	cfg := state.Config.Get()
	if cfg.TgToken == "" || cfg.TgChatID == "" || len(dcs) == 0 {
		return false
	}
	planDisplay := vpsPlanDisplay(planCode)
	var emoji, title string
	switch changeType {
	case "initial":
		emoji, title = "📊", "VPS初始状态"
	case "available":
		emoji, title = "🎉", "VPS补货通知"
	default:
		emoji, title = "📦", "VPS下架通知"
	}
	var sb strings.Builder
	sb.WriteString(emoji + " " + title + "\n\n")
	sb.WriteString("套餐: " + planDisplay + "\n")
	if sub := NormalizeSubsidiary(ovhSubsidiary); sub != "" {
		sb.WriteString("子公司: " + sub + " / " + RegionLabel(ovh.SubsidiaryRegion(sub)) + "\n")
	}
	sb.WriteString("时间: " + time.Now().Format("2006-01-02 15:04:05") + "\n\n")
	for idx, dc := range dcs {
		st, _ := dc["status"].(string)
		statusCN, ok := statusMap[st]
		if !ok {
			statusCN = st
		}
		name, _ := dc["name"].(string)
		code, _ := dc["code"].(string)
		sb.WriteString(fmt.Sprintf("%d. %s (%s)\n   状态: %s", idx+1, name, code, statusCN))
		if days, ok := numconv.ToInt64(dc["days"]); ok && days > 0 {
			sb.WriteString(fmt.Sprintf(" | 预计交付: %d天", days))
		}
		sb.WriteString("\n")
	}
	if changeType == "available" {
		sb.WriteString("\n💡 快去抢购吧！")
	}
	result := notify.Broadcast(state, sb.String(), nil) > 0
	if result {
		state.Logger.Info(fmt.Sprintf("✅ VPS汇总通知发送成功: %s (%d个机房)", planCode, len(dcs)), "vps_monitor")
	} else {
		state.Logger.Warn(fmt.Sprintf("⚠️ VPS汇总通知发送失败: %s", planCode), "vps_monitor")
	}
	return result
}

// MonitorLoop 兼容入口:按当前代际号跑。
func MonitorLoop(state *app.State) {
	runningMu.Lock()
	gen := generation
	runningMu.Unlock()
	monitorLoopGen(state, gen)
}

// stillMine 这个循环是不是还该继续跑:running 为真,且代际号还是自己那一代
func stillMine(gen int64) bool {
	runningMu.Lock()
	defer runningMu.Unlock()
	return running && generation == gen
}

func monitorLoopGen(state *app.State, gen int64) {
	state.Logger.Info("VPS监控循环已启动", "vps_monitor")
	for {
		isRunning := stillMine(gen)
		if !isRunning {
			break
		}

		// TG 失效 → 自停。checkNotifyOrStop 内部 5min 节流。
		if !checkNotifyOrStop(state) {
			break
		}

		state.VPSSubsMu.Lock()
		subs := cloneSubs(state.VPSSubscriptions)
		interval := state.VPSCheckInterval
		state.VPSSubsMu.Unlock()

		if len(subs) > 0 {
			state.Logger.Info(fmt.Sprintf("开始检查 %d 个VPS订阅...", len(subs)), "vps_monitor")
			for idx := range subs {
				isRunning = stillMine(gen)
				if !isRunning {
					break
				}
				sub := &subs[idx]
				// 老订阅可能没存子公司(或存了小写)。跟着自动下单账户所在站点走,
				// 不再一律回落 IE —— 美区账户拿 IE 查的是欧洲库存,永远等不到自己能买的货。
				ovhSub := NormalizeSubsidiary(sub.OvhSubsidiary)
				if ovhSub == "" {
					ovhSub = DefaultSubsidiary(state, sub.AutoOrderAccountID)
				}
				currentData, err := checkVPSDCAvailabilityOS(state, sub.PlanCode, ovhSub, sub.OS, sub.AutoOrderAccountID)
				if err != nil {
					logCheckFailure(state, sub.ID, sub.PlanCode, ovhSub, err)
					continue
				}
				clearCheckFailure(sub.ID)
				dcsRaw, _ := currentData["datacenters"].([]interface{})
				if sub.LastStatus == nil {
					sub.LastStatus = map[string]string{}
				}
				lastStatus := sub.LastStatus
				monitoredDCs := sub.Datacenters

				initialAvailable := []map[string]interface{}{}
				newAvailable := []map[string]interface{}{}
				newUnavailable := []map[string]interface{}{}
				isFirstCheckOverall := len(lastStatus) == 0
				// 勾了机房却一个都没匹配上 = 这条订阅永远不会响。
				// 不报出来的话它和"这些机房目前无货"表现完全一样:
				// 界面正常、日志干净、就是永远不动。
				matchedDC := false

				for _, dcRaw := range dcsRaw {
					dc, ok := dcRaw.(map[string]interface{})
					if !ok {
						continue
					}
					code, _ := dc["code"].(string)
					name, _ := dc["datacenter"].(string)
					// 按订阅勾选的系统取对应字段。schema 的 vps.order.rule.Datacenter
					// 有 status / linuxStatus / windowsStatus 三个,三区都有 ——
					// 以前只读 status,导致界面上的「监控系统」勾选框完全是摆设:
					// 只勾 Windows 的用户在 Linux 有货时照样收到补货通知,
					// 开了自动下单还会白跑一整条建车链路,到 checkout 才失败。
					currentStatus := pickOSStatus(dc, sub.MonitorLinux, sub.MonitorWindows)
					daysI64, _ := numconv.ToInt64(dc["daysBeforeDelivery"])
					days := int(daysI64)

					if len(monitoredDCs) > 0 {
						found := false
						for _, m := range monitoredDCs {
							// 必须不分大小写。这一边是 OVH 返回的 code
							// (可用性接口给的是 GRA / BHS / US-EAST-VA 这种大写,
							// 见 purchase.go 的 dcRegion 表),另一边是用户在输入框里
							// **手打**的一串文本,中间没有任何一层做过归一。
							// 用 == 的话大小写一对不上就全部 continue 掉:
							// 订阅恒定 0 状态更新、不发通知、不下单,而界面上它
							// 显示得完全正常 —— 这是最难被发现的那种坏法。
							if strings.EqualFold(strings.TrimSpace(m), code) {
								found = true
								break
							}
						}
						if !found {
							continue
						}
					}
					matchedDC = true
					oldStatus, hasOld := lastStatus[code]
					if !hasOld {
						initialAvailable = append(initialAvailable, map[string]interface{}{
							"name":   name,
							"code":   code,
							"status": currentStatus,
							"days":   days,
						})
						if vpsStockAvailable(currentStatus) {
							sub.History = append(sub.History, map[string]interface{}{
								"timestamp":      time.Now().Format(time.RFC3339Nano),
								"datacenter":     name,
								"datacenterCode": code,
								"status":         currentStatus,
								"changeType":     "available",
								"oldStatus":      nil,
							})
						}
					} else {
						// 白名单判据,和独服的 ovh.IsAvailableForOrder 同口径。
						// 以前用排除法列举 out-of-stock*,OVH 若新增一个"买不了"的取值
						// (独服那边就有 comingSoon),会被判成有货 → 误报 + 误下单。
						wasUnavail := !vpsStockAvailable(oldStatus)
						isUnavail := !vpsStockAvailable(currentStatus)
						if wasUnavail && !isUnavail {
							newAvailable = append(newAvailable, map[string]interface{}{
								"name":   name,
								"code":   code,
								"status": currentStatus,
								"days":   days,
							})
							sub.History = append(sub.History, map[string]interface{}{
								"timestamp":      time.Now().Format(time.RFC3339Nano),
								"datacenter":     name,
								"datacenterCode": code,
								"status":         currentStatus,
								"changeType":     "available",
								"oldStatus":      oldStatus,
							})
						} else if !wasUnavail && isUnavail {
							newUnavailable = append(newUnavailable, map[string]interface{}{
								"name":   name,
								"code":   code,
								"status": currentStatus,
								"days":   days,
							})
							sub.History = append(sub.History, map[string]interface{}{
								"timestamp":      time.Now().Format(time.RFC3339Nano),
								"datacenter":     name,
								"datacenterCode": code,
								"status":         currentStatus,
								"changeType":     "unavailable",
								"oldStatus":      oldStatus,
							})
						}
					}
					lastStatus[code] = currentStatus
				}

				if len(monitoredDCs) > 0 && !matchedDC {
					// 把 OVH 这次真正返回的机房码一并写出来,用户照着改就行。
					// 最常见的原因是机房码打错了或大小写/前缀不对
					// (比如把 US-EAST-VA 写成 va)。
					avail := make([]string, 0, len(dcsRaw))
					for _, dcRaw := range dcsRaw {
						if dc, ok := dcRaw.(map[string]interface{}); ok {
							if c, _ := dc["code"].(string); c != "" {
								avail = append(avail, c)
							}
						}
					}
					state.Logger.Warn(fmt.Sprintf(
						"VPS 订阅 %s 指定的机房 %v 一个都不在 OVH 返回的列表里,这条订阅不会有任何通知。OVH 当前返回:%v",
						sub.PlanCode, monitoredDCs, avail), "vps_monitor")
				}

				if isFirstCheckOverall && len(initialAvailable) > 0 && sub.NotifyAvailable {
					state.Logger.Info(fmt.Sprintf("VPS %s 初始状态检查完成，%d个数据中心", sub.PlanCode, len(initialAvailable)), "vps_monitor")
					SendSummaryNotification(state, sub.PlanCode, ovhSub, initialAvailable, "initial")
				} else {
					if len(newAvailable) > 0 && sub.NotifyAvailable {
						state.Logger.Info(fmt.Sprintf("VPS %s 补货：%d个数据中心", sub.PlanCode, len(newAvailable)), "vps_monitor")
						SendSummaryNotification(state, sub.PlanCode, ovhSub, newAvailable, "available")
					}
					// 补货了就下单。放在通知之后:先让用户知道有货,
					// 下单本身要走五六次 OVH 往返,失败了通知也已经发出去了。
					// 只认"无货→有货"这一种跳变,首次检查发现的存量不算 ——
					// 那可能是一台挂了几个月的订阅刚启动,用户并没打算现在就买。
					if len(newAvailable) > 0 && sub.AutoOrder {
						toOrder := *sub
						toOrder.OvhSubsidiary = ovhSub
						autoOrderOnRestock(state, toOrder, newAvailable)
					}
					if len(newUnavailable) > 0 && sub.NotifyUnavailable {
						state.Logger.Info(fmt.Sprintf("VPS %s 下架：%d个数据中心", sub.PlanCode, len(newUnavailable)), "vps_monitor")
						SendSummaryNotification(state, sub.PlanCode, ovhSub, newUnavailable, "unavailable")
					}
				}

				sub.LastStatus = lastStatus
				if len(sub.History) > 100 {
					sub.History = sub.History[len(sub.History)-100:]
				}
				time.Sleep(time.Second)
			}
			// 按 ID 合并写回：保留循环中对 LastStatus/History 的更新，不覆盖循环期间用户新增/删除的订阅
			state.VPSSubsMu.Lock()
			byID := map[string]*types.VPSSubscription{}
			for i := range subs {
				byID[subs[i].ID] = &subs[i]
			}
			for i := range state.VPSSubscriptions {
				if updated, ok := byID[state.VPSSubscriptions[i].ID]; ok {
					state.VPSSubscriptions[i].LastStatus = updated.LastStatus
					state.VPSSubscriptions[i].History = updated.History
				}
			}
			state.VPSSubsMu.Unlock()
			_ = SaveSubscriptions(state)
		} else {
			state.Logger.Info("当前无VPS订阅，跳过检查", "vps_monitor")
		}

		isRunning = stillMine(gen)
		if isRunning {
			state.Logger.Info(fmt.Sprintf("等待 %d 秒后进行下次VPS检查...", interval), "vps_monitor")
			for i := 0; i < interval; i++ {
				isRunning = stillMine(gen)
				if !isRunning {
					break
				}
				time.Sleep(time.Second)
			}
		}
	}
	state.Logger.Info("VPS监控循环已停止", "vps_monitor")
}

// Start 启动监控
func Start(state *app.State) bool {
	runningMu.Lock()
	if running {
		runningMu.Unlock()
		return false
	}
	running = true
	// 代际号:Start/Stop 只翻布尔的话,"停止后立刻启动"会让旧循环活下来 ——
	// 它下一个检查点看到 running 又是 true,不退出,于是两个循环并存,
	// 同一次补货被下两次单(VPS 侧界面就一个 停止/启动 按钮,点两下即可复现,
	// 而 VPS 循环退出延迟很大:每订阅一次 10 秒超时的 HTTP + 下单的五六次往返)。
	// 每个循环记住自己出生时的代际号,不是当前代就退出。
	generation++
	gen := generation
	runningMu.Unlock()
	// 重置 TG 检查时间戳,保证启动后第一轮一定 verify
	tgCheckMu.Lock()
	lastTGCheck = time.Time{}
	tgCheckMu.Unlock()
	go monitorLoopGen(state, gen)
	state.Logger.Info(fmt.Sprintf("VPS监控已启动 (检查间隔: %d秒)", state.VPSCheckInterval), "vps_monitor")
	return true
}

// Stop 停止监控
func Stop(state *app.State) bool {
	runningMu.Lock()
	if !running {
		runningMu.Unlock()
		return false
	}
	running = false
	runningMu.Unlock()
	state.Logger.Info("正在停止VPS监控...", "vps_monitor")
	return true
}

// Running 返回是否在运行
func Running() bool {
	runningMu.Lock()
	defer runningMu.Unlock()
	return running
}
