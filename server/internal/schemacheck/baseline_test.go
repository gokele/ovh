package schemacheck

// OVH 接口漂移基线。
//
// 由来:用户反复问"OVH 的接口是不是改了",而每次都靠人工拉 schema、人工比对,
// 既慢又容易漏。这里把"代码在用的每个端点的签名"固化成基线文件,
// 用一条联网测试跟 OVH 最新 schema 对比 —— 变了就红,并指出变在哪。
//
// 覆盖的是**签名**(是否存在 / apiStatus / 参数名+类型+必填 / 响应类型),
// 不是全量 schema:全量 4.8MB 入库没意义,而且噪声大到没人会去看 diff。
//
// 默认跳过(需要联网、且 OVH 偶尔抽风)。跑法:
//
//	go test ./internal/schemacheck/ -run TestOVHSchemaDrift -v -tags=netcheck
//
// 基线更新(确认变化是良性之后):
//
//	go test ./internal/schemacheck/ -run TestOVHSchemaDrift -tags=netcheck -update

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "把当前 OVH schema 写成新基线")

// regions 三个站点各自的 API 根。三区是独立系统,同一个端点可能只在其中两个存在。
var regions = map[string]string{
	"EU": "https://eu.api.ovh.com/1.0",
	"US": "https://api.us.ovhcloud.com/1.0",
	"CA": "https://ca.api.ovh.com/1.0",
}

// namespaces 项目实际用到的命名空间
var namespaces = []string{
	"dedicated/server",
	"dedicated/installationTemplate",
	"vps",
	"order",
	"me",
	"services",
	"ip",
}

// endpointSig 一个端点在某个区的签名。字段都是"变了就该有人看一眼"的那些。
type endpointSig struct {
	Status       string   `json:"status"`                // PRODUCTION / BETA / DEPRECATED / ALPHA
	Deprecated   string   `json:"deprecated,omitempty"`  // 废弃日期
	Deletion     string   `json:"deletion,omitempty"`    // 删除日期
	Replacement  string   `json:"replacement,omitempty"` // OVH 指定的替代端点
	ResponseType string   `json:"response,omitempty"`    // 响应模型
	Params       []string `json:"params,omitempty"`      // paramType:name:dataType:required
}

// baseline 形状: {"GET /vps/{serviceName}": {"EU": sig, "CA": sig}}
type baseline map[string]map[string]endpointSig

func baselinePath() string { return filepath.Join("testdata", "ovh-endpoints.json") }

// usedEndpoints 代码在用的端点,漂移检测就盯这一批。
//
// 这份清单**必须覆盖代码里每一个 OVH 调用** —— usage_test.go 的
// TestDriftListCoversEveryCalledEndpoint 每次 go test 都会用 AST 扫一遍源码来核对,
// 漏了当场失败并打出该补哪几行。
//
// 以前这里是纯手工维护的,理由写的是"自动扫路径拼接不可靠"。那句话对正则成立、
// 对 AST 不成立,而手工的代价是实测只覆盖了实际调用的一半多一点:
// POST /me/order/{orderId}/retraction、整套 features/backupFTP、ola/aggregation、
// VPS 的 reinstall/images/tasks 当时都不在里面 —— OVH 改了它们的签名不会有人知道。
//
// 清单里有、AST 扫不到的是正常的:目录和 VPS 机房可用性走公开 URL(裸 HTTP,
// 不经 OVH 客户端),它们同样需要监控。TestDriftListExtrasAreReported 会把这批列出来。
var usedEndpoints = []string{
	// —— 独服 ——
	"GET /dedicated/server",
	"GET /dedicated/server/datacenter/availabilities",
	"GET /dedicated/server/{serviceName}",
	"PUT /dedicated/server/{serviceName}",
	"GET /dedicated/server/{serviceName}/backupCloudOfferDetails",
	"GET /dedicated/server/{serviceName}/biosSettings",
	"GET /dedicated/server/{serviceName}/biosSettings/sgx",
	"GET /dedicated/server/{serviceName}/boot",
	"GET /dedicated/server/{serviceName}/boot/{bootId}",
	"GET /dedicated/server/{serviceName}/burst",
	"PUT /dedicated/server/{serviceName}/burst",
	"POST /dedicated/server/{serviceName}/changeContact",
	"POST /dedicated/server/{serviceName}/confirmTermination",
	"DELETE /dedicated/server/{serviceName}/features/backupCloud",
	"GET /dedicated/server/{serviceName}/features/backupCloud",
	"POST /dedicated/server/{serviceName}/features/backupCloud",
	"POST /dedicated/server/{serviceName}/features/backupCloud/password",
	"DELETE /dedicated/server/{serviceName}/features/backupFTP",
	"GET /dedicated/server/{serviceName}/features/backupFTP",
	"POST /dedicated/server/{serviceName}/features/backupFTP",
	"GET /dedicated/server/{serviceName}/features/backupFTP/access",
	"POST /dedicated/server/{serviceName}/features/backupFTP/access",
	"GET /dedicated/server/{serviceName}/features/backupFTP/authorizableBlocks",
	"POST /dedicated/server/{serviceName}/features/backupFTP/password",
	"GET /dedicated/server/{serviceName}/features/firewall",
	"PUT /dedicated/server/{serviceName}/features/firewall",
	"GET /dedicated/server/{serviceName}/features/ipmi",
	"GET /dedicated/server/{serviceName}/features/ipmi/access",
	"POST /dedicated/server/{serviceName}/features/ipmi/access",
	"GET /dedicated/server/{serviceName}/install/compatibleTemplates",
	"GET /dedicated/server/{serviceName}/install/hardwareRaidProfile",
	"GET /dedicated/server/{serviceName}/install/status",
	"GET /dedicated/server/{serviceName}/intervention",
	"GET /dedicated/server/{serviceName}/intervention/{interventionId}",
	"GET /dedicated/server/{serviceName}/ipCanBeMovedTo",
	"GET /dedicated/server/{serviceName}/ipCountryAvailable",
	"POST /dedicated/server/{serviceName}/ipMove",
	"GET /dedicated/server/{serviceName}/ips",
	"GET /dedicated/server/{serviceName}/license/compliantWindows",
	"GET /dedicated/server/{serviceName}/license/compliantWindowsSqlServer",
	"GET /dedicated/server/{serviceName}/mrtg",
	"GET /dedicated/server/{serviceName}/networkInterfaceController",
	"GET /dedicated/server/{serviceName}/networkInterfaceController/{mac}/mrtg",
	"POST /dedicated/server/{serviceName}/ola/aggregation",
	"POST /dedicated/server/{serviceName}/ola/reset",
	"GET /dedicated/server/{serviceName}/ongoing",
	"GET /dedicated/server/{serviceName}/option",
	"GET /dedicated/server/{serviceName}/orderable/bandwidth",
	"GET /dedicated/server/{serviceName}/orderable/ip",
	"GET /dedicated/server/{serviceName}/orderable/traffic",
	"GET /dedicated/server/{serviceName}/plannedIntervention",
	"GET /dedicated/server/{serviceName}/plannedIntervention/{interventionId}",
	"POST /dedicated/server/{serviceName}/reboot",
	"POST /dedicated/server/{serviceName}/reinstall",
	"GET /dedicated/server/{serviceName}/secondaryDnsDomains",
	"POST /dedicated/server/{serviceName}/secondaryDnsDomains",
	"DELETE /dedicated/server/{serviceName}/secondaryDnsDomains/{domain}",
	"GET /dedicated/server/{serviceName}/serviceInfos",
	"PUT /dedicated/server/{serviceName}/serviceInfos",
	"GET /dedicated/server/{serviceName}/specifications/hardware",
	"GET /dedicated/server/{serviceName}/specifications/ip",
	"GET /dedicated/server/{serviceName}/specifications/network",
	"GET /dedicated/server/{serviceName}/spla",
	"POST /dedicated/server/{serviceName}/spla",
	"POST /dedicated/server/{serviceName}/support/replace/cooling",
	"POST /dedicated/server/{serviceName}/support/replace/hardDiskDrive",
	"POST /dedicated/server/{serviceName}/support/replace/memory",
	"GET /dedicated/server/{serviceName}/task",
	"GET /dedicated/server/{serviceName}/task/{taskId}",
	"GET /dedicated/server/{serviceName}/task/{taskId}/availableTimeslots",
	"POST /dedicated/server/{serviceName}/task/{taskId}/cancel",
	"POST /dedicated/server/{serviceName}/task/{taskId}/schedule",
	"POST /dedicated/server/{serviceName}/terminate",
	"GET /dedicated/server/{serviceName}/virtualMac",
	"POST /dedicated/server/{serviceName}/virtualMac",
	"GET /dedicated/server/{serviceName}/virtualNetworkInterface",
	"POST /dedicated/server/{serviceName}/virtualNetworkInterface/{uuid}/disable",
	"POST /dedicated/server/{serviceName}/virtualNetworkInterface/{uuid}/enable",
	"GET /dedicated/server/{serviceName}/vrack",
	"DELETE /dedicated/server/{serviceName}/vrack/{vrack}",

	// —— 安装模板 ——
	"GET /dedicated/installationTemplate",
	"GET /dedicated/installationTemplate/{templateName}",
	"GET /dedicated/installationTemplate/{templateName}/partitionScheme",
	"GET /dedicated/installationTemplate/{templateName}/partitionScheme/{schemeName}",
	"GET /dedicated/installationTemplate/{templateName}/partitionScheme/{schemeName}/partition",
	"GET /dedicated/installationTemplate/{templateName}/partitionScheme/{schemeName}/partition/{mountpoint}",

	// —— VPS ——
	"GET /vps",
	"GET /vps/order/rule/datacenter",
	"GET /vps/{serviceName}",
	"PUT /vps/{serviceName}",
	"GET /vps/{serviceName}/automatedBackup",
	"POST /vps/{serviceName}/changeContact",
	"POST /vps/{serviceName}/confirmTermination",
	"POST /vps/{serviceName}/createSnapshot",
	"GET /vps/{serviceName}/datacenter",
	"GET /vps/{serviceName}/distribution",
	"POST /vps/{serviceName}/getConsoleUrl",
	"GET /vps/{serviceName}/images/available",
	"GET /vps/{serviceName}/images/current",
	"GET /vps/{serviceName}/ips",
	"PUT /vps/{serviceName}/ips/{ipAddress}",
	"GET /vps/{serviceName}/option",
	"DELETE /vps/{serviceName}/option/{option}",
	"POST /vps/{serviceName}/reboot",
	"POST /vps/{serviceName}/rebuild",
	"POST /vps/{serviceName}/reinstall",
	"GET /vps/{serviceName}/secondaryDnsDomains",
	"POST /vps/{serviceName}/secondaryDnsDomains",
	"DELETE /vps/{serviceName}/secondaryDnsDomains/{domain}",
	"GET /vps/{serviceName}/serviceInfos",
	"PUT /vps/{serviceName}/serviceInfos",
	"POST /vps/{serviceName}/setPassword",
	"DELETE /vps/{serviceName}/snapshot",
	"GET /vps/{serviceName}/snapshot",
	"PUT /vps/{serviceName}/snapshot",
	"POST /vps/{serviceName}/snapshot/revert",
	"POST /vps/{serviceName}/start",
	"GET /vps/{serviceName}/status",
	"POST /vps/{serviceName}/stop",
	"GET /vps/{serviceName}/tasks",
	"GET /vps/{serviceName}/tasks/{id}",
	"GET /vps/{serviceName}/templates",
	"POST /vps/{serviceName}/terminate",

	// —— 下单 / 目录 ——
	"POST /order/cart",
	"DELETE /order/cart/{cartId}",
	"POST /order/cart/{cartId}/assign",
	"POST /order/cart/{cartId}/checkout",
	"GET /order/cart/{cartId}/eco",
	"POST /order/cart/{cartId}/eco",
	"GET /order/cart/{cartId}/eco/options",
	"POST /order/cart/{cartId}/eco/options",
	"POST /order/cart/{cartId}/item/{itemId}/configuration",
	"GET /order/cart/{cartId}/item/{itemId}/requiredConfiguration",
	"GET /order/cart/{cartId}/summary",
	"GET /order/cart/{cartId}/vps",
	"POST /order/cart/{cartId}/vps",
	"GET /order/catalog/public/eco",
	"GET /order/catalog/public/vps",

	// —— 账户 ——
	"GET /me",
	"GET /me/bill",
	"GET /me/credit/balance",
	"GET /me/notification/email/history",
	"GET /me/order",
	"GET /me/order/{orderId}",
	"GET /me/order/{orderId}/details",
	"GET /me/order/{orderId}/details/{orderDetailId}",
	"POST /me/order/{orderId}/retraction",
	"GET /me/order/{orderId}/status",
	"GET /me/subAccount",
	"GET /me/task/contactChange",
	"GET /me/task/contactChange/{id}",
	"POST /me/task/contactChange/{id}/accept",
	"POST /me/task/contactChange/{id}/refuse",
	"POST /me/task/contactChange/{id}/resendEmail",

	// —— 服务(续费、终止、承诺期) ——
	"GET /services/{serviceId}",
	"PUT /services/{serviceId}",
	"GET /services/{serviceId}/billing/engagement",
	"GET /services/{serviceId}/billing/engagement/available",
	"PUT /services/{serviceId}/billing/engagement/endRule",
	"DELETE /services/{serviceId}/billing/engagement/request",
	"GET /services/{serviceId}/billing/engagement/request",
	"POST /services/{serviceId}/billing/engagement/request",

	// —— IP ——
	"GET /ip",
	"GET /ip/{ip}",
	"GET /ip/{ip}/mitigation",
	"POST /ip/{ip}/mitigation",
	"DELETE /ip/{ip}/mitigation/{ipOnMitigation}",
	"GET /ip/{ip}/mitigation/{ipOnMitigation}",
	"GET /ip/{ip}/reverse",
	"POST /ip/{ip}/reverse",
	"DELETE /ip/{ip}/reverse/{ipReverse}",
	"GET /ip/{ip}/reverse/{ipReverse}",
}

func fetchSchema(region, ns string) (map[string]interface{}, error) {
	url := regions[region] + "/" + ns + ".json"
	c := &http.Client{Timeout: 60 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s %s: HTTP %d", region, ns, resp.StatusCode)
	}
	var doc map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// collect 把三区 schema 拍成 {endpoint: {region: sig}}
func collect(t *testing.T) baseline {
	t.Helper()
	want := map[string]bool{}
	for _, e := range usedEndpoints {
		want[e] = true
	}
	out := baseline{}
	for region := range regions {
		for _, ns := range namespaces {
			doc, err := fetchSchema(region, ns)
			if err != nil {
				t.Fatalf("拉取 %s/%s 失败: %v", region, ns, err)
			}
			apis, _ := doc["apis"].([]interface{})
			for _, aRaw := range apis {
				a, _ := aRaw.(map[string]interface{})
				path, _ := a["path"].(string)
				ops, _ := a["operations"].([]interface{})
				for _, oRaw := range ops {
					o, _ := oRaw.(map[string]interface{})
					method, _ := o["httpMethod"].(string)
					key := method + " " + path
					if !want[key] {
						continue
					}
					sig := endpointSig{}
					if st, ok := o["apiStatus"].(map[string]interface{}); ok {
						sig.Status, _ = st["value"].(string)
						sig.Deprecated, _ = st["deprecatedDate"].(string)
						sig.Deletion, _ = st["deletionDate"].(string)
						sig.Replacement, _ = st["replacement"].(string)
					}
					sig.ResponseType, _ = o["responseType"].(string)
					if ps, ok := o["parameters"].([]interface{}); ok {
						for _, pRaw := range ps {
							p, _ := pRaw.(map[string]interface{})
							name, _ := p["name"].(string)
							sig.Params = append(sig.Params, fmt.Sprintf("%v:%s:%v:%v",
								p["paramType"], name, p["dataType"], p["required"]))
						}
						sort.Strings(sig.Params)
					}
					if out[key] == nil {
						out[key] = map[string]endpointSig{}
					}
					out[key][region] = sig
				}
			}
		}
	}
	// 清单里写错的端点会永远静默缺席:collect 找不到就跳过,基线里没有它,
	// 于是"端点整个消失了"那条比对也永远轮不到它 —— 一个写错的路径
	// 会伪装成"一直被监控着"。实际就踩过一次:清单里写的是
	// /dedicated/server/{serviceName}/ipmi,而三个区都只有 /features/ipmi。
	var never []string
	for _, e := range usedEndpoints {
		if len(out[e]) == 0 {
			never = append(never, e)
		}
	}
	if len(never) > 0 {
		sort.Strings(never)
		t.Errorf("usedEndpoints 里这 %d 个端点在 EU / US / CA 三个区都找不到 —— "+
			"要么路径拼错了,要么 OVH 已经把它们删了:\n  %s",
			len(never), strings.Join(never, "\n  "))
	}
	return out
}

func TestOVHSchemaDrift(t *testing.T) {
	if os.Getenv("OVH_SCHEMA_CHECK") == "" && !*update {
		t.Skip("需要联网:设 OVH_SCHEMA_CHECK=1 运行,或加 -update 刷新基线")
	}
	current := collect(t)

	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		b, err := json.MarshalIndent(current, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(baselinePath(), append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("基线已更新:%d 个端点", len(current))
		return
	}

	raw, err := os.ReadFile(baselinePath())
	if err != nil {
		t.Fatalf("读基线失败(第一次跑请加 -update 生成): %v", err)
	}
	var base baseline
	if err := json.Unmarshal(raw, &base); err != nil {
		t.Fatal(err)
	}

	var problems []string
	// 端点消失 / 状态变化 / 签名变化
	for ep, baseRegions := range base {
		curRegions, ok := current[ep]
		if !ok {
			problems = append(problems, fmt.Sprintf("端点整个消失了: %s", ep))
			continue
		}
		for region, bs := range baseRegions {
			cs, ok := curRegions[region]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s 在 %s 区消失了", ep, region))
				continue
			}
			if cs.Status != bs.Status {
				problems = append(problems, fmt.Sprintf("%s [%s] 状态 %s → %s", ep, region, bs.Status, cs.Status))
			}
			if cs.Deprecated != bs.Deprecated || cs.Deletion != bs.Deletion || cs.Replacement != bs.Replacement {
				problems = append(problems, fmt.Sprintf("%s [%s] 废弃标记变化: deprecated=%q deletion=%q replacement=%q",
					ep, region, cs.Deprecated, cs.Deletion, cs.Replacement))
			}
			if cs.ResponseType != bs.ResponseType {
				problems = append(problems, fmt.Sprintf("%s [%s] 响应类型 %s → %s", ep, region, bs.ResponseType, cs.ResponseType))
			}
			if strings.Join(cs.Params, "|") != strings.Join(bs.Params, "|") {
				problems = append(problems, fmt.Sprintf("%s [%s] 参数变化:\n    基线: %v\n    现在: %v",
					ep, region, bs.Params, cs.Params))
			}
		}
		// 端点新出现在某个区(比如 US 补上了某功能)——是好事,但也要知道
		for region := range curRegions {
			if _, ok := baseRegions[region]; !ok {
				problems = append(problems, fmt.Sprintf("%s 新出现在 %s 区(基线里没有)", ep, region))
			}
		}
	}
	for ep := range current {
		if _, ok := base[ep]; !ok {
			problems = append(problems, fmt.Sprintf("基线里没有这个端点(新加的代码?): %s", ep))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Errorf("OVH 接口相对基线发生了 %d 处变化:\n  %s\n\n确认变化无害后跑 -update 刷新基线",
			len(problems), strings.Join(problems, "\n  "))
	}
}
