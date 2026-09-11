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
  AlertTriangle,
  type LucideIcon,
} from "lucide-react";
import { UpdateButton } from "@/components/common/UpdateButton";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Chip } from "@/components/common/Chip";
import { StatusDot } from "@/components/common/StatusDot";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed, LoadFailedBanner, errorMessage } from "@/components/common/LoadFailed";
import { Skeleton } from "@/components/common/Skeleton";
import { MetricRing } from "@/components/common/MetricRing";
import { useIsNarrow } from "@/hooks/use-narrow";
import { useStats } from "@/hooks/use-stats";
import { useQueueList, type QueueItem } from "@/hooks/use-queue";
import { useSystemMetrics, useAppVersion, useUpdateCheck } from "@/hooks/use-system-metrics";

/** 仪表盘:3 KPI + 活跃队列 / 系统状态 + 系统监控(CPU / 内存 / 磁盘 / 网络) */
export const Route = createFileRoute("/")({
  component: DashboardPage,
});

function DashboardPage() {
  const stats = useStats();
  const queue = useQueueList();
  const sys = useSystemMetrics();
  const version = useAppVersion();
  const update = useUpdateCheck();

  const activeQueue: QueueItem[] = (queue.data || [])
    .filter((i) => ["running", "pending", "paused"].includes(i.status))
    .slice(0, 4);

  // 统计还没读到(加载中或失败)时,数值一律是 undefined —— KpiCard 会渲染成「—」。
  // 绝不能 `?? 0`:仪表盘上一个大大的 0 是条业务结论("现在没有任务在跑"、
  // "一台机器都没抢到"),而请求失败的真实含义是"我们不知道"。用户照着假 0
  // 会得出"抢购停了,再建一单"的结论,于是重复下单。
  const statsUnknown = stats.isPending || stats.isError;
  const statsUnknownText = stats.isError ? "读取失败" : "读取中…";

  // 系统监控同理:没读到就别画环。0% CPU 是个具体读数,看起来像"机器很闲"。
  const metricsHint = sys.isError ? "读取失败" : "读取中…";
  const metricsTitle = sys.isError ? `系统监控读取失败:${errorMessage(sys.error)}` : "正在读取系统监控";

  return (
    <div className="space-y-3 sm:space-y-6">
      <PageHeader icon={BarChart3} title="仪表盘" description="OVH 服务器抢购平台状态概览" />

      {stats.isError && (
        <LoadFailedBanner
          title="仪表盘统计读取失败"
          error={stats.error}
          onRetry={() => stats.refetch()}
        />
      )}

      {/* 顶部 KPI。手机端横排 3 列 —— 竖着叠 3 张卡要 540px,
          占掉 844px 首屏的三分之二,而它们一共只有三个数字。 */}
      <div className="grid grid-cols-3 gap-2 sm:gap-4">
        <KpiCard
          label="活跃队列"
          value={stats.data?.activeQueues}
          icon={ClipboardList}
          linkTo="/queue"
          linkText="查看队列"
          loading={stats.isPending}
          failed={stats.isError}
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
          failed={stats.isError}
        />
        <KpiCard
          label="下单成功(待付款)"
          value={stats.data?.purchaseSuccess}
          icon={CheckCircle2}
          linkTo="/history"
          linkText="查看历史"
          loading={stats.isPending}
          failed={stats.isError}
        />
      </div>

      {/* 中部：活跃队列 + 系统状态 */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <Card className="lg:col-span-2">
          <CardContent className="p-3.5 sm:p-6">
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
            ) : queue.isError ? (
              /* 队列没读到 ≠ 队列是空的。渲染成"暂无活跃任务"+"创建抢购任务"按钮,
                 是在直接引导用户对着一个可能正在跑的队列再建一单 —— 同机型同机房
                 抢两遍,后端拒不掉的那部分就是重复下单。 */
              <LoadFailed
                icon={ClipboardList}
                title="活跃队列读取失败"
                error={queue.error}
                onRetry={() => queue.refetch()}
                compact
              />
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
          <CardContent className="p-3.5 sm:p-6">
            <div className="flex items-center gap-2 mb-4">
              <CheckCheck className="w-4 h-4 text-muted-foreground" />
              <h2 className="text-[15px] font-semibold">系统状态</h2>
            </div>
            <div className="space-y-1">
              {/* 这一行是真的在测连通性:/stats 请求失败 = 后端这条路不通,
                  "未连接"就是它本来的意思,所以只需要把"还在问"的加载态摘出去。 */}
              <SystemRow
                icon={<Link2 className="w-3.5 h-3.5" />}
                label="API 连接"
                ok={!!stats.data && !stats.isError}
                onText="已连接"
                offText="未连接"
                unknown={stats.isPending}
                unknownText="检测中…"
              />
              {/* 下面两行读的是 /stats 里的业务字段,请求没成功就等于这两个字段没拿到。
                  写成"暂无任务" / "待启用",是把"我们没问到"包装成了"后台确实闲着 /
                  监控确实没开" —— 用户会据此去手动重开监控、或者以为抢购已经停了。 */}
              <SystemRow
                icon={<Bot className="w-3.5 h-3.5" />}
                label="自动抢购"
                ok={(stats.data?.activeQueues || 0) > 0}
                onText="运行中"
                offText="暂无任务"
                neutralOff
                unknown={statsUnknown}
                unknownText={statsUnknownText}
              />
              <SystemRow
                icon={<Bell className="w-3.5 h-3.5" />}
                label="服务器监控"
                ok={!!stats.data?.monitorRunning}
                onText="运行中"
                offText="待启用"
                warnOff
                unknown={statsUnknown}
                unknownText={statsUnknownText}
              />
              <div className="flex justify-between items-center px-3 py-2.5 mt-1 border-t border-border pt-3">
                <div className="inline-flex items-center gap-2 text-xs text-muted-foreground">
                  <Info className="w-3.5 h-3.5" />
                  系统版本
                </div>
                <div className="inline-flex items-center gap-2">
                  <span
                    className="text-xs font-mono font-semibold"
                    title={
                      version.isError
                        ? `版本号读取失败:${errorMessage(version.error)}`
                        : undefined
                    }
                  >
                    v{version.data || "—"}
                  </span>
                  {/* UpdateButton 在 check 为空时直接 return null,所以检查失败会让
                      "有新版本"这件事整个消失 —— 页面看起来跟"已是最新"一模一样,
                      用户可能几个月都不知道自己在跑一个有已知 bug 的版本。
                      失败要留一句低调的提示,并且点一下能重试。 */}
                  {update.isError ? (
                    <button
                      type="button"
                      onClick={() => update.refetch()}
                      className="inline-flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground transition-colors"
                      title={`更新检查失败:${errorMessage(update.error)}(点击重试)`}
                    >
                      <AlertTriangle className="w-3 h-3" />
                      更新检查失败
                    </button>
                  ) : (
                    <UpdateButton check={update.data} />
                  )}
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* 系统监控:CPU / 内存 / 磁盘 三个圆环。
          读不到就换成未知环(空轨道 + 中心「—」),不要拿 `?? 0` 顶上去:
          三个 0% 的绿环是"宿主机很空闲"这条读数,而真相是我们一个数都没拿到。
          抢购工具里这条差别很要命 —— 用户会因为"机器闲着"去加并发、加任务。 */}
      <div className="grid grid-cols-3 gap-2 sm:gap-4">
        <Card>
          <CardContent className="p-0">
            {sys.data ? (
              <MetricRing
                label="CPU"
                subLabel={`${sys.data.cpu.cores} 核心`}
                percent={sys.data.cpu.percent}
              />
            ) : (
              <MetricRingUnknown label="CPU" hint={metricsHint} title={metricsTitle} failed={sys.isError} />
            )}
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-0">
            {sys.data ? (
              <MetricRing
                label="内存"
                subLabel={`${formatBytesShort(sys.data.memory.usedBytes)} / ${formatBytesShort(sys.data.memory.totalBytes)}`}
                percent={sys.data.memory.percent}
              />
            ) : (
              <MetricRingUnknown label="内存" hint={metricsHint} title={metricsTitle} failed={sys.isError} />
            )}
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-0">
            {sys.data ? (
              <MetricRing
                label={sys.data.disk.path || "磁盘"}
                subLabel={`${formatBytesShort(sys.data.disk.usedBytes)} / ${formatBytesShort(sys.data.disk.totalBytes)}`}
                percent={sys.data.disk.percent}
              />
            ) : (
              <MetricRingUnknown label="磁盘" hint={metricsHint} title={metricsTitle} failed={sys.isError} />
            )}
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

/**
 * KPI 卡片。
 *
 * value 为 undefined = 这个数我们没拿到,一律显示「—」。
 * 以前写的是 `value ?? 0`,请求一失败仪表盘就变成"活跃队列 0 / 服务器总数 0 /
 * 下单成功 0" —— 三条看起来很确定的业务结论,全是假的。
 * failed 只负责在卡片上补一句"读取失败",让用户知道该重试而不是该重新建单。
 */
function KpiCard({
  label,
  value,
  extra,
  icon: Icon,
  linkTo,
  linkText,
  loading,
  failed,
}: {
  label: string;
  value: number | undefined;
  extra?: React.ReactNode;
  icon: LucideIcon;
  linkTo: string;
  linkText: string;
  loading?: boolean;
  /** 请求失败(区别于"还在加载"和"后端真的返回 0") */
  failed?: boolean;
}) {
  const valueUnknown = value === undefined;
  return (
    <Card>
      {/* 手机端三张卡并排,每张只有 ~120px 宽 —— 圆形图标、"查看队列 >" 这类
          装饰在这个宽度里会把数字挤到换行,所以它们只在 sm+ 出现。
          手机上留下的是这张卡真正要说的两件事:这是什么数、它是多少。 */}
      <CardContent className="p-3 sm:p-5">
        <div className="flex items-center justify-between sm:mb-4">
          <span className="text-[11px] sm:text-[13px] text-muted-foreground font-medium leading-tight">
            {label}
          </span>
          <div className="hidden sm:flex w-10 h-10 rounded-full bg-secondary items-center justify-center">
            <Icon className="w-5 h-5 text-foreground" strokeWidth={1.75} />
          </div>
        </div>
        <div className="flex items-baseline flex-wrap gap-x-1.5 mt-1 sm:mt-0">
          {loading ? (
            <Skeleton className="w-12 sm:w-16 h-8 sm:h-10 rounded-md" />
          ) : (
            <span
              className={`text-[24px] sm:text-[32px] font-bold leading-none ${valueUnknown ? "text-muted-foreground" : ""}`}
              title={valueUnknown ? "当前数值未知" : undefined}
            >
              {valueUnknown ? "—" : value}
            </span>
          )}
          {extra}
        </div>
        {failed && (
          <p className="mt-1 inline-flex items-center gap-1 text-[10px] sm:text-[11px] text-destructive">
            <AlertTriangle className="w-3 h-3 flex-shrink-0" />
            读取失败
          </p>
        )}
        <Link to={linkTo} className="hidden sm:inline-flex mt-3 items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground">
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

/**
 * 系统状态行。
 *
 * ok 是个 boolean,天然只有"是 / 否"两格 —— 而数据没读到时正确答案是第三格"不知道"。
 * 挤进"否"就会把 offText 说出口:"暂无任务"、"待启用",两句都是在替后端下结论。
 * unknown 就是那第三格:灰点 + 一句"读取失败 / 读取中…",不承诺任何业务事实。
 */
function SystemRow({
  icon,
  label,
  ok,
  onText,
  offText,
  neutralOff,
  warnOff,
  unknown,
  unknownText = "读取失败",
}: {
  icon: React.ReactNode;
  label: string;
  ok: boolean;
  onText: string;
  offText: string;
  neutralOff?: boolean;
  warnOff?: boolean;
  /** 数据没拿到(加载中或请求失败):既不是 on 也不是 off */
  unknown?: boolean;
  unknownText?: string;
}) {
  const dotTone = unknown
    ? "muted"
    : ok
      ? "success"
      : warnOff
        ? "warning"
        : neutralOff
          ? "muted"
          : "danger";
  const text = unknown ? unknownText : ok ? onText : offText;
  return (
    <div className="flex justify-between items-center px-3 py-2.5 rounded-lg hover:bg-secondary transition-colors">
      <div className="inline-flex items-center gap-2 text-[13px]">
        <span className="text-muted-foreground">{icon}</span>
        <span>{label}</span>
      </div>
      <div className="inline-flex items-center gap-1.5">
        <StatusDot tone={dotTone as any} pulse={!unknown && ok} size="xs" />
        <span className={`text-xs ${!unknown && ok ? "font-medium" : "text-muted-foreground"}`}>{text}</span>
      </div>
    </div>
  );
}

/**
 * 未知态的监控环:只画灰轨道,中心是「—」。
 *
 * 几何参数照抄 MetricRing,保证跟旁边有数据的环大小、位置、缺口方向完全一致,
 * 切换时不跳版。存在的唯一理由是 MetricRing 的 percent 是必填 number ——
 * 没有一个 number 能表达"不知道",0 只会被读成"空闲"。
 */
function MetricRingUnknown({
  label,
  hint,
  title,
  failed,
  size,
}: {
  label: string;
  hint: string;
  title?: string;
  failed?: boolean;
  size?: number;
}) {
  // 尺寸口径必须跟 MetricRing 完全一致,否则同一行里"有数据"和"没读到"
  // 两种格子会一大一小,刷新时来回跳版
  const narrow = useIsNarrow();
  size = size ?? (narrow ? 68 : 96);
  const stroke = 8;
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  const visibleArc = 0.75 * c;

  return (
    <div
      className="flex flex-col-reverse sm:flex-row items-center sm:justify-between gap-2 sm:gap-4 px-2.5 py-3 sm:px-5 sm:py-4 h-full"
      title={title}
    >
      <div className="min-w-0 text-center sm:text-left">
        <div className="text-[11px] sm:text-[12px] text-muted-foreground truncate">{label}</div>
        <div
          className={`mt-0.5 sm:mt-1 text-[11px] sm:text-[18px] font-semibold tabular-nums inline-flex items-center gap-1 ${
            failed ? "text-destructive" : "text-muted-foreground"
          }`}
        >
          {failed && <AlertTriangle className="w-3 h-3 sm:w-3.5 sm:h-3.5 flex-shrink-0" />}
          <span className="truncate">{hint}</span>
        </div>
      </div>
      <div className="relative flex-shrink-0" style={{ width: size, height: size }}>
        <svg
          width={size}
          height={size}
          viewBox={`0 0 ${size} ${size}`}
          style={{ transform: "rotate(135deg)" }}
        >
          <circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            strokeWidth={stroke}
            strokeLinecap="round"
            className="stroke-border"
            strokeDasharray={`${visibleArc} ${c}`}
          />
        </svg>
        <div className="absolute inset-0 flex items-center justify-center">
          <span className="text-[20px] font-semibold text-muted-foreground">—</span>
        </div>
      </div>
    </div>
  );
}

