package ovh

import (
	"net/http"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// IsolatedClientFor 为单次顺序工作流创建独立 SDK/HTTP client,复用账户的出站 transport。
// go-ovh 1.5.1 的 NewRequest 会写 http.Client.Timeout;共享实例并发调用会发生数据竞争。
// 返回值不能跨 goroutine 共享;不复制 SDK 中已经使用的 atomic.Value 或 HTTP client 内部状态。
func (f *Factory) IsolatedClientFor(accountID string) (*ovhsdk.Client, error) {
	base, err := f.ClientFor(accountID)
	if err != nil {
		return nil, err
	}
	return IsolateClient(base)
}

// IsolateClient 从只读 SDK 配置创建顺序请求专用实例,连接池仍由 transport 共享。
func IsolateClient(base *ovhsdk.Client) (*ovhsdk.Client, error) {
	client, err := ovhsdk.NewClient(base.Endpoint(), base.AppKey, base.AppSecret, base.ConsumerKey)
	if err != nil {
		return nil, err
	}
	client.Timeout, client.UserAgent, client.Logger = base.Timeout, base.UserAgent, base.Logger
	client.Client = &http.Client{Transport: base.Client.Transport, CheckRedirect: base.Client.CheckRedirect, Jar: base.Client.Jar}
	return client, nil
}
