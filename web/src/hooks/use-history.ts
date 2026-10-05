import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { toast } from "sonner";
import i18n from "@/i18n";
import { apiMessage } from "@/lib/api-error";

export interface PurchaseHistory {
  id: string;
  accountId: string;
  /** 关联的抢购队列任务 ID（后端 PurchaseHistoryEntry.taskId） */
  taskId?: string;
  planCode: string;
  datacenter: string;
  options?: string[];
  status: "success" | "failed";
  orderId?: string;
  orderUrl?: string;
  errorMessage?: string;
  purchaseTime: string;
  /** 抢购到这单时一共尝试了几次（后端 attemptCount） */
  attemptCount?: number;
  expirationTime?: string;
  /** 各阶段墙钟耗时。抢购输了之后唯一有用的信息就是"慢在哪一步" */
  timing?: { name: string; ms: number }[];
  totalMs?: number;
  /** OVH 侧订单状态(billing.order.OrderStatusEnum):notPaid / checking / delivering /
   *  delivered / cancelling / cancelled / documentsRequested / unknown。
   *  没有它,"下单成功"到底付没付永远不知道。空 = 还没查到 */
  orderStatus?: string;
  orderStatusAt?: string;
  price?: {
    withTax?: number;
    withoutTax?: number;
    tax?: number;
    currencyCode?: string;
  };
}

/** 抢购历史 */
export function useHistory() {
  return useQuery({
    queryKey: qk.history(),
    // Array.isArray:调用方直接 items.filter,truthy 非数组(代理层改写响应等)
    // 会整页白屏 —— 宁可当空列表走"没有历史"的空态
    queryFn: async () => {
      const d = (await api.get<PurchaseHistory[]>("/purchase-history")).data;
      return Array.isArray(d) ? d : [];
    },
  });
}

/**
 * 手动刷新所有未到终态订单的支付状态(后台每 10 分钟也会自动刷)。
 * 给"我刚付完款想马上看到"的场景。
 */
export function useRefreshOrderStatus() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () =>
      (await api.post<{ success: boolean; updated: number }>("/purchase-history/refresh-status")).data,
    onSuccess: (d) => {
      qc.invalidateQueries({ queryKey: qk.history() });
      toast.success(
        d.updated > 0
          ? i18n.t("hooksMsg.history.statusUpdated", { count: d.updated })
          : i18n.t("hooksMsg.history.statusAlreadyLatest")
      );
    },
    onError: (e: any) => toast.error(apiMessage(e) || i18n.t("hooksMsg.history.refreshStatusFailed")),
  });
}

/** 清空抢购历史 */
export function useClearHistory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => (await api.delete("/purchase-history")).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.history() });
      toast.success(i18n.t("hooksMsg.history.cleared"));
    },
    onError: (e: any) => toast.error(apiMessage(e) || i18n.t("hooksMsg.history.clearFailed")),
  });
}
