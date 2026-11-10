package monitor

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ovh-buy/server/internal/notify"
)

// 机房代码 → 中文显示。key 一律是「城市段」,长代码由 availabilityDCCity 归一化后再查。
//
// 取值范围来自 dedicated.AvailabilityDatacenterEnum —— 实测 EU / US / CA 三个站点
// 的这份枚举完全一致(48 个取值),里面既有短代码(gra / bhs / vin)也有带可用区的
// 长代码(eu-west-par-a / ca-east-tor-a / ap-southeast-sgp-a)。
// live 数据两种都会出现:三个站点的 availabilities 里都实际返回过
// eu-west-par-a|b|c 和 ca-east-tor-a,而 vin / hil 只在 US 站点出现。
//
// ⚠️ 国别/城市的唯一事实来源是 catalog 包里的 dcCityMap / dcCountryMap
// (服务器列表、前端 web/src/routes/vps-control.tsx 都按那张表显示)。
// 这里之所以还留一份,只是因为 catalog 的 lookupDCName 目前没导出;
// 两张表分歧过一次,后果是同一个机房在 Telegram 通知里叫「意大利·埃里切」、
// 在服务器列表里叫「英国·埃里斯」,用户没法判断到底下单到了哪个国家。
// 改这里必须同步 catalog,catalog 一旦导出 LookupDCName 就把本表删掉。
//
// 已按 catalog 校正过的三处(OVH 官方机房位置):
//
//	eri = 英国 Erith(伦敦东南),不是意大利埃里切
//	lim = 德国 Limburg(林堡),不是波兰利马诺瓦 —— OVH 在波兰只有华沙 WAW
//	bhs = 加拿大 Beauharnois(博阿尔诺),不是博舍维尔
var dcDisplayMapCN = map[string]string{
	"gra":    "🇫🇷 法国·格拉沃利讷",
	"rbx":    "🇫🇷 法国·鲁贝",
	"rbx-hz": "🇫🇷 法国·鲁贝(HZ)",
	"sbg":    "🇫🇷 法国·斯特拉斯堡",
	"par":    "🇫🇷 法国·巴黎",
	"eri":    "🇬🇧 英国·埃里斯",
	"mil":    "🇮🇹 意大利·米兰",
	"lim":    "🇩🇪 德国·林堡",
	"waw":    "🇵🇱 波兰·华沙",
	"fra":    "🇩🇪 德国·法兰克福",
	"lon":    "🇬🇧 英国·伦敦",
	"bhs":    "🇨🇦 加拿大·博阿尔诺",
	"tor":    "🇨🇦 加拿大·多伦多",
	"yyz":    "🇨🇦 加拿大·多伦多",
	"syd":    "🇦🇺 澳大利亚·悉尼",
	"sgp":    "🇸🇬 新加坡",
	"ynm":    "🇮🇳 印度·孟买",
	"mum":    "🇮🇳 印度·孟买",
	"vin":    "🇺🇸 美国·弗吉尼亚",
	"hil":    "🇺🇸 美国·俄勒冈",
	// 枚举里还有国别粒度的取值(不带城市),照样要能显示
	"fr": "🇫🇷 法国", "de": "🇩🇪 德国", "gb": "🇬🇧 英国", "pl": "🇵🇱 波兰",
	"ca": "🇨🇦 加拿大", "us": "🇺🇸 美国", "au": "🇦🇺 澳大利亚",
	"sg": "🇸🇬 新加坡", "in": "🇮🇳 印度",
	// 枚举里还有 eu / default 两个非国家取值,漏了就会显示成裸 "EU" / "DEFAULT"
	"eu": "🇪🇺 欧洲", "default": "默认机房",
}

var dcDisplayShort = map[string]string{
	"gra":    "🇫🇷 Gra",
	"rbx":    "🇫🇷 Rbx",
	"rbx-hz": "🇫🇷 RbxHZ",
	"sbg":    "🇫🇷 Sbg",
	"par":    "🇫🇷 Par",
	"eri":    "🇬🇧 Eri",
	"mil":    "🇮🇹 Mil",
	"lim":    "🇩🇪 Lim",
	"waw":    "🇵🇱 Waw",
	"fra":    "🇩🇪 Fra",
	"lon":    "🇬🇧 Lon",
	"bhs":    "🇨🇦 Bhs",
	"tor":    "🇨🇦 Tor",
	"yyz":    "🇨🇦 Tor",
	"syd":    "🇦🇺 Syd",
	"sgp":    "🇸🇬 Sgp",
	"ynm":    "🇮🇳 Mum",
	"mum":    "🇮🇳 Mum",
	"vin":    "🇺🇸 Vin",
	"hil":    "🇺🇸 Hil",
	"fr":     "🇫🇷 FR", "de": "🇩🇪 DE", "gb": "🇬🇧 GB", "pl": "🇵🇱 PL",
	"ca": "🇨🇦 CA", "us": "🇺🇸 US", "au": "🇦🇺 AU",
	"sg": "🇸🇬 SG", "in": "🇮🇳 IN",
	"eu": "🇪🇺 EU", "default": "默认",
}

// availabilityDCCity 把 dedicated.AvailabilityDatacenterEnum 的取值归一化成城市段 + 可用区。
//
//	"gra"                → ("gra", "")
//	"eu-west-par-b"      → ("par", "B")
//	"ca-east-tor-a"      → ("tor", "A")
//	"ap-southeast-sgp-a" → ("sgp", "A")
//	"rbx-hz"             → ("rbx-hz", "")   两段的先按整体查,查不到再退化
//	"eu-west-1-a"        → ("1", "A")       没有城市名,调用方会退回裸代码
//
// 规则:三段以上且最后一段是单个字母时,那一段是可用区,前一段是城市。
func availabilityDCCity(dc string) (city, az string) {
	d := strings.ToLower(strings.TrimSpace(dc))
	if d == "" {
		return "", ""
	}
	if _, ok := dcDisplayMapCN[d]; ok {
		return d, ""
	}
	parts := strings.Split(d, "-")
	if len(parts) >= 3 {
		last := parts[len(parts)-1]
		if len(last) == 1 && last[0] >= 'a' && last[0] <= 'z' {
			return parts[len(parts)-2], strings.ToUpper(last)
		}
		return last, ""
	}
	return d, ""
}

func dcDisplayCN(dc string) string {
	city, az := availabilityDCCity(dc)
	v, ok := dcDisplayMapCN[city]
	if !ok {
		// 未知代码原样回显:宁可让用户看到裸代码去查,也不要显示一个猜错的城市
		return strings.ToUpper(dc)
	}
	if az != "" {
		return v + " (AZ-" + az + ")"
	}
	return v
}

func dcDisplayShortName(dc string) string {
	city, az := availabilityDCCity(dc)
	v, ok := dcDisplayShort[city]
	if !ok {
		return strings.ToUpper(dc)
	}
	if az != "" {
		return v + "-" + az
	}
	return v
}

// resolveNotifyAccountID 决定「这条上架通知生成的一键下单按钮该落到哪个 OVH 账户」。
//
// 为什么必须解析出账户:planCode 是分区的(EU / US / CA 三份目录基本不重合,
// 实测 US 目录 143 个 planCode 里只有少数与 EU 重合)。按钮不带账户时 webhook 只能
// 用「默认账户」下单,多账户跨区的人就会把欧区机型下到美区账户上 —— OVH 返回
// 200 + 空库存而不是报错,队列一直重试到过期,用户看不出是账户选错了。
//
// 优先级:
//  1. explicit —— 调用方(check.go)显式传进来的账户,最权威;
//  2. sub.AutoOrderAccountID —— 用户为这条订阅明确指定的下单账户,与自动下单走同一个;
//  3. sub.LastCheckAccountID —— 本轮真正查到这批库存的账户,它的大区一定含这个 planCode。
//
// 只接受在账户表里还存在的 id:订阅里可能留着已删除账户的 id,存进按钮只会让回调
// 在几小时后才失败,不如当场退回默认账户。
//
// 锁:这里取 subsMu。调用链是 monitorLoop → runSubscriptionCheck → CheckAvailabilityChange
// → 本函数,全程不持有 subsMu(loop.go 只在拷贝订阅列表时短暂加锁),不会自锁。
// 注意 SendNewServerAlert 是在 CheckNewServers 持有 subsMu 时调用的,那条路径没有按钮,
// 也不要在它里面调本函数。
func (m *Monitor) resolveNotifyAccountID(planCode string, explicit ...string) string {
	valid := func(id string) string {
		id = strings.TrimSpace(id)
		if id == "" {
			return ""
		}
		if acc, ok := m.state.FindAccount(id); ok && acc.ID == id {
			return id
		}
		return ""
	}
	for _, id := range explicit {
		if v := valid(id); v != "" {
			return v
		}
	}

	// 订阅的可变字段必须走 s 自己的锁(types.go 里立的约定):
	// LastCheckAccountID 的写者是检查 goroutine 的 beginCheck(只持 s.mu、不持 subsMu),
	// 这里只持 subsMu 裸读就是无同步的读-写竞争。
	// 先在 subsMu 下把指针挑出来,再逐个取带锁快照。
	m.subsMu.Lock()
	matched := make([]*Subscription, 0, 2)
	for _, sub := range m.subscriptions {
		if sub != nil && sub.PlanCode == planCode {
			matched = append(matched, sub)
		}
	}
	m.subsMu.Unlock()

	var autoOrder, lastCheck string
	for _, sub := range matched {
		snap := sub.snapshot()
		if autoOrder == "" {
			autoOrder = snap.AutoOrderAccountID
		}
		if lastCheck == "" {
			lastCheck = snap.LastCheckAccountID
		}
	}

	if v := valid(autoOrder); v != "" {
		return v
	}
	return valid(lastCheck)
}

// accountID 是可变参数而不是必填形参:写入口 check.go 不在本次改动范围内,
// 加必填形参会直接编译不过。传了就用传的,没传就由 resolveNotifyAccountID 反查订阅。
// buildAvailabilityAlert 拼出上架通知的正文和按钮。
//
// 从 SendAvailabilityAlertGrouped 里拆出来，是为了能在测试里直接看到
// 用户真正会收到的那段文字 —— 通知的排版是这个工具的门面，
// 以前只能靠真的触发一次补货才看得见。
// 注意它有副作用：会把按钮 UUID 写进 telegram_order_buttons。
func (m *Monitor) buildAvailabilityAlert(planCode string, availableDCs []map[string]interface{},
	configInfo map[string]interface{}, serverName string, priceErrorMessage string, traceID, configTraceID string,
	accountID ...string) (string, map[string]interface{}) {

	var msg strings.Builder
	msg.WriteString("🎉 服务器上架通知\n\n")

	// 产品名称:型号名 + CPU。
	// 光一个 planCode(24sk602)对着手机看不出是什么机器,
	// 而抢购那一刻用户要在几秒内判断"这是不是我要的那台"。
	msg.WriteString("📦 产品名称: " + m.productName(planCode, serverName) + "\n")

	memory, _ := configInfo["memory"].(string)
	storage, _ := configInfo["storage"].(string)
	if memory != "" {
		msg.WriteString("💾 内存: " + memory + "\n")
	}
	if storage != "" {
		msg.WriteString("💿 存储: " + storage + "\n")
	}

	// 机房 + 可用性。
	// 单个机房时按"数据中心 / 可用性"两行摊开;多个机房时列成一张表 ——
	// 每个机房的可用性可能不一样(waw 是 1H-low、gra 是 72H),
	// 合并成一句"N 个机房有货"会把这个差别抹掉,而它直接决定先抢哪个。
	var detectedTimes []time.Time
	for _, dcInfo := range availableDCs {
		if dtStr, ok := dcInfo["detected_time"].(string); ok && dtStr != "" {
			if t, err := time.Parse(time.RFC3339Nano, dtStr); err == nil {
				detectedTimes = append(detectedTimes, t)
			}
		}
	}
	if len(availableDCs) == 1 {
		dc, _ := availableDCs[0]["dc"].(string)
		msg.WriteString("📍 数据中心: " + dcLine(dc) + "\n")
		msg.WriteString("✅ 可用性: " + availWording(availableDCs[0]) + "\n")
	} else {
		msg.WriteString(fmt.Sprintf("📍 数据中心: %d 个机房有货\n", len(availableDCs)))
		for _, dcInfo := range availableDCs {
			dc, _ := dcInfo["dc"].(string)
			msg.WriteString("   ✅ " + dcLine(dc) + " — " + availWording(dcInfo) + "\n")
		}
	}

	// 价格。月费和安装费分开写:安装费是一次性的,混在一起会让人以为月付这么多。
	priceText, _ := configInfo["cached_price"].(string)
	installText, _ := configInfo["install_price"].(string)
	switch {
	case priceText != "":
		msg.WriteString("💰 价格: " + priceText + "\n")
	case priceErrorMessage != "":
		// 价格查不到必须明说。留空会被读成"免费"或"还没加载",
		// 而这两种理解都会让用户按下一个他不知道要花多少钱的按钮。
		msg.WriteString("💰 价格: 未获取到（" + priceErrorMessage + "）\n")
	default:
		msg.WriteString("💰 价格: 未获取到\n")
	}
	if installText != "" {
		msg.WriteString("💵 安装费: " + installText + "（一次性）\n")
	}

	// 这个型号已经有几个任务在抢。
	// 补货常连着来好几条通知,对同一台机器按两次就是两笔真实订单,
	// 而按钮的一次性 claim 只挡得住同一颗按钮按两次。
	if n := m.activeQueueCount(planCode); n > 0 {
		msg.WriteString(fmt.Sprintf("\n⚠️ 这个型号已经有 %d 个任务在抢了（发 /queue 查看）\n", n))
	}

	pushTime := m.nowBeijing()
	detected := pushTime
	if len(detectedTimes) > 0 {
		detected = detectedTimes[0]
		for _, t := range detectedTimes[1:] {
			if t.Before(detected) {
				detected = t
			}
		}
	}
	msg.WriteString("\n🕐 检测时间: " + detected.Format("2006-01-02 15:04:05") + "\n")
	// 推送延迟只在明显偏大时才提 —— 正常情况下它是噪音,
	// 但延迟到分钟级说明通道有问题,那时候用户必须知道自己看到的是旧消息。
	if lag := pushTime.Sub(detected); lag >= 30*time.Second {
		msg.WriteString(fmt.Sprintf("⚠️ 这条通知延迟了 %.0f 秒才发出\n", lag.Seconds()))
	}

	// Trace ID 放最后,排查用。放中间会把机房列表和按钮挤开。
	if traceID != "" || configTraceID != "" {
		ids := traceID
		if traceID != "" && configTraceID != "" {
			ids = traceID + " / " + configTraceID
		} else if traceID == "" {
			ids = configTraceID
		}
		msg.WriteString("🆔 " + ids + "\n")
	}

	// 构建按钮（每行最多 2 个）
	type btn struct {
		Text         string `json:"text"`
		CallbackData string `json:"callback_data"`
	}
	keyboard := [][]btn{}
	row := []btn{}
	options := []string{}
	if configInfo != nil {
		if opts, ok := configInfo["options"].([]string); ok {
			options = opts
		} else if optsRaw, ok := configInfo["options"].([]interface{}); ok {
			for _, o := range optsRaw {
				if s, ok := o.(string); ok {
					options = append(options, s)
				}
			}
		}
	}
	btnAccountID := m.resolveNotifyAccountID(planCode, accountID...)
	if btnAccountID == "" {
		// 不是错误:单账户用户、或订阅没勾自动下单时本来就没有账户维度。
		// 记一行是为了在"按钮下到了错误大区"的事故里能一眼看出按钮当时是无账户的。
		m.state.Logger.Debug("一键下单按钮无法解析账户归属，回调时将退回默认账户: "+planCode, "monitor")
	}

	// 能买这台机器的账户。多于一个时每个机房 × 每个账户各一颗按钮 ——
	// 抢稀缺机器时多账户就是多一次机会,而看到补货通知那一刻是最不该去打字切账户的时候。
	//
	// 只认目录,不解析 planCode 后缀:实测美区目录里 42 个 `-eu` 后缀的机型
	// 卖的是欧洲机房的机器,却要用**美区**账户下单。
	btnAccounts := m.AccountsForPlan(planCode)
	if len(btnAccounts) <= 1 {
		// 判不出来 / 只有一个能买 → 保持老样子:每个机房一颗按钮,账户走上面解析出来的那个
		btnAccounts = nil
	}
	// 按钮总数封顶。5 个机房 × 3 个账户 = 15 颗,手机上会把消息挤得没法看,
	// Telegram 对键盘也有上限。超了就退回"每机房一颗",账户仍按订阅解析。
	if len(btnAccounts) > 0 && len(availableDCs)*len(btnAccounts) > maxOrderButtons {
		m.state.Logger.Debug(fmt.Sprintf("按钮数 %d×%d 超过上限 %d,退回每机房一颗",
			len(availableDCs), len(btnAccounts), maxOrderButtons), "monitor")
		btnAccounts = nil
	}

	// 一颗按钮 = 一个 (机房, 账户) 组合
	type target struct {
		dc        string
		accountID string
		label     string
	}
	targets := []target{}
	for _, dcInfo := range availableDCs {
		dc, _ := dcInfo["dc"].(string)
		if len(btnAccounts) == 0 {
			targets = append(targets, target{dc: dc, accountID: btnAccountID,
				label: dcDisplayShortName(dc) + " 一键下单"})
			continue
		}
		for _, a := range btnAccounts {
			targets = append(targets, target{dc: dc, accountID: a.ID,
				label: dcDisplayShortName(dc) + " · " + a.Name})
		}
	}

	for idx, t := range targets {
		msgUUID := uuid.NewString()
		m.AddMessageUUID(msgUUID, planCode, t.dc, options, configInfo)
		// AddMessageUUID 只写不带账户的行,这里紧接着补写 account_id。
		// 失败只退回"默认账户"的老行为,不影响按钮可用。
		if t.accountID != "" && m.state.DB != nil {
			if err := m.state.DB.SetTelegramButtonAccount(msgUUID, t.accountID); err != nil {
				m.state.Logger.Warn("一键下单按钮账户归属落库失败（回调将退回默认账户）: "+err.Error(), "telegram")
			}
		}
		m.state.Logger.Debug(fmt.Sprintf("生成消息UUID: %s, 配置: %s@%s, options=%v, account=%s",
			msgUUID, planCode, t.dc, options, t.accountID), "monitor")

		cb := map[string]string{"a": "add_to_queue", "u": msgUUID}
		cbStr, _ := json.Marshal(cb)
		if len(cbStr) > 64 {
			m.state.Logger.Warn(fmt.Sprintf("UUID callback_data异常长: %d字节, UUID=%s", len(cbStr), msgUUID), "monitor")
		}
		row = append(row, btn{Text: t.label, CallbackData: string(cbStr)})
		if len(row) >= 2 || idx == len(targets)-1 {
			keyboard = append(keyboard, row)
			row = nil
		}
	}
	replyMarkup := map[string]interface{}{"inline_keyboard": keyboard}
	return msg.String(), replyMarkup
}

// SendAvailabilityAlertGrouped 拼好通知并广播出去。
func (m *Monitor) SendAvailabilityAlertGrouped(planCode string, availableDCs []map[string]interface{},
	configInfo map[string]interface{}, serverName string, priceErrorMessage string, traceID, configTraceID string,
	accountID ...string) {
	msgText, replyMarkup := m.buildAvailabilityAlert(planCode, availableDCs, configInfo,
		serverName, priceErrorMessage, traceID, configTraceID, accountID...)

	configDesc := ""
	if configInfo != nil {
		if d, ok := configInfo["display"].(string); ok {
			configDesc = " [" + d + "]"
		}
	}
	m.state.Logger.Info(fmt.Sprintf("正在发送汇总Telegram通知: %s%s - %d个机房", planCode, configDesc, len(availableDCs)), "monitor")
	if notify.Broadcast(m.state, msgText, replyMarkup) > 0 {
		m.state.Logger.Info(fmt.Sprintf("✅ Telegram汇总通知发送成功: %s%s", planCode, configDesc), "monitor")
	} else {
		m.state.Logger.Warn(fmt.Sprintf("⚠️ Telegram汇总通知发送失败: %s%s", planCode, configDesc), "monitor")
	}
}

func (m *Monitor) SendUnavailableAlertGrouped(planCode string, unavailableDCs []map[string]interface{},
	configInfo map[string]interface{}, serverName, traceID, configTraceID string) {

	var msg strings.Builder
	msg.WriteString("📦 服务器下架通知\n\n")
	if serverName != "" {
		msg.WriteString("服务器: " + serverName + "\n")
	}
	msg.WriteString("型号: " + planCode + "\n")
	if configInfo != nil {
		display, _ := configInfo["display"].(string)
		memory, _ := configInfo["memory"].(string)
		storage, _ := configInfo["storage"].(string)
		msg.WriteString("配置: " + display + "\n")
		msg.WriteString("├─ 内存: " + memory + "\n")
		msg.WriteString("└─ 存储: " + storage + "\n")
	}
	msg.WriteString(fmt.Sprintf("\n已下架机房 (%d 个):\n", len(unavailableDCs)))
	for _, dcInfo := range unavailableDCs {
		dc, _ := dcInfo["dc"].(string)
		msg.WriteString("  • " + dcDisplayCN(dc) + " (" + strings.ToUpper(dc) + ")")
		if dt, ok := dcInfo["duration_text"].(string); ok && dt != "" {
			msg.WriteString(" - ⏱️ 本次上架持续: " + strings.TrimPrefix(dt, "历时 "))
		}
		msg.WriteString("\n")
	}
	if traceID != "" || configTraceID != "" {
		if traceID != "" && configTraceID != "" {
			msg.WriteString("\n🆔 Trace ID:\n  订阅: " + traceID + "\n  配置: " + configTraceID)
		} else if traceID != "" {
			msg.WriteString("\n🆔 Trace ID: " + traceID)
		} else {
			msg.WriteString("\n🆔 Trace ID: " + configTraceID)
		}
	}
	msg.WriteString("\n⏰ 时间: " + m.nowBeijing().Format("2006-01-02 15:04:05"))

	configDesc := ""
	if configInfo != nil {
		if d, ok := configInfo["display"].(string); ok {
			configDesc = " [" + d + "]"
		}
	}
	m.state.Logger.Info(fmt.Sprintf("正在发送聚合下架Telegram通知: %s%s - %d个机房", planCode, configDesc, len(unavailableDCs)), "monitor")
	if notify.Broadcast(m.state, msg.String(), nil) > 0 {
		m.state.Logger.Info(fmt.Sprintf("✅ Telegram聚合下架通知发送成功: %s%s", planCode, configDesc), "monitor")
	} else {
		m.state.Logger.Warn(fmt.Sprintf("⚠️ Telegram聚合下架通知发送失败: %s%s", planCode, configDesc), "monitor")
	}
}

func (m *Monitor) SendAvailabilityAlert(planCode, datacenter, status, changeType string,
	configInfo map[string]interface{}, serverName, durationText, priceCheckError, traceID, configTraceID, detectedTime string) {

	var msg strings.Builder
	pushTime := m.nowBeijing()

	switch changeType {
	case "available":
		msg.WriteString("🎉 服务器上架通知！\n\n")
		if serverName != "" {
			msg.WriteString("服务器: " + serverName + "\n")
		}
		msg.WriteString("型号: " + planCode + "\n")
		msg.WriteString("数据中心: " + datacenter + "\n")
		if configInfo != nil {
			display, _ := configInfo["display"].(string)
			memory, _ := configInfo["memory"].(string)
			storage, _ := configInfo["storage"].(string)
			msg.WriteString("配置: " + display + "\n")
			msg.WriteString("├─ 内存: " + memory + "\n")
			msg.WriteString("└─ 存储: " + storage + "\n")
		}
		priceText, _ := configInfo["cached_price"].(string)
		if priceText == "" {
			// 用 30 秒超时保护，
			// 否则在 OVH 价格 API 卡死时整个通知会阻塞
			priceText, _, _ = m.getPriceWithTimeout(planCode, datacenter, configInfo, 30*time.Second)
		}
		if priceText != "" {
			msg.WriteString("\n💰 价格: " + priceText + "\n")
		}
		msg.WriteString("状态: " + status + "\n")
		if durationText != "" {
			msg.WriteString("⏱️ 上次无货→本次有货: " + strings.TrimPrefix(durationText, "历时 ") + "\n")
		}
		if detectedTime != "" {
			if t, err := time.Parse(time.RFC3339Nano, detectedTime); err == nil {
				delay := pushTime.Sub(t)
				secs := int(delay.Seconds())
				minutes := secs / 60
				rem := secs % 60
				msg.WriteString("⏰ 检测时间: " + t.Format("2006-01-02 15:04:05") + "\n")
				msg.WriteString("📤 推送时间: " + pushTime.Format("2006-01-02 15:04:05") + "\n")
				switch {
				case secs > 0 && minutes > 0:
					msg.WriteString(fmt.Sprintf("⏱️ 推送延迟: %d分%d秒\n", minutes, rem))
				case secs > 0:
					msg.WriteString(fmt.Sprintf("⏱️ 推送延迟: %d秒\n", rem))
				default:
					msg.WriteString("⏱️ 推送延迟: <1秒\n")
				}
			}
		} else {
			msg.WriteString("⏰ 推送时间: " + pushTime.Format("2006-01-02 15:04:05") + "\n")
		}
		if traceID != "" || configTraceID != "" {
			if traceID != "" && configTraceID != "" {
				msg.WriteString("\n🆔 Trace ID:\n  订阅: " + traceID + "\n  配置: " + configTraceID)
			} else if traceID != "" {
				msg.WriteString("\n🆔 Trace ID: " + traceID)
			} else {
				msg.WriteString("\n🆔 Trace ID: " + configTraceID)
			}
		}
		msg.WriteString("\n\n💡 快去抢购吧！")
	case "price_check_failed":
		msg.WriteString("📦 服务器可用性通知\n\n")
		if serverName != "" {
			msg.WriteString("服务器: " + serverName + "\n")
		}
		msg.WriteString("型号: " + planCode + "\n")
		msg.WriteString("数据中心: " + datacenter + "\n")
		if configInfo != nil {
			display, _ := configInfo["display"].(string)
			memory, _ := configInfo["memory"].(string)
			storage, _ := configInfo["storage"].(string)
			msg.WriteString("配置: " + display + "\n")
			msg.WriteString("├─ 内存: " + memory + "\n")
			msg.WriteString("└─ 存储: " + storage + "\n")
		}
		if priceText, ok := configInfo["cached_price"].(string); ok && priceText != "" {
			msg.WriteString("\n💰 价格: " + priceText + "\n")
		}
		msg.WriteString("\n状态: 可用性显示有货\n")
		msg.WriteString("时间: " + pushTime.Format("2006-01-02 15:04:05") + "\n")
		if traceID != "" || configTraceID != "" {
			if traceID != "" && configTraceID != "" {
				msg.WriteString("🆔 Trace ID:\n  订阅: " + traceID + "\n  配置: " + configTraceID + "\n")
			} else if traceID != "" {
				msg.WriteString("🆔 Trace ID: " + traceID + "\n")
			} else {
				msg.WriteString("🆔 Trace ID: " + configTraceID + "\n")
			}
		}
		msg.WriteString("\n")
		msg.WriteString("⚠️ 特别说明：\n")
		if priceCheckError != "" {
			msg.WriteString(fmt.Sprintf("（价格校验未通过: %s，已跳过自动下单）", priceCheckError))
		} else {
			msg.WriteString("（价格校验未通过，已跳过自动下单）")
		}
	default:
		msg.WriteString("📦 服务器下架通知\n\n")
		if serverName != "" {
			msg.WriteString("服务器: " + serverName + "\n")
		}
		msg.WriteString("型号: " + planCode + "\n")
		if configInfo != nil {
			display, _ := configInfo["display"].(string)
			memory, _ := configInfo["memory"].(string)
			storage, _ := configInfo["storage"].(string)
			msg.WriteString("配置: " + display + "\n")
			msg.WriteString("├─ 内存: " + memory + "\n")
			msg.WriteString("└─ 存储: " + storage + "\n")
		}
		msg.WriteString("\n数据中心: " + datacenter + "\n")
		msg.WriteString("状态: 已无货\n")
		msg.WriteString("⏰ 时间: " + pushTime.Format("2006-01-02 15:04:05"))
		if traceID != "" || configTraceID != "" {
			if traceID != "" && configTraceID != "" {
				msg.WriteString("\n🆔 Trace ID:\n  订阅: " + traceID + "\n  配置: " + configTraceID)
			} else if traceID != "" {
				msg.WriteString("\n🆔 Trace ID: " + traceID)
			} else {
				msg.WriteString("\n🆔 Trace ID: " + configTraceID)
			}
		}
		if durationText != "" {
			msg.WriteString("\n⏱️ 本次上架持续: " + strings.TrimPrefix(durationText, "历时 "))
		}
	}

	configDesc := ""
	if configInfo != nil {
		if d, ok := configInfo["display"].(string); ok {
			configDesc = " [" + d + "]"
		}
	}
	m.state.Logger.Info(fmt.Sprintf("正在发送Telegram通知: %s@%s%s", planCode, datacenter, configDesc), "monitor")
	if notify.Broadcast(m.state, msg.String(), nil) > 0 {
		m.state.Logger.Info(fmt.Sprintf("✅ Telegram通知发送成功: %s@%s%s - %s", planCode, datacenter, configDesc, changeType), "monitor")
	} else {
		m.state.Logger.Warn(fmt.Sprintf("⚠️ Telegram通知发送失败: %s@%s%s", planCode, datacenter, configDesc), "monitor")
	}
}

func (m *Monitor) SendNewServerAlert(server map[string]interface{}) {
	msg := fmt.Sprintf("🆕 新服务器上架通知！\n\n型号: %v\n名称: %v\nCPU: %v\n内存: %v\n存储: %v\n带宽: %v\n时间: %s\n\n💡 快去查看详情！",
		server["planCode"], server["name"], server["cpu"], server["memory"], server["storage"], server["bandwidth"],
		m.nowBeijing().Format("2006-01-02 15:04:05"))
	notify.Broadcast(m.state, msg, nil)
	m.state.Logger.Info(fmt.Sprintf("发送新服务器提醒: %v", server["planCode"]), "monitor")
}

// maxOrderButtons 上架通知里最多放几颗一键下单按钮。
// 超了就不按账户铺开 —— 手机上一屏塞十几颗按钮没法用,Telegram 对键盘也有上限。
const maxOrderButtons = 10

// activeQueueCount 这个型号当前有几个进行中的抢购任务。
//
// 用来在上架通知里提醒"你已经在抢这台了"。补货往往连着来好几条通知,
// 用户在手机上对同一台机器按两次按钮是很自然的动作,而那就是两笔真实订单。
// 按钮自己的一次性 claim 只挡得住同一颗按钮按两次,挡不住两条通知各按一次。
func (m *Monitor) activeQueueCount(planCode string) int {
	if m.state == nil {
		return 0
	}
	m.state.QueueMu.Lock()
	defer m.state.QueueMu.Unlock()
	n := 0
	for _, it := range m.state.Queue {
		if it.PlanCode != planCode {
			continue
		}
		if it.Status == "running" || it.Status == "pending" || it.Status == "paused" {
			n++
		}
	}
	return n
}

// productName 通知抬头那一行。
//
// 光一个 planCode(24sk602)在手机上看不出是什么机器,而抢购那一刻用户
// 要在几秒内判断"这是不是我要的那台"。所以把型号名和 CPU 拼出来。
// 目录没加载到时退回 planCode —— 不编,也不留空。
func (m *Monitor) productName(planCode, serverName string) string {
	name := strings.TrimSpace(serverName)
	cpu := ""
	if m.state != nil {
		m.state.ServerPlansMu.RLock()
		for _, p := range m.state.ServerPlans {
			if p.PlanCode == planCode {
				if name == "" {
					name = strings.TrimSpace(p.Name)
				}
				cpu = strings.TrimSpace(p.CPU)
				break
			}
		}
		m.state.ServerPlansMu.RUnlock()
	}
	switch {
	case name != "" && cpu != "":
		return name + " | " + cpu
	case name != "":
		return name
	default:
		return planCode
	}
}

// dcLine "waw (波兰华沙)"。拿不到中文名就只写代码。
func dcLine(dc string) string {
	code := strings.ToUpper(dc)
	cn := strings.TrimSpace(dcDisplayCN(dc))
	// dcDisplayCN 拿不到时会退回大写代码,那种情况别写成 "WAW (WAW)"
	if cn == "" || strings.EqualFold(cn, dc) {
		return code
	}
	return code + " (" + cn + ")"
}

// availWording 这个机房的可用性该怎么说。
//
// 取的是 OVH 原样返回的值(1H-low / 72H …),它才带着"多久能交付、库存高低"
// 这两个决定要不要立刻下单的信息。没有原值时退回"有货"——
// 那是归一化之后仅剩的事实,不要编一个更具体的说法出来。
func availWording(dcInfo map[string]interface{}) string {
	if raw, ok := dcInfo["raw_status"].(string); ok && raw != "" {
		if w := AvailabilityCN(raw); w != "" {
			return w
		}
	}
	return "有货"
}
