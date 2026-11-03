// Package netfp 按账户构造出站 HTTP 客户端：代理隔离 + 可选的指纹差异。
//
// 为什么需要按账户隔离出口 IP：
// OVH 的限流是按来源 IP 算的，多个账户共用一个出口时，一个账户被限流会把
// 其它账户一起拖下水 —— 而这恰好发生在补货那一刻，也就是唯一要紧的时刻。
//
// **绝不静默回退直连。** 配了代理就必须走代理：代理连不上时请求要失败，
// 而不是"那就直连吧"。后者的表现是一切正常、隔离却已经没了，而你无从察觉。
// 这是这个包最重要的一条约束。
package netfp

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// Profile 一套出站指纹。
//
// ⚠️ 关于 TLS 指纹能做到什么程度，必须说清楚：
//
// Go 标准库**不允许**控制 JA3 的主要构成要素 —— crypto/tls 的文档原文：
//   - CipherSuites: "The order of the list is ignored. Note that TLS 1.3
//     ciphersuites are not configurable."
//   - CurvePreferences: "The order of the list is ignored."
//
// 也就是说套件顺序、TLS 1.3 套件、扩展顺序、GREASE 这些都定死在 Go 里，
// 所有 Go 程序的 ClientHello 长得几乎一样。
//
// 标准库能真正拉开差异的只有这几项，本类型也只承诺这几项：
//   - TLS 版本区间（1.2 vs 1.3 是很明显的差别）
//   - ALPN / 是否走 HTTP2（h2 与 http/1.1 的握手截然不同）
//   - 提供哪些 TLS 1.2 套件与曲线（集合层面，不是顺序）
//   - User-Agent / Accept-Language / Accept-Encoding
//
// 要做到真正不同的 JA3（模仿 Chrome/Firefox 的完整 ClientHello），
// 必须引入 uTLS 重写握手，那是另一件事，不在本包范围内。
type Profile struct {
	Name string
	// UserAgent 空 = 不覆盖（用 Go 默认）
	UserAgent string
	// AcceptLanguage 空 = 不发这个头
	AcceptLanguage string
	// HTTP2 是否允许协商 h2。false 则只发 http/1.1 ——
	// 这是标准库下最明显的一处握手差异。
	HTTP2 bool
	// MinTLS / MaxTLS 为 0 时用 Go 默认
	MinTLS uint16
	MaxTLS uint16
	// CipherSuites 只影响 TLS 1.2（Go 会忽略顺序，且 1.3 不可配）
	CipherSuites []uint16
	// Curves 同样只影响集合，不影响顺序
	Curves []tls.CurveID
}

// Profiles 预置的几套。名字进数据库，所以不要随便改。
var Profiles = map[string]Profile{
	// default：完全不动 Go 的默认行为。不配指纹时用它。
	"default": {
		Name:  "default",
		HTTP2: true,
	},
	// chrome-like：现代浏览器的常见取向 —— 只用 TLS 1.2+、允许 h2。
	// 注意这只是"取向相近"，JA3 与真实 Chrome 并不相同（见上面的说明）。
	"chrome-like": {
		Name:           "chrome-like",
		UserAgent:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		AcceptLanguage: "en-US,en;q=0.9",
		HTTP2:          true,
		MinTLS:         tls.VersionTLS12,
		MaxTLS:         tls.VersionTLS13,
	},
	// firefox-like：同上，取向不同。
	"firefox-like": {
		Name:           "firefox-like",
		UserAgent:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0",
		AcceptLanguage: "en-US,en;q=0.5",
		HTTP2:          true,
		MinTLS:         tls.VersionTLS12,
		MaxTLS:         tls.VersionTLS13,
		Curves:         []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384, tls.CurveP521},
	},
	// legacy-http1：强制 TLS 1.2 + 只发 http/1.1。
	// 和上面几个在握手层面差别最大的一套（ALPN 与版本都不同）。
	"legacy-http1": {
		Name:      "legacy-http1",
		UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.6 Safari/605.1.15",
		HTTP2:     false,
		MinTLS:    tls.VersionTLS12,
		MaxTLS:    tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
	},
}

// ProfileNames 供前端下拉用，顺序固定。
func ProfileNames() []string {
	return []string{"default", "chrome-like", "firefox-like", "legacy-http1"}
}

// LookupProfile 取一套指纹；名字不认识时退回 default 并说明。
func LookupProfile(name string) (Profile, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Profiles["default"], ""
	}
	p, ok := Profiles[name]
	if !ok {
		return Profiles["default"], fmt.Sprintf("不认识的指纹配置 %q，已按 default 处理", name)
	}
	return p, ""
}

// ValidateProxyURL 检查代理地址的形状。空串 = 直连，合法。
//
// 在保存那一刻就挡下来：放过去的话，用户要等到真有货那一刻才发现出口不对，
// 而那正是唯一不能出错的时刻。
func ValidateProxyURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("代理地址解析失败: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("不支持的代理协议 %q，只支持 http / https / socks5 / socks5h", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("代理地址缺少主机名")
	}
	if u.Port() == "" {
		return fmt.Errorf("代理地址缺少端口")
	}
	return nil
}

// ScrubProxyURL 把代理地址里的密码抹掉，用于日志和回显。
//
// 代理串常带 user:pass，原样打进日志就是把凭据写进了磁盘和终端。
func ScrubProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "(无法解析的代理地址)"
	}
	if u.User != nil {
		if _, hasPwd := u.User.Password(); hasPwd {
			u.User = url.UserPassword(u.User.Username(), "***")
		}
	}
	return u.String()
}

// Options 构造一个客户端需要的全部输入。
type Options struct {
	ProxyURL string
	Profile  Profile
	Timeout  time.Duration
	// OnProxyError 代理层面失败时回调(同步)。用来停任务 + 通知。
	// 只在配了代理时才可能触发。
	OnProxyError func(error)
}

// Client 构造出站 HTTP 客户端。
//
// 配了代理却建不出来时返回错误 —— **不退回直连**。
// 调用方拿到错误应当让这次操作失败，而不是"那就直连吧"：
// 后者会让隔离在用户毫不知情的情况下失效。
func Client(o Options) (*http.Client, error) {
	tr, err := Transport(o)
	if err != nil {
		return nil, err
	}
	timeout := o.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{Transport: tr, Timeout: timeout}, nil
}

// Transport 构造 RoundTripper。
func Transport(o Options) (http.RoundTripper, error) {
	tlsCfg := &tls.Config{
		MinVersion:         o.Profile.MinTLS,
		MaxVersion:         o.Profile.MaxTLS,
		ClientSessionCache: tls.NewLRUClientSessionCache(128),
	}
	if tlsCfg.MinVersion == 0 {
		// 不给默认值的话 Go 会允许 TLS 1.0/1.1;明确抬到 1.2
		tlsCfg.MinVersion = tls.VersionTLS12
	}
	if len(o.Profile.CipherSuites) > 0 {
		tlsCfg.CipherSuites = o.Profile.CipherSuites
	}
	if len(o.Profile.Curves) > 0 {
		tlsCfg.CurvePreferences = o.Profile.Curves
	}
	if !o.Profile.HTTP2 {
		// 只发 http/1.1。ForceAttemptHTTP2=false 还不够 ——
		// 必须同时把 ALPN 写死,否则 Go 仍可能把 h2 放进 ClientHello。
		tlsCfg.NextProtos = []string{"http/1.1"}
	}

	base := &http.Transport{
		TLSClientConfig:       tlsCfg,
		ForceAttemptHTTP2:     o.Profile.HTTP2,
		MaxIdleConns:          128,
		MaxIdleConnsPerHost:   64,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	raw := strings.TrimSpace(o.ProxyURL)
	if raw == "" {
		return wrap(base, o.Profile), nil
	}
	if err := ValidateProxyURL(raw); err != nil {
		return nil, err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("代理地址解析失败: %w", err)
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		// http 代理:CONNECT 隧道,由 Transport 自己处理
		base.Proxy = http.ProxyURL(u)
	case "socks5", "socks5h":
		// socks5h 与 socks5 的区别是域名由谁解析。x/net/proxy 的 SOCKS5
		// 拨号器本来就是把地址原样交给代理去解 —— 也就是 socks5h 的语义,
		// 这正是我们要的:本地不做 DNS,出口才是真正的出口,
		// 否则 DNS 查询仍然从本机发出,IP 隔离就漏了一半。
		var auth *proxy.Auth
		if u.User != nil {
			pwd, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pwd}
		}
		d, derr := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
		if derr != nil {
			return nil, fmt.Errorf("建立 SOCKS5 拨号器失败: %w", derr)
		}
		ctxDialer, ok := d.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("SOCKS5 拨号器不支持带 context 的拨号")
		}
		base.DialContext = ctxDialer.DialContext
		// 明确不设 base.Proxy:SOCKS 已经在拨号层接管了
	}
	// 配了代理才包这一层:直连时"连不上"就是 OVH 那边的事,不该被当成代理故障
	return &proxyAwareTransport{
		base:    wrap(base, o.Profile),
		proxy:   ScrubProxyURL(raw),
		onError: o.OnProxyError,
	}, nil
}

// headerTransport 给每个请求补上指纹相关的头。
//
// 只在请求自己没设过时才补 —— 调用方显式设的头优先。
type headerTransport struct {
	base http.RoundTripper
	p    Profile
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// 不能改调用方传进来的 req:RoundTripper 的契约要求不修改入参
	r := req.Clone(req.Context())
	if t.p.UserAgent != "" && r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", t.p.UserAgent)
	}
	if t.p.AcceptLanguage != "" && r.Header.Get("Accept-Language") == "" {
		r.Header.Set("Accept-Language", t.p.AcceptLanguage)
	}
	return t.base.RoundTrip(r)
}

func wrap(base http.RoundTripper, p Profile) http.RoundTripper {
	if p.UserAgent == "" && p.AcceptLanguage == "" {
		return base
	}
	return &headerTransport{base: base, p: p}
}

// EgressIP 通过给定配置查一次真实出口 IP。
//
// 这是用户唯一能自己确认"隔离到底生效没有"的手段：
// 两个账户查出来是同一个 IP，就说明配置没起作用 —— 而这件事
// 光看配置界面是看不出来的。
func EgressIP(o Options) (string, error) {
	if o.Timeout == 0 {
		o.Timeout = 15 * time.Second
	}
	cli, err := Client(o)
	if err != nil {
		return "", err
	}
	// 用 OVH 自己的站点会拿不到出口 IP,所以用一个只回 IP 的公共端点。
	// 走的是和业务请求完全相同的 transport,查出来的才是真实出口。
	req, _ := http.NewRequest(http.MethodGet, "https://api.ipify.org?format=text", nil)
	resp, err := cli.Do(req)
	if err != nil {
		return "", fmt.Errorf("通过该代理访问外网失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("查询出口 IP 返回 HTTP %d", resp.StatusCode)
	}
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	ip := strings.TrimSpace(string(buf[:n]))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("返回的不是合法 IP: %q", ip)
	}
	return ip, nil
}

// ProxyError 一次因为代理本身不通而失败的请求。
//
// 必须和"OVH 那边返回了错误"分开：后者是业务结果，重试有意义；
// 前者说明这个账户的出口断了，继续重试只会一直失败，而且**每一次重试都可能
// 在用户不知情的情况下走了别的路**。所以识别出来之后要停任务、要告诉用户。
type ProxyError struct {
	Proxy string // 已打码
	Err   error
}

func (e *ProxyError) Error() string {
	return fmt.Sprintf("出站代理不可用(%s): %v", e.Proxy, e.Err)
}
func (e *ProxyError) Unwrap() error { return e.Err }

// IsProxyError 这个错误是不是代理层面的。
func IsProxyError(err error) bool {
	var pe *ProxyError
	return errors.As(err, &pe)
}

// proxyAwareTransport 把代理层面的失败标出来，并回调通知。
//
// 为什么要在 transport 这一层判而不是在调用点：只有这里知道"这个请求本来
// 应该走代理"。到了调用点，一个 dial 失败和 OVH 超时长得一模一样。
type proxyAwareTransport struct {
	base    http.RoundTripper
	proxy   string // 已打码，只用于错误信息
	onError func(error)
}

func (t *proxyAwareTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err == nil {
		return resp, nil
	}
	if !looksLikeProxyFailure(err) {
		return nil, err
	}
	pe := &ProxyError{Proxy: t.proxy, Err: err}
	if t.onError != nil {
		t.onError(pe)
	}
	return nil, pe
}

// looksLikeProxyFailure 判断一个传输层错误是不是"代理没通"。
//
// 判据必须保守：误判成代理故障会把正常任务停掉。
// 只认那些明确指向代理握手/连接本身的信号；
// 目标站点的 TLS 错误、HTTP 状态码一律不算。
func looksLikeProxyFailure(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, needle := range []string{
		"proxyconnect",           // net/http 的 CONNECT 隧道失败
		"socks connect",          // x/net/proxy 的 SOCKS5 失败
		"unknown socks",          // SOCKS 协议错
		"socks5",                 // 其余 socks 相关
		"proxy authentication",   // 407
		"gateway timeout",        //
		"bad gateway",            //
		"connection refused",     // 拨不通代理端口
		"no such host",           // 代理域名解析不了
		"i/o timeout",            // 拨号超时
		"network is unreachable", //
		"connection reset by peer",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// sharedClient 给"公开、按子公司缓存、跨账户共享"的那些请求用的客户端。
//
// 这类请求（公开目录、区域探测）不带凭据、结果对所有账户是同一份，
// 所以**按账户隔离在架构上没有意义** —— 缓存本来就是共用的。
//
// 但它们仍然会暴露本机真实 IP。所以走一个统一的出口：有默认代理就用它，
// 没有就直连。这不是按账户隔离，也不该被当成按账户隔离。
var (
	sharedMu     sync.RWMutex
	sharedProxy  string
	sharedClient *http.Client
)

// SetSharedProxy 设置公开请求统一走的代理（一般是默认账户那个）。
// 传空串 = 直连。
func SetSharedProxy(proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		if err := ValidateProxyURL(proxyURL); err != nil {
			return err
		}
	}
	cli, err := Client(Options{ProxyURL: proxyURL, Profile: Profiles["default"], Timeout: 60 * time.Second})
	if err != nil {
		return err
	}
	sharedMu.Lock()
	sharedProxy, sharedClient = proxyURL, cli
	sharedMu.Unlock()
	return nil
}

// Shared 取公开请求用的客户端。没设过就是直连。
func Shared(timeout time.Duration) *http.Client {
	sharedMu.RLock()
	cli := sharedClient
	sharedMu.RUnlock()
	if cli == nil {
		return &http.Client{Timeout: timeout}
	}
	// 复用 transport（连接池、代理配置），只换超时
	return &http.Client{Transport: cli.Transport, Timeout: timeout}
}

// SharedProxy 当前公开请求走的代理（已打码），空 = 直连。
func SharedProxy() string {
	sharedMu.RLock()
	defer sharedMu.RUnlock()
	return ScrubProxyURL(sharedProxy)
}

// ── 连通性与延迟检测 ──────────────────────────────────────────────────────

// Probe 一个探测目标的结果。
type Probe struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Status 拿到的 HTTP 状态码。**任何**响应都说明链路是通的 ——
	// 404/302 只代表那个路径不存在或要跳转，不代表代理有问题。
	Status int `json:"status,omitempty"`
	// MinMS / AvgMS 多次采样的最小值与平均值（毫秒）。
	//
	// 为什么要两个：单次采样噪声很大，而抢购真正在意的是**稳定的**延迟。
	// min 是这条链路的最好情况，avg 与 min 差得远就说明抖动大 ——
	// 抖动大的代理在补货那一刻会不定时地慢一拍，而那一拍就决定抢不抢得到。
	MinMS int64  `json:"minMs,omitempty"`
	AvgMS int64  `json:"avgMs,omitempty"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// probeSamples 每个目标采几次。3 次足够看出抖动，又不会把检测本身拖成十几秒。
const probeSamples = 3

// ProbeTarget 探测一个地址：采样 probeSamples 次，返回最小与平均耗时。
//
// 判定「通」的标准是**拿到了任何 HTTP 响应**，而不是 200 ——
// 用 404 或 302 判失败会把「链路正常但这个路径不存在」误报成代理故障。
func ProbeTarget(o Options, name, url string) Probe {
	p := Probe{Name: name, URL: url}
	if o.Timeout == 0 {
		o.Timeout = 10 * time.Second
	}
	cli, err := Client(o)
	if err != nil {
		p.Error = err.Error()
		return p
	}
	var total time.Duration
	var min time.Duration
	ok := 0
	for i := 0; i < probeSamples; i++ {
		req, rerr := http.NewRequest(http.MethodGet, url, nil)
		if rerr != nil {
			p.Error = rerr.Error()
			return p
		}
		start := time.Now()
		resp, derr := cli.Do(req)
		elapsed := time.Since(start)
		if derr != nil {
			// 第一次就失败通常就是不通了，但仍然把后面几次跑完 ——
			// 偶发一次超时和"完全不通"是两回事，而用户要据此判断要不要换代理。
			if p.Error == "" {
				p.Error = derr.Error()
			}
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		p.Status = resp.StatusCode
		ok++
		total += elapsed
		if min == 0 || elapsed < min {
			min = elapsed
		}
	}
	if ok == 0 {
		return p
	}
	p.OK = true
	p.MinMS = min.Milliseconds()
	p.AvgMS = (total / time.Duration(ok)).Milliseconds()
	// 有成功的采样就不要再挂着那条偶发错误了，否则界面上"通了但红着"
	if ok == probeSamples {
		p.Error = ""
	}
	return p
}

// ProbeAll 并发探测一组目标。
//
// 并发而不是串行：串行 6 个目标 × 3 次采样 × 1 秒 = 十几秒，
// 用户会以为卡死了。并发之间互不影响延迟读数（各自独立连接）。
func ProbeAll(o Options, targets []ProbeSpec) []Probe {
	out := make([]Probe, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t ProbeSpec) {
			defer wg.Done()
			out[i] = ProbeTarget(o, t.Name, t.URL)
		}(i, t)
	}
	wg.Wait()
	return out
}

// ProbeSpec 一个待探测的目标。
type ProbeSpec struct {
	Name string
	URL  string
}
