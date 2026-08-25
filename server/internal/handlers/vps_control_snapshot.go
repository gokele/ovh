package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
)

// GetVpsSnapshot GET /api/vps-control/:service_name/snapshot
//
// 注意:OVH 一个 VPS 同时只能有 0 或 1 个快照(免费档),不是数组。
// 没快照时端点返回 404,这里转换成 200 + snapshot:null,前端可以判断显示「无快照」。
//
// /vps/{name}/snapshot 返回 vps.Snapshot { id, creationDate, description, region },
// 该模型和 GET/PUT/DELETE 三个方法在 EU / CA / US 三区完全一致(US 标 BETA),
// createSnapshot / snapshot/revert 也一样 —— 整个快照家族不需要区域门控。
func GetVpsSnapshot(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var snap map[string]interface{}
		if err := client.Get("/vps/"+svc+"/snapshot", &snap); err != nil {
			// 404 = 没快照,正常状态。判 HTTP 状态码而不是匹配错误文案:
			// 三个站点是独立部署,报错文案各写各的(而且会随 OVH 改版变),
			// 匹配 "does not exist" 这类英文串在某个区一旦对不上,就会把"没快照"渲染成一条 500。
			if ovhIsNotFound(err) {
				c.JSON(http.StatusOK, gin.H{"success": true, "snapshot": nil})
				return
			}
			state.Logger.Error("VPS "+svc+" 读取快照失败: "+err.Error(), "vps_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "snapshot": snap})
	}
}

// CreateVpsSnapshot POST /api/vps-control/:service_name/snapshot
//
// POST /vps/{name}/createSnapshot 返回 vps.Task。
// body: { description?: string } —— OVH 允许给快照打描述
//
// 失败常见原因:已有一个快照(免费档不允许多个),需先 delete 旧的
func CreateVpsSnapshot(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Description string `json:"description"`
		}
		_ = c.ShouldBindJSON(&body)
		params := map[string]interface{}{}
		if body.Description != "" {
			params["description"] = body.Description
		}
		var task map[string]interface{}
		if err := client.Post("/vps/"+svc+"/createSnapshot", params, &task); err != nil {
			// 这里只能靠文案兜一层:OVH 对"已有快照"回的是 400/409,状态码本身区分不出
			// 具体原因。文案匹配仅用于把提示说得更友好,匹配不上也会照常把原始错误返回,
			// 不像 404 降级那样会改变语义,所以三区文案差异在这里是可接受的。
			errMsg := strings.ToLower(err.Error())
			if strings.Contains(errMsg, "already") || strings.Contains(errMsg, "exists") {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error":   "该 VPS 已存在快照,免费档同时只允许 1 个,请先删除旧快照",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS "+svc+" 创建快照任务已提交", "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "创建快照任务已提交", "task": task})
	}
}

// UpdateVpsSnapshotDescription PUT /api/vps-control/:service_name/snapshot
// 修改快照的描述字段(其他字段如 id/creationDate 只读)
// PUT /vps/{name}/snapshot 返回 void/vps.Snapshot,body 是完整 vps.Snapshot
func UpdateVpsSnapshotDescription(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Description string `json:"description"`
		}
		_ = c.ShouldBindJSON(&body)
		// 先 GET 拿完整对象再 merge,跟 PUT serviceInfos 同款 read-modify-write
		var snap map[string]interface{}
		if err := client.Get("/vps/"+svc+"/snapshot", &snap); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		snap["description"] = body.Description
		if err := client.Put("/vps/"+svc+"/snapshot", snap, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS "+svc+" 快照描述已更新", "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "快照描述已更新"})
	}
}

// RevertVpsSnapshot POST /api/vps-control/:service_name/snapshot/revert
//
// 把 VPS 回滚到当前快照状态。**destructive** —— 快照后的所有改动丢失。
// POST /vps/{name}/snapshot/revert 返回 vps.Task
func RevertVpsSnapshot(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var task map[string]interface{}
		if err := client.Post("/vps/"+svc+"/snapshot/revert", map[string]interface{}{}, &task); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Warn("VPS "+svc+" 已触发快照回滚 (destructive)", "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "回滚任务已提交,VPS 即将进入维护态", "task": task})
	}
}

// DeleteVpsSnapshot DELETE /api/vps-control/:service_name/snapshot
// 删除当前快照(不影响 VPS 当前状态,只是把快照本身清掉,腾出位置给新快照)
// DELETE /vps/{name}/snapshot 返回 vps.Task
func DeleteVpsSnapshot(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var task map[string]interface{}
		if err := client.Delete("/vps/"+svc+"/snapshot", &task); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("VPS "+svc+" 快照已删除", "vps_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "快照删除任务已提交", "task": task})
	}
}
