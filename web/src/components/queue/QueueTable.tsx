import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Pencil, PauseCircle, PlayCircle, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { StatusDot } from "@/components/common/StatusDot";
import { AccountChip } from "@/components/common/AccountChip";
import { TimingChip } from "@/components/common/TimingChip";
import { OrderDurationLabel } from "@/components/queue/OrderDurationField";
import { describeOptionCodes } from "@/lib/option-groups";
import { fmtDateTime } from "@/i18n/format";
import type { PurchaseTiming, QueueItem } from "@/hooks/use-queue";
import { cn } from "@/lib/utils";
import type { ReactNode } from "react";
import { useContainerWide } from "@/hooks/use-container-wide";

/** 行内操作与选择回调。 */
export interface QueueRowProps {
  item: QueueItem;
  timing?: PurchaseTiming;
  selected: boolean;
  onSelect: () => void;
  onToggle: () => void;
  onDelete: () => void;
  onEdit: () => void;
  /** 进度列里的重试间隔编辑器,由页面注入(它依赖页面级 mutation) */
  intervalEditor?: ReactNode;
}

const STATUS_TONE = {
  running: "success",
  pending: "warning",
  paused: "muted",
  completed: "info",
  failed: "danger",
} as const;

/** 状态:圆点 + 文字,运行中时下方一行说明当前阶段。只用颜色区分状态,不再套彩色胶囊。 */
function StatusCell({ item }: { item: QueueItem }) {
  const { t } = useTranslation();
  const tone = STATUS_TONE[item.status as keyof typeof STATUS_TONE] ?? "muted";
  const sub =
    item.status === "running" && item.orderPhase
      ? t(`queue.phase.${item.orderPhase}`)
      : item.stopReason
        ? t(`queue.${item.stopReason}`)
        : "";
  return (
    <div className="min-w-0">
      <span className="inline-flex items-center gap-1.5 text-[13px] font-medium whitespace-nowrap">
        <StatusDot tone={tone} pulse={item.status === "running"} size="xs" />
        {t(`queue.status.${item.status}`)}
      </span>
      {sub && <div className="text-[11px] text-muted-foreground mt-0.5 truncate" title={sub}>{sub}</div>}
    </div>
  );
}

/** 设置:周期、自动付款、预售方式、共享上限、优惠码,纯文字以「·」分隔;自动付款用醒目色。 */
function SettingsCell({ item }: { item: QueueItem }) {
  const { t } = useTranslation();
  const parts: ReactNode[] = [<OrderDurationLabel key="d" value={item.duration} />];
  if (item.autoPay) parts.push(<span key="ap" className="text-warning font-medium" title={t("queue.autoPayChipTitle")}>{t("queue.autoPayChip")}</span>);
  if (item.preorder) parts.push(<span key="pre">{t("queue.preorderBadge")}</span>);
  if (item.orderGroup) parts.push(<span key="g">{t("queue.groupLimit", { n: item.groupLimit })}</span>);
  if (item.coupon) parts.push(<span key="c">{t("queue.couponValue", { code: item.coupon })}</span>);
  return (
    <div className="flex flex-wrap gap-x-1 text-[12px] text-muted-foreground leading-5">
      {parts.map((p, i) => (
        <span key={i} className="whitespace-nowrap">
          {i > 0 && <span className="mr-1 opacity-50">·</span>}
          {p}
        </span>
      ))}
    </div>
  );
}

/** 进度:下一次尝试 / 终态说明,加上一轮结论、派发耗时与失败次数。 */
function ProgressCell({ item, timing, intervalEditor }: { item: QueueItem; timing?: PurchaseTiming; intervalEditor?: ReactNode }) {
  const { t } = useTranslation();
  const main =
    item.status === "failed" ? (
      t("queue.stoppedRetrying")
    ) : item.status === "completed" ? (
      <Link to="/history" className="font-medium text-foreground underline-offset-4 hover:underline">
        {item.orderId ? t("queue.orderRecord", { id: item.orderId }) : t("queue.viewHistory")}
      </Link>
    ) : item.status === "paused" ? (
      t(item.stopReason ? `queue.${item.stopReason}` : "queue.waitingResume")
    ) : item.orderPhase === "waitingQuota" ? (
      t("queue.quotaWaitHint")
    ) : item.orderPhase === "submitting" ? (
      t("queue.phase.submitting")
    ) : item.preorder && item.orderPhase ? (
      t("queue.liveSaleHint")
    ) : item.retryCount > 0 ? (
      <span className="inline-flex items-center gap-1">
        {t("queue.nextAttempt")}
        {intervalEditor}
        {t("queue.retryInSec", { n: item.retryCount + 1 })}
      </span>
    ) : (
      t("queue.startingSoon")
    );

  const extras: ReactNode[] = [];
  if (timing && item.status !== "completed") {
    extras.push(
      <span key="last">
        {t("queue.lastRound")}
        {timing.outcome === "unavailable" ? t("queue.outcome.unavailable") : timing.outcome === "ordered" ? t("queue.outcome.ordered") : t("queue.outcome.error")}
      </span>,
    );
  }
  if (timing?.dispatch?.wroteRequest) {
    extras.push(
      <span
        key="dispatch"
        title={`${t(timing.dispatch.prewarmed ? "queue.cartPrewarmed" : "queue.cartCold")} · ${t(timing.dispatch.connectionReused ? "queue.connectionReused" : "queue.connectionCold")}`}
      >
        {t("queue.dispatchDelay", { us: timing.dispatch.detectionToWriteUs })}
        {timing.dispatchStats && (
          <span className="ml-1" title={t("queue.dispatchWindow")}>
            {t("queue.dispatchPercentiles", { p50: timing.dispatchStats.p50Us, p99: timing.dispatchStats.p99Us, n: timing.dispatchStats.samples })}
          </span>
        )}
      </span>,
    );
  }
  if (typeof item.failureCount === "number" && item.failureCount > 0) {
    extras.push(<span key="fail">{t("queue.failureCount", { n: item.failureCount })}</span>);
  }
  extras.push(<span key="created" className="tnum" title={t("queue.col.created")}>{fmtDateTime(item.createdAt)}</span>);

  return (
    <div className="text-[12px] leading-5 min-w-0">
      <div className="text-foreground/90">{main}</div>
      {extras.length > 0 && (
        <div className="text-[11px] text-muted-foreground flex flex-wrap gap-x-2">
          {extras}
          {item.status !== "completed" && <TimingChip totalMs={timing?.totalMs} phases={timing?.phases} />}
        </div>
      )}
    </div>
  );
}

/** 行内操作:图标按钮,悬停/读屏有完整说明。 */
function RowActions({ item, onEdit, onToggle, onDelete }: Pick<QueueRowProps, "item" | "onEdit" | "onToggle" | "onDelete">) {
  const { t } = useTranslation();
  const canToggle = item.status !== "completed" && item.status !== "failed" && item.stopReason !== "groupComplete";
  const toggleLabel = item.status === "paused" ? t("queue.resume") : t("queue.pause");
  return (
    <div className="flex items-center justify-end gap-0.5">
      {canToggle && (
        <Button variant="ghost" size="icon" className="h-8 w-8" onClick={onToggle} aria-label={toggleLabel} title={toggleLabel}>
          {item.status === "paused" ? <PlayCircle className="w-4 h-4" /> : <PauseCircle className="w-4 h-4" />}
        </Button>
      )}
      {item.status !== "completed" && (
        <Button variant="ghost" size="icon" className="h-8 w-8" onClick={onEdit} aria-label={t("queue.edit")} title={t("queue.edit")}>
          <Pencil className="w-4 h-4" />
        </Button>
      )}
      <Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-destructive" onClick={onDelete} aria-label={t("queue.delete")} title={t("queue.delete")}>
        <X className="w-4 h-4" />
      </Button>
    </div>
  );
}

/** 型号列:型号 + 配置描述(第二行,弱化)。 */
function PlanCell({ item }: { item: QueueItem }) {
  return (
    <div className="min-w-0">
      <div className="font-mono font-semibold text-[13px] truncate">{item.planCode}</div>
      {item.options && item.options.length > 0 && (
        <div className="text-[11px] text-muted-foreground truncate" title={item.options.join("\n")}>
          {describeOptionCodes(item.options)}
        </div>
      )}
    </div>
  );
}

/** 桌面表格的一行。 */
export function QueueRow(props: QueueRowProps) {
  const { t } = useTranslation();
  const { item, selected, onSelect } = props;
  return (
    <tr className={cn("border-b border-border last:border-0 align-top transition-colors hover:bg-muted/40", selected && "bg-muted/50")}>
      <td className="pl-4 pr-2 py-3 w-8">
        <Checkbox checked={selected} onCheckedChange={onSelect} aria-label={t("queue.selectTaskAria", { plan: item.planCode, dc: item.datacenter })} />
      </td>
      <td className="px-2 py-3 max-w-[200px]"><PlanCell item={item} /></td>
      <td className="px-2 py-3 font-mono text-[12px] uppercase whitespace-nowrap">{item.datacenter}</td>
      <td className="px-2 py-3"><AccountChip accountId={item.accountId} /></td>
      <td className="px-2 py-3 w-[130px]"><StatusCell item={item} /></td>
      <td className="px-2 py-3 min-w-[130px] max-w-[220px]"><SettingsCell item={item} /></td>
      <td className="px-2 py-3 min-w-[180px]"><ProgressCell item={item} timing={props.timing} intervalEditor={props.intervalEditor} /></td>
      <td className="pl-2 pr-3 py-2.5"><RowActions {...props} /></td>
    </tr>
  );
}

/** 手机端的一行:型号/机房/状态一行,设置与进度各一行,操作在右上。 */
export function QueueMobileRow(props: QueueRowProps) {
  const { t } = useTranslation();
  const { item, selected, onSelect } = props;
  return (
    <div className={cn("px-3 py-3 border-b border-border last:border-0", selected && "bg-muted/50")}>
      <div className="flex items-start gap-2.5">
        <Checkbox className="mt-0.5" checked={selected} onCheckedChange={onSelect} aria-label={t("queue.selectTaskAria", { plan: item.planCode, dc: item.datacenter })} />
        <div className="min-w-0 flex-1 space-y-1.5">
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="font-mono font-semibold text-[13px] truncate">{item.planCode}</span>
                <span className="font-mono text-[11px] uppercase text-muted-foreground">{item.datacenter}</span>
              </div>
              {item.options && item.options.length > 0 && (
                <div className="text-[11px] text-muted-foreground truncate">{describeOptionCodes(item.options)}</div>
              )}
            </div>
            <RowActions {...props} />
          </div>
          <div className="flex items-center justify-between gap-2">
            <StatusCell item={item} />
            <AccountChip accountId={item.accountId} />
          </div>
          <SettingsCell item={item} />
          <ProgressCell item={item} timing={props.timing} intervalEditor={props.intervalEditor} />
        </div>
      </div>
    </div>
  );
}

/** 抢购任务表:桌面为表格,手机为紧凑列表;表头复选框全选当前筛选结果。 */
export function QueueTable({
  items,
  rowProps,
  allSelected,
  onSelectAll,
}: {
  items: QueueItem[];
  rowProps: (item: QueueItem) => QueueRowProps;
  allSelected: boolean;
  onSelectAll: () => void;
}) {
  const { t } = useTranslation();
  const head = "px-2 py-2.5 text-left text-[11px] font-medium text-muted-foreground uppercase tracking-wide whitespace-nowrap";
  const [ref, wide] = useContainerWide(TABLE_MIN_WIDTH);
  return (
    <div ref={ref}>
      {wide ? (
      <div>
        <table className="w-full text-sm">
          <thead className="border-b border-border">
            <tr>
              <th className="pl-4 pr-2 py-2.5 w-8">
                <Checkbox checked={allSelected} onCheckedChange={onSelectAll} aria-label={allSelected ? t("queue.unselectAll") : t("queue.selectAll")} />
              </th>
              <th className={head}>{t("queue.col.plan")}</th>
              <th className={head}>{t("queue.col.dc")}</th>
              <th className={head}>{t("queue.col.account")}</th>
              <th className={head}>{t("queue.col.status")}</th>
              <th className={head}>{t("queue.col.settings")}</th>
              <th className={head}>{t("queue.col.progress")}</th>
              <th className={cn(head, "text-right pr-4")}>{t("queue.col.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => <QueueRow key={item.id} {...rowProps(item)} />)}
          </tbody>
        </table>
      </div>
      ) : (
      <div>
        <div className="flex items-center gap-2 px-3 py-2 border-b border-border">
          <Checkbox checked={allSelected} onCheckedChange={onSelectAll} aria-label={allSelected ? t("queue.unselectAll") : t("queue.selectAll")} />
          <span className="text-[12px] text-muted-foreground">{allSelected ? t("queue.unselectAll") : t("queue.selectAll")}</span>
        </div>
        {items.map((item) => <QueueMobileRow key={item.id} {...rowProps(item)} />)}
      </div>
      )}
    </div>
  );
}

/** 表格完整显示所需的最小宽度;不足时改用紧凑列表,不出现横向滚动。 */
const TABLE_MIN_WIDTH = 920;
