import { useTranslation } from "react-i18next";
import type { QueueItem, QueueStatus } from "@/hooks/use-queue";
import { cn } from "@/lib/utils";

export type QueueFilter = "all" | QueueStatus;

/** 筛选顺序:运行中是默认视图,放第一个;全部放最后。 */
export const QUEUE_FILTERS = ["running", "paused", "completed", "failed", "all"] as const;

/** 该筛选下的任务数;运行中包含待执行。 */
export function countForFilter(items: QueueItem[], status: QueueFilter): number {
  return items.filter((item) => status === "all" || item.status === status || (status === "running" && item.status === "pending")).length;
}

/** 紧凑分段筛选,显示各状态数量;已下单记录保留,可单独查看。 */
export function QueueStatusFilter({
  items,
  value,
  onChange,
}: {
  items: QueueItem[];
  value: QueueFilter;
  onChange: (value: QueueFilter) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="min-w-0">
      <div
        className="inline-flex flex-wrap max-w-full rounded-lg border border-border bg-muted/40 p-0.5"
        role="group"
        aria-label={t("queue.filterLabel")}
      >
        {QUEUE_FILTERS.map((status) => (
          <button
            key={status}
            type="button"
            aria-pressed={value === status}
            onClick={() => onChange(status)}
            className={cn(
              "inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md px-3 h-8 text-[13px] transition-colors",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              value === status ? "bg-background text-foreground shadow-sm font-medium" : "text-muted-foreground hover:text-foreground",
            )}
          >
            <span>{status === "all" ? t("queue.allTasks") : t(`queue.status.${status}`)}</span>
            <span className="tabular-nums text-[12px] opacity-70">{countForFilter(items, status)}</span>
          </button>
        ))}
      </div>
      <p className="sr-only">{t("queue.retainedHint")}</p>
    </div>
  );
}
