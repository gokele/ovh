import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import type { PartialList } from "./partial-list";

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
  reverse?: string;
  monitoring?: boolean;
  professionalUse?: boolean;
  bootId?: number | null;
  /**
   * 列表接口里的自动续费状态。
   * true=已开自动续费 / false=确实没开 / null|undefined=这次没查到。
   * 三态必须分开渲染：把 null 当成 false 显示成「手动」会让用户以为自己已经关过了，
   * 实际上机器可能正在自动续费（或反过来漏续费），这正是后端改用 *bool 的原因。
   */
  renewalType?: boolean | null;
  /** 有值表示这台机器的 serviceInfos 没拉到（renewalType/status 因此不可信），组件应提示可重试 */
  svcInfoError?: string;
  /** 有值表示这台机器连详情都没拉到，除 serviceName/name 外其它字段都缺 */
  error?: string;
}

export interface HardwareInfo {
  processorName: string;
  processorArchitecture: string;
  coresPerProcessor: number;
  threadsPerProcessor: number;
  memorySize?: { value: number; unit: string };
  diskGroups?: any[];
  expansionCards?: any[];
}

export interface ServiceInfo {
  status: string;
  expiration: string;
  creation: string;
  /**
   * 是否启用自动续费(后端解析 OVH renew.automatic)。
   * 这里是单台机器的 /serviceinfo 接口，后端拿不到 renew 就整个请求报错，
   * 不会像列表接口那样出现 null，所以保持 boolean —— 别跟 OwnedServer.renewalType 混淆。
   */
  renewalType: boolean;
  /** 续费周期,单位月(1 / 3 / 6 / 12 等) */
  renewalPeriod: number;
  /** 到期是否自动删除服务 —— true 等于"到期断网回收" */
  renewalDeleteAtExpiration: boolean;
  /** OVH 是否强制自动续费(部分付费服务) */
  renewalForced: boolean;
  /** 是否要求手动支付(true 时余额扣款会跳过,需用户手动付) */
  renewalManualPayment: boolean;
  /** OVH 允许的续费周期列表(月数),前端 select 选项用 */
  possibleRenewPeriod: number[];
}

/**
 * 已购服务器列表（后端返回 { success, servers, total }）
 * 过滤逻辑照搬旧前端：只显示 state === 'ok' | 'active'，排除 expired / suspended / error
 */
export function useOwnedServers() {
  return useQuery({
    queryKey: qk.serverControl.list(),
    queryFn: async () => {
      const res = await api.get("/server-control/list");
      const raw = (res.data?.servers || []) as OwnedServer[];
      return raw.filter((s) => {
        const state = s.state?.toLowerCase();
        const status = s.status?.toLowerCase();
        if (status === "expired" || status === "suspended") return false;
        if (state === "error" || state === "suspended") return false;
        return state === "ok" || state === "active";
      });
    },
    staleTime: 60_000,
  });
}

/** 硬件信息（后端返回 { success, hardware: {...} }） */
export function useServerHardware(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.hardware(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/hardware`);
      return (res.data?.hardware || null) as HardwareInfo | null;
    },
    enabled: !!serviceName,
  });
}


/** 服务信息（后端返回 { success, serviceInfo: {...} }） */
export function useServerServiceInfo(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.serviceInfo(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/serviceinfo`);
      return (res.data?.serviceInfo || null) as ServiceInfo | null;
    },
    enabled: !!serviceName,
  });
}

/**
 * 修改续费策略。OVH 这个 endpoint 需要 PUT 整个 service 对象,
 * 后端代为 GET + merge + PUT,前端只传 mode + 可选 period。
 */
export function useUpdateRenewal(serviceName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { mode: "auto" | "manual" | "delete"; period?: number }) => {
      const res = await api.put(`/server-control/${serviceName}/serviceinfo/renewal`, vars);
      return res.data;
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.serverControl.serviceInfo(serviceName) });
    },
  });
}

/* ──────────── 服务终止(到期注销) ──────────── */

/**
 * 请求终止服务。OVH 会把确认 token 发到账户管理员邮箱，拿到后再调 confirm。
 *
 * 为什么「到期注销」不走 serviceInfos 的 renew 字段：那条路 OVH 会回
 * 400 "Arguments conflicting"，而且 OVH 自己的 issue 里记录着这组标志位
 * 行为不可预测（同一份 payload 发两次会在自动/手动之间来回跳）。
 * 终止是有专用端点的（POST /terminate + POST /confirmTermination），
 * 也是 OVH 控制台「Terminate my service」走的那条 —— 效果就是
 * 「取消续费，服务保留到当期结束后销毁」，正是这里要的语义。
 */
export function useTerminateService(serviceName: string) {
  return useMutation({
    mutationFn: async () =>
      (await api.post(`/server-control/${serviceName}/terminate`)).data as {
        success: boolean;
        message: string;
      },
  });
}

/** 用邮件里的 token 确认终止 */
export function useConfirmTermination(serviceName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { token: string; reason?: string; commentary?: string }) =>
      (await api.post(`/server-control/${serviceName}/confirm-termination`, vars)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.serverControl.serviceInfo(serviceName) });
    },
  });
}

/* ──────────── Engagement(合同期切换) ──────────── */

export interface EngagementPricing {
  pricingMode: string;
  description: string;
  duration: string;
  interval: number;
  price: { value: number; currencyCode?: string; text?: string };
  priceInUcents?: number;
  engagementConfiguration?: {
    defaultEndAction: string;
    duration: string;
    type: string;
  };
}

export interface EngagementInfo {
  currentPeriod?: { startDate: string; endDate: string };
  endRule?: { strategy: string; possibleStrategies: string[] };
}

export interface EngagementRequest {
  pricing?: EngagementPricing;
  requestDate?: string;
  order?: { orderId?: number; url?: string };
}

/** 当前 engagement(无合同期返回 null) */
export function useEngagement(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.engagement(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/engagement`);
      return (res.data?.engagement || null) as EngagementInfo | null;
    },
    enabled: !!serviceName,
  });
}

/** 可订购的 engagement 选项列表 */
export function useEngagementAvailable(serviceName: string | null, enabled = true) {
  return useQuery({
    queryKey: qk.serverControl.engagementAvailable(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/engagement/available`);
      return (res.data?.pricings || []) as EngagementPricing[];
    },
    enabled: !!serviceName && enabled,
  });
}

/** 进行中的 engagement 变更请求 */
export function useEngagementRequest(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.engagementRequest(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/engagement/request`);
      return (res.data?.request || null) as EngagementRequest | null;
    },
    enabled: !!serviceName,
  });
}

/** 提交新的 engagement 请求 */
export function useCreateEngagementRequest(serviceName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { pricingMode: string }) => {
      const res = await api.post(`/server-control/${serviceName}/engagement/request`, vars);
      return res.data;
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.serverControl.engagement(serviceName) });
      qc.invalidateQueries({ queryKey: qk.serverControl.engagementRequest(serviceName) });
    },
  });
}

/** 撤销进行中的 engagement 请求 */
export function useDeleteEngagementRequest(serviceName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const res = await api.delete(`/server-control/${serviceName}/engagement/request`);
      return res.data;
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.serverControl.engagementRequest(serviceName) });
    },
  });
}

/** services.billing.engagement.EndStrategyEnum，后端也按这份白名单校验 */
export type EngagementEndStrategy =
  | "CANCEL_SERVICE"
  | "REACTIVATE_ENGAGEMENT"
  | "STOP_ENGAGEMENT_FALLBACK_DEFAULT_PRICE"
  | "STOP_ENGAGEMENT_KEEP_PRICE";

/**
 * 改 engagement 到期策略。
 * CANCEL_SERVICE = 承诺期结束后直接销毁服务器，不可撤销，所以后端要求带 confirm:true；
 * 组件必须先弹二次确认框拿到用户明确同意，再传 confirm。其余三个策略不需要 confirm。
 */
export function useUpdateEngagementEndRule(serviceName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { strategy: EngagementEndStrategy | string; confirm?: boolean }) => {
      const res = await api.put(`/server-control/${serviceName}/engagement/end-rule`, vars);
      return res.data;
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.serverControl.engagement(serviceName) });
    },
  });
}

/* ──────────── DDoS Mitigation ──────────── */

export interface MitigationIp {
  ipOnMitigation: string;
  state: string; // activated / pending / disabled / ...
  auto: boolean;
  permanent: boolean;
}

export interface MitigationBlock {
  ipBlock: string;
  mitigations: MitigationIp[];
  error?: string;
}

export function useMitigation(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.mitigation(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/mitigation`);
      return (res.data?.ips || []) as MitigationBlock[];
    },
    enabled: !!serviceName,
    // 过渡态自动轮询(creationPending/removalPending → ok 通常 30 秒-2 分钟)
    refetchInterval: (q) => {
      const data = q.state.data as MitigationBlock[] | undefined;
      if (!data) return false;
      const hasTransition = data.some((b) =>
        b.mitigations.some((m: any) => m.state === "creationPending" || m.state === "removalPending"),
      );
      return hasTransition ? 5000 : false;
    },
  });
}

export function useEnableMitigation(serviceName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { ip: string; block: string }) => {
      const res = await api.post(`/server-control/${serviceName}/mitigation/${vars.ip}?block=${encodeURIComponent(vars.block)}`);
      return res.data;
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.serverControl.mitigation(serviceName) });
    },
  });
}

export function useDisableMitigation(serviceName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { ip: string; block: string }) => {
      const res = await api.delete(`/server-control/${serviceName}/mitigation/${vars.ip}?block=${encodeURIComponent(vars.block)}`);
      return res.data;
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.serverControl.mitigation(serviceName) });
    },
  });
}

/** IP 列表（后端返回 { success, ips: [{ ip, type, ... }] }） */
export function useServerIps(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.ips(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/ips`);
      return (res.data?.ips || []) as Array<{ ip: string; type: string }>;
    },
    enabled: !!serviceName,
  });
}

/** 维护记录（后端返回 { success, interventions: [...] }） */
export function useServerInterventions(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.interventions(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/interventions`);
      return (res.data?.interventions || []) as any[];
    },
    enabled: !!serviceName,
  });
}

/**
 * 后端「主键列表 + 并发拉详情」类接口的统一读法：
 * 详情拉挂的行仍会返回，只是带 _detailError；partial/failedCount 说明这次少了几行的详情。
 */
function readPartialList<T>(data: any, listKey: string): PartialList<T> {
  const failedCount = Number(data?.failedCount) || 0;
  return {
    items: (data?.[listKey] || []) as T[],
    partial: data?.partial === true || failedCount > 0,
    failedCount,
  };
}

/** 详情拉取失败的行会带上这个字段，组件应给该行加「获取失败」标记 */
export interface DetailErrorMarked {
  _detailError?: string;
}

export interface NetworkInterface extends DetailErrorMarked {
  mac: string;
  linkType?: string;
}

/** 网络接口（后端返回 { success, interfaces, partial, failedCount }） */
export function useServerNetworkInterfaces(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.networkInterfaces(serviceName || ""),
    queryFn: async (): Promise<PartialList<NetworkInterface>> => {
      const res = await api.get(`/server-control/${serviceName}/network-interfaces`);
      return readPartialList<NetworkInterface>(res.data, "interfaces");
    },
    enabled: !!serviceName,
  });
}

export interface BootMode {
  id: number;
  bootType: string;
  description: string;
  kernel: string;
  active: boolean;
  /**
   * 有值表示这一项的详情没拉到（bootType/description/kernel 是占位值）。
   * 后端保留占位而不是丢条目，就是为了别让启动模式凭空少几个；
   * 组件应把这一行标成「获取失败，可重试」并禁止直接切换过去。
   * 后端另有 failed 计数，等于带 error 的行数。
   */
  error?: string;
}

/** 启动模式（后端返回 { success, bootModes: [...] }） */
export function useServerBootModes(serviceName: string | null, enabled = true) {
  return useQuery({
    queryKey: qk.serverControl.bootModes(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/boot-mode`);
      return (res.data?.bootModes || []) as BootMode[];
    },
    enabled: !!serviceName && enabled,
  });
}

/** 切换启动模式（旧前端会随后自动调 reboot；这里把 reboot 留给调用方决定） */
export function useSetServerBootMode() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ serviceName, bootId }: { serviceName: string; bootId: number }) => {
      const res = await api.put(`/server-control/${serviceName}/boot-mode`, { bootId });
      return res.data;
    },
    onSuccess: (_, vars) => {
      qc.invalidateQueries({ queryKey: qk.serverControl.bootModes(vars.serviceName) });
    },
  });
}

export interface ServerTask {
  taskId: number;
  function: string;
  status: string;
  startDate: string;
  doneDate: string;
  comment?: string;
  /**
   * 有值表示这条任务的详情没拉到（function/status 是占位值 N/A / unknown）。
   * 不显示的话用户会以为任务真的处于 unknown 状态，从而重复提交同一个操作。
   * 后端另有 failed 计数，等于带 error 的行数。
   */
  error?: string;
}

/** 服务器运维任务列表（后端返回 { success, tasks: [...] }） */
export function useServerTasks(serviceName: string | null, enabled = true) {
  return useQuery({
    queryKey: qk.serverControl.tasks(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/tasks`);
      return (res.data?.tasks || []) as ServerTask[];
    },
    enabled: !!serviceName && enabled,
  });
}

export interface OSTemplate {
  templateName: string;
  distribution: string;
  family: string;
  bitFormat: number;
}

/**
 * OS 模板列表（每台机器可用模板不同）（后端返回 { success, templates: [...] }）
 * 长期缓存：localStorage 持久化 + staleTime/gcTime 永不过期，
 * 只有用户点"刷新"才会重新拉取（dialog 里手动 refetch）。
 */
const TEMPLATES_LS_PREFIX = "ovh_sniper_templates_";
export function useServerTemplates(serviceName: string | null, enabled = true) {
  const lsKey = serviceName ? TEMPLATES_LS_PREFIX + serviceName : "";
  return useQuery({
    queryKey: qk.serverControl.osTemplates(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/templates`);
      const list = (res.data?.templates || []) as OSTemplate[];
      if (lsKey) {
        try {
          localStorage.setItem(lsKey, JSON.stringify(list));
          localStorage.setItem(lsKey + "_at", String(Date.now()));
        } catch { /* 配额满或隐私模式 */ }
      }
      return list;
    },
    initialData: () => {
      if (!lsKey) return undefined;
      try {
        const raw = localStorage.getItem(lsKey);
        return raw ? (JSON.parse(raw) as OSTemplate[]) : undefined;
      } catch {
        return undefined;
      }
    },
    initialDataUpdatedAt: () => {
      if (!lsKey) return undefined;
      try {
        const at = localStorage.getItem(lsKey + "_at");
        return at ? Number(at) : undefined;
      } catch {
        return undefined;
      }
    },
    enabled: !!serviceName && enabled,
    staleTime: Infinity,
    gcTime: Infinity,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
}

/** 硬件磁盘组（用于自定义 RAID / 分区）（后端返回 { success, diskGroups: { [id]: {...} } }） */
export interface DiskGroupDisk {
  number: number;
  capacity: number;
  unit: string;
  technology?: string;
  interface?: string;
}
export interface DiskGroup {
  raidController?: string;
  disks: DiskGroupDisk[];
}

export function useServerDiskInfo(serviceName: string | null, enabled = true) {
  return useQuery({
    queryKey: qk.serverControl.diskInfo(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/hardware-disk-info`);
      return (res.data?.diskGroups || {}) as Record<string, DiskGroup>;
    },
    enabled: !!serviceName && enabled,
    staleTime: 5 * 60_000,
  });
}

/** 硬件 RAID 支持情况（后端返回 { success, supported, profiles }） */
export function useServerRaidProfiles(serviceName: string | null, enabled = true) {
  return useQuery({
    queryKey: qk.serverControl.raidProfiles(serviceName || ""),
    queryFn: async () => {
      try {
        const res = await api.get(`/server-control/${serviceName}/hardware-raid-profiles`);
        return {
          supported: res.data?.supported !== false,
          profiles: (res.data?.profiles || []) as any[],
        };
      } catch {
        return { supported: false, profiles: [] as any[] };
      }
    },
    enabled: !!serviceName && enabled,
    staleTime: 5 * 60_000,
  });
}

/** 分区方案列表（每个模板的内置方案）（后端返回 { success, schemes: [...] }） */
export interface PartitionScheme {
  name: string;
  priority: number;
}
export function useServerPartitionSchemes(serviceName: string | null, templateName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.partitionSchemes(serviceName || "", templateName || ""),
    queryFn: async () => {
      const res = await api.get(
        `/server-control/${serviceName}/partition-schemes?templateName=${encodeURIComponent(templateName || "")}`
      );
      return (res.data?.schemes || []) as PartitionScheme[];
    },
    enabled: !!serviceName && !!templateName,
    staleTime: 5 * 60_000,
  });
}

/** 自定义分区一行（前端模型，提交时再转 OVH layout） */
export interface CustomPartition {
  mountpoint: string;
  filesystem: string;
  size: number; // MB，0 表示剩余
  order: number;
  type: string;
  raid?: string; // raid0/raid1/...
  diskGroupId?: number;
}

/** 重装系统：完整版（template / hostname / Proxmox ZFS / 硬件 RAID / 软 RAID / 自定义分区 / 内置分区方案） */
export interface ReinstallArgs {
  serviceName: string;
  templateName: string;
  customHostname?: string;
  // Proxmox 9 + ZFS（仅当 templateName === 'proxmox9_64' 时）
  useProxmox9Zfs?: boolean;
  zfsRaidLevel?: 0 | 1;
  zfsVzSize?: number; // MB
  // 老路径：选择某个内置分区方案
  partitionSchemeName?: string;
  // 新路径：自定义存储（硬件 RAID + 软 RAID + 分区）
  hardwareRaid?: Record<number, string>; // diskGroupId → raidLevel (raid0/...)
  softwareRaidLevel?: string; // 仅当 useSoftwareRaid 为 true 时
  useSoftwareRaid?: boolean;
  customPartitions?: CustomPartition[];
  diskGroups?: Record<string, DiskGroup>; // 用于硬件 RAID 时拼 disks 列表
}

export interface ReinstallResult {
  success: boolean;
  message?: string;
  taskId?: number;
  /**
   * 后端忽略了哪份存储配置的说明（例如同时勾了 Proxmox 9 + ZFS 和高级存储配置时，
   * ZFS 预设优先、另一份被忽略）。装是能装成的，但用户得知道自己填的东西没生效，
   * 组件应该用 toast.warning 逐条显示。
   */
  warnings?: string[];
}

export function useReinstallServer() {
  return useMutation({
    mutationFn: async (args: ReinstallArgs) => {
      const installData: any = {
        templateName: args.templateName,
        customHostname: args.customHostname || undefined,
        useProxmox9Zfs: !!args.useProxmox9Zfs,
        zfsRaidLevel: args.useProxmox9Zfs ? args.zfsRaidLevel : undefined,
        zfsVzSize: args.useProxmox9Zfs ? args.zfsVzSize : undefined,
      };

      const useCustom = !!(
        args.useSoftwareRaid ||
        (args.hardwareRaid && Object.values(args.hardwareRaid).some((v) => !!v)) ||
        (args.customPartitions && args.customPartitions.length > 0)
      );

      if (useCustom) {
        // 按 diskGroupId 分组
        const groups = new Map<string, any>();
        let partitions = args.customPartitions || [];
        // 启用软 RAID 但未自定义分区 → 默认根分区软 RAID
        if (args.useSoftwareRaid && partitions.length === 0) {
          partitions = [
            {
              mountpoint: "/",
              filesystem: "ext4",
              size: 0,
              order: 1,
              type: "primary",
              raid: args.softwareRaidLevel || "raid1",
            },
          ];
        }
        // 磁盘组编号从 1 起（官方分区文档：默认装在 diskGroupId 1 上），
        // 而且文档写明「the API only supports OS installation and storage
        // customisation on 1 single disk group」—— 所以没显式选组时用 0 当键
        // 会造出一个不存在的组，还会把分区和硬件 RAID 拆成两个 storage 条目，
        // 变成「在 0 组上分区、在 1 组上做 RAID」这种 OVH 不接受的配置。
        // 用 undefined 当键表示「没选，交给 OVH 用默认组」，发送时也不带这个字段。
        const DEFAULT_GID = "default";
        const gidKey = (v?: number) => (v && v > 0 ? String(v) : DEFAULT_GID);
        partitions.forEach((p) => {
          const gid = gidKey(p.diskGroupId);
          if (!groups.has(gid)) {
            const entry: any = { partitioning: { layout: [] } };
            if (gid !== DEFAULT_GID) entry.diskGroupId = Number(gid);
            groups.set(gid, entry);
          }
          const g = groups.get(gid);
          const ovhP: any = { mountPoint: p.mountpoint, fileSystem: p.filesystem, size: p.size || 0 };
          if (p.raid) {
            const m = p.raid.match(/raid(\d+)/);
            if (m) ovhP.raidLevel = parseInt(m[1]);
          }
          g.partitioning.layout.push(ovhP);
        });
        // 硬件 RAID。
        // schema dedicated.server.reinstall.storage.HardwareRaid 只有 arrays / disks(long) / raidLevel / spares：
        // disks 是「参与阵列的磁盘数量」而不是磁盘编号数组，mode / name / step 是旧 partitionScheme 的字段，
        // 不在 schema 里。后端虽然做了兼容映射，但继续发旧字段会掩盖前端和 schema 的偏差，所以这里直接按 schema 发。
        // 副作用：OVH 不接受「指定用哪几块盘」，只能给数量，磁盘选择交给 OVH。
        if (args.hardwareRaid) {
          Object.entries(args.hardwareRaid).forEach(([gidStr, raidMode]) => {
            if (!raidMode) return;
            const parsed = parseInt(gidStr);
            // 只有一个磁盘组时，硬件 RAID 和分区必须落在同一个 storage 条目里，
            // 否则就成了「两个组各配一半」。没选组时两边都用 DEFAULT_GID 这个键。
            const gid = groups.size === 1 && groups.has(DEFAULT_GID) ? DEFAULT_GID : gidKey(parsed);
            if (!groups.has(gid)) {
              const entry: any = {};
              if (gid !== DEFAULT_GID) entry.diskGroupId = Number(gid);
              groups.set(gid, entry);
            }
            const g = groups.get(gid);
            if (!g.hardwareRaid) g.hardwareRaid = [];
            const level = parseInt(raidMode.replace("raid", ""), 10);
            if (Number.isNaN(level)) return;
            const diskCount = args.diskGroups?.[gidStr]?.disks?.length || 0;
            const item: { raidLevel: number; disks?: number } = { raidLevel: level };
            // 0 表示「不知道这组有几块盘」，省略让 OVH 用默认值，别发 disks:0
            if (diskCount > 0) item.disks = diskCount;
            g.hardwareRaid.push(item);
          });
        }
        const storageArray = Array.from(groups.values());
        if (storageArray.length > 0) installData.storageConfig = storageArray;
      } else if (args.partitionSchemeName) {
        installData.partitionSchemeName = args.partitionSchemeName;
      }

      const res = await api.post(`/server-control/${args.serviceName}/install`, installData);
      return res.data as ReinstallResult;
    },
  });
}

export interface InstallStep {
  /** 已翻译成中文的步骤名 */
  comment: string;
  /** OVH 原文，翻译表没覆盖到时可回退显示 */
  commentOriginal: string;
  status: string; // todo / doing / done / error
  error: string;
}

export interface InstallStatus {
  elapsedTime: number;
  progressPercentage: number;
  totalSteps: number;
  completedSteps: number;
  hasError: boolean;
  allDone: boolean;
  /**
   * true = OVH 这次没返回 progress（schema 里它可空）。
   * 此时 progressPercentage 恒为 0、allDone 恒为 false，但那不代表「一步都没做」。
   * 组件必须显示「进度暂不可用」，否则用户会盯着一个永远停在 0% 的进度条。
   */
  progressUnknown: boolean;
  steps: InstallStep[];
}

/** 安装进度（前端轮询用，旧前端每 5s 轮一次）（后端返回 { success, hasInstallation, status: {...} }） */
export function useInstallStatus(serviceName: string | null, enabled = true) {
  return useQuery({
    queryKey: qk.serverControl.installStatus(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/install/status`);
      return {
        hasInstallation: res.data?.hasInstallation !== false,
        status: (res.data?.status || null) as InstallStatus | null,
      };
    },
    enabled: !!serviceName && enabled,
    refetchInterval: enabled ? 5_000 : false,
    staleTime: 0,
  });
}

// ───────────────────────────────── BIOS / Monitoring ─────────────────────────────────

/** BIOS 设置（response.data 即结果对象） */
export function useServerBiosSettings(serviceName: string | null, enabled = true) {
  return useQuery({
    queryKey: qk.serverControl.biosSettings(serviceName || ""),
    queryFn: async () => {
      try {
        const res = await api.get(`/server-control/${serviceName}/bios-settings`);
        const sgxRes = await api.get(`/server-control/${serviceName}/bios-settings/sgx`).catch(() => null);
        return {
          settings: res.data || {},
          sgx: sgxRes?.data?.sgx ?? sgxRes?.data?.data ?? sgxRes?.data ?? null,
        };
      } catch {
        return { settings: {}, sgx: null };
      }
    },
    enabled: !!serviceName && enabled,
  });
}

/** OVH 监控开关（res.data.monitoring → boolean） */
export function useServerMonitoring(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.monitoring(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/monitoring`);
      return !!res.data?.monitoring;
    },
    enabled: !!serviceName,
  });
}

export function useToggleMonitoring() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ serviceName, enabled }: { serviceName: string; enabled: boolean }) => {
      const res = await api.put(`/server-control/${serviceName}/monitoring`, { monitoring: enabled });
      return res.data;
    },
    onSuccess: (_, vars) => {
      qc.invalidateQueries({ queryKey: qk.serverControl.monitoring(vars.serviceName) });
    },
  });
}

// ───────────────────────────────── Burst / Firewall ─────────────────────────────────

/** Burst：res.data.burst（结构含 status / capacity 等）；某些服务器不支持，会返回 404 */
export function useServerBurst(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.burst(serviceName || ""),
    queryFn: async () => {
      try {
        const res = await api.get(`/server-control/${serviceName}/burst`);
        return { burst: res.data?.burst || null, notAvailable: false } as any;
      } catch (e: any) {
        if (e?.response?.status === 404) {
          return { burst: null, notAvailable: true, error: e?.response?.data?.error } as any;
        }
        throw e;
      }
    },
    enabled: !!serviceName,
    retry: false,
  });
}

export function useSetBurst() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ serviceName, status }: { serviceName: string; status: string }) => {
      const res = await api.put(`/server-control/${serviceName}/burst`, { status });
      return res.data;
    },
    onSuccess: (_, vars) => {
      qc.invalidateQueries({ queryKey: qk.serverControl.burst(vars.serviceName) });
    },
  });
}

/** 防火墙：res.data.firewall（结构含 state / mode / model 等）；某些服务器不支持，会返回 404 */
export function useServerFirewall(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.firewall(serviceName || ""),
    queryFn: async () => {
      try {
        const res = await api.get(`/server-control/${serviceName}/firewall`);
        return { firewall: res.data?.firewall || null, notAvailable: false } as any;
      } catch (e: any) {
        if (e?.response?.status === 404) {
          return { firewall: null, notAvailable: true, error: e?.response?.data?.error } as any;
        }
        throw e;
      }
    },
    enabled: !!serviceName,
    retry: false,
  });
}

export function useSetFirewall() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ serviceName, enabled }: { serviceName: string; enabled: boolean }) => {
      const res = await api.put(`/server-control/${serviceName}/firewall`, { enabled });
      return res.data;
    },
    onSuccess: (_, vars) => {
      qc.invalidateQueries({ queryKey: qk.serverControl.firewall(vars.serviceName) });
    },
  });
}

// ───────────────────────────────── Backup FTP ─────────────────────────────────

/** ACL 一行。详情没拉到时只有 ipBlock + error */
export interface BackupFtpAccessEntry {
  ipBlock: string;
  ftp?: boolean;
  nfs?: boolean;
  cifs?: boolean;
  isApplied?: boolean;
  error?: string;
}

export interface BackupFtpResult {
  backupFtp?: Record<string, any> | null;
  accessList?: BackupFtpAccessEntry[];
  /** ACL 详情拉取失败的条数（后端 failedCount） */
  accessFailedCount?: number;
  /** ACL 列表整体拉取失败时的原因（不影响主信息展示） */
  accessError?: string;
  /** 该区域/该机器没有备份FTP能力（US 区，或 OVH 说 cannot benefit） */
  notAvailable?: boolean;
  /** 功能存在但未激活 —— 只有这一种情况才该显示「激活」按钮 */
  notActivated?: boolean;
  /** 服务器不存在或不属于当前账户：显示 error/reason，不要给激活按钮 */
  unknownService?: boolean;
  error?: string;
  /** OVH 原文，用于排查 */
  reason?: string;
}

/** Backup FTP：可能 notAvailable / notActivated / unknownService / 正常对象 */
export function useServerBackupFtp(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.backupFtp(serviceName || ""),
    queryFn: async (): Promise<BackupFtpResult> => {
      try {
        const res = await api.get(`/server-control/${serviceName}/backup-ftp`);
        // 后端用 200 + success:false 表达「US 区没这功能」和「服务器不属于本账户」
        // ——这两种都不是 404「未激活」，必须把原因原样带出去，否则会渲染成一个点了必失败的激活按钮
        if (res.data?.success === false) {
          return {
            // notAvailable 一律置 true：现网组件就是靠它走「不可用 + 显示 error」分支的，
            // unknownService 只是额外的细分标记（组件可据此换成「服务器不属于当前账户」的文案）
            notAvailable: true,
            unknownService: res.data?.unknownService === true,
            error: res.data?.error,
            reason: res.data?.reason,
          };
        }
        // 尝试同时取 access 列表
        let accessList: BackupFtpAccessEntry[] = [];
        let accessFailedCount = 0;
        let accessError: string | undefined;
        try {
          const accRes = await api.get(`/server-control/${serviceName}/backup-ftp/access`);
          accessList = (accRes.data?.accessList || []) as BackupFtpAccessEntry[];
          accessFailedCount = Number(accRes.data?.failedCount) || 0;
        } catch (e: any) {
          // 访问列表拿不到不算整体失败，但要说明「列表为空是没查到」而不是「没配过 IP」
          accessError = e?.response?.data?.error || e?.message || "访问控制列表获取失败";
        }
        return { backupFtp: res.data?.backupFtp || null, accessList, accessFailedCount, accessError };
      } catch (e: any) {
        if (e?.response?.status === 404) {
          // US 区的写操作/查询被拦时也是 404，但带 notAvailable，别把它当成「未激活」
          if (e?.response?.data?.notAvailable === true) {
            return { notAvailable: true, error: e?.response?.data?.error };
          }
          return { notActivated: true };
        }
        return { notAvailable: true, error: e?.response?.data?.error || e?.message };
      }
    },
    enabled: !!serviceName,
  });
}

export function useActivateBackupFtp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (serviceName: string) => {
      const res = await api.post(`/server-control/${serviceName}/backup-ftp`);
      return res.data;
    },
    onSuccess: (_, serviceName) => {
      qc.invalidateQueries({ queryKey: qk.serverControl.backupFtp(serviceName) });
    },
  });
}

/** 关闭备份FTP服务（会删掉里面的备份，调用方必须先二次确认） */
export function useDeleteBackupFtp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (serviceName: string) => {
      const res = await api.delete(`/server-control/${serviceName}/backup-ftp`);
      return res.data;
    },
    onSuccess: (_, serviceName) => {
      qc.invalidateQueries({ queryKey: qk.serverControl.backupFtp(serviceName) });
    },
  });
}

/** 重置备份FTP密码（新密码由 OVH 发到账户邮箱，接口不返回明文） */
export function useResetBackupFtpPassword() {
  return useMutation({
    mutationFn: async (serviceName: string) => {
      const res = await api.post(`/server-control/${serviceName}/backup-ftp/password`);
      return res.data;
    },
  });
}

/** 可授权的 IP 段列表（给「添加访问 IP」做候选） */
export function useBackupFtpAuthorizableBlocks(serviceName: string | null, enabled = true) {
  return useQuery({
    queryKey: [...qk.serverControl.backupFtpAccess(serviceName || ""), "authorizable"] as const,
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/backup-ftp/authorizable-blocks`);
      return (res.data?.blocks || []) as string[];
    },
    enabled: !!serviceName && enabled,
  });
}

/** 添加备份FTP访问 IP。ftp 默认 true，nfs/cifs 默认 false（与后端一致） */
export function useAddBackupFtpAccess() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: {
      serviceName: string;
      ipBlock: string;
      ftp?: boolean;
      nfs?: boolean;
      cifs?: boolean;
    }) => {
      const res = await api.post(`/server-control/${vars.serviceName}/backup-ftp/access`, {
        ipBlock: vars.ipBlock,
        ftp: vars.ftp ?? true,
        nfs: !!vars.nfs,
        cifs: !!vars.cifs,
      });
      return res.data;
    },
    onSuccess: (_, vars) => {
      qc.invalidateQueries({ queryKey: qk.serverControl.backupFtp(vars.serviceName) });
    },
  });
}

/**
 * 删除备份FTP访问 IP。
 * ipBlock 是带掩码的 CIDR（37.59.1.0/28），放在路径里那个 "/" 会被 gin 还原成新的一段导致 404，
 * 所以走 ?ipBlock= query 形式 —— 后端的路径形式只作兼容兜底。
 */
export function useDeleteBackupFtpAccess() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { serviceName: string; ipBlock: string }) => {
      const res = await api.delete(
        `/server-control/${vars.serviceName}/backup-ftp/access?ipBlock=${encodeURIComponent(vars.ipBlock)}`
      );
      return res.data;
    },
    onSuccess: (_, vars) => {
      qc.invalidateQueries({ queryKey: qk.serverControl.backupFtp(vars.serviceName) });
    },
  });
}

// ───────────────────────────────── Secondary DNS / vMAC / vRack ─────────────────────────────────

export interface SecondaryDnsDomain extends DetailErrorMarked {
  domain: string;
  dns?: string;
  ipMaster?: string;
}

export interface VirtualMacEntry extends DetailErrorMarked {
  macAddress: string;
  type?: string;
  ipAddress?: string;
  virtualNetworkInterface?: string;
}

export interface VrackEntry extends DetailErrorMarked {
  vrackName: string;
  name?: string;
  description?: string;
}

export function useServerSecondaryDns(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.secondaryDns(serviceName || ""),
    queryFn: async (): Promise<PartialList<SecondaryDnsDomain>> => {
      const res = await api.get(`/server-control/${serviceName}/secondary-dns`);
      return readPartialList<SecondaryDnsDomain>(res.data, "domains");
    },
    enabled: !!serviceName,
  });
}

export function useServerVirtualMac(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.virtualMac(serviceName || ""),
    queryFn: async (): Promise<PartialList<VirtualMacEntry>> => {
      const res = await api.get(`/server-control/${serviceName}/virtual-mac`);
      return readPartialList<VirtualMacEntry>(res.data, "virtualMacs");
    },
    enabled: !!serviceName,
  });
}

export function useServerVrack(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.vrack(serviceName || ""),
    queryFn: async (): Promise<PartialList<VrackEntry>> => {
      const res = await api.get(`/server-control/${serviceName}/vrack`);
      return readPartialList<VrackEntry>(res.data, "vracks");
    },
    enabled: !!serviceName,
  });
}

// ───────────────────────────────── Orderable / Options / IP Specs / Network Specs ─────────────────────────────────

/** 可订购服务：并发取 bandwidth / traffic / ip 三项 */
export function useServerOrderable(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.orderable(serviceName || ""),
    queryFn: async () => {
      const [bw, tr, ip] = await Promise.all([
        api.get(`/server-control/${serviceName}/orderable/bandwidth`).catch(() => ({ data: { success: false } })),
        api.get(`/server-control/${serviceName}/orderable/traffic`).catch(() => ({ data: { success: false } })),
        api.get(`/server-control/${serviceName}/orderable/ip`).catch(() => ({ data: { success: false } })),
      ]);
      return {
        bandwidth: bw.data?.success ? bw.data.orderable : null,
        traffic: tr.data?.success ? tr.data.orderable : null,
        ip: ip.data?.success ? ip.data.orderable : null,
      };
    },
    enabled: !!serviceName,
  });
}

export interface ServerOptionEntry extends DetailErrorMarked {
  option: string;
  state?: string;
  expirationDate?: string;
}

export function useServerOptions(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.options(serviceName || ""),
    queryFn: async (): Promise<PartialList<ServerOptionEntry>> => {
      const res = await api.get(`/server-control/${serviceName}/options`);
      return readPartialList<ServerOptionEntry>(res.data, "options");
    },
    enabled: !!serviceName,
  });
}

export function useServerIpSpecs(serviceName: string | null) {
  return useQuery({
    queryKey: qk.serverControl.ipSpecs(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/ip-specs`);
      return (res.data?.ipSpecs || null) as any;
    },
    enabled: !!serviceName,
  });
}

export function useServerNetworkSpecs(serviceName: string | null, enabled = true) {
  return useQuery({
    queryKey: qk.serverControl.networkSpecs(serviceName || ""),
    queryFn: async () => {
      const res = await api.get(`/server-control/${serviceName}/network-specs`);
      return (res.data?.network || null) as any;
    },
    enabled: !!serviceName && enabled,
  });
}

// ───────────────────────────────── Interventions（创建工单） ─────────────────────────────────

/** 故障硬盘。disk_serial 是 OVH schema(dedicated.server.SupportReplaceHddInfo)的必填字段，
 *  slot_id 可选。字段名保持 snake_case 与官方一致，避免两层转换出错。 */
export interface FaultyDisk {
  disk_serial: string;
  slot_id?: number;
}

/** 创建硬件更换工单（硬盘 / 内存 / 散热）。
 *  端点是 POST /server-control/:svc/hardware/replace —— 旧代码发的 POST /interventions
 *  后端从未注册（只有 GET），所以这个功能此前一直是 404。
 *
 *  硬盘必须给出故障盘序列号：OVH 的 inverse 语义是「更换所有未列出的盘」，
 *  空列表 + inverse=true 等于申请更换整机每一块硬盘，后端现在会直接拒绝。 */
export function useCreateIntervention() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (args: {
      serviceName: string;
      type: string;
      details?: string;
      comment?: string;
      disks?: FaultyDisk[];
      slots?: string[];
    }) => {
      const res = await api.post(`/server-control/${args.serviceName}/hardware/replace`, {
        componentType: args.type,
        details: args.details,
        comment: args.comment,
        ...(args.disks?.length ? { disks: args.disks } : {}),
        ...(args.slots?.length ? { slots: args.slots } : {}),
      });
      return res.data;
    },
    onSuccess: (_, vars) => {
      qc.invalidateQueries({ queryKey: qk.serverControl.interventions(vars.serviceName) });
    },
  });
}

// ───────────────────────────────── Contact change ─────────────────────────────────

/** 提交变更联系人请求(POST /change-contact)
 *  字段名要跟 OVH API 一致: contactAdmin / contactTech / contactBilling */
export function useChangeContact() {
  return useMutation({
    mutationFn: async (args: { serviceName: string; admin?: string; tech?: string; billing?: string }) => {
      const res = await api.post(`/server-control/${args.serviceName}/change-contact`, {
        contactAdmin: args.admin || undefined,
        contactTech: args.tech || undefined,
        contactBilling: args.billing || undefined,
      });
      return res.data;
    },
  });
}

export interface ContactChangeRequestList {
  requests: any[];
  /**
   * true = 当前账户所在区域（US）根本没有 /me/task/contactChange 系列端点，后端返 501。
   * 这是「该区没有这个能力」而不是「请求失败」，组件应隐藏整个模块或显示 message，
   * 不要渲染成加载失败让用户反复重试。
   */
  unsupported: boolean;
  message?: string;
  /** 详情拉取失败的条数（后端同时会带 X-Partial-Failures 头） */
  failedCount: number;
}

/** 查询所有变更联系人请求（用户全局而非按服务器）。后端返回 { status, data, total, failed } */
export function useContactChangeRequests(enabled = true) {
  return useQuery({
    queryKey: qk.serverControl.contactRequests(),
    queryFn: async (): Promise<ContactChangeRequestList> => {
      try {
        const res = await api.get(`/ovh/contact-change-requests`);
        return {
          requests: (res.data?.data || res.data?.requests || []) as any[],
          unsupported: false,
          failedCount: Number(res.data?.failed) || 0,
        };
      } catch (e: any) {
        // 501 = 该区不支持，不是错误；其余错误照常抛出去让 isError 生效
        if (e?.response?.status === 501) {
          return {
            requests: [],
            unsupported: true,
            message: e?.response?.data?.message || "当前账户所在区域不支持联系人变更请求",
            failedCount: 0,
          };
        }
        throw e;
      }
    },
    enabled,
  });
}

/** 操作单个变更请求（接受 / 拒绝 / 重发邮件） */
export function useContactRequestAction() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (args: { id: number | string; action: "accept" | "refuse" | "resend"; token?: string }) => {
      if (args.action === "resend") {
        const res = await api.post(`/ovh/contact-change-requests/${args.id}/resend-email`);
        return res.data;
      }
      const res = await api.post(`/ovh/contact-change-requests/${args.id}/${args.action}`, {
        token: args.token,
      });
      return res.data;
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.serverControl.contactRequests() });
    },
  });
}

// ───────────────────────────────── Tasks · 可用时间段 ─────────────────────────────────

/** 任务的可用时间段（旧前端 GET /tasks/{id}/available-timeslots?periodStart=&periodEnd=） */
export function useTaskTimeslots(
  serviceName: string | null,
  taskId: number | null,
  periodStart: string,
  periodEnd: string,
  enabled = true
) {
  return useQuery({
    queryKey: qk.serverControl.taskTimeslots(serviceName || "", taskId || 0, periodStart, periodEnd),
    queryFn: async () => {
      const res = await api.get(
        `/server-control/${serviceName}/tasks/${taskId}/available-timeslots?periodStart=${encodeURIComponent(periodStart)}&periodEnd=${encodeURIComponent(periodEnd)}`
      );
      return {
        timeslots: (res.data?.timeslots || []) as any[],
        scheduleNotRequired: !!res.data?.scheduleNotRequired,
      };
    },
    enabled: !!serviceName && !!taskId && enabled,
  });
}

/**
 * 给任务改期（干预 / 维护类任务 OVH 要求先选时间段）。
 * hasPerformedBackup 直接发用户勾选框的真实值：后端不再强制 true，
 * 未备份也照实转发给 OVH（后端记警告日志），前端替用户勾上等于替他做担保。
 */
export function useScheduleTask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (args: {
      serviceName: string;
      taskId: number;
      /** RFC3339，通常取自 useTaskTimeslots 返回的时间段 */
      wantedBeginingDate: string;
      hasPerformedBackup: boolean;
    }) => {
      const res = await api.post(`/server-control/${args.serviceName}/tasks/${args.taskId}/schedule`, {
        wantedBeginingDate: args.wantedBeginingDate,
        hasPerformedBackup: args.hasPerformedBackup,
      });
      return res.data;
    },
    onSuccess: (_, vars) => {
      qc.invalidateQueries({ queryKey: qk.serverControl.tasks(vars.serviceName) });
    },
  });
}

/** 重启服务器（mutation 封装） */
export function useRebootServer() {
  return useMutation({
    mutationFn: async (serviceName: string) => {
      const res = await api.post(`/server-control/${serviceName}/reboot`);
      return res.data;
    },
  });
}
