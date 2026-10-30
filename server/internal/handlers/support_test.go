package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/types"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

type supportCall struct {
	Method, Path, Host, Query string
	Body                      map[string]interface{}
}
type supportMock struct {
	mu        sync.Mutex
	calls     []supportCall
	status    string
	canClose  bool
	postCode  int
	partial   bool
	missingID bool
}

// supportFixture 使用虚构账号和离线 transport,测试绝不发送真实客服工单。
func supportFixture(t *testing.T) (*app.State, *gin.Engine, *supportMock) {
	t.Helper()
	st, _ := newWatchTestMonitor(t)
	mock := &supportMock{status: "open", canClose: true, postCode: 200}
	st.Accounts = []types.OVHAccount{{ID: "eu", Name: "EU fixture", Endpoint: "ovh-eu", Zone: "IE", AppKey: "test", AppSecret: "test", ConsumerKey: "test", IsDefault: true}, {ID: "us", Name: "US fixture", Endpoint: "ovh-us", Zone: "US", AppKey: "test", AppSecret: "test", ConsumerKey: "test"}}
	for _, account := range st.Accounts {
		client, err := st.OVH.ClientFor(account.ID)
		if err != nil {
			t.Fatal(err)
		}
		client.Client = &http.Client{Transport: queuePreorderTransport(func(req *http.Request) (*http.Response, error) {
			mock.mu.Lock()
			defer mock.mu.Unlock()
			path := req.URL.Path
			body := `{}`
			code := 200
			payload := map[string]interface{}{}
			if req.Body != nil {
				_ = json.NewDecoder(req.Body).Decode(&payload)
			}
			mock.calls = append(mock.calls, supportCall{Method: req.Method, Path: path, Host: req.URL.Host, Query: req.URL.RawQuery, Body: payload})
			switch {
			case strings.HasSuffix(path, "/auth/time"):
				body = fmt.Sprint(time.Now().Unix())
			case strings.HasSuffix(path, "/dedicated/server"):
				body = `["ns-b.example","ns-a.example"]`
			case strings.HasSuffix(path, "/vps"):
				body = `["vps-fixture.example"]`
			case path == "/1.0/support/tickets":
				if req.URL.Query().Get("page") == "" || req.URL.Query().Get("pageSize") != "20" {
					t.Error("missing bounded pagination", req.URL.RawQuery)
				}
				body = `[123]`
				if mock.partial {
					body = `[123,124]`
				}
			case path == "/1.0/support/tickets/123":
				body = fmt.Sprintf(`{"ticketId":123,"ticketNumber":987654,"subject":"Fixture issue","state":%q,"category":"incident","product":"dedicated","canBeClosed":%t}`, mock.status, mock.canClose)
			case path == "/1.0/support/tickets/124":
				code = 403
				body = `{"message":"fixture permission denied"}`
			case strings.HasSuffix(path, "/123/messages"):
				body = `[{"messageId":1,"body":"Line 1\nLine 2 <script>plain text</script>","from":"support","creationDate":"2026-10-30T01:00:00Z"}]`
			case req.Method == "POST":
				code = mock.postCode
				if code >= 400 {
					body = `{"message":"fixture rejection"}`
					break
				}
				switch {
				case strings.HasSuffix(path, "/tickets/create"):
					body = `{"ticketId":123,"ticketNumber":987654,"messageId":2,"additionalNotice":"Fixture notice"}`
					if mock.missingID {
						body = `{}`
					}
				case strings.HasSuffix(path, "/123/close"):
					mock.status = "closed"
				case strings.HasSuffix(path, "/123/reopen"):
					mock.status = "open"
				case strings.HasSuffix(path, "/123/reply"):
				default:
					t.Errorf("unexpected POST %s", path)
				}
			default:
				code = 404
				body = `{"message":"missing fixture"}`
			}
			return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		})}
	}
	r := gin.New()
	r.GET("/options", GetSupportOptions(st))
	r.GET("/services", GetSupportServices(st))
	r.GET("/tickets", ListSupportTickets(st))
	r.POST("/tickets", CreateSupportTicket(st))
	r.GET("/tickets/:ticket_id", GetSupportTicket(st))
	r.POST("/tickets/:ticket_id", UpdateSupportTicket(st))
	return st, r, mock
}

// posts 在请求完成后读取记录,用于确认没有隐式提交或自动重试。
func (m *supportMock) posts() []supportCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []supportCall{}
	for _, call := range m.calls {
		if call.Method == "POST" {
			out = append(out, call)
		}
	}
	return out
}

// 类别、产品、子类别是可选项列表,读取它们不会发起工单操作。
func TestSupportOptionsAndReadOnlyAccountIsolation(t *testing.T) {
	_, r, m := supportFixture(t)
	w := payRequest(r, "GET", "/options?account=us", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var options struct {
		AccountID                           string `json:"accountId"`
		Categories, Products, Subcategories []string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &options); err != nil {
		t.Fatal(err)
	}
	if options.AccountID != "us" || len(options.Categories) != 3 || !supportMember("incident", options.Categories) || !supportMember("vps", options.Products) || !supportMember("down", options.Subcategories) {
		t.Fatalf("missing official selections: %+v", options)
	}
	w = payRequest(r, "GET", "/services?account=us&product=dedicated", "")
	if w.Code != 200 || w.Body.String() != `["ns-a.example","ns-b.example"]` {
		t.Fatal(w.Code, w.Body)
	}
	w = payRequest(r, "GET", "/tickets?account=us&page=0&status=open", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"accountId":"us"`) {
		t.Fatal(w.Code, w.Body)
	}
	w = payRequest(r, "GET", "/tickets/123?account=us", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Line 1\\nLine 2") {
		t.Fatal(w.Code, w.Body)
	}
	for _, call := range m.calls {
		if call.Host != "api.us.ovhcloud.com" {
			t.Fatal("account leaked to wrong region", call.Host)
		}
	}
	if len(m.posts()) != 0 {
		t.Fatal("read operation submitted ticket")
	}
}

// 创建只透传已验证的官方字段,保留正文段落并返回号码/附加提示。
func TestSupportCreateUsesSelectedCategories(t *testing.T) {
	_, r, m := supportFixture(t)
	w := payRequest(r, "POST", "/tickets?account=eu", `{"subject":"  Disk issue  ","body":"  First line\n\nSecond line  ","category":"incident","product":"dedicated","subcategory":"down","serviceName":" ns-a.example ","type":"criticalIntervention","impact":"high"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Fixture notice") {
		t.Fatal(w.Code, w.Body)
	}
	calls := m.posts()
	if len(calls) != 1 {
		t.Fatal(calls)
	}
	body := calls[0].Body
	if body["category"] != "incident" || body["product"] != "dedicated" || body["subcategory"] != "down" || body["serviceName"] != "ns-a.example" || body["body"] != "First line\n\nSecond line" || body["subject"] != "Disk issue" || body["type"] != nil || body["impact"] != nil {
		t.Fatal("wrong create payload", body)
	}
}

// 缺少必要类别或标题/正文、非法分类、跨账户 ID 都不能触发 POST。
func TestSupportRejectsInvalidInputBeforeSending(t *testing.T) {
	_, r, m := supportFixture(t)
	for _, body := range []string{`{}`, `{"subject":"x","body":"x","product":"vps"}`, `{"subject":"x","body":"x","category":"other","product":"vps"}`, `{"subject":"x","body":"x","category":"billing","product":"invalid"}`, `{"subject":"x","body":"x","category":"billing","product":"vps","subcategory":"invalid"}`, `{"subject":" ","body":"x","category":"billing","product":"vps"}`} {
		w := payRequest(r, "POST", "/tickets?account=eu", body)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body)
		}
	}
	for _, path := range []string{"/tickets?page=-1", "/tickets?page=not-a-number", "/tickets?status=unknown", "/tickets/987654", "/tickets/bad", "/options?account=missing"} {
		w := payRequest(r, "GET", path, "")
		if w.Code < 400 {
			t.Fatal(path, w.Code, w.Body)
		}
	}
	if len(m.posts()) != 0 {
		t.Fatal("invalid request sent to OVH")
	}
}

// 详情失败必须保留可识别的占位,不能伪装为完整列表。
func TestSupportListKeepsPartialFailureVisible(t *testing.T) {
	_, r, m := supportFixture(t)
	m.partial = true
	w := payRequest(r, "GET", "/tickets?account=eu", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"failedCount":1`) || !strings.Contains(w.Body.String(), `"ticketId":124`) || !strings.Contains(w.Body.String(), "fixture permission denied") || !strings.Contains(w.Body.String(), `"code":"SUPPORT_REQUEST_FAILED"`) || !strings.Contains(w.Body.String(), `"params":`) {
		t.Fatal(w.Code, w.Body)
	}
}

// 每次操作使用内部 ticketId,关闭状态及可关闭标志由服务端重新检查。
func TestSupportReplyCloseAndReopenStateChecks(t *testing.T) {
	_, r, m := supportFixture(t)
	for _, action := range []string{"reply", "close", "reopen"} {
		w := payRequest(r, "POST", "/tickets/123?account=eu", fmt.Sprintf(`{"action":%q,"body":"Reason\nDetails"}`, action))
		if w.Code != 200 {
			t.Fatal(action, w.Code, w.Body)
		}
	}
	calls := m.posts()
	if len(calls) != 3 || !strings.HasSuffix(calls[0].Path, "/123/reply") || !strings.HasSuffix(calls[1].Path, "/123/close") || !strings.HasSuffix(calls[2].Path, "/123/reopen") {
		t.Fatal(calls)
	}
	m.canClose = false
	for _, body := range []string{`{"action":"close"}`, `{"action":"delete"}`, `{"action":"reply","body":" "}`, `{"action":"reopen","body":"reason"}`} {
		w := payRequest(r, "POST", "/tickets/123?account=eu", body)
		if w.Code < 400 {
			t.Fatal(w.Code, w.Body)
		}
	}
	m.status = "closed"
	w := payRequest(r, "POST", "/tickets/123?account=eu", `{"action":"reply","body":"text"}`)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	if len(m.posts()) != 3 {
		t.Fatal("invalid operation sent POST")
	}
}

// 上游失败不自动重试创建请求,响应明确提示用户先核对结果。
func TestSupportAmbiguousCreateIsNotRetried(t *testing.T) {
	_, r, m := supportFixture(t)
	m.postCode = 503
	w := payRequest(r, "POST", "/tickets?account=eu", `{"subject":"Fixture","body":"Fixture","category":"assistance","product":"vps"}`)
	if w.Code != 502 || len(m.posts()) != 1 || !strings.Contains(w.Body.String(), "核对") {
		t.Fatal(w.Code, w.Body, len(m.posts()))
	}
}

// 成功响应缺少内部 ID 时不能谎报创建完成,也不能自动再发一次。
func TestSupportCreateMissingIDRequiresReconciliation(t *testing.T) {
	_, r, m := supportFixture(t)
	m.missingID = true
	w := payRequest(r, "POST", "/tickets?account=eu", `{"subject":"Fixture","body":"Fixture","category":"assistance","product":"vps"}`)
	if w.Code != 502 || len(m.posts()) != 1 || !strings.Contains(w.Body.String(), "核对") {
		t.Fatal(w.Code, w.Body)
	}
}

// 正文大小有限制,避免误粘贴大量日志耗尽内存。
func TestSupportRejectsOversizedBody(t *testing.T) {
	_, r, m := supportFixture(t)
	body := `{"subject":"Fixture","category":"assistance","product":"vps","body":"` + strings.Repeat("x", 1<<20) + `"}`
	w := payRequest(r, "POST", "/tickets?account=eu", body)
	if w.Code != 400 || len(m.posts()) != 0 {
		t.Fatal(w.Code, len(m.posts()))
	}
}

// 标题筛选透传给上游分页查询,不能只过滤当前页导致漏掉历史工单。
func TestSupportSubjectSearchBeforePagination(t *testing.T) {
	_, router, mock := supportFixture(t)
	response := payRequest(router, "GET", "/tickets?account=us&page=2&status=open&subject=%20Disk%20issue%20", "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	found := false
	for _, call := range mock.calls {
		if call.Path != "/1.0/support/tickets" {
			continue
		}
		q, err := url.ParseQuery(call.Query)
		if err != nil || q.Get("subject") != "Disk issue" || q.Get("page") != "2" || q.Get("status") != "open" || q.Get("pageSize") != "20" || call.Host != "api.us.ovhcloud.com" {
			t.Fatalf("wrong search scope: %+v", call)
		}
		found = true
	}
	if !found {
		t.Fatal("missing upstream search")
	}
}

// 本地校验返回稳定翻译码且只有一份 JSON,失败输入不会产生任何工单。
func TestSupportValidationProvidesTranslationCodes(t *testing.T) {
	_, router, mock := supportFixture(t)
	cases := []struct{ method, path, body, code string }{
		{"GET", "/options?account=missing", "", "SUPPORT_ACCOUNT_NOT_FOUND"},
		{"GET", "/services?product=invalid", "", "SUPPORT_PRODUCT_INVALID"},
		{"GET", "/tickets?page=-1", "", "SUPPORT_PAGE_INVALID"},
		{"GET", "/tickets?status=invalid", "", "SUPPORT_STATUS_INVALID"},
		{"GET", "/tickets?subject=" + strings.Repeat("x", 256), "", "SUPPORT_SEARCH_TOO_LONG"},
		{"GET", "/tickets/invalid", "", "SUPPORT_TICKET_ID_INVALID"},
		{"POST", "/tickets", `{`, "SUPPORT_PAYLOAD_INVALID"},
		{"POST", "/tickets", `{}`, "SUPPORT_FIELDS_INVALID"},
		{"POST", "/tickets/123", `{"action":"invalid"}`, "SUPPORT_ACTION_INVALID"},
		{"POST", "/tickets/123", `{"action":"reply"}`, "SUPPORT_BODY_REQUIRED"},
		{"POST", "/tickets/123", `{"action":"reopen","body":"Reason"}`, "SUPPORT_STATE_CONFLICT"},
	}
	for _, tc := range cases {
		w := payRequest(router, tc.method, tc.path, tc.body)
		var result struct {
			Success     bool
			Error, Code string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code < 400 || result.Success || result.Code != tc.code || result.Error == "" {
			t.Fatalf("%s %s: status=%d body=%s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	if len(mock.posts()) != 0 {
		t.Fatal("validation failure submitted a ticket")
	}
}

// 翻译只替换系统提示;上游原文和查询号保留,业务 401 不被识别为应用登录过期。
func TestSupportUpstreamErrorsPreserveDetailsAndUncertainResults(t *testing.T) {
	for _, upstream := range []int{401, 403, 408, 429, 503} {
		for _, mutation := range []bool{false, true} {
			err := &ovhsdk.APIError{Code: upstream, Message: "Fixture rejection", QueryID: "fixture-query"}
			status, payload := supportErrorResponse(err, mutation)
			wantStatus := upstream
			if upstream >= 500 {
				wantStatus = 502
			}
			wantCode := "SUPPORT_REQUEST_FAILED"
			if mutation && (upstream >= 500 || upstream == 408 || upstream == 429) {
				wantCode = "SUPPORT_SUBMISSION_UNCONFIRMED"
			}
			params := payload["params"].(gin.H)
			if status != wantStatus || payload["success"] != false || payload["code"] != wantCode || params["detail"] != err.Error() || !strings.Contains(params["detail"].(string), "fixture-query") {
				t.Fatalf("status=%d mutation=%t: %+v", status, mutation, payload)
			}
		}
	}
	_, payload := supportErrorResponse(errSupportDetailID, false)
	if payload["code"] != "SUPPORT_DETAIL_INVALID" || payload["params"] != nil {
		t.Fatal(payload)
	}
}
