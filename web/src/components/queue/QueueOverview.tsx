import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { PauseCircle, PlayCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { QueueItem } from "@/hooks/use-queue";
import { countOrders } from "@/components/queue/QueueTable";
import { cn } from "@/lib/utils";

/**
 * 抢购队列顶部的实时概览:现在有没有在抢(几单几台)、累计尝试、已下单、已暂停,以及数据多久前刷新。
 * 「全部暂停 / 全部恢复」放在这里,跟它作用的状态挨着;页头只留「新建」一个主按钮。
 */
export function QueueOverview({
  items,
  updatedAt,
  fetching,
  busy,
  onPauseAll,
  onResumeAll,
}: {
  items: QueueItem[];
  updatedAt: number;
  fetching: boolean;
  busy?: boolean;
  onPauseAll: (targets: QueueItem[]) => void;
  onResumeAll: (targets: QueueItem[]) => void;
}) {
  const { t } = useTranslation();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);
  const live = items.filter((i) => i.status === "running" || i.status === "pending");
  const pausable = live.filter((i) => i.stopReason !== "groupComplete");
  const resumable = items.filter((i) => i.status === "paused" && i.stopReason !== "groupComplete");
  const attempts = items.reduce((n, i) => n + (i.retryCount || 0), 0);
  const ordered = items.filter((i) => i.status === "completed").length;
  const paused = items.filter((i) => i.status === "paused").length;
  const ago = updatedAt ? Math.max(0, Math.round((now - updatedAt) / 1000)) : null;
  const on = live.length > 0;

  const stat = (label: string, value: string | number, tone?: string) => (
    <div className="min-w-0">
      <div className="text-[12px] text-muted-foreground">{label}</div>
      <div className={cn("mt-0.5 text-[20px] leading-7 font-semibold tabular-nums tracking-tight", tone)}>{value}</div>
    </div>
  );

  return (
    <section
      aria-label={t("queue.overview.label")}
      className="rounded-2xl border border-border bg-card px-4 py-4 sm:px-5 grid gap-4 sm:grid-cols-[minmax(0,1.3fr)_minmax(0,2fr)_auto] sm:items-center"
    >
      <div className="flex items-center gap-3 min-w-0">
        <span className={cn("relative grid place-items-center w-10 h-10 rounded-full shrink-0", on ? "bg-success/10 text-success" : "bg-muted text-muted-foreground")}>
          {on && <span className="absolute inset-0 rounded-full bg-success/20 animate-ping motion-reduce:hidden" aria-hidden />}
          <span className={cn("relative w-2.5 h-2.5 rounded-full", on ? "bg-success" : "bg-muted-foreground/50")} aria-hidden />
        </span>
        <div className="min-w-0">
          <div className="text-[15px] font-semibold">
            {on ? t("queue.overview.running", { orders: countOrders(live), tasks: live.length }) : t("queue.overview.idle")}
          </div>
          <div className="text-[12px] text-muted-foreground" aria-live="off">
            {fetching ? t("queue.row.refreshing") : ago === null ? "" : ago < 2 ? t("queue.overview.justNow") : t("queue.overview.updatedAgo", { s: ago })}
            {!on && resumable.length > 0 ? ` · ${t("queue.overview.idleHint")}` : ""}
          </div>
        </div>
      </div>
      <div className="grid grid-cols-3 gap-3 sm:border-l sm:border-border sm:pl-5">
        {stat(t("queue.overview.attempts"), attempts.toLocaleString())}
        {stat(t("queue.overview.ordered"), ordered, ordered > 0 ? "text-success" : undefined)}
        {stat(t("queue.overview.paused"), paused, paused > 0 ? "text-warning" : undefined)}
      </div>
      <div className="flex gap-2 sm:justify-end">
        {pausable.length > 0 && (
          <Button variant="outline" className="flex-1 sm:flex-none" disabled={busy} onClick={() => onPauseAll(pausable)}>
            <PauseCircle className="w-4 h-4" />
            {t("queue.pauseAllTasks")}
          </Button>
        )}
        {resumable.length > 0 && (
          <Button variant="outline" className="flex-1 sm:flex-none text-primary border-primary/40" disabled={busy} onClick={() => onResumeAll(resumable)}>
            <PlayCircle className="w-4 h-4" />
            {t("queue.resumeAllTasks")}
            <span className="tabular-nums opacity-70">{resumable.length}</span>
          </Button>
        )}
      </div>
    </section>
  );
}
