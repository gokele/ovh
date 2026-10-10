package ovh

import (
	"errors"
	"strings"
	"testing"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// 每一条都必须给出"下一步做什么",而不只是把英文翻译一遍。
// 401/403 是新手最容易卡住的两个:key 建完没点授权链接、权限规则没给全,
// 两者的原始英文都不包含解决办法。
func TestExplainGivesActionableChineseHint(t *testing.T) {
	cases := []struct {
		code    int
		class   string
		message string
		want    string // 必须出现的关键词
	}{
		{401, "Client::Forbidden", "This credential is not valid", "重新生成"},
		{403, "Client::Forbidden", "This call has not been granted", "权限规则"},
		{404, "Client::NotFound", "This service does not exist", "选错了账户"},
		{409, "Client::Conflict", "Task already in progress", "冲突"},
		{429, "Client::TooManyRequests", "Too many requests", "重试间隔"},
		{500, "Server::InternalServerError", "Internal error", "OVH 服务端"},
	}
	for _, c := range cases {
		got := Explain(&ovhsdk.APIError{Code: c.code, Class: c.class, Message: c.message, QueryID: "EU.ext-1.abc"})
		if !strings.Contains(got, c.want) {
			t.Errorf("HTTP %d: 少了关键提示 %q\n  实际: %s", c.code, c.want, got)
		}
		// 原文必须保留 —— 翻译不可能覆盖所有场景,丢了原文就没法排查
		if !strings.Contains(got, c.message) {
			t.Errorf("HTTP %d: 原始报错被丢掉了: %s", c.code, got)
		}
		// 查询号是找 OVH 客服时唯一有用的东西
		if !strings.Contains(got, "EU.ext-1.abc") {
			t.Errorf("HTTP %d: 查询号被丢掉了: %s", c.code, got)
		}
	}
}

// 非 OVH 错误必须原样返回 —— 这个函数要能无脑套在任何 error 上,
// 套错了不能把数据库错误、JSON 解析错误改得面目全非。
func TestExplainLeavesNonOVHErrorsAlone(t *testing.T) {
	for _, in := range []string{
		"sql: no rows in result set",
		"invalid character 'x' looking for beginning of value",
		"context canceled",
	} {
		if got := Explain(errors.New(in)); got != in {
			t.Errorf("非 OVH 错误被改写了:\n  输入: %s\n  输出: %s", in, got)
		}
	}
	if got := Explain(nil); got != "" {
		t.Errorf("nil 应当返回空串,实际 %q", got)
	}
}

// 包装过的错误也要能认出来(fmt.Errorf("...: %w", apiErr))
func TestExplainUnwrapsWrappedAPIError(t *testing.T) {
	inner := &ovhsdk.APIError{Code: 429, Message: "Too many requests"}
	got := Explain(errors.Join(errors.New("查库存失败"), inner))
	if !strings.Contains(got, "重试间隔") {
		t.Errorf("包装后认不出 APIError 了: %s", got)
	}
}

// 回归:403 + "Invalid token" 不能说成权限问题。
//
// 实测线上报错(v0.1.32):接受联系人变更请求时 token 填错,OVH 回
//
//	403 Invalid token (X-OVH-Query-Id: EU.ext-5.6aa6687d...)
//
// 而 Explain 按状态码硬猜,输出「多半是 consumer key 的权限规则没覆盖这个接口」——
// 把用户指去重建 API 凭据,而真正该做的是回邮件把确认令牌抄对。
//
// schema 里这个接口的 body 参数写得很清楚:
//
//	token: "The token you received by email for this request"
//
// 也就是说这个 token 跟 API 凭据毫无关系。
func TestExplainDoesNotBlamePermissionsForInvalidToken(t *testing.T) {
	got := Explain(&ovhsdk.APIError{
		Code: 403, Class: "Client::Forbidden", Message: "Invalid token",
		QueryID: "EU.ext-5.6aa6687d",
	})
	if strings.Contains(got, "consumer key") || strings.Contains(got, "权限规则") {
		t.Errorf("把令牌无效说成了权限问题,会让用户白白重建 API 凭据:\n  %s", got)
	}
	if !strings.Contains(got, "邮箱") {
		t.Errorf("没指出这是邮件里的一次性令牌:\n  %s", got)
	}
	if !strings.Contains(got, "Invalid token") {
		t.Errorf("原文被丢了:\n  %s", got)
	}
}

// 真正的权限不足仍然要给出权限的说法 —— 上面那条修复不能把这条一起削掉
func TestExplainStillDetectsRealPermissionError(t *testing.T) {
	got := Explain(&ovhsdk.APIError{Code: 403, Message: "This call has not been granted"})
	if !strings.Contains(got, "权限规则") {
		t.Errorf("真的权限不足反而不提权限了:\n  %s", got)
	}
}

// 原文认不出来时,403 只说"看原文",不许断言原因
func TestExplainStaysVagueWhenMessageIsUnknown(t *testing.T) {
	got := Explain(&ovhsdk.APIError{Code: 403, Message: "Some unrecognised reason"})
	if strings.Contains(got, "多半是") {
		t.Errorf("对认不出的原文做了断言:\n  %s", got)
	}
	if !strings.Contains(got, "原文") {
		t.Errorf("没把用户引向原文:\n  %s", got)
	}
}

// 回归:reboot 防抖(403 + "A reboot has already been requested")不能说成权限问题。
// 实测 OVH 对重复 reboot 回 403 而不是 409,按状态码硬猜会把人带去重建 API 凭据
func TestExplainRebootAlreadyRequested(t *testing.T) {
	got := Explain(&ovhsdk.APIError{
		Code:    403,
		Class:   "Client::Forbidden",
		Message: "A reboot has already been requested",
	})
	if strings.Contains(got, "consumer key") || strings.Contains(got, "权限规则") {
		t.Fatalf("把重启防抖说成了权限问题: %s", got)
	}
	if !strings.Contains(got, "重启在进行中") {
		t.Fatalf("应说明已有重启在进行: %s", got)
	}
}

// 回归:manualPayment 参数废弃(400 + "manualPayment parameter isn't available anymore",
// 实测 2026-10 US 区 PUT /vps/{svc}/serviceInfos)。
// 官方 schema 三区都还列着这个字段 —— 文档滞后于 live,IsParamUnavailable 是
// putServiceRenew 自动降级重试的判据,认不出 = 重试永远不会发生。
func TestIsParamUnavailableManualPayment(t *testing.T) {
	err := &ovhsdk.APIError{
		Code:    400,
		Class:   "Client::BadRequest",
		Message: "[renew] manualPayment parameter isn't available anymore",
	}
	if !IsParamUnavailable(err, "manualPayment") {
		t.Fatalf("应认出 manualPayment 参数废弃: %v", err)
	}
	// 其它参数不受牵连
	if IsParamUnavailable(err, "period") {
		t.Fatal("period 不该被认成废弃参数")
	}
	// nil 与无关错误不误报
	if IsParamUnavailable(nil, "manualPayment") {
		t.Fatal("nil 不该命中")
	}
	if IsParamUnavailable(errors.New("some network error"), "manualPayment") {
		t.Fatal("无关错误不该命中")
	}
	// 兜底文案:去掉参数重试仍被拒时,要告诉用户换路径,而不是指向凭据/权限
	got := Explain(err)
	if !strings.Contains(got, "自动按新契约重试") {
		t.Fatalf("缺兜底提示: %s", got)
	}
	if strings.Contains(got, "consumer key") {
		t.Fatalf("参数废弃不应指向凭据问题: %s", got)
	}
}
