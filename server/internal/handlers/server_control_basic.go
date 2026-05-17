package handlers

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/numconv"
)

// noOVHResp 401 帮助
func noOVHResp(c *gin.Context) {
	c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "未配置OVH API密钥"})
}

// ListMyServers GET /api/server-control/list
func ListMyServers(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := state.OVH.Client()
		if err != nil {
			noOVHResp(c)
			return
		}
		var names []string
		if err := client.Get("/dedicated/server", &names); err != nil {
			state.Logger.Error("获取服务器列表失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("获取服务器列表成功，共 %d 台", len(names)), "server_control")

		// 并发拉 detail + serviceInfos：N 台服务器 × 2 GET × 串行 ~200ms => 改 10 并发 ≈ N/5 * 200ms
		type srvResult struct {
			info        map[string]interface{}
			svcInfo     map[string]interface{}
			detailError error
		}
		results := make([]srvResult, len(names))
		sem := make(chan struct{}, 10)
		var wg sync.WaitGroup
		for i, name := range names {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, nm string) {
				defer wg.Done()
				defer func() { <-sem }()
				var info map[string]interface{}
				if err := client.Get("/dedicated/server/"+nm, &info); err != nil {
					results[idx].detailError = err
					return
				}
				results[idx].info = info
				var svc map[string]interface{}
				_ = client.Get("/dedicated/server/"+nm+"/serviceInfos", &svc)
				results[idx].svcInfo = svc
			}(i, name)
		}
		wg.Wait()

		servers := []gin.H{}
		for i, name := range names {
			r := results[i]
			if r.detailError != nil {
				state.Logger.Error("获取服务器 "+name+" 详情失败: "+r.detailError.Error(), "server_control")
				servers = append(servers, gin.H{"serviceName": name, "name": name, "error": r.detailError.Error()})
				continue
			}
			info, svcInfo := r.info, r.svcInfo
			renewalType := false
			if svcInfo != nil {
				if rn, ok := svcInfo["renew"].(map[string]interface{}); ok {
					if a, ok := rn["automatic"].(bool); ok {
						renewalType = a
					}
				}
			}
			// 1:1 对应 Python app.py:5083-5096：缺失字段补默认值
			monitoring := info["monitoring"]
			if monitoring == nil {
				monitoring = false
			}
			professionalUse := info["professionalUse"]
			if professionalUse == nil {
				professionalUse = false
			}
			servers = append(servers, gin.H{
				"serviceName":     name,
				"name":            valueOr(info, "name", name),
				"commercialRange": valueOr(info, "commercialRange", "N/A"),
				"datacenter":      valueOr(info, "datacenter", "N/A"),
				"state":           valueOr(info, "state", "unknown"),
				"monitoring":      monitoring,
				"reverse":         valueOr(info, "reverse", ""),
				"ip":              valueOr(info, "ip", "N/A"),
				"os":              valueOr(info, "os", "N/A"),
				"bootId":          info["bootId"],
				"professionalUse": professionalUse,
				"status":          valueOr(svcInfo, "status", "unknown"),
				"renewalType":     renewalType,
			})
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "servers": servers, "total": len(servers)})
	}
}

func valueOr(m map[string]interface{}, key, fallback string) interface{} {
	if v, ok := m[key]; ok && v != nil {
		return v
	}
	return fallback
}

// Reboot POST /api/server-control/:service_name/reboot
func Reboot(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := state.OVH.Client()
		if err != nil {
			noOVHResp(c)
			return
		}
		var result map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/reboot", map[string]interface{}{}, &result); err != nil {
			state.Logger.Error("重启服务器 "+svc+" 失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info("服务器 "+svc+" 重启请求已发送", "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "服务器 " + svc + " 重启请求已发送",
			"taskId":  result["taskId"],
		})
	}
}

// GetOSTemplates GET /api/server-control/:service_name/templates
func GetOSTemplates(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := state.OVH.Client()
		if err != nil {
			noOVHResp(c)
			return
		}
		var templates map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/install/compatibleTemplates", &templates); err != nil {
			state.Logger.Error("获取服务器 "+svc+" 系统模板失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		var allNames []string
		if ovhArr, ok := templates["ovh"].([]interface{}); ok {
			for _, t := range ovhArr {
				if s, ok := t.(string); ok {
					allNames = append(allNames, s)
				}
			}
		}
		state.Logger.Info(fmt.Sprintf("获取服务器 %s 可用系统模板成功，共 %d 个", svc, len(allNames)), "server_control")

		// Python app.py:5432 是串行逐个 GET 模板详情，50-100 模板要 10-50 秒。
		// 这里改成 10 路并发（OVH 通常允许 10-20 RPS），返回结构完全一致，仅顺序后排
		details := make([]gin.H, len(allNames))
		sem := make(chan struct{}, 10)
		var wg sync.WaitGroup
		for i, tn := range allNames {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, name string) {
				defer wg.Done()
				defer func() { <-sem }()
				var detail map[string]interface{}
				if err := client.Get("/dedicated/installationTemplate/"+name, &detail); err != nil {
					details[idx] = gin.H{
						"templateName": name,
						"distribution": name,
						"family":       "unknown",
						"bitFormat":    64,
					}
					return
				}
				bf := 64
				if v, ok := numconv.ToInt64(detail["bitFormat"]); ok {
					bf = int(v)
				}
				details[idx] = gin.H{
					"templateName": name,
					"distribution": valueOr(detail, "distribution", "N/A"),
					"family":       valueOr(detail, "family", "N/A"),
					"description":  valueOr(detail, "description", ""),
					"bitFormat":    bf,
				}
			}(i, tn)
		}
		wg.Wait()

		// 排序：常用优先
		priority := []string{"debian", "ubuntu", "centos", "rocky", "almalinux", "windows"}
		getPriority := func(t gin.H) int {
			d := strings.ToLower(fmt.Sprintf("%v", t["distribution"]))
			for i, p := range priority {
				if strings.Contains(d, p) {
					return i
				}
			}
			return len(priority)
		}
		for i := 1; i < len(details); i++ {
			for j := i; j > 0 && (getPriority(details[j-1]) > getPriority(details[j]) ||
				(getPriority(details[j-1]) == getPriority(details[j]) &&
					fmt.Sprintf("%v", details[j-1]["templateName"]) > fmt.Sprintf("%v", details[j]["templateName"]))); j-- {
				details[j-1], details[j] = details[j], details[j-1]
			}
		}
		ubuntuCount := 0
		for _, d := range details {
			if strings.Contains(strings.ToLower(fmt.Sprintf("%v", d["distribution"])), "ubuntu") {
				ubuntuCount++
			}
		}
		state.Logger.Info(fmt.Sprintf("返回 %d 个模板 (包括 %d 个Ubuntu)", len(details), ubuntuCount), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "templates": details, "total": len(details)})
	}
}

// installOSLocks 按 service_name 分别加锁，防同一台机器并发重装。
// 不同机器互不阻塞。TryLock 失败立即返回 409，不让前端干等。
var installOSLocks sync.Map // service_name → *sync.Mutex

func acquireInstallLock(svc string) (*sync.Mutex, bool) {
	v, _ := installOSLocks.LoadOrStore(svc, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	return mu, mu.TryLock()
}

// InstallOS POST /api/server-control/:service_name/install
func InstallOS(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")

		mu, ok := acquireInstallLock(svc)
		if !ok {
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"error":   "该服务器已有重装任务正在执行，请等待完成后再试",
			})
			return
		}
		defer mu.Unlock()

		client, err := state.OVH.Client()
		if err != nil {
			noOVHResp(c)
			return
		}
		var body map[string]interface{}
		_ = c.ShouldBindJSON(&body)
		templateName, _ := body["templateName"].(string)
		if templateName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "未指定系统模板"})
			return
		}

		installParams := map[string]interface{}{
			"operatingSystem": templateName,
		}
		if v, ok := body["customHostname"].(string); ok && v != "" {
			installParams["customHostname"] = v
			state.Logger.Info("设置自定义主机名: "+v, "server_control")
		}

		// Proxmox 9 + ZFS 预设
		if useZFS, _ := body["useProxmox9Zfs"].(bool); useZFS {
			raidLevel := 1
			if v, ok := numconv.ToInt64(body["zfsRaidLevel"]); ok {
				raidLevel = int(v)
			}
			vzSizeMB := 102400
			if v, ok := numconv.ToInt64(body["zfsVzSize"]); ok {
				vzSizeMB = int(v)
			}
			state.Logger.Info(fmt.Sprintf("🎯 使用 Proxmox 9 + ZFS 根文件系统预设 (RAID%d)", raidLevel), "server_control")

			totalCapacityGB := 480
			if raidLevel == 0 {
				totalCapacityGB = 960
			}
			var hardware map[string]interface{}
			if err := client.Get("/dedicated/server/"+svc+"/specifications/hardware", &hardware); err == nil {
				if dgs, ok := hardware["diskGroups"].([]interface{}); ok && len(dgs) > 0 {
					if dg, ok := dgs[0].(map[string]interface{}); ok {
						diskCount := 2
						if v, ok := numconv.ToInt64(dg["numberOfDisks"]); ok {
							diskCount = int(v)
						}
						singleDiskGB := 480
						if ds, ok := dg["diskSize"].(map[string]interface{}); ok {
							if v, ok := numconv.ToInt64(ds["value"]); ok {
								singleDiskGB = int(v)
							}
						}
						if raidLevel == 0 {
							totalCapacityGB = singleDiskGB * diskCount
						} else {
							totalCapacityGB = singleDiskGB
						}
						state.Logger.Info(fmt.Sprintf("📊 检测到磁盘: %dx%dGB, RAID%d 总容量: %dGB",
							diskCount, singleDiskGB, raidLevel, totalCapacityGB), "server_control")
					}
				}
			} else {
				state.Logger.Warn(fmt.Sprintf("获取硬件信息失败，使用默认容量: %dGB - %s", totalCapacityGB, err.Error()), "server_control")
			}

			usableCapacityMB := int(float64(totalCapacityGB) * 1024 * 0.92)
			bootSwapMB := 1024 + 8192
			rootSizeMB := usableCapacityMB - bootSwapMB - vzSizeMB
			state.Logger.Info(fmt.Sprintf("💾 容量计算: 理论%dGB, 实际可用~%dGB, 根目录%dGB",
				totalCapacityGB, usableCapacityMB/1024, rootSizeMB/1024), "server_control")

			installParams["storage"] = []map[string]interface{}{
				{
					"diskGroupId": 0,
					"partitioning": map[string]interface{}{
						"layout": []map[string]interface{}{
							{
								"fileSystem": "ext4",
								"mountPoint": "/boot",
								"raidLevel":  raidLevel,
								"size":       1024,
							},
							{
								"fileSystem": "swap",
								"mountPoint": "swap",
								"raidLevel":  1,
								"size":       8192,
							},
							{
								"fileSystem": "zfs",
								"mountPoint": "/",
								"raidLevel":  raidLevel,
								"size":       rootSizeMB,
								"extras": map[string]interface{}{
									"zp": map[string]interface{}{"name": "rpool"},
								},
							},
							{
								"fileSystem": "zfs",
								"mountPoint": "/var/lib/vz",
								"raidLevel":  raidLevel,
								"size":       0,
								"extras": map[string]interface{}{
									"zp": map[string]interface{}{"name": "rpool"},
								},
							},
						},
					},
				},
			}
			state.Logger.Info(fmt.Sprintf("✅ ZFS 配置: /boot (1GB) + swap (8GB) + / (%dGB) + /var/lib/vz", rootSizeMB/1024), "server_control")
		} else if sc := body["storageConfig"]; isNonEmptyStorage(sc) {
			// 1:1 对应 Python app.py:5603 `elif data.get('storageConfig')`：
			// 空数组/空字典/nil/false 都走 else 默认分区；之前 Go 把 `[]` 当真实 storage 传给 OVH 会 400
			state.Logger.Info("使用自定义存储配置", "server_control")
			installParams["storage"] = sc
		} else {
			state.Logger.Info("使用默认分区配置", "server_control")
		}

		state.Logger.Info("准备发送安装请求到OVH API", "server_control")
		state.Logger.Info("  - 服务器: "+svc, "server_control")
		state.Logger.Info("  - 模板: "+templateName, "server_control")

		// 用 requests 直接调用（与 Python 一致）
		baseURL := state.Config.APIBaseURL()
		apiURL := baseURL + "/1.0/dedicated/server/" + svc + "/reinstall"
		cfg := state.Config.Get()
		bodyBytes, _ := json.Marshal(installParams)
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		preHash := cfg.AppSecret + "+" + cfg.ConsumerKey + "+POST+" + apiURL + "+" + string(bodyBytes) + "+" + ts
		hash := sha1.Sum([]byte(preHash))
		signature := "$1$" + hex.EncodeToString(hash[:])

		req, _ := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
		req.Header.Set("X-Ovh-Application", cfg.AppKey)
		req.Header.Set("X-Ovh-Consumer", cfg.ConsumerKey)
		req.Header.Set("X-Ovh-Timestamp", ts)
		req.Header.Set("X-Ovh-Signature", signature)
		req.Header.Set("Content-Type", "application/json")
		state.Logger.Info("POST "+apiURL, "server_control")

		httpClient := &http.Client{Timeout: 30 * time.Second}
		resp, err := httpClient.Do(req)
		if err != nil {
			state.Logger.Error("重装服务器 "+svc+" 系统失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			state.Logger.Error(fmt.Sprintf("API返回错误: %d - %s", resp.StatusCode, string(respBody)), "server_control")
			c.JSON(resp.StatusCode, gin.H{"success": false, "error": "OVH API错误: " + string(respBody)})
			return
		}
		var result map[string]interface{}
		_ = json.Unmarshal(respBody, &result)
		state.Logger.Info("服务器 "+svc+" 系统重装请求已发送，模板: "+templateName, "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "服务器 " + svc + " 系统重装请求已发送",
			"taskId":  result["taskId"],
		})
		_ = url.Values{} // keep import
	}
}

// translateInstallStep 对应 Python: translate_install_step
var translationMap = map[string]string{
	"Pre-configuring Post-installation":               "预配置安装后脚本",
	"Downloading OS image":                            "下载系统镜像",
	"Deploying OS on disks":                           "部署系统到磁盘",
	"Configuring Boot":                                "配置启动项",
	"Checking Partitioning":                           "检查分区",
	"Switching boot":                                  "切换启动模式",
	"Running Last Reboot":                             "执行最后重启",
	"Waiting for services to be up":                   "等待服务启动",
	"Publishing Admin password on API":                "发布管理员密码到API",
	"Checking BIOS version":                           "检查BIOS版本",
	"Running Hardware Reboot":                         "执行硬件重启",
	"Setting up hardware raid":                        "配置硬件RAID",
	"Preparing disks for new Partitioning":            "准备磁盘分区",
	"Checking hardware":                               "检查硬件",
	"Initializing hardware":                           "初始化硬件",
	"Preparing installation":                          "准备安装",
	"Partitioning disk":                               "分区磁盘",
	"Partitioning disks":                              "分区磁盘",
	"Cleaning Partitioning":                           "清理分区",
	"Processing Partitioning":                         "处理分区",
	"Applying Partitioning":                           "应用分区配置",
	"Formatting partitions":                           "格式化分区",
	"Installing system":                               "安装系统",
	"Installing system files":                         "安装系统文件",
	"Installing packages":                             "安装软件包",
	"Installing bootloader":                           "安装引导程序",
	"Installing grub":                                 "安装GRUB引导",
	"Configuring system":                              "配置系统",
	"Configuring network":                             "配置网络",
	"Setting up network":                              "设置网络",
	"Setting up system":                               "设置系统",
	"Applying configuration":                          "应用配置",
	"Processing Post-installation configuration":      "处理安装后配置",
	"Finalizing installation":                         "完成安装",
	"Rebooting":                                       "重启中",
	"Rebooting server":                                "重启服务器",
	"Reboot":                                          "重启",
	"First boot":                                      "首次启动",
	"Booting":                                         "启动中",
	"Starting services":                               "启动服务",
	"Starting system services":                       "启动系统服务",
	"Enabling services":                               "启用服务",
	"Installation completed":                          "安装完成",
	"Installation finished":                           "安装完成",
	"Done":                                            "完成",
	"Completed":                                       "已完成",
	"Wiping disks":                                    "擦除磁盘",
	"Cleaning disks":                                  "清理磁盘",
	"Creating partitions":                             "创建分区",
	"Creating filesystems":                            "创建文件系统",
	"Mounting filesystems":                            "挂载文件系统",
	"Fetching image":                                  "获取镜像",
	"Extracting image":                                "解压镜像",
	"Copying files":                                   "复制文件",
	"Generating configuration":                        "生成配置",
	"Writing configuration":                           "写入配置",
	"Setting hostname":                                "设置主机名",
	"Configuring timezone":                            "配置时区",
	"Configuring locale":                              "配置语言",
	"Generating SSH keys":                             "生成SSH密钥",
	"Setting root password":                           "设置root密码",
	"Managing Admin password":                         "管理管理员密码",
	"Publishing password":                             "发布密码",
	"Sending end of installation mail":                "发送安装完成邮件",
	"Sending notification":                            "发送通知",
	"Notifying completion":                            "通知完成",
	"Failed":                                          "失败",
	"Failed to download":                              "下载失败",
	"Failed to install":                               "安装失败",
	"Error":                                           "错误",
	"Partition error":                                 "分区错误",
	"Boot configuration failed":                       "启动配置失败",
	"Network configuration failed":                    "网络配置失败",
	"Timeout":                                         "超时",
}

func translateInstallStep(comment string) string {
	if strings.TrimSpace(comment) == "" {
		return comment
	}
	for eng, chn := range translationMap {
		if strings.EqualFold(comment, eng) {
			return chn
		}
	}
	commentLower := strings.ToLower(comment)
	for eng, chn := range translationMap {
		if strings.Contains(commentLower, strings.ToLower(eng)) {
			return chn
		}
	}
	return comment
}

// GetInstallStatus GET /api/server-control/:service_name/install/status
func GetInstallStatus(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := state.OVH.Client()
		if err != nil {
			noOVHResp(c)
			return
		}
		var status map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/install/status", &status); err != nil {
			lower := strings.ToLower(err.Error())
			noInstall := []string{"404", "not found", "no installation", "no task", "does not exist",
				"resource not found", "this service is not", "no os installation", "not installing",
				"installation not found", "not being installed", "not being reinstalled",
				"being installed or reinstalled at the moment"}
			for _, ind := range noInstall {
				if strings.Contains(lower, ind) {
					state.Logger.Info("服务器 "+svc+" 当前没有正在进行的安装", "server_control")
					c.JSON(http.StatusOK, gin.H{
						"success":         true,
						"hasInstallation": false,
						"message":         "当前没有正在进行的安装",
					})
					return
				}
			}
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		elapsedTime := 0
		if v, ok := numconv.ToInt64(status["elapsedTime"]); ok {
			elapsedTime = int(v)
		}
		progressArr, _ := status["progress"].([]interface{})
		totalSteps := len(progressArr)
		completed := 0
		hasError := false
		formatted := []gin.H{}
		for _, sRaw := range progressArr {
			step, _ := sRaw.(map[string]interface{})
			st, _ := step["status"].(string)
			comment, _ := step["comment"].(string)
			errMsg, _ := step["error"].(string)
			if st == "done" {
				completed++
			}
			if st == "error" {
				hasError = true
			}
			formatted = append(formatted, gin.H{
				"comment":         translateInstallStep(comment),
				"commentOriginal": comment,
				"status":          st,
				"error":           errMsg,
			})
		}
		progressPercentage := 0
		if totalSteps > 0 {
			progressPercentage = completed * 100 / totalSteps
		}
		allDone := totalSteps > 0 && completed == totalSteps
		state.Logger.Info(fmt.Sprintf("获取服务器 %s 安装进度: %d%%", svc, progressPercentage), "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"status": gin.H{
				"elapsedTime":        elapsedTime,
				"progressPercentage": progressPercentage,
				"totalSteps":         totalSteps,
				"completedSteps":     completed,
				"hasError":           hasError,
				"allDone":            allDone,
				"steps":              formatted,
			},
		})
	}
}

// GetServerTasks GET /api/server-control/:service_name/tasks
func GetServerTasks(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := state.OVH.Client()
		if err != nil {
			noOVHResp(c)
			return
		}
		var taskIDs []interface{}
		if err := client.Get("/dedicated/server/"+svc+"/task", &taskIDs); err != nil {
			state.Logger.Error("获取服务器 "+svc+" 任务列表失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		// 只取最近 10 个，并发拉详情
		start := len(taskIDs) - 10
		if start < 0 {
			start = 0
		}
		recent := taskIDs[start:]
		details := parallelGetDetails(client, recent, func(k interface{}) string {
			return "/dedicated/server/" + svc + "/task/" + idToString(k)
		}, 10)
		tasks := []gin.H{}
		for i, taskID := range recent {
			detail := details[i]
			if detail == nil {
				continue
			}
			tasks = append(tasks, gin.H{
				"taskId":    taskID,
				"function":  valueOr(detail, "function", "N/A"),
				"status":    valueOr(detail, "status", "unknown"),
				"comment":   valueOr(detail, "comment", ""),
				"startDate": valueOr(detail, "startDate", ""),
				"doneDate":  valueOr(detail, "doneDate", ""),
			})
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "tasks": tasks, "total": len(tasks)})
	}
}

// GetTaskAvailableTimeslots GET /api/server-control/:service_name/tasks/:task_id/available-timeslots
func GetTaskAvailableTimeslots(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		taskID := c.Param("task_id")
		client, err := state.OVH.Client()
		if err != nil {
			noOVHResp(c)
			return
		}
		periodStart := c.Query("periodStart")
		periodEnd := c.Query("periodEnd")
		if periodStart == "" || periodEnd == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 periodStart 或 periodEnd 参数 (ISO8601)"})
			return
		}
		state.Logger.Info(fmt.Sprintf("[Task] 查询任务 %s 的可用时间段 %s -> %s", taskID, periodStart, periodEnd), "server_control")
		var slots []map[string]interface{}
		q := url.Values{}
		q.Set("periodStart", periodStart)
		q.Set("periodEnd", periodEnd)
		path := fmt.Sprintf("/dedicated/server/%s/task/%s/availableTimeslots?%s", svc, taskID, q.Encode())
		if err := client.Get(path, &slots); err != nil {
			msg := err.Error()
			lower := strings.ToLower(msg)
			if strings.Contains(lower, "no schedule needed") {
				state.Logger.Info("[Task] 任务无需预约: "+msg, "server_control")
				c.JSON(http.StatusOK, gin.H{
					"success":             true,
					"timeslots":           []interface{}{},
					"scheduleNotRequired": true,
					"message":             "该任务无需预约",
				})
				return
			}
			if strings.Contains(lower, "not found") || strings.Contains(lower, "does not exist") {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "任务或服务器不存在"})
				return
			}
			state.Logger.Error("[Task] 可用时间段API错误: "+msg, "server_control")
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": msg})
			return
		}
		if slots == nil {
			slots = []map[string]interface{}{}
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "timeslots": slots})
	}
}

// ScheduleTaskTimeslot POST /api/server-control/:service_name/tasks/:task_id/schedule
func ScheduleTaskTimeslot(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		taskID := c.Param("task_id")
		client, err := state.OVH.Client()
		if err != nil {
			noOVHResp(c)
			return
		}
		var body struct {
			StartDate string `json:"startDate"`
			EndDate   string `json:"endDate"`
		}
		_ = c.ShouldBindJSON(&body)
		if body.StartDate == "" || body.EndDate == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 startDate 或 endDate (ISO8601)"})
			return
		}
		state.Logger.Info(fmt.Sprintf("[Task] 预约任务 %s 时间段 %s -> %s", taskID, body.StartDate, body.EndDate), "server_control")
		var result map[string]interface{}
		path := fmt.Sprintf("/dedicated/server/%s/task/%s/schedule", svc, taskID)
		if err := client.Post(path, map[string]interface{}{
			"startDate": body.StartDate,
			"endDate":   body.EndDate,
		}, &result); err != nil {
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "no schedule needed") {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "该任务无需或不支持预约"})
				return
			}
			if strings.Contains(lower, "not found") || strings.Contains(lower, "does not exist") {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "任务或服务器不存在"})
				return
			}
			state.Logger.Error("[Task] 预约任务API错误: "+err.Error(), "server_control")
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "result": result})
	}
}
