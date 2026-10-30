package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// chatFixture 不读取真实凭据;只允许 /me、/auth/time 和一次令牌申请。
func chatFixture(t *testing.T, account string, meCode, tokenCode int, customer, token string) (*gin.Engine, *int) {
	t.Helper()
	st, _, _ := supportFixture(t)
	client, err := st.OVH.ClientFor(account)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	client.Client.Transport = queuePreorderTransport(func(req *http.Request) (*http.Response, error) {
		path := strings.TrimPrefix(req.URL.Path, "/1.0")
		code, body := 200, `{}`
		expectedHost := "eu.api.ovh.com"
		if account == "us" {
			expectedHost = "api.us.ovhcloud.com"
		}
		if req.URL.Host != expectedHost {
			t.Errorf("wrong account API host: %s", req.URL.Host)
		}
		switch {
		case path == "/auth/time":
			body = fmt.Sprint(time.Now().Unix())
		case path == "/me" && req.Method == "GET":
			code = meCode
			body = fmt.Sprintf(`{"nichandle":%q}`, customer)
		case path == "/auth/token" && req.Method == "POST":
			count++
			code = tokenCode
			body = fmt.Sprintf(`{"token":%q}`, token)
		default:
			t.Errorf("unexpected real service operation: %s %s", req.Method, path)
			code = 404
		}
		if code >= 400 {
			body = `{"message":"fictional permission failure secret-must-not-echo"}`
		}
		return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	r := gin.New()
	r.POST("/chat", OfficialChatSession(st))
	return r, &count
}

// 令牌、客户编号和 API 大区必须属于指定账户,不能默默落回默认账户。
func TestOfficialChatPinsAccountAndUsesOneTimeToken(t *testing.T) {
	for _, account := range []string{"eu", "us"} {
		t.Run(account, func(t *testing.T) {
			r, count := chatFixture(t, account, 200, 200, "customer-"+account, "one-time-"+account)
			w := payRequest(r, "POST", "/chat?account="+account, `{"product":"dedicated"}`)
			var result struct {
				AccountID    string `json:"accountId"`
				CustomerCode string `json:"customerCode"`
				LaunchURL    string `json:"launchUrl"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || *count != 1 || result.AccountID != account || result.CustomerCode != "customer-"+account {
				t.Fatal(w.Code, w.Body)
			}
			u, err := url.Parse(result.LaunchURL)
			if err != nil {
				t.Fatal(err)
			}
			region := "EU"
			if account == "us" {
				region = "US"
			}
			if u.Scheme != "https" || u.Host != "agent-chat.ovhcloud.com" || u.Path != "/sn_va_web_client_app_embed.do" || u.Query().Get("sysparm_session_id") != "one-time-"+account || u.Query().Get("sysparm_region") != region || u.Query().Get("sysparm_queue") != "dedicated" {
				t.Fatal("wrong official session URL")
			}
			if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("authentication response is cacheable")
			}
		})
	}
}

// 参数错误、身份缺失和凭据失效必须停止;令牌失败不自动重试。
func TestOfficialChatRejectsInvalidAndUnavailableSessions(t *testing.T) {
	for _, tc := range []struct {
		name, body, customer, token    string
		meCode, tokenCode, want, count int
		code                           string
	}{
		{"bad product", `{"product":"billing"}`, "c", "t", 200, 200, 400, 0, "CHAT_INVALID_PRODUCT"},
		{"bad JSON", `{"product":17}`, "c", "t", 200, 200, 400, 0, "CHAT_INVALID_PRODUCT"},
		{"no identity", `{"product":"vps"}`, "", "t", 200, 200, 502, 0, "CHAT_IDENTITY_MISSING"},
		{"me unauthorized", `{"product":"vps"}`, "c", "t", 401, 200, 401, 0, "CHAT_REQUEST_FAILED"},
		{"token forbidden", `{"product":"vps"}`, "c", "t", 200, 403, 403, 1, "CHAT_REQUEST_FAILED"},
		{"token outage", `{"product":"vps"}`, "c", "t", 200, 500, 502, 1, "CHAT_REQUEST_FAILED"},
		{"empty token", `{"product":"vps"}`, "c", "", 200, 200, 502, 1, "CHAT_TOKEN_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, count := chatFixture(t, "us", tc.meCode, tc.tokenCode, tc.customer, tc.token)
			w := payRequest(r, "POST", "/chat?account=us", tc.body)
			if w.Code != tc.want || *count != tc.count || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "launchUrl") || strings.Contains(w.Body.String(), "secret-must-not-echo") {
				t.Fatal(w.Code, w.Body, *count)
			}
			if !strings.Contains(w.Body.String(), `"success":false`) {
				t.Fatal("business auth failure must not expire the application login")
			}
		})
	}
}
