package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/gin-gonic/gin"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
)

// miscParallelGetStringKeys 与 util.go 的 parallelGetStringKeys 同构,额外把每项的错误带出来。
// 详情拉取失败时列表项只剩主键(状态/IP/到期全空),不把错误交出去的话前端无法区分
// "OVH 没返回这些字段" 和 "这次没拉到",只能把残缺数据当真实状态显示。
func miscParallelGetStringKeys(client *ovhsdk.Client, keys []string, pathFn func(string) string, concurrency int) ([]map[string]interface{}, []error) {
	if concurrency <= 0 {
		concurrency = 10
	}
	results := make([]map[string]interface{}, len(keys))
	errs := make([]error, len(keys))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, k := range keys {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, key string) {
			defer wg.Done()
			defer func() { <-sem }()
			var d map[string]interface{}
			if err := client.Get(pathFn(key), &d); err != nil {
				errs[idx] = err
				return
			}
			results[idx] = d
		}(i, k)
	}
	wg.Wait()
	return results, errs
}

// miscParallelGetDetails 同上,给主键是数字 ID(interface{})的列表用。
func miscParallelGetDetails(client *ovhsdk.Client, keys []interface{}, pathFn func(interface{}) string, concurrency int) ([]map[string]interface{}, []error) {
	if concurrency <= 0 {
		concurrency = 10
	}
	results := make([]map[string]interface{}, len(keys))
	errs := make([]error, len(keys))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, k := range keys {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, key interface{}) {
			defer wg.Done()
			defer func() { <-sem }()
			var d map[string]interface{}
			if err := client.Get(pathFn(key), &d); err != nil {
				errs[idx] = err
				return
			}
			results[idx] = d
		}(i, k)
	}
	wg.Wait()
	return results, errs
}

// miscKeysAsIface 把 string 主键列表转成 interface{} 列表,便于和 miscMergeDetails 复用同一段组装逻辑。
func miscKeysAsIface(keys []string) []interface{} {
	out := make([]interface{}, len(keys))
	for i, k := range keys {
		out[i] = k
	}
	return out
}

// miscMergeDetails 把主键列表和并发拉到的详情合并成前端列表,并统计失败项。
// 失败项仍然保留(用户至少知道这条资源存在),但带上 _detailError 说明这一行是残缺的。
func miscMergeDetails(keys []interface{}, keyField string, details []map[string]interface{}, errs []error) ([]interface{}, int) {
	list := []interface{}{}
	failed := 0
	for i, k := range keys {
		if i >= len(details) || details[i] == nil {
			item := map[string]interface{}{keyField: k}
			if i < len(errs) && errs[i] != nil {
				item["_detailError"] = errs[i].Error()
			}
			list = append(list, item)
			failed++
			continue
		}
		details[i][keyField] = k
		list = append(list, details[i])
	}
	return list, failed
}

// Secondary DNS
func GetSecondaryDNS(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var domains []string
		if err := client.Get("/dedicated/server/"+svc+"/secondaryDnsDomains", &domains); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		details, detailErrs := miscParallelGetStringKeys(client, domains, func(d string) string {
			return "/dedicated/server/" + svc + "/secondaryDnsDomains/" + d
		}, 10)
		list, failed := miscMergeDetails(miscKeysAsIface(domains), "domain", details, detailErrs)
		if failed > 0 {
			state.Logger.Warn(fmt.Sprintf("服务器 %s 从DNS域名详情有 %d/%d 项拉取失败", svc, failed, len(domains)), "server_control")
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "domains": list, "partial": failed > 0, "failedCount": failed})
	}
}

func AddSecondaryDNS(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Domain string `json:"domain"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Domain == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少domain参数"})
			return
		}
		if err := client.Post("/dedicated/server/"+svc+"/secondaryDnsDomains", map[string]interface{}{"domain": body.Domain}, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("添加从DNS域名 "+body.Domain+" 成功", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "从DNS域名已添加"})
	}
}

func DeleteSecondaryDNS(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		domain := c.Param("domain")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		if err := client.Delete("/dedicated/server/"+svc+"/secondaryDnsDomains/"+domain, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("删除从DNS域名 "+domain+" 成功", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "从DNS域名已删除"})
	}
}

// Virtual MAC
func GetVirtualMACList(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var macs []string
		if err := client.Get("/dedicated/server/"+svc+"/virtualMac", &macs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		details, detailErrs := miscParallelGetStringKeys(client, macs, func(m string) string {
			return "/dedicated/server/" + svc + "/virtualMac/" + m
		}, 10)
		list, failed := miscMergeDetails(miscKeysAsIface(macs), "macAddress", details, detailErrs)
		if failed > 0 {
			state.Logger.Warn(fmt.Sprintf("服务器 %s 虚拟MAC详情有 %d/%d 项拉取失败", svc, failed, len(macs)), "server_control")
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "virtualMacs": list, "partial": failed > 0, "failedCount": failed})
	}
}

func CreateVirtualMAC(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			IPAddress          string `json:"ipAddress"`
			Type               string `json:"type"`
			VirtualMachineName string `json:"virtualMachineName"`
		}
		_ = c.ShouldBindJSON(&body)
		// schema 里 ipAddress / type / virtualMachineName 三个 body 参数都是必填,
		// 少了名字只会换回 OVH 的英文参数错误,不如本地先说清楚缺哪个
		if body.IPAddress == "" || body.Type == "" || body.VirtualMachineName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少必需参数: ipAddress、type、virtualMachineName 都必须提供"})
			return
		}
		// dedicated.server.VmacTypeEnum 只有这两个取值
		if body.Type != "ovh" && body.Type != "vmware" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "无效的虚拟MAC类型: " + body.Type + " (只支持 ovh / vmware)"})
			return
		}
		var result map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/virtualMac", map[string]interface{}{
			"ipAddress":          body.IPAddress,
			"type":               body.Type,
			"virtualMachineName": body.VirtualMachineName,
		}, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("创建虚拟MAC成功: "+body.IPAddress, "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "虚拟MAC已创建", "result": result})
	}
}

// Virtual Network Interface
func GetVirtualNetworkInterfaces(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var uuids []string
		if err := client.Get("/dedicated/server/"+svc+"/virtualNetworkInterface", &uuids); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		details, detailErrs := miscParallelGetStringKeys(client, uuids, func(u string) string {
			return "/dedicated/server/" + svc + "/virtualNetworkInterface/" + u
		}, 10)
		list, failed := miscMergeDetails(miscKeysAsIface(uuids), "uuid", details, detailErrs)
		if failed > 0 {
			state.Logger.Warn(fmt.Sprintf("服务器 %s 虚拟网络接口详情有 %d/%d 项拉取失败", svc, failed, len(uuids)), "server_control")
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "interfaces": list, "partial": failed > 0, "failedCount": failed})
	}
}

func EnableVirtualNetworkInterface(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		id := c.Param("uuid")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		// schema 标了 DEPRECATED,replacement 写的是 /ola/aggregation;但那个端点是 BETA、
		// 必填 name + virtualNetworkInterfaces[],语义是"把多个接口聚合成一个",并不等价于
		// 单接口 enable,照搬会改变行为,所以继续用现端点,只把它返回的 task 交出去。
		// body 保持空对象(schema 无 body 参数),不改成 nil 是为了不动现在跑得通的请求形态。
		var task map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/virtualNetworkInterface/"+id+"/enable", map[string]interface{}{}, &task); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("启用虚拟网络接口 %s 成功, taskId=%v", id, task["taskId"]), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "虚拟网络接口已启用", "task": task, "taskId": task["taskId"]})
	}
}

func DisableVirtualNetworkInterface(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		id := c.Param("uuid")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		// 同 enable:端点虽标 DEPRECATED,但 /ola/aggregation 不是等价替换(见上),
		// 这里只补回被丢弃的 dedicated.server.Task,让前端能跟踪 OLA 任务进度
		var task map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/virtualNetworkInterface/"+id+"/disable", map[string]interface{}{}, &task); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("禁用虚拟网络接口 %s 成功, taskId=%v", id, task["taskId"]), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "虚拟网络接口已禁用", "task": task, "taskId": task["taskId"]})
	}
}

// vRack
func GetVRackList(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var vracks []string
		if err := client.Get("/dedicated/server/"+svc+"/vrack", &vracks); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		details, detailErrs := miscParallelGetStringKeys(client, vracks, func(v string) string {
			return "/dedicated/server/" + svc + "/vrack/" + v
		}, 10)
		list, failed := miscMergeDetails(miscKeysAsIface(vracks), "vrackName", details, detailErrs)
		if failed > 0 {
			state.Logger.Warn(fmt.Sprintf("服务器 %s vRack 详情有 %d/%d 项拉取失败", svc, failed, len(vracks)), "server_control")
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "vracks": list, "partial": failed > 0, "failedCount": failed})
	}
}

func RemoveFromVRack(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		vrack := c.Param("vrack")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		if err := client.Delete("/dedicated/server/"+svc+"/vrack/"+vrack, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("从vRack "+vrack+" 移除服务器成功", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "服务器已从vRack移除"})
	}
}

// Orderable
func GetOrderableBandwidth(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d interface{}
		if err := client.Get("/dedicated/server/"+svc+"/orderable/bandwidth", &d); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "orderable": d})
	}
}

func GetOrderableTraffic(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d interface{}
		if err := client.Get("/dedicated/server/"+svc+"/orderable/traffic", &d); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "orderable": d})
	}
}

func GetOrderableIP(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d interface{}
		if err := client.Get("/dedicated/server/"+svc+"/orderable/ip", &d); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "orderable": d})
	}
}

// Options
func GetServerOptions(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var opts []string
		if err := client.Get("/dedicated/server/"+svc+"/option", &opts); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		details, detailErrs := miscParallelGetStringKeys(client, opts, func(o string) string {
			return "/dedicated/server/" + svc + "/option/" + o
		}, 10)
		list, failed := miscMergeDetails(miscKeysAsIface(opts), "option", details, detailErrs)
		if failed > 0 {
			state.Logger.Warn(fmt.Sprintf("服务器 %s 选项详情有 %d/%d 项拉取失败", svc, failed, len(opts)), "server_control")
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "options": list, "partial": failed > 0, "failedCount": failed})
	}
}

// IP specs
func GetIPSpecs(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d interface{}
		if err := client.Get("/dedicated/server/"+svc+"/specifications/ip", &d); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "ipSpecs": d})
	}
}

func GetIPCanBeMovedTo(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		// schema: ip 是 query 必填(类型 ipBlock,可以带掩码,所以这里不做 ipv4 严格校验),
		// 不拼这个 query 的话 OVH 必定以缺参拒绝
		ip := c.Query("ip")
		if ip == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少ip参数"})
			return
		}
		// 该端点返回 void:它只做"这个 IP 能不能搬到本机"的校验,不返回任何目标列表,
		// 所以对外不再叫 targets。OVH 用什么状态码表达"不能搬"schema 没写死,
		// 不猜语义:出错就原样把 OVH 的错误交出去,由调用方判断,免得把权限/网络故障说成"不能迁移"
		if err := client.Get("/dedicated/server/"+svc+"/ipCanBeMovedTo?ip="+url.QueryEscape(ip), nil); err != nil {
			state.Logger.Warn("检查 IP "+ip+" 能否迁移到服务器 "+svc+" 失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error(), "ip": ip})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "canBeMoved": true, "ip": ip})
	}
}

func GetIPCountryAvailable(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d interface{}
		if err := client.Get("/dedicated/server/"+svc+"/ipCountryAvailable", &d); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "countries": d})
	}
}

func MoveIP(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		// /ipMove 的语义是"把这个 IP 搬到 URL 里的这台服务器",目标就是 svc 本身;
		// schema 里 body 只有 ip 一个参数,原来的 to 字段既非法又让调用方误以为能指定去向
		var body struct {
			IP string `json:"ip"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.IP == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少ip参数"})
			return
		}
		var result map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/ipMove", map[string]interface{}{
			"ip": body.IP,
		}, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("IP迁移任务已创建: "+body.IP+" -> "+svc, "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "IP迁移任务已创建", "result": result})
	}
}

// Ongoing
func GetOngoingTasks(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d interface{}
		if err := client.Get("/dedicated/server/"+svc+"/ongoing", &d); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "ongoing": d})
	}
}

// License
//
// ⚠️ 区域提醒：compliantWindows / compliantWindowsSqlServer 这两条【只读】端点 EU/US/CA 三区都有，
// 所以不做门控；但真正下单激活的 POST /dedicated/server/{sn}/license/windows 只有 EU 与 CA 有，
// US 站点整个 dedicated/server 命名空间里没有它(live 三站 schema 已核实)。
// 也就是说美区账户查得到"兼容哪些 Windows 版本"，却无法通过 API 买许可证。
// 将来若在这里补上"订购 Windows 许可证"的 handler，必须先用 srvRegionFor 挡掉 US，
// 否则美区用户会点到一个必然 404 的按钮。
func GetCompliantWindowsVersions(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d interface{}
		if err := client.Get("/dedicated/server/"+svc+"/license/compliantWindows", &d); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "versions": d})
	}
}

func GetCompliantWindowsSqlVersions(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d interface{}
		if err := client.Get("/dedicated/server/"+svc+"/license/compliantWindowsSqlServer", &d); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "versions": d})
	}
}

// Termination

// terminationReasons / terminationFutureUses 对应 schema 的 service.TerminationReasonEnum
// 与 service.TerminationFutureUseEnum,用于确认终止时的可选参数校验。
var terminationReasons = map[string]bool{
	"FEATURES_DONT_SUIT_ME":           true,
	"LACK_OF_PERFORMANCES":            true,
	"MIGRATED_TO_ANOTHER_OVH_PRODUCT": true,
	"MIGRATED_TO_COMPETITOR":          true,
	"NOT_ENOUGH_RECOGNITION":          true,
	"NOT_NEEDED_ANYMORE":              true,
	"NOT_RELIABLE":                    true,
	"NO_ANSWER":                       true,
	"OTHER":                           true,
	"PRODUCT_DIMENSION_DONT_SUIT_ME":  true,
	"PRODUCT_TOOLS_DONT_SUIT_ME":      true,
	"TOO_EXPENSIVE":                   true,
	"TOO_HARD_TO_USE":                 true,
	"UNSATIFIED_BY_CUSTOMER_SUPPORT":  true,
}

var terminationFutureUses = map[string]bool{
	"NOT_REPLACING_SERVICE":      true,
	"OTHER":                      true,
	"SUBSCRIBE_AN_OTHER_SERVICE": true,
	"SUBSCRIBE_OTHER_KIND_OF_SERVICE_WITH_COMPETITOR": true,
	"SUBSCRIBE_SIMILAR_SERVICE_WITH_COMPETITOR":       true,
}

func TerminateService(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		// OVH /terminate 返回一个 string,是这次请求的确认信息;真正的终止 token 按 schema
		// (confirmTermination.token: "sent by email to the admin contact")只会发到管理员邮箱,
		// 所以这里不能把它当 token 交给前端,否则用户会拿这段文字去确认终止
		var resp string
		if err := client.Post("/dedicated/server/"+svc+"/terminate", map[string]interface{}{}, &resp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Warn("服务器 "+svc+" 终止请求已提交, token 已发送至管理员邮箱", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "终止请求已提交,token 已发送至管理员邮箱,请查收邮件后再确认终止", "response": resp})
	}
}

func ConfirmTermination(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Token      string `json:"token"`
			Reason     string `json:"reason"`
			FutureUse  string `json:"futureUse"`
			Commentary string `json:"commentary"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Token == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少token参数"})
			return
		}
		// reason / futureUse / commentary 是 schema 里的可选参数;枚举值先本地校验,
		// 非法值直接挡掉而不是发出去等 OVH 报错(终止确认这一步失败重来成本高)
		if body.Reason != "" && !terminationReasons[body.Reason] {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "无效的终止原因: " + body.Reason})
			return
		}
		if body.FutureUse != "" && !terminationFutureUses[body.FutureUse] {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "无效的后续用途: " + body.FutureUse})
			return
		}
		payload := map[string]interface{}{"token": body.Token}
		// 只把用户真填了的可选字段发出去,不给 OVH 送多余的空值
		if body.Reason != "" {
			payload["reason"] = body.Reason
		}
		if body.FutureUse != "" {
			payload["futureUse"] = body.FutureUse
		}
		if body.Commentary != "" {
			payload["commentary"] = body.Commentary
		}
		// OVH /confirmTermination 返回 string(确认信息)
		var resp string
		if err := client.Post("/dedicated/server/"+svc+"/confirmTermination", payload, &resp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Warn("服务器 "+svc+" 终止已确认", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "终止已确认"})
	}
}

// SPLA
func GetSPLAList(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var ids []interface{}
		if err := client.Get("/dedicated/server/"+svc+"/spla", &ids); err != nil {
			state.Logger.Error("获取SPLA列表失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		details, detailErrs := miscParallelGetDetails(client, ids, func(k interface{}) string {
			return "/dedicated/server/" + svc + "/spla/" + idToString(k)
		}, 10)
		list, failed := miscMergeDetails(ids, "id", details, detailErrs)
		if failed > 0 {
			state.Logger.Warn(fmt.Sprintf("服务器 %s SPLA 详情有 %d/%d 项拉取失败", svc, failed, len(ids)), "server_control")
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "splaList": list, "partial": failed > 0, "failedCount": failed})
	}
}

func CreateSPLA(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Type         string `json:"type"`
			SerialNumber string `json:"serialNumber"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Type == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少type参数"})
			return
		}
		// dedicated.server.SplaTypeEnum 只有这三个取值
		if body.Type != "os" && body.Type != "sqlstd" && body.Type != "sqlweb" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "无效的许可证类型: " + body.Type + " (只支持 os / sqlstd / sqlweb)"})
			return
		}
		// serialNumber 在 schema 里是必填 string,原来照搬 Python 发 JSON null 一样会被 OVH 拒,
		// 不如本地挡下并给出中文提示
		if body.SerialNumber == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少serialNumber参数"})
			return
		}
		payload := map[string]interface{}{"type": body.Type, "serialNumber": body.SerialNumber}
		// OVH /spla 返回 long(新建 SPLA 的 ID),不是对象
		var newID int64
		if err := client.Post("/dedicated/server/"+svc+"/spla", payload, &newID); err != nil {
			state.Logger.Error("创建SPLA许可证失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("创建SPLA许可证成功: %s, id=%d", body.Type, newID), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "SPLA许可证已创建", "id": newID})
	}
}

// BIOS
func GetBIOSSettings(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		state.Logger.Info("[BIOS] 获取服务器 "+svc+" BIOS 设置", "server_control")
		var d map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/biosSettings", &d); err != nil {
			msg := err.Error()
			// 只有 OVH 明确回 404 才是"这台机器没有 BIOS 设置对象";原来的 "object" 子串匹配
			// 会把 403(无权限)、锁定等临时错误也说成"不支持",用户会以为硬件不支持而放弃
			if ovhIsNotFound(err) {
				state.Logger.Warn("[BIOS] 服务器 "+svc+" 不支持 BIOS 设置: "+msg, "server_control")
				c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "BIOS 设置不可用"})
				return
			}
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": msg})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "bios": d})
	}
}

func GetBIOSSettingsSGX(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		state.Logger.Info("[BIOS] 获取服务器 "+svc+" SGX BIOS 设置", "server_control")
		var d map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/biosSettings/sgx", &d); err != nil {
			msg := err.Error()
			// 同上:只认 404,别把权限/锁定错误误报成"SGX 不可用"
			if ovhIsNotFound(err) {
				state.Logger.Warn("[BIOS] 服务器 "+svc+" 不支持 SGX: "+msg, "server_control")
				c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "SGX 不可用"})
				return
			}
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": msg})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "sgx": d})
	}
}

func intToStr(v int64) string {
	return formatInt(v)
}

func formatInt(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func containsAny(s string, subs []string) bool {
	lc := lower(s)
	for _, sub := range subs {
		if indexOf(lc, lower(sub)) >= 0 {
			return true
		}
	}
	return false
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
