import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { toast } from "sonner";

export interface ServerOption {
  label: string;
  value: string;
  family?: string;
}

export interface ServerPlan {
  planCode: string;
  name: string;
  description?: string;
  cpu: string;
  memory: string;
  storage: string;
  bandwidth: string;
  vrackBandwidth: string;
  defaultOptions: ServerOption[];
  availableOptions: ServerOption[];
  datacenters: {
    datacenter: string;
    dcName: string;
    region: string;
    availability: string;
    countryCode: string;
  }[];
}

/** 服务器目录（带可用性）。
 *  - 2 小时内不会因为 mount / 切 tab / focus 重新请求；后端 ServerCache 也是 2 小时
 *  - 后端无定时刷新：只有访问时才检查缓存是否过期，过期才会调 OVH
 *  - forceRefresh()：先 POST /cache/clear 清后端内存缓存，再走标准 q.refetch()
 *                   走 react-query 自己的请求流程，data 一定通知订阅者重渲染
 *  - isRefreshing = q.isRefetching：只在 refetch 期间为 true，跟首次加载 isLoading 严格分开
 */
export function useServers(showApiServers: boolean = true) {
  const qc = useQueryClient();
  const key = qk.servers.list(showApiServers);
  const q = useQuery({
    queryKey: key,
    queryFn: async () => {
      const res = await api.get("/servers", { params: { showApiServers } });
      return (res.data.servers || res.data || []) as ServerPlan[];
    },
    staleTime: 2 * 60 * 60_000,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

  const forceRefresh = async () => {
    // 后端内存缓存清掉，让接下来 refetch 一定打 OVH
    try {
      await api.post("/cache/clear", { type: "memory" });
    } catch {
      // 清缓存失败不致命，refetch 还能拿到现有缓存
    }
    // 标准 react-query refetch：期间 q.isRefetching=true，完成后 data 自动通知重渲染
    await q.refetch();
    // 让 /api/cache/info 也刷新一下，徽章里"X 分钟前"立刻归零
    qc.invalidateQueries({ queryKey: ["settings", "cache-info"] });
  };

  return Object.assign(q, {
    forceRefresh,
    isRefreshing: q.isRefetching,
  });
}

/** 添加到监控订阅 */
export function useAddToMonitor() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (payload: { planCode: string; datacenters: string[]; serverName?: string }) =>
      (await api.post("/monitor/subscriptions", { ...payload, notifyAvailable: true, notifyUnavailable: false })).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.monitor.list() });
      toast.success("已加入监控");
    },
    onError: (e: any) => toast.error(e.response?.data?.error || "加入监控失败"),
  });
}
