import { Link } from "@tanstack/react-router";
import { ArrowRight, Bell, Bot, Link2, Server } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { fmtDateTime } from "@/i18n/format";
import { useStats } from "@/hooks/use-stats";
import { useQueueList, type QueueStatus } from "@/hooks/use-queue";
import { useSystemMetrics, useAppVersion, useUpdateCheck } from "@/hooks/use-system-metrics";
import { UpdateButton } from "@/components/common/UpdateButton";
import { StatusDot } from "@/components/common/StatusDot";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

/**
 * 仪表盘(克制版):
 *   第一行 4 个读数卡:活跃队列 / 下单成功 / 下单失败 / 服务器(可用/总数)
 *   第二行 最近队列(宽) + 系统状态与版本(窄)
 *   第三行 宿主机资源:CPU / 内存 / 磁盘 三条细进度条
 * 所有读数在未读到时显示 —,不把 0 当作缺省值。
 */

function fmtBytes(n: number): string {
  if (n >= 1024 ** 3) return `${(n / 1024 ** 3).toFixed(1)}G`;
  if (n >= 1024 ** 2) return `${(n / 1024 ** 2).toFixed(0)}M`;
  return `${(n / 1024).toFixed(0)}K`;
}

const QUEUE_TONE: Record<QueueStatus, "success" | "warning" | "muted" | "danger" | "info"> = {
  running: "success",
  pending: "muted",
  paused: "warning",
  completed: "info",
  failed: "danger",
};

function Kpi({
  label,
  value,
  sub,
  to,
  tone,
}: {
  label: string;
  value: ReactNode;
  sub?: ReactNode;
  to?: "/queue" | "/history" | "/servers";
  tone?: "danger" | "success";
}) {
  const body = (
    <>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div
        className={cn(
          "mt-1 text-2xl font-semibold tabular-nums tracking-tight",
          tone === "danger" && "text-destructive",
          tone === "success" && "text-success",
        )}
      >
        {value}
      </div>
      {sub && <div className="mt-1 text-[11px] text-muted-foreground truncate">{sub}</div>}
    </>
  );
  return (
    <Card className="p-4 min-w-0">
      {to ? (
        <Link to={to} className="block rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
          {body}
        </Link>
      ) : (
        body
      )}
    </Card>
  );
}

function StatusLine({
  icon,
  label,
  on,
  onText,
  offText,
  unknown,
}: {
  icon: ReactNode;
  label: string;
  on: boolean;
  onText: string;
  offText: string;
  unknown?: boolean;
}) {
  return (
    <div className="flex items-center gap-3 py-2">
      <span className="w-7 h-7 rounded-md bg-secondary flex items-center justify-center text-muted-foreground shrink-0">{icon}</span>
      <span className="flex-1 min-w-0 text-[13px] truncate">{label}</span>
      <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground whitespace-nowrap">
        <StatusDot tone={unknown ? "muted" : on ? "success" : "warning"} size="xs" pulse={on && !unknown} />
        {unknown ? "…" : on ? onText : offText}
      </span>
    </div>
  );
}

function MeterRow({ label, percent, detail }: { label: string; percent?: number; detail?: string }) {
  const { t } = useTranslation();
  const known = typeof percent === "number" && Number.isFinite(percent);
  const p = known ? Math.max(0, Math.min(100, percent)) : 0;
  const tone = p >= 90 ? "bg-destructive" : p >= 75 ? "bg-warning" : "bg-foreground/70";
  return (
    <div className="min-w-0">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-[13px] font-medium">{label}</span>
        <span className="text-[13px] tabular-nums" title={known ? undefined : t("dashboard.valueUnknown")}>
          {known ? `${p.toFixed(0)}%` : "—"}
        </span>
      </div>
      <div className="mt-1.5 h-1.5 rounded-full bg-muted overflow-hidden" role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={known ? Math.round(p) : undefined}>
        <div className={cn("h-full rounded-full transition-[width]", tone)} style={{ width: `${p}%` }} />
      </div>
      {detail && <div className="mt-1 text-[11px] text-muted-foreground whitespace-nowrap overflow-hidden text-ellipsis">{detail}</div>}
    </div>
  );
}

export function DashboardPearl() {
  const { t } = useTranslation();
  const stats = useStats();
  const queue = useQueueList();
  const sys = useSystemMetrics();
  const version = useAppVersion();
  // UpdateButton 在 check 为空时直接 return null —— 必须把检查结果传进去
  const update = useUpdateCheck();

  const s = stats.data;
  const unknown = stats.isError || stats.isPending;
  const num = (v: number | undefined) => (stats.isPending ? "…" : stats.isError || v === undefined ? "—" : v);
  const recentQueue = (queue.data || []).slice(0, 6);
  const m = sys.data;

  return (
    <div className="space-y-3 sm:space-y-5">
      <header className="page-head">
        <div className="page-head-main">
          <span className="page-date">{fmtDateTime(new Date().toISOString())}</span>
          <h1 className="page-title">{t("nav.dashboard")}</h1>
          <p className="page-desc">{t("dashboard.subtitle")}</p>
        </div>
      </header>

      {stats.isError && (
        <p className="text-xs text-destructive" role="status">
          {t("dashboard.statsFailedTitle")}
        </p>
      )}

      <div className="grid grid-cols-2 xl:grid-cols-4 gap-3 sm:gap-4">
        <Kpi label={t("dashboard.kpi.activeQueues")} value={num(s?.activeQueues)} sub={t("dashboard.kpi.viewQueue")} to="/queue" />
        <Kpi label={t("dashboard.kpi.purchaseSuccess")} value={num(s?.purchaseSuccess)} sub={t("dashboard.kpi.viewHistory")} to="/history" tone={s?.purchaseSuccess ? "success" : undefined} />
        <Kpi label={t("dashboard.kpi.purchaseFailed")} value={num(s?.purchaseFailed)} sub={t("dashboard.kpi.viewHistory")} to="/history" tone={s?.purchaseFailed ? "danger" : undefined} />
        <Kpi
          label={t("dashboard.kpi.totalServers")}
          value={num(s?.totalServers)}
          sub={`${t("dashboard.kpi.available")} ${num(s?.availableServers)}`}
          to="/servers"
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-3 sm:gap-4">
        {/* 最近队列 */}
        <Card className="lg:col-span-2 min-w-0 overflow-hidden">
          <div className="flex items-center justify-between gap-2 px-4 py-3 border-b border-border">
            <span className="text-[13px] font-medium">{t("dashboard.recentQueue")}</span>
            <Link to="/queue" className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
              {t("dashboard.viewAll")} <ArrowRight className="w-3 h-3" />
            </Link>
          </div>
          {queue.isError ? (
            <p className="px-4 py-8 text-center text-sm text-destructive">{t("dashboard.queueFailedTitle")}</p>
          ) : recentQueue.length === 0 ? (
            <div className="px-4 py-10 flex flex-col items-center gap-2 text-muted-foreground">
              <Server className="w-5 h-5" />
              <span className="text-sm">{queue.isPending ? t("dashboard.reading") : t("dashboard.queueEmpty")}</span>
            </div>
          ) : (
            <ul className="divide-y divide-border">
              {recentQueue.map((q) => (
                <li key={q.id} className="flex items-center gap-3 px-4 py-2.5 min-w-0">
                  <span className="font-mono text-[13px] font-medium truncate">{q.planCode}</span>
                  <span className="text-xs text-muted-foreground uppercase shrink-0">{q.datacenter}</span>
                  <span className="ml-auto inline-flex items-center gap-1.5 text-xs text-muted-foreground whitespace-nowrap">
                    <StatusDot tone={QUEUE_TONE[q.status] ?? "muted"} size="xs" />
                    {t(`queue.status.${q.status}`, { defaultValue: q.status })}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Card>

        {/* 系统状态 + 版本 */}
        <Card className="min-w-0 overflow-hidden">
          <div className="px-4 py-3 border-b border-border text-[13px] font-medium">{t("dashboard.systemStatus")}</div>
          <div className="px-4 py-1 divide-y divide-border">
            <StatusLine
              icon={<Link2 className="w-3.5 h-3.5" />}
              label={t("dashboard.apiConn")}
              on={!!s && !stats.isError}
              onText={t("dashboard.connected")}
              offText={t("dashboard.disconnected")}
              unknown={stats.isPending}
            />
            <StatusLine
              icon={<Bot className="w-3.5 h-3.5" />}
              label={t("dashboard.autoSnipe")}
              on={(s?.activeQueues || 0) > 0}
              onText={t("dashboard.running")}
              offText={t("dashboard.noTasks")}
              unknown={unknown}
            />
            <StatusLine
              icon={<Bell className="w-3.5 h-3.5" />}
              label={t("dashboard.serverMonitor")}
              on={!!s?.monitorRunning}
              onText={t("dashboard.running")}
              offText={t("dashboard.standby")}
              unknown={unknown}
            />
          </div>
          <div className="px-4 py-3 border-t border-border flex flex-wrap items-center gap-x-2 gap-y-2">
            <span className="text-xs text-muted-foreground">{t("dashboard.systemVersion")}</span>
            <span className="font-mono text-xs" title={version.isError ? t("dashboard.versionFailedTitle", { error: String(version.error) }) : undefined}>
              v{version.data || "—"}
            </span>
            <span className="ml-auto inline-flex">
              <UpdateButton check={update.data} />
            </span>
          </div>
        </Card>
      </div>

      {/* 宿主机资源 */}
      <Card className="min-w-0 overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-3 border-b border-border">
          <span className="text-[13px] font-medium">{t("dashboard.hostMetrics")}</span>
          <span className="text-xs text-muted-foreground font-mono truncate max-w-full">
            {m ? `${m.host.hostname} · ${m.host.platform} · ${t("dashboard.cores", { n: m.cpu.cores })}` : sys.isError ? t("dashboard.readFailed") : "…"}
          </span>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 sm:gap-6 p-4">
          <MeterRow label={t("dashboard.cpu")} percent={m?.cpu.percent} />
          <MeterRow
            label={t("dashboard.memory")}
            percent={m?.memory.percent}
            detail={m?.memory ? t("dashboard.usedFree", { used: fmtBytes(m.memory.usedBytes), free: fmtBytes(m.memory.totalBytes - m.memory.usedBytes) }) : undefined}
          />
          <MeterRow
            label={t("dashboard.disk")}
            percent={m?.disk.percent}
            detail={m?.disk ? t("dashboard.usedFree", { used: fmtBytes(m.disk.usedBytes), free: fmtBytes(m.disk.totalBytes - m.disk.usedBytes) }) : undefined}
          />
        </div>
      </Card>
    </div>
  );
}
