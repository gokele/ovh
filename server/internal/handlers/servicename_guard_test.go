package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
)

// # 和 ? 会让请求被**静默发到另一个端点**,而不是报错。
// 这条测试先证明那个截断是真的(不是我臆想的),再证明中间件挡住了它。
func TestServiceNameWithFragmentWouldHitWrongEndpoint(t *testing.T) {
	// handler 里一律是这样拼路径的,两百多处
	req, err := http.NewRequest(http.MethodPost,
		"https://eu.api.ovh.com/1.0"+"/dedicated/server/"+"a#b"+"/reboot", nil)
	if err != nil {
		t.Fatalf("建请求失败: %v", err)
	}
	if req.URL.Path != "/1.0/dedicated/server/a" {
		t.Fatalf("截断行为变了,这条测试的前提需要重新确认。实际路径: %q", req.URL.Path)
	}
	if req.URL.Fragment != "b/reboot" {
		t.Fatalf("期望 /reboot 掉进片段里,实际片段 %q", req.URL.Fragment)
	}
}

func TestValidateServiceName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/x")
	g.Use(ValidateServiceName())
	g.GET("/:service_name/info", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	g.GET("/list", func(c *gin.Context) { c.String(http.StatusOK, "list") })

	pass := []string{
		"ns3001234.ip-1-2-3.eu", // 独服
		"vps-abc12345.vps.ovh.net",
		"vps12345.vps.ovh.net",
		"ns12345.ovh.net",
		"abc_123-x.y", // 下划线也放行
	}
	for _, svc := range pass {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x/"+svc+"/info", nil))
		if w.Code != http.StatusOK {
			t.Errorf("正常服务名 %q 被挡了: %d %s", svc, w.Code, w.Body.String())
		}
	}

	// 这些都会把 OVH 请求打歪或打到别处
	block := map[string]string{
		"a#b":   "井号:后面的路径会变成片段",
		"a?b":   "问号:后面的路径会变成查询串",
		"a b":   "空格",
		"服务器":   "非 ASCII",
		"a%2Fb": "编码过的斜杠",
	}
	for svc, why := range block {
		w := httptest.NewRecorder()
		// 编码后再发,模拟前端 encodeURIComponent —— Gin 会解码回原始字符
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x/"+url.PathEscape(svc)+"/info", nil))
		if w.Code == http.StatusOK {
			t.Errorf("%s(%q)没被挡住", why, svc)
		}
	}

	// 组里不带 :service_name 的路由不能被误伤
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x/list", nil))
	if w.Code != http.StatusOK {
		t.Errorf("不带服务名的路由被误伤: %d", w.Code)
	}
}
