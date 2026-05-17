import { createFileRoute, Link } from "@tanstack/react-router";
import {
  BarChart3,
  ClipboardList,
  Server,
  CheckCircle2,
  ChevronRight,
  Plus,
  Clock,
  Link2,
  Bot,
  Bell,
  Info,
  CheckCheck,
  Calendar,
  type LucideIcon,
} from "lucide-react";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Chip } from "@/components/common/Chip";
import { StatusDot } from "@/components/common/StatusDot";
import { EmptyState } from "@/components/common/EmptyState";
import { Skeleton } from "@/components/common/Skeleton";
import { MetricRing } from "@/components/common/MetricRing";
import { useStats } from "@/hooks/use-stats";
import { useQueueList, type QueueItem } from "@/hooks/use-queue";
import { useSystemMetrics, useAppVersion } from "@/hooks/use-system-metrics";

/** 仪表盘:3 KPI + 活跃队列 / 系统状态 + 系统监控(CPU / 内存 / 磁盘 / 网络) */
export const Route = createFileRoute("/")({
  component: DashboardPage,
});

function DashboardPage() {
  const stats = useStats();
  const queue = useQueueList();
  const sys = useSystemMetrics();
  const version = useAppVersion();

  const activeQueue: QueueItem[] = (queue.data || [])
    .filter((i) => ["running", "pending", "paused"].includes(i.status))
    .slice(0, 4);

  return (
    <div className="space-y-6">
      <PageHeader icon={BarChart3} title="仪表盘" description="OVH 服务器抢购平台状态概览" />

      {/* 顶部 KPI */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <KpiCard
          label="活跃队列"
          value={stats.data?.activeQueues}
          icon={ClipboardList}
          linkTo="/queue"
          linkText="查看队列"
          loading={stats.isPending}
        />
        <KpiCard
          label="服务器总数"
          value={stats.data?.totalServers}
          extra={
            stats.data && (
              <span className="text-[12px] text-muted-foreground ml-2">
                可用 <span className="font-semibold text-success">{stats.data.availableServers}</span>
              </span>
            )
          }
          icon={Server}
          linkTo="/servers"
          linkText="查看服务"
          loading={stats.isPending}
        />
        <KpiCard
          label="抢购成功"
          value={stats.data?.purchaseSuccess}
          icon={CheckCircle2}
          linkTo="/history"
          linkText="查看历史"
          loading={stats.isPending}
        />
      </div>

      {/* 中部：活跃队列 + 系统状态 */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <Card className="lg:col-span-2">
          <CardContent className="p-6">
            <div className="flex items-center justify-between mb-4">
              <div className="flex items-center gap-2">
                <ClipboardList className="w-4 h-4 text-muted-foreground" />
                <h2 className="text-[15px] font-semibold">活跃队列</h2>
              </div>
              <Link to="/queue" className="text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-1">
                查看全部
                <ChevronRight className="w-3 h-3" />
              </Link>
            </div>
            {queue.isPending ? (
              <div className="space-y-2">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-16 rounded-xl" />
                ))}
              </div>
            ) : activeQueue.length === 0 ? (
              <EmptyState
                icon={Calendar}
                title="暂无活跃任务"
                action={
                  <Button asChild>
                    <Link to="/queue">
                      <Plus className="w-4 h-4" />
                      创建抢购任务
                    </Link>
                  </Button>
                }
              />
            ) : (
              <div className="space-y-2">
                {activeQueue.map((q) => (
                  <div
                    key={q.id}
                    className="flex items-center justify-between gap-3 rounded-xl px-4 py-3 bg-secondary/50 border border-border"
                  >
                    <div className="min-w-0 flex-1">
                      <p className="font-medium text-sm truncate">{q.planCode}</p>
                      <div className="flex items-center gap-2 text-[11px] text-muted-foreground mt-0.5">
                        <span className="inline-flex items-center gap-1">
                          <Clock className="w-3 h-3" />
                          {q.datacenter.toUpperCase()}
                        </span>
                        <span className="text-muted-foreground/50">·</span>
                        <span>第 {q.retryCount + 1} 次尝试</span>
                      </div>
                    </div>
                    <QueueStatusChip status={q.status} />
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardContent className="p-6">
            <div className="flex items-center gap-2 mb-4">
              <CheckCheck className="w-4 h-4 text-muted-foreground" />
              <h2 className="text-[15px] font-semibold">系统状态</h2>
            </div>
            <div className="space-y-1">
              <SystemRow
                icon={<Link2 className="w-3.5 h-3.5" />}
                label="API 连接"
                ok={!!stats.data}
                onText="已连接"
                offText="未连接"
              />
              <SystemRow
                icon={<Bot className="w-3.5 h-3.5" />}
                label="自动抢购"
                ok={(stats.data?.activeQueues || 0) > 0}
                onText="运行中"
                offText="暂无任务"
                neutralOff
              />
              <SystemRow
                icon={<Bell className="w-3.5 h-3.5" />}
                label="服务器监控"
                ok={!!stats.data?.monitorRunning}
                onText="运行中"
                offText="待启用"
                warnOff
              />
              <div className="flex justify-between items-center px-3 py-2.5 mt-1 border-t border-border pt-3">
                <div className="inline-flex items-center gap-2 text-xs text-muted-foreground">
                  <Info className="w-3.5 h-3.5" />
                  系统版本
                </div>
                <span className="text-xs font-mono font-semibold">v{version.data || "—"}</span>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* 系统监控:CPU / 内存 / 磁盘 三个圆环 */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <Card>
          <CardContent className="p-0">
            <MetricRing
              label="CPU"
              subLabel={sys.data ? `${sys.data.cpu.cores} 核心` : "—"}
              percent={sys.data?.cpu.percent ?? 0}
            />
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-0">
            <MetricRing
              label="内存"
              subLabel={
                sys.data
                  ? `${formatBytesShort(sys.data.memory.usedBytes)} / ${formatBytesShort(sys.data.memory.totalBytes)}`
                  : "—"
              }
              percent={sys.data?.memory.percent ?? 0}
            />
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-0">
            <MetricRing
              label={sys.data?.disk.path || "磁盘"}
              subLabel={
                sys.data
                  ? `${formatBytesShort(sys.data.disk.usedBytes)} / ${formatBytesShort(sys.data.disk.totalBytes)}`
                  : "—"
              }
              percent={sys.data?.disk.percent ?? 0}
            />
          </CardContent>
        </Card>
      </div>

    </div>
  );
}

/** 字节短格式:1.5 GB / 11.4 GB 这种 */
function formatBytesShort(n: number): string {
  if (!n || n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB", "PB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(1)} ${units[i]}`;
}

/* ---------- 小组件 ---------- */

function KpiCard({
  label,
  value,
  extra,
  icon: Icon,
  linkTo,
  linkText,
  loading,
}: {
  label: string;
  value: number | undefined;
  extra?: React.ReactNode;
  icon: LucideIcon;
  linkTo: string;
  linkText: string;
  loading?: boolean;
}) {
  return (
    <Card>
      <CardContent className="p-5">
        <div className="flex items-center justify-between mb-4">
          <span className="text-[13px] text-muted-foreground font-medium">{label}</span>
          <div className="w-10 h-10 rounded-full bg-secondary flex items-center justify-center">
            <Icon className="w-5 h-5 text-foreground" strokeWidth={1.75} />
          </div>
        </div>
        <div className="flex items-baseline">
          {loading ? (
            <Skeleton className="w-16 h-10 rounded-md" />
          ) : (
            <span className="text-[32px] font-bold leading-none">{value ?? 0}</span>
          )}
          {extra}
        </div>
        <Link to={linkTo} className="mt-3 inline-flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground">
          {linkText}
          <ChevronRight className="w-3 h-3" />
        </Link>
      </CardContent>
    </Card>
  );
}

function QueueStatusChip({ status }: { status: string }) {
  if (status === "running")
    return (
      <Chip tone="success">
        <StatusDot tone="success" pulse size="xs" />
        运行中
      </Chip>
    );
  if (status === "pending")
    return (
      <Chip tone="warning">
        <StatusDot tone="warning" size="xs" />
        等待中
      </Chip>
    );
  return (
    <Chip tone="default">
      <StatusDot tone="muted" size="xs" />
      已暂停
    </Chip>
  );
}

function SystemRow({
  icon,
  label,
  ok,
  onText,
  offText,
  neutralOff,
  warnOff,
}: {
  icon: React.ReactNode;
  label: string;
  ok: boolean;
  onText: string;
  offText: string;
  neutralOff?: boolean;
  warnOff?: boolean;
}) {
  const dotTone = ok ? "success" : warnOff ? "warning" : neutralOff ? "muted" : "danger";
  return (
    <div className="flex justify-between items-center px-3 py-2.5 rounded-lg hover:bg-secondary transition-colors">
      <div className="inline-flex items-center gap-2 text-[13px]">
        <span className="text-muted-foreground">{icon}</span>
        <span>{label}</span>
      </div>
      <div className="inline-flex items-center gap-1.5">
        <StatusDot tone={dotTone as any} pulse={ok} size="xs" />
        <span className={`text-xs ${ok ? "font-medium" : "text-muted-foreground"}`}>{ok ? onText : offText}</span>
      </div>
    </div>
  );
}

