package ovh

import (
	"errors"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// CheckoutUncertain requires an explicit business rejection before retrying a checkout.
func CheckoutUncertain(err error) bool {
	var apiErr *ovhsdk.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code < 400 || apiErr.Code >= 500 || apiErr.Code == 408 || apiErr.Code == 429
	}
	return err != nil
}

// CheckoutNotSent 结账请求在完整写出之前就失败(代理/DNS/建连/写入错误),OVH 不可能收到这笔结账。
// 依据 httptrace.WroteRequest 是否成功回调;只要请求写出过、或拿到了任何 HTTP 响应,
// 都不算「未发出」,仍按结果不明处理。重复发送的保护错误同样不算。
func CheckoutNotSent(err error, wrote bool) bool {
	if err == nil || wrote || errors.Is(err, ErrCheckoutAlreadySent) {
		return false
	}
	var apiErr *ovhsdk.APIError
	return !errors.As(err, &apiErr)
}
