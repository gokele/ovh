package handlers

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// safetyFixture 截获所有 SDK 请求,禁止测试访问真实服务器或提交真实撤单。
func safetyFixture(t *testing.T, deadline, domain string, readCode, writeCode int) (*gin.Engine, *[]supportCall) {
	t.Helper()
	st, _, _ := supportFixture(t)
	client, err := st.OVH.ClientFor("eu")
	if err != nil {
		t.Fatal(err)
	}
	calls := []supportCall{}
	client.Client.Transport = queuePreorderTransport(func(req *http.Request) (*http.Response, error) {
		path := strings.TrimPrefix(req.URL.Path, "/1.0")
		body, code := `{}`, 200
		payload := map[string]interface{}{}
		if req.Body != nil {
			_ = json.NewDecoder(req.Body).Decode(&payload)
		}
		if path != "/auth/time" {
			calls = append(calls, supportCall{Method: req.Method, Path: path, Body: payload})
		}
		switch {
		case path == "/auth/time":
			body = fmt.Sprint(time.Now().Unix())
		case req.Method == "GET" && path == "/dedicated/server/fixture":
			body = `{"bootId":1,"rescueMail":"old@example.com"}`
			code = readCode
		case req.Method == "GET" && path == "/me":
			body = `{"email":"contact@example.com"}`
		case req.Method == "GET" && path == "/dedicated/server/fixture/boot":
			if req.URL.Query().Get("bootType") == "harddisk" {
				body = `[1]`
			} else {
				body = `[2]`
			}
		case req.Method == "GET" && strings.HasPrefix(path, "/dedicated/server/fixture/boot/"):
			if strings.HasSuffix(path, "/2") {
				body = `{"bootId":2,"bootType":"rescue","kernel":"rescue-customer"}`
			} else {
				body = `{"bootId":1,"bootType":"harddisk"}`
			}
		case req.Method == "GET" && path == "/me/order/123":
			body = fmt.Sprintf(`{"retractionDate":%q}`, deadline)
			code = readCode
		case req.Method == "GET" && path == "/me/order/123/details":
			body = `[8]`
		case req.Method == "GET" && path == "/me/order/123/details/8":
			body = fmt.Sprintf(`{"domain":%q,"cancelled":false}`, domain)
		case req.Method == "PUT" && path == "/dedicated/server/fixture":
			code = writeCode
		case req.Method == "POST" && (path == "/dedicated/server/fixture/reboot" || path == "/me/order/123/retraction"):
			code = writeCode
		default:
			t.Errorf("unexpected operation %s %s", req.Method, path)
			code = 404
		}
		if code >= 400 {
			body = `{"message":"fictional failure"}`
		}
		return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	r := gin.New()
	r.GET("/server/:service_name/rescue", GetRescueStatus(st))
	r.POST("/server/:service_name/rescue", EnterRescue(st))
	r.POST("/server/:service_name/rescue/exit", ExitRescue(st))
	r.POST("/server/:service_name/retraction", PostRetraction(st))
	return r, &calls
}

// writesOnly 仅保留会改变服务或订单的请求。
func writesOnly(calls []supportCall) []supportCall {
	var result []supportCall
	for _, c := range calls {
		if c.Method != "GET" {
			result = append(result, c)
		}
	}
	return result
}

// 即使解析出了 confirm:true,未完成邮箱或剩余字段解析失败也必须停止。
func TestRescueRejectsIncompleteRecipientAndMalformedConfirmation(t *testing.T) {
	for _, tc := range []struct{ path, body string }{
		{"", `{"confirm":true}`}, {"", `{"confirm":true,"email":42}`}, {"", `{"confirm":true,"email":"test@gma"}`},
		{"", `{"confirm":true,"email":"Name <test@example.com>"}`}, {"", `{"confirm":false,"email":"test@example.com"}`},
		{"/exit", `{"confirm":true,"expectedBoot":"not-a-number"}`}, {"/exit", `{"confirm":false}`},
	} {
		r, calls := safetyFixture(t, "2099-01-01T00:00:00Z", "fixture", 200, 200)
		w := payRequest(r, "POST", "/server/fixture/rescue"+tc.path+"?account=eu", tc.body)
		if w.Code != 400 || len(*calls) != 0 {
			t.Fatalf("%s: %d %s calls=%+v", tc.body, w.Code, w.Body, *calls)
		}
	}
}

// 打开只读,明确确认后先写入邮箱再重启一次;默认邮箱不能沿用旧 rescueMail。
func TestRescueReadOnlyStatusAndExplicitRecipient(t *testing.T) {
	for _, tc := range []struct{ body, email string }{
		{`{"confirm":true,"email":"new@example.com","expectedBoot":1}`, "new@example.com"},
		{`{"confirm":true,"useDefaultEmail":true,"expectedBoot":1}`, "contact@example.com"},
	} {
		r, calls := safetyFixture(t, "2099-01-01T00:00:00Z", "fixture", 200, 200)
		w := payRequest(r, "GET", "/server/fixture/rescue?account=eu", "")
		if w.Code != 200 || len(writesOnly(*calls)) != 0 || !strings.Contains(w.Body.String(), `"rescueConfigured":false`) {
			t.Fatal(w.Code, w.Body, *calls)
		}
		w = payRequest(r, "POST", "/server/fixture/rescue?account=eu", tc.body)
		writes := writesOnly(*calls)
		if w.Code != 200 || len(writes) != 2 || writes[0].Method != "PUT" || writes[0].Body["rescueMail"] != tc.email || writes[0].Body["bootId"] != float64(2) || writes[1].Path != "/dedicated/server/fixture/reboot" {
			t.Fatal(w.Code, w.Body, writes)
		}
	}
}

// 配置变化、查询失败、非法启动项或设置失败均不能继续发重启请求。
func TestRescueStopsBeforeRebootOnChangedOrInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		path, body                        string
		readCode, writeCode, want, writes int
	}{
		{"", `{"confirm":true,"email":"new@example.com","expectedBoot":9}`, 200, 200, 409, 0},
		{"/exit", `{"confirm":true,"expectedBoot":9}`, 200, 200, 409, 0},
		{"", `{"confirm":true,"email":"new@example.com","expectedBoot":1}`, 403, 200, 502, 0},
		{"", `{"confirm":true,"email":"new@example.com","bootId":1,"expectedBoot":1}`, 200, 200, 400, 0},
		{"", `{"confirm":true,"email":"new@example.com","expectedBoot":1}`, 200, 500, 500, 1},
	} {
		r, calls := safetyFixture(t, "2099-01-01T00:00:00Z", "fixture", tc.readCode, tc.writeCode)
		w := payRequest(r, "POST", "/server/fixture/rescue"+tc.path+"?account=eu", tc.body)
		writes := writesOnly(*calls)
		if w.Code != tc.want || len(writes) != tc.writes {
			t.Fatal(tc, w.Code, w.Body, writes)
		}
		for _, call := range writes {
			if call.Method == "POST" {
				t.Fatal("unexpected reboot", call)
			}
		}
	}
}

// 官网式确认固定 other,请求只到对应整单撤回接口,不调用终止服务接口。
func TestRetractionUsesOfficialWholeOrderEndpoint(t *testing.T) {
	r, calls := safetyFixture(t, "2099-01-01T00:00:00Z", "fixture", 200, 200)
	w := payRequest(r, "POST", "/server/fixture/retraction?account=eu", `{"confirm":true,"orderId":123}`)
	writes := writesOnly(*calls)
	if w.Code != 200 || len(writes) != 1 || writes[0].Path != "/me/order/123/retraction" || len(writes[0].Body) != 1 || writes[0].Body["reason"] != "other" {
		t.Fatal(w.Code, w.Body, writes)
	}
}

// 过期、缺资格、服务归属不符、读取失败或确认错误时不得撤单。
func TestRetractionRevalidatesEligibilityAndOrderIdentity(t *testing.T) {
	for _, tc := range []struct {
		deadline, domain, body string
		readCode, want         int
	}{
		{"2000-01-01T00:00:00Z", "fixture", `{"confirm":true,"orderId":123}`, 200, 409},
		{"", "fixture", `{"confirm":true,"orderId":123}`, 200, 409},
		{"2099-01-01T00:00:00Z", "other-server", `{"confirm":true,"orderId":123}`, 200, 409},
		{"2099-01-01T00:00:00Z", "fixture", `{"confirm":true,"orderId":123}`, 403, 409},
		{"2099-01-01T00:00:00Z", "fixture", `{"confirm":true}`, 200, 400},
		{"2099-01-01T00:00:00Z", "fixture", `{"confirm":false,"orderId":123}`, 200, 400},
		{"2099-01-01T00:00:00Z", "fixture", `{"confirm":true,"orderId":"invalid"}`, 200, 400},
	} {
		r, calls := safetyFixture(t, tc.deadline, tc.domain, tc.readCode, 200)
		w := payRequest(r, "POST", "/server/fixture/retraction?account=eu", tc.body)
		if w.Code != tc.want || len(writesOnly(*calls)) != 0 {
			t.Fatal(tc, w.Code, w.Body, *calls)
		}
	}
}

// 退出救援同样只在明确确认后改回硬盘启动并提交一次重启。
func TestRescueExitAfterExplicitConfirmation(t *testing.T) {
	r, calls := safetyFixture(t, "2099-01-01T00:00:00Z", "fixture", 200, 200)
	w := payRequest(r, "POST", "/server/fixture/rescue/exit?account=eu", `{"confirm":true,"expectedBoot":1}`)
	writes := writesOnly(*calls)
	if w.Code != 200 || len(writes) != 2 || writes[0].Method != "PUT" || writes[0].Body["bootId"] != float64(1) || writes[1].Path != "/dedicated/server/fixture/reboot" {
		t.Fatal(w.Code, w.Body, writes)
	}
}
