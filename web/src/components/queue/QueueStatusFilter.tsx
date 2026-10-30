import { useTranslation } from "react-i18next";
import type { QueueItem, QueueStatus } from "@/hooks/use-queue";

export type QueueFilter = "all" | QueueStatus;

/** 显示各状态数量,已下单记录默认保留,可单独筛选正在抢购的任务。 */
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
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2" aria-label={t("queue.filterLabel")}>
        {(["all", "running", "paused", "completed", "failed"] as const).map(
          (status) => {
            const count = items.filter(
              (item) =>
                status === "all" ||
                item.status === status ||
                (status === "running" && item.status === "pending"),
            ).length;
            return (
              <button
                key={status}
                type="button"
                aria-pressed={value === status}
                onClick={() => onChange(status)}
                className={`inline-flex min-h-11 items-center gap-2 rounded-xl border px-4 text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${value === status ? "border-foreground bg-foreground text-background" : "border-border bg-card text-muted-foreground hover:bg-muted hover:text-foreground"}`}
              >
                <span>
                  {status === "all"
                    ? t("queue.allTasks")
                    : t(`queue.status.${status}`)}
                </span>
                <span className="tabular-nums opacity-80">{count}</span>
              </button>
            );
          },
        )}
      </div>
      <p className="text-xs leading-5 text-muted-foreground">
        {t("queue.retainedHint")}
      </p>
    </div>
  );
}
