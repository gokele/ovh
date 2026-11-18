import { Link } from "@tanstack/react-router";
import { ArrowRight, Bell, Bot, CheckCircle2, Link2, ListChecks, Plus, Server, XCircle } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { fmtDateTime } from "@/i18n/format";
import { useStats } from "@/hooks/use-stats";
import { useQueueList, type QueueStatus } from "@/hooks/use-queue";
import { useSystemMetrics, useAppVersion, useUpdateCheck } from "@/hooks/use-system-metrics";
import { UpdateButton } from "@/components/common/UpdateButton";
import { StatusDot } from "@/components/common/StatusDot";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { AccountChip } from "@/components/common/AccountChip";
import { EmptyState } from "@/components/common/EmptyState";
import { DashboardArt } from "@/components/illustrations/DashboardArt";
import { cn } from "@/lib/utils";

/**
 * 仪表盘:
 *   欢迎横幅(状态一句话 + 新建任务 / 浏览服务器 + 自绘插画)
 *   4 个读数卡(图标块):活跃队列 / 下单成功 / 下单失败 / 服务器(可用/总数)
 *   最近队列(宽) + 系统状态与版本(窄)
 *   宿主机资源:CPU / 内存 / 磁盘 环形读数
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

type KpiTone = "brand" | "blue" | "green" | "red";
const KPI_TILE: Record<KpiTone, string> = {
  brand: "bg-white/20 text-white",
  blue: "bg-primary/10 text-primary",
  green: "bg-success/[.12] text-success",
  red: "bg-destructive/10 text-destructive",
};

function Kpi({
  label,
  value,
  sub,
  to,
  icon,
  tone,
  emphasis,
}: {
  label: string;
  value: ReactNode;
  sub?: ReactNode;
  to?: "/queue" | "/history" | "/servers";
  icon: ReactNode;
  tone: KpiTone;
  emphasis?: "danger" | "success";
}) {
  const brand = tone === "brand";
  const body = (
    <div className="flex items-start gap-3">
      <div className="min-w-0 flex-1">
        <div className={cn("text-[13px] truncate", brand ? "text-white/85" : "text-muted-foreground")} title={label}>{label}</div>
        <div
          className={cn(
            "mt-2 text-[28px] leading-8 font-semibold tabular-nums tracking-tight",
            !brand && emphasis === "danger" && "text-destructive",
            !brand && emphasis === "success" && "text-success",
          )}
        >
          {value}
        </div>
        {sub && (
          <div
            className={cn(
              "mt-3 inline-flex items-center gap-1 text-[12px] transition-colors truncate max-w-full",
              brand ? "text-white/80 group-hover:text-white" : "text-muted-foreground group-hover:text-primary",
            )}
          >
            {sub}
            {to && <ArrowRight className="w-3 h-3 shrink-0" />}
          </div>
        )}
      </div>
      <span className={cn("w-11 h-11 rounded-full flex items-center justify-center shrink-0", KPI_TILE[tone])}>{icon}</span>
    </div>
  );
  return (
    <Card
      className={cn(
        "min-w-0 transition-[box-shadow,transform] hover:shadow-[var(--soft-2)] hover:-translate-y-px",
        brand && "!bg-[#2f74ff] bg-gradient-to-br from-[#4c8dff] to-[#1f5fe6] text-white !shadow-[0_8px_20px_-6px_rgba(31,107,255,.55)]",
      )}
    >
      {to ? (
        <Link to={to} className="group block p-5 rounded-[inherit] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
          {body}
        </Link>
      ) : (
        <div className="p-5">{body}</div>
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
    <div className="flex items-center gap-3 py-2.5">
      <span className="w-8 h-8 rounded-lg bg-muted flex items-center justify-center text-muted-foreground shrink-0">{icon}</span>
      <span className="flex-1 min-w-0 text-[13px] truncate">{label}</span>
      <span
        className={cn(
          "inline-flex items-center gap-1.5 h-6 px-2 rounded-full text-[11.5px] whitespace-nowrap",
          unknown ? "bg-muted text-muted-foreground" : on ? "bg-success/10 text-success" : "bg-warning/[.12] text-warning",
        )}
      >
        <StatusDot tone={unknown ? "muted" : on ? "success" : "warning"} size="xs" pulse={on && !unknown} />
        {unknown ? "…" : on ? onText : offText}
      </span>
    </div>
  );
}

/** 环形读数:宿主机 CPU / 内存 / 磁盘。未读到时显示 —,环为空。 */
function MeterRing({ label, percent, detail }: { label: string; percent?: number; detail?: string }) {
  const { t } = useTranslation();
  const known = typeof percent === "number" && Number.isFinite(percent);
  const p = known ? Math.max(0, Math.min(100, percent)) : 0;
  const stroke = p >= 90 ? "var(--red)" : p >= 75 ? "#f5a524" : "var(--blue)";
  const r = 24;
  const c = 2 * Math.PI * r;
  return (
    <div className="flex items-center gap-3.5 min-w-0">
      <div
        className="relative w-[60px] h-[60px] shrink-0"
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={known ? Math.round(p) : undefined}
      >
        <svg viewBox="0 0 60 60" className="w-full h-full -rotate-90" aria-hidden>
          <circle cx="30" cy="30" r={r} fill="none" strokeWidth="6" className="stroke-muted" />
          <circle
            cx="30"
            cy="30"
            r={r}
            fill="none"
            strokeWidth="6"
            strokeLinecap="round"
            stroke={stroke}
            strokeDasharray={`${(p / 100) * c} ${c}`}
            className="transition-[stroke-dasharray] duration-500"
          />
        </svg>
        <span className="absolute inset-0 flex items-center justify-center text-[13px] font-semibold tabular-nums" title={known ? undefined : t("dashboard.valueUnknown")}>
          {known ? `${p.toFixed(0)}%` : "—"}
        </span>
      </div>
      <div className="min-w-0">
        <div className="text-[13px] font-medium">{label}</div>
        {detail && <div className="mt-0.5 text-[11.5px] text-muted-foreground truncate">{detail}</div>}
      </div>
    </div>
  );
}

/** 数字从 0 滚动到目标值(约 0.7 秒,缓出);减少动态效果时直接显示。 */
function CountUp({ value }: { value: number }) {
  const [shown, setShown] = useState(value);
  const from = useRef(0);
  useEffect(() => {
    const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    const start = from.current;
    from.current = value;
    if (reduce || start === value) {
      setShown(value);
      return;
    }
    let raf = 0;
    const t0 = performance.now();
    const tick = (now: number) => {
      const p = Math.min(1, (now - t0) / 700);
      const eased = 1 - Math.pow(1 - p, 3);
      setShown(Math.round(start + (value - start) * eased));
      if (p < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [value]);
  return <>{shown}</>;
}

function greetingKey(h: number): string {
  if (h < 5) return "dashboard.greetNight";
  if (h < 12) return "dashboard.greetMorning";
  if (h < 18) return "dashboard.greetAfternoon";
  return "dashboard.greetEvening";
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
  const num = (v: number | undefined) => (stats.isPending ? "…" : stats.isError || v === undefined ? "—" : <CountUp value={v} />);
  const recentQueue = (queue.data || []).slice(0, 6);
  const m = sys.data;

  const activeCount = s?.activeQueues ?? 0;

  return (
    <div className="space-y-4 sm:space-y-5">
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

      {/* 欢迎横幅:状态一句话 + 两个常用入口 + 插画 */}
      <Card className="relative overflow-hidden">
        <div className="absolute inset-0 bg-gradient-to-r from-primary/[0.10] via-primary/[0.04] to-transparent pointer-events-none" aria-hidden />
        <div className="relative flex items-center gap-6 px-5 py-5 sm:px-7 sm:py-6">
          <div className="min-w-0 flex-1">
            <h2 className="text-[19px] sm:text-[21px] font-semibold tracking-tight">{t(greetingKey(new Date().getHours()))}</h2>
            <ul className="mt-2.5 space-y-1.5 text-[13px] text-muted-foreground">
              <li className="flex items-center gap-2">
                <StatusDot tone={unknown ? "muted" : activeCount > 0 ? "success" : "muted"} size="xs" pulse={!unknown && activeCount > 0} />
                {unknown ? t("dashboard.reading") : activeCount > 0 ? t("dashboard.heroTasks", { n: activeCount }) : t("dashboard.heroIdle")}
              </li>
              <li className="flex items-center gap-2">
                <StatusDot tone={unknown ? "muted" : s?.monitorRunning ? "success" : "warning"} size="xs" />
                {unknown ? t("dashboard.reading") : s?.monitorRunning ? t("dashboard.heroMonitorOn") : t("dashboard.heroMonitorOff")}
              </li>
            </ul>
            <div className="mt-4 flex flex-wrap gap-2">
              <Button asChild size="sm">
                <Link to="/queue">
                  <Plus className="w-4 h-4" />
                  {t("dashboard.createTask")}
                </Link>
              </Button>
              <Button asChild size="sm" variant="outline">
                <Link to="/servers">
                  <Server className="w-4 h-4" />
                  {t("dashboard.heroBrowse")}
                </Link>
              </Button>
            </div>
          </div>
          <div className="hidden sm:block shrink-0 -my-2" aria-hidden>
            <DashboardArt className="w-[220px] lg:w-[260px] h-auto" />
          </div>
        </div>
      </Card>

      <div className="bw-stagger grid grid-cols-2 xl:grid-cols-4 gap-3 sm:gap-4">
        <Kpi tone="brand" icon={<ListChecks className="w-5 h-5" />} label={t("dashboard.kpi.activeQueues")} value={num(s?.activeQueues)} sub={t("dashboard.kpi.viewQueue")} to="/queue" />
        <Kpi tone="green" icon={<CheckCircle2 className="w-5 h-5" />} label={t("dashboard.kpi.purchaseSuccess")} value={num(s?.purchaseSuccess)} sub={t("dashboard.kpi.viewHistory")} to="/history" emphasis={s?.purchaseSuccess ? "success" : undefined} />
        <Kpi tone="red" icon={<XCircle className="w-5 h-5" />} label={t("dashboard.kpi.purchaseFailed")} value={num(s?.purchaseFailed)} sub={t("dashboard.kpi.viewHistory")} to="/history" emphasis={s?.purchaseFailed ? "danger" : undefined} />
        <Kpi
          tone="blue"
          icon={<Server className="w-5 h-5" />}
          label={t("dashboard.kpi.totalServers")}
          value={num(s?.totalServers)}
          sub={<>{t("dashboard.kpi.available")} {num(s?.availableServers)}</>}
          to="/servers"
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-3 sm:gap-4">
        {/* 最近队列 */}
        <Card className="lg:col-span-2 min-w-0 overflow-hidden">
          <div className="flex items-center justify-between gap-2 px-5 py-3.5 border-b border-border">
            <span className="text-[14px] font-semibold">{t("dashboard.recentQueue")}</span>
            <Link to="/queue" className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
              {t("dashboard.viewAll")} <ArrowRight className="w-3 h-3" />
            </Link>
          </div>
          {queue.isError ? (
            <p className="px-5 py-8 text-center text-sm text-destructive">{t("dashboard.queueFailedTitle")}</p>
          ) : recentQueue.length === 0 ? (
            queue.isPending ? (
              <p className="px-5 py-10 text-center text-sm text-muted-foreground">{t("dashboard.reading")}</p>
            ) : (
              <EmptyState illustration="queue" title={t("dashboard.queueEmpty")} className="py-6" />
            )
          ) : (
            <ul className="divide-y divide-border">
              {recentQueue.map((q) => (
                <li key={q.id} className="flex items-center gap-3 px-5 py-2.5 min-w-0 hover:bg-muted/40 transition-colors">
                  <span className="w-8 h-8 rounded-lg bg-muted flex items-center justify-center shrink-0 text-muted-foreground">
                    <Server className="w-4 h-4" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 min-w-0">
                      <span className="font-mono text-[13px] font-semibold truncate">{q.planCode}</span>
                      <span className="font-mono text-[11px] text-muted-foreground uppercase shrink-0">{q.datacenter}</span>
                    </div>
                  </div>
                  <AccountChip accountId={q.accountId} className="hidden sm:inline-flex" />
                  <span className="inline-flex items-center gap-1.5 h-6 px-2 rounded-full bg-muted/70 text-[11.5px] text-muted-foreground whitespace-nowrap">
                    <StatusDot tone={QUEUE_TONE[q.status] ?? "muted"} size="xs" pulse={q.status === "running"} />
                    {t(`queue.status.${q.status}`, { defaultValue: q.status })}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Card>

        {/* 系统状态 + 版本 */}
        <Card className="min-w-0 overflow-hidden">
          <div className="px-5 py-3.5 border-b border-border text-[14px] font-semibold">{t("dashboard.systemStatus")}</div>
          <div className="px-5 py-1 divide-y divide-border">
            <StatusLine
              icon={<Link2 className="w-4 h-4" />}
              label={t("dashboard.apiConn")}
              on={!!s && !stats.isError}
              onText={t("dashboard.connected")}
              offText={t("dashboard.disconnected")}
              unknown={stats.isPending}
            />
            <StatusLine
              icon={<Bot className="w-4 h-4" />}
              label={t("dashboard.autoSnipe")}
              on={activeCount > 0}
              onText={t("dashboard.running")}
              offText={t("dashboard.noTasks")}
              unknown={unknown}
            />
            <StatusLine
              icon={<Bell className="w-4 h-4" />}
              label={t("dashboard.serverMonitor")}
              on={!!s?.monitorRunning}
              onText={t("dashboard.running")}
              offText={t("dashboard.standby")}
              unknown={unknown}
            />
          </div>
          <div className="px-5 py-3 border-t border-border flex flex-wrap items-center gap-x-2 gap-y-2">
            <span className="text-xs text-muted-foreground">{t("dashboard.systemVersion")}</span>
            <span className="font-mono text-xs px-1.5 py-0.5 rounded-md bg-muted" title={version.isError ? t("dashboard.versionFailedTitle", { error: String(version.error) }) : undefined}>
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
        <div className="flex flex-wrap items-center justify-between gap-2 px-5 py-3.5 border-b border-border">
          <span className="text-[14px] font-semibold">{t("dashboard.hostMetrics")}</span>
          <span className="text-xs text-muted-foreground font-mono truncate max-w-full">
            {m ? `${m.host.hostname} · ${m.host.platform} · ${t("dashboard.cores", { n: m.cpu.cores })}` : sys.isError ? t("dashboard.readFailed") : "…"}
          </span>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 sm:gap-6 px-5 py-5">
          <MeterRing label={t("dashboard.cpu")} percent={m?.cpu.percent} detail={m ? t("dashboard.cores", { n: m.cpu.cores }) : undefined} />
          <MeterRing
            label={t("dashboard.memory")}
            percent={m?.memory.percent}
            detail={m?.memory ? t("dashboard.usedFree", { used: fmtBytes(m.memory.usedBytes), free: fmtBytes(m.memory.totalBytes - m.memory.usedBytes) }) : undefined}
          />
          <MeterRing
            label={t("dashboard.disk")}
            percent={m?.disk.percent}
            detail={m?.disk ? t("dashboard.usedFree", { used: fmtBytes(m.disk.usedBytes), free: fmtBytes(m.disk.totalBytes - m.disk.usedBytes) }) : undefined}
          />
        </div>
      </Card>
    </div>
  );
}
