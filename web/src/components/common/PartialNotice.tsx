import { AlertTriangle } from "lucide-react";

/**
 * 「部分数据获取失败」提示条。
 *
 * 后端这一轮把「主键列表 + 并发拉详情」类接口的失败如实上报了（partial / failedCount /
 * X-Partial-Failures）。前端如果不显示，用户看到的仍然只是一个莫名变短、或者某几行字段
 * 全空的列表，会当成「OVH 里就是没有」——这正是要修的误导。所以凡是消费 PartialList 的
 * 地方都挂一条这个提示，明确告诉用户「这次少查到了 N 条，可以刷新重试」。
 */
export function PartialNotice({
  failedCount,
  what,
  className,
}: {
  failedCount: number;
  /** 这批数据叫什么，用于拼文案，例如「网卡接口」 */
  what: string;
  className?: string;
}) {
  if (!failedCount || failedCount <= 0) return null;
  return (
    <div
      className={
        "flex items-start gap-2 rounded-xl border border-warning/40 bg-warning/5 px-3 py-2 text-[11px] text-foreground/80 " +
        (className || "")
      }
    >
      <AlertTriangle className="w-3.5 h-3.5 text-warning flex-shrink-0 mt-0.5" />
      <span>
        {what}有 {failedCount} 条详情未能获取，下方列表可能不完整或存在占位行，可刷新重试。
      </span>
    </div>
  );
}

/** 单行「详情获取失败」标记。列表里那一行是占位数据时挂它，避免用户把占位值当真实状态。 */
export function DetailErrorTag({ message }: { message?: string }) {
  if (!message) return null;
  return (
    <span
      className="inline-flex items-center gap-1 text-[10.5px] text-destructive whitespace-nowrap"
      title={message}
    >
      <AlertTriangle className="w-3 h-3" />
      获取失败
    </span>
  );
}
