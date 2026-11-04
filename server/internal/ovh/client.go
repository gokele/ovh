package ovh

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/netfp"
	"github.com/ovh-buy/server/internal/types"
)

// AccountLookup 由 State 提供,根据 id 找账户:
//
//	id == ""  → 找默认账户(或第一个);适合"未指定账户时 fallback"
//	id == "x" → 精确找 x
type AccountLookup func(id string) (types.OVHAccount, bool)

func clientAccountSettings(a types.OVHAccount) types.OVHAccount {
	return types.OVHAccount{Endpoint: a.Endpoint, AppKey: a.AppKey, AppSecret: a.AppSecret,
		ConsumerKey: a.ConsumerKey, ProxyURL: a.ProxyURL, Fingerprint: a.Fingerprint}
}

func SameClientConfiguration(a, b types.OVHAccount) bool {
	return clientAccountSettings(a) == clientAccountSettings(b)
}

// Factory OVH client 工厂。按 accountID 缓存 client 实例;
// 同一账户多次取拿到同一个 client;Invalidate 失效特定账户的缓存。
//
// 不同账户即使 endpoint 相同(都 ovh-eu)也是独立 client(凭据不同),
// 缓存 key 是 accountID 不是 endpoint。
type Factory struct {
	lookup   AccountLookup
	fallback *config.Store // 兼容老 Client() 调用,等所有 callsite 迁完可移除
	// logf 可选日志钩子。指纹名不认识这类事要说出来,但 ovh 包不该依赖 logger。
	logf func(format string, args ...interface{})
	// onProxyError 代理层面失败时回调(accountID, err)。由 main 接到 proxyguard。
	onProxyError func(accountID string, err error)

	mu            sync.Mutex
	cache         map[string]*ovh.Client // accountID → client
	cacheAccounts map[string]types.OVHAccount
}

// NewFactory 构造工厂。lookup 由 State 闭包注入。
func NewFactory(cfg *config.Store, lookup AccountLookup) *Factory {
	return &Factory{
		lookup:   lookup,
		fallback: cfg,
		cache:    map[string]*ovh.Client{},
	}
}

// SetLogf 注入日志钩子(可选)。
func (f *Factory) SetLogf(fn func(format string, args ...interface{})) {
	f.mu.Lock()
	f.logf = fn
	f.mu.Unlock()
}

// SetProxyErrorHook 注入代理故障回调。必须在任何 ClientFor 之前设好 ——
// 已经建出来的 client 里捕获的是设置那一刻的值。
func (f *Factory) SetProxyErrorHook(fn func(accountID string, err error)) {
	f.mu.Lock()
	f.onProxyError = fn
	f.cache = map[string]*ovh.Client{} // 让已缓存的 client 重建,带上钩子
	f.cacheAccounts = map[string]types.OVHAccount{}
	f.mu.Unlock()
}

// ClientFor 返回指定账户的 OVH client。accountID="" 走默认账户。
// 凭据缺失 / 账户不存在返回 error;同账户重复调用复用缓存实例。
func (f *Factory) ClientFor(accountID string) (*ovh.Client, error) {
	if f.lookup == nil {
		// State 还没把 lookup 注入(理论上不会发生)→ 退到 fallback
		return f.Client()
	}
	acc, ok := f.lookup(accountID)
	if !ok {
		if accountID == "" {
			return nil, fmt.Errorf("no default OVH account configured")
		}
		return nil, fmt.Errorf("ovh account %s not found", accountID)
	}
	return f.ClientForAccount(acc)
}

// ClientForAccount preserves the account snapshot selected for a multi-step workflow.
func (f *Factory) ClientForAccount(acc types.OVHAccount) (*ovh.Client, error) {
	if acc.AppKey == "" || acc.AppSecret == "" || acc.ConsumerKey == "" {
		return nil, fmt.Errorf("ovh account %s missing credentials", acc.ID)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	// Labels and default-account selection do not change the transport or credentials.
	settings := clientAccountSettings(acc)
	if cli, ok := f.cache[acc.ID]; ok && f.cacheAccounts[acc.ID] == settings {
		return cli, nil
	}
	cli, err := ovh.NewClient(acc.Endpoint, acc.AppKey, acc.AppSecret, acc.ConsumerKey)
	if err != nil {
		return nil, err
	}

	// 按账户装出站通道:代理隔离 + 指纹。
	//
	// **配了代理却建不出来时直接失败,不退回直连。**
	// 退回直连的表现是一切正常、隔离却已经没了 —— 而 OVH 的限流是按 IP 算的,
	// 用户要等到补货那一刻被限流才会发现,那正是唯一不能出错的时刻。
	prof, warn := netfp.LookupProfile(acc.Fingerprint)
	if warn != "" && f.logf != nil {
		f.logf("账户 %s: %s", acc.Name, warn)
	}
	accID := acc.ID
	// 在锁内取出钩子并按值捕获:闭包是在请求时刻跑的,
	// 那时候没有持锁,直接读 f.onProxyError 就是数据竞争。
	hook := f.onProxyError
	httpCli, herr := netfp.Client(netfp.Options{
		ProxyURL: acc.ProxyURL,
		Profile:  prof,
		// go-ovh 默认没有超时。抢购链路上一个卡死的连接等于这一轮白等,
		// 而下一轮要等到 retryInterval 之后。
		Timeout: 60 * time.Second,
		// 代理层面的失败上报给看门狗:连续失败会暂停这个账户的任务并通知用户。
		// 注入的是回调而不是直接 import proxyguard —— 那会绕成 import 环
		// (proxyguard 依赖 app,app 依赖 ovh)。
		OnProxyError: func(e error) {
			if hook != nil {
				hook(accID, e)
			}
		},
	})
	if herr != nil {
		return nil, fmt.Errorf("账户 %s 的出站代理配置有问题(%s): %w",
			acc.Name, netfp.ScrubProxyURL(acc.ProxyURL), herr)
	}
	cli.Client = httpCli
	cli.Client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	cli.Timeout = 60 * time.Second

	f.cache[acc.ID] = cli
	if f.cacheAccounts == nil {
		f.cacheAccounts = map[string]types.OVHAccount{}
	}
	f.cacheAccounts[acc.ID] = settings
	return cli, nil
}

// Invalidate 清掉指定账户的缓存 client(更新 / 删除账户后调,避免拿到旧凭据)
func (f *Factory) Invalidate(accountID string) {
	f.mu.Lock()
	delete(f.cache, accountID)
	delete(f.cacheAccounts, accountID)
	f.mu.Unlock()
}

// InvalidateAll 清全部缓存(比如重置 OVH 配置时)
func (f *Factory) InvalidateAll() {
	f.mu.Lock()
	f.cache = map[string]*ovh.Client{}
	f.cacheAccounts = map[string]types.OVHAccount{}
	f.mu.Unlock()
}

// Client 老接口,等价于 ClientFor("")(默认账户)。
// 未迁移到 ClientFor 的旧调用站点先用它;新代码不要用这个。
//
// Deprecated: 调用方应明确传 accountID。
func (f *Factory) Client() (*ovh.Client, error) {
	// 优先走 lookup 拿默认账户
	if f.lookup != nil {
		if cli, err := f.ClientFor(""); err == nil {
			return cli, nil
		}
		// lookup 找不到任何账户,退到旧 cfg
	}
	// fallback: 从 config.Store 拿凭据(老逻辑,P2 完全迁移后可删)
	if f.fallback == nil {
		return nil, fmt.Errorf("no default OVH account configured")
	}
	c := f.fallback.Get()
	if c.AppKey == "" || c.AppSecret == "" || c.ConsumerKey == "" {
		return nil, fmt.Errorf("missing OVH API credentials")
	}
	cli, err := ovh.NewClient(c.Endpoint, c.AppKey, c.AppSecret, c.ConsumerKey)
	if err != nil {
		return nil, err
	}
	cli.Timeout = 60 * time.Second
	cli.Client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return cli, nil
}
