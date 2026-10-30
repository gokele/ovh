import i18n from "@/i18n";
import type { AxiosError } from "axios";

/**
 * 后端消息翻译层。
 *
 * 后端(Go)在返回 error/message 的同时带稳定错误码 `code`;这里按
 * `api.<CODE>` 查当前语言的译文。约定:
 *   - 有 code 且语言包有对应 key → 译文
 *   - 有 code 但没翻译 → 原文(Go 端的中文默认值),渐进迁移不炸
 *   - 没有 code → 原文;网络层错误(断网/超时)给通用文案
 * 动态消息可通过 params 插值:只翻译系统提示,OVH 原文保留在 detail 中。
 */
export function apiMessage(e: unknown): string {
  const err = e as AxiosError<{
    error?: string;
    message?: string;
    code?: string;
    params?: Record<string, unknown>;
  }>;
  const data = err?.response?.data;
  const raw = data?.error || data?.message || err?.message || "";

  const translated = bodyMessage(data);
  if (translated) return translated;
  // 网络层错误(请求根本没到后端)原文是英文技术串,给可读的通用文案
  if (!data && (err?.code === "ERR_NETWORK" || err?.code === "ECONNABORTED")) {
    return i18n.t("common.networkError") as string;
  }
  return raw;
}

/** 按当前语言显示响应体提示,包括 200 列表中的单条错误及插值参数。 */
export function bodyMessage(
  data:
    | {
        error?: string;
        message?: string;
        code?: string;
        params?: Record<string, unknown>;
      }
    | undefined
    | null,
): string | undefined {
  if (!data) return undefined;
  if (data.code) {
    const key = `api.${data.code}`;
    if (i18n.exists(key)) return i18n.t(key, data.params || {}) as string;
  }
  return data.error || data.message || undefined;
}
