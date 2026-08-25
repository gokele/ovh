import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { toast } from "sonner";

export interface MonitorSubscription {
  planCode: string;
  serverName?: string;
  datacenters: string[];
  notifyAvailable: boolean;
  notifyUnavailable: boolean;
  autoOrder?: boolean;
  quantity?: number;
  /** 触发 auto-order 时用哪个 OVH 账户下单;空 = 只通知 */
  autoOrderAccountId?: string;
  lastStatus: Record<string, string>;
  createdAt: string;
}

export interface MonitorStatus {
  running: boolean;
  subscriptions_count: number;
  check_interval: number;
  known_servers_count: number;
}

export interface MonitorHistoryEntry {
  timestamp: string;
  datacenter: string;
  status: string;
  changeType: string;
  /** 后端是 interface{}，实际可能是 string / null / 任意 OVH 状态字符串。这里宽松成 unknown */
  oldStatus: unknown;
  config?: {
    memory?: string;
    storage?: string;
    display?: string;
    [key: string]: any;
  };
}

/** 监控订阅列表 */
export function useMonitorList() {
  return useQuery({
    queryKey: qk.monitor.list(),
    queryFn: async () => (await api.get<MonitorSubscription[]>("/monitor/subscriptions")).data,
  });
}

/** 监控引擎运行状态 */
export function useMonitorStatus() {
  return useQuery({
    queryKey: qk.monitor.status(),
    queryFn: async () => (await api.get<MonitorStatus>("/monitor/status")).data,
    refetchInterval: 30_000,
  });
}

/** 某订阅的变化历史（后端直接返回数组，倒序最新在前） */
export function useMonitorHistory(planCode: string | null) {
  return useQuery({
    queryKey: qk.monitor.history(planCode || ""),
    queryFn: async () =>
      (await api.get<MonitorHistoryEntry[]>(`/monitor/subscriptions/${planCode}/history`)).data,
    enabled: !!planCode,
  });
}

/** 新增 / 修改订阅 */
export function useUpsertMonitorSubscription() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (payload: Partial<MonitorSubscription> & { planCode: string }) =>
      (await api.post("/monitor/subscriptions", payload)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.monitor.list() });
      qc.invalidateQueries({ queryKey: qk.monitor.status() });
      toast.success("订阅已保存");
    },
    onError: (e: any) => toast.error(e.response?.data?.error || "保存失败"),
  });
}

/** 创建新订阅（对外更语义化的别名） */
export const useCreateMonitorSubscription = useUpsertMonitorSubscription;

/** 移除订阅 */
export function useRemoveMonitorSubscription() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (planCode: string) =>
      (await api.delete(`/monitor/subscriptions/${planCode}`)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.monitor.list() });
      qc.invalidateQueries({ queryKey: qk.monitor.status() });
      toast.success("已删除订阅");
    },
    onError: (e: any) => toast.error(e.response?.data?.error || "删除失败"),
  });
}

/** 清空所有订阅 */
export function useClearMonitor() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => (await api.delete("/monitor/subscriptions/clear")).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.monitor.list() });
      qc.invalidateQueries({ queryKey: qk.monitor.status() });
      toast.success("已清空全部订阅");
    },
    onError: (e: any) => toast.error(e.response?.data?.error || "清空失败"),
  });
}

/** 设置检查间隔（秒）。后端会夹到 5-3600，返回真正生效的值。 */
export function useSetMonitorInterval() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (interval: number) =>
      (await api.put("/monitor/interval", { interval })).data as {
        status: string;
        message: string;
        check_interval: number;
      },
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: qk.monitor.status() });
      toast.success(data.message || "检查间隔已更新");
    },
    onError: (e: any) => toast.error(e.response?.data?.message || "设置失败"),
  });
}
