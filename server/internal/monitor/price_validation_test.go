package monitor

import (
	"testing"
	"time"

	"github.com/ovh-buy/server/internal/price"
)

func TestPriceGateRejectsInvalidAmounts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value interface{}
		valid bool
	}{
		{"number", 12.5, true}, {"numeric string", "12.5", true},
		{"zero", 0, false}, {"negative", -1, false}, {"missing", nil, false},
		{"not numeric", "pending", false}, {"NaN", "NaN", false}, {"infinity", "+Inf", false},
		{"object", map[string]int{"value": 10}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := renderTestMonitor(t)
			m.priceFn = func(string, string, string, []string, string) price.Result {
				return price.Result{Success: true, Price: &price.PriceInfo{Prices: map[string]interface{}{"withTax": tc.value}}}
			}
			if valid, _, _ := m.verifyPriceAvailable("plan", "gra", nil); valid != tc.valid {
				t.Fatalf("valid=%t, want %t", valid, tc.valid)
			}
		})
	}
}

// 询价暂时失败或超时:返回「没验成」而不是「不可下单」;明确的业务失败仍是不可下单。
func TestPriceGateSeparatesTransientFromDefiniteFailures(t *testing.T) {
	m := renderTestMonitor(t)
	m.priceFn = func(string, string, string, []string, string) price.Result {
		return price.Result{Error: "429 Too Many Requests", Transient: true}
	}
	if ok, _, transient := m.verifyPriceAvailable("plan", "gra", nil); ok || !transient {
		t.Fatalf("429 must be reported as transient: ok=%t transient=%t", ok, transient)
	}
	m.priceFn = func(string, string, string, []string, string) price.Result {
		return price.Result{Error: "该配置在指定数据中心不可用"}
	}
	if ok, _, transient := m.verifyPriceAvailable("plan", "gra", nil); ok || transient {
		t.Fatal("a definite rejection must not be transient")
	}
	release := make(chan struct{})
	defer close(release)
	m.priceFn = func(string, string, string, []string, string) price.Result {
		<-release
		return price.Result{Success: true}
	}
	start := time.Now()
	result, transient := m.lookupPrice("plan", "gra", nil, 50*time.Millisecond)
	if !transient || result["success"] == true || time.Since(start) > time.Second {
		t.Fatalf("slow pricing must time out as transient: %v %t", result, transient)
	}
}
