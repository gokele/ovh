package handlers

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/numconv"
	"github.com/ovh-buy/server/internal/ovh"
)

// ── VPS 区域门控 ────────────────────────────────────────────────────────────
//
// EU / US / CA 三个 OVH 站点是彼此独立的系统,/vps 命名空间的可用路径并不一致。
// 实测三站的 /1.0/vps.json:EU 74 条、CA 74 条(两者逐条完全相同)、US 只有 46 条。
// US 缺的 28 条(GET https://api.us.ovhcloud.com/1.0/vps.json 里查无此路径):
//
//	/vps/datacenter                 /vps/{sn}/availableUpgrade   /vps/{sn}/models
//	/vps/{sn}/status                /vps/{sn}/distribution(+/software)
//	/vps/{sn}/templates(+/{id}/software)                         /vps/{sn}/reinstall
//	/vps/{sn}/setPassword           /vps/{sn}/changeContact      /vps/{sn}/openConsoleAccess
//	/vps/{sn}/backupftp 全家桶(access / authorizableBlocks / password)
//	/vps/{sn}/veeam 全家桶(restorePoints / restoredBackup)
//	/vps/{sn}/migration2016  /vps/{sn}/migration2018  /vps/{sn}/use
//
// 其余路径(ips / snapshot / option / secondaryDnsDomains / automatedBackup /
// serviceInfos / tasks / rebuild / images/* / getConsoleUrl / datacenter …)三区都有,
// 只是 US 上整片 vps 命名空间标了 BETA —— BETA 不影响可调用性,不做门控。
//
// 门控一律走 ovh.EndpointRegion,不要在各 handler 里再写 `acc.Endpoint == "ovh-us"`:
// endpoint 是用户可填的自由字符串,还有 kimsufi-* / soyoustart-* 品牌别名,
// 散装比较早晚会漏掉一种写法。
const vpsRegionUS = "US"

// vpsRegionFor 当前请求所用账户的 OVH 大区(EU / US / CA)。
// 账户查不到时 acc 是零值,EndpointRegion("") 回落 EU —— 这条路径上紧接着的
// ovhClientFor 一定会失败并走 noOVHResp,所以不会出现"用错误的大区放行"的情况。
func vpsRegionFor(state *app.State, c *gin.Context) string {
	acc, _ := ovhAccountFor(state, c)
	return ovh.EndpointRegion(acc.Endpoint)
}

// vpsUnsupportedRead 只读接口在本大区不存在时的降级响应。
//
// 用 200 + 空值 + unsupported:true,而不是 404/500:这类面板是详情页一进来就并发拉的,
// 报错会在美区账户上常驻一条红色提示,用户以为是故障。前端按 unsupported 隐藏整块区域。
// field 是业务字段名(前端读的那个 key),value 给该字段的空值(nil / 空数组)。
func vpsUnsupportedRead(c *gin.Context, field string, value interface{}, message string) {
	c.JSON(http.StatusOK, gin.H{
		"success": true, field: value,
		"unsupported": true, "region": vpsRegionUS, "message": message,
	})
}

// vpsUnsupportedWrite 写操作在本大区不存在时的降级响应。
// 写操作必须失败(不能假装成功),但要给明确的中文原因和替代做法,而不是把 OVH 的 404 甩出去。
func vpsUnsupportedWrite(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{
		"success": false, "error": message,
		"unsupported": true, "region": vpsRegionUS,
	})
}

// ListVps GET /api/vps-control/list
//
// OVH /vps 返回 string[](serviceName 列表)。每个 VPS 并发拉 /vps/{name} 详情 +
// /vps/{name}/status 状态(running / stopped / migrating / ...)
func ListVps(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var names []string
		if err := client.Get("/vps", &names); err != nil {
			state.Logger.Error("获取 VPS 列表失败: "+err.Error(), "vps_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("获取 VPS 列表成功", "vps_control")

		type vpsResult struct {
			info   map[string]interface{}
			svc    map[string]interface{}
			err    error
			svcErr error
		}
		results := make([]vpsResult, len(names))
		sem := make(chan struct{}, 10)
		var wg sync.WaitGroup
		for i, name := range names {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, nm string) {
				defer wg.Done()
				defer func() { <-sem }()
				var info map[string]interface{}
				if err := client.Get("/vps/"+nm, &info); err != nil {
					results[idx].err = err
					return
				}
				results[idx].info = info
				var svcInfo map[string]interface{}
				// 10 并发拉列表时 OVH 很容易对 serviceInfos 限流。以前这里丢错,
				// 结果「没取到」被渲染成 renewalType:false / status:"unknown",跟真值无法区分。
				if err := client.Get("/vps/"+nm+"/serviceInfos", &svcInfo); err != nil {
					results[idx].svcErr = err
					return
				}
				results[idx].svc = svcInfo
			}(i, name)
		}
		wg.Wait()

		list := []gin.H{}
		for i, name := range names {
			r := results[i]
			if r.err != nil || r.info == nil {
				list = append(list, gin.H{"name": name, "serviceName": name, "error": "fetch failed"})
				continue
			}
			info := r.info
			// services.Service.renew 本身 canBeNull=true,再叠加拉取失败的情况:
			// 这两列只有真正取到值才输出,否则给 null,让前端显示「—」而不是编一个 false。
			var renewalType interface{}
			var svcStatus interface{}
			if r.svcErr != nil {
				state.Logger.Warn("VPS "+name+" serviceInfos 获取失败,续费/状态列返回 null: "+r.svcErr.Error(), "vps_control")
			} else if r.svc != nil {
				svcStatus = valueOr(r.svc, "status", "unknown")
				renewalType = false
				if rn, ok := r.svc["renew"].(map[string]interface{}); ok {
					if a, ok := rn["automatic"].(bool); ok {
						renewalType = a
					}
				}
			}
			// vps.Model 字段嵌套在 info["model"] 里
			vcore := 0
			memMB := 0
			diskGB := 0
			modelName := ""
			if model, ok := info["model"].(map[string]interface{}); ok {
				if v, ok := numconv.ToInt64(model["vcore"]); ok {
					vcore = int(v)
				}
				if v, ok := numconv.ToInt64(model["memory"]); ok {
					memMB = int(v)
				}
				if v, ok := numconv.ToInt64(model["disk"]); ok {
					diskGB = int(v)
				}
				modelName, _ = model["name"].(string)
			}
			// vps.LockStatus 是对象 { locked: bool, reason: enum } —— 不能直接给前端渲染,
			// 拍平成字符串。OVH 实测 reason 目前只有 "abuse",未锁定时 locked=false。
			lockStr := "unlocked"
			if ls, ok := info["lockStatus"].(map[string]interface{}); ok {
				if locked, _ := ls["locked"].(bool); locked {
					reason, _ := ls["reason"].(string)
					if reason != "" {
						lockStr = "locked (" + reason + ")"
					} else {
						lockStr = "locked"
					}
				}
			}
			list = append(list, gin.H{
				"serviceName":   name,
				"name":          name,
				"displayName":   valueOr(info, "displayName", name),
				"state":         valueOr(info, "state", "unknown"),
				"cluster":       valueOr(info, "cluster", ""),
				"zone":          valueOr(info, "zone", ""),
				"keymap":        valueOr(info, "keymap", "us"),
				"netbootMode":   valueOr(info, "netbootMode", "local"),
				"offerType":     valueOr(info, "offerType", ""),
				"slaMonitoring": info["slaMonitoring"], // 布尔字段不能走 valueOr,直接透传(nil 也 OK)
				"lockStatus":    lockStr,
				"model":         modelName,
				"vcore":         vcore,
				"memoryMB":      memMB,
				"diskGB":        diskGB,
				"status":        svcStatus,
				"renewalType":   renewalType,
			})
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "vps": list, "total": len(list)})
	}
}

// GetVpsInfo GET /api/vps-control/:service_name/info
// 返回 VPS 详细信息(model + state + zone 等)
func GetVpsInfo(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var info map[string]interface{}
		if err := client.Get("/vps/"+svc, &info); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "info": info})
	}
}

// GetVpsServiceStatus GET /api/vps-control/:service_name/status
//
// OVH /vps/{name}/status 返回 vps.ip.ServiceStatus(ping/dns/http/https/smtp/ssh/tools 服务端口探测),
// 跟 /vps/{name}.state(running/stopped/...)是两码事 —— 前者是网络服务存活,后者是 VPS 自身状态。
func GetVpsServiceStatus(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		// /vps/{serviceName}/status 在 EU 和 CA 两个站点都有(responseType 均为 vps.ip.ServiceStatus),
		// 只有 US 站点整片 vps 命名空间里没有这条路径,硬打过去必然 404。
		// 原注释写的"只在 EU 注册"是错的 —— 加拿大区同样可用,别照着它给 CA 也加门控。
		if vpsRegionFor(state, c) == vpsRegionUS {
			vpsUnsupportedRead(c, "status", nil,
				"美区 OVHcloud 未提供 VPS 服务端口探测接口(该端点仅欧洲区 / 加拿大区有);想看端口存活请自行用外部监控")
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var status map[string]interface{}
		if err := client.Get("/vps/"+svc+"/status", &status); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "status": status})
	}
}

// GetVpsServiceInfo GET /api/vps-control/:service_name/serviceinfo
// 跟 dedicated 一致:返回 renew + expiration + creation,字段名对齐已有的 RenewalDialog
func GetVpsServiceInfo(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var info map[string]interface{}
		if err := client.Get("/vps/"+svc+"/serviceInfos", &info); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		renew, _ := info["renew"].(map[string]interface{})
		automatic, period, delAtExp, forced, manualPay := false, 0, false, false, false
		if renew != nil {
			if v, ok := renew["automatic"].(bool); ok {
				automatic = v
			}
			if v, ok := numconv.ToInt64(renew["period"]); ok {
				period = int(v)
			}
			if v, ok := renew["deleteAtExpiration"].(bool); ok {
				delAtExp = v
			}
			if v, ok := renew["forced"].(bool); ok {
				forced = v
			}
			if v, ok := renew["manualPayment"].(bool); ok {
				manualPay = v
			}
		}
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
				"renewalType":               automatic,
				"renewalPeriod":             period,
				"renewalDeleteAtExpiration": delAtExp,
				"renewalForced":             forced,
				"renewalManualPayment":      manualPay,
				"possibleRenewPeriod":       possiblePeriods,
			},
		})
	}
}

// UpdateVpsRenewal PUT /api/vps-control/:service_name/serviceinfo/renewal
// 同 dedicated 的 UpdateServiceRenewal 逻辑:GET → merge renew → PUT
func UpdateVpsRenewal(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Mode   string `json:"mode"`
			Period int    `json:"period"`
		}
		_ = c.ShouldBindJSON(&body)

		var info map[string]interface{}
		if err := client.Get("/vps/"+svc+"/serviceInfos", &info); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		renew, _ := info["renew"].(map[string]interface{})
		if renew == nil {
			renew = map[string]interface{}{}
		}
		if f, ok := renew["forced"].(bool); ok && f {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "该 VPS 处于 OVH 合同期内,续费策略由 OVH 锁定"})
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
		delete(renew, "forced")
		// 同独服:services.Service 只有 renew 可写,整对象发回去会被 400
		if err := client.Put("/vps/"+svc+"/serviceInfos",
			map[string]interface{}{"renew": renew}, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS "+svc+" 续费策略已更新: "+body.Mode, "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "续费策略已更新"})
	}
}

// GetVpsIps GET /api/vps-control/:service_name/ips
// /vps/{name}/ips 返回 ip[](IP 字符串数组),为每个 IP 并发拉详情
func GetVpsIps(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var ips []string
		if err := client.Get("/vps/"+svc+"/ips", &ips); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		details := parallelGetStringKeys(client, ips, func(ip string) string {
			return "/vps/" + svc + "/ips/" + ip
		}, 8)
		list := []gin.H{}
		for i, ip := range ips {
			d := details[i]
			if d == nil {
				list = append(list, gin.H{"ipAddress": ip})
				continue
			}
			list = append(list, gin.H{
				"ipAddress":   valueOr(d, "ipAddress", ip),
				"reverse":     valueOr(d, "reverse", ""),
				"type":        valueOr(d, "type", ""),
				"version":     valueOr(d, "version", ""),
				"gateway":     valueOr(d, "gateway", ""),
				"geolocation": valueOr(d, "geolocation", ""),
				"macAddress":  valueOr(d, "macAddress", ""),
			})
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "ips": list, "total": len(list)})
	}
}

// SetVpsIpReverse PUT /api/vps-control/:service_name/ips/:ip/reverse
//
// OVH PUT /vps/{name}/ips/{ipAddress} 期望完整 vps.Ip 对象。read-modify-write:
// 先 GET 拿当前 ip 详情,改 reverse 字段,再整体 PUT 回去 —— 防止 OVH 把其他字段当 null 重置。
func SetVpsIpReverse(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		ip := c.Param("ip")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Reverse string `json:"reverse"`
		}
		_ = c.ShouldBindJSON(&body)
		// vps.Ip 里只有 reverse 可写(ipAddress / type / version / gateway /
		// geolocation / macAddress 都是只读),所以不需要先 GET 再 merge ——
		// 那样反而会把只读字段一起发回去,被 OVH 400 掉
		if err := client.Put("/vps/"+svc+"/ips/"+ip,
			map[string]interface{}{"reverse": body.Reverse}, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS "+svc+" IP "+ip+" 反向 DNS 设为 "+body.Reverse, "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "反向 DNS 已更新"})
	}
}

// GetVpsDatacenter GET /api/vps-control/:service_name/datacenter
//
// 别被区域 diff 里的 "/vps/datacenter 缺 US" 误导:那是全局机房清单,是另一条路径。
// 这里用的 /vps/{serviceName}/datacenter 三个站点都有(EU/CA 为 PRODUCTION、US 为 BETA,
// responseType 都是 vps.Datacenter),不需要门控。
func GetVpsDatacenter(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var dc map[string]interface{}
		if err := client.Get("/vps/"+svc+"/datacenter", &dc); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "datacenter": dc})
	}
}

// VPS CPU/内存监控两个端点 OVH 已废弃,不再实现:
//   /vps/{name}/monitoring  - DEPRECATED 2024-07-15(deletionDate 2024-09-15)
//   /vps/{name}/statistics  - DEPRECATED 2023-11-07(deletionDate 2024-01-07)
// 实测 US OVH 返 500 Internal Server Error。OVH 没提供替代的 VPS 级监控端点
// (只剩 /disks/{id}/use 磁盘级),所以前端干脆移除监控视图。看负载请登 VPS top。
