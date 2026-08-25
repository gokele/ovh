import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import type { PartialList } from "./partial-list";

/* ────────────── 类型定义 ────────────── */

export interface OwnedVps {
  serviceName: string;
  name: string;
  displayName: string;
  state: string;        // "running" / "stopped" / "migrating" / "installing"
  cluster: string;
  zone: string;
  keymap: string;
  netbootMode: string;
  offerType: string;
  slaMonitoring: boolean | null;
  lockStatus: string;
  model: string;        // vps.Model.name
  vcore: number;
  memoryMB: number;
  diskGB: number;
  /** billing status。serviceInfos 拉取失败时为 null —— 「没查到」不是一种状态，别渲染成「unknown」 */
  status: string | null;
  /** 是否自动续费。null = 这次没查到（serviceInfos 失败或 renew 为空），必须和 false（确实没开）分开显示 */
  renewalType: boolean | null;
  error?: string;
}

export interface VpsServiceInfo {
  status: string;
  expiration: string;
  creation: string;
  renewalType: boolean;
  renewalPeriod: number;
  renewalDeleteAtExpiration: boolean;
  renewalForced: boolean;
  renewalManualPayment: boolean;
  possibleRenewPeriod: number[];
}

export interface VpsIp {
  ipAddress: string;
  reverse?: string;
  type?: string;
  version?: string;
  gateway?: string;
  geolocation?: string;
  macAddress?: string;
}

export interface VpsTemplate {
  /** EU 返回 long, US 返回 string —— 直接当 ID 回传给 reinstall 接口即可 */
  id: number | string;
  name: string;
  distribution: string;
  bitFormat: number;
  locale: string;
  availableLanguage: string[];
}

/** templates 接口返回的 kind:
 *   "templateId" - EU,reinstall body 用 templateId (long)
 *   "imageId"    - US,rebuild body 用 imageId (string),前端只展示用,后端按 endpoint 自动分路 */
export type TemplateKind = "templateId" | "imageId";

export interface VpsTask {
  id: number;
  type: string;
  state: string;       // todo / doing / done / cancelled / paused
  date: string;
  progress: number;
}

export interface VpsSnapshot {
  id: string;
  creationDate: string;
  description: string;
  region: string;
}

/* ────────────── List + Info + Status ────────────── */

export function useOwnedVps() {
  return useQuery({
    queryKey: qk.vpsControl.list(),
    queryFn: async () => {
      const res = await api.get("/vps-control/list");
      return (res.data?.vps || []) as OwnedVps[];
    },
    staleTime: 60_000,
  });
}

export function useVpsInfo(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.info(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/info`);
      return res.data?.info as Record<string, any> | null;
    },
    enabled: !!svc,
  });
}

/** 服务端口探测结果。US 区没有这个 OVH 端点，后端返 200 + status:null + unsupported:true */
export interface VpsServiceStatusResult {
  status: Record<string, any> | null;
  /** true 表示当前账户所在区域没有该能力，组件应整块隐藏而不是显示「加载失败」 */
  unsupported: boolean;
  /** unsupported 时的中文说明 */
  message?: string;
  /** 后端判定的大区（目前只会是 "US"）。前端优先用它写文案，而不是自己再判一次 endpoint */
  region?: string;
}

/** VPS 网络服务存活探测(ping/dns/http/https/smtp/ssh) — 跟 info.state 不一样 */
export function useVpsServiceStatus(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.status(svc || ""),
    queryFn: async (): Promise<VpsServiceStatusResult> => {
      const res = await api.get(`/vps-control/${svc}/status`);
      return {
        status: (res.data?.status ?? null) as Record<string, any> | null,
        unsupported: res.data?.unsupported === true,
        message: res.data?.message,
        region: res.data?.region,
      };
    },
    enabled: !!svc,
    staleTime: 30_000,
  });
}

export function useVpsServiceInfo(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.serviceInfo(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/serviceinfo`);
      return (res.data?.serviceInfo || null) as VpsServiceInfo | null;
    },
    enabled: !!svc,
  });
}

export function useUpdateVpsRenewal(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { mode: "auto" | "manual" | "delete"; period?: number }) => {
      const res = await api.put(`/vps-control/${svc}/serviceinfo/renewal`, vars);
      return res.data;
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.vpsControl.serviceInfo(svc) }),
  });
}

export function useVpsIps(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.ips(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/ips`);
      return (res.data?.ips || []) as VpsIp[];
    },
    enabled: !!svc,
  });
}

export function useSetVpsIpReverse(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { ip: string; reverse: string }) => {
      const res = await api.put(`/vps-control/${svc}/ips/${vars.ip}/reverse`, { reverse: vars.reverse });
      return res.data;
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.vpsControl.ips(svc) }),
  });
}

export function useVpsDatacenter(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.datacenter(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/datacenter`);
      return res.data?.datacenter as { name: string; longName: string; country: string } | null;
    },
    enabled: !!svc,
  });
}

// VPS CPU/内存监控端点已被 OVH 全面 DEPRECATED:
//   /vps/{name}/monitoring  - DEPRECATED 2024-07,计划 2024-09 删除
//   /vps/{name}/statistics  - DEPRECATED 2023-11,计划 2024-01 删除
// OVH 没有提供新的 VPS 级 CPU/内存监控端点,只剩磁盘监控(/disks/{id}/use)。
// 移除监控功能,需要看负载请登录 VPS 用 top/htop。

/* ────────────── Power ────────────── */

export function useVpsStart(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => (await api.post(`/vps-control/${svc}/start`)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.vpsControl.list() });
      qc.invalidateQueries({ queryKey: qk.vpsControl.info(svc) });
    },
  });
}

export function useVpsStop(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => (await api.post(`/vps-control/${svc}/stop`)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.vpsControl.list() });
      qc.invalidateQueries({ queryKey: qk.vpsControl.info(svc) });
    },
  });
}

export function useVpsReboot(svc: string) {
  return useMutation({
    mutationFn: async () => (await api.post(`/vps-control/${svc}/reboot`)).data,
  });
}

export function useVpsConsoleUrl(svc: string) {
  return useMutation({
    mutationFn: async () => {
      const res = await api.post(`/vps-control/${svc}/console`);
      return res.data?.url as string;
    },
  });
}

export function useVpsSetPassword(svc: string) {
  return useMutation({
    mutationFn: async () => (await api.post(`/vps-control/${svc}/password`)).data,
  });
}

/* ────────────── Reinstall ────────────── */

/** 当前安装的系统信息(EU /distribution / US /images/current) */
export function useVpsCurrentOS(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.currentOS(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/current-os`);
      return res.data?.currentOS as {
        id: number | string;
        name: string;
        distribution: string;
        bitFormat: number;
        locale: string;
        source: string;
      } | null;
    },
    enabled: !!svc,
    staleTime: 5 * 60_000,
  });
}

/** 模板列表 + 部分失败信息。kind 决定 reinstall 用 templateId 还是 imageId（后端按 endpoint 自动分路） */
export interface VpsTemplateList extends PartialList<VpsTemplate> {
  kind: TemplateKind | "";
}

/**
 * 系统模板列表。
 * 后端现在区分「详情全挂」和「账户真没模板」：全挂返 500（这里直接抛出去让 isError 生效），
 * 部分挂返 200 + partial/failed。组件必须把 isError 渲染成「读取失败，请重试」，
 * 而不是沿用「暂无可用模板」那句空态文案 —— 那会让用户以为账户没模板从而放弃重试。
 */
export function useVpsTemplates(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.templates(svc || ""),
    queryFn: async (): Promise<VpsTemplateList> => {
      const res = await api.get(`/vps-control/${svc}/templates`);
      const failedCount = Number(res.data?.failed) || 0;
      return {
        items: (res.data?.templates || []) as VpsTemplate[],
        kind: (res.data?.kind || "") as TemplateKind | "",
        partial: res.data?.partial === true || failedCount > 0,
        failedCount,
      };
    },
    enabled: !!svc,
    staleTime: 5 * 60_000,
  });
}

export function useReinstallVps(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: {
      templateId: number | string;
      language?: string;
      sshKey?: string[];
      doNotSendPassword?: boolean;
      softwareId?: number[];
    }) => (await api.post(`/vps-control/${svc}/reinstall`, vars)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.vpsControl.tasks(svc) });
    },
  });
}

/* ────────────── Tasks ────────────── */

export function useVpsTasks(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.tasks(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/tasks`);
      return (res.data?.tasks || []) as VpsTask[];
    },
    enabled: !!svc,
  });
}

export function useVpsTask(svc: string | null, taskId: number | string | null, refetchInterval = 0) {
  return useQuery({
    queryKey: qk.vpsControl.task(svc || "", taskId || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/tasks/${taskId}`);
      return res.data?.task as VpsTask | null;
    },
    enabled: !!svc && !!taskId,
    refetchInterval,
  });
}

/* ────────────── Snapshot ────────────── */

export function useVpsSnapshot(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.snapshot(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/snapshot`);
      return res.data?.snapshot as VpsSnapshot | null;
    },
    enabled: !!svc,
  });
}

export function useCreateVpsSnapshot(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { description?: string }) => (await api.post(`/vps-control/${svc}/snapshot`, vars)).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.vpsControl.snapshot(svc) }),
  });
}

export function useUpdateVpsSnapshot(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { description: string }) => (await api.put(`/vps-control/${svc}/snapshot`, vars)).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.vpsControl.snapshot(svc) }),
  });
}

export function useRevertVpsSnapshot(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => (await api.post(`/vps-control/${svc}/snapshot/revert`)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.vpsControl.list() });
      qc.invalidateQueries({ queryKey: qk.vpsControl.tasks(svc) });
    },
  });
}

export function useDeleteVpsSnapshot(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => (await api.delete(`/vps-control/${svc}/snapshot`)).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.vpsControl.snapshot(svc) }),
  });
}

/* ────────────── Misc ────────────── */

export function useChangeVpsContact() {
  return useMutation({
    mutationFn: async (vars: { serviceName: string; admin?: string; tech?: string; billing?: string }) => {
      const body: Record<string, string> = {};
      if (vars.admin) body.contactAdmin = vars.admin;
      if (vars.tech) body.contactTech = vars.tech;
      if (vars.billing) body.contactBilling = vars.billing;
      return (await api.post(`/vps-control/${vars.serviceName}/change-contact`, body)).data;
    },
  });
}

/**
 * VPS 的到期终止策略。和独服同理：**不要**用 /terminate（那是立即终止）。
 * 到期终止只能通过 PUT /services/{serviceId} 的 terminationPolicy 设置。
 */
export function useUpdateVpsTerminationPolicy() {
  return useMutation({
    mutationFn: async (vars: { serviceName: string; policy: string }) =>
      (await api.put(`/vps-control/${vars.serviceName}/termination-policy`, {
        policy: vars.policy,
      })).data,
  });
}

/**
 * ⚠️ 立即终止 —— 提交并确认后 OVH 会**当场暂停** VPS，
 * 并邮件通知「5 天内不付款就彻底清除硬盘数据」。
 * 想要「到期才终止」请用 useUpdateVpsTerminationPolicy，不要用这个。
 * 目前界面上没有入口，保留仅为将来做「立即终止」时复用。
 */
export function useTerminateVps() {
  return useMutation({
    mutationFn: async (vars: { serviceName: string }) =>
      (await api.post(`/vps-control/${vars.serviceName}/terminate`)).data,
  });
}

export function useConfirmTerminateVps() {
  return useMutation({
    mutationFn: async (vars: { serviceName: string; token: string; reason?: string; commentary?: string }) =>
      (await api.post(`/vps-control/${vars.serviceName}/confirm-termination`, {
        token: vars.token,
        reason: vars.reason,
        commentary: vars.commentary,
      })).data,
  });
}

export function useVpsSecondaryDns(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.secondaryDns(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/secondary-dns`);
      return (res.data?.domains || []) as any[];
    },
    enabled: !!svc,
  });
}

export function useAddVpsSecondaryDns(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { domain: string; ip: string }) =>
      (await api.post(`/vps-control/${svc}/secondary-dns`, vars)).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.vpsControl.secondaryDns(svc) }),
  });
}

export function useDeleteVpsSecondaryDns(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (domain: string) =>
      (await api.delete(`/vps-control/${svc}/secondary-dns/${domain}`)).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.vpsControl.secondaryDns(svc) }),
  });
}

/**
 * VPS 附加选项(ftpbackup / veeam / snapshot / automatedBackup / windows / cpanel / plesk / additionalDisk)。
 *
 * manageEndpointsAvailable=false 表示"这个选项在当前账户所在区域没有专属管理端点":
 * 美区 OVHcloud 的 /vps/{sn}/backupftp 与 /vps/{sn}/veeam 整套端点都不存在(EU/CA 才有),
 * 但 /vps/{sn}/option 照样把它们列出来(三区的 VpsOptionEnum 完全一致)。
 * 前端必须据此把「管理」入口置灰并显示 unsupportedReason,而不是让用户点进去吃一串 404。
 *
 * 注意:它只说"没有管理端点",不代表选项没生效、也不影响退订 ——
 * DELETE /vps/{sn}/option/{option} 三区都注册,所以别拿它去禁用「取消选项」。
 */
export interface VpsOption {
  option: string;
  state?: string;
  /** false = 该选项在本区没有专属管理端点(后端 handlers/vps_control_misc.go 打的标记) */
  manageEndpointsAvailable?: boolean;
  /** manageEndpointsAvailable=false 时的中文原因 */
  unsupportedReason?: string;
  /** 打标记时后端带回的大区,目前只会是 "US" */
  region?: string;
  [k: string]: any;
}

export function useVpsOptions(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.options(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/options`);
      return (res.data?.options || []) as VpsOption[];
    },
    enabled: !!svc,
  });
}

/**
 * 取消附加选项。
 * deleteNow 是 OVH schema 里的可选 query 参数：不传 = 到期时释放（默认），
 * 传 true = 立刻释放。两种语义差别很大（后者当场失去该选项），所以必须由调用方显式选，
 * 不能替用户决定。响应里的 deprecated:true 表示 OVH 已废弃该操作。
 */
export function useDeleteVpsOption(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { option: string; deleteNow?: boolean }) => {
      const qs = vars.deleteNow ? "?deleteNow=true" : "";
      return (await api.delete(`/vps-control/${svc}/options/${encodeURIComponent(vars.option)}${qs}`)).data as {
        success: boolean;
        message?: string;
        deleteNow?: boolean;
        deprecated?: boolean;
      };
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.vpsControl.options(svc) }),
  });
}

/* ────────────── Engagement(合同期) ────────────── */

export function useVpsEngagement(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.engagement(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/engagement`);
      return res.data?.engagement as { currentPeriod?: any; endRule?: any } | null;
    },
    enabled: !!svc,
  });
}

export function useVpsEngagementAvailable(svc: string | null, enabled = true) {
  return useQuery({
    queryKey: qk.vpsControl.engagementAvailable(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/engagement/available`);
      return (res.data?.pricings || []) as any[];
    },
    enabled: !!svc && enabled,
  });
}

export function useVpsEngagementRequest(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.engagementRequest(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/engagement/request`);
      return res.data?.request as any | null;
    },
    enabled: !!svc,
  });
}

export function useCreateVpsEngagementRequest(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { pricingMode: string }) =>
      (await api.post(`/vps-control/${svc}/engagement/request`, vars)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.vpsControl.engagement(svc) });
      qc.invalidateQueries({ queryKey: qk.vpsControl.engagementRequest(svc) });
    },
  });
}

export function useDeleteVpsEngagementRequest(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () =>
      (await api.delete(`/vps-control/${svc}/engagement/request`)).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.vpsControl.engagementRequest(svc) }),
  });
}

export function useUpdateVpsEngagementEndRule(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { strategy: string }) =>
      (await api.put(`/vps-control/${svc}/engagement/end-rule`, vars)).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.vpsControl.engagement(svc) }),
  });
}

/* ────────────── DDoS Mitigation ────────────── */

export interface VpsMitigationIp {
  ipOnMitigation: string;
  state: string;
  auto: boolean;
  permanent: boolean;
}

export interface VpsMitigationBlock {
  /** OVH 认的带掩码 ipBlock，后端保证以本行的 ipAddress 开头（归一化失败时就是裸 IP） */
  ipBlock: string;
  /** 裸 IP。要显示地址就直接用它，别再从 ipBlock 上 split("/") 反推 */
  ipAddress?: string;
  mitigations: VpsMitigationIp[];
  error?: string;
}

export function useVpsMitigation(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.mitigation(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/mitigation`);
      return (res.data?.ips || []) as VpsMitigationBlock[];
    },
    enabled: !!svc,
    // 有过渡态(creationPending/removalPending)时每 5 秒轮询一次,稳定就停
    refetchInterval: (q) => {
      const data = q.state.data as VpsMitigationBlock[] | undefined;
      if (!data) return false;
      const hasTransition = data.some((b) =>
        b.mitigations.some((m) => m.state === "creationPending" || m.state === "removalPending"),
      );
      return hasTransition ? 5000 : false;
    },
  });
}

export function useEnableVpsMitigation(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { ip: string; block: string }) =>
      (await api.post(`/vps-control/${svc}/mitigation/${vars.ip}?block=${encodeURIComponent(vars.block)}`)).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.vpsControl.mitigation(svc) }),
  });
}

export function useDisableVpsMitigation(svc: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { ip: string; block: string }) =>
      (await api.delete(`/vps-control/${svc}/mitigation/${vars.ip}?block=${encodeURIComponent(vars.block)}`)).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.vpsControl.mitigation(svc) }),
  });
}

export function useVpsAutomatedBackup(svc: string | null) {
  return useQuery({
    queryKey: qk.vpsControl.automatedBackup(svc || ""),
    queryFn: async () => {
      const res = await api.get(`/vps-control/${svc}/automated-backup`);
      return res.data?.automatedBackup as { rotation: number; schedule: string; state: string } | null;
    },
    enabled: !!svc,
  });
}
