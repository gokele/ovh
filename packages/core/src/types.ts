/**
 * 读侧共享类型:App 与网页渲染所需的字段子集。
 * 后端是权威(字段链检测在 server/internal/schemacheck 守着),这里只声明
 * 我们真正读取的键 —— 新增字段不需要两边同步改,删字段会被后端测试先抓住。
 */

/** 已购独服(server-control/list 返回行) */
export interface OwnedServer {
  serviceName: string;
  name: string;
  commercialRange: string;
  datacenter: string;
  state: string;
  status?: string;
  ip: string;
  os: string;
  orderId?: string | number;
  /** 三态:true=自动续费 / false=确实没开 / null=这次没查到。null 不能当 false 渲染 */
  renewalType?: boolean | null;
  monitoring?: boolean;
  bootId?: number | null;
  /** 救援态:netbootMode === "rescue" 时控制台换救援布局 */
  netbootMode?: string;
}

/** 已购 VPS(vps-control/list 返回行) */
export interface OwnedVps {
  name: string;
  model?: string;
  displayName?: string;
  state: string;
  status?: string;
  zone?: string;
  ips?: string[];
  os?: string;
  netbootMode?: string;
}

/** 抢购队列任务 */
export interface QueueItem {
  id: string;
  accountId: string;
  planCode: string;
  datacenter: string;
  quantity: number;
  status: string; // running / paused / failed / success
  retryInterval: number;
  failureCount?: number;
  maxRetries: number;
  /** 上轮各阶段耗时(毫秒):availability/price/cart/checkout */
  timings?: Record<string, number>;
  lastError?: string;
  createdAt?: string;
}

/** OVH 账户(列表接口返回的是打码版,见后端 sanitizeAccount) */
export interface OvhAccount {
  id: string;
  name: string;
  zone: string;
  endpoint: string;
  valid: boolean;
  isDefault: boolean;
  iam?: string;
}

/** 目录机型(雷达页) */
export interface ServerPlan {
  planCode: string;
  name: string;
  cpu?: string;
  memory?: string;
  storage?: string;
  bandwidth?: string;
  datacenters?: Array<{
    datacenter: string;
    dcName?: string;
    region?: string;
    availability: string;
  }>;
  pricings?: Array<{
    capacities?: string[];
    duration?: string;
    price?: { amount?: number; currencyCode?: string };
  }>;
}

/** 后端 /api/stats 的看板数字 */
export interface Stats {
  queueActive?: number;
  queueTotal?: number;
  historyTotal?: number;
  unpaidCount?: number;
  serversCount?: number;
  availableServers?: number;
}

/** 配对结果(POST /api/app/pair) */
export interface PairResult {
  token: string;
  deviceId: string;
  serverVersion?: string;
}

/** 配对码(网页端生成,GET/POST /api/app/pairing-codes) */
export interface PairingCode {
  code: string;
  expiresAt: string;
}

/** 已配对设备(网页端管理,GET /api/app/devices) */
export interface AppDevice {
  id: number;
  name: string;
  createdAt: string;
  lastUsedAt?: string;
  revokedAt?: string;
}
