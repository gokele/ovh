import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";

export interface DashboardStats {
  activeQueues: number;
  totalServers: number;
  availableServers: number;
  purchaseSuccess: number;
  purchaseFailed: number;
  queueProcessorRunning?: boolean;
  monitorRunning?: boolean;
}

/** 拉取仪表盘 KPI 总览 */
export function useStats() {
  return useQuery({
    queryKey: qk.stats(),
    queryFn: async () => (await api.get<DashboardStats>("/stats")).data,
  });
}
