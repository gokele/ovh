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
 * 拼接类动态消息(含 OVH 原文的那些)后端不带 code,始终透传 —— 那类
 * 文案本来就以后端为准。
 */
export function apiMessage(e: unknown): string {
  const err = e as AxiosError<{ error?: string; message?: string; code?: string; params?: Record<string, unknown> }>;
  const data = err?.response?.data;
  const raw = data?.error || data?.message || err?.message || "";

  if (data?.code) {
    const key = `api.${data.code}`;
    if (i18n.exists(key)) {
      // 结构化校验错误带 params:按当前语言插值出译文(值可能是数字)
      const params = data.params as Record<string, string | number> | undefined;
      if (params && Object.keys(params).length > 0) {
        return i18n.t(key, params) as string;
      }
      return i18n.t(key) as string;
    }
  }
  // 网络层错误(请求根本没到后端)原文是英文技术串,给可读的通用文案
  if (!data && err?.code === "ERR_NETWORK") {
    return i18n.t("common.networkError") as string;
  }
  return raw;
}

/** 同 apiMessage,但只处理后端成功响应体里的 message(部分接口 200 也带提示) */
export function bodyMessage(data: { message?: string; code?: string } | undefined | null): string | undefined {
  if (!data) return undefined;
  if (data.code) {
    const key = `api.${data.code}`;
    if (i18n.exists(key)) return i18n.t(key) as string;
  }
  return data.message || undefined;
}
