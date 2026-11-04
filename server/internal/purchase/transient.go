package purchase

import (
	"errors"
	"net"
	"strings"

	"github.com/ovh-buy/server/internal/ovh"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// IsTransient 判断一次 OVH 调用的失败是不是"过一会再试就可能成功"。
//
// 为什么要区分:队列处理器拿 Outcome.Attempted 去累加 FailureCount,
// 到 MaxRetries 就把任务置 failed。以前只要走到"向 OVH 发过请求"这一步,
// 无论失败原因一律 Attempted=true —— 包括 429。
//
// 而 429 恰恰是抢购最需要撑住的那一刻:补货瞬间所有人都在打同一个接口,
// OVH 限流是常态。MaxRetries=5、retryInterval=10s 的任务会在 **不到一分钟内**
// 被自己判死,而那一分钟正是唯一有货的窗口。用户第二天看到的是一条
// "连续 5 次下单尝试均失败"的记录,和一台被别人买走的机器。
//
// 判定为 transient 的:
//   - 429 限流
//   - 5xx(OVH 自己挂了/网关超时)
//   - 408 请求超时
//   - 传输层错误:超时、连接被重置、DNS 解析失败、EOF
//
// 不算 transient 的是 4xx 业务拒绝(参数错、无权限、无货、机型不在本区目录),
// 那些重试多少次都是同一个答案,该计数就得计数。
func IsTransient(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *ovhsdk.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Code == 429, apiErr.Code == 408:
			return true
		case apiErr.Code >= 500 && apiErr.Code <= 599:
			return true
		case apiErr.Code > 0:
			// 明确的业务级 4xx —— 重试无益
			return false
		}
	}
	// 非 APIError:多半还没拿到 HTTP 响应
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	s := strings.ToLower(err.Error())
	for _, needle := range []string{
		"connection reset",
		"connection refused",
		"no such host",
		"i/o timeout",
		"timeout",
		"eof",
		"broken pipe",
		"tls handshake",
		"too many requests",
		"service unavailable",
		"bad gateway",
		"gateway timeout",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// attemptOutcome 把一次失败包装成 Outcome。
// transient 的失败不计进 FailureCount —— 它没告诉我们"这单买不成",
// 只告诉我们"这一下没打通"。
func attemptOutcome(err error) Outcome {
	return Outcome{Attempted: !IsTransient(err)}
}

// checkoutUncertain 只有明确的业务 4xx 才证明请求被拒绝;其余错误不能安全重发结账。
func checkoutUncertain(err error) bool {
	return ovh.CheckoutUncertain(err)
}

func expiredCartError(err error) bool {
	var apiErr *ovhsdk.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Code == 404 {
		return true
	}
	message := strings.ToLower(apiErr.Message)
	return apiErr.Code == 400 && strings.Contains(message, "cart") && (strings.Contains(message, "expired") || strings.Contains(message, "invalid") || strings.Contains(message, "not found"))
}
