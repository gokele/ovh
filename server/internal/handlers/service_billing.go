package handlers

import (
	"fmt"
	"strconv"
	"strings"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// renewalValidationError 让本地能力/参数校验沿用前端稳定错误码翻译。
type renewalValidationError struct{ Code, Message string }

// Error 返回拒绝修改的原因,不会改变为其他续费方式。
func (e *renewalValidationError) Error() string { return e.Message }

// serviceBilling 读取具体服务的续费能力和当前状态,不能用账户地区推断。
type serviceBilling struct {
	Billing struct {
		Renew *struct {
			Capacities struct {
				Mode []string `json:"mode"`
			} `json:"capacities"`
			Current struct {
				Mode   string `json:"mode"`
				Period string `json:"period"`
			} `json:"current"`
		} `json:"renew"`
		Lifecycle struct {
			Current struct {
				PendingActions  []string `json:"pendingActions"`
				TerminationDate string   `json:"terminationDate"`
			} `json:"current"`
		} `json:"lifecycle"`
	} `json:"billing"`
}

// readServiceBilling 只读服务级能力,不创建续费订单或修改续费策略。
func readServiceBilling(client *ovhsdk.Client, id int64) (*serviceBilling, error) {
	var value serviceBilling
	if err := client.Get(fmt.Sprintf("/services/%d", id), &value); err != nil {
		return nil, err
	}
	return &value, nil
}

// renewalMonths 把 API 的整月/整年 ISO 8601 周期转换为既有界面的月数。
func renewalMonths(period string) int {
	if !strings.HasPrefix(period, "P") || len(period) < 3 {
		return 0
	}
	n, err := strconv.Atoi(period[1 : len(period)-1])
	if err != nil || n <= 0 {
		return 0
	}
	switch period[len(period)-1] {
	case 'M':
		return n
	case 'Y':
		if n <= int(^uint(0)>>1)/12 {
			return n * 12
		}
	}
	return 0
}

// attachRenewalState 优先显示新接口的当前模式,仅在明确能力列表缺少 manual 时禁用。
func attachRenewalState(out map[string]interface{}, value *serviceBilling) {
	renew := value.Billing.Renew
	if renew == nil {
		return
	}
	if renew.Capacities.Mode != nil {
		out["renewalModes"] = renew.Capacities.Mode
		manual := false
		for _, mode := range renew.Capacities.Mode {
			manual = manual || mode == "manual"
		}
		out["renewalManualUnsupported"] = !manual
	}
	if renew.Current.Mode == "automatic" || renew.Current.Mode == "manual" {
		out["renewalType"] = renew.Current.Mode == "automatic"
		out["renewalManualPayment"] = renew.Current.Mode == "manual"
	}
	if months := renewalMonths(renew.Current.Period); months > 0 {
		out["renewalPeriod"] = months
	}
}
