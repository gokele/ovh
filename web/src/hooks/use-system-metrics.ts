import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

/** 后端二进制版本*/
export function useAppVersion() {
  return useQuery({
    queryKey: ["app", "version"],
    queryFn: async () => (await api.get<{ version: string }>("/version")).data.version,
    staleTime: Infinity, // 进程跑起来版本号不会变,缓存到 unmount
    gcTime: Infinity,
    retry: 0,
  });
}

export interface SystemMetrics {
  cpu: { percent: number; cores: number };
  memory: { totalBytes: number; usedBytes: number; percent: number };
  disk: { totalBytes: number; usedBytes: number; percent: number; path: string };
  host: { hostname: string; platform: string; uptimeSec: number };
}

/** 宿主机实时监控。仪表盘专用，每 2 秒拉一次。
 *  - 这是唯一需要后台轮询的查询(实时监控本质要求)
 *  - 组件 unmount(切走仪表盘) 后 React Query 自动停止 refetch
 */
export function useSystemMetrics() {
  return useQuery({
    queryKey: ["system", "metrics"],
    queryFn: async () => (await api.get<SystemMetrics>("/system/metrics")).data,
    refetchInterval: 2000,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    staleTime: 1500,
  });
}
