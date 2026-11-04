package ovh

import (
	"testing"

	"github.com/ovh-buy/server/internal/types"
)

func TestClientCacheChecksCurrentConfigurationWithoutInvalidation(t *testing.T) {
	account := types.OVHAccount{ID: "changed", Endpoint: "ovh-eu", Zone: "IE", AppKey: "test", AppSecret: "old", ConsumerKey: "test"}
	factory := NewFactory(nil, func(string) (types.OVHAccount, bool) { return account, true })
	old, err := factory.ClientFor(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	account.Name, account.IsDefault = "renamed", true
	if same, err := factory.ClientFor(account.ID); err != nil || same != old {
		t.Fatal("display/default changes rebuilt the transport")
	}
	account.AppSecret = "new"
	current, err := factory.ClientFor(account.ID)
	if err != nil || current == old || current.AppSecret != account.AppSecret {
		t.Fatal("stale credentials remained cached")
	}
	if again, err := factory.ClientFor(account.ID); err != nil || again != current {
		t.Fatal("unchanged configuration lost connection reuse")
	}
}

// client 是按 accountID 缓存的。改了代理如果不让缓存失效，
// 旧 client 会继续用旧出口跑，而界面上显示的是新配置 ——
// 用户以为换了代理，实际一直从原来那个 IP 出去。
//
// 这个测试盯住：改配置 → Invalidate → 再取，拿到的必须是**新的**实例。
func TestProxyChangeInvalidatesCachedClient(t *testing.T) {
	acc := types.OVHAccount{
		ID: "a1", Name: "测试", Endpoint: "ovh-eu", Zone: "IE",
		AppKey: "k", AppSecret: "s", ConsumerKey: "c",
	}
	f := NewFactory(nil, func(id string) (types.OVHAccount, bool) { return acc, true })

	c1, err := f.ClientFor("a1")
	if err != nil {
		t.Fatalf("第一次取: %v", err)
	}
	// 没改配置时必须复用，否则每次调用都新建 transport，连接池就白搭了
	c1again, _ := f.ClientFor("a1")
	if c1 != c1again {
		t.Fatal("配置没变时应当复用同一个 client")
	}

	// 直连 → 走代理
	acc.ProxyURL = "socks5://127.0.0.1:1080"
	f.Invalidate("a1")
	c2, err := f.ClientFor("a1")
	if err != nil {
		t.Fatalf("改成走代理后取: %v", err)
	}
	if c2 == c1 {
		t.Fatal("改了代理却拿到同一个 client —— 旧出口还在用")
	}
	if c2.Client == c1.Client {
		t.Fatal("底层 http.Client 没换，transport 还是旧的")
	}

	// 走代理 → 换一个代理
	acc.ProxyURL = "http://127.0.0.1:8888"
	f.Invalidate("a1")
	c3, _ := f.ClientFor("a1")
	if c3.Client == c2.Client {
		t.Fatal("换代理后 transport 没换")
	}

	// 走代理 → 改回直连
	acc.ProxyURL = ""
	f.Invalidate("a1")
	c4, err := f.ClientFor("a1")
	if err != nil {
		t.Fatalf("改回直连后取: %v", err)
	}
	if c4.Client == c3.Client {
		t.Fatal("改回直连后 transport 没换 —— 还在往代理上发")
	}
}

// 代理地址写错时必须**建不出 client**，而不是悄悄退回直连。
// 退回直连的表现是一切正常、隔离却没了。
func TestBadProxyFailsInsteadOfFallingBack(t *testing.T) {
	acc := types.OVHAccount{
		ID: "a1", Name: "测试", Endpoint: "ovh-eu", Zone: "IE",
		AppKey: "k", AppSecret: "s", ConsumerKey: "c",
		ProxyURL: "ftp://1.2.3.4:21", // 不支持的协议
	}
	f := NewFactory(nil, func(id string) (types.OVHAccount, bool) { return acc, true })
	if _, err := f.ClientFor("a1"); err == nil {
		t.Fatal("代理配置非法时应当直接失败，绝不能退回直连")
	}
}

// 没配代理的账户照常直连，不该被代理逻辑影响。
func TestNoProxyStillWorks(t *testing.T) {
	acc := types.OVHAccount{
		ID: "a1", Name: "测试", Endpoint: "ovh-eu", Zone: "IE",
		AppKey: "k", AppSecret: "s", ConsumerKey: "c",
	}
	f := NewFactory(nil, func(id string) (types.OVHAccount, bool) { return acc, true })
	cli, err := f.ClientFor("a1")
	if err != nil {
		t.Fatalf("没配代理的账户应当能正常建 client: %v", err)
	}
	if cli.Client == nil {
		t.Fatal("http.Client 没装上")
	}
}

// 不同账户各用各的 transport —— 这是"隔离"的字面意思。
func TestPerAccountIsolation(t *testing.T) {
	accs := map[string]types.OVHAccount{
		"eu": {ID: "eu", Name: "欧", Endpoint: "ovh-eu", Zone: "IE",
			AppKey: "k", AppSecret: "s", ConsumerKey: "c", ProxyURL: "socks5://127.0.0.1:1080"},
		"us": {ID: "us", Name: "美", Endpoint: "ovh-us", Zone: "US",
			AppKey: "k", AppSecret: "s", ConsumerKey: "c", ProxyURL: "socks5://127.0.0.1:1081"},
	}
	f := NewFactory(nil, func(id string) (types.OVHAccount, bool) {
		a, ok := accs[id]
		return a, ok
	})
	ce, err := f.ClientFor("eu")
	if err != nil {
		t.Fatal(err)
	}
	cu, err := f.ClientFor("us")
	if err != nil {
		t.Fatal(err)
	}
	if ce.Client == cu.Client {
		t.Fatal("两个账户共用了同一个 http.Client —— 出口没有隔离")
	}
	if ce.Client.Transport == cu.Client.Transport {
		t.Fatal("两个账户共用了同一个 transport —— 出口没有隔离")
	}
}
