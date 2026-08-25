package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/numconv"
)

// GetBootConfig GET /api/server-control/:service_name/boot
func GetBootConfig(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var info map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc, &info); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		bootID := info["bootId"]
		bootIDInt, _ := numconv.ToInt64(bootID)
		var bootList []int64
		// 这个错误之前被吞掉：OVH 限流/权限不足时前端只会看到"没有可用启动模式"，
		// 用户以为机器不支持切 rescue，实际是调用失败，必须原样报出来
		if err := client.Get("/dedicated/server/"+svc+"/boot", &bootList); err != nil {
			state.Logger.Error("获取服务器 "+svc+" 启动模式列表失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// 并发拉每个 boot id 的详情
		details := make([]map[string]interface{}, len(bootList))
		detailErrs := make([]error, len(bootList))
		sem := make(chan struct{}, 10)
		var wg sync.WaitGroup
		for i, bid := range bootList {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, b int64) {
				defer wg.Done()
				defer func() { <-sem }()
				var d map[string]interface{}
				if err := client.Get(fmt.Sprintf("/dedicated/server/%s/boot/%d", svc, b), &d); err != nil {
					detailErrs[idx] = err
					return
				}
				details[idx] = d
			}(i, bid)
		}
		wg.Wait()

		boots := []gin.H{}
		failed := 0
		for i, bid := range bootList {
			detail := details[i]
			isCurrent := bootIDInt == bid
			// 详情失败的项之前直接从列表里消失，导致启动模式时多时少；
			// 保留占位并带上错误，前端才有机会提示重试
			if detail == nil {
				failed++
				msg := "启动模式详情获取失败"
				if detailErrs[i] != nil {
					msg = detailErrs[i].Error()
				}
				state.Logger.Error(fmt.Sprintf("获取服务器 %s 启动模式 %d 详情失败: %s", svc, bid, msg), "server_control")
				boots = append(boots, gin.H{
					"id":          bid,
					"bootType":    "unknown",
					"description": "",
					"kernel":      "",
					"isCurrent":   isCurrent,
					"error":       msg,
				})
				continue
			}
			boots = append(boots, gin.H{
				"id":          bid,
				"bootType":    valueOr(detail, "bootType", "N/A"),
				"description": valueOr(detail, "description", ""),
				"kernel":      valueOr(detail, "kernel", ""),
				"isCurrent":   isCurrent,
			})
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "currentBootId": bootID, "boots": boots, "failed": failed})
	}
}

// SetBootConfig PUT /api/server-control/:service_name/boot/:boot_id
func SetBootConfig(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		bootIDStr := c.Param("boot_id")
		// Python 路由 <int:boot_id> 强制转 int，OVH API bootId 字段也要求整数
		// 之前 Go 把字符串直接塞进 body 会被 OVH 拒
		bootID, err := strconv.ParseInt(bootIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "boot_id 必须是整数"})
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		if err := client.Put("/dedicated/server/"+svc, map[string]interface{}{
			"bootId": bootID,
		}, nil); err != nil {
			state.Logger.Error("设置服务器 "+svc+" 启动模式失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("服务器 %s 启动模式已设置为 %d", svc, bootID), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "启动模式已更新，重启后生效"})
	}
}

// GetMonitoringStatus GET /api/server-control/:service_name/monitoring
func GetMonitoringStatus(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var info map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc, &info); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// 1:1 对应 Python app.py:6111：缺失时默认 false
		monitoring := info["monitoring"]
		if monitoring == nil {
			monitoring = false
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "monitoring": monitoring})
	}
}

// SetMonitoringStatus PUT /api/server-control/:service_name/monitoring
func SetMonitoringStatus(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		// 前端发的字段名是 monitoring，而这里只绑 enabled 且吞掉绑定错误，
		// body.Enabled 恒为 false —— 点"开启监控"反而把 OVH 侧监控关了，宕机不再告警。
		// 两个字段名都收，并且必须是显式布尔，缺了宁可 400 也不能默认成关闭
		var body struct {
			Enabled    *bool `json:"enabled"`
			Monitoring *bool `json:"monitoring"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求体格式错误: " + err.Error()})
			return
		}
		enabled := body.Enabled
		if enabled == nil {
			enabled = body.Monitoring
		}
		if enabled == nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 enabled 参数（必须显式传 true 或 false）"})
			return
		}
		if err := client.Put("/dedicated/server/"+svc, map[string]interface{}{
			"monitoring": *enabled,
		}, nil); err != nil {
			state.Logger.Error("设置服务器 "+svc+" 监控失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		statusText := "开启"
		if !*enabled {
			statusText = "关闭"
		}
		state.Logger.Info("服务器 "+svc+" 监控已"+statusText, "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "监控已" + statusText})
	}
}

// GetBootModes GET /api/server-control/:service_name/boot-mode
func GetBootModes(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var info map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc, &info); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		currentBootID := info["bootId"]
		currentBootIDInt, _ := numconv.ToInt64(currentBootID)
		var bootIDs []int64
		// 同 GetBootConfig：吞掉这个错误会让 boot-mode 页面显示成空列表且无任何提示
		if err := client.Get("/dedicated/server/"+svc+"/boot", &bootIDs); err != nil {
			state.Logger.Error("获取服务器 "+svc+" 启动模式列表失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// 并发拉每个 boot id 详情
		biDetails := make([]map[string]interface{}, len(bootIDs))
		biErrs := make([]error, len(bootIDs))
		bSem := make(chan struct{}, 10)
		var bWg sync.WaitGroup
		for i, bid := range bootIDs {
			bWg.Add(1)
			bSem <- struct{}{}
			go func(idx int, b int64) {
				defer bWg.Done()
				defer func() { <-bSem }()
				var bi map[string]interface{}
				if err := client.Get(fmt.Sprintf("/dedicated/server/%s/boot/%d", svc, b), &bi); err != nil {
					biErrs[idx] = err
					return
				}
				biDetails[idx] = bi
			}(i, bid)
		}
		bWg.Wait()

		modes := []gin.H{}
		failed := 0
		for i, bid := range bootIDs {
			bi := biDetails[i]
			active := currentBootIDInt == bid
			// 失败的项保留占位，不要让启动模式凭空少几个
			if bi == nil {
				failed++
				msg := "启动模式详情获取失败"
				if biErrs[i] != nil {
					msg = biErrs[i].Error()
				}
				state.Logger.Error(fmt.Sprintf("[Boot] 获取服务器 %s 启动模式 %d 详情失败: %s", svc, bid, msg), "server_control")
				modes = append(modes, gin.H{
					"id":          bid,
					"bootType":    "unknown",
					"description": "",
					"kernel":      "",
					"active":      active,
					"error":       msg,
				})
				continue
			}
			modes = append(modes, gin.H{
				"id":          bid,
				"bootType":    bi["bootType"],
				"description": bi["description"],
				"kernel":      bi["kernel"],
				"active":      active,
			})
		}
		state.Logger.Info(fmt.Sprintf("[Boot] 找到 %d 个启动模式(%d 个详情获取失败)", len(modes), failed), "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success":       true,
			"currentBootId": currentBootID,
			"bootModes":     modes,
			"failed":        failed,
		})
	}
}

// ChangeBootMode PUT /api/server-control/:service_name/boot-mode
func ChangeBootMode(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			BootID int64 `json:"bootId"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.BootID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少bootId参数"})
			return
		}
		state.Logger.Info(fmt.Sprintf("[Boot] 切换服务器 %s 启动模式到 %d", svc, body.BootID), "server_control")
		if err := client.Put("/dedicated/server/"+svc, map[string]interface{}{
			"bootId": body.BootID,
		}, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("[Boot] 启动模式切换成功，需要重启服务器生效", "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "启动模式已切换，需要重启服务器生效",
			"bootId":  body.BootID,
		})
	}
}
