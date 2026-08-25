/**
 * 「部分失败」列表的统一形状。
 *
 * 后端很多列表接口是「先拿主键数组，再并发拉每条详情」，其中若干条详情拉挂（限流 / 超时 / 权限）
 * 以前是被静默丢弃或补空值的，用户只会看到列表莫名变短或字段全空。后端这一轮改成如实上报
 * （body 里的 partial / failedCount / failed，或裸数组接口的 X-Partial-Failures 响应头），
 * 前端就必须有个地方接住它，否则这些信息到不了 UI，等于没修。
 */
export interface PartialList<T> {
  /** 本次真正拿到的行（失败行后端仍会保留占位，带 _detailError / error 字段） */
  items: T[];
  /** true 表示至少有一行的详情没拉到，列表可能不完整 */
  partial: boolean;
  /** 详情拉取失败的行数 */
  failedCount: number;
}

/**
 * 读 X-Partial-Failures 响应头。
 * refunds / email-history 这类接口的 body 是裸数组，后端没法往 body 里加字段，只能放响应头。
 * 浏览器里的 header key 一律小写；同源请求不需要 Access-Control-Expose-Headers。
 */
export function readPartialFailures(headers: unknown): number {
  const raw = (headers as Record<string, unknown> | undefined)?.["x-partial-failures"];
  const n = Number(raw);
  return Number.isFinite(n) && n > 0 ? n : 0;
}

/** 把「裸数组 + X-Partial-Failures 头」的响应包成 PartialList */
export function toPartialList<T>(items: T[] | undefined, failedCount: number): PartialList<T> {
  return { items: items || [], partial: failedCount > 0, failedCount };
}
