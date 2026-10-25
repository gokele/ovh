package notify

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/types"
)

func testState(t *testing.T) *app.State {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	lg := logger.New(filepath.Join(dir, "t.log"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	return app.NewState(storage.Paths{DataDir: dir}, config.New(database), lg, database)
}

// webhook 的接收端五花八门(钉钉/飞书/Bark/自建),各家读的字段名不一样。
// 同一条文本要同时出现在几个最常见的字段里,少一个就是"某一类接收端收到空消息"。
func TestWebhookPayloadCarriesTextInEveryCommonField(t *testing.T) {
	var got map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := sendWebhook(srv.URL, "有货了"); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if got["text"] != "有货了" || got["message"] != "有货了" {
		t.Errorf("text / message 字段不对: %v", got)
	}
	tc, _ := got["text_content"].(map[string]interface{})
	if tc == nil || tc["text"] != "有货了" {
		t.Errorf("text_content.text 不对: %v", got["text_content"])
	}
	fc, _ := got["content"].(map[string]interface{})
	if got["msg_type"] != "text" || fc == nil || fc["text"] != "有货了" {
		t.Errorf("飞书 msg_type/content.text 不对: %v", got)
	}
	if got["msgtype"] != "text" {
		t.Errorf("msgtype 应该是 text,实际 %v", got["msgtype"])
	}
}

// 非 2xx 必须当失败:静默吞掉的话 Broadcast 会把它算作"已送达",
// 于是所有通道都挂了的时候监控还以为通知发出去了
func TestWebhookNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "队列满了", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := sendWebhook(srv.URL, "x")
	if err == nil {
		t.Fatal("503 应该报错")
	}
	// 报错里要带上对方说了什么,否则用户只知道"失败了"却不知道为什么
	if !contains(err.Error(), "队列满了") || !contains(err.Error(), "503") {
		t.Errorf("错误信息应该带上状态码和响应内容,实际: %v", err)
	}
}

// 只配 webhook 也算"有通道可用" —— 这正是这个包存在的理由:
// 以前 Telegram 一挂,监控就整个停掉
func TestAnyAvailableWithWebhookOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	state := testState(t)
	state.Config.Set(types.Config{NotifyWebhookURL: srv.URL})

	ok, reason := AnyAvailable(state, true)
	if !ok {
		t.Errorf("只配 webhook 应该算可用,实际被拒: %s", reason)
	}
	if n := Broadcast(state, "测试", nil); n != 1 {
		t.Errorf("应该送达 1 个通道,实际 %d", n)
	}
}

// 一条都没配 = 不可用,而且要说清楚是"没配"而不是"配了但连不上"
func TestAnyAvailableWithNothingConfigured(t *testing.T) {
	state := testState(t)
	ok, reason := AnyAvailable(state, false)
	if ok {
		t.Error("什么都没配不该算可用")
	}
	if !contains(reason, "没有配置") {
		t.Errorf("原因应该说明是没配置,实际: %q", reason)
	}
}

// 配了但连不上:Broadcast 要返回 0,监控据此才知道这条通知根本没发出去
func TestBroadcastReturnsZeroWhenWebhookDown(t *testing.T) {
	state := testState(t)
	// 关掉的服务器地址:连接直接被拒
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	state.Config.Set(types.Config{NotifyWebhookURL: url})

	if n := Broadcast(state, "测试", nil); n != 0 {
		t.Errorf("通道连不上时应该返回 0,实际 %d", n)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// These requests never leave the process, including fixtures using vendor hostnames.
type webhookTransport func(*http.Request) (*http.Response, error)

func (f webhookTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWebhookBusinessResponsesAndVendorPayloads(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, tc := range []struct{ name, host, response, wantError string }{
		{"feishu accepted", "open.feishu.cn", `{"code":0,"msg":"success"}`, ""},
		{"lark accepted", "open.larksuite.com", `{"code":0}`, ""},
		{"feishu rejected", "open.feishu.cn", `{"code":19002,"data":{},"msg":"params error, msg_type need"}`, "19002"},
		{"feishu security rejected", "open.feishu.cn", `{"code":19021,"msg":"sign match fail"}`, "sign match fail"},
		{"feishu legacy accepted", "open.feishu.cn", `{"StatusCode":0,"StatusMessage":"success"}`, ""},
		{"feishu legacy rejected", "open.feishu.cn", `{"StatusCode":19024,"StatusMessage":"keyword missing"}`, "keyword missing"},
		{"feishu malformed", "open.feishu.cn", `ok`, "无效"},
		{"feishu missing result", "open.feishu.cn", `{}`, "缺少"},
		{"feishu code 200 is not success", "open.feishu.cn", `{"code":200}`, "code 200"},
		{"dingtalk accepted", "oapi.dingtalk.com", `{"errcode":0,"errmsg":"ok"}`, ""},
		{"dingtalk rejected", "oapi.dingtalk.com", `{"errcode":310000,"errmsg":"keywords not in content"}`, "keywords not in content"},
		{"quoted error code", "open.feishu.cn", `{"code":"19002","msg":"bad"}`, "19002"},
		{"custom proxy feishu response", "hooks.invalid", `{"code":19002,"msg":"rejected"}`, "19002"},
		{"custom proxy dingtalk response", "hooks.invalid", `{"errcode":310000,"errmsg":"rejected"}`, "310000"},
		{"one successful field cannot mask failure", "hooks.invalid", `{"code":0,"errcode":310000,"errmsg":"rejected"}`, "310000"},
		{"custom empty response", "hooks.invalid", ``, ""},
		{"custom text response", "hooks.invalid", `ok`, ""},
		{"custom JSON response", "hooks.invalid", `{"ok":true}`, ""},
		{"custom textual code", "hooks.invalid", `{"code":"OK"}`, ""},
		{"bark style code 200", "hooks.invalid", `{"code":200,"message":"success"}`, ""},
		{"custom error with message", "hooks.invalid", `{"code":500,"message":"queue full"}`, "queue full"},
		{"oversized response", "hooks.invalid", strings.Repeat(" ", (64<<10)+1), "过大"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			http.DefaultTransport = webhookTransport(func(r *http.Request) (*http.Response, error) {
				var payload map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Fatal("missing JSON header")
				}
				if tc.host == "oapi.dingtalk.com" {
					text, _ := payload["text"].(map[string]interface{})
					if text == nil || text["content"] != "有货了" || payload["msgtype"] != "text" {
						t.Fatalf("invalid DingTalk payload: %v", payload)
					}
				} else if payload["text"] != "有货了" || payload["message"] != "有货了" {
					t.Fatalf("legacy fields changed: %v", payload)
				}
				content, _ := payload["content"].(map[string]interface{})
				if content == nil || content["text"] != "有货了" || payload["msg_type"] != "text" {
					t.Fatalf("invalid Feishu payload: %v", payload)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.response)), Request: r}, nil
			})
			err := sendWebhook("https://"+tc.host+"/fixture", "有货了")
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected %q, got %v", tc.wantError, err)
			}
		})
	}
}

func TestWebhookBusinessErrorNotCountedAsDelivered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":19002,"msg":"msg_type need"}`)
	}))
	defer srv.Close()
	state := testState(t)
	state.Config.Set(types.Config{NotifyWebhookURL: srv.URL})
	delivered, channels := BroadcastWithStatus(state, "测试", nil)
	if delivered != 0 || len(channels) != 2 || channels[1].OK || !strings.Contains(channels[1].Detail, "19002") {
		t.Fatalf("false success: %d %+v", delivered, channels)
	}
	if ok, reason := AnyAvailable(state, true); ok || !strings.Contains(reason, "19002") {
		t.Fatalf("false healthy: %v %s", ok, reason)
	}
}
