package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// 独立 SDK/HTTP client 避免并发查询详情时同时改写共享的 http.Client.Timeout。
func TestConcurrentDetailReadersKeepIndependentClients(t *testing.T) {
	client, err := ovhsdk.NewClient("ovh-eu", "fixture", "fixture", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	client.Client = &http.Client{Transport: queuePreorderTransport(func(req *http.Request) (*http.Response, error) {
		body := `{"value":"ok"}`
		if strings.HasSuffix(req.URL.Path, "/auth/time") {
			body = strconv.FormatInt(time.Now().Unix(), 10)
		}
		time.Sleep(time.Millisecond)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	keys := make([]interface{}, 30)
	textKeys := make([]string, 30)
	for i := range keys {
		keys[i] = i
		textKeys[i] = fmt.Sprint(i)
	}
	path := func(v interface{}) string { return fmt.Sprint("/fixture/", v) }
	for _, read := range []func() []map[string]interface{}{
		func() []map[string]interface{} { return parallelGetDetails(client, keys, path, 10) },
		func() []map[string]interface{} {
			return parallelGetStringKeys(client, textKeys, func(s string) string { return "/fixture/" + s }, 10)
		},
		func() []map[string]interface{} {
			rows, errs := parallelGetDetailsWithErrs(client, keys, path, 10)
			if n, _ := countErrs(errs); n != 0 {
				t.Errorf("unexpected errors: %v", errs)
			}
			return rows
		},
		func() []map[string]interface{} {
			rows, errs := hardwareParallelGetDetails(client, keys, path, 10)
			if n, _ := countErrs(errs); n != 0 {
				t.Errorf("unexpected errors: %v", errs)
			}
			return rows
		},
	} {
		for _, row := range read() {
			if row["value"] != "ok" {
				t.Fatalf("lost detail: %+v", row)
			}
		}
	}
}

// 不同 HTTP 请求、询价和抢购共享出站连接池,但绝不能共享会被 SDK 写入的客户端。
func TestOVHRequestClientsAreIsolatedFromFactoryCache(t *testing.T) {
	st, _, _ := supportFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, _ := gin.CreateTestContext(nil)
			ctx.Request, _ = http.NewRequest("GET", "/fixture?account=eu", nil)
			first, err := ovhClientFor(st, ctx)
			if err != nil {
				t.Error(err)
				return
			}
			second, err := ovhClientFor(st, ctx)
			if err != nil {
				t.Error(err)
				return
			}
			if first == second || first.Client == second.Client || reflect.ValueOf(first.Client.Transport).Pointer() != reflect.ValueOf(second.Client.Transport).Pointer() {
				t.Error("client not isolated or transport not reused")
			}
			var out []int64
			if err := first.GetWithContext(context.Background(), "/support/tickets?page=0&pageSize=20", &out); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
