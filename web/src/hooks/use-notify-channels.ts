import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { toast } from "sonner";
import i18n from "@/i18n";
import { apiMessage } from "@/lib/api-error";

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
  // 形状不对(缺 channels / anyAvailable)按"无法判断"处理,不拦截 ——
  // 这只是前端预检,后端在提交时还有真正的强校验;宁可放行到后端,
  // 也不要因为一次响应抖动把订阅入口整个锁死,更不能在这里崩掉整页
  // (这个 hook 被 AddSubscriptionDialog 常驻调用,崩 = 监控页全白)
  if (!q.data || !Array.isArray(q.data.channels) || typeof q.data.anyAvailable !== "boolean") {
    return [false, "", false];
  }
  if (q.data.anyAvailable) return [false, "", false];
  const detail = q.data.channels
    .filter((c) => c.configured && !c.ok)
    .map((c) => i18n.t("hooksMsg.notify.channelLine", { name: c.name, detail: c.detail || i18n.t("hooksMsg.notify.channelUnavailable") }))
    .join(i18n.t("hooksMsg.notify.channelSeparator"));
  return [true, detail || i18n.t("hooksMsg.notify.noChannelConfigured"), false];
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
        toast.error(d.message || i18n.t("hooksMsg.notify.noneDelivered"));
      } else if (failed.length) {
        toast.warning(
          i18n.t("hooksMsg.notify.partialDelivered", {
            delivered: d.delivered,
            names: failed.map((c) => c.name).join(i18n.t("hooksMsg.notify.nameSeparator")),
            reasons: failed
              .map((c) => c.detail || i18n.t("hooksMsg.notify.unknownReason"))
              .join(i18n.t("hooksMsg.notify.channelSeparator")),
          })
        );
      } else {
        toast.success(d.message || i18n.t("hooksMsg.notify.sentToChannels", { count: d.delivered }));
      }
    },
    onError: (e: any) => toast.error(apiMessage(e) || i18n.t("hooksMsg.notify.sendFailed")),
  });
}
