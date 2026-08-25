package handlers

import (
	"fmt"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
)

// ChangeVpsContact POST /api/vps-control/:service_name/change-contact
//
// EU + CA 有,US 没有 —— 原注释写的 "EU only" 是错的:加拿大区(ca.api.ovh.com)同样注册了
// POST /vps/{serviceName}/changeContact → long[]。只有 api.us.ovhcloud.com 的 vps 命名空间里
// 查无此路径:OVHcloud US 是独立公司,客户实体走 us.ovhcloud.com 自己的账户体系,没有 NIC 联系人。
// 这里提前拒,避免 OVH 报 404 让人摸不着头脑。
func ChangeVpsContact(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		if vpsRegionFor(state, c) == vpsRegionUS {
			vpsUnsupportedWrite(c, "美区 OVHcloud 未提供 VPS「变更联系人」接口(该端点仅欧洲区 / 加拿大区有)——"+
				"US 站点没有 NIC 联系人体系,过户需直接联系 OVHcloud US 客服")
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
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "至少需要指定一个联系人"})
			return
		}
		var taskIDs []int64
		if err := client.Post("/vps/"+svc+"/changeContact", params, &taskIDs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("VPS %s 联系人变更已提交: %v, tasks=%v", svc, params, taskIDs), "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "联系人变更请求已提交", "taskIds": taskIDs})
	}
}

// TerminateVps POST /api/vps-control/:service_name/terminate
// 跟 dedicated 一致:OVH 返回 string(确认 token,通过邮件验证)
func TerminateVps(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var token string
		if err := client.Post("/vps/"+svc+"/terminate", map[string]interface{}{}, &token); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Warn("VPS "+svc+" 终止请求已提交,等邮件 token", "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "终止请求已提交,请查邮件获取 token", "token": token})
	}
}

// ConfirmVpsTermination POST /api/vps-control/:service_name/confirm-termination
// /vps/{name}/confirmTermination 返回 string(确认消息)。body 至少需要 token + commentary 之一
func ConfirmVpsTermination(state *app.State) gin.HandlerFunc {
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
			Commentary string `json:"commentary"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Token == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 token"})
			return
		}
		params := map[string]interface{}{"token": body.Token}
		if body.Reason != "" {
			params["reason"] = body.Reason
		}
		if body.Commentary != "" {
			params["commentary"] = body.Commentary
		}
		var resp string
		if err := client.Post("/vps/"+svc+"/confirmTermination", params, &resp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Warn("VPS "+svc+" 终止已确认", "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "终止已确认"})
	}
}

// GetVpsSecondaryDns GET /api/vps-control/:service_name/secondary-dns
// /vps/{name}/secondaryDnsDomains 返回 string[](域名数组)
func GetVpsSecondaryDns(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var domains []string
		if err := client.Get("/vps/"+svc+"/secondaryDnsDomains", &domains); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		details := parallelGetStringKeys(client, domains, func(d string) string {
			return "/vps/" + svc + "/secondaryDnsDomains/" + d
		}, 8)
		list := []interface{}{}
		for i, d := range domains {
			if details[i] == nil {
				list = append(list, map[string]interface{}{"domain": d})
				continue
			}
			details[i]["domain"] = d
			list = append(list, details[i])
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "domains": list})
	}
}

// AddVpsSecondaryDns POST /api/vps-control/:service_name/secondary-dns
//
// body: { domain(必填), ip?(可选,主 DNS 的 IPv4) }。OVH 返回 void。
// 官方模型 vps.secondaryDnsDomains.post 里只有 domain required=true,ip 是 required=false 的 ipv4 ——
// 不填时 OVH 自己去解析域名的主 DNS,所以这里不能比 OVH 更严。
func AddVpsSecondaryDns(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Domain string `json:"domain"`
			IP     string `json:"ip"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Domain == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "domain 必填"})
			return
		}
		params := map[string]interface{}{"domain": body.Domain}
		if body.IP != "" {
			// schema 里该字段类型是 ipv4,先在本地挡掉 IPv6/乱填,避免把 OVH 的英文类型错误甩给用户
			if parsed := net.ParseIP(body.IP); parsed == nil || parsed.To4() == nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "ip 必须是合法的 IPv4 地址(OVH 二级 DNS 只接受 IPv4)"})
				return
			}
			params["ip"] = body.IP
		}
		if err := client.Post("/vps/"+svc+"/secondaryDnsDomains", params, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS "+svc+" 添加二级 DNS "+body.Domain, "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "二级 DNS 域名已添加"})
	}
}

// DeleteVpsSecondaryDns DELETE /api/vps-control/:service_name/secondary-dns/:domain
func DeleteVpsSecondaryDns(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		domain := c.Param("domain")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		if err := client.Delete("/vps/"+svc+"/secondaryDnsDomains/"+domain, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS "+svc+" 删除二级 DNS "+domain, "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "二级 DNS 域名已删除"})
	}
}

// usUnmanageableVpsOptions 美区列得出来、却管不了的附加选项。
//
// vps.VpsOptionEnum 三区完全一致(additionalDisk / automatedBackup / cpanel / ftpbackup /
// plesk / snapshot / veeam / windows),所以 /vps/{sn}/option 在美区照样能返回 ftpbackup、veeam
// —— 但管理这两项的整套端点在 api.us.ovhcloud.com 上根本不存在:
//
//	ftpbackup → /vps/{sn}/backupftp、/backupftp/access、/backupftp/access/{ipBlock}、
//	            /backupftp/authorizableBlocks、/backupftp/password  全部缺失
//	veeam     → /vps/{sn}/veeam、/veeam/restorePoints(/{id}/restore)、/veeam/restoredBackup 全部缺失
//
// 光返回选项名会让前端渲染出点不动的入口。这里给每行打 manageEndpointsAvailable 标记
// + 中文原因,前端据此把该选项的「管理」入口置灰,而不是点进去弹一堆 404。
//
// 注意这个标记只说"没有专属管理端点",不影响退订:DELETE /vps/{sn}/option/{option}
// 在三区都注册(且三区都标 DEPRECATED),美区照样能取消 ftpbackup / veeam,
// 所以前端不要拿它去禁用「取消选项」按钮。
var usUnmanageableVpsOptions = map[string]string{
	"ftpbackup": "美区 OVHcloud 未提供 VPS 备份 FTP 管理接口(/vps/{服务名}/backupftp 全套仅欧洲区 / 加拿大区有),请到 OVHcloud US 控制面板操作",
	"veeam":     "美区 OVHcloud 未提供 VPS Veeam 备份管理接口(/vps/{服务名}/veeam 全套仅欧洲区 / 加拿大区有),请到 OVHcloud US 控制面板操作",
}

// GetVpsOptions GET /api/vps-control/:service_name/options
// /vps/{name}/option 返回 vps.VpsOptionEnum[](string enum 数组),每个 /option/{name} 是 vps.Option 详情。
// 这两条路径三区都有(US 标 BETA),不做整体门控;只对个别在美区管不了的选项打标记。
func GetVpsOptions(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		isUS := vpsRegionFor(state, c) == vpsRegionUS
		var opts []string
		if err := client.Get("/vps/"+svc+"/option", &opts); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		details := parallelGetStringKeys(client, opts, func(o string) string {
			return "/vps/" + svc + "/option/" + o
		}, 8)
		list := []interface{}{}
		for i, opt := range opts {
			row := details[i]
			if row == nil {
				row = map[string]interface{}{}
			}
			row["option"] = opt
			// 只表示"这个选项有没有专属管理端点",不代表选项本身没生效 ——
			// 美区的 ftpbackup/veeam 照样在计费、照样在跑,只是没有 API 可管。
			row["manageEndpointsAvailable"] = true
			if isUS {
				if reason, bad := usUnmanageableVpsOptions[opt]; bad {
					row["manageEndpointsAvailable"] = false
					row["unsupportedReason"] = reason
					row["region"] = vpsRegionUS
				}
			}
			list = append(list, row)
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "options": list})
	}
}

// DeleteVpsOption DELETE /api/vps-control/:service_name/options/:option[?deleteNow=true]
//
// 注意:该端点在 EU / US / CA 三区 schema 里都标了 DEPRECATED(deprecatedDate 2023-12-22,
// deletionDate 2024-06-01 已过),OVH 随时可能真下线,届时会直接 404。schema 没给 replacement,
// 所以只能继续用,同时把「已废弃」透传给前端提示用户。
//
// deleteNow 是 schema 里的可选 query 参数(Delete option now, don't wait for expiration)。
// 不传时 OVH 的默认行为是「等计费周期结束才释放」,以前无条件回「已取消」是错的 ——
// 现在按实际传的参数给不同文案。
func DeleteVpsOption(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		opt := c.Param("option")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		deleteNow := c.Query("deleteNow") == "true" || c.Query("deleteNow") == "1"
		path := "/vps/" + svc + "/option/" + opt
		if deleteNow {
			path += "?deleteNow=true"
		}
		if err := client.Delete(path, nil); err != nil {
			state.Logger.Error("VPS "+svc+" 取消附加选项 "+opt+" 失败: "+err.Error(), "vps_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		msg := "附加选项已提交取消,将在当前计费周期结束时释放"
		if deleteNow {
			msg = "附加选项已立即释放"
		}
		state.Logger.Info(fmt.Sprintf("VPS %s 取消附加选项 %s (deleteNow=%v)", svc, opt, deleteNow), "vps_control")
		c.JSON(http.StatusOK, gin.H{
			"success": true, "message": msg, "deleteNow": deleteNow,
			"deprecated": true,
		})
	}
}

// GetVpsAutomatedBackup GET /api/vps-control/:service_name/automated-backup
// 高端 VPS 才有,/vps/{name}/automatedBackup 返回 vps.AutomatedBackup 对象,无则 404。
//
// 这条跟 backupftp / veeam 不是一回事:automatedBackup 全家桶三区都注册了(US 标 BETA),
// 不要因为"美区缺备份相关端点"就顺手给它加门控。
func GetVpsAutomatedBackup(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d map[string]interface{}
		if err := client.Get("/vps/"+svc+"/automatedBackup", &d); err != nil {
			// 只有 404 才代表「这台 VPS 没订阅自动备份」。401/403/429/5xx 也吞成 null 的话,
			// 已经买了备份的机器会被显示成「无备份」,这比报错更危险。
			if ovhIsNotFound(err) {
				c.JSON(http.StatusOK, gin.H{"success": true, "automatedBackup": nil})
				return
			}
			state.Logger.Error("VPS "+svc+" 查询自动备份失败: "+err.Error(), "vps_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "automatedBackup": d})
	}
}
