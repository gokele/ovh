import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { toast } from "sonner";

export interface NotifyChannel {
  name: string;
  configured: boolean;
  ok: boolean;
  detail?: string;
}

export interface NotifyChannelsResult {
  channels: NotifyChannel[];
  anyAvailable: boolean;
  verified: boolean;
}

/**
 * 通知通道体检。
 *
 * 为什么不再直接用 useTelegramVerify 做订阅门禁：订阅现在只要求「至少有一条通道能用」。
 * 只看 Telegram 的话，一个只配了 webhook 的用户会被前端拦住，
 * 而后端其实是放行的——用户会看到一个自己无论如何都消不掉的报错。
 */
export function useNotifyChannels(verify = true) {
  return useQuery<NotifyChannelsResult>({
    queryKey: ["notify", "channels", verify],
    queryFn: async () =>
      (await api.get<NotifyChannelsResult>(`/notify/channels?verify=${verify}`)).data,
    staleTime: 5 * 60 * 1000,
    gcTime: 30 * 60 * 1000,
    retry: 0,
    refetchOnWindowFocus: false,
  });
}

/** 订阅门禁：返回 [是否拦截, 原因, 是否还在查] */
export function useNotifyGate(): [boolean, string, boolean] {
  const q = useNotifyChannels(true);
  if (q.isPending) return [false, "", true];
  if (!q.data) return [false, "", false];
  if (q.data.anyAvailable) return [false, "", false];
  const detail = q.data.channels
    .filter((c) => c.configured && !c.ok)
    .map((c) => `${c.name}: ${c.detail || "不可用"}`)
    .join("；");
  return [true, detail || "还没有配置任何通知通道（Telegram / Webhook 至少配一个）", false];
}

/**
 * 发一条测试通知到所有已配置的通道。
 *
 * 后端返回每条通道各自的结果，所以这里不做"成功/失败"的二元判断 ——
 * 两条通道配了、只有一条通得了，那既不是成功也不是失败，用户需要知道是哪条挂了。
 */
export function useTestNotification() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () =>
      (
        await api.post<{ delivered: number; message: string; channels: NotifyChannel[] }>(
          "/monitor/test-notification"
        )
      ).data,
    onSuccess: (d) => {
      // 测试本身就是一次最真实的体检，顺手刷新上面的状态
      qc.invalidateQueries({ queryKey: ["notify", "channels"] });
      const failed = (d.channels || []).filter((c) => c.configured && !c.ok);
      if (d.delivered === 0) {
        toast.error(d.message || "一条都没发出去");
      } else if (failed.length) {
        toast.warning(
          `${d.delivered} 条已送达，但 ${failed.map((c) => c.name).join("、")} 失败：` +
            failed.map((c) => c.detail || "未知原因").join("；")
        );
      } else {
        toast.success(d.message || `已发往 ${d.delivered} 个通道`);
      }
    },
    onError: (e: any) =>
      toast.error(e.response?.data?.message || e.response?.data?.error || "发送失败"),
  });
}
