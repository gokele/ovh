package ovh

import (
	"errors"
	"fmt"
	"net"
	"strings"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// Explain 把一个 OVH 调用的错误变成用户能照着做的中文。
//
// 为什么需要:handler 里有近两百处直接把 err.Error() 塞进响应,前端原样显示。
// 用户看到的是:
//
//	OVHcloud API error (status code 403): Client::Forbidden: "This call has not been granted" (X-OVH-Query-Id: EU.ext-1.x)
//
// 这句话里唯一有用的信息是"建 consumer key 时权限没给全",但它没写在任何地方。
// 用户只会看到一串英文然后来问"为什么一直失败"。
//
// 两条原则:
//  1. 先说**该怎么办**,不是先说发生了什么。
//  2. 原文一定保留。翻译永远可能对不上具体场景,把原文去掉就等于把排查路径也砍了。
//
// 不是 OVH API 错误的(JSON 解析、数据库、上下文取消)原样返回 ——
// 这个函数是可以无脑套在任何 error 上的。
func Explain(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *ovhsdk.APIError
	if !errors.As(err, &apiErr) {
		// 网络层单独认一下:这是"你的机器连不上 OVH",和"OVH 拒绝了你"
		// 完全是两件事,给的下一步也不一样。
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return "连接 OVH 超时。检查本机网络或代理设置;抢购高峰期 OVH 也可能变慢。原文:" + err.Error()
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return "解析不了 OVH 的域名,基本可以确定是本机 DNS 或网络问题。原文:" + err.Error()
		}
		return err.Error()
	}

	hint := hintFor(apiErr)
	msg := strings.TrimSpace(apiErr.Message)
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", apiErr.Code)
	}
	out := fmt.Sprintf("%s(OVH %d)。原文:%s", hint, apiErr.Code, msg)
	if apiErr.QueryID != "" {
		// QueryID 是找 OVH 客服时唯一有用的东西,别丢
		out += "(查询号 " + apiErr.QueryID + ")"
	}
	return out
}

// hintFor 按状态码 + 错误类给一句"该怎么办"。
func hintFor(e *ovhsdk.APIError) string {
	lower := strings.ToLower(e.Message)
	switch e.Code {
	case 401:
		return "账户凭据无效或已过期。去「账户」页把这个账户的 consumer key 重新生成一次," +
			"并**点开 OVH 返回的授权链接确认**——没点这一步的 key 是不能用的"
	case 403:
		if strings.Contains(lower, "not been granted") || strings.Contains(lower, "granted") {
			return "这个 consumer key 没有调用该接口的权限。生成 key 时的权限规则要覆盖用到的路径" +
				"(最省事的是 GET/POST/PUT/DELETE 各给一条 /*),改完要重新授权"
		}
		if strings.Contains(lower, "ip") {
			return "OVH 拒绝了来自当前 IP 的调用。建 key 时如果限制过 IP,换网络或代理后就会被挡"
		}
		return "OVH 拒绝了这次调用(权限不足)。多半是 consumer key 的权限规则没覆盖这个接口"
	case 404:
		return "OVH 说这个资源不存在。最常见的原因是**选错了账户**——" +
			"EU / US / CA 三个站点各自独立,在 A 账户下查 B 账户的服务器一律是 404"
	case 409:
		return "和已有的操作冲突了。同一台机器上通常只能有一个进行中的任务,等上一个跑完再试"
	case 429:
		return "请求太密,被 OVH 限流了。把重试间隔调大一点(设置 → 抢购)," +
			"多个任务盯同一个账户时尤其容易触发"
	case 400:
		return "OVH 认为这次请求的参数不对"
	}
	switch {
	case e.Code >= 500:
		return "OVH 服务端出错了,不是你这边的问题。过几分钟再试"
	case e.Code >= 400:
		// 460 / 462 这类是 OVH 自定义码,含义随接口变,不硬编
		return "OVH 拒绝了这次操作"
	}
	return "调用 OVH 失败"
}
