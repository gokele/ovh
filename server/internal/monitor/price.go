package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/numconv"
)

// optionsFromConfig 取本次配置组合要询价的 addon 列表
func optionsFromConfig(configInfo map[string]interface{}) []string {
	options := []string{}
	if configInfo == nil {
		return options
	}
	if opts, ok := configInfo["options"].([]string); ok {
		return opts
	}
	if optsRaw, ok := configInfo["options"].([]interface{}); ok {
		for _, o := range optsRaw {
			if s, ok := o.(string); ok {
				options = append(options, s)
			}
		}
	}
	return options
}

// accountIDFromConfig 取本次询价该用哪个账户。
// 账户随 configInfo 走而不是加参数,是为了让 notify.go 里那条"通知时兜底再查一次价格"的
// 路径也能拿到同一个账户 —— 询价账户决定 endpoint 和 ovhSubsidiary,
// 用默认账户验价、却用订阅指定账户下单,两边的可售性和币种都可能对不上。
func accountIDFromConfig(configInfo map[string]interface{}) string {
	if configInfo == nil {
		return ""
	}
	id, _ := configInfo["account_id"].(string)
	return id
}

// —— 通知里的币种 ——
//
// 24 个子公司分属 EU / US / CA 三个站点,计价币种一共 11 种,逐个用
// /v1/order/catalog/public/eco?ovhSubsidiary=X 的 locale.currencyCode 实测过:
//
//	EUR: CZ DE ES FI FR IE IT LT NL PT   GBP: GB   PLN: PL
//	MAD: MA   TND: TN   XOF: SN                          (以上 EU 站点)
//	CAD: CA QC   USD: ASIA WE WS   AUD: AU   INR: IN   SGD: SG   (以上 CA 站点)
//	USD: US                                              (US 站点)
//
// 以前这里在 currencyCode 缺失时一律兜底成 "EUR",并且符号表只认 EUR/USD ——
// 美区/加区账户的通知会把 USD/CAD 的金额印成 "€xx",用户按欧元判断值不值得抢。
// 现在兜底改成按账户子公司取,拿不准就只印 ISO 代码,绝不假装是欧元。
//
// TODO(needsOther): 这张表和 ovh.subsidiaryRegions 是同一个维度的东西,
// 更适合放在 internal/ovh/helpers.go 里供 internal/price 一起用
// (internal/price/price.go 现在只能在 summary 没回币种时留空)。
var subsidiaryCurrency = map[string]string{
	// EU 站点
	"CZ": "EUR", "DE": "EUR", "ES": "EUR", "EU": "EUR", "FI": "EUR", "FR": "EUR",
	"IE": "EUR", "IT": "EUR", "LT": "EUR", "NL": "EUR", "PT": "EUR",
	"GB": "GBP", "PL": "PLN", "MA": "MAD", "TN": "TND", "SN": "XOF",
	// CA 站点
	"CA": "CAD", "QC": "CAD", "ASIA": "USD", "WE": "USD", "WS": "USD",
	"AU": "AUD", "IN": "INR", "SG": "SGD",
	// US 站点
	"US": "USD",
}

// currencyForSubsidiary 子公司的计价币种;未知子公司返回 ""(宁可不印符号也不猜)
func currencyForSubsidiary(sub string) string {
	return subsidiaryCurrency[strings.ToUpper(strings.TrimSpace(sub))]
}

// subsidiaryOfPricingAccount 询价用的那个账户的 ovhSubsidiary。
// accountID 为空时 FindAccount 返回默认账户 —— 与 check.go 里 choice.accountID==""
// 落默认账户查库存是同一个口径,验价和查库存不会跑到两个账户上去。
// 子公司的取法统一走 catalog.SubsidiaryOfAccount(zone 优先,空则按 endpoint 推),
// 不在这里重写一遍 zone/endpoint 的兜底逻辑。
func (m *Monitor) subsidiaryOfPricingAccount(accountID string) string {
	acc, ok := m.state.FindAccount(accountID)
	if !ok {
		return ""
	}
	return catalog.SubsidiaryOfAccount(acc)
}

// defaultCurrencyForAccount OVH 没回 currencyCode 时,按账户所属子公司兜底。
func (m *Monitor) defaultCurrencyForAccount(accountID string) string {
	return currencyForSubsidiary(m.subsidiaryOfPricingAccount(accountID))
}

// formatMoney 把金额渲染成通知里那一行。
// $ 家族(USD/CAD/AUD/SGD)必须带国别前缀:光一个 "$" 在同时管着美区账户和
// 加区账户的监控里是有歧义的,CAD 和 USD 差着三成汇率。
// 没有独占符号的币种(MAD/TND/XOF...)直接印 ISO 代码。
func formatMoney(currency string, v float64) string {
	switch strings.ToUpper(currency) {
	case "EUR":
		return fmt.Sprintf("€%.2f/月", v)
	case "GBP":
		return fmt.Sprintf("£%.2f/月", v)
	case "INR":
		return fmt.Sprintf("₹%.2f/月", v)
	case "PLN":
		return fmt.Sprintf("%.2f zł/月", v)
	case "USD":
		return fmt.Sprintf("US$%.2f/月", v)
	case "CAD":
		return fmt.Sprintf("CA$%.2f/月", v)
	case "AUD":
		return fmt.Sprintf("A$%.2f/月", v)
	case "SGD":
		return fmt.Sprintf("S$%.2f/月", v)
	case "":
		// 连账户子公司都推不出来:只给数字 + 明确提示,不冒充任何币种
		return fmt.Sprintf("%.2f/月(币种未知)", v)
	default:
		return fmt.Sprintf("%.2f %s/月", v, strings.ToUpper(currency))
	}
}

// verifyPriceAvailable 对应 Python: _verify_price_available
// 返回 (是否可下单, 失败原因)
func (m *Monitor) verifyPriceAvailable(planCode, datacenter string, configInfo map[string]interface{}) (bool, string) {
	options := optionsFromConfig(configInfo)

	url := "http://127.0.0.1:" + m.state.Port + "/api/internal/monitor/price"
	body, _ := json.Marshal(map[string]interface{}{
		"account_id": accountIDFromConfig(configInfo),
		"plan_code":  planCode,
		"datacenter": datacenter,
		"options":    options,
	})
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		errMsg := "价格校验API请求失败: " + err.Error()
		m.state.Logger.Debug(fmt.Sprintf("价格校验API请求失败: %s@%s - %s", planCode, datacenter, err.Error()), "monitor")
		return false, errMsg
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return false, "价格校验API响应解析失败"
	}

	success, _ := result["success"].(bool)
	if !success {
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			errMsg = "未知错误"
		}
		m.state.Logger.Debug(fmt.Sprintf("价格校验失败: %s@%s - %s", planCode, datacenter, errMsg), "monitor")
		return false, errMsg
	}

	// degraded = 价格算出来了,但购物车没配成用户要的样子(dedicated_os / region 没设上)。
	// 同样的配置在 purchase.go 是 fail-fast、在 quick_order 是 400 拒绝入队,
	// 这里若只看 success 就会"发有货通知 + 触发自动下单",然后订单静默创建失败,
	// 用户只看到告警、拿不到机器。校验闸门必须跟下单闸门同口径。
	if isDegraded, _ := result["degraded"].(bool); isDegraded {
		reason, _ := result["degradedReason"].(string)
		if reason == "" {
			reason = "购物车必填配置未设置成功"
		}
		errMsg := "询价结果降级，无法下单：" + reason
		m.state.Logger.Debug(fmt.Sprintf("价格校验失败: %s@%s - %s", planCode, datacenter, errMsg), "monitor")
		return false, errMsg
	}

	priceRaw, ok := result["price"]
	if !ok || priceRaw == nil {
		m.state.Logger.Debug(fmt.Sprintf("价格校验失败: %s@%s - price字段缺失", planCode, datacenter), "monitor")
		return false, "price字段缺失"
	}
	priceInfo, ok := priceRaw.(map[string]interface{})
	if !ok {
		m.state.Logger.Debug(fmt.Sprintf("价格校验失败: %s@%s - price字段类型错误", planCode, datacenter), "monitor")
		return false, "price字段类型错误"
	}
	prices, ok := priceInfo["prices"].(map[string]interface{})
	if !ok {
		m.state.Logger.Debug(fmt.Sprintf("价格校验失败: %s@%s - prices字段缺失或类型错误", planCode, datacenter), "monitor")
		return false, "prices字段缺失或类型错误"
	}
	withTax := prices["withTax"]
	if withTax == nil {
		errMsg := "withTax无效(<nil>)"
		m.state.Logger.Debug(fmt.Sprintf("价格校验失败: %s@%s - %s", planCode, datacenter, errMsg), "monitor")
		return false, errMsg
	}
	if v, ok := numconv.ToFloat64(withTax); ok {
		if v == 0 {
			m.state.Logger.Debug(fmt.Sprintf("价格校验失败: %s@%s - withTax无效(0)", planCode, datacenter), "monitor")
			return false, "withTax无效(0)"
		}
	}
	m.state.Logger.Debug(fmt.Sprintf("价格校验通过: %s@%s - 含税价格: %v", planCode, datacenter, withTax), "monitor")
	return true, ""
}

// GetPriceInfoText 对应 Python: _get_price_info
func (m *Monitor) GetPriceInfoText(planCode, datacenter string, configInfo map[string]interface{}) string {
	options := optionsFromConfig(configInfo)

	m.state.Logger.Debug(fmt.Sprintf("开始获取价格: plan_code=%s, datacenter=%s, options=%v",
		planCode, datacenter, options), "monitor")

	url := "http://127.0.0.1:" + m.state.Port + "/api/internal/monitor/price"
	body, _ := json.Marshal(map[string]interface{}{
		"account_id": accountIDFromConfig(configInfo),
		"plan_code":  planCode,
		"datacenter": datacenter,
		"options":    options,
	})
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		m.state.Logger.Warn("价格API请求失败: "+err.Error(), "monitor")
		return ""
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return ""
	}
	if ok, _ := result["success"].(bool); !ok {
		errMsg, _ := result["error"].(string)
		m.state.Logger.Warn("价格获取失败: "+errMsg, "monitor")
		return ""
	}
	priceInfo, _ := result["price"].(map[string]interface{})
	if priceInfo == nil {
		return ""
	}
	prices, _ := priceInfo["prices"].(map[string]interface{})
	if prices == nil {
		return ""
	}
	withTaxRaw, ok := prices["withTax"]
	if !ok || withTaxRaw == nil {
		m.state.Logger.Warn("价格获取成功但withTax为None", "monitor")
		return ""
	}
	accountID := accountIDFromConfig(configInfo)
	subsidiary := m.subsidiaryOfPricingAccount(accountID)
	currency, _ := prices["currencyCode"].(string)
	switch {
	case currency == "":
		// OVH 没回币种时按询价账户的子公司兜底,而不是一律当欧元:
		// 同一条通知在美区账户下是 USD、加区账户下是 CAD,印成 "€" 会误导下单决策。
		currency = currencyForSubsidiary(subsidiary)
		m.state.Logger.Debug(fmt.Sprintf("价格接口未返回 currencyCode,按账户(子公司 %s)兜底为 %q",
			subsidiary, currency), "monitor")
	default:
		// 币种是"这次询价到底落在哪个大区"的免费探针:用美区账户询价却回了 EUR,
		// 说明询价账户和查库存的账户不是同一个(或账户 zone 配错了)。
		// 这种错配的表现只是"通知里的价格偏低/偏高",不报错,不查一下根本发现不了。
		if want := currencyForSubsidiary(subsidiary); want != "" && !strings.EqualFold(want, currency) {
			m.state.Logger.Warn(fmt.Sprintf(
				"询价币种与账户子公司不符: %s@%s 返回 %s,而询价账户(子公司 %s)应为 %s —— 请检查账户 zone / endpoint 配置",
				planCode, datacenter, strings.ToUpper(currency), subsidiary, want), "monitor")
		}
	}
	if v, ok := numconv.ToFloat64(withTaxRaw); ok {
		text := formatMoney(currency, v)
		m.state.Logger.Debug("价格获取成功: "+text, "monitor")
		return text
	}
	return ""
}

// getPriceWithTimeout 模拟 Python 中 30 秒超时
func (m *Monitor) getPriceWithTimeout(planCode, datacenter string, configInfo map[string]interface{}, timeout time.Duration) (string, string) {
	type res struct {
		text   string
		errMsg string
	}
	ch := make(chan res, 1)
	start := time.Now()
	go func() {
		text := m.GetPriceInfoText(planCode, datacenter, configInfo)
		ch <- res{text: text}
	}()
	select {
	case r := <-ch:
		if r.text == "" {
			elapsed := time.Since(start).Seconds()
			return "", fmt.Sprintf("价格接口未返回结果（耗时%.1f秒）", elapsed)
		}
		return r.text, ""
	case <-time.After(timeout):
		elapsed := time.Since(start).Seconds()
		errMsg := fmt.Sprintf("价格接口超时（等待%.1f秒）", elapsed)
		m.state.Logger.Warn("价格获取超时，发送不带价格的通知。后台请求将继续运行直到完成。", "monitor")
		return "", errMsg
	}
}
