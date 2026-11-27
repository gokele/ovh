import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { CheckCircle2, Server as ServerIcon, ChevronLeft, ChevronRight, CreditCard, Loader2, Minus, Plus, ChevronsDownUp, ChevronsUpDown, Circle, Clock3, MoreHorizontal, Pencil, PauseCircle, PlayCircle, RotateCw, Trash2, X, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { StatusDot } from "@/components/common/StatusDot";
import { AccountChip } from "@/components/common/AccountChip";
import { OrderTrackingButton } from "@/components/account/OrderTrackingButton";
import { TimingChip } from "@/components/common/TimingChip";
import { OrderDurationLabel } from "@/components/queue/OrderDurationField";
import { describeOptionCodes } from "@/lib/option-groups";
import { fmtDateTime } from "@/i18n/format";
import { useSetQueueAutoPay, useUpdateQueueBundle, type PurchaseTiming, type QueueItem } from "@/hooks/use-queue";
import { useConfirm } from "@/components/common/ConfirmDialog";
import { AutoPayStatus } from "@/components/common/AutoPayStatus";
import { cn } from "@/lib/utils";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Chip } from "@/components/common/Chip";
import { Switch } from "@/components/ui/switch";
import { useServers } from "@/hooks/use-servers";
import { useAccounts, useDefaultAccount } from "@/hooks/use-accounts";
import { fqnMatchesOption, useAvailabilityByEndpoints, variantDcStatus, variantsForPlan } from "@/hooks/use-availability";
import { isOrderable } from "@/lib/availability";
import { apiDatacenterCode } from "@/lib/datacenters";
import { endpointRegion } from "@/lib/ovh-regions";

/** 行内操作与选择回调。 */
export interface QueueRowProps {
  item: QueueItem;
  timing?: PurchaseTiming;
  selected: boolean;
  onSelect: () => void;
  onToggle: () => void;
  onDelete: () => void;
  onEdit: () => void;
  /** 已下单 / 已失败的任务按原设置再建一个(页面弹确认框) */
  onBuyAgain?: () => void;
  /** 进度列里的重试间隔编辑器,由页面注入(它依赖页面级 mutation) */
  intervalEditor?: ReactNode;
  /** 在套餐分组里展开显示:左侧缩进与竖线 */
  nested?: boolean;
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

function CompletedOrderLinks({ item }: { item: QueueItem }) {
  const { t } = useTranslation();
  return (
    <span className="inline-flex flex-wrap items-center gap-x-2">
      <Link to="/history" className="font-medium underline-offset-4 hover:underline">
        {item.orderId ? t("queue.orderRecord", { id: item.orderId }) : t("queue.viewHistory")}
      </Link>
      <OrderTrackingButton accountId={item.accountId} orderId={item.orderId} />
    </span>
  );
}

/** 进度:下一次尝试 / 终态说明,加上一轮结论、派发耗时与失败次数。 */
function ProgressCell({ item, timing, intervalEditor }: { item: QueueItem; timing?: PurchaseTiming; intervalEditor?: ReactNode }) {
  const { t } = useTranslation();
  const main =
    item.status === "failed" ? (
      t("queue.stoppedRetrying")
    ) : item.status === "completed" ? (
      <CompletedOrderLinks item={item} />
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

/**
 * 行内操作:主操作是带文字的按钮(暂停 / 恢复 / 再抢一单),一眼能看懂、好点中;
 * 编辑与删除是次要操作,保留图标按钮。
 */
function RowActions({ item, onEdit, onToggle, onDelete, onBuyAgain }: Pick<QueueRowProps, "item" | "onEdit" | "onToggle" | "onDelete" | "onBuyAgain">) {
  const { t } = useTranslation();
  const finished = item.status === "completed" || item.status === "failed";
  const canToggle = !finished && item.stopReason !== "groupComplete";
  const paused = item.status === "paused";
  return (
    <div className="flex items-center justify-end gap-1">
      {canToggle && (
        <Button
          variant={paused ? "default" : "outline"}
          size="sm"
          className="h-8 px-2.5 gap-1 text-[12.5px]"
          onClick={onToggle}
        >
          {paused ? <PlayCircle className="w-3.5 h-3.5" /> : <PauseCircle className="w-3.5 h-3.5" />}
          {paused ? t("queue.resume") : t("queue.pause")}
        </Button>
      )}
      {finished && onBuyAgain && (
        <Button variant="outline" size="sm" className="h-8 px-2.5 gap-1 text-[12.5px] text-primary border-primary/40 hover:bg-primary/5" onClick={onBuyAgain}>
          <RotateCw className="w-3.5 h-3.5" />
          {t("queue.buyAgain")}
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
    <tr className={cn("border-b border-border last:border-0 align-top transition-colors hover:bg-muted/40", selected && "bg-muted/50", props.nested && "bg-primary/[.02] animate-in fade-in slide-in-from-top-1 duration-200")}>
      <td className={cn("pr-2 py-3 w-8", props.nested ? "pl-4 shadow-[inset_3px_0_0_hsl(var(--primary)/.5)]" : "pl-4")}>
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




/** 组摘要:机房 → 任务数(多到少)、各状态数量、共享上限 */
function summarize(items: QueueItem[]) {
  const dcs = new Map<string, number>();
  const statuses = new Map<string, number>();
  const limits = new Set<number>();
  for (const it of items) {
    const dc = (it.datacenter || "").toUpperCase();
    dcs.set(dc, (dcs.get(dc) || 0) + 1);
    statuses.set(it.status, (statuses.get(it.status) || 0) + 1);
    if (it.orderGroup && it.groupLimit) limits.add(it.groupLimit);
  }
  const order: string[] = ["running", "pending", "paused", "failed", "completed"];
  return {
    dcs: [...dcs.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])),
    statuses: [...statuses.entries()].sort((a, b) => order.indexOf(a[0]) - order.indexOf(b[0])),
    limit: limits.size === 1 ? [...limits][0] : undefined,
  };
}


/** 组头复选框:全选 / 部分选中 / 未选 */
function groupCheckState(items: QueueItem[], rowProps: (item: QueueItem) => QueueRowProps): boolean | "indeterminate" {
  const sel = items.filter((it) => rowProps(it).selected).length;
  return sel === 0 ? false : sel === items.length ? true : "indeterminate";
}

function toggleGroup(items: QueueItem[], rowProps: (item: QueueItem) => QueueRowProps) {
  const all = items.every((it) => rowProps(it).selected);
  for (const it of items) {
    const p = rowProps(it);
    if (all ? p.selected : !p.selected) p.onSelect();
  }
}

export type GroupAction = (items: QueueItem[], action: "pause" | "resume", planCode: string) => void;



/** 状态大图标(参考 GitHub Actions):运行中转圈、暂停、已下单打勾、失败打叉 */
function TaskStatusIcon({ item }: { item: QueueItem }) {
  const { t } = useTranslation();
  const label = t(`queue.status.${item.status}`);
  const common = "w-5 h-5 shrink-0";
  switch (item.status) {
    case "running":
      return (
        <span className="relative inline-flex w-5 h-5" title={label} aria-label={label} role="img">
          <span className="absolute inset-0 rounded-full border-2 border-success/25" />
          <span className="absolute inset-0 rounded-full border-2 border-transparent border-t-success animate-spin [animation-duration:1.4s]" />
          <span className="absolute inset-[6px] rounded-full bg-success" />
        </span>
      );
    case "pending":
      return <Clock3 className={cn(common, "text-muted-foreground")} aria-label={label} />;
    case "paused":
      return <PauseCircle className={cn(common, "text-warning")} aria-label={label} />;
    case "completed":
      return <CheckCircle2 className={cn(common, "text-primary")} aria-label={label} />;
    case "failed":
      return <XCircle className={cn(common, "text-destructive")} aria-label={label} />;
    default:
      return <Circle className={cn(common, "text-muted-foreground")} aria-label={label} />;
  }
}

/** 每秒更新一次的「现在」;只有可见的倒计时会用到 */
function useNow(active: boolean) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [active]);
  return now;
}

/** 下次尝试:倒计时圆环 + 秒数;下面一行「每 N 秒(可点改) · 第 k 次」。非运行状态显示状态说明。 */
function NextAttempt({ item, intervalEditor }: { item: QueueItem; intervalEditor?: ReactNode }) {
  const { t } = useTranslation();
  // 每次尝试都会刷新 lastCheckTime;「等待补货 / 等报价」仍按间隔重试,照常倒计时。
  // 名额处理中、提交中与预售开售说明不是按间隔走的,显示说明文字。
  const phase = item.orderPhase || "";
  const ticking =
    (item.status === "running" || item.status === "pending") &&
    phase !== "waitingQuota" &&
    phase !== "submitting" &&
    !(item.preorder && phase);
  const now = useNow(ticking);

  if (!ticking) {
    // 非倒计时状态沿用原有说明:已下单链接 / 暂停原因 / 等待配额 / 提交中 / 预售说明 / 停止重试
    return <ProgressMain item={item} intervalEditor={intervalEditor} />;
  }
  const interval = Math.max(1, item.retryInterval || 1);
  const last = item.lastCheckTime ? item.lastCheckTime * 1000 : 0;
  const remaining = last ? Math.max(0, Math.ceil((last + interval * 1000 - now) / 1000)) : 0;
  const progress = last ? Math.min(1, (now - last) / (interval * 1000)) : 0;
  const r = 8;
  const c = 2 * Math.PI * r;
  return (
    <div className="flex items-center gap-2.5 min-w-0">
      <svg viewBox="0 0 20 20" className="w-5 h-5 -rotate-90 shrink-0" aria-hidden>
        <circle cx="10" cy="10" r={r} fill="none" strokeWidth="2.5" className="stroke-muted" />
        <circle cx="10" cy="10" r={r} fill="none" strokeWidth="2.5" strokeLinecap="round" stroke="hsl(var(--primary))" strokeDasharray={`${progress * c} ${c}`} className="transition-[stroke-dasharray] duration-1000 ease-linear" />
      </svg>
      <div className="min-w-0 leading-tight">
        <div className="text-[13px] font-semibold tabular-nums">
          {item.retryCount > 0 && last && remaining > 0 ? t("queue.row.nextIn", { s: remaining }) : t("queue.row.soon")}
        </div>
        <div className="text-[11.5px] text-muted-foreground">
          {t("queue.row.every")} {intervalEditor ?? item.retryInterval} {t("queue.row.sec")}
          {item.retryCount > 0 && <span className="whitespace-nowrap"> · {t("queue.row.attemptN", { n: item.retryCount + 1 })}</span>}
        </div>
      </div>
    </div>
  );
}

/** 非倒计时状态的说明文字(与 ProgressCell 主文案一致) */
function ProgressMain({ item, intervalEditor }: { item: QueueItem; intervalEditor?: ReactNode }) {
  const { t } = useTranslation();
  const text =
    item.status === "failed" ? (
      t("queue.stoppedRetrying")
    ) : item.status === "completed" ? (
      <CompletedOrderLinks item={item} />
    ) : item.status === "paused" ? (
      t(item.stopReason ? `queue.${item.stopReason}` : "queue.waitingResume")
    ) : item.orderPhase === "waitingQuota" ? (
      t("queue.quotaWaitHint")
    ) : item.orderPhase === "submitting" ? (
      t("queue.phase.submitting")
    ) : item.preorder && item.orderPhase ? (
      t("queue.liveSaleHint")
    ) : (
      <span className="inline-flex items-center gap-1">
        {t("queue.nextAttempt")} {intervalEditor} {t("queue.retryInSec", { n: item.retryCount + 1 })}
      </span>
    );
  return <div className="text-[12.5px] text-foreground/85 leading-snug min-w-0">{text}</div>;
}

/** 上一轮:结论标签 + 耗时(可点看分解)+ 派发延迟;下面一行时间。没有数据时说明「还没有尝试」。 */
function LastRound({ item, timing }: { item: QueueItem; timing?: PurchaseTiming }) {
  const { t } = useTranslation();
  if (item.status === "completed") {
    return <div className="text-[11.5px] text-muted-foreground tnum">{fmtDateTime(item.updatedAt || item.createdAt)}</div>;
  }
  if (!timing) {
    return <div className="text-[12px] text-muted-foreground">{t("queue.row.noRound")}</div>;
  }
  const tone = timing.outcome === "ordered" ? "success" : timing.outcome === "unavailable" ? "default" : "danger";
  return (
    <div className="min-w-0 leading-tight space-y-1">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <Chip tone={tone}>
          {timing.outcome === "unavailable" ? t("queue.outcome.unavailable") : timing.outcome === "ordered" ? t("queue.outcome.ordered") : t("queue.outcome.error")}
        </Chip>
        <TimingChip totalMs={timing.totalMs} phases={timing.phases} />
        {timing.dispatch?.wroteRequest && (
          <span
            className="text-[11px] text-muted-foreground tabular-nums whitespace-nowrap"
            title={`${t(timing.dispatch.prewarmed ? "queue.cartPrewarmed" : "queue.cartCold")} · ${t(timing.dispatch.connectionReused ? "queue.connectionReused" : "queue.connectionCold")}`}
          >
            {t("queue.dispatchDelay", { us: timing.dispatch.detectionToWriteUs })}
          </span>
        )}
      </div>
      <div className="text-[11px] text-muted-foreground tnum whitespace-nowrap">
        {fmtDateTime(timing.at)}
        {typeof item.failureCount === "number" && item.failureCount > 0 && <span className="ml-2 text-destructive/80">{t("queue.failureCount", { n: item.failureCount })}</span>}
      </div>
    </div>
  );
}

/** 「…」菜单:编辑 / 再抢一单 / 删除(删除放最后,红色) */
function RowMenu({ item, onEdit, onDelete, onBuyAgain }: Pick<QueueRowProps, "item" | "onDelete" | "onBuyAgain"> & { onEdit?: () => void }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const finished = item.status === "completed" || item.status === "failed";
  const itemCls = "w-full flex items-center gap-2 px-2.5 h-9 rounded-md text-[13px] text-left hover:bg-muted transition-colors";
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon" className="h-8 w-8" aria-label={t("queue.row.more")} title={t("queue.row.more")}>
          <MoreHorizontal className="w-4 h-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-44 p-1.5">
        {item.status !== "completed" && onEdit && (
          <button type="button" className={itemCls} onClick={() => { setOpen(false); onEdit(); }}>
            <Pencil className="w-4 h-4 text-muted-foreground" />{t("queue.edit")}
          </button>
        )}
        {finished && onBuyAgain && (
          <button type="button" className={itemCls} onClick={() => { setOpen(false); onBuyAgain(); }}>
            <RotateCw className="w-4 h-4 text-muted-foreground" />{t("queue.buyAgain")}
          </button>
        )}
        <div className="my-1 h-px bg-border" />
        <button type="button" className={cn(itemCls, "text-destructive hover:bg-destructive/10")} onClick={() => { setOpen(false); onDelete(); }}>
          <Trash2 className="w-4 h-4" />{t("queue.delete")}
        </button>
      </PopoverContent>
    </Popover>
  );
}

const PAGE_SIZE = 20;

const SWITCH_CLS = "data-[state=checked]:bg-success data-[state=unchecked]:bg-input";

/**
 * 行内主操作(参考交易机器人面板):运行 / 暂停用开关,开 = 抢购中,关 = 已暂停,一下切换;
 * 已下单 / 失败的任务没有开关,换成「再抢一单」。
 */
function PrimaryAction({ item, onToggle, onBuyAgain }: Pick<QueueRowProps, "item" | "onToggle" | "onBuyAgain">) {
  const { t } = useTranslation();
  // 失败的任务:在原任务上重新开始(后端清零失败计数),不再复制出一条新任务
  if (item.status === "failed") {
    return (
      <Button variant="outline" size="sm" className="h-8 px-3 gap-1.5 text-[12.5px] text-primary border-primary/40 hover:bg-primary/5" onClick={onToggle}>
        <PlayCircle className="w-3.5 h-3.5" />{t("queue.restart")}
      </Button>
    );
  }
  if (item.status === "completed") {
    return onBuyAgain ? (
      <Button variant="outline" size="sm" className="h-8 px-3 gap-1.5 text-[12.5px] text-primary border-primary/40 hover:bg-primary/5" onClick={onBuyAgain}>
        <RotateCw className="w-3.5 h-3.5" />{t("queue.buyAgain")}
      </Button>
    ) : null;
  }
  if (item.stopReason === "groupComplete") return null;
  const on = item.status === "running" || item.status === "pending";
  return (
    <label className="inline-flex items-center gap-2 h-8 pl-2.5 pr-1.5 rounded-lg hover:bg-muted/70 cursor-pointer select-none">
      <span className={cn("hidden sm:inline text-[12.5px] font-medium min-w-[3.5em] text-right", on ? "text-success" : "text-muted-foreground")}>
        {on ? t("queue.row.switchOn") : t("queue.row.switchOff")}
      </span>
      <Switch
        checked={on}
        onCheckedChange={() => onToggle()}
        className={SWITCH_CLS}
        aria-label={t("queue.row.switchAria", { plan: item.planCode, dc: item.datacenter.toUpperCase() })}
      />
    </label>
  );
}

/**
 * 单个任务行(参考 GitHub Actions / Linear):
 *   ☐ 状态图标 | 机房 + 配置(标题)/ 账户 · 周期 · 自动付款…(次行) | 下次尝试 | 上一轮 | 主操作 …
 * showPlan:平铺视图里在标题前加套餐名。布局按容器宽度切换(.task-row,见 calm.css)。
 */
function TaskRow({ showPlan, planName, compact, ...props }: QueueRowProps & { showPlan?: boolean; planName?: string; compact?: boolean }) {
  const { t } = useTranslation();
  const { item, selected, onSelect } = props;
  return (
    <div className={cn("task-row px-4 py-3 transition-colors hover:bg-primary/[.03]", selected && "bg-primary/[.05]")}>
      <div className="tr-check pt-0.5">
        <Checkbox checked={selected} onCheckedChange={onSelect} aria-label={t("queue.selectTaskAria", { plan: item.planCode, dc: item.datacenter })} />
      </div>
      <div className="tr-icon pt-px"><TaskStatusIcon item={item} /></div>
      <div className="tr-main min-w-0">
        <div className="flex items-start sm:items-center gap-2 min-w-0">
          {showPlan && (
            <span className="shrink-0 font-semibold text-[13.5px]">
              {planName || <span className="font-mono">{item.planCode}</span>}
            </span>
          )}
          <span className="shrink-0 inline-flex items-center h-[22px] px-1.5 rounded-md bg-muted font-mono text-[12px] font-semibold uppercase">{item.datacenter}</span>
          {compact ? (
            <span className="text-[13px] text-muted-foreground">{t(`queue.status.${item.status}`)}</span>
          ) : (
            <span className="min-w-0 line-clamp-2 sm:line-clamp-1 text-[13px] font-medium" title={item.options?.join("\n")}>
              {item.options && item.options.length > 0 ? describeOptionCodes(item.options) : t("history.defaultOptions")}
            </span>
          )}
          {compact && item.status === "running" && item.orderPhase && <span className="text-[12px] text-primary">{t(`queue.phase.${item.orderPhase}`)}</span>}
        </div>
        <div className={cn("mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[12px] text-muted-foreground", compact && "hidden")}>
          <AccountChip accountId={item.accountId} />
          <SettingsCell item={item} />
          {item.status === "running" && item.orderPhase && <span className="text-primary">{t(`queue.phase.${item.orderPhase}`)}</span>}
        </div>
      </div>
      <div className="tr-next min-w-0"><NextAttempt item={item} intervalEditor={props.intervalEditor} /></div>
      <div className="tr-last min-w-0"><LastRound item={item} timing={props.timing} /></div>
      <div className="tr-act flex items-center justify-end gap-1">
        <PrimaryAction item={item} onToggle={props.onToggle} onBuyAgain={props.onBuyAgain} />
        <RowMenu item={item} onEdit={compact ? undefined : props.onEdit} onDelete={props.onDelete} onBuyAgain={props.onBuyAgain} />
      </div>
    </div>
  );
}

/**
 * 同一单:账户、型号、配置、周期、自动付款、优惠码、预售、合计组都相同的任务合并成一行,
 * 显示数量和机房分布;选中这一行 = 选中它的全部任务。只有一个任务时照常显示单行。
 */
interface Bundle {
  key: string;
  items: QueueItem[];
}

function bundleKey(it: QueueItem): string {
  return JSON.stringify([
    it.accountId,
    it.planCode,
    [...(it.options || [])].sort(),
    it.duration || "",
    !!it.autoPay,
    (it.coupon || "").trim(),
    !!it.preorder,
    it.orderGroup || "",
  ]);
}

/** 相同设置合并后的「单」数 */
export function countOrders(items: QueueItem[]): number {
  return new Set(items.map(bundleKey)).size;
}

function bundleItems(items: QueueItem[]): Bundle[] {
  const map = new Map<string, Bundle>();
  for (const it of items) {
    const k = bundleKey(it);
    const b = map.get(k);
    if (b) b.items.push(it);
    else map.set(k, { key: k, items: [it] });
  }
  return [...map.values()];
}

/** 这一单的代表任务:优先正在抢的,其次排队、暂停、失败、已下单 */
function representative(items: QueueItem[]): QueueItem {
  const rank: Record<string, number> = { running: 0, pending: 1, paused: 2, failed: 3, completed: 4 };
  return [...items].sort((a, b) => (rank[a.status] ?? 9) - (rank[b.status] ?? 9))[0];
}

/** 一单的整体状态:有在抢的算在抢,否则暂停 / 失败 / 已下单 */
function orderStatus(items: QueueItem[]): keyof typeof STATUS_TONE {
  if (items.some((i) => i.status === "running")) return "running";
  if (items.some((i) => i.status === "pending")) return "pending";
  if (items.some((i) => i.status === "paused")) return "paused";
  if (items.every((i) => i.status === "completed")) return "completed";
  if (items.some((i) => i.status === "failed")) return "failed";
  return "paused";
}

/** 机房标签:FRA ×2,按实时库存着色(绿 = 有货,灰 = 无货,不着色 = 还没查到) */
function DcPills({ dcs, stock }: { dcs: [string, number][]; stock?: Record<string, boolean> }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap gap-1.5">
      {dcs.map(([dc, n]) => {
        const s = stock?.[dc.toLowerCase()];
        return (
          <span
            key={dc}
            title={s === undefined ? undefined : t(s ? "queue.bundle.inStock" : "queue.bundle.noStock")}
            className={cn(
              "inline-flex items-center gap-1 h-6 pl-2 pr-2 rounded-full text-[11.5px] font-mono font-semibold tracking-wide whitespace-nowrap ring-1 ring-inset",
              s ? "bg-success/10 text-success ring-success/25" : "bg-muted/60 text-foreground/75 ring-border",
            )}
          >
            {s && <span className="w-1.5 h-1.5 rounded-full bg-success" />}
            {dc}
            {n > 1 && <span className={cn("font-sans font-medium tabular-nums", s ? "text-success/80" : "text-muted-foreground")}>×{n}</span>}
            {s && <span className="font-sans font-medium">{t("queue.bundle.inStock")}</span>}
            {s === false && <span className="sr-only">{t("queue.bundle.noStock")}</span>}
          </span>
        );
      })}
    </div>
  );
}

/** 进度:已下单 / 总数 + 细进度条 */
function OrderProgress({ done, total }: { done: number; total: number }) {
  const { t } = useTranslation();
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;
  return (
    <div className="min-w-0">
      <div className="flex items-baseline justify-between gap-2 text-[12px]">
        <span className="text-muted-foreground">{t("queue.bundle.ordered", { done, n: total })}</span>
        <span className="tabular-nums text-muted-foreground">{pct}%</span>
      </div>
      <div className="mt-1 h-1.5 rounded-full bg-muted overflow-hidden">
        <div className={cn("h-full rounded-full transition-all", done >= total && total > 0 ? "bg-success" : "bg-primary")} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}


type Section = "running" | "paused" | "failed" | "completed";
function sectionOf(items: QueueItem[]): Section {
  const st = orderStatus(items);
  return st === "pending" ? "running" : st;
}

interface OrderRowProps {
  bundle: Bundle;
  name?: string;
  stock?: Record<string, boolean>;
  rowProps: (item: QueueItem) => QueueRowProps;
  onGroupAction?: GroupAction;
  busy?: boolean;
  onOpen: () => void;
  onEdit?: (items: QueueItem[]) => void;
  onDelete?: (items: QueueItem[]) => void;
}

/**
 * 一单的「…」菜单:编辑整单 / 自动付款 / 删除整单。都作用于整单,不是其中一台。
 * 打开自动付款前确认(会扣款);删除走页面的批量删除确认。
 */
function BundleMenu({ items, onEdit, onDelete }: { items: QueueItem[]; onEdit?: (items: QueueItem[]) => void; onDelete?: (items: QueueItem[]) => void }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const setAutoPay = useSetQueueAutoPay();
  const [confirm, confirmDialog] = useConfirm();
  const unfinished = items.filter((i) => i.status !== "completed");
  const autoOn = unfinished.length > 0 && unfinished.every((i) => !!i.autoPay);
  const itemCls = "w-full flex items-center gap-2 px-2.5 h-9 rounded-md text-[13px] text-left hover:bg-muted transition-colors";
  if (!onEdit && !onDelete && unfinished.length === 0) return null;
  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button variant="ghost" size="icon" className="h-8 w-8" aria-label={t("queue.bundle.menu")} title={t("queue.bundle.menu")}>
            <MoreHorizontal className="w-4 h-4" />
          </Button>
        </PopoverTrigger>
        <PopoverContent align="end" className="w-48 p-1.5">
          {onEdit && unfinished.length > 0 && (
            <button type="button" className={itemCls} onClick={() => { setOpen(false); onEdit(items); }}>
              <Pencil className="w-4 h-4 text-muted-foreground" />{t("queue.bundle.editOrder")}
            </button>
          )}
          {unfinished.length > 0 && (
            <button
              type="button"
              className={itemCls}
              disabled={setAutoPay.isPending}
              onClick={async () => {
                setOpen(false);
                if (!autoOn) {
                  const ok = await confirm({
                    title: t("queue.autoPayToggle.confirmTitle"),
                    description: t("queue.autoPayToggle.confirmDesc", { n: unfinished.length }),
                    confirmText: t("queue.autoPayToggle.confirmOk"),
                  });
                  if (!ok) return;
                }
                setAutoPay.mutate({ ids: unfinished.map((i) => i.id), autoPay: !autoOn });
              }}
            >
              <CreditCard className="w-4 h-4 text-muted-foreground" />
              {t(autoOn ? "queue.autoPayToggle.batchOff" : "queue.autoPayToggle.batchOn")}
            </button>
          )}
          {onDelete && (
            <>
              <div className="my-1 h-px bg-border" />
              <button type="button" className={cn(itemCls, "text-destructive hover:bg-destructive/10")} onClick={() => { setOpen(false); onDelete(items); }}>
                <Trash2 className="w-4 h-4" />{t("queue.bundle.deleteAll", { n: items.length })}
              </button>
            </>
          )}
        </PopoverContent>
      </Popover>
      {confirmDialog}
    </>
  );
}

/** 每种状态一套颜色:左侧色条、型号图标底色、状态标签 */
const SEC_STYLE: Record<Section, { bar: string; tile: string; pill: string; dot: "success" | "warning" | "danger" | "info" }> = {
  running: { bar: "bg-success", tile: "bg-success/10 text-success", pill: "bg-success/10 text-success", dot: "success" },
  paused: { bar: "bg-warning", tile: "bg-warning/10 text-warning", pill: "bg-warning/10 text-warning", dot: "warning" },
  failed: { bar: "bg-destructive", tile: "bg-destructive/10 text-destructive", pill: "bg-destructive/10 text-destructive", dot: "danger" },
  completed: { bar: "bg-primary", tile: "bg-primary/10 text-primary", pill: "bg-primary/10 text-primary", dot: "info" },
};

/** 状态列:在抢(倒计时)/ 已暂停 / 失败 / 已下单;暂停与失败的原因放在悬停提示里 */
function OrderStatusCell({ items, sec }: { items: QueueItem[]; sec: Section }) {
  const { t } = useTranslation();
  const now = useNow(sec === "running");
  if (sec === "running") {
    const live = items.filter((i) => i.status === "running" || i.status === "pending");
    let next = Infinity;
    for (const i of live) {
      if (!i.lastCheckTime) continue;
      const left = Math.ceil((i.lastCheckTime * 1000 + Math.max(1, i.retryInterval || 1) * 1000 - now) / 1000);
      next = Math.min(next, Math.max(0, left));
    }
    const phase = live.find((i) => i.orderPhase && i.orderPhase !== "waitingStock")?.orderPhase;
    const attempts = items.reduce((n, i) => n + (i.retryCount || 0), 0);
    return (
      <span className="block leading-5" title={t("queue.bundle.attempts", { n: attempts.toLocaleString() })}>
        <span className={cn("inline-flex items-center gap-1.5 h-6 px-2.5 rounded-full text-[12px] font-medium whitespace-nowrap", SEC_STYLE.running.pill)}>
          <StatusDot tone="success" pulse size="xs" />
          {t("queue.bundle.section.running")}
        </span>
        <span className="block mt-1 pl-1 text-[11.5px] text-muted-foreground tabular-nums truncate">
          {phase ? t(`queue.phase.${phase}`) : Number.isFinite(next) && next > 0 ? t("queue.bundle.nextShort", { s: next }) : t("queue.bundle.soon")}
        </span>
      </span>
    );
  }
  const reasonItem = items.find((i) => i.status === sec && i.stopReason);
  const reason = reasonItem?.stopReason ? t(`queue.${reasonItem.stopReason}`) : sec === "paused" ? t("queue.waitingResume") : undefined;
  return (
    <span className={cn("inline-flex items-center gap-1.5 h-6 px-2.5 rounded-full text-[12px] font-medium whitespace-nowrap", SEC_STYLE[sec].pill)} title={reason}>
      <StatusDot tone={SEC_STYLE[sec].dot} size="xs" />
      {t(`queue.bundle.section.${sec}`)}
      {reason && <span className="sr-only">{reason}</span>}
    </span>
  );
}

/** 方案 A:一单一行的表格行。点行看按机房的详情;开关 / 编辑 / 菜单都作用于整单 */
function OrderTableRow({ bundle, name, stock, rowProps, onGroupAction, busy, onOpen, onEdit, onDelete }: OrderRowProps) {
  const { t } = useTranslation();
  const items = bundle.items;
  const rep = representative(items);
  const sum = summarize(items);
  const sec = sectionOf(items);
  const done = items.filter((i) => i.status === "completed").length;
  const check = groupCheckState(items, rowProps);
  const pausable = items.filter((i) => (i.status === "running" || i.status === "pending") && i.stopReason !== "groupComplete");
  const resumable = items.filter((i) => (i.status === "paused" || i.status === "failed") && i.stopReason !== "groupComplete");
  const unfinished = items.some((i) => i.status !== "completed");
  const stop = (e: React.SyntheticEvent) => e.stopPropagation();
  const label = name || rep.planCode;
  const dcPills = <DcPills dcs={sum.dcs} stock={sec === "completed" ? undefined : stock} />;
  return (
    <tr onClick={onOpen} className={cn("group cursor-pointer transition-colors hover:bg-primary/[.035]", check === true && "bg-primary/[.06] hover:bg-primary/[.07]")}>
      <td className="relative pl-4 pr-2 py-3 w-10" onClick={stop}>
        <span aria-hidden className={cn("absolute left-0 top-2.5 bottom-2.5 w-[3px] rounded-r-full", SEC_STYLE[sec].bar, sec === "completed" && "opacity-50")} />
        <span className="flex p-1.5 -m-1.5">
          <Checkbox checked={check} onCheckedChange={() => toggleGroup(items, rowProps)} aria-label={t("queue.bundle.selectAria", { n: items.length })} />
        </span>
      </td>
      <td className="px-2 py-3 min-w-0">
        <div className="flex items-start gap-3 min-w-0">
        <span aria-hidden className={cn("hidden sm:grid place-items-center w-9 h-9 rounded-xl shrink-0", SEC_STYLE[sec].tile)}>
          <ServerIcon className="w-[18px] h-[18px]" strokeWidth={1.75} />
        </span>
        <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5 min-w-0">
          <span className="font-semibold text-[13.5px] truncate" title={name ? `${name} · ${rep.planCode}` : rep.planCode}>{label}</span>
          {name && <span className="hidden 2xl:inline font-mono text-[11.5px] text-muted-foreground shrink-0">{rep.planCode}</span>}
          {rep.autoPay && <Chip tone="warning">{t("queue.autoPayChip")}</Chip>}
          {rep.preorder && <Chip tone="info">{t("queue.preorderBadge")}</Chip>}
        </div>
        <div className="mt-0.5 text-[12px] text-muted-foreground truncate" title={rep.options?.join("\n")}>
          <OrderDurationLabel value={rep.duration} />
          {" · "}
          {rep.options && rep.options.length > 0 ? describeOptionCodes(rep.options) : t("history.defaultOptions")}
        </div>
        <div className="sm:hidden mt-1.5 flex flex-wrap items-center gap-1.5">
          {dcPills}
          <span className="text-[12px] text-muted-foreground tabular-nums">{done}/{items.length}</span>
        </div>
        <div className="sm:hidden mt-1.5"><OrderStatusCell items={items} sec={sec} /></div>
        </div>
        </div>
      </td>
      <td className="hidden sm:table-cell px-2 py-3">
        <div className="flex flex-wrap items-center gap-1.5">
          {dcPills}
          <span className="2xl:hidden"><AccountChip accountId={rep.accountId} /></span>
        </div>
        {sum.limit ? <div className="mt-1 text-[11.5px] text-muted-foreground">{t("queue.groupLimit", { n: sum.limit })}</div> : null}
      </td>
      <td className="hidden 2xl:table-cell px-2 py-3"><AccountChip accountId={rep.accountId} /></td>
      <td className="hidden xl:table-cell px-2 py-3 text-[12.5px] tabular-nums whitespace-nowrap">
        <div>
          <span className={done > 0 ? "text-success font-semibold" : "text-foreground/70 font-medium"}>{done}</span>
          <span className="text-muted-foreground"> / {items.length}</span>
        </div>
        <div className="mt-1.5 h-1 w-12 rounded-full bg-muted overflow-hidden">
          <div className="h-full rounded-full bg-success" style={{ width: `${items.length ? Math.round((done / items.length) * 100) : 0}%` }} />
        </div>
      </td>
      <td className="hidden sm:table-cell px-2 py-3"><OrderStatusCell items={items} sec={sec} /></td>
      <td className="px-2 py-3" onClick={stop}>
        {sec === "failed" && onGroupAction && resumable.length > 0 ? (
          <Button size="sm" variant="outline" className="h-7 px-2 text-[12px] text-primary border-primary/40" disabled={busy} onClick={() => onGroupAction(resumable, "resume", rep.planCode)}>
            {t("queue.bundle.restart")}
          </Button>
        ) : onGroupAction && (pausable.length > 0 || resumable.length > 0) ? (
          <Switch
            checked={pausable.length > 0}
            disabled={busy}
            onCheckedChange={(on) => (on ? onGroupAction(resumable, "resume", rep.planCode) : onGroupAction(pausable, "pause", rep.planCode))}
            className={SWITCH_CLS}
            aria-label={t(pausable.length > 0 ? "queue.bundle.switchPauseAria" : "queue.bundle.switchResumeAria", { plan: label })}
          />
        ) : null}
      </td>
      <td className="pl-1 pr-3 py-3 text-right whitespace-nowrap" onClick={stop}>
        {onEdit && unfinished && (
          <Button variant="ghost" size="icon" className="h-8 w-8 hidden sm:inline-flex text-muted-foreground hover:text-primary hover:bg-primary/10" onClick={() => onEdit(items)} aria-label={t("queue.bundle.editOrder")} title={t("queue.bundle.editOrder")}>
            <Pencil className="w-4 h-4" />
          </Button>
        )}
        <BundleMenu items={items} onEdit={onEdit} onDelete={onDelete} />
      </td>
    </tr>
  );
}

/**
 * 详情里的自动付款开关:一键切换这一单所有未下单的任务。
 * 打开前确认一次(会自动扣款);关闭直接生效。已发出的结账不受影响。
 */
function BundleAutoPay({ items, accountId }: { items: QueueItem[]; accountId?: string }) {
  const { t } = useTranslation();
  const setAutoPay = useSetQueueAutoPay();
  const [confirm, confirmDialog] = useConfirm();
  const open = items.filter((i) => i.status !== "completed");
  if (open.length === 0) return null;
  const on = open.every((i) => !!i.autoPay);
  const mixed = !on && open.some((i) => !!i.autoPay);
  const id = `bundle-autopay-${items[0].id}`;
  const toggle = async (next: boolean) => {
    if (next) {
      const ok = await confirm({
        title: t("queue.autoPayToggle.confirmTitle"),
        description: t("queue.autoPayToggle.confirmDesc", { n: open.length }),
        confirmText: t("queue.autoPayToggle.confirmOk"),
      });
      if (!ok) return;
    }
    setAutoPay.mutate({ ids: open.map((i) => i.id), autoPay: next });
  };
  return (
    <div className="pt-1 space-y-1">
      <div className="flex items-center gap-2.5">
        <Switch id={id} checked={on} disabled={setAutoPay.isPending} onCheckedChange={(v) => void toggle(v)} />
        <label htmlFor={id} className="text-[13px] font-medium cursor-pointer">
          {t("queue.autoPayToggle.label")}
        </label>
        {mixed && <span className="text-[11.5px] text-muted-foreground">{t("queue.autoPayToggle.mixed")}</span>}
        {setAutoPay.isPending && <Loader2 className="w-3.5 h-3.5 animate-spin text-muted-foreground" />}
      </div>
      {accountId && <AutoPayStatus accountId={accountId} enabled={on} />}
      {confirmDialog}
    </div>
  );
}

/** 秒数 → 「12 秒 / 5 分钟 / 3 小时 / 2 天」 */
function agoText(sec: number, t: (k: string, o?: Record<string, unknown>) => string): string {
  if (sec < 60) return t("queue.bundle.agoS", { n: sec });
  if (sec < 3600) return t("queue.bundle.agoM", { n: Math.floor(sec / 60) });
  if (sec < 86400) return t("queue.bundle.agoH", { n: Math.floor(sec / 3600) });
  return t("queue.bundle.agoD", { n: Math.floor(sec / 86400) });
}

/**
 * 按机房看这一单:每个机房一行 —— 状态、已下单/共几台、最近检查;该机房暂停/恢复;
 * 按机房模式下可直接 −/+ 台数(合计模式的台数在「编辑整单」里改)。
 */
function DcBreakdown({ items, onGroupAction, busy }: { items: QueueItem[]; onGroupAction?: GroupAction; busy?: boolean }) {
  const { t } = useTranslation();
  const update = useUpdateQueueBundle();
  const now = useNow(true);
  const rep = representative(items);
  const shared = items.some((i) => (i.groupLimit || 0) > 0);
  const rows = useMemo(() => {
    const m = new Map<string, QueueItem[]>();
    for (const i of items) {
      const k = i.datacenter.toLowerCase();
      m.set(k, [...(m.get(k) || []), i]);
    }
    return [...m.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }, [items]);
  const openCount = (list: QueueItem[]) => list.filter((i) => i.status !== "completed").length;
  const totalOpen = items.filter((i) => i.status !== "completed").length;
  const done = items.length - totalOpen;
  const setQty = (dc: string, n: number) => {
    const quantities: Record<string, number> = {};
    for (const [k, list] of rows) quantities[k] = openCount(list);
    quantities[dc] = n;
    update.mutate({
      ids: items.map((i) => i.id),
      options: rep.options || [],
      duration: rep.duration,
      retryInterval: rep.retryInterval,
      autoPay: !!rep.autoPay,
      coupon: rep.coupon || "",
      quantities,
    });
  };
  return (
    <div className="rounded-xl border border-border divide-y divide-border">
      <div className="px-3.5 py-2 text-[12px] text-muted-foreground flex items-center justify-between">
        <span>{t("queue.bundle.byDc")}</span>
        {shared && <span>{t("queue.groupLimit", { n: rep.groupLimit })}</span>}
      </div>
      {rows.map(([dc, list]) => {
        const running = list.filter((i) => i.status === "running" || i.status === "pending");
        const paused = list.filter((i) => (i.status === "paused" || i.status === "failed") && i.stopReason !== "groupComplete");
        const ordered = list.length - openCount(list);
        const last = Math.max(0, ...list.map((i) => i.lastCheckTime || 0));
        const ago = last > 0 ? Math.max(0, Math.round(now / 1000 - last)) : -1;
        const n = openCount(list);
        return (
          <div key={dc} className="px-3.5 py-2.5 flex flex-wrap items-center gap-x-4 gap-y-2">
            <div className="flex items-center gap-2 min-w-[92px]">
              <StatusDot tone={running.length > 0 ? "success" : n === 0 ? "info" : "warning"} pulse={running.length > 0} size="xs" />
              <span className="font-mono text-[13px] font-semibold">{dc.toUpperCase()}</span>
            </div>
            <div className="flex-1 min-w-[140px] text-[12px] text-muted-foreground space-x-2">
              <span>{t("queue.bundle.dcOrdered", { done: ordered, n: list.length })}</span>
              {running.length > 0 && <span className="text-success">{t("queue.bundle.dcRunning", { n: running.length })}</span>}
              {paused.length > 0 && <span className="text-warning">{t("queue.bundle.dcPaused", { n: paused.length })}</span>}
              {ago >= 0 && <span>{t("queue.bundle.dcChecked", { ago: agoText(ago, t) })}</span>}
            </div>
            <div className="flex items-center gap-2">
              {!shared && n > 0 && (
                <div className="inline-flex items-center rounded-lg border border-border" role="group" aria-label={t("queue.bundle.dcQty", { dc: dc.toUpperCase() })}>
                  <button
                    type="button"
                    className="h-7 w-7 grid place-items-center text-muted-foreground hover:text-foreground disabled:opacity-40"
                    disabled={update.isPending || (n <= 1 && totalOpen <= 1 && done === 0)}
                    onClick={() => setQty(dc, n - 1)}
                    aria-label={t("queue.bundleEdit.less", { label: dc.toUpperCase() })}
                  >
                    <Minus className="w-3.5 h-3.5" />
                  </button>
                  <span className="w-7 text-center text-[13px] font-semibold tabular-nums">{n}</span>
                  <button
                    type="button"
                    className="h-7 w-7 grid place-items-center text-muted-foreground hover:text-foreground disabled:opacity-40"
                    disabled={update.isPending || n >= 60}
                    onClick={() => setQty(dc, n + 1)}
                    aria-label={t("queue.bundleEdit.more", { label: dc.toUpperCase() })}
                  >
                    <Plus className="w-3.5 h-3.5" />
                  </button>
                </div>
              )}
              {onGroupAction && running.length > 0 && (
                <Button size="sm" variant="outline" className="h-7" disabled={busy} onClick={() => onGroupAction(running, "pause", rep.planCode)}>
                  <PauseCircle className="w-3.5 h-3.5" />{t("queue.pause")}
                </Button>
              )}
              {onGroupAction && running.length === 0 && paused.length > 0 && (
                <Button size="sm" variant="outline" className="h-7 text-primary border-primary/40" disabled={busy} onClick={() => onGroupAction(paused, "resume", rep.planCode)}>
                  <PlayCircle className="w-3.5 h-3.5" />{t("queue.resume")}
                </Button>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

/** 单详情:这一单的设置和每个机房 / 每台的任务明细(可单独暂停、编辑、删除) */
function OrderDetailDialog({
  bundle,
  name,
  rowProps,
  onGroupAction,
  onBundleDelete,
  onBundleEdit,
  busy,
  onClose,
}: {
  bundle: Bundle | null;
  name?: string;
  rowProps: (item: QueueItem) => QueueRowProps;
  onGroupAction?: GroupAction;
  onBundleDelete?: (items: QueueItem[]) => void;
  onBundleEdit?: (items: QueueItem[]) => void;
  busy?: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  if (!bundle) return null;
  const items = bundle.items;
  const rep = representative(items);
  const done = items.filter((i) => i.status === "completed").length;
  const pausable = items.filter((i) => (i.status === "running" || i.status === "pending") && i.stopReason !== "groupComplete");
  const resumable = items.filter((i) => i.status === "paused" && i.stopReason !== "groupComplete");
  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="w-[95vw] sm:max-w-3xl max-h-[88vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex flex-wrap items-baseline gap-2 pr-6">
            {name || <span className="font-mono">{rep.planCode}</span>}
            {name && <span className="font-mono text-[13px] font-normal text-muted-foreground">{rep.planCode}</span>}
          </DialogTitle>
          <DialogDescription>{rep.options && rep.options.length > 0 ? describeOptionCodes(rep.options) : t("history.defaultOptions")}</DialogDescription>
        </DialogHeader>
        <div className="grid grid-cols-1 sm:grid-cols-[1fr_auto] gap-3 items-center rounded-xl bg-muted/40 px-4 py-3">
          <div className="space-y-1.5 min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <AccountChip accountId={rep.accountId} />
              <SettingsCell item={rep} />
            </div>
            <OrderProgress done={done} total={items.length} />
            <BundleAutoPay items={items} accountId={rep.accountId} />
          </div>
          <div className="flex flex-wrap gap-2 sm:justify-end">
            {onBundleEdit && items.some((i) => i.status !== "completed") && (
              <Button
                size="sm"
                onClick={() => {
                  onClose();
                  onBundleEdit(items);
                }}
              >
                <Pencil className="w-4 h-4" />
                {t("queue.bundle.editOrder")}
              </Button>
            )}
            {pausable.length > 0 && onGroupAction && (
              <Button size="sm" variant="outline" disabled={busy} onClick={() => onGroupAction(pausable, "pause", rep.planCode)}>
                <PauseCircle className="w-4 h-4" />
                {t("queue.planGroup.pauseAll")}
              </Button>
            )}
            {resumable.length > 0 && onGroupAction && (
              <Button size="sm" variant="outline" className="text-primary border-primary/40" disabled={busy} onClick={() => onGroupAction(resumable, "resume", rep.planCode)}>
                <PlayCircle className="w-4 h-4" />
                {t("queue.planGroup.resumeAll")}
              </Button>
            )}
            {onBundleDelete && (
              <Button
                size="sm"
                variant="outline"
                className="text-destructive border-destructive/40"
                onClick={() => {
                  onClose();
                  onBundleDelete(items);
                }}
              >
                <Trash2 className="w-4 h-4" />
                {t("queue.bundle.deleteAll", { n: items.length })}
              </Button>
            )}
          </div>
        </div>
        <div className="-mx-6 px-6 overflow-y-auto flex-1 space-y-3">
          <DcBreakdown items={items} onGroupAction={onGroupAction} busy={busy} />
          <details className="group rounded-xl border border-border">
            <summary className="flex items-center justify-between gap-2 px-3.5 py-2.5 text-[13px] font-medium list-none cursor-pointer [&::-webkit-details-marker]:hidden">
              <span>{t("queue.bundle.eachTask", { n: items.length })}</span>
              <ChevronRight className="w-4 h-4 text-muted-foreground transition-transform group-open:rotate-90" aria-hidden />
            </summary>
            <div className="cq px-1 pb-1 divide-y divide-border border-t border-border">
              <TaskHead />
              {items.map((item) => (
                <TaskRow key={item.id} compact {...rowProps(item)} />
              ))}
            </div>
          </details>
        </div>
      </DialogContent>
    </Dialog>
  );
}



/** 每单各机房的实时库存:按账户所在大区查 OVH 公开库存,只用会出现在 FQN 里的选项匹配(带宽等不参与) */
function useBundleStock(bundles: Bundle[]) {
  const accounts = useAccounts();
  const def = useDefaultAccount();
  const endpointOf = (id: string) => (id ? accounts.data?.find((a) => a.id === id)?.endpoint : def?.endpoint) || "";
  const endpoints = [...new Set(bundles.map((b) => endpointOf(b.items[0].accountId || "")))];
  const data = useAvailabilityByEndpoints(endpoints, 60_000);
  return (b: Bundle): Record<string, boolean> | undefined => {
    const ep = endpointOf(b.items[0].accountId || "");
    const all = ep ? data.get(endpointRegion(ep)) : undefined;
    if (!all) return undefined;
    const variants = variantsForPlan(all, b.items[0].planCode);
    if (!variants || variants.length === 0) return undefined;
    const opts = (b.items[0].options || []).filter((o) => variants.some((v) => fqnMatchesOption(v.fqn, o)));
    const st = variantDcStatus(variants, opts);
    const out: Record<string, boolean> = {};
    for (const it of b.items) {
      const dc = (it.datacenter || "").toLowerCase();
      const code = apiDatacenterCode(dc);
      const v = st[code] ?? st[dc];
      if (v !== undefined) out[dc] = isOrderable(v);
    }
    return out;
  };
}

/**
 * 抢购列表:相同设置的任务合并成一单,一单一行(数量、机房、进度一眼看完);
 * 勾选一单 = 选中它的全部任务;点开看每台的明细。
 */
export function QueueTable({
  items,
  rowProps,
  allSelected,
  onSelectAll,
  onGroupAction,
  busy,
  onBundleDelete,
  onBundleEdit,
}: {
  items: QueueItem[];
  rowProps: (item: QueueItem) => QueueRowProps;
  allSelected: boolean;
  onSelectAll: () => void;
  onGroupAction?: GroupAction;
  busy?: boolean;
  /** 兼容旧调用,已不再区分分组 / 平铺 */
  view?: "grouped" | "flat";
  onBundleDelete?: (items: QueueItem[]) => void;
  onBundleEdit?: (items: QueueItem[]) => void;
}) {
  const { t } = useTranslation();
  const bundles = useMemo(() => bundleItems(items), [items]);
  const catalog = useServers(true);
  const names = useMemo(() => {
    const m = new Map<string, string>();
    for (const it of items) if (it.planName) m.set(it.planCode, it.planName);
    for (const p of catalog.data || []) {
      if (m.has(p.planCode)) continue;
      const n = (p.name || "").split("|")[0].trim();
      if (n && n !== p.planCode) m.set(p.planCode, n);
    }
    return m;
  }, [catalog.data, items]);
  // 按任务 ID 记住打开的是哪一单:切换自动付款会改变合并键,弹窗不能因此关掉
  const [openId, setOpenId] = useState<string | null>(null);
  const openBundle = openId ? bundles.find((b) => b.items.some((i) => i.id === openId)) || null : null;
  const stockOf = useBundleStock(bundles);
  // 排序:在抢 → 暂停 → 失败 → 已下单,同段内保持原顺序(新建的在前)
  const sorted = useMemo(() => {
    const rank: Record<Section, number> = { running: 0, paused: 1, failed: 2, completed: 3 };
    return [...bundles].sort((a, b) => rank[sectionOf(a.items)] - rank[sectionOf(b.items)]);
  }, [bundles]);
  const pageCount = Math.max(1, Math.ceil(sorted.length / PAGE_SIZE));
  const [page, setPage] = useState(0);
  const current = Math.min(page, pageCount - 1);
  const shown = sorted.slice(current * PAGE_SIZE, current * PAGE_SIZE + PAGE_SIZE);

  return (
    <div>
      <table className="w-full table-fixed border-collapse text-left">
        <thead>
          <tr className="border-b border-border bg-primary/[.035] text-[12px] text-muted-foreground [&>th]:h-10 [&>th]:font-medium [&>th]:tracking-wide">
            <th className="w-10 pl-4 pr-2 py-2 font-normal">
              <Checkbox checked={allSelected} onCheckedChange={onSelectAll} aria-label={allSelected ? t("queue.unselectAll") : t("queue.selectAll")} />
            </th>
            <th className="px-2 py-2 font-medium">{t("queue.table.plan")}</th>
            <th className="hidden sm:table-cell w-[30%] px-2 py-2 font-medium">
              <span className="inline-flex items-center gap-2">
                {t("queue.table.dcs")}
                <span className="inline-flex items-center gap-1 font-normal text-[11.5px] text-success"><span className="w-1.5 h-1.5 rounded-full bg-success" />{t("queue.bundle.inStock")}</span>
              </span>
            </th>
            <th className="hidden 2xl:table-cell w-[112px] px-2 py-2 font-medium">{t("queue.table.account")}</th>
            <th className="hidden xl:table-cell w-[68px] px-2 py-2 font-medium">{t("queue.table.progress")}</th>
            <th className="hidden sm:table-cell w-[124px] px-2 py-2 font-medium">{t("queue.table.status")}</th>
            <th className="w-[56px] sm:w-[60px] px-2 py-2 font-medium">{t("queue.table.switch")}</th>
            <th className="w-[48px] sm:w-[80px] pl-1 pr-3 py-2"><span className="sr-only">{t("queue.col.actions")}</span></th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border/70">
          {shown.map((b) => (
            <OrderTableRow key={b.key} bundle={b} name={names.get(b.items[0].planCode)} stock={stockOf(b)} rowProps={rowProps} onGroupAction={onGroupAction} busy={busy} onOpen={() => setOpenId(b.items[0].id)} onEdit={onBundleEdit} onDelete={onBundleDelete} />
          ))}
        </tbody>
      </table>
      <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-3 border-t border-border bg-primary/[.02] text-[12.5px] text-muted-foreground">
        <span>
          {t("queue.table.range", {
            from: sorted.length === 0 ? 0 : current * PAGE_SIZE + 1,
            to: Math.min(sorted.length, (current + 1) * PAGE_SIZE),
            orders: sorted.length,
            tasks: items.length,
          })}
        </span>
        {pageCount > 1 && (
          <nav className="flex items-center gap-1" aria-label={t("queue.table.pages")}>
            <Button variant="ghost" size="icon" className="h-8 w-8 rounded-lg" disabled={current === 0} onClick={() => setPage(current - 1)} aria-label={t("queue.table.prev")}>
              <ChevronLeft className="w-4 h-4" />
            </Button>
            {Array.from({ length: pageCount }, (_, n) => n)
              .filter((n) => n === 0 || n === pageCount - 1 || Math.abs(n - current) <= 1)
              .map((n, idx, arr) => (
                <span key={n} className="flex items-center">
                  {idx > 0 && n - arr[idx - 1] > 1 && <span className="px-1">…</span>}
                  <Button
                    variant={n === current ? "default" : "ghost"}
                    size="sm"
                    className={cn("h-8 min-w-8 px-2 rounded-lg tabular-nums", n !== current && "text-foreground/70 hover:text-primary")}
                    onClick={() => setPage(n)}
                    aria-current={n === current ? "page" : undefined}
                  >
                    {n + 1}
                  </Button>
                </span>
              ))}
            <Button variant="ghost" size="icon" className="h-8 w-8 rounded-lg" disabled={current >= pageCount - 1} onClick={() => setPage(current + 1)} aria-label={t("queue.table.next")}>
              <ChevronRight className="w-4 h-4" />
            </Button>
          </nav>
        )}
      </div>
      <OrderDetailDialog
        bundle={openBundle}
        name={openBundle ? names.get(openBundle.items[0].planCode) : undefined}
        rowProps={rowProps}
        onGroupAction={onGroupAction}
        onBundleDelete={onBundleDelete}
        onBundleEdit={onBundleEdit}
        busy={busy}
        onClose={() => setOpenId(null)}
      />
    </div>
  );
}

/** 任务行的列标题(宽屏才显示) */
function TaskHead() {
  const { t } = useTranslation();
  return (
    <div className="task-row task-head px-4 py-2 text-[11px] font-medium text-muted-foreground" aria-hidden>
      <span className="tr-check" />
      <span className="tr-icon" />
      <span className="tr-main">{t("queue.col.plan")}</span>
      <span className="tr-next">{t("queue.row.nextLabel")}</span>
      <span className="tr-last">{t("queue.row.lastLabel")}</span>
      <span className="tr-act text-right">{t("queue.col.actions")}</span>
    </div>
  );
}
