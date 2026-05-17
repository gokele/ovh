package main

import (
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/auth"
	"github.com/ovh-buy/server/internal/config"
	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/handlers"
	"github.com/ovh-buy/server/internal/logger"
	"github.com/ovh-buy/server/internal/monitor"
	"github.com/ovh-buy/server/internal/purchase"
	"github.com/ovh-buy/server/internal/sniper"
	"github.com/ovh-buy/server/internal/storage"
)

func main() {
	_ = godotenv.Load()

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

	sqliteDB, err := db.Open(paths.DataDir)
	if err != nil {
		console.Error("open sqlite", "err", err)
		os.Exit(1)
	}
	defer sqliteDB.Close()

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

	// 监控器
	mon := monitor.New(state)
	mon.LoadFromDB()
	mon.SetCheckInterval(5)
	mon.SaveToDB()
	console.Info("监控检查间隔已强制设置为: 5秒（全局固定值）")

	// Gin
	if mode := os.Getenv("GIN_MODE"); mode != "" {
		gin.SetMode(mode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(cors.New(cors.Config{
		AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-API-Key", "X-Request-Time"},
		ExposeHeaders:    []string{"X-Cache-Warning"},
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
		api.POST("/monitor/subscriptions/batch-add-all", handlers.BatchAddAll(state, mon))
		api.DELETE("/monitor/subscriptions/clear", handlers.ClearSubscriptions(state, mon))
		api.DELETE("/monitor/subscriptions/:planCode", handlers.RemoveSubscription(state, mon))
		api.GET("/monitor/subscriptions/:planCode/history", handlers.GetSubscriptionHistory(state, mon))
		api.POST("/monitor/start", handlers.StartMonitor(state, mon))
		api.POST("/monitor/stop", handlers.StopMonitor(state, mon))
		api.GET("/monitor/status", handlers.GetMonitorStatus(state, mon))
		api.PUT("/monitor/interval", handlers.SetMonitorInterval(state, mon))
		api.POST("/monitor/test-notification", handlers.TestNotification(state))

		// Telegram
		api.POST("/telegram/set-webhook", handlers.SetTelegramWebhook(state))
		api.GET("/telegram/get-webhook-info", handlers.GetTelegramWebhookInfo(state))
		api.POST("/telegram/webhook", handlers.TelegramWebhook(state, mon))

		// Servers / availability / cache
		api.GET("/servers", handlers.GetServers(state))
		api.GET("/availability/*planCode", availabilityHandler(handlers.GetAvailability(state)))
		api.POST("/availability/*planCode", availabilityHandler(handlers.GetAvailability(state)))
		api.POST("/internal/monitor/price", handlers.MonitorPrice(state))
		api.GET("/cache/info", handlers.CacheInfo(state))
		api.POST("/cache/clear", handlers.ClearCache(state))
		api.GET("/catalog", handlers.GetCatalog(state))
		api.GET("/system/metrics", handlers.GetSystemMetrics(state))
		api.GET("/version", handlers.GetVersion(state))

		// Config sniper
		api.GET("/config-sniper/options/:planCode", handlers.GetConfigOptions(state))
		api.GET("/config-sniper/tasks", handlers.GetConfigSniperTasks(state))
		api.POST("/config-sniper/tasks", handlers.CreateConfigSniperTask(state))
		api.DELETE("/config-sniper/tasks/:task_id", handlers.DeleteConfigSniperTask(state))
		api.PUT("/config-sniper/tasks/:task_id/toggle", handlers.ToggleConfigSniperTask(state))
		api.POST("/config-sniper/tasks/:task_id/check", handlers.CheckConfigSniperTask(state))
		api.POST("/config-sniper/quick-order", handlers.QuickOrder(state))

		// Server control - basic
		sc := api.Group("/server-control")
		{
			sc.GET("/list", handlers.ListMyServers(state))
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
			sc.GET("/:service_name/serviceinfo", handlers.GetServiceInfo(state))
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
			sc.DELETE("/:service_name/backup-ftp/access/:ip_block", handlers.DeleteBackupFTPAccess(state))
			sc.POST("/:service_name/backup-ftp/password", handlers.ChangeBackupFTPPassword(state))
			sc.GET("/:service_name/backup-ftp/authorizable-blocks", handlers.GetBackupFTPAuthorizableBlocks(state))
			sc.GET("/:service_name/backup-cloud", handlers.GetBackupCloud(state))
			sc.GET("/:service_name/backup-cloud/offer-details", handlers.GetBackupCloudOfferDetails(state))

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
			sc.POST("/:service_name/terminate", handlers.TerminateService(state))
			sc.POST("/:service_name/confirm-termination", handlers.ConfirmTermination(state))
			sc.GET("/:service_name/spla", handlers.GetSPLAList(state))
			sc.POST("/:service_name/spla", handlers.CreateSPLA(state))
			sc.GET("/:service_name/bios-settings", handlers.GetBIOSSettings(state))
			sc.GET("/:service_name/bios-settings/sgx", handlers.GetBIOSSettingsSGX(state))
		}

		// VPS monitor
		api.GET("/vps-monitor/subscriptions", handlers.GetVPSSubscriptions(state))
		api.POST("/vps-monitor/subscriptions", handlers.AddVPSSubscription(state))
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
	go sniper.MonitorLoop(state)
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
	if err := r.Run(addr); err != nil {
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
