package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/auth"
	"github.com/ovh-buy/server/internal/catalog"
	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/handlers"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/purchase"
	"github.com/ovh-buy/server/internal/secret"
	"github.com/ovh-buy/server/internal/storage"
	"github.com/ovh-buy/server/internal/telegram"
	"github.com/ovh-buy/server/internal/updater"
)

func main() {
	// envPath 就是 godotenv 读的那个文件。密钥自动生成时会追加到这里,
	// 所以路径必须和 Load() 用的完全一致 —— 分叉了就会出现
	// "写进了 A、下次从 B 读"的情况,而那意味着重新生成一把新密钥。
	envPath := envFilePath()
	_ = godotenv.Load(envPath)

	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("DEBUG"), "true") {
		level = slog.LevelDebug
	}
	console := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	paths := storage.DefaultPaths()
	if err := paths.EnsureDirs(); err != nil {
		console.Error("create dirs", "err", err)
		os.Exit(1)
	}

	// 落盘加密:必须在打开数据库**之前**初始化,否则第一次读账户时解不开。
	// 拿不到密钥不致命 —— 退化成明文(与升级前一致),但要明确告警,
	// 不能让用户以为自己开了加密其实没开。
	if err := secret.Init(paths.DataDir, envPath); err != nil {
		console.Warn("凭据加密未启用，数据库里的 OVH 密钥与 Telegram Token 将以明文存储", "err", err)
	} else if secret.KeyWasGenerated() {
		console.Info("已自动生成数据库加密密钥并写入 " + secret.KeySource())
		console.Info("请把它连同配置文件一起备份 —— 丢了这把钥匙，已保存的 OVH 凭据和 Telegram Token 就再也解不开了")
	} else {
		console.Info("凭据加密已启用（密钥来自 " + secret.KeySource() + "）")
	}

	sqliteDB, err := db.Open(paths.DataDir)
	if err != nil {
		console.Error("open sqlite", "err", err)
		os.Exit(1)
	}

	// 密钥是刚生成的、库里却已经有密文 —— 说明原来那把钥匙丢了。
	// 直接起来的话程序看着一切正常，只是每次调 OVH 都报签名错误，
	// 没人猜得到是密钥问题；更糟的是用户会重新录入凭据把旧密文覆盖掉，
	// 最后一点恢复余地也没了。所以这里宁可不起来，把话说清楚。
	if secret.KeyWasGenerated() && !isTrue(os.Getenv("OVH_DB_KEY_RESET")) {
		if has, herr := sqliteDB.HasEncryptedSecrets(); herr == nil && has {
			console.Error("数据库里有加密过的凭据，但找不到对应的密钥，已停止启动")
			console.Error("这通常是配置文件被覆盖/重置了，或者换机器时只拷了数据库没拷配置")
			console.Error("怎么办：把原来的 " + secret.KeyEnv + " 那一行放回 " + envPath +
				"（或用环境变量传进来）；确实找不回来就设 OVH_DB_KEY_RESET=1 启动，" +
				"那些账户需要重新录入 OVH 凭据")
			sqliteDB.Close()
			os.Exit(1)
		}
	}
	defer sqliteDB.Close()

	// 老库升级:把已有的明文凭据就地加密。幂等,不迁移也能跑(读的时候兼容明文)
	if n, merr := sqliteDB.EncryptExistingSecrets(); merr != nil {
		console.Warn("迁移明文凭据时出错（不影响启动）", "err", merr)
	} else if n > 0 {
		console.Info("已把数据库里的明文凭据加密", "账户数", n)
	}

	lg := logger.New(paths.LogFile("app.log.json"), console)
	cfgStore := config.New(sqliteDB)
	state := app.NewState(paths, cfgStore, lg, sqliteDB)
	state.APIKey = os.Getenv("API_SECRET_KEY")
	if state.APIKey == "" {
		state.APIKey = "123456"
	}
	state.Port = os.Getenv("PORT")
	if state.Port == "" {
		state.Port = "19998"
	}
	state.LoadAll()

	// 密钥对不上时凭据会全部解成空串。只静默显示"空账户"的话,用户看到的现象是
	// "账户还在、但连不上 OVH",完全猜不到根因。这里大声说清楚,并给出可执行的下一步。
	if n := secret.DecryptFailures(); n > 0 {
		console.Error("有 " + fmt.Sprintf("%d", n) + " 个加密字段解不开 —— 密钥与数据库对不上。" +
			"常见原因:只恢复了 sniper.db 却没恢复 .dbkey,或换过 " + secret.KeyEnv + "。" +
			"把原来的密钥放回去即可;找不回来的话需要在设置页重新录入 OVH 凭据。")
		state.Logger.Error(fmt.Sprintf("[加密] %d 个字段解密失败:密钥与数据库不匹配", n), "system")
	} else if secret.KeyWasGenerated() && len(state.Accounts) > 0 {
		console.Warn("新生成了加密密钥,但库里已有账户 —— 如果这些账户的凭据是空的,说明原密钥丢了")
	}

	// 上一次更新留下的残骸(中断的临时文件)在这里清掉
	updater.CleanupStale()

	// 上一次更新之后没能正常启动?换回更新前的版本再跑。
	// 判据是"有备份 + 有待验证标记":新版本活到对外服务那一刻会把标记删掉,
	// 标记还在说明它没撑到那一步(配置不兼容、平台问题、启动即崩)。
	// 抢购服务停机就是错过补货,不能让一次坏更新把机器撂在那儿。
	if updater.RollbackIfStale(state) {
		if exe, err := os.Executable(); err == nil {
			console.Warn("已回滚到更新前的版本，正在用它重启")
			_ = sqliteDB.Close()
			if err := updater.Restart(exe); err != nil {
				console.Error("回滚后重启失败", "err", err)
				os.Exit(1)
			}
		}
	}

	// gracefulRestart 由 SelfUpdate 在替换完二进制后调用。
	// 必须先关监听端口和 SQLite 再 exec:端口不放新进程会撞 "address already in use",
	// SQLite 不干净关闭会留下 -wal / -shm。
	var gracefulRestart = func(exe string) {}

	// 监控器
	mon := monitor.New(state)
	mon.LoadFromDB()
	console.Info("监控就绪", "checkInterval", mon.CheckInterval())

	// Gin
	if mode := os.Getenv("GIN_MODE"); mode != "" {
		gin.SetMode(mode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(cors.New(cors.Config{
		AllowAllOrigins: true,
		AllowMethods:    []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:    []string{"Content-Type", "Authorization", "X-API-Key", "X-Request-Time"},
		// X-Partial-Failures:部分明细拉取失败的计数(账单/退款/邮件等走响应头下发),
		// 跨源部署时不列进 ExposeHeaders 浏览器就读不到,前端的"部分失败"提示会恒不显示
		ExposeHeaders:    []string{"X-Cache-Warning", "X-Partial-Failures", "X-Cache-Age-Seconds"},
		AllowCredentials: false,
	}))

	enableAuth := !strings.EqualFold(os.Getenv("ENABLE_API_KEY_AUTH"), "false")
	r.Use(auth.Middleware(auth.Config{
		APIKey:         state.APIKey,
		Enabled:        enableAuth,
		WhitelistPaths: auth.DefaultWhitelist(),
	}))

	// 健康检查
	r.GET("/health", handlers.Health())

	api := r.Group("/api")
	{
		api.GET("/health", handlers.Health())

		// Settings
		api.GET("/settings", handlers.GetSettings(state))
		api.POST("/settings", handlers.SaveSettings(state))
		api.POST("/verify-auth", handlers.VerifyAuth(state))
		api.GET("/endpoint-config", handlers.EndpointConfig(state))

		// Logs / stats
		api.GET("/logs", handlers.GetLogs(state))
		api.POST("/logs/flush", handlers.FlushLogs(state))
		api.DELETE("/logs", handlers.ClearLogs(state))
		api.GET("/stats", handlers.GetStats(state, mon))

		// Queue
		api.GET("/queue", handlers.GetQueue(state))
		api.GET("/queue/timings", handlers.GetPurchaseTimings(state))
		api.POST("/queue", handlers.AddQueueItem(state))
		api.DELETE("/queue/clear", handlers.ClearQueue(state))
		api.DELETE("/queue/:id", handlers.RemoveQueueItem(state))
		api.PUT("/queue/:id/status", handlers.UpdateQueueStatus(state))

		// Purchase history
		api.GET("/purchase-history", handlers.GetPurchaseHistory(state))
		api.DELETE("/purchase-history", handlers.ClearPurchaseHistory(state))

		// Monitor
		api.GET("/monitor/subscriptions", handlers.GetSubscriptions(state, mon))
		api.POST("/monitor/subscriptions", handlers.AddSubscription(state, mon))
		api.PUT("/monitor/subscriptions/:planCode", handlers.UpdateSubscription(state, mon))
		api.POST("/monitor/subscriptions/batch-add-all", handlers.BatchAddAll(state, mon))
		api.DELETE("/monitor/subscriptions/clear", handlers.ClearSubscriptions(state, mon))
		api.DELETE("/monitor/subscriptions/:planCode", handlers.RemoveSubscription(state, mon))
		api.GET("/monitor/subscriptions/:planCode/history", handlers.GetSubscriptionHistory(state, mon))
		api.POST("/monitor/start", handlers.StartMonitor(state, mon))
		api.POST("/monitor/stop", handlers.StopMonitor(state, mon))
		api.GET("/monitor/status", handlers.GetMonitorStatus(state, mon))
		api.PUT("/monitor/interval", handlers.SetMonitorInterval(state, mon))
		api.POST("/monitor/test-notification", handlers.TestNotification(state))
		// 通知通道体检:哪条配了、哪条能用。?verify=true 会真的去调远端
		api.GET("/notify/channels", handlers.GetNotifyChannels(state))
		api.GET("/telegram/verify", handlers.VerifyTelegram(state))

		// Telegram
		api.POST("/telegram/set-webhook", handlers.SetTelegramWebhook(state))
		api.GET("/telegram/get-webhook-info", handlers.GetTelegramWebhookInfo(state))
		api.POST("/telegram/webhook", handlers.TelegramWebhook(state, mon))

		// Servers / availability / cache
		api.GET("/servers", handlers.GetServers(state))
		api.GET("/availability/*planCode", availabilityHandler(handlers.GetAvailability(state)))
		api.POST("/availability/*planCode", availabilityHandler(handlers.GetAvailability(state)))
		api.POST("/internal/monitor/price", handlers.MonitorPrice(state))
		api.POST("/servers/:planCode/price", handlers.ServerPrice(state))
		api.GET("/cache/info", handlers.CacheInfo(state))
		api.POST("/cache/clear", handlers.ClearCache(state))
		api.GET("/catalog", handlers.GetCatalog(state))
		api.GET("/system/metrics", handlers.GetSystemMetrics(state))
		api.GET("/version", handlers.GetVersion(state))
		api.GET("/version/check-update", handlers.CheckUpdate(state))
		// 在线更新:下载 → 校验 → 替换自己 → 自动重启。gracefulRestart 在下面赋值,
		// 这里用闭包间接引用,避免"路由要在 server 之前注册、server 又要在路由之后创建"的鸡生蛋
		api.POST("/version/update", handlers.SelfUpdate(state, func(exe string) { gracefulRestart(exe) }))
		api.GET("/version/update/status", handlers.GetUpdateStatus(state))

		// Accounts (多账户管理)
		api.GET("/accounts", handlers.ListAccounts(state))
		api.GET("/accounts/:id", handlers.GetAccountByID(state))
		api.POST("/accounts", handlers.CreateAccount(state))
		api.PUT("/accounts/:id", handlers.UpdateAccount(state))
		api.DELETE("/accounts/:id", handlers.DeleteAccountByID(state))
		api.POST("/accounts/:id/set-default", handlers.SetDefaultAccountByID(state))
		api.POST("/accounts/:id/verify", handlers.VerifyAccount(state))

		// 快速下单端点 (监控的 auto-order 通过 HTTP 自调走它,前端也可直接调)
		api.POST("/queue/quick-order", handlers.QuickOrder(state))

		// Server control - basic
		sc := api.Group("/server-control")
		{
			sc.GET("/list", handlers.ListMyServers(state))
			// 服务器本地别名:纯本地显示用,不下发 OVH
			sc.GET("/aliases", handlers.ListServerAliases(state))
			sc.PUT("/:service_name/alias", handlers.SetServerAlias(state))
			sc.DELETE("/:service_name/alias", handlers.DeleteServerAlias(state))
			sc.GET("/order-mapping", handlers.GetOrderMapping(state))
			sc.POST("/:service_name/reboot", handlers.Reboot(state))
			sc.GET("/:service_name/templates", handlers.GetOSTemplates(state))
			sc.POST("/:service_name/install", handlers.InstallOS(state))
			sc.GET("/:service_name/install/status", handlers.GetInstallStatus(state))
			sc.GET("/:service_name/tasks", handlers.GetServerTasks(state))
			sc.GET("/:service_name/tasks/:task_id/available-timeslots", handlers.GetTaskAvailableTimeslots(state))
			sc.POST("/:service_name/tasks/:task_id/schedule", handlers.ScheduleTaskTimeslot(state))

			// boot/monitoring
			sc.GET("/:service_name/boot", handlers.GetBootConfig(state))
			sc.PUT("/:service_name/boot/:boot_id", handlers.SetBootConfig(state))
			sc.GET("/:service_name/monitoring", handlers.GetMonitoringStatus(state))
			sc.PUT("/:service_name/monitoring", handlers.SetMonitoringStatus(state))
			sc.GET("/:service_name/boot-mode", handlers.GetBootModes(state))
			sc.PUT("/:service_name/boot-mode", handlers.ChangeBootMode(state))

			// hardware/network/dns
			sc.GET("/:service_name/hardware", handlers.GetHardwareInfo(state))
			sc.GET("/:service_name/network-specs", handlers.GetNetworkSpecs(state))
			sc.GET("/:service_name/ips", handlers.GetServerIPs(state))
			sc.GET("/:service_name/reverse", handlers.GetReverseDNS(state))
			sc.POST("/:service_name/reverse", handlers.SetReverseDNS(state))
			sc.DELETE("/:service_name/reverse/:ip", handlers.DeleteReverseDNS(state))
			sc.GET("/:service_name/serviceinfo", handlers.GetServiceInfo(state))
			sc.PUT("/:service_name/serviceinfo/renewal", handlers.UpdateServiceRenewal(state))

			// engagement(合同期切换)
			sc.GET("/:service_name/engagement", handlers.GetEngagement(state))
			sc.GET("/:service_name/engagement/available", handlers.GetEngagementAvailable(state))
			sc.GET("/:service_name/engagement/request", handlers.GetEngagementRequest(state))
			sc.POST("/:service_name/engagement/request", handlers.CreateEngagementRequest(state))
			sc.DELETE("/:service_name/engagement/request", handlers.DeleteEngagementRequest(state))
			sc.PUT("/:service_name/engagement/end-rule", handlers.UpdateEngagementEndRule(state))

			// DDoS mitigation
			sc.GET("/:service_name/mitigation", handlers.GetMitigation(state))
			sc.POST("/:service_name/mitigation/:ip", handlers.EnableMitigation(state))
			sc.DELETE("/:service_name/mitigation/:ip", handlers.DisableMitigation(state))
			sc.POST("/:service_name/change-contact", handlers.ChangeContact(state))
			sc.GET("/:service_name/interventions", handlers.GetInterventions(state))
			sc.GET("/:service_name/interventions/:intervention_id", handlers.GetInterventionDetail(state))
			sc.GET("/:service_name/planned-interventions", handlers.GetPlannedInterventions(state))
			sc.GET("/:service_name/planned-interventions/:intervention_id", handlers.GetPlannedInterventionDetail(state))
			sc.POST("/:service_name/hardware/replace", handlers.HardwareReplace(state))
			sc.GET("/:service_name/hardware-raid-profiles", handlers.GetHardwareRaidProfiles(state))
			sc.GET("/:service_name/hardware-disk-info", handlers.GetHardwareDiskInfo(state))
			sc.GET("/:service_name/partition-schemes", handlers.GetPartitionSchemes(state))

			// network
			sc.GET("/:service_name/network-interfaces", handlers.GetNetworkInterfaces(state))
			sc.GET("/:service_name/mrtg", handlers.GetMRTGData(state))
			sc.POST("/:service_name/ola/aggregation", handlers.ConfigureOLAAggregation(state))
			sc.POST("/:service_name/ola/reset", handlers.ResetOLAConfiguration(state))
			sc.POST("/:service_name/ola/group", handlers.OLAGroup(state))
			sc.POST("/:service_name/ola/ungroup", handlers.OLAUngroup(state))
			sc.GET("/:service_name/console", handlers.GetIPMIConsole(state))
			// 只查支持哪几种控制台类型(HTML5 / Java KVM / SOL),不申请会话
			sc.GET("/:service_name/ipmi-types", handlers.GetIPMIAccessTypes(state))
			sc.GET("/:service_name/statistics", handlers.GetTrafficStatistics(state))
			sc.GET("/:service_name/network-stats", handlers.GetNetworkInterfaceStats(state))

			// features
			sc.GET("/:service_name/burst", handlers.GetBurst(state))
			sc.PUT("/:service_name/burst", handlers.UpdateBurst(state))
			sc.GET("/:service_name/firewall", handlers.GetFirewall(state))
			sc.PUT("/:service_name/firewall", handlers.UpdateFirewall(state))
			sc.GET("/:service_name/backup-ftp", handlers.GetBackupFTP(state))
			sc.POST("/:service_name/backup-ftp", handlers.ActivateBackupFTP(state))
			sc.DELETE("/:service_name/backup-ftp", handlers.DeleteBackupFTP(state))
			sc.GET("/:service_name/backup-ftp/access", handlers.GetBackupFTPAccess(state))
			sc.POST("/:service_name/backup-ftp/access", handlers.AddBackupFTPAccess(state))
			// ipBlock 是带掩码的 CIDR(如 37.59.1.0/28)。gin 默认 UseRawPath=false,%2F 会被还原成 "/",
			// 把 URL 撑成多一段,:ip_block 永远匹配不上(实测编码与否都 404)。
			// 所以主用 query 形式 ?ipBlock=...,旧的路径形式保留做兼容(handler 三级兜底取值)。
			sc.DELETE("/:service_name/backup-ftp/access", handlers.DeleteBackupFTPAccess(state))
			sc.DELETE("/:service_name/backup-ftp/access/:ip_block", handlers.DeleteBackupFTPAccess(state))
			sc.POST("/:service_name/backup-ftp/password", handlers.ChangeBackupFTPPassword(state))
			sc.GET("/:service_name/backup-ftp/authorizable-blocks", handlers.GetBackupFTPAuthorizableBlocks(state))
			sc.GET("/:service_name/backup-cloud", handlers.GetBackupCloud(state))
			sc.GET("/:service_name/backup-cloud/offer-details", handlers.GetBackupCloudOfferDetails(state))
			// 云备份的写操作(官方 /features/backupCloud POST/DELETE 与 /password POST),
			// 原来只实现了只读,用户无法在控制台激活/停用/重置密码
			sc.POST("/:service_name/backup-cloud", handlers.ActivateBackupCloud(state))
			sc.DELETE("/:service_name/backup-cloud", handlers.DeleteBackupCloud(state))
			sc.POST("/:service_name/backup-cloud/password", handlers.ChangeBackupCloudPassword(state))

			// misc
			sc.GET("/:service_name/secondary-dns", handlers.GetSecondaryDNS(state))
			sc.POST("/:service_name/secondary-dns", handlers.AddSecondaryDNS(state))
			sc.DELETE("/:service_name/secondary-dns/:domain", handlers.DeleteSecondaryDNS(state))
			sc.GET("/:service_name/virtual-mac", handlers.GetVirtualMACList(state))
			sc.POST("/:service_name/virtual-mac", handlers.CreateVirtualMAC(state))
			sc.GET("/:service_name/virtual-network-interface", handlers.GetVirtualNetworkInterfaces(state))
			sc.POST("/:service_name/virtual-network-interface/:uuid/enable", handlers.EnableVirtualNetworkInterface(state))
			sc.POST("/:service_name/virtual-network-interface/:uuid/disable", handlers.DisableVirtualNetworkInterface(state))
			sc.GET("/:service_name/vrack", handlers.GetVRackList(state))
			sc.DELETE("/:service_name/vrack/:vrack", handlers.RemoveFromVRack(state))
			sc.GET("/:service_name/orderable/bandwidth", handlers.GetOrderableBandwidth(state))
			sc.GET("/:service_name/orderable/traffic", handlers.GetOrderableTraffic(state))
			sc.GET("/:service_name/orderable/ip", handlers.GetOrderableIP(state))
			sc.GET("/:service_name/options", handlers.GetServerOptions(state))
			sc.GET("/:service_name/ip-specs", handlers.GetIPSpecs(state))
			sc.GET("/:service_name/ip/can-be-moved-to", handlers.GetIPCanBeMovedTo(state))
			sc.GET("/:service_name/ip/country-available", handlers.GetIPCountryAvailable(state))
			sc.POST("/:service_name/ip/move", handlers.MoveIP(state))
			sc.GET("/:service_name/ongoing", handlers.GetOngoingTasks(state))
			sc.GET("/:service_name/license/windows/compliant", handlers.GetCompliantWindowsVersions(state))
			sc.GET("/:service_name/license/windows-sql/compliant", handlers.GetCompliantWindowsSqlVersions(state))
			// 到期终止走 terminationPolicy,不是 /terminate ——
			// 后者是立即终止,提交即暂停服务器
			sc.PUT("/:service_name/termination-policy", handlers.UpdateTerminationPolicy(state))
			sc.POST("/:service_name/terminate", handlers.TerminateService(state))
			sc.POST("/:service_name/confirm-termination", handlers.ConfirmTermination(state))
			sc.GET("/:service_name/spla", handlers.GetSPLAList(state))
			sc.POST("/:service_name/spla", handlers.CreateSPLA(state))
			sc.GET("/:service_name/bios-settings", handlers.GetBIOSSettings(state))
			sc.GET("/:service_name/bios-settings/sgx", handlers.GetBIOSSettingsSGX(state))
		}

		// VPS control(已购 VPS 管理)
		vc := api.Group("/vps-control")
		{
			vc.GET("/list", handlers.ListVps(state))
			vc.GET("/:service_name/info", handlers.GetVpsInfo(state))
			vc.GET("/:service_name/status", handlers.GetVpsServiceStatus(state))
			vc.GET("/:service_name/serviceinfo", handlers.GetVpsServiceInfo(state))
			vc.PUT("/:service_name/serviceinfo/renewal", handlers.UpdateVpsRenewal(state))
			vc.GET("/:service_name/ips", handlers.GetVpsIps(state))
			vc.PUT("/:service_name/ips/:ip/reverse", handlers.SetVpsIpReverse(state))
			vc.GET("/:service_name/datacenter", handlers.GetVpsDatacenter(state))
			// 注:OVH 已废弃 /vps/{name}/monitoring (2024-07) 和 /statistics (2023-11),
			// 不提供替代的 VPS 级监控端点,前端不再展示监控视图。

			// 电源
			vc.POST("/:service_name/start", handlers.VpsStart(state))
			vc.POST("/:service_name/stop", handlers.VpsStop(state))
			vc.POST("/:service_name/reboot", handlers.VpsReboot(state))
			vc.POST("/:service_name/console", handlers.VpsGetConsoleUrl(state))
			vc.POST("/:service_name/password", handlers.VpsSetPassword(state))

			// 重装系统
			vc.GET("/:service_name/current-os", handlers.GetVpsCurrentOS(state))
			vc.GET("/:service_name/templates", handlers.GetVpsTemplates(state))
			vc.POST("/:service_name/reinstall", handlers.ReinstallVps(state))

			// 任务
			vc.GET("/:service_name/tasks", handlers.GetVpsTasks(state))
			vc.GET("/:service_name/tasks/:task_id", handlers.GetVpsTaskDetail(state))

			// 快照
			vc.GET("/:service_name/snapshot", handlers.GetVpsSnapshot(state))
			vc.POST("/:service_name/snapshot", handlers.CreateVpsSnapshot(state))
			vc.PUT("/:service_name/snapshot", handlers.UpdateVpsSnapshotDescription(state))
			vc.POST("/:service_name/snapshot/revert", handlers.RevertVpsSnapshot(state))
			vc.DELETE("/:service_name/snapshot", handlers.DeleteVpsSnapshot(state))

			// 杂项
			vc.POST("/:service_name/change-contact", handlers.ChangeVpsContact(state))
			vc.PUT("/:service_name/termination-policy", handlers.UpdateVpsTerminationPolicy(state))
			vc.POST("/:service_name/terminate", handlers.TerminateVps(state))
			vc.POST("/:service_name/confirm-termination", handlers.ConfirmVpsTermination(state))
			vc.GET("/:service_name/secondary-dns", handlers.GetVpsSecondaryDns(state))
			vc.POST("/:service_name/secondary-dns", handlers.AddVpsSecondaryDns(state))
			vc.DELETE("/:service_name/secondary-dns/:domain", handlers.DeleteVpsSecondaryDns(state))
			vc.GET("/:service_name/options", handlers.GetVpsOptions(state))
			vc.DELETE("/:service_name/options/:option", handlers.DeleteVpsOption(state))
			vc.GET("/:service_name/automated-backup", handlers.GetVpsAutomatedBackup(state))

			// 合同期(engagement)
			vc.GET("/:service_name/engagement", handlers.GetVpsEngagement(state))
			vc.GET("/:service_name/engagement/available", handlers.GetVpsEngagementAvailable(state))
			vc.GET("/:service_name/engagement/request", handlers.GetVpsEngagementRequest(state))
			vc.POST("/:service_name/engagement/request", handlers.CreateVpsEngagementRequest(state))
			vc.DELETE("/:service_name/engagement/request", handlers.DeleteVpsEngagementRequest(state))
			vc.PUT("/:service_name/engagement/end-rule", handlers.UpdateVpsEngagementEndRule(state))

			// DDoS mitigation(IP 级别,但 IP 列表从 /vps/{svc}/ips 取)
			vc.GET("/:service_name/mitigation", handlers.GetVpsMitigation(state))
			vc.POST("/:service_name/mitigation/:ip", handlers.EnableVpsMitigation(state))
			vc.DELETE("/:service_name/mitigation/:ip", handlers.DisableVpsMitigation(state))
		}

		// VPS monitor
		// 在售型号来自 OVH 实时目录 —— 写死过的那份已经整代停售了
		api.GET("/vps-monitor/models", handlers.GetVPSModels(state))
		api.GET("/vps-monitor/subscriptions", handlers.GetVPSSubscriptions(state))
		api.POST("/vps-monitor/subscriptions", handlers.AddVPSSubscription(state))
		api.PUT("/vps-monitor/subscriptions/:subscription_id", handlers.UpdateVPSSubscription(state))
		api.DELETE("/vps-monitor/subscriptions/clear", handlers.ClearVPSSubscriptions(state))
		api.DELETE("/vps-monitor/subscriptions/:subscription_id", handlers.RemoveVPSSubscription(state))
		api.GET("/vps-monitor/subscriptions/:subscription_id/history", handlers.GetVPSSubscriptionHistory(state))
		api.POST("/vps-monitor/start", handlers.StartVPSMonitor(state))
		api.POST("/vps-monitor/stop", handlers.StopVPSMonitor(state))
		api.GET("/vps-monitor/status", handlers.GetVPSMonitorStatus(state))
		api.PUT("/vps-monitor/interval", handlers.SetVPSMonitorInterval(state))
		api.POST("/vps-monitor/check/:plan_code", handlers.ManualCheckVPS(state))

		// Account
		api.GET("/ovh/account/info", handlers.GetAccountInfo(state))
		api.GET("/ovh/account/refunds", handlers.GetAccountRefunds(state))
		api.GET("/ovh/account/credit-balance", handlers.GetCreditBalance(state))
		api.GET("/ovh/account/email-history", handlers.GetEmailHistory(state))
		api.GET("/ovh/contact-change-requests", handlers.GetContactChangeRequests(state))
		api.GET("/ovh/contact-change-requests/:task_id", handlers.GetContactChangeRequestDetail(state))
		api.POST("/ovh/contact-change-requests/:task_id/accept", handlers.AcceptContactChangeRequest(state))
		api.POST("/ovh/contact-change-requests/:task_id/refuse", handlers.RefuseContactChangeRequest(state))
		api.POST("/ovh/contact-change-requests/:task_id/resend-email", handlers.ResendContactChangeEmail(state))
		api.GET("/ovh/account/sub-accounts", handlers.GetSubAccounts(state))
		api.GET("/ovh/account/bills", handlers.GetAccountBills(state))
	}

	// 前端静态文件（仅 `-tags ui` 构建时生效）
	mountEmbeddedUI(r)

	// 后台线程
	go purchase.ProcessQueueLoop(state)
	// 预热各账户子公司的区域配置:region 的合法取值要从 10MB 的公开目录里解析,
	// 首次解析放在抢购链路上会白白慢 2-7 秒
	go catalog.WarmRegionCache(state)
	// Telegram webhook secret 自愈：老部署注册过的 webhook 不带 secret_token，
	// 启动时用同一 URL 重注册一次，把强校验补上（未配置 TG 时无操作）。
	go telegram.AutoUpgradeWebhookSecret(state)
	// 服务器目录走懒加载：访问到且缓存过期时才打 OVH，无后台定时刷新

	// 自动启动监控（如果有订阅）
	if len(mon.Snapshot()) > 0 {
		mon.Start()
		state.Logger.Info("自动启动服务器监控", "system")
	}

	state.Logger.Info("Server started", "system")
	// 默认监听所有网卡（双栈 IPv4+IPv6），这样 localhost / 127.0.0.1 / 局域网 IP 都能访问。
	// Windows 上 localhost 常先解析到 ::1，单绑 127.0.0.1 会被浏览器拒连。
	// 如果只想锁本机回环，设 LISTEN_HOST=127.0.0.1
	host := os.Getenv("LISTEN_HOST")
	addr := host + ":" + state.Port
	console.Info("Listening", "addr", addr, "auth", enableAuth, "ui", hasUI(), "dataDir", paths.DataDir)

	srv := &http.Server{Addr: addr, Handler: r}

	// 端口真正 Listen 成功之后才标记"这一版能跑" —— 此时数据库已打开、路由已注册、
	// 端口也占上了。太早标记等于没验证:启动就 panic、端口被占、数据库损坏,
	// 恰恰是最需要回滚的几种情况。
	//
	// 自己 Listen 而不是用 ListenAndServe + sleep:后者只能靠"睡几秒应该起来了"猜,
	// 猜早了端口还没占上就宣布健康,猜晚了这几秒里被重启一次就会被误判成启动失败。
	// 拿到 listener 就是确凿的成功信号,没有窗口。
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		console.Error("listen", "err", err)
		os.Exit(1)
	}
	updater.MarkHealthy(state)

	// 自更新完成后走这里:先停止接受新请求并等在途请求收尾,再关数据库,最后换进程映像。
	// 顺序不能反 —— 先 exec 的话,新进程会发现端口还被自己占着。
	gracefulRestart = func(exe string) {
		state.Logger.Info("[更新] 正在优雅关闭以完成重启", "version")
		state.Logger.Flush()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			console.Warn("shutdown", "err", err)
		}
		if err := sqliteDB.Close(); err != nil {
			console.Warn("close sqlite", "err", err)
		}
		if err := updater.Restart(exe); err != nil {
			console.Error("restart", "err", err)
			os.Exit(1)
		}
	}

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		console.Error("server run", "err", err)
		os.Exit(1)
	}
}

// mountEmbeddedUI 把嵌入的前端挂到根路径。
// 没启用 -tags ui 时 hasUI() 为 false，不注册任何 NoRoute；
// 启用时：/api/* 未匹配 → 404 JSON；命中具体文件 → 直接 serve；其余 → 返回 index.html 让 SPA 路由接管。
//
// 注意：index.html 不能交给 http.FileServer 去 serve，否则它会把 /index.html 301 重定向到 /，
// 触发与我们 SPA fallback 的相互重定向死循环（ERR_TOO_MANY_REDIRECTS）。
// 直接读出来缓存到内存，命中 SPA 路径时手工写回，绕开 FileServer 的内部行为。
func mountEmbeddedUI(r *gin.Engine) {
	if !hasUI() {
		return
	}
	distFS := webDistFS()
	indexHTML, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		// 没构出 index.html，等于没 UI；退化为纯 API
		return
	}
	fileServer := http.FileServer(http.FS(distFS))

	serveIndex := func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-cache")
		c.Status(http.StatusOK)
		_, _ = c.Writer.Write(indexHTML)
	}

	r.NoRoute(func(c *gin.Context) {
		reqPath := c.Request.URL.Path
		if strings.HasPrefix(reqPath, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		clean := strings.TrimPrefix(reqPath, "/")
		// 根路径或显式访问 index.html：直接写 index.html，绕开 FileServer 的 301 重定向
		if clean == "" || clean == "index.html" {
			serveIndex(c)
			return
		}
		// 命中具体文件 → FileServer 处理（带正确 Content-Type + 缓存语义）
		if info, err := fs.Stat(distFS, clean); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		// SPA 客户端路由：写 index.html，让前端 router 接管
		serveIndex(c)
	})
}

// availabilityHandler 用 *planCode 通配符处理像 "/api/availability/24sk20-ram-64g" 这样的路径
func availabilityHandler(h gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		pc := c.Param("planCode")
		pc = strings.TrimPrefix(pc, "/")
		c.Params = append(c.Params[:0], gin.Param{Key: "planCode", Value: pc})
		h(c)
	}
}

// envFilePath 配置文件的位置。
// 默认是工作目录下的 .env（godotenv 的默认行为），允许用 OVH_ENV_FILE 覆盖 ——
// systemd / docker 里工作目录未必是程序所在目录，把路径写死会让密钥"下次启动就找不到"。
func envFilePath() string {
	if p := strings.TrimSpace(os.Getenv("OVH_ENV_FILE")); p != "" {
		return p
	}
	return ".env"
}

// isTrue 环境变量的宽松真值判断
func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
