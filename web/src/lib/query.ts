import { QueryClient } from "@tanstack/react-query";

/**
 * 统一 QueryClient + 默认缓存策略：
 * - staleTime 30s：列表/统计默认 30 秒后才算过期
 * - gcTime 24h：组件 unmount / 切页面后，query 数据在内存里保留 24 小时
 *   （之前 5 分钟太短，切走 5 分钟回来就要重新拉，跟"走了缓存还是要加载"对得上）
 * - 错误重试 1 次
 * - 窗口聚焦不自动 refetch（控制台型应用，用户不喜欢晃）
 */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      gcTime: 24 * 60 * 60_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

/**
 * 集中管理 query key factory，避免业务代码散写字符串 key。
 * 每个资源域一个对象，按"列表 / 详情 / 子资源"分层。
 */
export const qk = {
  // 概览统计
  stats: () => ["stats"] as const,

  // 服务器列表（含可用性）
  // accountId 进 key:后端 /api/servers 按 ?account= 分桶返回该账户所属子公司的目录
  // (EU/US/CA 三区机型集合不同)，不分 key 会把上一个账户的机型列表留给下一个账户看
  servers: {
    list: (showApiServers: boolean, accountId: string) =>
      ["servers", "list", { showApiServers, accountId }] as const,
    availability: (planCode: string) => ["servers", "availability", planCode] as const,
  },

  // 实时可用性（OVH 公共 API 直查）
  availability: {
    all: (endpoint: string) => ["availability", "all", endpoint] as const,
  },

  // 抢购队列
  queue: {
    list: () => ["queue", "list"] as const,
  },

  // 服务器监控订阅
  monitor: {
    list: () => ["monitor", "list"] as const,
    status: () => ["monitor", "status"] as const,
    history: (planCode: string) => ["monitor", "history", planCode] as const,
  },

  // VPS 补货通知
  vpsMonitor: {
    list: () => ["vps-monitor", "list"] as const,
    status: () => ["vps-monitor", "status"] as const,
    history: (id: string) => ["vps-monitor", "history", id] as const,
  },

  // 服务器控制（已购）
  //
  // 注意 list / contactRequests 必须带 accountId。
  // 账户是在**请求**里注入的(api.ts 的拦截器加 ?account=),但 key 里没有它 ——
  // 于是两个账户共用同一份缓存。切账户时虽然 invalidate 了,invalidate 只是标记过期
  // 并在后台重拉,期间组件读到的 data 仍是上一个账户的:切过去的那几百毫秒里
  // 看到的是别人的服务器列表、别人的 /me 身份信息;重拉一旦失败,就一直停在那儿。
  // 把账户放进 key,切换即缓存未命中 → 走骨架屏,不会串号。
  // (按 serviceName 索引的那些不用带:serviceName 在 OVH 全局唯一。)
  serverControl: {
    list: (accountId: string) => ["server-control", "list", accountId] as const,
    hardware: (serviceName: string) => ["server-control", "hardware", serviceName] as const,
    rescue: (serviceName: string) => ["server-control", "rescue", serviceName] as const,
    serviceInfo: (serviceName: string) => ["server-control", "service-info", serviceName] as const,
    ips: (serviceName: string) => ["server-control", "ips", serviceName] as const,
    interventions: (serviceName: string) => ["server-control", "interventions", serviceName] as const,
    networkInterfaces: (serviceName: string) => ["server-control", "network", serviceName] as const,
    mrtg: (serviceName: string, period: string, type: string) =>
      ["server-control", "mrtg", serviceName, period, type] as const,
    bootModes: (serviceName: string) => ["server-control", "boot-modes", serviceName] as const,
    bios: (serviceName: string) => ["server-control", "bios", serviceName] as const,
    osTemplates: (serviceName: string) => ["server-control", "os-templates", serviceName] as const,
    tasks: (serviceName: string) => ["server-control", "tasks", serviceName] as const,
    diskInfo: (serviceName: string) => ["server-control", "disk-info", serviceName] as const,
    raidProfiles: (serviceName: string) => ["server-control", "raid-profiles", serviceName] as const,
    partitionSchemes: (serviceName: string, templateName: string) =>
      ["server-control", "partition-schemes", serviceName, templateName] as const,
    installStatus: (serviceName: string) => ["server-control", "install-status", serviceName] as const,
    biosSettings: (serviceName: string) => ["server-control", "bios-settings", serviceName] as const,
    biosSgx: (serviceName: string) => ["server-control", "bios-sgx", serviceName] as const,
    monitoring: (serviceName: string) => ["server-control", "monitoring", serviceName] as const,
    burst: (serviceName: string) => ["server-control", "burst", serviceName] as const,
    firewall: (serviceName: string) => ["server-control", "firewall", serviceName] as const,
    backupFtp: (serviceName: string) => ["server-control", "backup-ftp", serviceName] as const,
    backupFtpAccess: (serviceName: string) => ["server-control", "backup-ftp-access", serviceName] as const,
    secondaryDns: (serviceName: string) => ["server-control", "secondary-dns", serviceName] as const,
    virtualMac: (serviceName: string) => ["server-control", "virtual-mac", serviceName] as const,
    vrack: (serviceName: string) => ["server-control", "vrack", serviceName] as const,
    orderable: (serviceName: string) => ["server-control", "orderable", serviceName] as const,
    options: (serviceName: string) => ["server-control", "options", serviceName] as const,
    ipSpecs: (serviceName: string) => ["server-control", "ip-specs", serviceName] as const,
    networkSpecs: (serviceName: string) => ["server-control", "network-specs", serviceName] as const,
    contactRequests: (accountId: string) => ["server-control", "contact-requests", accountId] as const,
    engagement: (serviceName: string) => ["server-control", "engagement", serviceName] as const,
    engagementAvailable: (serviceName: string) => ["server-control", "engagement-available", serviceName] as const,
    engagementRequest: (serviceName: string) => ["server-control", "engagement-request", serviceName] as const,
    mitigation: (serviceName: string) => ["server-control", "mitigation", serviceName] as const,
    taskTimeslots: (serviceName: string, taskId: number, periodStart: string, periodEnd: string) =>
      ["server-control", "task-timeslots", serviceName, taskId, periodStart, periodEnd] as const,
  },

  // VPS 控制(已购 VPS 管理,跟监控库存的 vpsMonitor 不同)
  vpsControl: {
    list: (accountId: string) => ["vps-control", "list", accountId] as const,
    info: (svc: string) => ["vps-control", "info", svc] as const,
    status: (svc: string) => ["vps-control", "status", svc] as const,
    serviceInfo: (svc: string) => ["vps-control", "service-info", svc] as const,
    ips: (svc: string) => ["vps-control", "ips", svc] as const,
    datacenter: (svc: string) => ["vps-control", "datacenter", svc] as const,
    templates: (svc: string) => ["vps-control", "templates", svc] as const,
    currentOS: (svc: string) => ["vps-control", "current-os", svc] as const,
    tasks: (svc: string) => ["vps-control", "tasks", svc] as const,
    task: (svc: string, id: number | string) => ["vps-control", "task", svc, id] as const,
    snapshot: (svc: string) => ["vps-control", "snapshot", svc] as const,
    secondaryDns: (svc: string) => ["vps-control", "secondary-dns", svc] as const,
    options: (svc: string) => ["vps-control", "options", svc] as const,
    automatedBackup: (svc: string) => ["vps-control", "automated-backup", svc] as const,
    engagement: (svc: string) => ["vps-control", "engagement", svc] as const,
    engagementAvailable: (svc: string) => ["vps-control", "engagement-available", svc] as const,
    engagementRequest: (svc: string) => ["vps-control", "engagement-request", svc] as const,
    mitigation: (svc: string) => ["vps-control", "mitigation", svc] as const,
  },

  // 账户。这三个是"当前账户是谁 / 它的退款和邮件",同上必须带 accountId ——
  // 不带的话切账户的瞬间会把上一个账户的身份、KYC 状态、退款记录显示给下一个账户。
  account: {
    info: (accountId: string) => ["account", "info", accountId] as const,
    refunds: (accountId: string) => ["account", "refunds", accountId] as const,
    emails: (accountId: string) => ["account", "emails", accountId] as const,
  },

  // 账户的出站代理:健康状况 + 最近一次出口 IP 测试。
  //
  // proxyStatus 是真的去问后端(30 秒一轮),proxyTest 不是 —— 它没有对应的 GET,
  // 只是「测试出口 IP」那一下把结果塞进缓存的存放处(见 use-accounts 的 useProxyTest)。
  // 走 query 缓存而不是组件 state,是因为测试在编辑框里点、结果要显示在账户列表上:
  // 多个账户的出口 IP 并排比对是确认隔离生效的唯一手段,两个账户同一个 IP 就等于没生效。
  accounts: {
    proxyStatus: () => ["accounts", "proxy-status"] as const,
    proxyTest: (accountId: string) => ["accounts", "proxy-test", accountId] as const,
    // 链路检测同理,也没有对应的 GET,只是「链路检测」那一下把结果存起来的地方。
    // 它比出口 IP 测试贵得多(每个目标真打 3 次),所以结果必须留住:
    // 关掉弹窗再打开看到的是上次那份 + 那次的时间,而不是又去打一轮 OVH。
    proxyCheck: (accountId: string) => ["accounts", "proxy-check", accountId] as const,
  },

  // 历史与日志
  history: () => ["history"] as const,
  logs: () => ["logs"] as const,

  // 设置
  settings: {
    config: () => ["settings", "config"] as const,
    cacheInfo: () => ["settings", "cache-info"] as const,
    telegramPoller: () => ["settings", "telegram-poller"] as const,
  },
} as const;
