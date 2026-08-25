import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { toast } from "sonner";

export interface VPSSubscription {
  id: string;
  planCode: string;
  ovhSubsidiary: string;
  datacenters: string[];
  monitorLinux: boolean;
  monitorWindows: boolean;
  notifyAvailable: boolean;
  notifyUnavailable: boolean;
  autoOrder?: boolean;
  quantity?: number;
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
    queryFn: async () => (await api.get<VPSSubscription[]>("/vps-monitor/subscriptions")).data,
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
      toast.success(running ? "VPS 监控已停止" : "VPS 监控已启动");
    },
    onError: (e: any) => toast.error(e.response?.data?.error || "操作失败"),
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
      toast.success("VPS 订阅已添加");
    },
    onError: (e: any) => toast.error(e.response?.data?.error || "添加失败"),
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
      toast.success("已删除");
    },
    onError: (e: any) => toast.error(e.response?.data?.error || "删除失败"),
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
      toast.success(data?.message || "订阅已更新");
    },
    onError: (e: any) =>
      toast.error(e.response?.data?.message || e.response?.data?.error || "更新失败"),
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
      toast.success("已清空全部 VPS 订阅");
    },
    onError: (e: any) => toast.error(e.response?.data?.error || "清空失败"),
  });
}
