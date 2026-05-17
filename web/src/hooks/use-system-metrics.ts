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

/** GitHub releases 上游更新检查结果 */
export interface UpdateCheck {
  current: string;
  latest: string;
  tag: string;
  name: string;
  hasUpdate: boolean;
  url: string;
  publishedAt: string;
  body: string;
  prerelease: boolean;
  checkedAt: string;
}

/** 检查上游 (gokele/ovh) 是否有新版本。
 *  - 后端不缓存,收到请求就直连 GitHub
 *  - 前端 staleTime 1 小时,同一会话内访问仪表盘不会反复打 GitHub(避免触发 60 次/小时未鉴权限速)
 *  - 不监听 focus、不轮询,仅在组件 mount 且 stale 时触发一次
 */
export function useUpdateCheck() {
  return useQuery<UpdateCheck>({
    queryKey: ["app", "update-check"],
    queryFn: async () => (await api.get<UpdateCheck>("/version/check-update")).data,
    staleTime: 60 * 60 * 1000, // 1h
    gcTime: 24 * 60 * 60 * 1000,
    retry: 0,
    refetchOnWindowFocus: false,
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
