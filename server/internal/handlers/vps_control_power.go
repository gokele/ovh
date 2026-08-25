package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
)

// VpsStart POST /api/vps-control/:service_name/start
// /vps/{name}/start 返回 vps.Task 对象 { id, state, type, progress, date }
func VpsStart(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var task map[string]interface{}
		if err := client.Post("/vps/"+svc+"/start", map[string]interface{}{}, &task); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS "+svc+" 启动任务已创建", "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "启动任务已创建", "task": task})
	}
}

// VpsStop POST /api/vps-control/:service_name/stop
// 注意:OVH 不会因为 stop 停止计费;省电是物理服务器视角,VPS 仍占用 hypervisor 配额
func VpsStop(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var task map[string]interface{}
		if err := client.Post("/vps/"+svc+"/stop", map[string]interface{}{}, &task); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS "+svc+" 关机任务已创建", "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "关机任务已创建", "task": task})
	}
}

// VpsReboot POST /api/vps-control/:service_name/reboot
func VpsReboot(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var task map[string]interface{}
		if err := client.Post("/vps/"+svc+"/reboot", map[string]interface{}{}, &task); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS "+svc+" 重启任务已创建", "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "重启任务已创建", "task": task})
	}
}

// VpsGetConsoleUrl POST /api/vps-control/:service_name/console
// OVH POST /vps/{name}/getConsoleUrl 返回 string(noVNC 一次性 URL,典型 5 分钟有效)
//
// 三区都有这条路径,不需要门控。注意别改用 /vps/{name}/openConsoleAccess(返回 vps.Vnc)——
// 那条只在 EU/CA 注册,US 站点没有,换过去美区控制台会直接坏掉。
func VpsGetConsoleUrl(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var url string
		if err := client.Post("/vps/"+svc+"/getConsoleUrl", map[string]interface{}{}, &url); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS "+svc+" 控制台 URL 已生成", "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "url": url})
	}
}

// VpsSetPassword POST /api/vps-control/:service_name/password
//
// EU + CA 有,US 没有 —— 原注释写的 "EU only" 是错的,加拿大区
// (ca.api.ovh.com)同样注册了 POST /vps/{serviceName}/setPassword → vps.Task。
// 只有 api.us.ovhcloud.com 的 vps 命名空间里查无此路径,美区用户得在 noVNC 控制台里用 passwd 自助改。
// 这里提前拒,避免把 OVH 的英文 404 甩给用户。
func VpsSetPassword(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		if vpsRegionFor(state, c) == vpsRegionUS {
			vpsUnsupportedWrite(c, "美区 OVHcloud 未提供 VPS 远程重置密码接口(该端点仅欧洲区 / 加拿大区有)——"+
				"请点「控制台」打开 noVNC,进系统后用 passwd 命令自助修改")
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var task map[string]interface{}
		if err := client.Post("/vps/"+svc+"/setPassword", map[string]interface{}{}, &task); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS "+svc+" 密码重置任务已创建,新密码将邮件发送", "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "密码重置任务已创建,新密码已发送至邮箱", "task": task})
	}
}
