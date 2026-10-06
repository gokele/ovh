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

// hintFor 给一句"该怎么办"。
//
// **顺序很要紧:先看 OVH 原文,再看状态码。**
// 反过来做会出事 —— 实测 POST /me/task/contactChange/{id}/accept 的 token 填错时,
// OVH 回的是 403 + "Invalid token"。按状态码硬猜就会告诉用户
// 「consumer key 权限规则没覆盖这个接口」,把人指去重建 API 凭据,
// 而真正该做的是回邮件里把确认令牌抄对(或点重发邮件拿一封新的)。
// 状态码只说"哪一类拒绝",原文才说"具体哪件事",原文能认出来时以原文为准。
func hintFor(e *ovhsdk.APIError) string {
	lower := strings.ToLower(e.Message)

	// —— 先按原文认。这些说法的含义与状态码无关 ——
	switch {
	case strings.Contains(lower, "reboot has already been requested"):
		// OVH 对同一台机器的重启防抖(实测回 403 而不是 409,状态码认不出来)。
		// 它不是权限问题:已经有一个重启在跑了,这次请求要做的件事已经在发生
		return "这台服务器已经有一个重启在进行中,等它完成即可(不需要重新授权或改凭据)"
	case strings.Contains(lower, "invalid token"), strings.Contains(lower, "token is invalid"):
		return "这里的 token 不是 API 密钥,而是 OVH **发到邮箱里**的一次性确认令牌。" +
			"它填错、过期或已经用过都会这样 —— 回邮件重新抄一遍,或让 OVH 重发一封"
	case strings.Contains(lower, "not been granted"), strings.Contains(lower, "not granted"):
		return "这个 consumer key 没有调用该接口的权限。生成 key 时的权限规则要覆盖用到的路径" +
			"(最省事的是 GET/POST/PUT/DELETE 各给一条 /*),改完要重新授权"
	case strings.Contains(lower, "expired"):
		return "凭据或令牌已过期,需要重新生成"
	}

	switch e.Code {
	case 401:
		return "账户凭据无效或已过期。去「账户」页把这个账户的 consumer key 重新生成一次," +
			"并**点开 OVH 返回的授权链接确认**——没点这一步的 key 是不能用的"
	case 403:
		if strings.Contains(lower, "ip") {
			return "OVH 拒绝了来自当前 IP 的调用。建 key 时如果限制过 IP,换网络或代理后就会被挡"
		}
		// 走到这里说明原文没给出可识别的原因。不要断言"就是权限不足" ——
		// OVH 的 403 也用来表示令牌无效、状态不允许等等,断言错了会把人带偏。
		return "OVH 拒绝了这次操作,具体原因看下面的原文。" +
			"如果原文看不出名堂,常见的一种是 consumer key 的权限规则没覆盖这个接口"
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
