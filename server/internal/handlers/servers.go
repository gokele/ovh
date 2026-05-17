package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/price"
	"github.com/ovh-buy/server/internal/types"
)

// GetServers GET /api/servers
func GetServers(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		showAPI := strings.EqualFold(c.Query("showApiServers"), "true")
		forceRefresh := strings.EqualFold(c.Query("forceRefresh"), "true")

		usingExpiredCache := false
		cacheAgeMinutes := 0

		cached, valid := state.ServerCache.Get()
		if state.ServerCache.Timestamp != nil {
			cacheAgeMinutes = int(time.Since(*state.ServerCache.Timestamp).Minutes())
		}

		hasOVH := state.Config.HasCredentials()
		var serverPlans []types.ServerPlan

		if valid && !forceRefresh {
			state.Logger.Info("使用缓存的服务器列表 (缓存时间: "+strconv.Itoa(cacheAgeMinutes)+" 分钟前)", "")
			serverPlans = cached
		} else if showAPI && hasOVH {
			state.Logger.Info("正在从OVH API重新加载服务器列表...", "")
			apiServers := catalog.LoadServerList(state)
			if len(apiServers) > 0 {
				state.ServerPlansMu.Lock()
				state.ServerPlans = apiServers
				state.ServerPlansMu.Unlock()
				state.ServerCache.Set(apiServers)
				_ = state.SaveServers()
				serverPlans = apiServers
				state.Logger.Info("从OVH API加载了 "+strconv.Itoa(len(apiServers))+" 台服务器，已更新缓存", "")
			} else {
				state.Logger.Warn("从OVH API加载服务器列表失败或返回空数据", "")
				if len(cached) > 0 {
					serverPlans = cached
					usingExpiredCache = true
					state.Logger.Warn("⚠️ OVH API 调用失败，使用过期缓存数据", "")
				} else {
					state.ServerPlansMu.RLock()
					n := len(state.ServerPlans)
					serverPlans = make([]types.ServerPlan, n)
					copy(serverPlans, state.ServerPlans)
					state.ServerPlansMu.RUnlock()
					if n > 0 {
						usingExpiredCache = true
						state.Logger.Warn("⚠️ OVH API 调用失败，使用全局服务器数据", "")
					} else {
						state.Logger.Error("❌ OVH API 调用失败且没有缓存数据可用！", "")
						c.JSON(http.StatusServiceUnavailable, gin.H{
							"error":   "No data available",
							"message": "无法获取服务器列表：OVH API 调用失败且没有缓存数据",
						})
						return
					}
				}
			}
		} else if !valid && len(cached) > 0 {
			usingExpiredCache = true
			state.Logger.Warn("⚠️ 缓存已过期但未配置 OVH API，使用过期缓存数据", "")
			serverPlans = cached
		}

		// 验证并补全字段
		validated := make([]types.ServerPlan, 0, len(serverPlans))
		for _, s := range serverPlans {
			if s.Name == "" {
				s.Name = "未命名服务器"
			}
			if s.CPU == "" {
				s.CPU = "N/A"
			}
			if s.Memory == "" {
				s.Memory = "N/A"
			}
			if s.Storage == "" {
				s.Storage = "N/A"
			}
			if s.Bandwidth == "" {
				s.Bandwidth = "N/A"
			}
			if s.VrackBandwidth == "" {
				s.VrackBandwidth = "N/A"
			}
			if s.DefaultOptions == nil {
				s.DefaultOptions = []types.ServerOption{}
			}
			if s.AvailableOptions == nil {
				s.AvailableOptions = []types.ServerOption{}
			}
			if s.Datacenters == nil {
				s.Datacenters = []types.Datacenter{}
			}
			validated = append(validated, s)
		}

		var ts *float64
		var nextRefresh *float64
		var cacheAgeSecs *int
		if state.ServerCache.Timestamp != nil {
			tsFloat := float64(state.ServerCache.Timestamp.Unix())
			ts = &tsFloat
			next := tsFloat + state.ServerCache.TTL.Seconds()
			nextRefresh = &next
			age := int(time.Since(*state.ServerCache.Timestamp).Seconds())
			cacheAgeSecs = &age
		}

		resp := gin.H{
			"servers": validated,
			"cacheInfo": gin.H{
				"cached":             valid,
				"usingExpiredCache":  usingExpiredCache,
				"cacheAgeMinutes":    cacheAgeMinutes,
				"timestamp":          ts,
				"cacheAge":           cacheAgeSecs,
				"cacheDuration":      int(state.ServerCache.TTL.Seconds()),
				"nextAutoRefresh":    nextRefresh,
				"autoRefreshEnabled": true,
			},
		}

		if usingExpiredCache {
			c.Header("X-Cache-Warning", "Using expired cache ("+strconv.Itoa(cacheAgeMinutes)+" minutes old)")
		}
		c.JSON(http.StatusOK, resp)
	}
}

// GetAvailability GET/POST /api/availability/:plancode
func GetAvailability(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		planCode := c.Param("planCode")
		var options []string
		if c.Request.Method == http.MethodPost {
			var body struct {
				Options interface{} `json:"options"`
			}
			_ = c.ShouldBindJSON(&body)
			switch v := body.Options.(type) {
			case []interface{}:
				for _, o := range v {
					if s, ok := o.(string); ok && strings.TrimSpace(s) != "" {
						options = append(options, s)
					}
				}
			case string:
				for _, s := range strings.Split(v, ",") {
					s = strings.TrimSpace(s)
					if s != "" {
						options = append(options, s)
					}
				}
			}
		} else {
			optsStr := c.Query("options")
			if optsStr != "" {
				for _, s := range strings.Split(optsStr, ",") {
					s = strings.TrimSpace(s)
					if s != "" {
						options = append(options, s)
					}
				}
			}
		}

		state.Logger.Debug("查询可用性: plan_code="+planCode+", method="+c.Request.Method, "availability")

		availability, err := catalog.CheckServerAvailability(state, planCode, options)
		if err != nil || availability == nil {
			state.Logger.Warn("未找到 "+planCode+" 的可用性数据", "availability")
			c.JSON(http.StatusNotFound, gin.H{})
			return
		}
		c.JSON(http.StatusOK, availability)
	}
}

// MonitorPrice POST /api/internal/monitor/price (本地白名单)
func MonitorPrice(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		if clientIP != "127.0.0.1" && clientIP != "::1" && clientIP != "localhost" {
			state.Logger.Warn("[monitor price API] 拒绝非本地请求: "+clientIP, "price")
			c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "此API仅限本地访问"})
			return
		}
		var body struct {
			PlanCode   string   `json:"plan_code"`
			Datacenter string   `json:"datacenter"`
			Options    []string `json:"options"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.PlanCode == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 plan_code 参数"})
			return
		}
		if body.Datacenter == "" {
			body.Datacenter = "gra"
		}
		result := price.GetInternal(state, body.PlanCode, body.Datacenter, body.Options)
		c.JSON(http.StatusOK, result)
	}
}

// CacheInfo GET /api/cache/info
func CacheInfo(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		cached, valid := state.ServerCache.Get()
		var ts *float64
		var age *int
		if state.ServerCache.Timestamp != nil {
			t := float64(state.ServerCache.Timestamp.Unix())
			ts = &t
			a := int(time.Since(*state.ServerCache.Timestamp).Seconds())
			age = &a
		}
		sqliteCount, _ := state.DB.ServerCount()
		sqliteUpdatedMs, _ := state.DB.ServersUpdatedAt()
		c.JSON(http.StatusOK, gin.H{
			"backend": gin.H{
				"hasCachedData": len(cached) > 0,
				"timestamp":     ts,
				"cacheAge":      age,
				"cacheDuration": int(state.ServerCache.TTL.Seconds()),
				"serverCount":   len(cached),
				"cacheValid":    valid,
			},
			"sqlite": gin.H{
				"serverCount": sqliteCount,
				"updatedAtMs": sqliteUpdatedMs, // 0 表示从没刷新过
				"path":        state.DB.Path,
			},
			"storage": gin.H{
				"dataDir":  state.Paths.DataDir,
				"cacheDir": state.Paths.CacheDir,
				"logsDir":  state.Paths.LogsDir,
			},
		})
	}
}

// ClearCache POST /api/cache/clear
// type:
//   "memory" → 只清进程内存（ServerCache + ServerPlans），下次刷新若有 SQLite 缓存仍会用
//   "sqlite" → 只清 SQLite servers 表（重启后不会回灌旧目录），内存里如果还有照常用
//   "all"    → 内存 + SQLite 都清
// 注意：queue / history / monitor / vps / sniper 这些是业务数据，不算"缓存"，不在清理范围内。
func ClearCache(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Type string `json:"type"`
		}
		_ = c.ShouldBindJSON(&body)
		cacheType := body.Type
		if cacheType == "" {
			cacheType = "all"
		}
		cleared := []string{}

		if cacheType == "all" || cacheType == "memory" {
			state.ServerPlansMu.Lock()
			state.ServerPlans = []types.ServerPlan{}
			state.ServerPlansMu.Unlock()
			state.ServerCache.Set(nil)
			state.ServerCache.Timestamp = nil
			cleared = append(cleared, "memory")
			state.Logger.Info("已清除内存缓存", "")
		}

		if cacheType == "all" || cacheType == "sqlite" {
			if err := state.DB.ClearServers(); err != nil {
				state.Logger.Error("清除 SQLite 服务器缓存失败: "+err.Error(), "")
			} else {
				cleared = append(cleared, "sqlite_servers")
				state.Logger.Info("已清除 SQLite 服务器缓存", "")
			}
			if err := state.DB.ClearCatalogs(); err != nil {
				state.Logger.Error("清除 SQLite catalog 缓存失败: "+err.Error(), "")
			} else {
				cleared = append(cleared, "sqlite_catalogs")
				state.Logger.Info("已清除 SQLite catalog 缓存", "")
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"cleared": cleared,
			"message": "已清除缓存: " + strings.Join(cleared, ", "),
		})
	}
}

// JSONString 简便序列化
func JSONString(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
