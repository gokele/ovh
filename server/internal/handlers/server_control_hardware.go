package handlers

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/numconv"
)

// hardwareParallelGetDetails 与 util.go 的 parallelGetDetails 行为一致,但额外把每个
// goroutine 的 error 按索引保留下来。
// 之所以不复用原版:本文件里的几个列表接口(反向 DNS / 干预记录)拉的都是需要独立授权域的
// 资源(/ip/** 与 /dedicated/server/** 是两套 access rule),用户粘贴的 consumerKey 很可能只
// 覆盖其中一个;原版"err != nil 就当这条不存在"会把 403 / 429 变成"列表是空的",用户会误以为
// 记录被删了。保留 error 才能区分"OVH 确实没有这条"和"我们没拿到"。
func hardwareParallelGetDetails(client *ovhsdk.Client, keys []interface{}, pathFn func(interface{}) string, concurrency int) ([]map[string]interface{}, []error) {
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
			// OVH 偶尔对已消失的资源回 200 + null,这里也算"没拿到",
			// 保证 results[idx]==nil 与 errs[idx]!=nil 严格同步(调用方靠这个统计失败数)
			if d == nil {
				errs[idx] = fmt.Errorf("OVH 返回了空对象: %s", pathFn(key))
				return
			}
			results[idx] = d
		}(i, k)
	}
	wg.Wait()
	return results, errs
}

// hardwareFirstErr 返回一组结果里第一个非空 error(用于把"部分失败"里最有代表性的原因回给前端)
func hardwareFirstErr(errs ...[]error) error {
	for _, group := range errs {
		for _, e := range group {
			if e != nil {
				return e
			}
		}
	}
	return nil
}

// GetHardwareInfo GET /api/server-control/:service_name/hardware
func GetHardwareInfo(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var hardware map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/specifications/hardware", &hardware); err != nil {
			state.Logger.Error("获取服务器 "+svc+" 硬件信息失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// 缺字段补 N/A / 0 / {} / []，
		// 否则 JSON 序列化 null 让前端 .toLowerCase / .length 崩溃
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"hardware": gin.H{
				"bootMode":                valueOr(hardware, "bootMode", "N/A"),
				"coresPerProcessor":       defaultZero(hardware["coresPerProcessor"]),
				"threadsPerProcessor":     defaultZero(hardware["threadsPerProcessor"]),
				"numberOfProcessors":      defaultZero(hardware["numberOfProcessors"]),
				"processorName":           valueOr(hardware, "processorName", "N/A"),
				"processorArchitecture":   valueOr(hardware, "processorArchitecture", "N/A"),
				"memorySize":              defaultObj(hardware["memorySize"]),
				"motherboard":             valueOr(hardware, "motherboard", "N/A"),
				"formFactor":              valueOr(hardware, "formFactor", "N/A"),
				"description":             valueOr(hardware, "description", ""),
				"diskGroups":              defaultArr(hardware["diskGroups"]),
				"expansionCards":          defaultArr(hardware["expansionCards"]),
				"usbKeys":                 defaultArr(hardware["usbKeys"]),
				"defaultHardwareRaidSize": defaultObj(hardware["defaultHardwareRaidSize"]),
				"defaultHardwareRaidType": valueOr(hardware, "defaultHardwareRaidType", "N/A"),
			},
		})
	}
}

// GetNetworkSpecs GET /api/server-control/:service_name/network-specs
func GetNetworkSpecs(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var network map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/specifications/network", &network); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"network": gin.H{
				"bandwidth":  network["bandwidth"],
				"connection": network["connection"],
				"ola":        network["ola"],
				"routing":    network["routing"],
				"traffic":    network["traffic"],
				"switching":  network["switching"],
				"vmac":       network["vmac"],
				"vrack":      network["vrack"],
			},
		})
	}
}

// GetServerIPs GET /api/server-control/:service_name/ips
func GetServerIPs(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var list []string
		if err := client.Get("/dedicated/server/"+svc+"/ips", &list); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// 并发拉每个 IP 的详情
		details := parallelGetStringKeys(client, list, func(ip string) string {
			return "/ip/" + strings.ReplaceAll(ip, "/", "%2F")
		}, 10)
		ips := []gin.H{}
		for i, ip := range list {
			detail := details[i]
			if detail == nil {
				ips = append(ips, gin.H{"ip": ip, "type": "unknown"})
				continue
			}
			routedTo := ""
			if r, ok := detail["routedTo"].(map[string]interface{}); ok {
				if s, ok := r["serviceName"].(string); ok {
					routedTo = s
				}
			}
			ips = append(ips, gin.H{
				"ip":          ip,
				"type":        valueOr(detail, "type", "N/A"),
				"description": valueOr(detail, "description", ""),
				"routedTo":    routedTo,
			})
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "ips": ips, "total": len(ips)})
	}
}

// GetReverseDNS GET /api/server-control/:service_name/reverse
//
// 反向 DNS 不在 /dedicated/server/ 域下,而在 /ip/{ipBlock}/reverse。
// 流程:服务器 IP 块 → 每块查 reverse 列表 → 每个 IP 查 reverse 详情。
func GetReverseDNS(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var ipBlocks []string
		if err := client.Get("/dedicated/server/"+svc+"/ips", &ipBlocks); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// 1) 每个 IP 块并发拉 reverse 列表(块下哪些具体 IP 配了反向)
		// /ip/** 与 /dedicated/server/** 是两个独立授权域,上一步成功不代表这一步有权限,
		// 所以每块的 error 都要留下来,不能像原来那样 `_ =` 丢掉。
		type blockResult struct {
			block string
			ips   []string
		}
		blockResults := make([]blockResult, len(ipBlocks))
		blockErrs := make([]error, len(ipBlocks))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 8)
		for i, blk := range ipBlocks {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, block string) {
				defer wg.Done()
				defer func() { <-sem }()
				encoded := strings.ReplaceAll(block, "/", "%2F")
				var ips []string
				if err := client.Get("/ip/"+encoded+"/reverse", &ips); err != nil {
					blockErrs[idx] = err
				}
				blockResults[idx] = blockResult{block: block, ips: ips}
			}(i, blk)
		}
		wg.Wait()

		// 2) 把 (block, ip) 配对展开,并发拉每条 reverse 详情
		type entry struct {
			block string
			ip    string
		}
		entries := []entry{}
		for _, r := range blockResults {
			for _, ip := range r.ips {
				entries = append(entries, entry{block: r.block, ip: ip})
			}
		}
		details := make([]map[string]interface{}, len(entries))
		detailErrs := make([]error, len(entries))
		var wg2 sync.WaitGroup
		sem2 := make(chan struct{}, 10)
		for i, e := range entries {
			wg2.Add(1)
			sem2 <- struct{}{}
			go func(idx int, en entry) {
				defer wg2.Done()
				defer func() { <-sem2 }()
				encoded := strings.ReplaceAll(en.block, "/", "%2F")
				var d map[string]interface{}
				if err := client.Get("/ip/"+encoded+"/reverse/"+en.ip, &d); err != nil {
					detailErrs[idx] = err
					return
				}
				// 同上:200 + null 也当失败记账,否则 failed 计数和被跳过的条目会对不上
				if d == nil {
					detailErrs[idx] = fmt.Errorf("OVH 返回了空的反向DNS记录: %s", en.ip)
					return
				}
				details[idx] = d
			}(i, e)
		}
		wg2.Wait()

		reverseList := []gin.H{}
		for i, e := range entries {
			if details[i] == nil {
				continue
			}
			reverseList = append(reverseList, gin.H{
				"ipReverse": e.ip,
				"reverse":   details[i]["reverse"],
				"ipBlock":   e.block,
			})
		}

		// 统计两阶段的失败数:全军覆没(通常是 /ip/* 无授权或 OVH 限流)必须报错,
		// 否则前端会显示"没有配置任何反向 DNS",用户会以为记录被删了
		failed := 0
		for _, e := range blockErrs {
			if e != nil {
				failed++
			}
		}
		for _, e := range detailErrs {
			if e != nil {
				failed++
			}
		}
		total := len(blockErrs) + len(detailErrs)
		if total > 0 && failed == total {
			fe := hardwareFirstErr(blockErrs, detailErrs)
			state.Logger.Error("获取服务器 "+svc+" 反向DNS失败(全部 "+fmt.Sprintf("%d", total)+" 次 /ip 调用均失败): "+fe.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": fe.Error()})
			return
		}
		resp := gin.H{"success": true, "reverses": reverseList}
		if failed > 0 {
			fe := hardwareFirstErr(blockErrs, detailErrs)
			state.Logger.Warn(fmt.Sprintf("服务器 %s 反向DNS 部分获取失败(%d/%d): %s", svc, failed, total, fe.Error()), "server_control")
			resp["partial"] = true
			resp["failed"] = failed
			resp["error"] = "部分反向DNS记录获取失败：" + fe.Error()
		}
		c.JSON(http.StatusOK, resp)
	}
}

// SetReverseDNS POST /api/server-control/:service_name/reverse
//
// 设反向 DNS 也走 /ip/{ipBlock}/reverse。需要先找到 body.IP 所属的 IP 块。
func SetReverseDNS(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			IP      string `json:"ip"`
			Reverse string `json:"reverse"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.IP == "" || body.Reverse == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "IP地址和反向DNS不能为空"})
			return
		}
		// 找该 IP 所在的服务器 IP 块
		block, err := findIPBlockForServer(client, svc, body.IP)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
			return
		}
		encoded := strings.ReplaceAll(block, "/", "%2F")
		if err := client.Post("/ip/"+encoded+"/reverse", map[string]interface{}{
			"ipReverse": body.IP,
			"reverse":   body.Reverse,
		}, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("服务器 "+svc+" IP "+body.IP+" 反向DNS已设置为 "+body.Reverse, "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "反向DNS已设置"})
	}
}

// DeleteReverseDNS DELETE /api/server-control/:service_name/reverse/:ip
// OVH 的 DELETE /ip/{ipBlock}/reverse/{ipReverse} 删除单条反向记录。
func DeleteReverseDNS(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		ip := c.Param("ip")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		block, err := findIPBlockForServer(client, svc, ip)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
			return
		}
		encoded := strings.ReplaceAll(block, "/", "%2F")
		if err := client.Delete("/ip/"+encoded+"/reverse/"+ip, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("服务器 "+svc+" IP "+ip+" 反向DNS已删除", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "反向DNS已删除"})
	}
}

// findIPBlockForServer 在服务器的 IP 块列表里找出包含给定 IPv4 的那个块。
// 用 net.ParseCIDR + Contains 判定;返回精确块字符串(如 "1.2.3.0/29")。
func findIPBlockForServer(client interface {
	Get(path string, result interface{}) error
}, svc, ipStr string) (string, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "", fmt.Errorf("非法 IP: %s", ipStr)
	}
	var blocks []string
	if err := client.Get("/dedicated/server/"+svc+"/ips", &blocks); err != nil {
		return "", err
	}
	for _, blk := range blocks {
		// 补掩码只是为了能 ParseCIDR,补出来的字符串绝不能外传:
		// /ip/{ip} 的 path 参数是 ipBlock 资源 id,必须与 /dedicated/server/{svc}/ips
		// 返回的原样值一致,自作主张加 "/32" 会让后续 reverse 请求 404。
		parseable := blk
		_, ipnet, err := net.ParseCIDR(parseable)
		if err != nil {
			// 老格式可能没带 mask,补 /32 / /128 再 parse
			if strings.Contains(parseable, ":") {
				parseable += "/128"
			} else {
				parseable += "/32"
			}
			_, ipnet, err = net.ParseCIDR(parseable)
			if err != nil {
				continue
			}
		}
		if ipnet.Contains(ip) {
			return blk, nil
		}
	}
	return "", fmt.Errorf("IP %s 不在服务器 %s 的任何 IP 块内", ipStr, svc)
}

// GetServiceInfo GET /api/server-control/:service_name/serviceinfo
func GetServiceInfo(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var info map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/serviceInfos", &info); err != nil {
			state.Logger.Error("获取服务器 "+svc+" 服务信息失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		renew, _ := info["renew"].(map[string]interface{})
		automatic := false
		period := 0
		deleteAtExpiration := false
		forced := false
		manualPayment := false
		if renew != nil {
			if a, ok := renew["automatic"].(bool); ok {
				automatic = a
			}
			if p, ok := numconv.ToInt64(renew["period"]); ok {
				period = int(p)
			}
			if d, ok := renew["deleteAtExpiration"].(bool); ok {
				deleteAtExpiration = d
			}
			if f, ok := renew["forced"].(bool); ok {
				forced = f
			}
			if m, ok := renew["manualPayment"].(bool); ok {
				manualPayment = m
			}
		}
		// possibleRenewPeriod: OVH 给的合法续费周期(月数数组,前端 select 用)
		possiblePeriods := []int{}
		if arr, ok := info["possibleRenewPeriod"].([]interface{}); ok {
			for _, v := range arr {
				if p, ok := numconv.ToInt64(v); ok && p > 0 {
					possiblePeriods = append(possiblePeriods, int(p))
				}
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"serviceInfo": gin.H{
				"status":                    valueOr(info, "status", "unknown"),
				"expiration":                valueOr(info, "expiration", ""),
				"creation":                  valueOr(info, "creation", ""),
				"renewalType":               automatic, // 自动续费 yes/no
				"renewalPeriod":             period,    // 续费周期(月)
				"renewalDeleteAtExpiration": deleteAtExpiration,
				"renewalForced":             forced, // OVH 强制自动续费(不能改)
				"renewalManualPayment":      manualPayment,
				"possibleRenewPeriod":       possiblePeriods,
			},
		})
	}
}

// UpdateServiceRenewal PUT /api/server-control/:service_name/serviceinfo
//
// 修改服务的续费策略。前端传 mode (auto / manual / delete-at-expiration) + 可选 period,
// 后端先 GET 当前 serviceInfos,合并 renew 字段,再 PUT 整体回去(OVH PUT 要求完整对象)。
// forced=true (engaged 合同期) 时 OVH 会拒,我们这里直接返 400 提示用户。
func UpdateServiceRenewal(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Mode   string `json:"mode"`   // "auto" / "manual" / "delete"
			Period int    `json:"period"` // 月数,0 表示不改
		}
		_ = c.ShouldBindJSON(&body)

		var info map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/serviceInfos", &info); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		renew, _ := info["renew"].(map[string]interface{})
		if renew == nil {
			renew = map[string]interface{}{}
		}
		// forced 锁:OVH engaged 合同期内,renew 改不动 —— 提前拒绝,不让 PUT 浪费一次往返
		if f, ok := renew["forced"].(bool); ok && f {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "该服务器处于 OVH 合同期(engaged),续费策略由 OVH 锁定,无法修改",
			})
			return
		}
		switch body.Mode {
		case "auto":
			renew["automatic"] = true
			renew["deleteAtExpiration"] = false
			renew["manualPayment"] = false
		case "manual":
			renew["automatic"] = false
			renew["deleteAtExpiration"] = false
			renew["manualPayment"] = true
		case "delete":
			renew["automatic"] = false
			renew["deleteAtExpiration"] = true
			renew["manualPayment"] = false
		default:
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "mode 必须是 auto / manual / delete 之一"})
			return
		}
		if body.Period > 0 {
			renew["period"] = body.Period
		}
		info["renew"] = renew

		// PUT 整对象回去(OVH 这个端点要求完整 services.Service)
		if err := client.Put("/dedicated/server/"+svc+"/serviceInfos", info, nil); err != nil {
			state.Logger.Error("修改服务器 "+svc+" 续费策略失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("服务器 "+svc+" 续费策略已更新: mode="+body.Mode, "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "续费策略已更新"})
	}
}

// ChangeContact POST /api/server-control/:service_name/change-contact
//
// EU/CA only —— /dedicated/server/{serviceName}/changeContact 在 EU 与 CA 两站都存在(POST),
// 只有 US(api.us.ovhcloud.com)整个 dedicated/server 命名空间里没有这个端点
// (本地三区 dump + live schema 双重核实:EU/CA 各 107 条路径含 changeContact,US 98 条不含)。
// OVHcloud US 是独立公司,没有 NIC 联系人体系,配套的 /me/task/contactChange 系列在 US 同样缺失。
// 这里提前拒,免得用户看到 OVH 原始的 404。
//
// 判大区走 srvRegionFor(见 server_control_basic.go),不比 acc.Endpoint 字符串:
// endpoint 还有 kimsufi-* / soyoustart-* 别名,散装比较漏一种写法门控就失效了。
func ChangeContact(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		if region := srvRegionFor(state, c); region == srvRegionUS {
			state.Logger.Warn("账户所在大区 "+region+" 不提供服务器联系人变更(changeContact),已拦截请求", "server_control")
			c.JSON(http.StatusBadRequest, gin.H{
				"success":     false,
				"error":       "美区账户不支持通过 API 变更服务器联系人 —— OVHcloud US 没有 NIC 联系人系统,请在 OVHcloud US 控制台或联系客服办理",
				"unsupported": true,
				"region":      region,
			})
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body map[string]interface{}
		_ = c.ShouldBindJSON(&body)
		params := map[string]interface{}{}
		if v, ok := body["contactAdmin"].(string); ok && v != "" {
			params["contactAdmin"] = v
		}
		if v, ok := body["contactTech"].(string); ok && v != "" {
			params["contactTech"] = v
		}
		if v, ok := body["contactBilling"].(string); ok && v != "" {
			params["contactBilling"] = v
		}
		if len(params) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "至少需要指定一个联系人（管理员、技术或计费）"})
			return
		}
		// OVH 这个接口返回 long[](任务 ID 数组),不是 map
		var taskIDs []int64
		if err := client.Post("/dedicated/server/"+svc+"/changeContact", params, &taskIDs); err != nil {
			state.Logger.Error("变更服务器 "+svc+" 联系人失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("服务器 %s 联系人变更请求已提交: %v, tasks=%v", svc, params, taskIDs), "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "联系人变更请求已提交",
			"taskIds": taskIDs,
		})
	}
}

// GetInterventions GET /api/server-control/:service_name/interventions
func GetInterventions(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var ids []interface{}
		if err := client.Get("/dedicated/server/"+svc+"/intervention", &ids); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// 并发拉详情。失败的条目不能静默丢弃,否则限流/权限问题会让列表凭空变短
		details, detailErrs := hardwareParallelGetDetails(client, ids, func(k interface{}) string {
			return "/dedicated/server/" + svc + "/intervention/" + idToString(k)
		}, 10)
		list := []map[string]interface{}{}
		for _, d := range details {
			if d != nil {
				list = append(list, d)
			}
		}
		failed := len(ids) - len(list)
		if len(ids) > 0 && failed == len(ids) {
			fe := hardwareFirstErr(detailErrs)
			state.Logger.Error("获取服务器 "+svc+" 干预记录详情全部失败: "+fe.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": fe.Error()})
			return
		}
		resp := gin.H{"success": true, "interventions": list}
		if failed > 0 {
			fe := hardwareFirstErr(detailErrs)
			state.Logger.Warn(fmt.Sprintf("服务器 %s 干预记录部分获取失败(%d/%d): %s", svc, failed, len(ids), fe.Error()), "server_control")
			resp["partial"] = true
			resp["failed"] = failed
			resp["error"] = "部分干预记录获取失败：" + fe.Error()
		}
		c.JSON(http.StatusOK, resp)
	}
}

// GetInterventionDetail GET /api/server-control/:service_name/interventions/:intervention_id
func GetInterventionDetail(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		id := c.Param("intervention_id")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/intervention/"+id, &d); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "intervention": d})
	}
}

// GetPlannedInterventions GET /api/server-control/:service_name/planned-interventions
func GetPlannedInterventions(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var ids []interface{}
		if err := client.Get("/dedicated/server/"+svc+"/plannedIntervention", &ids); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// 并发拉详情。同 GetInterventions:失败要显式暴露,不能让列表静默变空
		details, detailErrs := hardwareParallelGetDetails(client, ids, func(k interface{}) string {
			return "/dedicated/server/" + svc + "/plannedIntervention/" + idToString(k)
		}, 10)
		list := []map[string]interface{}{}
		for _, d := range details {
			if d != nil {
				list = append(list, d)
			}
		}
		failed := len(ids) - len(list)
		if len(ids) > 0 && failed == len(ids) {
			fe := hardwareFirstErr(detailErrs)
			state.Logger.Error("获取服务器 "+svc+" 计划干预详情全部失败: "+fe.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": fe.Error()})
			return
		}
		resp := gin.H{"success": true, "plannedInterventions": list}
		if failed > 0 {
			fe := hardwareFirstErr(detailErrs)
			state.Logger.Warn(fmt.Sprintf("服务器 %s 计划干预部分获取失败(%d/%d): %s", svc, failed, len(ids), fe.Error()), "server_control")
			resp["partial"] = true
			resp["failed"] = failed
			resp["error"] = "部分计划干预记录获取失败：" + fe.Error()
		}
		c.JSON(http.StatusOK, resp)
	}
}

// GetPlannedInterventionDetail GET /api/server-control/:service_name/planned-interventions/:intervention_id
func GetPlannedInterventionDetail(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		id := c.Param("intervention_id")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d map[string]interface{}
		if err := client.Get(fmt.Sprintf("/dedicated/server/%s/plannedIntervention/%s", svc, id), &d); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "plannedIntervention": d})
	}
}

// parseReplaceDisks 把请求里的故障盘列表转成 dedicated.server.SupportReplaceHddInfo[]。
// schema: disk_serial 必填 string / slot_id 可空 long。slot_id 拿不到就整个键不发,
// 免得给 OVH 一个 null 槽位号让工单校验失败。
func parseReplaceDisks(raw interface{}) ([]map[string]interface{}, error) {
	if raw == nil {
		return nil, nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("disks 必须是数组")
	}
	out := []map[string]interface{}{}
	for i, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("disks[%d] 必须是对象", i)
		}
		serial := ""
		// 前端可能用 snake_case(贴合 OVH schema)或 camelCase,两种都收
		for _, k := range []string{"disk_serial", "diskSerial", "serial"} {
			if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
				serial = strings.TrimSpace(v)
				break
			}
		}
		if serial == "" {
			return nil, fmt.Errorf("disks[%d] 缺少硬盘序列号 disk_serial", i)
		}
		disk := map[string]interface{}{"disk_serial": serial}
		for _, k := range []string{"slot_id", "slotId"} {
			if v, ok := numconv.ToInt64(m[k]); ok {
				disk["slot_id"] = v
				break
			}
		}
		out = append(out, disk)
	}
	return out, nil
}

// parseMemorySlots 把请求里的故障内存槽位转成 schema 要的 string[]。
// 前端可能给数组,也可能给一串逗号分隔的描述,两种都归一化;空值一律丢弃。
func parseMemorySlots(raw interface{}) []string {
	out := []string{}
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
	case string:
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// HardwareReplace POST /api/server-control/:service_name/hardware/replace
func HardwareReplace(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body map[string]interface{}
		_ = c.ShouldBindJSON(&body)
		componentType, _ := body["componentType"].(string)
		comment, _ := body["comment"].(string)
		if componentType == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 componentType 参数"})
			return
		}
		var result map[string]interface{}
		var err2 error
		switch componentType {
		case "hardDiskDrive":
			if comment == "" {
				comment = "Request hard disk drive replacement - faulty disk detected"
			}
			// schema 里 inverse 的语义是"更换所有【未列出】的盘",disks 是"要更换的盘"。
			// 老代码写死 disks:[] + inverse:true,等于向 OVH 申请更换整机每一块硬盘 ——
			// 故障盘必须由调用方显式给出,拿不到就宁可 400 也不发这张工单。
			disks, derr := parseReplaceDisks(body["disks"])
			if derr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": derr.Error()})
				return
			}
			if len(disks) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error":   "请指定要更换的故障硬盘：disks 至少需要一项且包含 disk_serial（硬盘序列号）",
				})
				return
			}
			// inverse 只有在调用方明确传 true 时才反转语义(即"除这些盘之外全换"),默认 false
			inverse := false
			if v, ok := body["inverse"].(bool); ok {
				inverse = v
			}
			err2 = client.Post("/dedicated/server/"+svc+"/support/replace/hardDiskDrive", map[string]interface{}{
				"comment": comment,
				"disks":   disks,
				"inverse": inverse,
			}, &result)
		case "memory":
			details := "Memory module failure"
			if v, ok := body["details"].(string); ok && v != "" {
				details = v
			}
			if comment == "" {
				comment = "Request memory module replacement - hardware failure detected"
			}
			// schema 只认 comment / details / slots(string[],可选);slotsDescription 这个字段
			// 在 EU/US schema 里都不存在。可选字段拿不到值就整个不发,不给 OVH 塞未知参数
			// (OVH 是否忽略未知 body 参数没有文档保证,不赌它的宽容度)。
			memoryBody := map[string]interface{}{
				"comment": comment,
				"details": details,
			}
			if slots := parseMemorySlots(body["slots"]); len(slots) > 0 {
				memoryBody["slots"] = slots
			}
			err2 = client.Post("/dedicated/server/"+svc+"/support/replace/memory", memoryBody, &result)
		case "cooling":
			details := "Cooling system failure"
			if v, ok := body["details"].(string); ok && v != "" {
				details = v
			}
			if comment == "" {
				comment = "Request cooling system replacement - fan failure or overheating"
			}
			err2 = client.Post("/dedicated/server/"+svc+"/support/replace/cooling", map[string]interface{}{
				"comment": comment,
				"details": details,
			}, &result)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "不支持的组件类型: " + componentType})
			return
		}
		if err2 != nil {
			errMsg := err2.Error()
			state.Logger.Error("硬件更换失败: "+svc+" - "+componentType+" - "+errMsg, "server_control")
			if strings.Contains(errMsg, "Action pending") {
				ticketID := "未知"
				if m := regexp.MustCompile(`ticketId[:\s]+(\d+)`).FindStringSubmatch(errMsg); m != nil {
					ticketID = m[1]
				}
				c.JSON(http.StatusBadRequest, gin.H{
					"success":   false,
					"error":     "已有待处理的硬件更换工单 (Ticket #" + ticketID + ")，请等待完成后再提交新请求",
					"ticketId":  ticketID,
					"isPending": true,
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": errMsg})
			return
		}
		state.Logger.Info("硬件更换请求已发送: "+svc+" - "+componentType, "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "硬件更换请求已发送", "task": result})
	}
}

// GetHardwareRaidProfiles GET /api/server-control/:service_name/hardware-raid-profiles
func GetHardwareRaidProfiles(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var profiles interface{}
		if err := client.Get("/dedicated/server/"+svc+"/install/hardwareRaidProfile", &profiles); err != nil {
			errMsg := strings.ToLower(err.Error())
			if strings.Contains(errMsg, "not supported") {
				c.JSON(http.StatusOK, gin.H{
					"success":   true,
					"profiles":  []interface{}{},
					"supported": false,
					"message":   "此服务器不支持硬件RAID",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "profiles": profiles, "supported": true})
	}
}

// GetHardwareDiskInfo GET /api/server-control/:service_name/hardware-disk-info
func GetHardwareDiskInfo(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var hardware map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/specifications/hardware", &hardware); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		diskGroups := map[string]interface{}{}
		if dgs, ok := hardware["diskGroups"].([]interface{}); ok {
			for _, dgRaw := range dgs {
				dg, ok := dgRaw.(map[string]interface{})
				if !ok {
					continue
				}
				id := 0
				if v, ok := numconv.ToInt64(dg["diskGroupId"]); ok {
					id = int(v)
				}
				numberOfDisks := 0
				if v, ok := numconv.ToInt64(dg["numberOfDisks"]); ok {
					numberOfDisks = int(v)
				}
				diskSizeValue := 0
				diskSizeUnit := "GB"
				if ds, ok := dg["diskSize"].(map[string]interface{}); ok {
					if v, ok := numconv.ToInt64(ds["value"]); ok {
						diskSizeValue = int(v)
					}
					if u, ok := ds["unit"].(string); ok {
						diskSizeUnit = u
					}
				}
				disks := []map[string]interface{}{}
				for i := 0; i < numberOfDisks; i++ {
					disks = append(disks, map[string]interface{}{
						"capacity": diskSizeValue,
						"unit":     diskSizeUnit,
						"number":   i + 1,
						"diskType": dg["diskType"],
					})
				}
				diskGroups[fmt.Sprintf("%d", id)] = map[string]interface{}{
					"id":             id,
					"diskType":       dg["diskType"],
					"description":    dg["description"],
					"raidController": dg["raidController"],
					"disks":          disks,
				}
			}
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "diskGroups": diskGroups, "hardware": hardware})
	}
}

// GetPartitionSchemes GET /api/server-control/:service_name/partition-schemes
func GetPartitionSchemes(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		templateName := c.Query("templateName")
		if templateName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少templateName参数"})
			return
		}
		encodedTpl := url.PathEscape(templateName)
		var schemes []string
		if err := client.Get("/dedicated/installationTemplate/"+encodedTpl+"/partitionScheme", &schemes); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// 双层嵌套并发：先并发拉每个 scheme 的 info + partition list，
		// 再对每个 scheme 内的 partition 并发拉详情
		type schemeResult struct {
			name        string
			info        map[string]interface{}
			parts       []string
			missingInfo bool
		}
		schemeResults := make([]schemeResult, len(schemes))
		sem := make(chan struct{}, 10)
		var wg sync.WaitGroup
		for i, schemeName := range schemes {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, sname string) {
				defer wg.Done()
				defer func() { <-sem }()
				es := url.PathEscape(sname)
				var info map[string]interface{}
				if err := client.Get("/dedicated/installationTemplate/"+encodedTpl+"/partitionScheme/"+es, &info); err != nil {
					schemeResults[idx] = schemeResult{name: sname, missingInfo: true}
					return
				}
				var partitions []string
				_ = client.Get("/dedicated/installationTemplate/"+encodedTpl+"/partitionScheme/"+es+"/partition", &partitions)
				schemeResults[idx] = schemeResult{name: sname, info: info, parts: partitions}
			}(i, schemeName)
		}
		wg.Wait()

		details := []gin.H{}
		for _, sr := range schemeResults {
			if sr.missingInfo {
				details = append(details, gin.H{"name": sr.name, "priority": 0, "partitions": []interface{}{}})
				continue
			}
			priority := 0
			if v, ok := numconv.ToInt64(sr.info["priority"]); ok {
				priority = int(v)
			}
			// 并发拉该 scheme 的 partition 详情
			encodedScheme := url.PathEscape(sr.name)
			partDetails := make([]gin.H, len(sr.parts))
			pSem := make(chan struct{}, 10)
			var pWg sync.WaitGroup
			for pi, part := range sr.parts {
				pWg.Add(1)
				pSem <- struct{}{}
				go func(pidx int, p string) {
					defer pWg.Done()
					defer func() { <-pSem }()
					var partInfo map[string]interface{}
					if err := client.Get("/dedicated/installationTemplate/"+encodedTpl+"/partitionScheme/"+encodedScheme+"/partition/"+url.PathEscape(p), &partInfo); err != nil {
						return
					}
					order := 0
					if v, ok := numconv.ToInt64(partInfo["order"]); ok {
						order = int(v)
					}
					partDetails[pidx] = gin.H{
						"mountpoint": p,
						"filesystem": valueOr(partInfo, "filesystem", ""),
						"size":       defaultZero(partInfo["size"]),
						"order":      order,
						"raid":       partInfo["raid"],
						"type":       valueOr(partInfo, "type", "primary"),
					}
				}(pi, part)
			}
			pWg.Wait()
			// 去掉拉取失败的 nil 项
			cleaned := make([]gin.H, 0, len(partDetails))
			for _, pd := range partDetails {
				if pd != nil {
					cleaned = append(cleaned, pd)
				}
			}
			// 冒泡按 order 排序
			for i := 1; i < len(cleaned); i++ {
				for j := i; j > 0; j-- {
					oi, _ := cleaned[j]["order"].(int)
					oj, _ := cleaned[j-1]["order"].(int)
					if oj > oi {
						cleaned[j-1], cleaned[j] = cleaned[j], cleaned[j-1]
					}
				}
			}
			details = append(details, gin.H{
				"name":       sr.name,
				"priority":   priority,
				"partitions": cleaned,
			})
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "schemes": details})
	}
}
