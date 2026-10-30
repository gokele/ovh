package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
)

// renewalFixture 使用虚构美国站账户和离线 transport,记录所有写入以排除误终止。
func renewalFixture(t *testing.T, modes string, legacyStatus int) (*app.State, *gin.Engine, *[]supportCall) {
	t.Helper()
	st, _, _ := supportFixture(t)
	client, err := st.OVH.ClientFor("us")
	if err != nil {
		t.Fatal(err)
	}
	calls := []supportCall{}
	client.Client.Transport = queuePreorderTransport(func(req *http.Request) (*http.Response, error) {
		path := strings.TrimPrefix(req.URL.Path, "/1.0")
		body, code := `{}`, 200
		if strings.HasSuffix(path, "/auth/time") {
			body = fmt.Sprint(time.Now().Unix())
		} else if req.Method == "GET" && strings.HasSuffix(path, "/serviceInfos") {
			body = `{"serviceId":123,"renew":{"automatic":true,"forced":false,"period":1,"manualPayment":false}}`
		} else if req.Method == "GET" && path == "/services/123" {
			if modes == "unreadable" {
				code = 403
				body = `{"message":"fixture no modern read permission"}`
			} else {
				body = fmt.Sprintf(`{"billing":{"renew":{"capacities":{"mode":%s},"current":{"mode":"manual","period":"P1Y"}},"lifecycle":{"current":{"pendingActions":["terminateAtExpirationDate"],"terminationDate":"2027-01-01"}}}}`, modes)
			}
		} else if req.Method == "PUT" {
			payload := map[string]interface{}{}
			if req.Body != nil {
				_ = json.NewDecoder(req.Body).Decode(&payload)
			}
			calls = append(calls, supportCall{Method: req.Method, Path: path, Body: payload})
			if strings.HasSuffix(path, "/serviceInfos") {
				code = legacyStatus
				if code >= 400 {
					body = `{"message":"Unknown renew automatic parameter"}`
				}
			}
		} else {
			t.Errorf("unexpected request %s %s", req.Method, path)
			code = 404
		}
		return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	r := gin.New()
	r.GET("/dedicated/:service_name", GetServiceInfo(st))
	r.PUT("/dedicated/:service_name", UpdateServiceRenewal(st))
	r.GET("/vps/:service_name", GetVpsServiceInfo(st))
	r.PUT("/vps/:service_name", UpdateVpsRenewal(st))
	return st, r, &calls
}

// 两种产品都按具体服务能力判断,新模式和周期覆盖过期的旧字段。
func TestRenewalUsesServiceCapabilitiesInsteadOfAccountRegion(t *testing.T) {
	for _, product := range []string{"dedicated", "vps"} {
		for _, tc := range []struct {
			modes   string
			blocked bool
		}{{`["automatic","manual"]`, false}, {`["automatic"]`, true}, {`[]`, true}} {
			t.Run(product+tc.modes, func(t *testing.T) {
				_, r, calls := renewalFixture(t, tc.modes, 200)
				res := payRequest(r, "GET", "/"+product+"/fixture?account=us", "")
				var result struct {
					ServiceInfo struct {
						RenewalManualUnsupported bool
						RenewalType              bool
						RenewalPeriod            int
						TerminationScheduled     bool
						TerminationAction        string
					}
				}
				if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				info := result.ServiceInfo
				if res.Code != 200 || info.RenewalManualUnsupported != tc.blocked || info.RenewalType || info.RenewalPeriod != 12 || !info.TerminationScheduled || info.TerminationAction != "terminateAtExpirationDate" {
					t.Fatalf("unexpected info: %s", res.Body)
				}
				if len(*calls) != 0 {
					t.Fatal("read-only preflight changed a service")
				}
			})
		}
	}
}

// 新接口使用 mode 和 ISO 8601 period,不能退化成终止服务或丢失用户选择。
func TestRenewalModernFallbackUsesDocumentedPayload(t *testing.T) {
	for _, product := range []string{"dedicated", "vps"} {
		for _, mode := range []string{"manual", "auto"} {
			t.Run(product+mode, func(t *testing.T) {
				_, r, calls := renewalFixture(t, `["automatic","manual"]`, 400)
				res := payRequest(r, "PUT", "/"+product+"/fixture?account=us", `{"mode":"`+mode+`","period":12}`)
				if res.Code != 200 || len(*calls) != 2 {
					t.Fatalf("%d %s calls=%+v", res.Code, res.Body, *calls)
				}
				got := (*calls)[1]
				renew, _ := got.Body["renew"].(map[string]interface{})
				want := mode
				if mode == "auto" {
					want = "automatic"
				}
				if got.Path != "/services/123" || len(got.Body) != 1 || len(renew) != 2 || renew["mode"] != want || renew["period"] != "P12M" {
					t.Fatalf("unsafe/obsolete fallback: %+v", got)
				}
			})
		}
	}
}

// 没指定周期时必须保持新接口的当前周期,不能用过期的旧周期覆盖。
func TestRenewalPreservesAuthoritativePeriodWhenOmitted(t *testing.T) {
	for _, product := range []string{"dedicated", "vps"} {
		_, r, calls := renewalFixture(t, `["automatic","manual"]`, 400)
		response := payRequest(r, "PUT", "/"+product+"/fixture?account=us", `{"mode":"manual"}`)
		if response.Code != 200 || len(*calls) != 2 {
			t.Fatal(response.Code, response.Body, calls)
		}
		legacy := (*calls)[0].Body["renew"].(map[string]interface{})
		modern := (*calls)[1].Body["renew"].(map[string]interface{})
		if legacy["period"] != float64(12) || modern["period"] != "P1Y" {
			t.Fatalf("period was changed: %+v", calls)
		}
	}
}

// 明确不支持时不写入;读取失败是未知,不能按地区拒绝;损坏参数不得执行。
func TestRenewalUnsupportedUnknownAndMalformedRequests(t *testing.T) {
	for _, product := range []string{"dedicated", "vps"} {
		for _, tc := range []struct {
			modes, body    string
			status, writes int
		}{
			{`["automatic"]`, `{"mode":"manual","period":12}`, 400, 0},
			{`unreadable`, `{"mode":"manual","period":12}`, 200, 1},
			{`null`, `{"mode":"manual","period":12}`, 200, 1},
			{`["automatic","manual"]`, `{"mode":"manual","period":"invalid"}`, 400, 0},
			{`["automatic","manual"]`, `{"mode":"manual","period":-1}`, 400, 0},
		} {
			t.Run(product+tc.modes+tc.body, func(t *testing.T) {
				_, r, calls := renewalFixture(t, tc.modes, 200)
				res := payRequest(r, "PUT", "/"+product+"/fixture?account=us", tc.body)
				if res.Code != tc.status || len(*calls) != tc.writes {
					t.Fatalf("%d %s calls=%+v", res.Code, res.Body, *calls)
				}
			})
		}
	}
}
