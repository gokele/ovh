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
