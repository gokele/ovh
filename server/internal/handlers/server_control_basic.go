package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	ovhsdk "github.com/ovh/go-ovh/ovh"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/numconv"
	"github.com/ovh-buy/server/internal/ovh"
)

// noOVHResp 401 帮助
func noOVHResp(c *gin.Context) {
	c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "未配置OVH API密钥"})
}

// ── 区域门控 ──────────────────────────────────────────────────────────────
//
// EU / US / CA 是三套彼此独立的 OVH 系统，/dedicated/server 命名空间的路径数就不一样
// （live schema 实测：EU 107 条、CA 107 条、US 98 条）。US 少的正好是这 9 条：
//
//	/dedicated/server/log                       /dedicated/server/virtualNetworkInterface/{uuid}
//	/dedicated/server/{sn}/changeContact        /dedicated/server/{sn}/license/windows
//	/dedicated/server/{sn}/features/backupFTP  (+ access / access/{ipBlock} / authorizableBlocks / password)
//
// 其中本域真正会调用的只有 changeContact 和 backupFTP 全家桶，两处都必须提前拦下来，
// 否则 go-ovh 会拿一个本站点不存在的路径去请求，用户看到的是一句 "Got an invalid (or empty) URL"
// 或 OVH 的英文 404，完全看不出"这是美区没有的功能"。
// EU 与 CA 在 /dedicated/server、/ip、/services、/dedicated/installationTemplate 四个命名空间上
// 逐条对比过（路径 + 方法 + 参数 + 返回类型 + 废弃标记），零差异，所以本域不需要 CA 专属门控。
const srvRegionUS = "US"

// srvRegionFor 当前请求所用账户的 OVH 大区(EU / US / CA)。
//
// 一律走 ovh.EndpointRegion，不要在各 handler 里散写 `acc.Endpoint == "ovh-us"`：
// endpoint 是账户表里的自由字符串，除了 ovh-* 还有 kimsufi-* / soyoustart-* 品牌别名，
// 散装字符串比较早晚会漏掉一种写法，而漏掉的后果是美区用户点到一个必然失败的按钮。
// 账户查不到时 acc 是零值，EndpointRegion("") 回落 EU —— 这条路径上紧接着的 ovhClientFor
// 一定会失败并走 noOVHResp，所以不存在"用错误的大区放行"的情况。
func srvRegionFor(state *app.State, c *gin.Context) string {
	acc, _ := ovhAccountFor(state, c)
	return ovh.EndpointRegion(acc.Endpoint)
}

// ListMyServers GET /api/server-control/list
func ListMyServers(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		client, err := ovhClientFor(state, c)
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
			info         map[string]interface{}
			svcInfo      map[string]interface{}
			detailError  error
			svcInfoError error
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
				// serviceInfos 的错误不能吞：吞掉之后"查不到"会被渲染成"没开自动续费"，
				// 用户以为不用管，机器到期就被回收了
				if err := client.Get("/dedicated/server/"+nm+"/serviceInfos", &svc); err != nil {
					results[idx].svcInfoError = err
					return
				}
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
			// 用 *bool：schema 里 renew 可空，加上查询本身也可能失败，
			// 这两种"不知道"必须和"确实关闭了自动续费"区分开
			var renewalType *bool
			if r.svcInfoError != nil {
				state.Logger.Error("获取服务器 "+name+" 续费信息失败: "+r.svcInfoError.Error(), "server_control")
			} else if svcInfo != nil {
				if rn, ok := svcInfo["renew"].(map[string]interface{}); ok {
					if a, ok := rn["automatic"].(bool); ok {
						renewalType = &a
					}
				}
				if renewalType == nil {
					// renew 为 null 时退回顶层 renewalType(service.RenewalTypeEnum)，
					// 枚举里 automatic* 系列都表示自动续费
					if rt, ok := svcInfo["renewalType"].(string); ok && rt != "" {
						auto := strings.HasPrefix(rt, "automatic")
						renewalType = &auto
					}
				}
			}
			// 缺失字段补默认值
			monitoring := info["monitoring"]
			if monitoring == nil {
				monitoring = false
			}
			professionalUse := info["professionalUse"]
			if professionalUse == nil {
				professionalUse = false
			}
			entry := gin.H{
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
			}
			if r.svcInfoError != nil {
				entry["svcInfoError"] = r.svcInfoError.Error()
			}
			servers = append(servers, entry)
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
		client, err := ovhClientFor(state, c)
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
		client, err := ovhClientFor(state, c)
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

		// 串行逐个 GET 模板详情要 10-50 秒（50-100 个模板）。
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

// reinstallSoftRaidLevels 软 RAID 合法级别
// schema: dedicated.server.reinstall.storage.partitioning.layout.RaidLevelEnum
var reinstallSoftRaidLevels = map[int64]bool{0: true, 1: true, 5: true, 6: true, 7: true, 10: true}

// reinstallHardRaidLevels 硬件 RAID 合法级别
// schema: dedicated.server.reinstall.storage.hardwareRaid.RaidLevelEnum
var reinstallHardRaidLevels = map[int64]bool{0: true, 1: true, 5: true, 6: true, 10: true, 50: true, 60: true}

// reinstallFileSystems 分区文件系统合法取值
// schema: dedicated.server.reinstall.storage.partitioning.layout.FileSystemEnum
var reinstallFileSystems = map[string]bool{
	"btrfs": true, "ext3": true, "ext4": true, "fat16": true, "none": true,
	"ntfs": true, "reiserfs": true, "swap": true, "ufs": true,
	"vmfs5": true, "vmfs6": true, "vmfsl": true, "xfs": true, "zfs": true,
}

// diskSizeToGB 把 complexType.UnitAndValue<long> 归一成 GB。
// schema 只写了 unit 是必填 string、没写枚举，OVH 实际会返回法语缩写(Go/To/Mo)也会返回 GB/TB，
// 两套都认；认不出来的单位宁可放弃自动算容量，也不要按 GB 硬算出一个差 1000 倍的分区大小
func diskSizeToGB(raw interface{}) (int, bool) {
	ds, ok := raw.(map[string]interface{})
	if !ok {
		return 0, false
	}
	v, ok := numconv.ToInt64(ds["value"])
	if !ok {
		return 0, false
	}
	unit, _ := ds["unit"].(string)
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "g", "go", "gb", "gib":
		return int(v), true
	case "m", "mo", "mb", "mib":
		return int(v / 1024), true
	case "t", "to", "tb", "tib":
		return int(v * 1024), true
	case "p", "po", "pb", "pib":
		return int(v * 1024 * 1024), true
	}
	return 0, false
}

// parseRaidLevelValue 兼容前端可能传的 1 / "1" / "raid1" 三种写法
func parseRaidLevelValue(v interface{}) (int64, bool) {
	if v == nil {
		return 0, false
	}
	if n, ok := numconv.ToInt64(v); ok {
		return n, true
	}
	if s, ok := v.(string); ok {
		s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "raid")
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

// normalizeStorageConfig 把前端的 storageConfig 显式映射成 dedicated.server.reinstall.Storage[]。
// 前端拼硬件 RAID 用的是旧 partitionScheme 的字段名(disks 磁盘编号数组 / mode / name / step)，
// 而 schema 的 HardwareRaid 只有 arrays / disks(数量) / raidLevel / spares，字段名和类型都对不上。
// schema 没写 OVH 会不会忽略未知字段，所以这里只放行 schema 里存在的字段，
// 非法值在后端就拦下来报中文错误，而不是丢给 OVH 返回一句英文错误码
// fsRaidIncompatible 文件系统 × 软 RAID 级别的不兼容组合。
//
// 取自 OVH 官方分区文档的兼容表(LVM, RAID Levels & Filesystems Compatibility):
//
//	Btrfs / ext4 / XFS   RAID 7  ❌(其余 0/1/5/6/10 都 ✅)
//	ZFS                  RAID 10 ❌(其余 0/1/5/6/7 都 ✅)
//	NTFS                 只有 RAID 1 ✅
//	UFS / VMFS*          任何 RAID 都 ❌
//
// 这些约束只在文档里,schema 的 RaidLevelEnum 是 [0,1,5,6,7,10] 一视同仁,
// 不本地拦的话用户会拿到一句 OVH 的英文错误,不知道是自己选错了组合。
// swap 那一行文档自相矛盾(表格标 RAID 0 可用、RAID 1 不可用,脚注却说"只能设为 1"),
// 所以这里不对 swap 下判断 —— 没有可靠依据的地方不替用户做决定。
func checkFSRaidCompat(fs string, level int64) string {
	switch fs {
	case "btrfs", "ext4", "ext3", "xfs", "reiserfs":
		if level == 7 {
			return "ext4 / XFS / Btrfs 不支持 RAID 7(OVH 分区文档),请改用 0/1/5/6/10"
		}
	case "zfs":
		if level == 10 {
			return "ZFS 不支持 RAID 10(OVH 分区文档),请改用 0/1/5/6/7"
		}
	case "ntfs":
		if level != 1 {
			return "NTFS 只支持 RAID 1(OVH 分区文档)"
		}
	case "ufs", "vmfs5", "vmfs6", "vmfsl":
		return fs + " 不支持任何软 RAID(OVH 分区文档)"
	}
	return ""
}

func normalizeStorageConfig(raw interface{}) ([]map[string]interface{}, error) {
	groups, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("自定义存储配置格式不正确，应为数组")
	}
	out := []map[string]interface{}{}
	// size=0(占满剩余空间)的分区在整份配置里最多一个,跨磁盘组一起数
	fillCount := 0
	for _, gRaw := range groups {
		g, ok := gRaw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("自定义存储配置格式不正确，磁盘组应为对象")
		}
		entry := map[string]interface{}{}
		// 磁盘组编号从 1 起 —— 官方分区文档:"By default, the OS will be installed
		// on diskGroupId 1",示例里也是 1 / 2。0 是前端"没选磁盘组"的占位值,
		// 发出去等于指定了一个不存在的组。省略即用默认组,这才是文档的语义。
		if v, ok := numconv.ToInt64(g["diskGroupId"]); ok && v > 0 {
			entry["diskGroupId"] = v
		}
		if hrRaw, ok := g["hardwareRaid"].([]interface{}); ok && len(hrRaw) > 0 {
			arrays := []map[string]interface{}{}
			for _, hRaw := range hrRaw {
				h, ok := hRaw.(map[string]interface{})
				if !ok {
					return nil, fmt.Errorf("硬件 RAID 配置格式不正确")
				}
				level, ok := parseRaidLevelValue(h["raidLevel"])
				if !ok {
					level, ok = parseRaidLevelValue(h["mode"])
				}
				if !ok {
					level, ok = parseRaidLevelValue(h["name"])
				}
				if !ok {
					return nil, fmt.Errorf("硬件 RAID 缺少 raidLevel")
				}
				if !reinstallHardRaidLevels[level] {
					return nil, fmt.Errorf("硬件 RAID 级别 %d 不受支持，可选：0/1/5/6/10/50/60", level)
				}
				item := map[string]interface{}{"raidLevel": level}
				// schema 的 disks 是"参与阵列的磁盘数量"，前端给的是磁盘编号数组，这里换算成数量
				switch d := h["disks"].(type) {
				case []interface{}:
					if len(d) > 0 {
						item["disks"] = int64(len(d))
					}
				default:
					if n, ok := numconv.ToInt64(h["disks"]); ok && n > 0 {
						item["disks"] = n
					}
				}
				if n, ok := numconv.ToInt64(h["arrays"]); ok && n > 0 {
					item["arrays"] = n
				}
				if n, ok := numconv.ToInt64(h["spares"]); ok && n > 0 {
					item["spares"] = n
				}
				arrays = append(arrays, item)
			}
			entry["hardwareRaid"] = arrays
		}
		if pRaw, ok := g["partitioning"].(map[string]interface{}); ok {
			part := map[string]interface{}{}
			if s, ok := pRaw["schemeName"].(string); ok && strings.TrimSpace(s) != "" {
				part["schemeName"] = strings.TrimSpace(s)
			}
			if n, ok := numconv.ToInt64(pRaw["disks"]); ok && n > 0 {
				part["disks"] = n
			}
			if lRaw, ok := pRaw["layout"].([]interface{}); ok {
				layout := []map[string]interface{}{}
				for _, itRaw := range lRaw {
					it, ok := itRaw.(map[string]interface{})
					if !ok {
						return nil, fmt.Errorf("分区配置格式不正确")
					}
					fs, _ := it["fileSystem"].(string)
					fs = strings.ToLower(strings.TrimSpace(fs))
					if !reinstallFileSystems[fs] {
						return nil, fmt.Errorf("不支持的文件系统 %q", fs)
					}
					mp, _ := it["mountPoint"].(string)
					mp = strings.TrimSpace(mp)
					if mp == "" {
						return nil, fmt.Errorf("分区缺少挂载点")
					}
					size, _ := numconv.ToInt64(it["size"])
					if size < 0 {
						return nil, fmt.Errorf("分区 %s 的大小不能为负数", mp)
					}
					// size=0 = 占满剩余空间。官方分区文档:
					// "Up to 1 partition can be configured to fill the remaining space (size 0)"
					if size == 0 {
						fillCount++
						if fillCount > 1 {
							return nil, fmt.Errorf("最多只能有一个分区把大小留空(占满剩余空间),当前有多个,请给其余分区指定大小")
						}
						// 同一份文档明确禁止 swap 占满磁盘:
						// "You have chosen the swap partition to fill the disk ... we disallow this"
						if fs == "swap" {
							return nil, fmt.Errorf("swap 分区必须指定大小,不能留空占满磁盘(OVH 不允许)")
						}
					}
					// schema: size 是必填 long，0 表示用尽剩余空间
					lay := map[string]interface{}{
						"fileSystem": fs,
						"mountPoint": mp,
						"size":       size,
					}
					if rl, ok := parseRaidLevelValue(it["raidLevel"]); ok {
						if !reinstallSoftRaidLevels[rl] {
							return nil, fmt.Errorf("分区 %s 的软 RAID 级别 %d 不受支持，可选：0/1/5/6/7/10", mp, rl)
						}
						if msg := checkFSRaidCompat(fs, rl); msg != "" {
							return nil, fmt.Errorf("分区 %s: %s", mp, msg)
						}
						lay["raidLevel"] = rl
					}
					if ex, ok := it["extras"].(map[string]interface{}); ok {
						extras := map[string]interface{}{}
						if zp, ok := ex["zp"].(map[string]interface{}); ok {
							if n, ok := zp["name"].(string); ok && n != "" {
								extras["zp"] = map[string]interface{}{"name": n}
							}
						}
						if lv, ok := ex["lv"].(map[string]interface{}); ok {
							if n, ok := lv["name"].(string); ok && n != "" {
								extras["lv"] = map[string]interface{}{"name": n}
							}
						}
						if len(extras) > 0 {
							lay["extras"] = extras
						}
					}
					layout = append(layout, lay)
				}
				if len(layout) > 0 {
					part["layout"] = layout
				}
			}
			if len(part) > 0 {
				entry["partitioning"] = part
			}
		}
		if len(entry) == 0 || (entry["partitioning"] == nil && entry["hardwareRaid"] == nil) {
			return nil, fmt.Errorf("自定义存储配置里有磁盘组既没有分区也没有硬件 RAID")
		}
		out = append(out, entry)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("自定义存储配置为空")
	}
	return out, nil
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

		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var body map[string]interface{}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求体格式错误: " + err.Error()})
			return
		}
		templateName, _ := body["templateName"].(string)
		if templateName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "未指定系统模板"})
			return
		}

		installParams := map[string]interface{}{
			"operatingSystem": templateName,
		}
		if v, ok := body["customHostname"].(string); ok && v != "" {
			// schema: 主机名在 dedicated.server.reinstall.Customizations.hostname，
			// 顶层 customHostname 是旧 /install/start 的写法，reinstall 不认这个字段
			installParams["customizations"] = map[string]interface{}{"hostname": v}
			state.Logger.Info("设置自定义主机名: "+v, "server_control")
		}

		useZFS, _ := body["useProxmox9Zfs"].(bool)
		schemeName, _ := body["partitionSchemeName"].(string)
		schemeName = strings.TrimSpace(schemeName)
		hasCustomStorage := isNonEmptyStorage(body["storageConfig"])
		// 三条路径都往同一个 storage 字段写，同时给必然互相覆盖。但不能一律 400：
		// 前端 ReinstallDialog 的 "Proxmox 9 + ZFS" 开关默认就是勾上的（useProxmox9Zfs 只受模板约束），
		// 和"高级存储配置"/"内置分区方案"在 UI 上并不互斥，一律拒绝会把本来能跑通的 proxmox9 重装打成硬失败。
		// 所以只有 storageConfig 与 partitionSchemeName 这两个"用户亲手填的存储配置"同时出现时才算真歧义；
		// ZFS 预设与它们撞车时沿用历史行为（预设优先），把被忽略的那份写进日志和响应，用户看得见但装得成。
		if hasCustomStorage && schemeName != "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "自定义存储配置与内置分区方案只能选择一种",
			})
			return
		}
		var installWarnings []string
		if useZFS && (hasCustomStorage || schemeName != "") {
			ignored := "自定义存储配置"
			if !hasCustomStorage {
				ignored = "内置分区方案 " + schemeName
			}
			warn := "已启用 Proxmox 9 + ZFS 预设，本次忽略同时提交的" + ignored
			installWarnings = append(installWarnings, warn)
			state.Logger.Warn(warn, "server_control")
		}

		// Proxmox 9 + ZFS 预设
		if useZFS {
			raidLevel := int64(1)
			if raw, ok := body["zfsRaidLevel"]; ok && raw != nil {
				v, ok := numconv.ToInt64(raw)
				if !ok {
					c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "zfsRaidLevel 必须是整数"})
					return
				}
				raidLevel = v
			}
			if !reinstallSoftRaidLevels[raidLevel] {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error":   fmt.Sprintf("RAID 级别 %d 不受支持，可选：0/1/5/6/7/10", raidLevel),
				})
				return
			}
			vzSizeMB := int64(102400)
			if raw, ok := body["zfsVzSize"]; ok && raw != nil {
				v, ok := numconv.ToInt64(raw)
				if !ok {
					c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "zfsVzSize 必须是整数(MB)"})
					return
				}
				vzSizeMB = v
			}
			if vzSizeMB < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "/var/lib/vz 容量不能为负数"})
				return
			}
			state.Logger.Info(fmt.Sprintf("🎯 使用 Proxmox 9 + ZFS 根文件系统预设 (RAID%d)", raidLevel), "server_control")

			totalCapacityGB := 480
			if raidLevel == 0 {
				totalCapacityGB = 960
			}
			diskCount := 2
			var diskGroupID interface{}
			var hardware map[string]interface{}
			if err := client.Get("/dedicated/server/"+svc+"/specifications/hardware", &hardware); err == nil {
				if v, ok := numconv.ToInt64(hardware["defaultDiskGroupId"]); ok {
					diskGroupID = v
				}
				if dgs, ok := hardware["diskGroups"].([]interface{}); ok && len(dgs) > 0 {
					if dg, ok := dgs[0].(map[string]interface{}); ok {
						if v, ok := numconv.ToInt64(dg["numberOfDisks"]); ok {
							diskCount = int(v)
						}
						// 容量是按 diskGroups[0] 算的，diskGroupId 就得跟着它走；
						// 之前写死 0 会出现"按 A 组算容量、往默认组下分区"
						if v, ok := numconv.ToInt64(dg["diskGroupId"]); ok {
							diskGroupID = v
						}
						if singleDiskGB, ok := diskSizeToGB(dg["diskSize"]); ok {
							if raidLevel == 0 {
								totalCapacityGB = singleDiskGB * diskCount
							} else {
								totalCapacityGB = singleDiskGB
							}
							state.Logger.Info(fmt.Sprintf("📊 检测到磁盘: %dx%dGB, RAID%d 总容量: %dGB",
								diskCount, singleDiskGB, raidLevel, totalCapacityGB), "server_control")
						} else {
							state.Logger.Warn(fmt.Sprintf("磁盘容量或单位无法识别，使用默认容量: %dGB", totalCapacityGB), "server_control")
						}
					}
				}
			} else {
				state.Logger.Warn(fmt.Sprintf("获取硬件信息失败，使用默认容量: %dGB - %s", totalCapacityGB, err.Error()), "server_control")
			}

			if diskCount < 2 && raidLevel != 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error":   fmt.Sprintf("该服务器只有 %d 块磁盘，无法使用 RAID%d，请改用 RAID0", diskCount, raidLevel),
				})
				return
			}

			usableCapacityMB := int64(float64(totalCapacityGB) * 1024 * 0.92)
			const bootSizeMB = int64(1024)
			const swapSizeMB = int64(8192)
			bootSwapMB := bootSizeMB + swapSizeMB
			rootSizeMB := usableCapacityMB - bootSwapMB - vzSizeMB
			// schema 的 layout.size 是必填 long，算成负数会被 OVH 以 PartitionIncompatibleParams 拒掉，
			// 在这里拦下来才能给出用户看得懂的可用范围
			if rootSizeMB <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error": fmt.Sprintf("/var/lib/vz 容量 %dMB 超出可用范围：本机 RAID%d 下可用约 %dMB，扣除 /boot %dMB 和 swap %dMB 后，该值必须小于 %dMB",
						vzSizeMB, raidLevel, usableCapacityMB, bootSizeMB, swapSizeMB, usableCapacityMB-bootSwapMB),
				})
				return
			}
			state.Logger.Info(fmt.Sprintf("💾 容量计算: 理论%dGB, 实际可用~%dGB, 根目录%dGB",
				totalCapacityGB, usableCapacityMB/1024, rootSizeMB/1024), "server_control")

			// swap 之前写死 raidLevel:1，单盘机器必然失败；只有 2 盘及以上才做镜像
			swapRaidLevel := int64(1)
			if diskCount < 2 {
				swapRaidLevel = 0
			}
			storageEntry := map[string]interface{}{
				"partitioning": map[string]interface{}{
					"layout": []map[string]interface{}{
						{
							"fileSystem": "ext4",
							"mountPoint": "/boot",
							"raidLevel":  raidLevel,
							"size":       bootSizeMB,
						},
						{
							"fileSystem": "swap",
							"mountPoint": "swap",
							"raidLevel":  swapRaidLevel,
							"size":       swapSizeMB,
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
			}
			// diskGroupId 是可空字段，取不到真实 id 就整个省略，让 OVH 用默认磁盘组
			if diskGroupID != nil {
				storageEntry["diskGroupId"] = diskGroupID
			}
			installParams["storage"] = []map[string]interface{}{storageEntry}
			state.Logger.Info(fmt.Sprintf("✅ ZFS 配置: /boot (1GB) + swap (8GB) + / (%dGB) + /var/lib/vz", rootSizeMB/1024), "server_control")
		} else if hasCustomStorage {
			// 之前是把前端的 storageConfig 原样透传，字段名/类型跟 schema 对不上，
			// 硬件 RAID 重装 100% 被 OVH 拒；现在显式映射成 schema 结构
			storage, serr := normalizeStorageConfig(body["storageConfig"])
			if serr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": serr.Error()})
				return
			}
			state.Logger.Info("使用自定义存储配置", "server_control")
			installParams["storage"] = storage
		} else if schemeName != "" {
			// 前端选的 OVH 预置分区方案之前被整个丢掉了；
			// schema 里它对应 storage[].partitioning.schemeName
			state.Logger.Info("使用分区方案: "+schemeName, "server_control")
			installParams["storage"] = []map[string]interface{}{
				{"partitioning": map[string]interface{}{"schemeName": schemeName}},
			}
		} else {
			state.Logger.Info("使用默认分区配置", "server_control")
		}

		state.Logger.Info("准备发送安装请求到OVH API", "server_control")
		state.Logger.Info("  - 服务器: "+svc, "server_control")
		state.Logger.Info("  - 模板: "+templateName, "server_control")

		// 之前这里手拼签名 + 本地时钟时间戳：宿主机时钟一漂就只有重装报 Invalid signature，
		// 而且 base URL 是自己映射的，未知 endpoint 会静默落到 EU。
		// go-ovh 会先跟 /auth/time 校时再签名，并且用的就是该账户自己的 endpoint
		var result map[string]interface{}
		if err := client.Post("/dedicated/server/"+svc+"/reinstall", installParams, &result); err != nil {
			state.Logger.Error("重装服务器 "+svc+" 系统失败: "+err.Error(), "server_control")
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": "OVH API错误: " + err.Error()})
			return
		}
		state.Logger.Info("服务器 "+svc+" 系统重装请求已发送，模板: "+templateName, "server_control")
		resp := gin.H{
			"success": true,
			"message": "服务器 " + svc + " 系统重装请求已发送",
			"taskId":  result["taskId"],
		}
		if len(installWarnings) > 0 {
			resp["warnings"] = installWarnings
		}
		c.JSON(http.StatusOK, resp)
	}
}

var translationMap = map[string]string{
	"Pre-configuring Post-installation":          "预配置安装后脚本",
	"Downloading OS image":                       "下载系统镜像",
	"Deploying OS on disks":                      "部署系统到磁盘",
	"Configuring Boot":                           "配置启动项",
	"Checking Partitioning":                      "检查分区",
	"Switching boot":                             "切换启动模式",
	"Running Last Reboot":                        "执行最后重启",
	"Waiting for services to be up":              "等待服务启动",
	"Publishing Admin password on API":           "发布管理员密码到API",
	"Checking BIOS version":                      "检查BIOS版本",
	"Running Hardware Reboot":                    "执行硬件重启",
	"Setting up hardware raid":                   "配置硬件RAID",
	"Preparing disks for new Partitioning":       "准备磁盘分区",
	"Checking hardware":                          "检查硬件",
	"Initializing hardware":                      "初始化硬件",
	"Preparing installation":                     "准备安装",
	"Partitioning disk":                          "分区磁盘",
	"Partitioning disks":                         "分区磁盘",
	"Cleaning Partitioning":                      "清理分区",
	"Processing Partitioning":                    "处理分区",
	"Applying Partitioning":                      "应用分区配置",
	"Formatting partitions":                      "格式化分区",
	"Installing system":                          "安装系统",
	"Installing system files":                    "安装系统文件",
	"Installing packages":                        "安装软件包",
	"Installing bootloader":                      "安装引导程序",
	"Installing grub":                            "安装GRUB引导",
	"Configuring system":                         "配置系统",
	"Configuring network":                        "配置网络",
	"Setting up network":                         "设置网络",
	"Setting up system":                          "设置系统",
	"Applying configuration":                     "应用配置",
	"Processing Post-installation configuration": "处理安装后配置",
	"Finalizing installation":                    "完成安装",
	"Rebooting":                                  "重启中",
	"Rebooting server":                           "重启服务器",
	"Reboot":                                     "重启",
	"First boot":                                 "首次启动",
	"Booting":                                    "启动中",
	"Starting services":                          "启动服务",
	"Starting system services":                   "启动系统服务",
	"Enabling services":                          "启用服务",
	"Installation completed":                     "安装完成",
	"Installation finished":                      "安装完成",
	"Done":                                       "完成",
	"Completed":                                  "已完成",
	"Wiping disks":                               "擦除磁盘",
	"Cleaning disks":                             "清理磁盘",
	"Creating partitions":                        "创建分区",
	"Creating filesystems":                       "创建文件系统",
	"Mounting filesystems":                       "挂载文件系统",
	"Fetching image":                             "获取镜像",
	"Extracting image":                           "解压镜像",
	"Copying files":                              "复制文件",
	"Generating configuration":                   "生成配置",
	"Writing configuration":                      "写入配置",
	"Setting hostname":                           "设置主机名",
	"Configuring timezone":                       "配置时区",
	"Configuring locale":                         "配置语言",
	"Generating SSH keys":                        "生成SSH密钥",
	"Setting root password":                      "设置root密码",
	"Managing Admin password":                    "管理管理员密码",
	"Publishing password":                        "发布密码",
	"Sending end of installation mail":           "发送安装完成邮件",
	"Sending notification":                       "发送通知",
	"Notifying completion":                       "通知完成",
	"Failed":                                     "失败",
	"Failed to download":                         "下载失败",
	"Failed to install":                          "安装失败",
	"Error":                                      "错误",
	"Partition error":                            "分区错误",
	"Boot configuration failed":                  "启动配置失败",
	"Network configuration failed":               "网络配置失败",
	"Timeout":                                    "超时",
}

// translationOrder 是 translationMap 的键按长度降序排的副本。
// map 迭代顺序随机，而 "Failed"/"Failed to download" 这类键互相包含，
// 直接 range 匹配会让同一条状态在前端轮询之间来回跳译，固定成最长匹配优先
var translationOrder = func() []string {
	keys := make([]string, 0, len(translationMap))
	for k := range translationMap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	return keys
}()

func translateInstallStep(comment string) string {
	if strings.TrimSpace(comment) == "" {
		return comment
	}
	for _, eng := range translationOrder {
		if strings.EqualFold(comment, eng) {
			return translationMap[eng]
		}
	}
	commentLower := strings.ToLower(comment)
	for _, eng := range translationOrder {
		if strings.Contains(commentLower, strings.ToLower(eng)) {
			return translationMap[eng]
		}
	}
	return comment
}

// GetInstallStatus GET /api/server-control/:service_name/install/status
func GetInstallStatus(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		var status map[string]interface{}
		if err := client.Get("/dedicated/server/"+svc+"/install/status", &status); err != nil {
			// 之前拿 err.Error() 做子串匹配，"404" 三个字必然命中 go-ovh 的错误文案，
			// 服务器名写错/机器不属于当前账户也被当成"没有安装任务"。
			// 改成判 HTTP 状态码，并且 404 时多问一次服务器本体，
			// 确认机器确实存在才敢降级成"当前没有安装"
			var apiErr *ovhsdk.APIError
			if errors.As(err, &apiErr) {
				// 只认 OVH 明确说"这台机器没有安装任务"的这几句文案；
				// 原来的列表里还有裸的 "404"/"not found"，那种任何 404 都命中
				lower := strings.ToLower(apiErr.Message)
				for _, ind := range []string{"no installation", "no os installation", "not installing",
					"installation not found", "not being installed", "not being reinstalled",
					"being installed or reinstalled at the moment"} {
					if strings.Contains(lower, ind) {
						state.Logger.Info("服务器 "+svc+" 当前没有正在进行的安装: "+apiErr.Message, "server_control")
						c.JSON(http.StatusOK, gin.H{
							"success":         true,
							"hasInstallation": false,
							"message":         "当前没有正在进行的安装",
						})
						return
					}
				}
				if apiErr.Code == http.StatusNotFound {
					// 404 文案不确定时多问一次服务器本体：机器确实在，才敢说"没有安装"
					var info map[string]interface{}
					if serr := client.Get("/dedicated/server/"+svc, &info); serr != nil {
						state.Logger.Error("获取服务器 "+svc+" 安装状态失败(服务器不可访问): "+serr.Error(), "server_control")
						c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "服务器不存在或不属于当前账户"})
						return
					}
					state.Logger.Info("服务器 "+svc+" 当前没有正在进行的安装", "server_control")
					c.JSON(http.StatusOK, gin.H{
						"success":         true,
						"hasInstallation": false,
						"message":         "当前没有正在进行的安装",
					})
					return
				}
			}
			state.Logger.Error("获取服务器 "+svc+" 安装状态失败: "+err.Error(), "server_control")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		elapsedTime := 0
		if v, ok := numconv.ToInt64(status["elapsedTime"]); ok {
			elapsedTime = int(v)
		}
		// schema 里 progress 可空：null 不等于"0 步全没做完"，
		// 否则进度条会一直停在 0% 且 allDone 永远为 false 而没人知道为什么
		progressRaw, hasProgress := status["progress"]
		progressUnknown := !hasProgress || progressRaw == nil
		progressArr, _ := progressRaw.([]interface{})
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
		if progressUnknown {
			state.Logger.Warn("服务器 "+svc+" 安装进度为空(OVH 未返回 progress)", "server_control")
		}
		state.Logger.Info(fmt.Sprintf("获取服务器 %s 安装进度: %d%%", svc, progressPercentage), "server_control")
		c.JSON(http.StatusOK, gin.H{
			"success":         true,
			"hasInstallation": true,
			"status": gin.H{
				"elapsedTime":        elapsedTime,
				"progressPercentage": progressPercentage,
				"totalSteps":         totalSteps,
				"completedSteps":     completed,
				"hasError":           hasError,
				"allDone":            allDone,
				"progressUnknown":    progressUnknown,
				"steps":              formatted,
			},
		})
	}
}

// GetServerTasks GET /api/server-control/:service_name/tasks
func GetServerTasks(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		client, err := ovhClientFor(state, c)
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
		// schema 只承诺返回 long[]、没承诺顺序，所以先自己按 id 升序排再取最近 10 个
		sort.SliceStable(taskIDs, func(i, j int) bool {
			a, _ := numconv.ToInt64(taskIDs[i])
			b, _ := numconv.ToInt64(taskIDs[j])
			return a < b
		})
		start := len(taskIDs) - 10
		if start < 0 {
			start = 0
		}
		recent := taskIDs[start:]

		// 详情失败不能静默丢条目：并发拉详情遇到限流/超时时列表会莫名其妙变短，
		// 用户看不到刚触发的任务就会重复提交，这里保留占位并把错误带给前端
		details := make([]map[string]interface{}, len(recent))
		detailErrs := make([]error, len(recent))
		sem := make(chan struct{}, 10)
		var wg sync.WaitGroup
		for i, tid := range recent {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, id interface{}) {
				defer wg.Done()
				defer func() { <-sem }()
				var d map[string]interface{}
				if err := client.Get("/dedicated/server/"+svc+"/task/"+idToString(id), &d); err != nil {
					detailErrs[idx] = err
					return
				}
				details[idx] = d
			}(i, tid)
		}
		wg.Wait()

		tasks := []gin.H{}
		failed := 0
		for i, taskID := range recent {
			detail := details[i]
			if detail == nil {
				failed++
				msg := "任务详情获取失败"
				if detailErrs[i] != nil {
					msg = detailErrs[i].Error()
				}
				state.Logger.Error(fmt.Sprintf("获取服务器 %s 任务 %s 详情失败: %s", svc, idToString(taskID), msg), "server_control")
				tasks = append(tasks, gin.H{
					"taskId":    taskID,
					"function":  "N/A",
					"status":    "unknown",
					"comment":   "",
					"startDate": "",
					"doneDate":  "",
					"error":     msg,
				})
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
		c.JSON(http.StatusOK, gin.H{"success": true, "tasks": tasks, "total": len(tasks), "failed": failed})
	}
}

// normalizeAPIDate 把入参归一成 OVH 的 date 类型(YYYY-MM-DD)。
// 接受纯日期和完整 ISO8601 时间戳两种写法，其余一律拒掉：
// schema 写死了参数是 date，与其赌 OVH 解析宽松，不如在这里就把格式错误讲清楚
func normalizeAPIDate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("2006-01-02"), true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		// 取时间戳自带偏移下的那一天，不折算成 UTC：
		// 2026-08-24T23:00:00-05:00 在调用方眼里就是 8/24，折成 UTC 会整体偏一天，查询窗口跟着错位
		return t.Format("2006-01-02"), true
	}
	return "", false
}

// GetTaskAvailableTimeslots GET /api/server-control/:service_name/tasks/:task_id/available-timeslots
func GetTaskAvailableTimeslots(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc := c.Param("service_name")
		taskID := c.Param("task_id")
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		periodStart, ok := normalizeAPIDate(c.Query("periodStart"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "periodStart 必须是 YYYY-MM-DD 或 ISO8601 时间"})
			return
		}
		periodEnd, ok := normalizeAPIDate(c.Query("periodEnd"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "periodEnd 必须是 YYYY-MM-DD 或 ISO8601 时间"})
			return
		}
		state.Logger.Info(fmt.Sprintf("[Task] 查询任务 %s 的可用时间段 %s -> %s", taskID, periodStart, periodEnd), "server_control")
		var slots []map[string]interface{}
		q := url.Values{}
		q.Set("periodStart", periodStart)
		q.Set("periodEnd", periodEnd)
		path := fmt.Sprintf("/dedicated/server/%s/task/%s/availableTimeslots?%s", svc, taskID, q.Encode())
		if err := client.Get(path, &slots); err != nil {
			var apiErr *ovhsdk.APIError
			if errors.As(err, &apiErr) {
				// "no schedule needed" 是 OVH 对不需要预约的任务给的业务语义，
				// 只认这条具体消息，不拿整条 error 字符串模糊匹配
				if strings.Contains(strings.ToLower(apiErr.Message), "no schedule needed") {
					state.Logger.Info("[Task] 任务无需预约: "+apiErr.Message, "server_control")
					c.JSON(http.StatusOK, gin.H{
						"success":             true,
						"timeslots":           []interface{}{},
						"scheduleNotRequired": true,
						"message":             "该任务无需预约",
					})
					return
				}
				if apiErr.Code == http.StatusNotFound {
					state.Logger.Warn("[Task] 任务或服务器不存在: "+err.Error(), "server_control")
					c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "任务或服务器不存在"})
					return
				}
			}
			state.Logger.Error("[Task] 可用时间段API错误: "+err.Error(), "server_control")
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": err.Error()})
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
		client, err := ovhClientFor(state, c)
		if err != nil {
			noOVHResp(c)
			return
		}
		// schema: POST .../schedule 的 body 只有 wantedBeginingDate(datetime) 和
		// hasPerformedBackup(boolean) 两个必填参数，之前发的 startDate/endDate 必被 400。
		// startDate 留作兼容别名，免得已有调用方直接断链
		var body struct {
			WantedBeginingDate string `json:"wantedBeginingDate"`
			StartDate          string `json:"startDate"`
			HasPerformedBackup *bool  `json:"hasPerformedBackup"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求体格式错误: " + err.Error()})
			return
		}
		wanted := strings.TrimSpace(body.WantedBeginingDate)
		if wanted == "" {
			wanted = strings.TrimSpace(body.StartDate)
		}
		if wanted == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 wantedBeginingDate (ISO8601 日期时间)"})
			return
		}
		wantedTime, perr := time.Parse(time.RFC3339, wanted)
		if perr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "wantedBeginingDate 必须是 ISO8601 日期时间，例如 2026-01-02T15:04:05Z"})
			return
		}
		// hasPerformedBackup 是 schema 必填项，缺了 OVH 必 400，所以在这里就要求调用方显式表态。
		// 但 false 在 schema 里是合法取值（它是"你是否已备份"的如实回答，不是开关），
		// 上一轮直接 400 拒掉等于把 OVH 允许的调用变成不可达，这里改成原样透传 + 记一条警告日志。
		if body.HasPerformedBackup == nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少 hasPerformedBackup：预约干预前必须确认是否已备份数据"})
			return
		}
		if !*body.HasPerformedBackup {
			state.Logger.Warn(fmt.Sprintf("[Task] 任务 %s 预约干预时声明未备份数据(hasPerformedBackup=false)，干预可能导致数据丢失", taskID), "server_control")
		}
		state.Logger.Info(fmt.Sprintf("[Task] 预约任务 %s 干预开始时间 %s", taskID, wantedTime.Format(time.RFC3339)), "server_control")
		path := fmt.Sprintf("/dedicated/server/%s/task/%s/schedule", svc, taskID)
		// 该端点返回 void，不给 resType，免得 go-ovh 去解析一个空 body
		if err := client.Post(path, map[string]interface{}{
			"wantedBeginingDate": wantedTime.Format(time.RFC3339),
			"hasPerformedBackup": *body.HasPerformedBackup,
		}, nil); err != nil {
			var apiErr *ovhsdk.APIError
			if errors.As(err, &apiErr) {
				switch apiErr.Code {
				case http.StatusNotFound:
					state.Logger.Warn("[Task] 任务或服务器不存在: "+err.Error(), "server_control")
					c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "任务或服务器不存在"})
					return
				case http.StatusBadRequest, http.StatusConflict:
					// OVH 的业务校验（任务不支持预约、时间段已被占用等）原样透出
					state.Logger.Error("[Task] 预约任务被拒绝: "+err.Error(), "server_control")
					c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": apiErr.Message})
					return
				}
			}
			state.Logger.Error("[Task] 预约任务API错误: "+err.Error(), "server_control")
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": err.Error()})
			return
		}
		state.Logger.Info(fmt.Sprintf("[Task] 任务 %s 干预时间预约成功", taskID), "server_control")
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "干预时间已预约"})
	}
}
