package auth

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func testRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware(Config{APIKey: "correct-key", Enabled: true, WhitelistPaths: DefaultWhitelist()}))
	r.GET("/api/stats", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/api/health", func(c *gin.Context) { c.String(http.StatusOK, "health") })
	return r
}

func req(r *gin.Engine, key, ip string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	q := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	if key != "" {
		q.Header.Set("X-API-Key", key)
	}
	q.RemoteAddr = ip + ":12345"
	r.ServeHTTP(w, q)
	return w
}

// 密钥错误必须是有代价的。
//
// 在加这个限流之前,端口可达的人可以**不限次数**地猜 API 密钥,
// 一秒几千次、没有任何阻力 —— 而这个服务能用你的 OVH 账户下单、能重装服务器。
// 单二进制部署时 LISTEN_HOST 默认为空(监听所有网卡),所以这不是只有公网才成立的问题。
func TestBruteForceGetsBlocked(t *testing.T) {
	ResetAuthFailures()
	r := testRouter()
	const ip = "10.0.0.5"

	for i := 0; i < MaxAuthFailures; i++ {
		if got := req(r, "wrong", ip).Code; got != http.StatusUnauthorized {
			t.Fatalf("第 %d 次错误密钥应当是 401,实际 %d", i+1, got)
		}
	}
	w := req(r, "wrong", ip)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("超过 %d 次后应当被限流(429),实际 %d", MaxAuthFailures, w.Code)
	}
	// 必须告诉调用方等多久,否则前端只能盲目重试
	ra := w.Header().Get("Retry-After")
	if n, err := strconv.Atoi(ra); err != nil || n <= 0 {
		t.Errorf("Retry-After 头缺失或不合法: %q", ra)
	}
	// 被限流期间,**正确**的密钥也进不来 —— 否则爆破者只要在被挡后继续试就行
	if got := req(r, "correct-key", ip).Code; got != http.StatusTooManyRequests {
		t.Errorf("限流期间正确密钥也应被挡,实际 %d", got)
	}
}

// 限流按来源分。一个人打错密钥不能把别人也挡在外面。
func TestBlockIsPerClient(t *testing.T) {
	ResetAuthFailures()
	r := testRouter()
	for i := 0; i <= MaxAuthFailures; i++ {
		req(r, "wrong", "10.0.0.6")
	}
	if got := req(r, "wrong", "10.0.0.6").Code; got != http.StatusTooManyRequests {
		t.Fatalf("该来源应已被限流,实际 %d", got)
	}
	if got := req(r, "correct-key", "10.0.0.7").Code; got != http.StatusOK {
		t.Errorf("另一个来源被误伤了,实际 %d", got)
	}
}

// 打错一次再打对,不该留下任何后遗症 —— 手机上输密钥打错很常见。
func TestSuccessClearsFailures(t *testing.T) {
	ResetAuthFailures()
	r := testRouter()
	const ip = "10.0.0.8"
	for i := 0; i < MaxAuthFailures-1; i++ {
		req(r, "wrong", ip)
	}
	if got := req(r, "correct-key", ip).Code; got != http.StatusOK {
		t.Fatalf("还没到上限,正确密钥应当放行,实际 %d", got)
	}
	// 清零之后又能重新错满一整轮
	for i := 0; i < MaxAuthFailures; i++ {
		if got := req(r, "wrong", ip).Code; got != http.StatusUnauthorized {
			t.Fatalf("成功后计数没清零:第 %d 次就被挡了(%d)", i+1, got)
		}
	}
}

// 窗口过去之后要自动放行,不能把人永久锁死
func TestWindowExpires(t *testing.T) {
	ResetAuthFailures()
	const ip = "10.0.0.9"
	past := time.Now().Add(-AuthFailureWindow - time.Second)
	for i := 0; i < MaxAuthFailures; i++ {
		recordAuthFailure(ip, past)
	}
	if blocked, _ := authBlocked(ip, time.Now()); blocked {
		t.Error("窗口已过期,不该继续拦截")
	}
}

// 健康检查在白名单里,限流不能影响它 —— 否则容器健康检查会把自己搞成 unhealthy
func TestWhitelistUnaffected(t *testing.T) {
	ResetAuthFailures()
	r := testRouter()
	const ip = "10.0.0.10"
	for i := 0; i <= MaxAuthFailures; i++ {
		req(r, "wrong", ip)
	}
	w := httptest.NewRecorder()
	q := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	q.RemoteAddr = ip + ":1"
	r.ServeHTTP(w, q)
	if w.Code != http.StatusOK {
		t.Errorf("白名单路径被限流误伤了,实际 %d", w.Code)
	}
}
