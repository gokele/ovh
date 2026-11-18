import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChevronRight, ChevronsDownUp, ChevronsUpDown, Pencil, PauseCircle, PlayCircle, RotateCw, X } from "lucide-react";
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
import { useMemo, useState, type ReactNode } from "react";
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

/** 组级一键暂停 / 恢复:只对能切换的任务生效,数量写在按钮上 */
function GroupActions({ group, onGroupAction, busy }: { group: PlanGroup; onGroupAction?: GroupAction; busy?: boolean }) {
  const { t } = useTranslation();
  if (!onGroupAction) return null;
  const pausable = group.items.filter((i) => (i.status === "running" || i.status === "pending") && i.stopReason !== "groupComplete");
  const resumable = group.items.filter((i) => i.status === "paused" && i.stopReason !== "groupComplete");
  if (pausable.length + resumable.length === 0) return null;
  return (
    <div className="flex items-center gap-1.5" onClick={(e) => e.stopPropagation()}>
      {pausable.length > 0 && (
        <Button variant="outline" size="sm" disabled={busy} className="h-8 px-2.5 gap-1 text-[12.5px]" onClick={() => onGroupAction(pausable, "pause", group.planCode)}>
          <PauseCircle className="w-3.5 h-3.5" />
          {t("queue.planGroup.pauseAll")}
          <span className="tabular-nums opacity-60">{pausable.length}</span>
        </Button>
      )}
      {resumable.length > 0 && (
        <Button size="sm" disabled={busy} className="h-8 px-2.5 gap-1 text-[12.5px]" onClick={() => onGroupAction(resumable, "resume", group.planCode)}>
          <PlayCircle className="w-3.5 h-3.5" />
          {t("queue.planGroup.resumeAll")}
          <span className="tabular-nums opacity-80">{resumable.length}</span>
        </Button>
      )}
    </div>
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

/**
 * 展开后的单个任务:套餐已在组头,这里只放 机房 / 账户 / 配置与设置 / 状态 / 进度 / 操作。
 * 布局按容器宽度切换(.task-row,见 calm.css),任何宽度都不横向滚动。
 */
function TaskRow(props: QueueRowProps) {
  const { t } = useTranslation();
  const { item, selected, onSelect } = props;
  return (
    <div className={cn("task-row px-4 py-3 transition-colors hover:bg-card/80", selected && "bg-primary/[.05]")}>
      <div className="tr-check pt-0.5">
        <Checkbox checked={selected} onCheckedChange={onSelect} aria-label={t("queue.selectTaskAria", { plan: item.planCode, dc: item.datacenter })} />
      </div>
      <div className="tr-dc">
        <span className="inline-flex items-center h-6 px-2 rounded-md bg-card ring-1 ring-border font-mono text-[12px] font-semibold uppercase">{item.datacenter}</span>
      </div>
      <div className="tr-acc min-w-0"><AccountChip accountId={item.accountId} /></div>
      <div className="tr-cfg min-w-0">
        <div className="text-[12.5px] text-foreground/90 truncate" title={item.options?.join("\n")}>
          {item.options && item.options.length > 0 ? describeOptionCodes(item.options) : t("history.defaultOptions")}
        </div>
        <SettingsCell item={item} />
      </div>
      <div className="tr-status min-w-0"><StatusCell item={item} /></div>
      <div className="tr-prog min-w-0"><ProgressCell item={item} timing={props.timing} intervalEditor={props.intervalEditor} /></div>
      <div className="tr-act"><RowActions {...props} /></div>
    </div>
  );
}

/**
 * 抢购任务列表:同一套餐的多个任务(不同机房 / 配置 / 账户)合并成一组。
 * 收起时一行看完:商品名、型号、任务数、各状态数量、抢购地区与数量,并可整组暂停 / 恢复;
 * 点击展开看每个任务。只有一个任务的套餐默认展开。
 */
export function QueueTable({
  items,
  rowProps,
  allSelected,
  onSelectAll,
  onGroupAction,
  busy,
}: {
  items: QueueItem[];
  rowProps: (item: QueueItem) => QueueRowProps;
  allSelected: boolean;
  onSelectAll: () => void;
  /** 组头的一键暂停 / 恢复 */
  onGroupAction?: GroupAction;
  /** 批量操作进行中:禁用组按钮,避免重复提交 */
  busy?: boolean;
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
  const isOpen = (g: PlanGroup) => openMap[g.planCode] ?? g.items.length === 1;
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
        {groups.length > 0 && (
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
                <div className="cq mx-3 mb-3 rounded-xl bg-muted/45 ring-1 ring-border/60 overflow-hidden animate-in fade-in slide-in-from-top-1 duration-200">
                  <div className="task-row task-head px-4 py-2 text-[11px] font-medium text-muted-foreground">
                    <span className="tr-check" />
                    <span className="tr-dc">{t("queue.col.dc")}</span>
                    <span className="tr-acc">{t("queue.col.account")}</span>
                    <span className="tr-cfg">{t("queue.col.settings")}</span>
                    <span className="tr-status">{t("queue.col.status")}</span>
                    <span className="tr-prog">{t("queue.col.progress")}</span>
                    <span className="tr-act text-right">{t("queue.col.actions")}</span>
                  </div>
                  <div className="divide-y divide-border/70">
                    {g.items.map((item) => <TaskRow key={item.id} {...rowProps(item)} />)}
                  </div>
                </div>
              )}
            </section>
          );
        })}
      </div>
    </div>
  );
}
