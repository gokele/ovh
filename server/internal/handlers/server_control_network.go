package handlers

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
)

// netOVHStatusCode 取 OVH 返回的 HTTP 状态码；不是 OVH API 错误(DNS/超时等传输层错误)时返回 0。
// 之所以不用 strings.Contains 匹配错误文案：OVH 对「服务不属于本凭据」「服务名拼错」
// 也回 404 "This service does not exist"，靠子串匹配会把权限问题一并降级成「暂无数据」，
// 用户完全无从自查。判状态码至少能把 401/403/5xx 挡在降级之外。
func netOVHStatusCode(err error) int {
	var apiErr *ovhsdk.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return 0
}

// netOVHMessage 取 OVH 原始 message，用于在降级响应里保留可自查的线索。
func netOVHMessage(err error) string {
	var apiErr *ovhsdk.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Message
	}
	return err.Error()
}

// MRTG 的 period / type 在 schema 里是枚举(dedicated.server.MrtgPeriodEnum / MrtgTypeEnum)，
// 枚举外的值 OVH 直接 400。前端 query 是用户可控输入，先在本地兜住比赌 OVH 宽容度稳妥。
var mrtgPeriodEnum = map[string]bool{
	"daily": true, "hourly": true, "monthly": true, "weekly": true, "yearly": true,
}

var mrtgTypeEnum = map[string]bool{
	"errors:download": true, "errors:upload": true,
	"packets:download": true, "packets:upload": true,
	"traffic:download": true, "traffic:upload": true,
}

// normalizeMRTGQuery 解析并校验 period / type，非法值直接告诉用户合法取值，不透传给 OVH。
func normalizeMRTGQuery(c *gin.Context) (period string, trafficType string, err error) {
	period = c.DefaultQuery("period", "daily")
	trafficType = c.DefaultQuery("type", "traffic:download")
	if !mrtgPeriodEnum[period] {
		return "", "", fmt.Errorf("period 参数非法: %s (合法值: daily/hourly/weekly/monthly/yearly)", period)
	}
	if !mrtgTypeEnum[trafficType] {
		return "", "", fmt.Errorf("type 参数非法: %s (合法值: traffic:download/traffic:upload/packets:download/packets:upload/errors:download/errors:upload)", trafficType)
	}
	return period, trafficType, nil
}

// legacyMRTGFallback 打 /dedicated/server/{svc}/mrtg。
// ⚠️ 该端点在 EU / US / CA 三区 schema 里都存在且都标了 DEPRECATED，官方 replacement 正是
// /dedicated/server/{svc}/networkInterfaceController(+/{mac}/mrtg)——那两条也是三区齐全。
// 即三区在流量图这条链路上没有能力差异，不需要区域门控；仅作兜底，随时可能被 OVH 下线，
// 所以调用方必须在响应里把 deprecated 标出来。
func legacyMRTGFallback(client *ovhsdk.Client, svc, period, trafficType string) ([]map[string]interface{}, error) {
	q := url.Values{}
	q.Set("period", period)
	q.Set("type", trafficType)
	var data []map[string]interface{}
	if err := client.Get("/dedicated/server/"+svc+"/mrtg?"+q.Encode(), &data); err != nil {
		return nil, err
	}
	return data, nil
}

// perNICMRTG 按 schema 推荐路径逐张网卡取流量图：/networkInterfaceController/{mac}/mrtg。
func perNICMRTG(client *ovhsdk.Client, svc string, macs []string, period, trafficType string) []gin.H {
	type mrtgResult struct {
		data []map[string]interface{}
		err  error
	}
	results := make([]mrtgResult, len(macs))
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	for i, mac := range macs {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, m string) {
			defer wg.Done()
			defer func() { <-sem }()
			var d []map[string]interface{}
			q := url.Values{}
			q.Set("period", period)
			q.Set("type", trafficType)
			path := "/dedicated/server/" + svc + "/networkInterfaceController/" + m + "/mrtg?" + q.Encode()
			if err := client.Get(path, &d); err != nil {
				results[idx] = mrtgResult{err: err}
				return
			}
			results[idx] = mrtgResult{data: d}
		}(i, mac)
	}
	wg.Wait()

	all := []gin.H{}
	for i, mac := range macs {
		r := results[i]
		if r.err != nil {
			all = append(all, gin.H{"mac": mac, "data": []interface{}{}, "error": r.err.Error()})
			continue
		}
		all = append(all, gin.H{"mac": mac, "data": r.data})
	}
	return all
}

// GetNetworkInterfaces GET /api/server-control/:service_name/network-interfaces
func GetNetworkInterfaces(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		state.Logger.Info("[网卡] 获取物理网卡列表: "+svc, "server_control")
		var macs []string
		if err := client.Get("/dedicated/server/"+svc+"/networkInterfaceController", &macs); err != nil {
			// networkInterfaceController 是 BETA 端点，对没有网卡数据的机器会回 404，
			// 这类空资源降级成空列表是刻意的产品行为；401/403/5xx/传输层错误必须照实报出来，
			// 否则切错账户或服务名拼错时用户只会看到「暂无网卡信息」，无从自查。
			if netOVHStatusCode(err) == http.StatusNotFound {
				state.Logger.Warn("[网卡] "+svc+" 返回 404，按「暂无网卡信息」处理: "+err.Error(), "server_control")
				c.JSON(http.StatusOK, gin.H{
					"success":    true,
					"interfaces": []interface{}{},
					"count":      0,
					"message":    "该服务器暂无网卡信息",
					"detail":     netOVHMessage(err),
				})
				return
			}
			state.Logger.Error("[网卡] 获取网卡列表失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// 并发拉每张网卡详情。用带 error 的版本:失败原因要原样带给前端,
		// 编一句 "fetch failed" 会让用户分不清是权限问题还是这张卡真没数据。
		details, errs := parallelGetStringKeysWithErrs(client, macs, func(m string) string {
			return "/dedicated/server/" + svc + "/networkInterfaceController/" + m
		}, 10)
		failed, firstErr := countErrs(errs)
		if failed > 0 {
			state.Logger.Warn(fmt.Sprintf("[网卡] %s 有 %d/%d 张网卡详情获取失败,首个错误: %v",
				svc, failed, len(macs), firstErr), "server_control")
		}
		interfaces := []gin.H{}
		for i, mac := range macs {
			d := details[i]
			if d == nil {
				// 字段名与前端 readPartialList / DetailErrorTag 的约定一致(_detailError)
				msg := "详情获取失败"
				if errs[i] != nil {
					msg = errs[i].Error()
				}
				interfaces = append(interfaces, gin.H{
					"mac":          mac,
					"linkType":     "unknown",
					"_detailError": msg,
				})
				continue
			}
			interfaces = append(interfaces, gin.H{
				"mac":                     mac,
				"linkType":                d["linkType"],
				"virtualNetworkInterface": d["virtualNetworkInterface"],
			})
		}
		state.Logger.Info(fmt.Sprintf("[网卡] 找到 %d 个物理网卡(%d 张详情缺失)", len(interfaces), failed), "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"interfaces":  interfaces,
			"count":       len(interfaces),
			"partial":     failed > 0,
			"failedCount": failed,
		})
	}
}

// GetMRTGData GET /api/server-control/:service_name/mrtg
func GetMRTGData(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		period, trafficType, qErr := normalizeMRTGQuery(c)
		if qErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": qErr.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("[MRTG] 获取流量数据: %s - %s - %s", svc, period, trafficType), "server_control")

		var macs []string
		listErr := client.Get("/dedicated/server/"+svc+"/networkInterfaceController", &macs)
		// networkInterfaceController 是 BETA 端点：既可能报错，也可能 200 回空数组。
		// 两种情况都得回落到旧端点，否则用户拿到的是一张没有任何提示的空流量图，
		// 分不清「本来就没数据」还是「接口坏了」。
		if listErr != nil || len(macs) == 0 {
			reason := "网卡列表为空"
			if listErr != nil {
				reason = listErr.Error()
			}
			state.Logger.Warn("[MRTG] "+reason+"，回落到已废弃(DEPRECATED)的 /dedicated/server/{svc}/mrtg", "server_control")
			data, fbErr := legacyMRTGFallback(client, svc, period, trafficType)
			if fbErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"error":   "新旧API均失败: " + reason + " / " + fbErr.Error(),
				})
				return
			}
			// 回落数据必须包成 interfaces:[{mac,data}]：前端 use-mrtg.ts 的 MrtgResponse 只读
			// interfaces，放在顶层 data 里等于回落了个寂寞（用户看到的还是空图）。
			// mac 留空表示「这条曲线来自旧端点，OVH 没告诉我们是哪张网卡」，
			// 与同文件 GetTrafficStatistics 的 statistics:[{mac:"",data}] 包法保持一致。
			c.JSON(http.StatusOK, gin.H{
				"success":    true,
				"period":     period,
				"type":       trafficType,
				"interfaces": []gin.H{{"mac": "", "data": data}},
				"deprecated": true,
				"message":    "网卡列表接口无数据，已回落到 OVH 已废弃的旧版流量接口（该接口随时可能被 OVH 下线）",
			})
			return
		}

		all := perNICMRTG(client, svc, macs, period, trafficType)
		for _, item := range all {
			if e, ok := item["error"]; ok {
				state.Logger.Warn(fmt.Sprintf("[MRTG] 获取网卡 %v 数据失败: %v", item["mac"], e), "server_control")
				continue
			}
			if d, ok := item["data"].([]map[string]interface{}); ok {
				state.Logger.Info(fmt.Sprintf("[MRTG] 获取网卡 %v 数据成功: %d 个数据点", item["mac"], len(d)), "server_control")
			}
		}
		state.Logger.Info(fmt.Sprintf("[MRTG] 成功获取 %d 个网卡的流量数据", len(all)), "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"interfaces": all,
			"period":     period,
			"type":       trafficType,
			"server":     svc,
		})
	}
}

// ConfigureOLAAggregation POST /api/server-control/:service_name/ola/aggregation
func ConfigureOLAAggregation(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Name                     string   `json:"name"`
			VirtualNetworkInterfaces []string `json:"virtualNetworkInterfaces"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少聚合名称(name)参数"})
			return
		}
		if len(body.VirtualNetworkInterfaces) < 2 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "至少需要2个网络接口进行聚合"})
			return
		}
		state.Logger.Info(fmt.Sprintf("[OLA] 配置网络聚合: %s - %s - %d个接口", svc, body.Name, len(body.VirtualNetworkInterfaces)), "server_control")
		var result map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/ola/aggregation", map[string]interface{}{
			"name":                     body.Name,
			"virtualNetworkInterfaces": body.VirtualNetworkInterfaces,
		}, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("[OLA] 网络聚合配置任务已创建: Task#%v", result["taskId"]), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "网络聚合配置任务已创建", "task": result})
	}
}

// ResetOLAConfiguration POST /api/server-control/:service_name/ola/reset
func ResetOLAConfiguration(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			VirtualNetworkInterface string `json:"virtualNetworkInterface"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.VirtualNetworkInterface == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少虚拟网络接口UUID(virtualNetworkInterface)参数"})
			return
		}
		state.Logger.Info("[OLA] 重置网络接口: "+svc+" - "+body.VirtualNetworkInterface, "server_control")
		var result map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/ola/reset", map[string]interface{}{
			"virtualNetworkInterface": body.VirtualNetworkInterface,
		}, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("[OLA] 网络接口重置任务已创建: Task#%v", result["taskId"]), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "网络接口重置任务已创建", "task": result})
	}
}

// OLAGroup POST /api/server-control/:service_name/ola/group
//
// 路由名保留(前端/外部调用方可能按老名字调)，但实际打的是 OVH 官方 replacement:
// schema 里 /dedicated/server/{svc}/ola/group 已 DEPRECATED → /dedicated/server/{svc}/ola/aggregation，
// 两者 body 签名完全一致(name + virtualNetworkInterfaces)，所以直接走新端点不会有行为差异，
// 还能避免 OVH 下线旧端点后这条路由突然 404。
func OLAGroup(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		// schema: name(string) 与 virtualNetworkInterfaces(uuid[]) 都是 body 必填。
		// 老实现发的是空 {}，必被 OVH 400。
		var body struct {
			Name                     string   `json:"name"`
			VirtualNetworkInterfaces []string `json:"virtualNetworkInterfaces"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少聚合名称(name)参数"})
			return
		}
		if len(body.VirtualNetworkInterfaces) < 2 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "至少需要2个网络接口进行聚合"})
			return
		}
		state.Logger.Info(fmt.Sprintf("[OLA] 创建OLA组(走 ola/aggregation): %s - %s - %d个接口", svc, body.Name, len(body.VirtualNetworkInterfaces)), "server_control")
		var result map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/ola/aggregation", map[string]interface{}{
			"name":                     body.Name,
			"virtualNetworkInterfaces": body.VirtualNetworkInterfaces,
		}, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("创建OLA组成功: %s, Task#%v", svc, result["taskId"]), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "OLA组已创建", "result": result, "task": result})
	}
}

// OLAUngroup POST /api/server-control/:service_name/ola/ungroup
//
// 同 OLAGroup：schema 里 /ola/ungroup 已 DEPRECATED → /ola/reset，body 签名一致
// (virtualNetworkInterface uuid)，故实际打新端点。
// 差异只有返回值：旧 /ola/ungroup 是 Task[]，新 /ola/reset 是单个 Task；
// 为了不破坏可能已按数组解析的调用方，tasks 仍以数组形式返回。
func OLAUngroup(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		// schema: virtualNetworkInterface(uuid) 是 body 必填。老实现发空 {}，必被 OVH 400。
		var body struct {
			VirtualNetworkInterface string `json:"virtualNetworkInterface"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.VirtualNetworkInterface == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少虚拟网络接口UUID(virtualNetworkInterface)参数"})
			return
		}
		state.Logger.Info("[OLA] 解散OLA组(走 ola/reset): "+svc+" - "+body.VirtualNetworkInterface, "server_control")
		var task map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/ola/reset", map[string]interface{}{
			"virtualNetworkInterface": body.VirtualNetworkInterface,
		}, &task); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		tasks := []map[string]interface{}{task}
		state.Logger.Info(fmt.Sprintf("解散OLA组成功: %s, Task#%v", svc, task["taskId"]), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "OLA组已解散", "tasks": tasks, "task": task})
	}
}

// ipmiAccessPriority 自动挑选时的优先级：HTML5 KVM 最通用（浏览器直接开），
// 其次 Java KVM（JNLP，需要 Java Web Start），再退到 Serial-over-LAN(URL)，
// 最后才是需要 SSH 公钥的 Serial-over-LAN(SSH)。
// 四个值都在 dedicated.server.IpmiAccessTypeEnum 里。
//
// 注意这只是**没指定类型时**的兜底顺序。调用方可以用 ?type= 显式指定 ——
// 有些机型的 HTML5 KVM 有兼容问题（键盘映射、鼠标不同步），老运维就是要 Java KVM，
// 自动挑选把 HTML5 排第一会让他们永远拿不到 JNLP。
var ipmiAccessPriority = []string{"kvmipHtml5URL", "kvmipJnlp", "serialOverLanURL", "serialOverLanSshKey"}

// ipmiAccessTypes dedicated.server.IpmiAccessTypeEnum 的全部合法取值
var ipmiAccessTypes = map[string]string{
	"kvmipHtml5URL":       "HTML5 KVM（浏览器直接打开）",
	"kvmipJnlp":           "Java KVM（下载 .jnlp，需要 Java Web Start）",
	"serialOverLanURL":    "串口重定向 SOL（浏览器打开）",
	"serialOverLanSshKey": "串口重定向 SOL（SSH，需要公钥）",
}

// GetIPMIAccessTypes GET /api/server-control/:service_name/ipmi-types
//
// 只查这台机器支持哪几种控制台接入方式,不申请会话。
// 单独开这个接口是因为申请会话要轮询 OVH 任务、动辄 20 秒 ——
// 让用户等 20 秒才知道"原来这台机器没有 Java KVM"是很糟的体验。
func GetIPMIAccessTypes(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var ipmi map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/features/ipmi", &ipmi); err != nil {
			if ovhIsNotFound(err) {
				c.JSON(http.StatusOK, gin.H{
					"success":        true,
					"supportedTypes": []string{},
					"message":        "该服务器没有 IPMI 功能",
				})
				return
			}
			state.Logger.Error("[IPMI] 查询支持的控制台类型失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		supportedList := []string{}
		if sf, ok := ipmi["supportedFeatures"].(map[string]interface{}); ok {
			for _, t := range ipmiAccessPriority {
				if b, _ := sf[t].(bool); b {
					supportedList = append(supportedList, t)
				}
			}
		}
		activated := true
		if v, ok := ipmi["activated"].(bool); ok {
			activated = v
		}
		c.JSON(http.StatusOK, gin.H{
			"success":        true,
			"supportedTypes": supportedList,
			"typeLabels":     ipmiAccessTypes,
			"activated":      activated,
			// 没指定 type 时后端会挑哪个,前端拿它当默认选中项
			"defaultType": func() string {
				if len(supportedList) > 0 {
					return supportedList[0]
				}
				return ""
			}(),
		})
	}
}

// IPMI Console
func GetIPMIConsole(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		state.Logger.Info("[IPMI] 获取服务器 "+svc+" IPMI信息", "server_control")
		var ipmi map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/features/ipmi", &ipmi); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// dedicated.server.Ipmi.activated 是必填 boolean，但 schema 没说清它是「IPMI 功能可用」
		// 还是「当前有活动会话」。之前在这里直接 400 会把部分机型原本能开的控制台堵死
		// 该字段以前从不检查，所以只记一笔、并在 OVH 真的拒绝时把它当成提示补上。
		ipmiActivated := true
		if v, ok := ipmi["activated"].(bool); ok {
			ipmiActivated = v
		}
		if !ipmiActivated {
			state.Logger.Warn("[IPMI] 服务器 "+svc+" 的 activated=false，仍尝试申请控制台访问", "server_control")
		}
		// dedicated.server.IpmiSupportedFeatures 的四个字段都是「必填 boolean」，
		// OVH 总把 4 个 key 全返回(值为 true/false)。老代码判的是「key 是否存在」，
		// 于是永远命中第一个分支 kvmipHtml5URL，只支持 SOL 的老机型会被强行要 HTML5 KVM。
		// 先算出这台机器到底支持哪几种接入方式,后面无论自动挑还是用户指定都要用
		supported := map[string]bool{}
		supportedList := []string{}
		if sf, ok := ipmi["supportedFeatures"].(map[string]interface{}); ok {
			for _, t := range ipmiAccessPriority {
				if b, _ := sf[t].(bool); b {
					supported[t] = true
					supportedList = append(supportedList, t)
				}
			}
		}

		var accessType string
		// ?type= 显式指定(例如老运维就要 Java KVM),校验两道:枚举合法 + 这台机器确实支持
		if want := strings.TrimSpace(c.Query("type")); want != "" {
			if _, ok := ipmiAccessTypes[want]; !ok {
				c.JSON(http.StatusBadRequest, gin.H{
					"success":    false,
					"error":      "不支持的控制台类型: " + want,
					"validTypes": ipmiAccessTypes,
				})
				return
			}
			if !supported[want] {
				c.JSON(http.StatusBadRequest, gin.H{
					"success":        false,
					"error":          "该服务器不支持「" + ipmiAccessTypes[want] + "」",
					"supportedTypes": supportedList,
				})
				return
			}
			accessType = want
		} else {
			// 没指定 → 按优先级自动挑
			for _, t := range ipmiAccessPriority {
				if supported[t] {
					accessType = t
					break
				}
			}
		}
		if accessType == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "服务器不支持KVM控制台访问"})
			return
		}
		state.Logger.Info("[IPMI] 请求KVM控制台访问，类型: "+accessType, "server_control")
		clientIP := c.GetHeader("X-Forwarded-For")
		if clientIP == "" {
			clientIP = c.ClientIP()
		}
		if idx := strings.Index(clientIP, ","); idx != -1 {
			clientIP = strings.TrimSpace(clientIP[:idx])
		}
		params := map[string]interface{}{
			"type": accessType,
			// ttl 是 dedicated.server.CacheTTLEnum(enumType=long)，合法值 1/3/5/10/15，15 是上限
			"ttl": 15,
		}
		// schema: ipToAllow 是 body 可选 ipv4。IPv6 客户端地址(通过 X-Forwarded-For 或
		// c.ClientIP() 拿到的字面量)塞进去 OVH 必然 400；私网/回环/链路本地地址即使被接受，
		// 放进 OVH 侧白名单也毫无意义。既然是可选字段，判不出公网 IPv4 就干脆省略。
		if parsed := net.ParseIP(clientIP); parsed != nil && parsed.To4() != nil &&
			!parsed.IsLoopback() && !parsed.IsPrivate() && !parsed.IsLinkLocalUnicast() && !parsed.IsUnspecified() {
			// 用 String() 归一化，避免 ::ffff:1.2.3.4 这类 IPv4-mapped 写法直接透传
			normalized := parsed.To4().String()
			params["ipToAllow"] = normalized
			state.Logger.Info("[IPMI] 添加IP白名单: "+normalized, "server_control")
		} else {
			// 省略 ipToAllow 意味着 OVH 侧这次会话不限来源 IP。填一个私网/回环地址反而更糟：
			// OVH 永远看不到那个地址，等于把会话锁死成谁都进不去。所以这里选择省略并明确告警。
			state.Logger.Warn("[IPMI] 跳过IP白名单（非公网 IPv4 地址: "+clientIP+"），本次控制台会话不限制来源 IP", "server_control")
		}
		// serialOverLanSshKey 方式需要一把公钥；schema 里 sshKey 是 body 可选(text)，
		// 调用方没给就不发，交给 OVH 用账户里已登记的密钥。
		if sshKey := strings.TrimSpace(c.Query("sshKey")); sshKey != "" {
			params["sshKey"] = sshKey
		}
		var task map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/features/ipmi/access", params, &task); err != nil {
			msg := err.Error()
			if !ipmiActivated {
				msg += "（该服务器的 IPMI activated=false，很可能需要先在 OVH 管理后台启用 IPMI）"
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": msg})
			return
		}
		taskID := task["taskId"]
		state.Logger.Info(fmt.Sprintf("[IPMI] 创建访问任务: taskId=%v, status=%v", taskID, task["status"]), "server_control")
		// 轮询
		maxRetries := 10
		taskCompleted := false
		for i := 0; i < maxRetries; i++ {
			time.Sleep(2 * time.Second)
			var ts map[string]interface{}
			// OVH 错误直接返回 500，
			// 之前 Go 静默 continue 会掩盖 OVH 真错误，最终用 "超时" 假面具吞掉
			if err := client.Get(fmt.Sprintf("/dedicated/server/%s/task/%v", svc, taskID), &ts); err != nil {
				state.Logger.Error(fmt.Sprintf("[IPMI] 查询任务 %v 状态失败: %s", taskID, err.Error()), "server_control")
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
				return
			}
			status, _ := ts["status"].(string)
			state.Logger.Info(fmt.Sprintf("[IPMI] 任务状态检查 (%d/%d): %s", i+1, maxRetries, status), "server_control")
			if status == "done" {
				taskCompleted = true
				break
			}
			if status == "cancelled" || status == "customerError" || status == "ovhError" {
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "IPMI访问任务失败: " + status})
				return
			}
		}
		if !taskCompleted {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "IPMI访问任务超时"})
			return
		}
		var consoleAccess map[string]interface{}
		// 这一步的错误以前被 `_ =` 吞掉，前端只能拿到 success:true + console:null，日志里也查不到原因
		if err := client.Get("/dedicated/server/"+svc+"/features/ipmi/access?type="+url.QueryEscape(accessType), &consoleAccess); err != nil {
			state.Logger.Error("[IPMI] 获取控制台地址失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取控制台地址失败: " + err.Error()})
			return
		}
		// dedicated.server.IpmiAccessValue.value 是「可空」string：任务 done 了 OVH 也可能还没生成地址
		if v, _ := consoleAccess["value"].(string); v == "" {
			state.Logger.Error(fmt.Sprintf("[IPMI] OVH 返回空控制台地址: type=%s, resp=%v", accessType, consoleAccess), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "OVH 未返回控制台地址，请稍后重试"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"ipmi":       ipmi,
			"console":    consoleAccess,
			"accessType": accessType,
			// 告诉前端这台机器还支持哪些方式,好让用户换一种再开
			"supportedTypes": supportedList,
			"typeLabels":     ipmiAccessTypes,
		})
	}
}

// GetTrafficStatistics GET /api/server-control/:service_name/statistics
//
// OVH 独服根本没有 /dedicated/server/{svc}/statistics 这个端点：EU / US / CA 三区的本地 dump
// 与 live 的 dedicated/server.json 都查不到(三区各自 grep "statistic" 结果均为空)，
// 全局带 statistics 的只有 /vps/{serviceName}/statistics —— 这不是区域差异，是这个端点从来不存在。
// 老实现还手搓 http.Request 却只塞了 X-Ovh-Application / X-Ovh-Consumer，
// 漏了 v1 签名要求的 X-Ovh-Timestamp / X-Ovh-Signature，即使路径存在也必被 403。
// 现改为走 schema 里真实存在的流量图端点(networkInterfaceController + 旧版 mrtg 兜底)，
// 并统一用带签名的 go-ovh client，不再手搓请求。路由与 statistics 响应字段保持不变。
func GetTrafficStatistics(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		// 老实现的默认 period 是 "lastday"，不在 dedicated.server.MrtgPeriodEnum 里；
		// 这里靠 normalizeMRTGQuery 统一收敛到合法枚举（默认 daily）。
		period, typeParam, qErr := normalizeMRTGQuery(c)
		if qErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": qErr.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("[Stats] 获取服务器 %s 流量统计: %s - %s", svc, period, typeParam), "server_control")

		var macs []string
		listErr := client.Get("/dedicated/server/"+svc+"/networkInterfaceController", &macs)
		if listErr != nil || len(macs) == 0 {
			reason := "网卡列表为空"
			if listErr != nil {
				reason = listErr.Error()
			}
			state.Logger.Warn("[Stats] "+reason+"，回落到已废弃(DEPRECATED)的 /dedicated/server/{svc}/mrtg", "server_control")
			data, fbErr := legacyMRTGFallback(client, svc, period, typeParam)
			if fbErr != nil {
				state.Logger.Error("[Stats] 新旧流量接口均失败: "+reason+" / "+fbErr.Error(), "server_control")
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"error":   "获取流量统计失败: " + fbErr.Error(),
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"success":    true,
				"statistics": []gin.H{{"mac": "", "data": data}},
				"period":     period,
				"type":       typeParam,
				"deprecated": true,
				"message":    "网卡列表接口无数据，已回落到 OVH 已废弃的旧版流量接口（该接口随时可能被 OVH 下线）",
			})
			return
		}

		stats := perNICMRTG(client, svc, macs, period, typeParam)
		state.Logger.Info(fmt.Sprintf("[Stats] 流量统计获取成功: %d 个网卡", len(stats)), "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"statistics": stats,
			"period":     period,
			"type":       typeParam,
		})
	}
}

// GetNetworkInterfaceStats GET /api/server-control/:service_name/network-stats
func GetNetworkInterfaceStats(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		state.Logger.Info("[Network] 获取服务器 "+svc+" 网络接口信息", "server_control")
		var macs []string
		if err := client.Get("/dedicated/server/"+svc+"/networkInterfaceController", &macs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// 并发拉每张网卡详情
		details := parallelGetStringKeys(client, macs, func(m string) string {
			return "/dedicated/server/" + svc + "/networkInterfaceController/" + m
		}, 10)
		// 失败的网卡不能直接丢：静默少几张卡会让用户以为服务器就这么多网卡，
		// 进而影响 OLA 聚合时的接口选择。与 GetNetworkInterfaces 保持一致的做法：按索引补位。
		interfaces := []map[string]interface{}{}
		failed := 0
		for i, mac := range macs {
			d := details[i]
			if d == nil {
				failed++
				interfaces = append(interfaces, map[string]interface{}{
					"mac":   mac,
					"error": "fetch failed",
				})
				continue
			}
			interfaces = append(interfaces, d)
		}
		if failed > 0 {
			state.Logger.Warn(fmt.Sprintf("[Network] %d/%d 个网卡详情获取失败", failed, len(macs)), "server_control")
		}
		state.Logger.Info(fmt.Sprintf("[Network] 找到 %d 个网络接口", len(interfaces)), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "interfaces": interfaces})
	}
}
