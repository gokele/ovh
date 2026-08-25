package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
)

// 区域核对结论(逐条对过 EU / US / CA 三站的 /1.0/ip.json):
// /ip、/ip/{ip}/mitigation、/ip/{ip}/mitigation/{ipOnMitigation} 三区都注册,
// 方法集(GET/POST/DELETE)、responseType(ipv4[] / ip.MitigationIp)、
// path 参数类型(ipBlock)、body 字段(ipOnMitigation: ipv4)、GET /ip 的 ip 过滤器
// 全部一字不差。所以 VPS 的 DDoS 缓解在三个大区都能用,不需要区域门控 ——
// 这里唯一的分支只跟 IP 协议版本有关,跟账户在哪个站点无关。

// isIPv4 简单判 IPv4 地址(含点号、不含冒号)。OVH 的 anti-DDoS mitigation 只支持 IPv4,
// IPv6 传过去会 400 "[ipOnMitigation] Given data is not valid for type ipv4"
func isIPv4(s string) bool {
	return strings.Contains(s, ".") && !strings.Contains(s, ":")
}

// resolveIPBlock 把裸 IP 换成它所属的真实 ipBlock(带掩码)。
//
// /ip/{ip}/mitigation 的 path 参数在 schema 里的类型是 ipBlock,而 /vps/{svc}/ips 的
// responseType 是 ip[](裸地址,不带掩码)。OVH 会不会对裸 IPv4 自动补 /32 属于运行时行为,
// schema 没有任何承诺,不能赌 —— 所以多发一次 GET /ip?ip=xxx(schema 上该 filter 语义是
// "contains or equals")把地址换成 OVH 认的规范形态。
// 查不到 / 出错时退回裸 IP,保证这次改动不会打断原本能跑通的路径。
//
// 只接受「以该 IP 开头」的精确匹配(x.x.x.x 或 x.x.x.x/nn),不再把唯一候选的上级网段
// (如 192.99.10.0/24)当成结果:返回值会被当作 ipBlock 发给前端,而前端
// VpsMitigationPane.tsx 是用 ipBlock.split("/")[0] 反推要缓解的 IP 的 ——
// 一旦给出网段,它就会把网段地址 192.99.10.0 当成 VPS 的 IP 提交给 OVH。
func resolveIPBlock(client *ovhsdk.Client, ip string) string {
	if strings.Contains(ip, "/") {
		return ip
	}
	var blocks []string
	if err := client.Get("/ip?ip="+url.QueryEscape(ip), &blocks); err != nil {
		return ip
	}
	for _, b := range blocks {
		if b == ip || strings.HasPrefix(b, ip+"/") {
			return b
		}
	}
	return ip
}

// GetVpsMitigation GET /api/vps-control/:service_name/mitigation
//
// 跟 dedicated 的 GetMitigation 逻辑一样,只是 IP 列表来源不同:
//   - dedicated: /dedicated/server/{svc}/ips     (返回 ipBlock[],带 mask)
//   - vps:       /vps/{svc}/ips                  (返回 ip[],单 IP)
//
// 返回的 ipBlock 字段给的是 resolveIPBlock 归一化后的值,前端把它原样当 block 回传给
// 启用/关闭接口,这样列表和写操作用的是同一个 OVH 认的服务名。
func GetVpsMitigation(state *app.State) gin.HandlerFunc {
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
		type ipResult struct {
			ip          string
			block       string
			mitigations []map[string]interface{}
			err         error
		}
		results := make([]ipResult, len(ips))
		sem := make(chan struct{}, 8)
		var wg sync.WaitGroup
		for i, ip := range ips {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, ipAddr string) {
				defer wg.Done()
				defer func() { <-sem }()
				// OVH anti-DDoS 不覆盖 IPv6(GET 返回 ipv4[],POST 的 ipOnMitigation 也是 ipv4)。
				// 以前 VPS 的 IPv6 也照打一次注定失败的请求,把 OVH 的英文报错渲染到面板上,
				// 还白白吃限流额度 —— 直接跳过,前端已有「IPv6 不适用」的分支。
				if !isIPv4(ipAddr) {
					results[idx] = ipResult{ip: ipAddr, block: ipAddr}
					return
				}
				block := resolveIPBlock(client, ipAddr)
				encoded := strings.ReplaceAll(block, "/", "%2F")
				var miti []string
				if err := client.Get("/ip/"+encoded+"/mitigation", &miti); err != nil {
					results[idx] = ipResult{ip: ipAddr, block: block, err: err}
					return
				}
				// 详情拉不到时写占位而不是丢行:静默少一行会让用户以为这个 IP 没开缓解。
				// 占位字段与专用服务器侧(server_control_mitigation.go)保持一致,前端共用同一套渲染。
				details := make([]map[string]interface{}, 0, len(miti))
				for _, m := range miti {
					var d map[string]interface{}
					if err := client.Get("/ip/"+encoded+"/mitigation/"+m, &d); err != nil {
						state.Logger.Warn("[VPS Mitigation] 获取 "+m+" 缓解详情失败: "+err.Error(), "vps_control")
						details = append(details, map[string]interface{}{
							"ipOnMitigation": m,
							"state":          "unknown",
							"auto":           false,
							"permanent":      false,
							"_detailError":   err.Error(),
						})
						continue
					}
					details = append(details, d)
				}
				results[idx] = ipResult{ip: ipAddr, block: block, mitigations: details}
			}(i, ip)
		}
		wg.Wait()

		list := []gin.H{}
		for _, r := range results {
			block := r.block
			if block == "" {
				block = r.ip
			}
			row := gin.H{"ipBlock": block, "ipAddress": r.ip, "mitigations": r.mitigations}
			if r.err != nil {
				row["error"] = r.err.Error()
			}
			if r.mitigations == nil {
				row["mitigations"] = []interface{}{}
			}
			list = append(list, row)
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "ips": list})
	}
}

// EnableVpsMitigation POST /api/vps-control/:service_name/mitigation/:ip?block=xxx
func EnableVpsMitigation(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.Param("ip")
		ipBlock := c.Query("block")
		if ipBlock == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 block 参数"})
			return
		}
		if !isIPv4(ip) {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "OVH anti-DDoS Mitigation 只支持 IPv4。IPv6 地址在 OVH 网络层默认免疫常见 volumetric 攻击,无需手动配置",
			})
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		// 前端可能拿到的是老响应里的裸 IP(或用户手工调接口),这里再归一化一次:
		// 写操作频率低,多一次 GET /ip 换取路径参数符合 schema 的 ipBlock 类型是划算的。
		encoded := strings.ReplaceAll(resolveIPBlock(client, ipBlock), "/", "%2F")
		var result map[string]interface{}
		if err := client.Post("/ip/"+encoded+"/mitigation",
			map[string]interface{}{"ipOnMitigation": ip}, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS IP "+ip+" 启用永久 DDoS 缓解", "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "DDoS 缓解已启用", "mitigation": result})
	}
}

// DisableVpsMitigation DELETE /api/vps-control/:service_name/mitigation/:ip?block=xxx
func DisableVpsMitigation(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.Param("ip")
		ipBlock := c.Query("block")
		if ipBlock == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 block 参数"})
			return
		}
		if !isIPv4(ip) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "IPv6 不支持 anti-DDoS Mitigation"})
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		encoded := strings.ReplaceAll(resolveIPBlock(client, ipBlock), "/", "%2F")
		if err := client.Delete("/ip/"+encoded+"/mitigation/"+ip, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS IP "+ip+" 关闭永久 DDoS 缓解", "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "DDoS 缓解已关闭"})
	}
}
