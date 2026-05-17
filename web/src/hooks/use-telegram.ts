import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export interface TelegramVerifyResult {
  ok: boolean;
  reason: string;
}

/**
 * 查 Telegram 通知是否配置且有效。
 * 后端 GET /api/telegram/verify 真去 telegram API getMe + getChat 探一下,返回 {ok, reason}。
 * 监控订阅、批量订阅都强依赖这个,无效则不允许添加(后端会 400 拦截,前端预先 disable 提交按钮)。
 *
 * - staleTime 5min:对话框反复打开不会反复打 telegram API
 * - 修改 TG 设置时通过 invalidateQueries(['telegram','verify']) 强制刷
 */
export function useTelegramVerify() {
  return useQuery<TelegramVerifyResult>({
    queryKey: ["telegram", "verify"],
    queryFn: async () => (await api.get<TelegramVerifyResult>("/telegram/verify")).data,
    staleTime: 5 * 60 * 1000,
    gcTime: 30 * 60 * 1000,
    retry: 0,
    refetchOnWindowFocus: false,
  });
}
