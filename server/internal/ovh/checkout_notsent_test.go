package ovh

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// 建连失败时请求从未写出:判定为「未发出」,可以安全重试。
func TestCheckoutNotSentWhenConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // 端口已关闭,连接会被拒绝
	timeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`1700000000`)) }))
	defer timeSrv.Close()
	client, err := ovhsdk.NewClient(timeSrv.URL, "ak", "as", "ck")
	if err != nil {
		t.Fatal(err)
	}
	call, err := PreparePost(client, "/order/cart/c/checkout", map[string]interface{}{"autoPayWithPreferredPaymentMethod": false})
	if err != nil {
		t.Fatal(err)
	}
	call.request.URL.Host = addr // 指向已关闭端口
	trace := &DispatchTrace{DetectedAt: time.Now()}
	sendErr := call.Send(context.Background(), trace, nil)
	if sendErr == nil || !CheckoutNotSent(sendErr, trace.Snapshot(false).WroteRequest) {
		t.Fatalf("refused connection must count as not sent: %v wrote=%v", sendErr, trace.Snapshot(false).WroteRequest)
	}
	if again := call.Send(context.Background(), trace, nil); !errors.Is(again, ErrCheckoutAlreadySent) || CheckoutNotSent(again, false) {
		t.Fatal("a second send of the same call must never look like a safe retry")
	}
}

// 请求已写出、服务器中途断开:结果不明,不能当作未发出。
func TestCheckoutWrittenThenDroppedIsUncertain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/time" {
			_, _ = w.Write([]byte(`1700000000`))
			return
		}
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		conn.Close() // 收到完整请求后直接断开,不回响应
	}))
	defer srv.Close()
	client, err := ovhsdk.NewClient(srv.URL, "ak", "as", "ck")
	if err != nil {
		t.Fatal(err)
	}
	call, err := PreparePost(client, "/order/cart/c/checkout", map[string]interface{}{"autoPayWithPreferredPaymentMethod": false})
	if err != nil {
		t.Fatal(err)
	}
	trace := &DispatchTrace{DetectedAt: time.Now()}
	sendErr := call.Send(context.Background(), trace, nil)
	if sendErr == nil {
		t.Fatal("expected a transport error")
	}
	if CheckoutNotSent(sendErr, trace.Snapshot(false).WroteRequest) {
		t.Fatal("a request the server received must stay uncertain")
	}
	if !CheckoutUncertain(sendErr) {
		t.Fatal("dropped response must be uncertain")
	}
}
