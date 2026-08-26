import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { toast } from "sonner";

export type QueueStatus = "pending" | "running" | "paused" | "completed" | "failed";

export interface QueueItem {
  id: string;
  accountId: string;
  planCode: string;
  datacenter: string;
  /** 下单成功后用 OVH 默认支付方式自动付款(显式开关,默认关) */
  autoPay?: boolean;
  options: string[];
  status: QueueStatus;
  createdAt: string;
  updatedAt: string;
  retryInterval: number;
  retryCount: number;
  /** 真正提交给 OVH 并失败的次数（无货的轮次不计）。后端按它封顶重试。 */
  failureCount?: number;
  /** 后端 types.QueueItem 还会传回这几个字段（多为 omitempty），前端目前不渲染但保留类型对齐 */
  maxRetries?: number;
  lastCheckTime?: number;
  quickOrder?: boolean;
  priority?: number;
  fromTelegram?: boolean;
  configSniperTaskId?: string;
}

/** 抢购队列列表 */
export function useQueueList() {
  return useQuery({
    queryKey: qk.queue.list(),
    queryFn: async () => (await api.get<QueueItem[]>("/queue")).data,
    refetchInterval: 5000,
  });
}

export interface PurchaseTiming {
  at: string;
  totalMs: number;
  phases: { name: string; ms: number }[];
  /** ordered = 真的下单了；unavailable = 那一轮没货；failed = 中途出错 */
  outcome: "ordered" | "unavailable" | "failed";
}

/**
 * 每条抢购链路（机型@机房）最近一轮的阶段耗时。
 *
 * 抢购还在跑的时候，用户最想知道的是"我到底卡在哪一步" ——
 * 是 OVH 一直没货（那就换机型），还是每轮建购物车要 3 秒（那就换台机器）。
 * 轮询频率和队列一致，多一个请求换一个能直接采取行动的答案。
 */
export function usePurchaseTimings() {
  return useQuery({
    queryKey: ["queue", "timings"],
    queryFn: async () =>
      (await api.get<{ timings: Record<string, PurchaseTiming> }>("/queue/timings")).data.timings,
    refetchInterval: 5000,
  });
}

/**
 * 批量创建抢购任务：对每个 datacenter × quantity 调用 POST /queue。
 * 返回成功 / 失败计数。
 *
 * 多账户:account_id 必填,后端据此决定下单走哪个账户,以及购物车的 ovhSubsidiary。
 */
export function useCreateQueueItem() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (payload: {
      account_id: string;
      planCode: string;
      datacenters: string[];
      options?: string[];
      retryInterval?: number;
      quantity?: number;
      autoPay?: boolean;
    }) => {
      const qty = Math.max(1, payload.quantity ?? 1);
      const dcs = payload.datacenters;
      let success = 0;
      let failed = 0;
      for (const dc of dcs) {
        for (let i = 0; i < qty; i++) {
          try {
            await api.post("/queue", {
              account_id: payload.account_id,
              planCode: payload.planCode,
              datacenter: dc,
              retryInterval: payload.retryInterval,
              options: payload.options || [],
              autoPay: payload.autoPay ?? false,
            });
            success++;
          } catch (e) {
            failed++;
          }
        }
      }
      return { success, failed, total: dcs.length * qty };
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.queue.list() });
      qc.invalidateQueries({ queryKey: qk.stats() });
    },
    onError: (e: any) => toast.error(e.response?.data?.error || "添加任务失败"),
  });
}

/** 切换任务状态（暂停 / 恢复） */
export function useToggleQueueItem() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, action }: { id: string; action: "pause" | "resume" }) =>
      (await api.put(`/queue/${id}/status`, { status: action === "pause" ? "paused" : "running" })).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.queue.list() }),
    onError: (e: any) => toast.error(e.response?.data?.error || "操作失败"),
  });
}

/** 删除单个任务 */
export function useRemoveQueueItem() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => (await api.delete(`/queue/${id}`)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.queue.list() });
      qc.invalidateQueries({ queryKey: qk.stats() });
    },
    onError: (e: any) => toast.error(e.response?.data?.error || "删除失败"),
  });
}

/** 清空所有任务 */
export function useClearQueue() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => (await api.delete("/queue/clear")).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.queue.list() });
      qc.invalidateQueries({ queryKey: qk.stats() });
      toast.success("已清空队列");
    },
    onError: (e: any) => toast.error(e.response?.data?.error || "清空失败"),
  });
}
