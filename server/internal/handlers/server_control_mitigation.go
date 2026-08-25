package handlers

import (
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
)

// GetMitigation GET /api/server-control/:service_name/mitigation
//
// 列服务器所有 IP 的 DDoS 缓解状态。
// OVH 的 /ip/{ip}/mitigation 端点要求 {ip} 是 IP 块(/32 用 %2F 转义),
// 但是从 /dedicated/server/{svc}/ips 拿到的就是 IP 块格式,直接拼。
//
// 返回结构:
//
//	ips: [{ ipBlock, mitigations: [{ ipOnMitigation, state, auto, permanent }] }]
func GetMitigation(state *app.State) gin.HandlerFunc {
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
		type ipResult struct {
			block       string
			mitigations []map[string]interface{}
			note        string
			err         error
		}
		results := make([]ipResult, len(ipBlocks))
		sem := make(chan struct{}, 8)
		// 详情请求单独限流：外层 goroutine 不参与这个信号量，两层不会互相饿死。
		detailSem := make(chan struct{}, 16)
		var wg sync.WaitGroup
		for i, blk := range ipBlocks {
			// /ip/{ip}/mitigation 返回 ipv4[]，ip.MitigationIp.ipOnMitigation 也是 ipv4：
			// anti-DDoS Mitigation 只覆盖 IPv4。对 IPv6 块发请求只会换回一条 OVH 报错，
			// 白白污染页面，不如直接标注跳过（与 Enable/DisableMitigation 的 IPv6 判断一致）。
			if strings.Contains(blk, ":") {
				results[i] = ipResult{
					block: blk,
					note:  "IPv6 不支持 anti-DDoS Mitigation（IPv6 默认有网络层免疫）",
				}
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, ipBlock string) {
				defer wg.Done()
				defer func() { <-sem }()
				encoded := strings.ReplaceAll(ipBlock, "/", "%2F")
				// 1) 列出该 block 下处于 mitigation 的具体 IP
				var ips []string
				if err := client.Get("/ip/"+encoded+"/mitigation", &ips); err != nil {
					results[idx] = ipResult{block: ipBlock, err: err}
					return
				}
				// 2) 并发拉每个 IP 的详情。失败的 IP 也必须留在结果里：
				// 直接跳过会让「正在被缓解」的 IP 从页面上凭空消失，前端据此显示
				// 「无永久缓解」并诱导用户重复点开启，属于把瞬时错误伪装成业务状态。
				details := make([]map[string]interface{}, len(ips))
				var dwg sync.WaitGroup
				for j, ip := range ips {
					dwg.Add(1)
					detailSem <- struct{}{}
					go func(jdx int, ipOnMitigation string) {
						defer dwg.Done()
						defer func() { <-detailSem }()
						var d map[string]interface{}
						if err := client.Get("/ip/"+encoded+"/mitigation/"+ipOnMitigation, &d); err != nil {
							state.Logger.Warn("[Mitigation] 获取 "+ipOnMitigation+" 缓解详情失败: "+err.Error(), "server_control")
							// 占位要带齐前端会读的字段,否则 state/auto/permanent 是 undefined,
							// 界面渲染出一个空白 Chip,看不出这行是"拉取失败"
							details[jdx] = map[string]interface{}{
								"ipOnMitigation": ipOnMitigation,
								"state":          "unknown",
								"auto":           false,
								"permanent":      false,
								"_detailError":   err.Error(),
							}
							return
						}
						details[jdx] = d
					}(j, ip)
				}
				dwg.Wait()
				results[idx] = ipResult{block: ipBlock, mitigations: details}
			}(i, blk)
		}
		wg.Wait()

		list := []gin.H{}
		for _, r := range results {
			row := gin.H{"ipBlock": r.block, "mitigations": r.mitigations}
			if r.err != nil {
				row["error"] = r.err.Error()
				state.Logger.Warn("[Mitigation] 获取 "+r.block+" 缓解列表失败: "+r.err.Error(), "server_control")
			}
			if r.note != "" {
				row["note"] = r.note
			}
			if r.mitigations == nil {
				row["mitigations"] = []interface{}{}
			}
			list = append(list, row)
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "ips": list})
	}
}

// EnableMitigation POST /api/server-control/:service_name/mitigation/:ip
// 对指定 IP 开 permanent mitigation。注意 :ip 参数是单个 IPv4,所属 block 用 query ?block=xxx
func EnableMitigation(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.Param("ip")
		ipBlock := c.Query("block")
		if ipBlock == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 block 参数(IP 所属的 ipBlock)"})
			return
		}
		// OVH ipOnMitigation 字段是 ipv4 类型,IPv6 走过去会 400
		if !strings.Contains(ip, ".") || strings.Contains(ip, ":") {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "OVH anti-DDoS Mitigation 只支持 IPv4。IPv6 地址默认有网络层免疫",
			})
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		encoded := strings.ReplaceAll(ipBlock, "/", "%2F")
		var result map[string]interface{}
		if err := client.Post("/ip/"+encoded+"/mitigation",
			map[string]interface{}{"ipOnMitigation": ip}, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("启用 IP "+ip+" 的永久 DDoS 缓解", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "DDoS 缓解已启用", "mitigation": result})
	}
}

// DisableMitigation DELETE /api/server-control/:service_name/mitigation/:ip?block=...
// 关闭指定 IP 的 permanent mitigation。auto mitigation 在攻击时仍会自动启用。
func DisableMitigation(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.Param("ip")
		ipBlock := c.Query("block")
		if ipBlock == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 block 参数"})
			return
		}
		if !strings.Contains(ip, ".") || strings.Contains(ip, ":") {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "IPv6 不支持 anti-DDoS Mitigation"})
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		encoded := strings.ReplaceAll(ipBlock, "/", "%2F")
		if err := client.Delete("/ip/"+encoded+"/mitigation/"+ip, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("关闭 IP "+ip+" 的永久 DDoS 缓解", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "DDoS 缓解已关闭"})
	}
}
