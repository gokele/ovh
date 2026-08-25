package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/price"
	"github.com/ovh-buy/server/internal/types"
)

// defaultDatacenterFor 调用方没给机房时的缺省机房。
//
// 先问目录:这个 plan 到底在哪些机房卖(catalog.PreferredDatacenterForPlan 会在
// 目录列出的机房里优先挑与账户同大区的那个)。按大区写死一个主力机房是不够的 ——
// 2026-08 实测三份公开 eco 目录:US 143 个 plan 里含 vin 的只有 33 个(42 个只卖欧洲机房、
// 36 个只卖 bhs、32 个只卖 sgp/syd/ynm),EU/CA 99 个 plan 里含 gra 的 47 个、含 bhs 的 42 个,
// 另有 51 个只卖 sgp/syd/ynm。写死等于让一大半机型拿一个自己不卖的机房去询价,cart 直接报错。
//
// 目录拿不到、或该 plan 不在 eco 目录里(Scale/HCI/SDS 这些机型有库存但不在 eco 目录),
// 才退回各区主力机房(EU=gra / CA=bhs / US=vin)—— 这三个都在对应站点的 availabilities 里
// 实际出现过,至少不会跨站点。
func defaultDatacenterFor(state *app.State, accountID, planCode string) string {
	acc, ok := state.FindAccount(accountID)
	if !ok {
		return "gra"
	}
	if dc := catalog.PreferredDatacenterForPlan(state, accountID, planCode); dc != "" {
		return dc
	}
	region := ovh.EndpointRegion(acc.Endpoint)
	if sub := strings.ToUpper(strings.TrimSpace(acc.Zone)); sub != "" && ovh.KnownSubsidiary(sub) {
		region = ovh.SubsidiaryRegion(sub)
	}
	switch region {
	case "US":
		return "vin"
	case "CA":
		return "bhs"
	default:
		return "gra"
	}
}

// GetServers GET /api/servers
func GetServers(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		showAPI := strings.EqualFold(c.Query("showApiServers"), "true")
		forceRefresh := strings.EqualFold(c.Query("forceRefresh"), "true")

		// 账户来源与其它 handler 一致(?account=)。拼错的 account 必须当场 400,
		// 否则会静默落回默认账户视角,用户以为在看 US 目录、其实是 EU 的
		reqAccount := c.Query("account")
		if reqAccount != "" {
			if _, ok := state.FindAccount(reqAccount); !ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": "account 不存在"})
				return
			}
		}
		// 目录内容随账户的 endpoint + ovhSubsidiary 变,缓存按同一维度分桶:
		// 带 ?account= 的请求命中自己那桶,既不会读到别人的目录,也不用每次重拉 96 个 plan
		cacheKey := state.ServerCacheKey(reqAccount)
		isDefaultBucket := cacheKey == app.DefaultServerBucket

		usingExpiredCache := false
		cacheAgeMinutes := 0

		cached, valid := state.ServerCache.GetBucket(cacheKey)
		if ts := state.ServerCache.TimestampOf(cacheKey); ts != nil {
			cacheAgeMinutes = int(time.Since(*ts).Minutes())
		}

		// 多账户:凭据来源是 ovh_accounts 表,不再是旧的 state.Config
		hasOVH := state.HasAnyAccount()
		var serverPlans []types.ServerPlan

		if valid && !forceRefresh {
			state.Logger.Info("使用缓存的服务器列表 (缓存时间: "+strconv.Itoa(cacheAgeMinutes)+" 分钟前)", "")
			serverPlans = cached
		} else if showAPI && hasOVH {
			state.Logger.Info("正在从OVH API重新加载服务器列表...", "")
			// 目录随账户走:ovhSubsidiary 取该账户的 zone,endpoint 也跟着账户,
			// 否则切到 US 账户仍然看到默认(EU)账户子公司的机型集合
			apiServers, availFailed := catalog.LoadServerList(state, reqAccount)
			if len(apiServers) > 0 && availFailed > 0 && len(cached) > 0 {
				// 部分 plan 的可用性没拉到(多半是 OVH 限流),这份结果的机房列是残缺的。
				// 用它覆盖缓存 + SQLite 会让用户在整个 TTL 内看到"全线无货",宁可继续用旧缓存。
				serverPlans = cached
				// 只有缓存真的过期了才算"用过期数据":forceRefresh 时缓存往往还在 TTL 内,
				// 这时回一条"Using expired cache"告警是假警报
				usingExpiredCache = !valid
				state.Logger.Warn("⚠️ 本次刷新有 "+strconv.Itoa(availFailed)+" 个机型的可用性拉取失败，结果不完整，保留旧缓存", "")
			} else if len(apiServers) > 0 {
				if availFailed > 0 {
					state.Logger.Warn("⚠️ 有 "+strconv.Itoa(availFailed)+" 个机型的可用性拉取失败，但本地无缓存可退，先用这份不完整数据", "")
				}
				// 内存缓存分桶后每个账户视角都写自己那桶;
				// SQLite 的 servers 是唯一一张全局表(没有 account 列),只有默认视角能落盘,
				// 否则下次启动会把 US 的机型集合全局回灌给所有账户
				state.ServerCache.SetBucket(cacheKey, apiServers)
				if isDefaultBucket {
					state.ServerPlansMu.Lock()
					state.ServerPlans = apiServers
					state.ServerPlansMu.Unlock()
					_ = state.SaveServers()
					state.Logger.Info("从OVH API加载了 "+strconv.Itoa(len(apiServers))+" 台服务器，已更新缓存", "")
				} else {
					state.Logger.Info("从OVH API加载了 "+strconv.Itoa(len(apiServers))+" 台服务器，已更新该账户视角的缓存（不落 SQLite）", "")
				}
				serverPlans = apiServers
			} else {
				state.Logger.Warn("从OVH API加载服务器列表失败或返回空数据", "")
				if len(cached) > 0 {
					serverPlans = cached
					usingExpiredCache = true
					state.Logger.Warn("⚠️ OVH API 调用失败，使用过期缓存数据", "")
				} else if isDefaultBucket {
					// state.ServerPlans 是默认账户视角的全局副本,只有默认桶能拿它兜底;
					// 指定账户时宁可 503,也不能把别的子公司的机型集合当成它的目录返回
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
				} else {
					state.Logger.Error("❌ OVH API 调用失败且该账户视角没有缓存数据可用！", "")
					c.JSON(http.StatusServiceUnavailable, gin.H{
						"error":   "No data available",
						"message": "无法获取该账户的服务器列表：OVH API 调用失败且该账户暂无缓存",
					})
					return
				}
			}
		} else if !valid && len(cached) > 0 {
			usingExpiredCache = true
			state.Logger.Warn("⚠️ 缓存已过期但未配置 OVH API，使用过期缓存数据", "")
			serverPlans = cached
		}

		// 上面每条分支都没命中(典型:带 ?account= 的非默认桶第一次访问、又没带 showApiServers=true,
		// 而 SQLite 只回灌默认桶)。这时 serverPlans 是 nil,直接返回会变成 200 + 空数组,
		// 和"该账户目录确实为空"完全无法区分。宁可现拉一次,拉不到就 503。
		if serverPlans == nil {
			if !hasOVH {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error":   "No data available",
					"message": "尚未配置任何 OVH 账户，无法获取服务器列表",
				})
				return
			}
			state.Logger.Info("该账户视角无缓存且未要求刷新，改为现拉一次目录", "")
			apiServers, availFailed := catalog.LoadServerList(state, reqAccount)
			if len(apiServers) == 0 {
				state.Logger.Error("❌ 现拉目录失败，且该账户视角无任何缓存可用", "")
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error":   "No data available",
					"message": "无法获取该账户的服务器列表：OVH API 调用失败且暂无缓存",
				})
				return
			}
			if availFailed > 0 {
				state.Logger.Warn("⚠️ 现拉目录时有 "+strconv.Itoa(availFailed)+" 个机型的可用性拉取失败", "")
			}
			state.ServerCache.SetBucket(cacheKey, apiServers)
			if isDefaultBucket {
				state.ServerPlansMu.Lock()
				state.ServerPlans = apiServers
				state.ServerPlansMu.Unlock()
				_ = state.SaveServers()
			}
			serverPlans = apiServers
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

		// 时间戳重新取一遍:上面若刚写过桶,这里要反映新的写入时间
		var ts *float64
		var nextRefresh *float64
		var cacheAgeSecs *int
		if bucketTS := state.ServerCache.TimestampOf(cacheKey); bucketTS != nil {
			tsFloat := float64(bucketTS.Unix())
			ts = &tsFloat
			next := tsFloat + state.ServerCache.TTL.Seconds()
			nextRefresh = &next
			age := int(time.Since(*bucketTS).Seconds())
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

		// 账户来源与其它 handler 一致(?account=):EU/US/CA 三个 endpoint 的库存视图各自独立。
		// 先做存在性校验,跟 ServerPrice 一个口径 —— 否则 account 拼错时
		// ClientFor 报错会被下面折成 502「查询可用性失败」,看着像 OVH 挂了
		reqAccount := c.Query("account")
		if reqAccount != "" {
			if _, ok := state.FindAccount(reqAccount); !ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": "account 不存在"})
				return
			}
		}
		availability, err := catalog.CheckServerAvailability(state, planCode, options, reqAccount)
		if err != nil {
			switch {
			case errors.Is(err, catalog.ErrPlanNotInRegion):
				// OVH 对"不属于本站点的 planCode"回的是 200 + 空数组,以前被折成 200 {}，
				// 和"所有机房都没货"长得一模一样。这里明确告诉调用方是拿错区了。
				state.Logger.Warn("查询 "+planCode+" 可用性: "+err.Error(), "availability")
				c.JSON(http.StatusNotFound, gin.H{
					"error":   "planCode 不属于该账户所在站点",
					"message": err.Error(),
				})
			case errors.Is(err, catalog.ErrConfigNotMatched):
				state.Logger.Warn("查询 "+planCode+" 可用性: "+err.Error(), "availability")
				c.JSON(http.StatusNotFound, gin.H{
					"error":   "该 plan 没有这套配置组合",
					"message": err.Error(),
				})
			default:
				// OVH 的 429 限流 / 403 凭据失效 / 404 不存在,以前全被折叠成 404 空对象,
				// 用户只会以为"全线下架"。原样把 OVH 错误交给前端,才能看出该去查密钥还是等限流。
				state.Logger.Error("查询 "+planCode+" 可用性失败: "+err.Error(), "availability")
				c.JSON(http.StatusBadGateway, gin.H{"error": "查询可用性失败", "message": err.Error()})
			}
			return
		}
		if availability == nil {
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
			AccountID  string   `json:"account_id"` // 哪个账户询价(空 = 默认)
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
			body.Datacenter = defaultDatacenterFor(state, body.AccountID, body.PlanCode)
		}
		result := price.GetInternal(state, body.AccountID, body.PlanCode, body.Datacenter, body.Options)
		c.JSON(http.StatusOK, result)
	}
}

// ServerPrice POST /api/servers/:planCode/price
// 后端兜底询价：走 OVH cart 真实询价（创建购物车 → 加商品 → 加 addon → summary → 删车），
// 拿到的是含税/不含税/币种的权威价格。
//
// 前端浏览页的价格是用本地 catalog 自己算的（plan 月费 + 各 addon 月费累加），
// catalog 缺项、OVH 改结构、或者某个 addon 在目标机房不可订购时，本地算不出来 ——
// 这个端点就是那时候的兜底，也给外部脚本一个不必自己复刻算价公式的入口。
//
// 账户来源：?account=<id>（缺省走默认账户），与其它 handler 一致；
// 也兼容 body 里的 account_id。购物车的 subsidiary 取该账户的 zone。
func ServerPrice(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		planCode := strings.TrimPrefix(c.Param("planCode"), "/")
		if planCode == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 planCode"})
			return
		}
		var body struct {
			AccountID  string   `json:"account_id"`
			Datacenter string   `json:"datacenter"`
			Options    []string `json:"options"`
		}
		_ = c.ShouldBindJSON(&body)

		accountID := c.Query("account")
		if accountID == "" {
			accountID = body.AccountID
		}
		if accountID != "" {
			if _, ok := state.FindAccount(accountID); !ok {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "account 不存在"})
				return
			}
		}
		if !state.HasAnyAccount() {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "未配置任何 OVH 账户"})
			return
		}
		datacenter := body.Datacenter
		if datacenter == "" {
			datacenter = c.Query("datacenter")
		}
		if datacenter == "" {
			// 与 MonitorPrice 保持一致:按账户所属站点选缺省机房,而不是写死欧洲的 gra
			datacenter = defaultDatacenterFor(state, accountID, planCode)
		}

		result := price.GetInternal(state, accountID, planCode, datacenter, body.Options)
		if !result.Success {
			// 询价失败是业务结果（配置在该机房不可订购等），不是服务端故障，
			// 保持 200 + success:false，前端按 success 字段判断即可。
			state.Logger.Warn("询价失败: "+planCode+"@"+datacenter+" - "+result.Error, "price")
		}
		c.JSON(http.StatusOK, result)
	}
}

// CacheInfo GET /api/cache/info
func CacheInfo(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 缓存按账户视角分桶,不带 ?account= 时看的就是默认视角那桶(行为与以前一致)
		cacheKey := state.ServerCacheKey(c.Query("account"))
		cached, valid := state.ServerCache.GetBucket(cacheKey)
		var ts *float64
		var age *int
		if bucketTS := state.ServerCache.TimestampOf(cacheKey); bucketTS != nil {
			t := float64(bucketTS.Unix())
			ts = &t
			a := int(time.Since(*bucketTS).Seconds())
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
//
//	"memory" → 只清进程内存（ServerCache + ServerPlans），下次刷新若有 SQLite 缓存仍会用
//	"sqlite" → 只清 SQLite servers 表（重启后不会回灌旧目录），内存里如果还有照常用
//	"all"    → 内存 + SQLite 都清
//
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
			// 分桶后"清内存缓存"= 清掉所有账户视角的桶
			state.ServerCache.Clear()
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
