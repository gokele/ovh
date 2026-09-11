import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { errorMessage } from "@/components/common/LoadFailed";
import { toast } from "sonner";

export interface SettingsConfig {
  appKey?: string;
  appSecret?: string;
  consumerKey?: string;
  endpoint?: string;
  zone?: string;
  iam?: string;
  tgToken?: string;
  tgChatId?: string;
  /** Telegram 回调地址：Telegram 把用户点按钮的动作推到这里（进） */
  /** 自定义通知地址：补货/下单结果由本程序 POST 到这里（出）。和上面那个方向相反 */
  notifyWebhookUrl?: string;
  /** 新建抢购任务的默认重试间隔（秒）。网页弹窗 / TG /buy / 一键下单按钮都用它 */
  defaultRetryInterval?: number;
  /** 监控触发的自动下单用的重试间隔（秒）。货刚出现那一刻窗口很窄，默认比普通任务激进 */
  quickOrderRetryInterval?: number;
}

/** 重试间隔的合法区间与默认值，与后端 types.ClampRetryInterval 一致 */
export const RETRY_INTERVAL = {
  min: 1,
  max: 86400,
  defaultTask: 60,
  defaultQuick: 2,
} as const;

/** 读取后端 config */
export function useSettings() {
  return useQuery({
    queryKey: qk.settings.config(),
    queryFn: async () => (await api.get<SettingsConfig>("/settings")).data,
  });
}

/** 保存 config */
export function useSaveSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (payload: SettingsConfig) => (await api.post("/settings", payload)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.settings.config() });
      // TG 配置可能变了,让监控对话框下次打开重新 verify
      qc.invalidateQueries({ queryKey: ["telegram", "verify"] });
      toast.success("设置已保存");
    },
    onError: (e: any) => toast.error(e.response?.data?.error || "保存失败"),
  });
}

/** 缓存信息 */
export function useCacheInfo() {
  return useQuery({
    queryKey: qk.settings.cacheInfo(),
    queryFn: async () => (await api.get("/cache/info")).data,
  });
}

export function useClearCache() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (type: "all" | "memory" | "sqlite") =>
      (await api.post("/cache/clear", { type })).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.settings.cacheInfo() });
      toast.success("已清除缓存");
    },
    onError: (e: any) => toast.error(e.response?.data?.error || "清除失败"),
  });
}

/**
 * 长轮询收取器的运行快照。
 *
 * 注意整个对象是可能缺的(后端 poller 没初始化) —— 缺失是"没问到状态",
 * 不等于 running:false。这两件事混在一起,用户会以为轮询停了而去反复重启。
 */
export interface TelegramPollerStatus {
  running: boolean;
  /** 已确认到的 update_id,落库的,重启不会重放旧消息 */
  offset: number;
  lastError: string;
  lastPollAt?: string;
}

export interface TelegramPollerInfo {
  /** 后端**已保存**的配置里有没有 Bot Token。输入框里刚敲进去还没保存的不算 */
  hasToken: boolean;
  poller?: TelegramPollerStatus;
}

/**
 * 长轮询的运行状态。
 *
 * 这是收 Telegram 消息的唯一一条路(webhook 已经删掉了),它停了就等于
 * 一键下单、文本下单、所有命令全部失效 —— 而界面上不会有任何别的迹象。
 * 所以让它自己刷,别指望用户想起来点刷新;
 * "同一个 Token 有另一个进程也在拉"这种冲突后端只写日志,界面上只有这里看得见。
 */
export function useTelegramPoller() {
  return useQuery({
    queryKey: qk.settings.telegramPoller(),
    queryFn: async () => {
      const res = await api.get<{ success: boolean; error?: string } & TelegramPollerInfo>(
        "/telegram/poller"
      );
      if (!res.data?.success) throw new Error(res.data?.error || "读取长轮询状态失败");
      return res.data as TelegramPollerInfo;
    },
    refetchInterval: 10_000,
  });
}
