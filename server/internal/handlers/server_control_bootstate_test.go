package handlers

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/types"
)

// 只有"本应用改过启动项 + 之后有一次重启完成"才算已生效;正在重启优先;
// 在别处改成救援的无法确认;硬盘启动没有记录时按默认状态视为已生效。
func TestBootEffect(t *testing.T) {
	changed := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	rec := &bootChangeRecord{BootID: 9, At: changed.Format(time.RFC3339)}
	task := func(status string, done time.Time) map[string]interface{} {
		m := map[string]interface{}{"status": status}
		if !done.IsZero() {
			m["doneDate"] = done.Format(time.RFC3339)
		}
		return m
	}
	for _, tc := range []struct {
		name     string
		bootType string
		bootID   int64
		reboot   map[string]interface{}
		record   *bootChangeRecord
		want     string
	}{
		{"rebooting wins", "rescue", 9, task("doing", time.Time{}), rec, "rebooting"},
		{"reboot done after change", "rescue", 9, task("done", changed.Add(3*time.Minute)), rec, "active"},
		{"reboot done before change", "rescue", 9, task("done", changed.Add(-time.Hour)), rec, "pending"},
		{"no reboot after change", "rescue", 9, nil, rec, "pending"},
		{"failed reboot keeps pending", "harddisk", 9, task("ovhError", changed.Add(time.Minute)), rec, "pending"},
		{"record for another boot ignored", "rescue", 7, task("done", changed.Add(time.Minute)), rec, "unknown"},
		{"rescue set elsewhere", "rescue", 9, task("done", changed), nil, "unknown"},
		{"harddisk default", "harddisk", 1, nil, nil, "active"},
	} {
		if got := bootEffect(tc.bootType, tc.bootID, tc.reboot, tc.record); got != tc.want {
			t.Errorf("%s: got %s want %s", tc.name, got, tc.want)
		}
	}
}

// 接口:读启动项类型与最新重启任务,结合本地变更记录给出 effect;只发 GET。
func TestGetBootStateEndpoint(t *testing.T) {
	st, _ := newWatchTestMonitor(t)
	st.Accounts = []types.OVHAccount{{ID: "acc", Endpoint: "ovh-eu", Zone: "IE", AppKey: "test", AppSecret: "test", ConsumerKey: "test", IsDefault: true}}
	client, err := st.OVH.ClientFor("acc")
	if err != nil {
		t.Fatal(err)
	}
	doneAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	client.Client = &http.Client{Transport: queuePreorderTransport(func(r *http.Request) (*http.Response, error) {
		body, status := "", 200
		if r.Method != http.MethodGet {
			t.Errorf("unexpected write %s %s", r.Method, r.URL.Path)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/time"):
			body = jsonNumber(time.Now().Unix())
		case strings.HasSuffix(r.URL.Path, "/boot/22"):
			body = `{"bootType":"rescue","description":"Rescue customer"}`
		case strings.HasSuffix(r.URL.Path, "/task/31"):
			body = `{"taskId":31,"status":"done","doneDate":"` + doneAt + `"}`
		case strings.HasSuffix(r.URL.Path, "/task"):
			if r.URL.Query().Get("function") != "hardReboot" {
				t.Errorf("task filter %s", r.URL.RawQuery)
			}
			body = `[12,31,20]`
		case strings.HasSuffix(r.URL.Path, "/dedicated/server/ns1"):
			body = `{"bootId":22,"powerState":"poweron","state":"ok"}`
		default:
			t.Errorf("unexpected OVH request: %s %s", r.Method, r.URL.Path)
			status, body = 404, `{}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	router := gin.New()
	router.GET("/sc/:service_name/boot-state", GetBootState(st))

	w := payRequest(router, "GET", "/sc/ns1/boot-state?account=acc", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"bootType":"rescue"`) || !strings.Contains(w.Body.String(), `"effect":"unknown"`) {
		t.Fatalf("without record %d %s", w.Code, w.Body.String())
	}
	if err := st.DB.SetKV(bootChangeKey("acc", "ns1"), bootChangeRecord{BootID: 22, At: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	w = payRequest(router, "GET", "/sc/ns1/boot-state?account=acc", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"effect":"active"`) || !strings.Contains(w.Body.String(), `"changedAt"`) {
		t.Fatalf("with record %d %s", w.Code, w.Body.String())
	}
}
