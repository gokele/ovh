package ovh

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"sync/atomic"
	"time"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// DispatchTrace 只在 httptrace 确认真正完成请求写入后报告时间;不把 Do 返回当作网络写出。
type DispatchTrace struct {
	DetectedAt time.Time
	wroteUS    atomic.Int64
	wrote      atomic.Bool
	reused     atomic.Bool
}

// DispatchSnapshot 使用微秒保留热路径精度,未发生写入时 WroteRequest 为 false。
type DispatchSnapshot struct {
	WroteRequest       bool  `json:"wroteRequest"`
	DetectionToWriteUS int64 `json:"detectionToWriteUs"`
	ConnectionReused   bool  `json:"connectionReused"`
	Prewarmed          bool  `json:"prewarmed"`
}

// Snapshot 允许 HTTP 回调与结果记录并发读取,失败写入不伪造零延迟成功。
func (t *DispatchTrace) Snapshot(prewarmed bool) DispatchSnapshot {
	return DispatchSnapshot{WroteRequest: t.wrote.Load(), DetectionToWriteUS: t.wroteUS.Load(), ConnectionReused: t.reused.Load(), Prewarmed: prewarmed}
}

// context 将微小的内存打点附在请求上,不在网络回调中写日志或数据库。
func (t *DispatchTrace) context(ctx context.Context) context.Context {
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { t.reused.Store(info.Reused) },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				t.wroteUS.Store(time.Since(t.DetectedAt).Microseconds())
				t.wrote.Store(true)
			}
		},
	})
}

// ErrCheckoutAlreadySent 同一份预构造结账被第二次调用;它说明之前已经发过,绝不能当作「未发出」。
var ErrCheckoutAlreadySent = errors.New("prepared checkout has already been sent")

// PreparedCall 的请求体、URL、静态请求头和签名前缀在等库存时生成,每辆购物车只允许发送一次。
type PreparedCall struct {
	request *http.Request
	client  *http.Client
	sdk     *ovhsdk.Client
	prefix  []byte
	delta   time.Duration
	sent    atomic.Bool
}

// PreparePost 通过官方 SDK 构造请求并校准时钟;此处不会提交结账。
func PreparePost(client *ovhsdk.Client, path string, body interface{}) (*PreparedCall, error) {
	if client.AppKey == "" || client.AppSecret == "" || client.ConsumerKey == "" {
		return nil, fmt.Errorf("prepared checkout requires OVH application credentials")
	}
	req, err := client.NewRequest(http.MethodPost, path, body, true)
	if err != nil {
		return nil, err
	}
	delta, err := client.TimeDelta()
	if err != nil {
		return nil, err
	}
	payload, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	// NewRequest 的 GetBody 已保存同一份序列化结果,不重新编码或改变签名内容。
	if req.GetBody != nil {
		req.Body, err = req.GetBody()
	}
	if err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("%s+%s+POST+%s+%s+", client.AppSecret, client.ConsumerKey, req.URL.String(), payload)
	buffer := make([]byte, len(prefix), len(prefix)+20)
	copy(buffer, prefix)
	return &PreparedCall{request: req, sdk: client, delta: delta,
		prefix: buffer,
		client: &http.Client{Transport: client.Client.Transport, Timeout: client.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

// Send 只更新时间戳和 SHA1 签名;没有日志、序列化、数据库访问或隐式 POST 重试。
func (p *PreparedCall) Send(ctx context.Context, trace *DispatchTrace, result interface{}) error {
	if !p.sent.CompareAndSwap(false, true) {
		return ErrCheckoutAlreadySent
	}
	input := strconv.AppendInt(p.prefix, time.Now().Add(-p.delta).Unix(), 10)
	sum := sha1.Sum(input)
	var signature [43]byte
	copy(signature[:], "$1$")
	hex.Encode(signature[3:], sum[:])
	p.request.Header["X-Ovh-Timestamp"][0] = string(input[len(p.prefix):])
	p.request.Header["X-Ovh-Signature"][0] = string(signature[:])

	if trace != nil {
		ctx = trace.context(ctx)
	}
	response, err := p.client.Do(p.request.WithContext(ctx))
	if err != nil {
		return err
	}
	return p.sdk.UnmarshalResponse(response, result)
}

// KeepWarm 使用同一账户 transport 的轻量无鉴权接口,完整读完响应以保留连接。
func KeepWarm(ctx context.Context, client *ovhsdk.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.Endpoint()+"/auth/time", nil)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Transport: client.Client.Transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if err != nil {
		return err
	}
	if response.StatusCode != 200 {
		return fmt.Errorf("OVH warmup returned HTTP %d", response.StatusCode)
	}
	return nil
}
