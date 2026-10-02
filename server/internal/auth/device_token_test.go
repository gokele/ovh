package auth

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// Bearer 设备令牌与 X-API-Key 双入口的行为矩阵。
// 吊销 = 令牌立即失效(手机丢了的唯一补救),这条不能断。
func TestDeviceTokenAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type dev struct {
		id      int64
		revoked bool
	}
	var mu sync.Mutex
	devices := map[string]dev{} // token → 设备
	cfg := Config{
		APIKey:         "master-key",
		Enabled:        true,
		WhitelistPaths: DefaultWhitelist(),
		DeviceTokenValid: func(token string) (int64, bool) {
			mu.Lock()
			defer mu.Unlock()
			d, ok := devices[token]
			return d.id, ok && !d.revoked
		},
	}
	r := gin.New()
	r.Use(Middleware(cfg))
	r.GET("/api/echo", func(c *gin.Context) { c.Status(200) })

	do := func(headers map[string]string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/echo", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if got := do(map[string]string{"Authorization": "Bearer good-token"}); got != 401 {
		t.Fatalf("未知令牌应 401,得 %d", got)
	}
	mu.Lock()
	devices["good-token"] = dev{id: 7}
	mu.Unlock()
	if got := do(map[string]string{"Authorization": "Bearer good-token"}); got != 200 {
		t.Fatalf("有效令牌应放行,得 %d", got)
	}
	// 吊销:立即失效
	mu.Lock()
	devices["good-token"] = dev{id: 7, revoked: true}
	mu.Unlock()
	if got := do(map[string]string{"Authorization": "Bearer good-token"}); got != 401 {
		t.Fatalf("已吊销令牌应 401,得 %d —— 吊销不生效等于没有吊销功能", got)
	}
	// 坏 Bearer 头(非 Bearer 前缀)回落到密钥路径
	if got := do(map[string]string{"Authorization": "Basic abc", "X-API-Key": "master-key"}); got != 200 {
		t.Fatalf("密钥 + 无关 Authorization 应放行,得 %d", got)
	}
	// 配对兑换端点在白名单(免密钥)
	req := httptest.NewRequest(http.MethodPost, "/api/app/pair", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == 401 {
		t.Fatal("/api/app/pair 不应要求密钥(凭一次性码)")
	}
}
