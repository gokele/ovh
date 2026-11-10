package price

import (
	"math"
	"testing"
)

// 复现用户通知:summary 合计 €37.98(含税),其中安装费 €23.36(含税),月费应为 €14.62。
func TestSplitRecurringAndSetup(t *testing.T) {
	near := func(got interface{}, want float64) bool {
		f, ok := got.(float64)
		return ok && math.Abs(f-want) <= 0.01
	}
	summary := func(duration, install float64) map[string]interface{} {
		return map[string]interface{}{"details": []interface{}{
			map[string]interface{}{"detailType": "DURATION", "totalPrice": map[string]interface{}{"value": duration, "currencyCode": "EUR"}},
			map[string]interface{}{"detailType": "INSTALLATION", "totalPrice": map[string]interface{}{"value": install, "currencyCode": "EUR"}},
		}}
	}
	// 明细不含税(常见形态)
	info := &PriceInfo{Prices: map[string]interface{}{"withTax": 37.98, "withoutTax": 31.65}}
	splitRecurringAndSetup(info, summary(12.18, 19.47))
	if !near(info.Prices["recurringWithTax"], 14.62) || !near(info.Prices["setupWithTax"], 23.36) {
		t.Fatalf("tax-exclusive lines split wrong: %v", info.Prices)
	}
	if info.Prices["withTax"] != 37.98 {
		t.Fatal("the total must stay intact for the price gate")
	}
	// 明细含税
	info = &PriceInfo{Prices: map[string]interface{}{"withTax": 37.98, "withoutTax": 31.65}}
	splitRecurringAndSetup(info, summary(14.62, 23.36))
	if !near(info.Prices["recurringWithTax"], 14.62) || !near(info.Prices["setupWithTax"], 23.36) {
		t.Fatalf("tax-inclusive lines split wrong: %v", info.Prices)
	}
	// 没有安装费:周期费就是合计
	info = &PriceInfo{Prices: map[string]interface{}{"withTax": 14.62, "withoutTax": 12.18}}
	splitRecurringAndSetup(info, map[string]interface{}{"details": []interface{}{
		map[string]interface{}{"detailType": "DURATION", "totalPrice": map[string]interface{}{"value": 12.18}},
	}})
	if !near(info.Prices["recurringWithTax"], 14.62) || info.Prices["setupWithTax"] != nil {
		t.Fatalf("no-setup case wrong: %v", info.Prices)
	}
}
