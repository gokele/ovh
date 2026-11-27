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


/**
 * 状态卡片兼筛选:每张卡显示该状态的任务数,点击切换筛选,当前项蓝色描边。
 * 运行中是默认视图,放第一个;全部放最后。已下单记录保留,可单独查看。
 */
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
  const order = ["all", "running", "paused", "failed", "completed"] as const;
  return (
    <div className="flex flex-wrap gap-1.5" role="group" aria-label={t("queue.filterLabel")}>
      {order.map((status) => {
        const active = value === status;
        const n = countForFilter(items, status);
        if (n === 0 && !active && status !== "all") return null;
        return (
          <button
            key={status}
            type="button"
            aria-pressed={active}
            onClick={() => onChange(status)}
            className={cn(
              "inline-flex items-center gap-1.5 h-8 px-3 rounded-full text-[13px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              active ? "bg-primary text-primary-foreground" : "bg-card ring-1 ring-border text-muted-foreground hover:text-foreground",
            )}
          >
            <span>{status === "all" ? t("queue.allTasks") : t(`queue.bundle.section.${status}`)}</span>
            <span className={cn("tabular-nums text-[12px]", active ? "opacity-90" : "text-foreground/70")}>{n}</span>
          </button>
        );
      })}
      <p className="sr-only">{t("queue.retainedHint")}</p>
    </div>
  );
}
