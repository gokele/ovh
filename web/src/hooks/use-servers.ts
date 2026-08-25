import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { toast } from "sonner";
import { useAccounts, findAccountByID } from "@/hooks/use-accounts";
import { useActiveServerControlAccount } from "@/hooks/use-active-account";

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
  // 目录跟着当前账户走:后端 /api/servers 支持 ?account=,按该账户的 zone(子公司)+ endpoint
  // 取目录并单独分桶缓存。不传的话永远是默认账户的视角 —— 切到美区账户后,页面上的机型集合、
  // 机房状态、价格分别来自三个不同的账户/站点,互相对不上。
  // 只在这个 id 确实存在于账户列表里时才带上:后端对未知 account 直接 400,
  // 而 localStorage 里可能留着已删账户的 id,那会让整页变成一条红错。
  const [activeId] = useActiveServerControlAccount();
  const { data: accounts, isPending: accountsPending } = useAccounts();
  const accountId = findAccountByID(accounts, activeId) ? activeId : "";
  const key = qk.servers.list(showApiServers, accountId);
  const q = useQuery({
    queryKey: key,
    queryFn: async () => {
      const params: Record<string, unknown> = { showApiServers };
      if (accountId) params.account = accountId;
      const res = await api.get("/servers", { params });
      return (res.data.servers || res.data || []) as ServerPlan[];
    },
    // 账户列表还没到手时不发请求:否则会先按默认视角拉一份完整目录(后端要打 ~100 次 OVH),
    // 紧接着 key 变了再拉一次。等一下就好,这个查询本来就有 2 小时缓存。
    enabled: !accountsPending,
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
