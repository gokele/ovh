import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { toast } from "sonner";
import i18n from "@/i18n";
import { apiMessage, bodyMessage } from "@/lib/api-error";

export interface VPSSubscription {
  id: string;
  /** OVH 已经不卖这个型号了 —— 这条订阅永远不会响。后端每次读列表时现算，不落库 */
  retired?: boolean;
  planCode: string;
  ovhSubsidiary: string;
  datacenters: string[];
  monitorLinux: boolean;
  monitorWindows: boolean;
  notifyAvailable: boolean;
  notifyUnavailable: boolean;
  autoOrder?: boolean;
  quantity?: number;
  /** 自动下单时装什么系统。空 = 用 OVH 默认镜像 */
  os?: string;
  /** 下单成功后自动付款(显式开关,默认关) */
  autoPay?: boolean;
  /** 触发 auto-order 时用哪个 OVH 账户下单(空 = 只通知) */
  autoOrderAccountId?: string;
  lastStatus: Record<string, string>;
  createdAt: string;
}

export interface VPSMonitorStatus {
  running: boolean;
  subscriptions_count: number;
  check_interval: number;
}

export interface VPSMonitorHistoryEntry {
  timestamp: string;
  datacenter: string;
  datacenterCode?: string;
  status: string;
  changeType: string;
  oldStatus?: string | null;
}

/** VPS 补货订阅列表 */
export function useVPSMonitorList() {
  return useQuery({
    queryKey: qk.vpsMonitor.list(),
    // Array.isArray:调用方直接 .filter/.map,truthy 非数组会整页白屏
    queryFn: async () => {
      const d = (await api.get<VPSSubscription[]>("/vps-monitor/subscriptions")).data;
      return Array.isArray(d) ? d : [];
    },
  });
}

/** VPS 监控状态 */
export function useVPSMonitorStatus() {
  return useQuery({
    queryKey: qk.vpsMonitor.status(),
    queryFn: async () => (await api.get<VPSMonitorStatus>("/vps-monitor/status")).data,
    refetchInterval: 30_000,
  });
}

/** 切换 VPS 监控 on/off */
export function useToggleVPSMonitor() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (running: boolean) =>
      (await api.post(`/vps-monitor/${running ? "stop" : "start"}`)).data,
    onSuccess: (_, running) => {
      qc.invalidateQueries({ queryKey: qk.vpsMonitor.status() });
      toast.success(i18n.t(running ? "hooksMsg.vpsMonitor.stopped" : "hooksMsg.vpsMonitor.started"));
    },
    onError: (e: any) => toast.error(apiMessage(e)),
  });
}

/** VPS 某订阅的变化历史（后端直接返回数组，倒序最新在前） */
export function useVPSMonitorHistory(id: string | null) {
  return useQuery({
    queryKey: qk.vpsMonitor.history(id || ""),
    queryFn: async () =>
      (await api.get<VPSMonitorHistoryEntry[]>(`/vps-monitor/subscriptions/${id}/history`)).data,
    enabled: !!id,
  });
}

/** 添加 VPS 订阅 */
export function useAddVPSSubscription() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (
      payload: Omit<VPSSubscription, "id" | "lastStatus" | "createdAt">
    ) => (await api.post("/vps-monitor/subscriptions", payload)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.vpsMonitor.list() });
      qc.invalidateQueries({ queryKey: qk.vpsMonitor.status() });
      toast.success(i18n.t("hooksMsg.vpsMonitor.subAdded"));
    },
    onError: (e: any) => toast.error(apiMessage(e)),
  });
}

/** 创建新 VPS 订阅（对外语义化别名） */
export const useCreateVPSMonitorSubscription = useAddVPSSubscription;

/** 删除 VPS 订阅 */
export function useRemoveVPSSubscription() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) =>
      (await api.delete(`/vps-monitor/subscriptions/${id}`)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.vpsMonitor.list() });
      qc.invalidateQueries({ queryKey: qk.vpsMonitor.status() });
      toast.success(i18n.t("hooksMsg.vpsMonitor.subDeleted"));
    },
    onError: (e: any) => toast.error(apiMessage(e)),
  });
}

/** 修改已有 VPS 订阅（PUT，只改配置不重置历史） */
export function useUpdateVPSSubscription() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, ...patch }: Partial<VPSSubscription> & { id: string }) =>
      (await api.put(`/vps-monitor/subscriptions/${encodeURIComponent(id)}`, patch)).data,
    onSuccess: (data: any) => {
      qc.invalidateQueries({ queryKey: qk.vpsMonitor.list() });
      qc.invalidateQueries({ queryKey: qk.vpsMonitor.status() });
      toast.success(bodyMessage(data) || i18n.t("hooksMsg.vpsMonitor.subUpdated"));
    },
    onError: (e: any) =>
      toast.error(apiMessage(e)),
  });
}

/** 清空 VPS 订阅 */
export function useClearVPSMonitor() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => (await api.delete("/vps-monitor/subscriptions/clear")).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.vpsMonitor.list() });
      qc.invalidateQueries({ queryKey: qk.vpsMonitor.status() });
      toast.success(i18n.t("hooksMsg.vpsMonitor.subsCleared"));
    },
    onError: (e: any) => toast.error(apiMessage(e)),
  });
}

export interface VPSModel {
  planCode: string;
  name: string;
  generation: string;
  price?: string;
  /** US 站点会把欧洲/加拿大机房的 VPS 以 -eu / -ca 后缀单独卖，同名不同货 */
  location?: string;
  /** 这个型号能装在哪些机房。三个站点取值完全不同 */
  datacenters?: string[];
  /** 下单时可选的系统。VPS 的系统是下单时就要定的，不是买完再装 */
  osChoices?: string[];
}

/**
 * 当前还在售的 VPS 型号，来自 OVH 实时目录。
 *
 * 为什么不写死：型号会**整代下架**。实测 vps-2025 全线已经退出 OVH 下单目录，
 * 而这个下拉框以前就写死着它 —— 订阅一个停售型号，库存接口老实返回"全部无货"，
 * 永远不跳变也就永远不通知，症状和"这机器确实抢手"一模一样。
 */
export function useVPSModels(subsidiary?: string) {
  return useQuery({
    queryKey: ["vps-monitor", "models", subsidiary || ""],
    queryFn: async () =>
      (
        await api.get<{ subsidiary: string; models: VPSModel[] }>("/vps-monitor/models", {
          params: subsidiary ? { subsidiary } : undefined,
        })
      ).data,
    staleTime: 30 * 60 * 1000,
    retry: 0,
  });
}