import { useTranslation } from "react-i18next";
import type { QueueItem, QueueStatus } from "@/hooks/use-queue";
import { cn } from "@/lib/utils";
import { CheckCircle2, Clock, Layers, PauseCircle, XCircle, Zap, type LucideIcon } from "lucide-react";

export type QueueFilter = "all" | QueueStatus;

/** 筛选顺序:运行中是默认视图,放第一个;全部放最后。 */
export const QUEUE_FILTERS = ["running", "paused", "completed", "failed", "all"] as const;

/** 该筛选下的任务数;运行中包含待执行。 */
export function countForFilter(items: QueueItem[], status: QueueFilter): number {
  return items.filter((item) => status === "all" || item.status === status || (status === "running" && item.status === "pending")).length;
}

const TILE: Record<QueueFilter, { icon: LucideIcon; tint: string }> = {
  running: { icon: Zap, tint: "bg-success/[.12] text-success" },
  pending: { icon: Clock, tint: "bg-muted text-muted-foreground" },
  paused: { icon: PauseCircle, tint: "bg-warning/[.12] text-warning" },
  completed: { icon: CheckCircle2, tint: "bg-primary/10 text-primary" },
  failed: { icon: XCircle, tint: "bg-destructive/10 text-destructive" },
  all: { icon: Layers, tint: "bg-muted text-foreground/70" },
};

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
  return (
    <div className="min-w-0">
      <div className="bw-stagger grid grid-cols-5 gap-1.5 sm:gap-3" role="group" aria-label={t("queue.filterLabel")}>
        {QUEUE_FILTERS.map((status) => {
          const active = value === status;
          const { icon: Icon, tint } = TILE[status];
          return (
            <button
              key={status}
              type="button"
              aria-pressed={active}
              onClick={() => onChange(status)}
              className={cn(
                "group flex flex-col sm:flex-row items-center sm:items-center gap-1 sm:gap-3 rounded-xl bg-card px-1.5 py-2 sm:px-4 sm:py-3 text-center sm:text-left transition-all min-w-0",
                "shadow-[var(--soft-1)] ring-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                active ? "ring-primary/70 bg-primary/[.04] shadow-[0_4px_14px_-6px_rgba(31,107,255,.45)]" : "ring-transparent hover:ring-border hover:-translate-y-px",
              )}
            >
              <span className={cn("hidden md:flex w-9 h-9 rounded-full items-center justify-center shrink-0", tint)}>
                <Icon className="w-[18px] h-[18px]" />
              </span>
              <span className="flex flex-col min-w-0 max-w-full">
                <span className={cn("text-[11px] sm:text-[12.5px] truncate", active ? "text-primary font-medium" : "text-muted-foreground")}>{status === "all" ? t("queue.allTasks") : t(`queue.status.${status}`)}</span><span className="text-[18px] sm:text-[22px] leading-6 sm:leading-7 font-semibold tabular-nums tracking-tight text-foreground">{countForFilter(items, status)}</span>
              </span>
            </button>
          );
        })}
      </div>
      <p className="sr-only">{t("queue.retainedHint")}</p>
    </div>
  );
}
