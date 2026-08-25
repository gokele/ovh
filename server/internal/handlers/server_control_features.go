package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
)

// featureOVHErr 拆出 OVH 错误里的 HTTP 状态码和原始 message。
// go-ovh 把状态码放进 *ovh.APIError.Code（ovh.go UnmarshalResponse），err.Error() 只是把
// message 拼成一句人话；只靠子串匹配判业务语义会误伤（"服务不存在" 和 "功能未开通" 用同一句式），
// 所以这里统一取结构化字段。非 OVH 错误（网络/超时）返回 code=0。
func featureOVHErr(err error) (int, string) {
	var apiErr *ovhsdk.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code, apiErr.Message
	}
	return 0, err.Error()
}

// featureIsFeatureMissing 判断一个 404/"does not exist" 到底是"该功能未开通"还是"服务器压根不在这个账户下"。
// 为什么要为此多打一次 /dedicated/server/{svc}：OVH 对这两种情况返回的文案高度相似，schema 也没有规定
// 二者的差异（属于运行时行为），光看字符串会把"用户切错 ?account="误报成"备份FTP未激活"，
// 还会给出一个点了必然失败的"立即激活"按钮。宁可在错误路径上多一次请求换取判断的确定性。
// 校验本身失败（限流/网络）时返回 true，保持原有的降级行为，不把已经能用的功能改坏。
func featureIsFeatureMissing(client *ovhsdk.Client, svc string) bool {
	var d map[string]interface{}
	if err := client.Get("/dedicated/server/"+svc, &d); err != nil {
		code, _ := featureOVHErr(err)
		// 404=服务不存在，403=存在但当前凭据无权访问，两者都不该被说成"功能未开通"
		if code == http.StatusNotFound || code == http.StatusForbidden {
			return false
		}
		return true
	}
	return true
}

// featureBackupFTPUnavailable 在 US 区直接拦掉整套备份FTP 请求。
// 依据：官方 schema 里 US endpoint 的 /dedicated/server 只有 98 条路径，backupFTP 五条一条都没有
// （EU 与 CA 都是 107 条、五条齐全，已用 live api.us.ovhcloud.com/1.0/dedicated/server.json
// 与 eu./ca. 三站并排复核；US 侧同类能力只有 backupCloud，那三条端点三区都在，不需要门控）。
// 不拦的话请求会拿到一句 "Got an invalid (or empty) URL" 的 500，对用户毫无意义。
//
// 判大区走 srvRegionFor(见 server_control_basic.go)而不是比 acc.Endpoint 字符串：
// endpoint 还有 kimsufi-* / soyoustart-* 别名，散装比较漏一种写法就等于门控失效。
//
// 状态码按方法分开，是为了对上前端 use-server-control.ts 的两套读法：
// GET 走 useServerBackupFtp，它是"状态码优先"——任何 404 都会被当成"未激活"并丢掉整个 body，
// 于是 US 用户会看到一个点了必然失败的"激活 Backup FTP"按钮；而 200 + success:false 正好命中
// 该 hook 的 notAvailable 分支，页面显示"此服务器无 Backup FTP + 原因"，无需前端配合。
// 写操作（POST/DELETE）走 mutation，必须以非 2xx 结束才会走 catch 弹错误提示，
// 否则会假装"激活请求已发送"，所以这里返回 404，body 里的 error 前端会读。
func featureBackupFTPUnavailable(state *app.State, c *gin.Context) bool {
	region := srvRegionFor(state, c)
	if region != srvRegionUS {
		return false
	}
	state.Logger.Info("账户所在大区 "+region+" 不提供备份FTP功能，已拦截请求", "server_control")
	payload := gin.H{
		"success":      false,
		"error":        "当前账户所在区域（US）不提供备份FTP功能，请改用云备份（Backup Cloud）",
		"notAvailable": true,
		"unsupported":  true,
		"region":       region,
	}
	if c.Request.Method == http.MethodGet {
		c.JSON(http.StatusOK, payload)
	} else {
		c.JSON(http.StatusNotFound, payload)
	}
	return true
}

// featureBackupFTPACLPath 拼 ACL 详情路径。ipBlock 的 schema 类型就是 ipBlock（带掩码的 CIDR，如 37.59.1.0/28），
// 里面的 "/" 必须转义，否则 go-ovh 原样拼接会让 OVH 收到多一段的路径直接 404；
// go-ovh 的签名算的也是这个已转义的 target 字符串，所以编码后签名与实际 URL 一致。
func featureBackupFTPACLPath(svc, ipBlock string) string {
	return "/dedicated/server/" + svc + "/features/backupFTP/access/" + url.PathEscape(ipBlock)
}

// featureParallelACLDetails 并发拉每个 ipBlock 的 ACL 详情，失败位把真实错误留下来。
// 没有复用 util.go 的 parallelGetStringKeys：那个 helper 直接把 err 丢掉，
// 凭据失效(401)、限流(429)、OVH 5xx 和"这个 block 确实没了"会被压成同一个空值。
func featureParallelACLDetails(client *ovhsdk.Client, svc string, blocks []string, concurrency int) ([]map[string]interface{}, []error) {
	if concurrency <= 0 {
		concurrency = 10
	}
	details := make([]map[string]interface{}, len(blocks))
	errs := make([]error, len(blocks))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, b := range blocks {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, block string) {
			defer wg.Done()
			defer func() { <-sem }()
			var d map[string]interface{}
			if err := client.Get(featureBackupFTPACLPath(svc, block), &d); err != nil {
				errs[idx] = err
				return
			}
			details[idx] = d
		}(i, b)
	}
	wg.Wait()
	return details, errs
}

// Burst GET
func GetBurst(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var burst map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/burst", &burst); err != nil {
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "does not exist") || strings.Contains(lower, "not exist") {
				state.Logger.Info("服务器 "+svc+" 不支持突发带宽功能", "server_control")
				c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "该服务器不支持突发带宽功能", "notAvailable": true})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "burst": burst})
	}
}

// Burst PUT
func UpdateBurst(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Status string `json:"status"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Status == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少status参数"})
			return
		}
		// 合法取值由 schema 写死：body 模型 dedicated.server.ServerBurst 的 status 是
		// dedicated.server.BurstStatusEnum = [active, inactive, inactiveLocked]。
		// 不做白名单的话，"enabled"/"ACTIVE" 这类值会被 OVH 400 打回却包成 500，前端可能反复重试。
		switch body.Status {
		case "active", "inactive", "inactiveLocked":
		default:
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "status 参数无效，只能是 active / inactive / inactiveLocked"})
			return
		}
		var result map[string]interface{}
		if err := client.Put("/dedicated/server/"+svc+"/burst", map[string]interface{}{
			"status": body.Status,
		}, &result); err != nil {
			// OVH 判定的参数错误照样是参数错误，不该报成服务器内部错误让前端重试
			status := http.StatusInternalServerError
			if code, _ := featureOVHErr(err); code == http.StatusBadRequest {
				status = http.StatusBadRequest
			}
			state.Logger.Error("更新服务器 "+svc+" 突发带宽状态失败: "+err.Error(), "server_control")
			c.JSON(status, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("更新服务器 "+svc+" 突发带宽状态为: "+body.Status, "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "突发带宽状态已更新", "result": result})
	}
}

// Firewall GET
func GetFirewall(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var fw map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/features/firewall", &fw); err != nil {
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "does not exist") || strings.Contains(lower, "not exist") {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "该服务器不支持防火墙功能", "notAvailable": true})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "firewall": fw})
	}
}

// Firewall PUT
func UpdateFirewall(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.Enabled == nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少enabled参数"})
			return
		}
		var result map[string]interface{}
		if err := client.Put("/dedicated/server/"+svc+"/features/firewall", map[string]interface{}{
			"enabled": *body.Enabled,
		}, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		text := "启用"
		if !*body.Enabled {
			text = "禁用"
		}
		state.Logger.Info(text+"服务器 "+svc+" 防火墙", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "防火墙已" + text, "result": result})
	}
}

// Backup FTP GET
func GetBackupFTP(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		if featureBackupFTPUnavailable(state, c) {
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/features/backupFTP", &d); err != nil {
			code, msg := featureOVHErr(err)
			if code == http.StatusNotFound || strings.Contains(strings.ToLower(msg), "does not exist") {
				if featureIsFeatureMissing(client, svc) {
					c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "备份FTP未激活", "notActivated": true})
					return
				}
				state.Logger.Warn("查询服务器 "+svc+" 备份FTP：服务器不存在或不属于当前账户 - "+msg, "server_control")
				// 这里必须是 200 + success:false：前端 useServerBackupFtp 对 404 只看状态码、
				// 直接当成"未激活"并丢掉 body，unknownService/reason 根本到不了 UI，
				// 用户会看到一个点下去必然失败的"激活"按钮。200 才会走 notAvailable 分支把原因显示出来。
				c.JSON(http.StatusOK, gin.H{
					"success":        false,
					"error":          "服务器不存在或不属于当前账户",
					"unknownService": true,
					"reason":         msg,
				})
				return
			}
			state.Logger.Error("查询服务器 "+svc+" 备份FTP失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "backupFtp": d})
	}
}

// Backup FTP POST (activate)
func ActivateBackupFTP(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		if featureBackupFTPUnavailable(state, c) {
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var result map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/features/backupFTP", map[string]interface{}{}, &result); err != nil {
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "cannot benefit") || strings.Contains(lower, "not available") {
				c.JSON(http.StatusBadRequest, gin.H{
					"success":      false,
					"error":        "该服务器无法使用备份FTP服务",
					"notAvailable": true,
					"reason":       err.Error(),
				})
				return
			}
			state.Logger.Error("激活服务器 "+svc+" 备份FTP失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("激活服务器 "+svc+" 备份FTP成功", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "备份FTP已激活", "result": result})
	}
}

// Backup FTP DELETE
func DeleteBackupFTP(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		if featureBackupFTPUnavailable(state, c) {
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var result map[string]interface{}
		if err := client.Delete("/dedicated/server/"+svc+"/features/backupFTP", &result); err != nil {
			state.Logger.Error("删除服务器 "+svc+" 备份FTP失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("删除服务器 "+svc+" 备份FTP成功", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "备份FTP已删除", "result": result})
	}
}

// Backup FTP access list
func GetBackupFTPAccess(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		if featureBackupFTPUnavailable(state, c) {
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var blocks []string
		if err := client.Get("/dedicated/server/"+svc+"/features/backupFTP/access", &blocks); err != nil {
			state.Logger.Error("获取服务器 "+svc+" 备份FTP授权IP列表失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// 并发拉每个 IP block 的详情
		details, errs := featureParallelACLDetails(client, svc, blocks, 10)
		list := []interface{}{}
		failed := 0
		var firstErr error
		for i, b := range blocks {
			if errs[i] != nil {
				failed++
				if firstErr == nil {
					firstErr = errs[i]
				}
				_, msg := featureOVHErr(errs[i])
				state.Logger.Warn("获取备份FTP授权IP "+b+" 详情失败: "+errs[i].Error(), "server_control")
				list = append(list, map[string]interface{}{"ipBlock": b, "error": msg})
				continue
			}
			if details[i] == nil {
				list = append(list, map[string]interface{}{"ipBlock": b})
				continue
			}
			list = append(list, details[i])
		}
		// 全挂通常是凭据失效/限流/OVH 故障，这种时候不能伪装成 success:true 让前端把占位对象当成真实数据
		if failed > 0 && failed == len(blocks) {
			state.Logger.Error("服务器 "+svc+" 备份FTP授权IP详情全部获取失败: "+firstErr.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": firstErr.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "accessList": list, "failedCount": failed})
	}
}

// Backup FTP access add
func AddBackupFTPAccess(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		if featureBackupFTPUnavailable(state, c) {
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			IPBlock string `json:"ipBlock"`
			FTP     *bool  `json:"ftp"`
			NFS     bool   `json:"nfs"`
			CIFS    bool   `json:"cifs"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.IPBlock == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少ipBlock参数"})
			return
		}
		ftp := true
		if body.FTP != nil {
			ftp = *body.FTP
		}
		var result map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/features/backupFTP/access", map[string]interface{}{
			"cifs":    body.CIFS,
			"ftp":     ftp,
			"ipBlock": body.IPBlock,
			"nfs":     body.NFS,
		}, &result); err != nil {
			state.Logger.Error("添加备份FTP访问IP "+body.IPBlock+" 失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("添加备份FTP访问IP "+body.IPBlock+" 成功", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "访问IP已添加", "result": result})
	}
}

// Backup FTP access delete
func DeleteBackupFTPAccess(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		if featureBackupFTPUnavailable(state, c) {
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		// ipBlock 是带掩码的 CIDR，而 gin 默认 UseRawPath=false：路径参数里的 %2F 会先被还原成 "/"，
		// 于是 URL 多出一段，路由根本匹配不上（实测编码与否都是 404）。所以优先从 query / body 取完整值，
		// 路径参数只作兜底，并兼容主控把路由改成通配段 *ip_block（那样值会带前导 "/"）。
		ipBlock := strings.TrimSpace(c.Query("ipBlock"))
		if ipBlock == "" {
			var body struct {
				IPBlock string `json:"ipBlock"`
			}
			_ = c.ShouldBindJSON(&body)
			ipBlock = strings.TrimSpace(body.IPBlock)
		}
		if ipBlock == "" {
			ipBlock = strings.TrimPrefix(strings.TrimSpace(c.Param("ip_block")), "/")
		}
		if ipBlock == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少ipBlock参数"})
			return
		}
		if err := client.Delete(featureBackupFTPACLPath(svc, ipBlock), nil); err != nil {
			state.Logger.Error("删除备份FTP访问IP "+ipBlock+" 失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("删除备份FTP访问IP "+ipBlock+" 成功", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "访问IP已删除"})
	}
}

// Backup FTP password
func ChangeBackupFTPPassword(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		if featureBackupFTPUnavailable(state, c) {
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var result map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/features/backupFTP/password", map[string]interface{}{}, &result); err != nil {
			state.Logger.Error("修改服务器 "+svc+" 备份FTP密码失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("修改服务器 "+svc+" 备份FTP密码成功", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "密码已重置，新密码已发送至邮箱", "result": result})
	}
}

// Backup FTP authorizable blocks
func GetBackupFTPAuthorizableBlocks(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		if featureBackupFTPUnavailable(state, c) {
			return
		}
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var blocks []string
		if err := client.Get("/dedicated/server/"+svc+"/features/backupFTP/authorizableBlocks", &blocks); err != nil {
			state.Logger.Error("获取服务器 "+svc+" 备份FTP可授权IP段失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "blocks": blocks})
	}
}

// Backup Cloud GET
func GetBackupCloud(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/features/backupCloud", &d); err != nil {
			code, msg := featureOVHErr(err)
			if code == http.StatusNotFound || strings.Contains(strings.ToLower(msg), "does not exist") {
				if featureIsFeatureMissing(client, svc) {
					c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "云备份未激活", "notActivated": true})
					return
				}
				state.Logger.Warn("查询服务器 "+svc+" 云备份：服务器不存在或不属于当前账户 - "+msg, "server_control")
				c.JSON(http.StatusNotFound, gin.H{
					"success":        false,
					"error":          "服务器不存在或不属于当前账户",
					"unknownService": true,
					"reason":         msg,
				})
				return
			}
			state.Logger.Error("查询服务器 "+svc+" 云备份失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "backupCloud": d})
	}
}

// Backup Cloud POST (activate)
// schema: POST /dedicated/server/{serviceName}/features/backupCloud ⚠️ BETA
// cloudProjectId / projectDescription 两个 body 字段都是可选的：前者复用已有 public cloud 项目，
// 后者只在新建项目时生效。空值不发送——schema 没有写明空串合法，BETA 端点对空/未知字段的宽容度也无从确认，
// 少发一个字段比赌 OVH 的宽容度安全。
func ActivateBackupCloud(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			CloudProjectID     string `json:"cloudProjectId"`
			ProjectDescription string `json:"projectDescription"`
		}
		_ = c.ShouldBindJSON(&body)
		payload := map[string]interface{}{}
		if v := strings.TrimSpace(body.CloudProjectID); v != "" {
			payload["cloudProjectId"] = v
		}
		if v := strings.TrimSpace(body.ProjectDescription); v != "" {
			payload["projectDescription"] = v
		}
		var result map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/features/backupCloud", payload, &result); err != nil {
			status := http.StatusInternalServerError
			if code, _ := featureOVHErr(err); code == http.StatusBadRequest || code == http.StatusConflict {
				status = code
			}
			state.Logger.Error("激活服务器 "+svc+" 云备份失败: "+err.Error(), "server_control")
			c.JSON(status, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("激活服务器 "+svc+" 云备份成功", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "云备份已激活", "result": result})
	}
}

// Backup Cloud DELETE
// schema: DELETE /dedicated/server/{serviceName}/features/backupCloud ⚠️ BETA → void
// OVH 原文注明只是停用云备份，不会删除容器里的数据，所以提示语按"停用"写。
func DeleteBackupCloud(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		if err := client.Delete("/dedicated/server/"+svc+"/features/backupCloud", nil); err != nil {
			code, msg := featureOVHErr(err)
			if code == http.StatusNotFound || strings.Contains(strings.ToLower(msg), "does not exist") {
				if featureIsFeatureMissing(client, svc) {
					c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "云备份未激活", "notActivated": true})
					return
				}
				state.Logger.Warn("停用服务器 "+svc+" 云备份：服务器不存在或不属于当前账户 - "+msg, "server_control")
				c.JSON(http.StatusNotFound, gin.H{
					"success":        false,
					"error":          "服务器不存在或不属于当前账户",
					"unknownService": true,
					"reason":         msg,
				})
				return
			}
			state.Logger.Error("停用服务器 "+svc+" 云备份失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("停用服务器 "+svc+" 云备份成功", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "云备份已停用（容器内数据不会被删除）"})
	}
}

// Backup Cloud password
// schema: POST /dedicated/server/{serviceName}/features/backupCloud/password ⚠️ BETA
// → dedicated.server.backup.BackupPassword，新密码在响应里，原样透传给前端。
func ChangeBackupCloudPassword(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var result map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/features/backupCloud/password", map[string]interface{}{}, &result); err != nil {
			code, msg := featureOVHErr(err)
			if code == http.StatusNotFound || strings.Contains(strings.ToLower(msg), "does not exist") {
				if featureIsFeatureMissing(client, svc) {
					c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "云备份未激活", "notActivated": true})
					return
				}
				state.Logger.Warn("重置服务器 "+svc+" 云备份密码：服务器不存在或不属于当前账户 - "+msg, "server_control")
				c.JSON(http.StatusNotFound, gin.H{
					"success":        false,
					"error":          "服务器不存在或不属于当前账户",
					"unknownService": true,
					"reason":         msg,
				})
				return
			}
			state.Logger.Error("重置服务器 "+svc+" 云备份密码失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("重置服务器 "+svc+" 云备份密码成功", "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "云备份密码已重置", "result": result})
	}
}

// Backup Cloud offer details
func GetBackupCloudOfferDetails(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var d map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/backupCloudOfferDetails", &d); err != nil {
			// schema 的 desc 写的是 "if available for the current server"，即没有报价时本来就会报错，
			// 这属于正常业务状态而不是故障，降级成 notAvailable，别让前端弹"服务器内部错误"。
			code, msg := featureOVHErr(err)
			if code == http.StatusNotFound || strings.Contains(strings.ToLower(msg), "does not exist") {
				if featureIsFeatureMissing(client, svc) {
					state.Logger.Info("服务器 "+svc+" 没有可用的云备份报价", "server_control")
					c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "该服务器不支持云备份", "notAvailable": true})
					return
				}
				state.Logger.Warn("查询服务器 "+svc+" 云备份报价：服务器不存在或不属于当前账户 - "+msg, "server_control")
				c.JSON(http.StatusNotFound, gin.H{
					"success":        false,
					"error":          "服务器不存在或不属于当前账户",
					"unknownService": true,
					"reason":         msg,
				})
				return
			}
			state.Logger.Error("查询服务器 "+svc+" 云备份报价失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "offerDetails": d})
	}
}
