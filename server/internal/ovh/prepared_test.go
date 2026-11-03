package ovh

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// preparedFixture 仅启动本机假 API,检查签名覆盖实际 URL/正文和即时服务端时间。
func preparedFixture(t testing.TB, redirect bool) (*ovhsdk.Client, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/time" {
			fmt.Fprint(w, time.Now().Unix())
			return
		}
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		stamp := r.Header.Get("X-Ovh-Timestamp")
		target := "http://" + r.Host + r.URL.RequestURI()
		signature := fmt.Sprintf("$1$%x", sha1.Sum([]byte("secret+consumer+POST+"+target+"+"+string(body)+"+"+stamp)))
		if r.Header.Get("X-Ovh-Signature") != signature || r.Header.Get("X-Ovh-Consumer") != "consumer" || r.Header.Get("X-Ovh-Application") != "application" || string(body) != `{"autoPayWithPreferredPaymentMethod":false}` {
			t.Errorf("invalid signed checkout %s %s", r.Method, r.URL.Path)
		}
		seconds, err := strconv.ParseInt(stamp, 10, 64)
		if err != nil || time.Since(time.Unix(seconds, 0)) > 2*time.Second {
			t.Error("stale timestamp")
		}
		if redirect {
			w.Header().Set("Location", "/unexpected-checkout")
			w.WriteHeader(307)
			return
		}
		fmt.Fprint(w, `{"orderId":123}`)
	}))
	t.Cleanup(server.Close)
	client, err := ovhsdk.NewClient(server.URL, "application", "secret", "consumer")
	if err != nil {
		t.Fatal(err)
	}
	client.Client = server.Client()
	return client, calls
}

// 正文已预构造且时钟已校准;只有实际网络写入回调才报告微秒耗时。
func TestPreparedCheckoutReusesConnectionAndHasValidSignature(t *testing.T) {
	client, calls := preparedFixture(t, false)
	call, err := PreparePost(client, "/checkout", map[string]interface{}{"autoPayWithPreferredPaymentMethod": false})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("preparation sent a checkout")
	}
	trace := &DispatchTrace{DetectedAt: time.Now()}
	var result map[string]interface{}
	if err := call.Send(context.Background(), trace, &result); err != nil {
		t.Fatal(err)
	}
	stats := trace.Snapshot(true)
	if !stats.WroteRequest || !stats.ConnectionReused || !stats.Prewarmed || calls.Load() != 1 || stats.DetectionToWriteUS < 0 {
		t.Fatal(stats, calls.Load())
	}
	if err := call.Send(context.Background(), nil, nil); err == nil || calls.Load() != 1 {
		t.Fatal("checkout repeated")
	}
}

// 并发发送和 307 重定向均不能让同一辆车发出第二个 POST。
func TestPreparedCheckoutConcurrentSendAndRedirectCannotDuplicate(t *testing.T) {
	client, calls := preparedFixture(t, true)
	call, err := PreparePost(client, "/checkout", map[string]interface{}{"autoPayWithPreferredPaymentMethod": false})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := call.Send(context.Background(), nil, nil); err == nil {
				t.Error("expected redirect/already-sent error")
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicated checkout", calls.Load())
	}
}

// 用真实本机 TCP 连接记录分位数,不把 HTTP 替身的零开销冒充网络性能。
func TestPreparedCheckoutLocalWriteLatency(t *testing.T) {
	client, _ := preparedFixture(t, false)
	samples := make([]int64, 0, 200)
	for i := 0; i < 200; i++ {
		call, err := PreparePost(client, "/checkout", map[string]interface{}{"autoPayWithPreferredPaymentMethod": false})
		if err != nil {
			t.Fatal(err)
		}
		trace := &DispatchTrace{DetectedAt: time.Now()}
		if err := call.Send(context.Background(), trace, nil); err != nil {
			t.Fatal(err)
		}
		stats := trace.Snapshot(true)
		if !stats.WroteRequest || !stats.ConnectionReused {
			t.Fatal(stats)
		}
		samples = append(samples, stats.DetectionToWriteUS)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	t.Logf("warm local TCP dispatch: n=%d p50=%dus p95=%dus p99=%dus max=%dus", len(samples), samples[100], samples[190], samples[198], samples[199])
}

// BenchmarkPreparedCheckout 包含 SHA1、HTTP 连接取得和真实本机请求,复用已热连接。
func BenchmarkPreparedCheckout(b *testing.B) {
	client, _ := preparedFixture(b, false)
	var total int64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		call, err := PreparePost(client, "/checkout", map[string]interface{}{"autoPayWithPreferredPaymentMethod": false})
		if err != nil {
			b.Fatal(err)
		}
		trace := &DispatchTrace{}
		b.StartTimer()
		trace.DetectedAt = time.Now()
		if err := call.Send(context.Background(), trace, nil); err != nil {
			b.Fatal(err)
		}
		total += trace.Snapshot(true).DetectionToWriteUS
	}
	b.ReportMetric(float64(total)/float64(b.N), "write-us/op")
}

// KeepWarm 只调用无鉴权时间接口,不需要凭据或执行任何写操作。
func TestKeepWarmOnlyReadsTime(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/auth/time") || r.Header.Get("X-Ovh-Consumer") != "" {
			t.Error("unexpected warmup request")
		}
		fmt.Fprint(w, time.Now().Unix())
	}))
	defer server.Close()
	client, err := ovhsdk.NewClient(server.URL, "test", "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	client.Client = server.Client()
	if err := KeepWarm(context.Background(), client); err != nil || requests != 1 {
		t.Fatal(err, requests)
	}
}

// 失败写入不能伪造 wroteRequest=true 或一个看似极快的 0µs 成绩。
func TestFailedTransportHasNoWriteTiming(t *testing.T) {
	client, calls := preparedFixture(t, false)
	call, err := PreparePost(client, "/checkout", map[string]interface{}{"autoPayWithPreferredPaymentMethod": false})
	if err != nil {
		t.Fatal(err)
	}
	call.client.Transport = failedPreparedTransport{}
	trace := &DispatchTrace{DetectedAt: time.Now()}
	if err := call.Send(context.Background(), trace, nil); err == nil {
		t.Fatal("transport did not fail")
	}
	if snapshot := trace.Snapshot(true); snapshot.WroteRequest || snapshot.DetectionToWriteUS != 0 || calls.Load() != 0 {
		t.Fatal(snapshot, calls.Load())
	}
}

type failedPreparedTransport struct{}

// RoundTrip 替身只制造发送前故障,不会建立任何外部连接。
func (failedPreparedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("fictional transport failure")
}
