import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { CheckCircle2, ChevronRight, ChevronsDownUp, ChevronsUpDown, Circle, Clock3, MoreHorizontal, Pencil, PauseCircle, PlayCircle, RotateCw, Trash2, X, XCircle } from "lucide-react";
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
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Chip } from "@/components/common/Chip";
import { Switch } from "@/components/ui/switch";
import { useServers } from "@/hooks/use-servers";

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

/** 手机端的一行:型号/机房/状态一行,设置与进度各一行,操作在右上。 */
export function QueueMobileRow(props: QueueRowProps) {
  const { t } = useTranslation();
  const { item, selected, onSelect } = props;
  return (
    <div className={cn("px-3 py-3 border-b border-border last:border-0", selected && "bg-muted/50", props.nested && "pl-5 bg-primary/[.02] shadow-[inset_3px_0_0_hsl(var(--primary)/.5)] animate-in fade-in slide-in-from-top-1 duration-200")}>
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

/** 同一套餐(planCode)的任务归为一组;保持各组首次出现的顺序。 */
interface PlanGroup {
  planCode: string;
  items: QueueItem[];
}

function groupByPlan(items: QueueItem[]): PlanGroup[] {
  const map = new Map<string, PlanGroup>();
  for (const it of items) {
    const g = map.get(it.planCode);
    if (g) g.items.push(it);
    else map.set(it.planCode, { planCode: it.planCode, items: [it] });
  }
  return [...map.values()];
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

/** 机房小标签:代码 + 任务数 */
function RegionChips({ dcs }: { dcs: [string, number][] }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {dcs.map(([dc, n]) => (
        <span key={dc} className="inline-flex items-center gap-1 h-6 pl-2 pr-1 rounded-md bg-card ring-1 ring-border text-[12px] font-mono font-medium whitespace-nowrap">
          {dc}
          <span className="min-w-[18px] h-[18px] px-1 rounded bg-primary/10 text-primary text-[11px] leading-[18px] text-center tabular-nums">{n}</span>
        </span>
      ))}
    </div>
  );
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

/**
 * 组级总开关:有任务在抢时为开,关掉 = 暂停组内全部在抢的任务;全部暂停时为关,打开 = 恢复全部。
 * 旁边写「在抢 / 可控总数」。没有可切换任务(都已下单 / 失败)时不显示。
 */
function GroupActions({ group, onGroupAction, busy }: { group: PlanGroup; onGroupAction?: GroupAction; busy?: boolean }) {
  const { t } = useTranslation();
  if (!onGroupAction) return null;
  const pausable = group.items.filter((i) => (i.status === "running" || i.status === "pending") && i.stopReason !== "groupComplete");
  const resumable = group.items.filter((i) => i.status === "paused" && i.stopReason !== "groupComplete");
  const total = pausable.length + resumable.length;
  if (total === 0) return null;
  const on = pausable.length > 0;
  return (
    <label
      className="inline-flex items-center gap-2 h-8 pl-2.5 pr-1.5 rounded-lg bg-card ring-1 ring-border cursor-pointer select-none hover:ring-foreground/20"
      onClick={(e) => e.stopPropagation()}
    >
      <span className="text-[12.5px] text-muted-foreground whitespace-nowrap">
        <span className={cn("font-semibold tabular-nums", on ? "text-success" : "text-foreground")}>{pausable.length}</span>
        <span className="tabular-nums">/{total}</span> {t("queue.planGroup.active")}
      </span>
      <Switch
        checked={on}
        disabled={busy}
        onCheckedChange={() => (on ? onGroupAction(pausable, "pause", group.planCode) : onGroupAction(resumable, "resume", group.planCode))}
        className={SWITCH_CLS}
        aria-label={t("queue.planGroup.switchAria", { plan: group.planCode })}
      />
    </label>
  );
}

/** 组头一行:☐ › 商品名 型号代码 N 个任务 ● 状态数量 ……(右)机房×数量 [全部暂停/恢复] */
function PlanGroupHeader({
  group,
  name,
  open,
  onToggle,
  rowProps,
  onGroupAction,
  busy,
}: {
  group: PlanGroup;
  name?: string;
  open: boolean;
  onToggle: () => void;
  rowProps: (item: QueueItem) => QueueRowProps;
  onGroupAction?: GroupAction;
  busy?: boolean;
}) {
  const { t } = useTranslation();
  const sum = summarize(group.items);
  const label = t(open ? "queue.planGroup.collapse" : "queue.planGroup.expand", { plan: group.planCode });
  return (
    <div
      className={cn("group/plan px-4 py-3.5 cursor-pointer transition-colors hover:bg-primary/[.035]", open && "bg-primary/[.025]")}
      onClick={onToggle}
    >
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2.5 min-w-0">
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <span onClick={(e) => e.stopPropagation()} className="flex">
            <Checkbox
              checked={groupCheckState(group.items, rowProps)}
              onCheckedChange={() => toggleGroup(group.items, rowProps)}
              aria-label={t("queue.planGroup.selectAria", { plan: group.planCode })}
            />
          </span>
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onToggle();
            }}
            aria-expanded={open}
            aria-label={label}
            className="flex items-baseline gap-x-2.5 gap-y-1 flex-wrap min-w-0 text-left rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <ChevronRight
              className={cn("self-center w-4 h-4 shrink-0 text-muted-foreground transition-transform duration-200 group-hover/plan:text-foreground", open && "rotate-90 !text-primary")}
              aria-hidden
            />
            {name && <span className="text-[15px] font-semibold tracking-tight text-foreground whitespace-nowrap">{name}</span>}
            <span className={cn("font-mono whitespace-nowrap", name ? "text-[13px] text-muted-foreground" : "text-[14px] font-semibold text-foreground")}>{group.planCode}</span>
            <span className="text-[13px] text-muted-foreground whitespace-nowrap">{t("queue.planGroup.tasks", { n: group.items.length })}</span>
            {sum.statuses.map(([st, n]) => (
              <span key={st} className="inline-flex items-center gap-1.5 text-[13px] text-muted-foreground whitespace-nowrap">
                <StatusDot tone={STATUS_TONE[st as keyof typeof STATUS_TONE] ?? "muted"} pulse={st === "running"} size="xs" className="self-center" />
                <span className="tabular-nums text-foreground/80">{n}</span>
                {t(`queue.status.${st}`)}
              </span>
            ))}
            {sum.limit ? <span className="text-[12px] text-muted-foreground whitespace-nowrap">· {t("queue.groupLimit", { n: sum.limit })}</span> : null}
          </button>
        </div>
        <div className="flex flex-wrap items-center gap-2 pl-[52px] sm:pl-0 sm:ml-auto">
          <RegionChips dcs={sum.dcs} />
          <GroupActions group={group} onGroupAction={onGroupAction} busy={busy} />
        </div>
      </div>
    </div>
  );
}

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
        <div className="text-[11.5px] text-muted-foreground whitespace-nowrap">
          {t("queue.row.every")} {intervalEditor ?? item.retryInterval} {t("queue.row.sec")}
          {item.retryCount > 0 && <span> · {t("queue.row.attemptN", { n: item.retryCount + 1 })}</span>}
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
      <Link to="/history" className="font-medium text-primary underline-offset-4 hover:underline">
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
function RowMenu({ item, onEdit, onDelete, onBuyAgain }: Pick<QueueRowProps, "item" | "onEdit" | "onDelete" | "onBuyAgain">) {
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
        {item.status !== "completed" && (
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
function TaskRow({ showPlan, planName, ...props }: QueueRowProps & { showPlan?: boolean; planName?: string }) {
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
          <span className="min-w-0 line-clamp-2 sm:line-clamp-1 text-[13px] font-medium" title={item.options?.join("\n")}>
            {item.options && item.options.length > 0 ? describeOptionCodes(item.options) : t("history.defaultOptions")}
          </span>
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[12px] text-muted-foreground">
          <AccountChip accountId={item.accountId} />
          <SettingsCell item={item} />
          {item.status === "running" && item.orderPhase && <span className="text-primary">{t(`queue.phase.${item.orderPhase}`)}</span>}
        </div>
      </div>
      <div className="tr-next min-w-0"><NextAttempt item={item} intervalEditor={props.intervalEditor} /></div>
      <div className="tr-last min-w-0"><LastRound item={item} timing={props.timing} /></div>
      <div className="tr-act flex items-center justify-end gap-1">
        <PrimaryAction item={item} onToggle={props.onToggle} onBuyAgain={props.onBuyAgain} />
        <RowMenu item={item} onEdit={props.onEdit} onDelete={props.onDelete} onBuyAgain={props.onBuyAgain} />
      </div>
    </div>
  );
}

/**
 * 抢购任务列表:同一套餐的多个任务(不同机房 / 配置 / 账户)合并成一组。
 * 收起时一行看完:商品名、型号、任务数、各状态数量、抢购地区与数量,并可整组暂停 / 恢复;
 * 默认全部展开(分组像分区标题),需要时可折叠。
 */
export function QueueTable({
  items,
  rowProps,
  allSelected,
  onSelectAll,
  onGroupAction,
  busy,
  view = "grouped",
}: {
  items: QueueItem[];
  rowProps: (item: QueueItem) => QueueRowProps;
  allSelected: boolean;
  onSelectAll: () => void;
  /** 组头的一键暂停 / 恢复 */
  onGroupAction?: GroupAction;
  /** 批量操作进行中:禁用组按钮,避免重复提交 */
  busy?: boolean;
  /** grouped:按套餐分组;flat:平铺所有任务 */
  view?: "grouped" | "flat";
}) {
  const { t } = useTranslation();
  const groups = useMemo(() => groupByPlan(items), [items]);
  // 商品名(KS-LE-6 / ADVANCE-1)取自服务器目录;目录没加载到或不认识的型号只显示型号代码
  const catalog = useServers(true);
  const names = useMemo(() => {
    const m = new Map<string, string>();
    for (const p of catalog.data || []) {
      const n = (p.name || "").split("|")[0].trim();
      if (n && n !== p.planCode) m.set(p.planCode, n);
    }
    return m;
  }, [catalog.data]);

  const [openMap, setOpenMap] = useState<Record<string, boolean>>({});
  const isOpen = (g: PlanGroup) => openMap[g.planCode] ?? true;
  const toggle = (g: PlanGroup) => setOpenMap((m) => ({ ...m, [g.planCode]: !isOpen(g) }));
  const allOpen = groups.length > 0 && groups.every(isOpen);
  const setAll = (v: boolean) => setOpenMap(Object.fromEntries(groups.map((g) => [g.planCode, v])));

  return (
    <div>
      <div className="flex items-center gap-3 px-4 py-2.5 border-b border-border bg-muted/30">
        <Checkbox checked={allSelected} onCheckedChange={onSelectAll} aria-label={allSelected ? t("queue.unselectAll") : t("queue.selectAll")} />
        <span className="text-[12.5px] text-muted-foreground">
          {t("queue.planGroup.summary", { plans: groups.length, tasks: items.length })}
        </span>
        {view === "grouped" && groups.length > 0 && (
          <button
            type="button"
            onClick={() => setAll(!allOpen)}
            className="ml-auto inline-flex items-center gap-1 text-[12.5px] text-primary hover:underline underline-offset-2"
          >
            {allOpen ? <ChevronsDownUp className="w-3.5 h-3.5" /> : <ChevronsUpDown className="w-3.5 h-3.5" />}
            {t(allOpen ? "queue.planGroup.collapseAll" : "queue.planGroup.expandAll")}
          </button>
        )}
      </div>
      {view === "flat" ? (
        <div className="cq divide-y divide-border">
          <TaskHead />
          {items.map((item) => <TaskRow key={item.id} {...rowProps(item)} showPlan planName={names.get(item.planCode)} />)}
        </div>
      ) : (
      <div className="divide-y divide-border">
        {groups.map((g) => {
          const open = isOpen(g);
          return (
            <section key={g.planCode} aria-label={names.get(g.planCode) || g.planCode}>
              <PlanGroupHeader
                group={g}
                name={names.get(g.planCode)}
                open={open}
                onToggle={() => toggle(g)}
                rowProps={rowProps}
                onGroupAction={onGroupAction}
                busy={busy}
              />
              {open && (
                <div className="cq mx-3 mb-3 rounded-xl bg-muted/40 ring-1 ring-border/60 overflow-hidden animate-in fade-in slide-in-from-top-1 duration-200">
                  <TaskHead />
                  <div className="divide-y divide-border/70">
                    {g.items.map((item) => <TaskRow key={item.id} {...rowProps(item)} />)}
                  </div>
                </div>
              )}
            </section>
          );
        })}
      </div>
      )}
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
