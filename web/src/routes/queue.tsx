import { OrderPurchaseScope, type PurchaseMode } from "@/components/queue/OrderPurchaseScope";
import { AutoPayOption } from "@/components/common/AutoPayStatus";
import { hasPreorderHardware } from "@/lib/preorder-options";
import { OrderDurationField, OrderDurationLabel } from "@/components/queue/OrderDurationField";
import { preorderVariantCoversAll, preorderOptions, retainPreorderPlan } from "@/lib/preorder-options";
import { useOrderDurations } from "@/hooks/use-order-durations";
import { supportsDuration } from "@/lib/order-duration";
import { QueueStatusFilter, type QueueFilter } from "@/components/queue/QueueStatusFilter";
import { QueueTable, type QueueRowProps } from "@/components/queue/QueueTable";
import { FacetFilter, type FacetOption } from "@/components/queue/FacetFilter";
import { useConfirm } from "@/components/common/ConfirmDialog";
import { cn } from "@/lib/utils";
import { OrderPrewarm, StandbyCarts } from "@/components/queue/OrderPrewarm";
import { useOrderPrewarm } from "@/hooks/use-order-prewarm";
import { createFileRoute, Link } from "@tanstack/react-router";
import {
  ClipboardList,
  RefreshCw,
  Trash2,
  PauseCircle,
  PlayCircle,
  X,
  Clock,
  Plus,
  Pencil,
  Loader2,
  Search,
  RotateCw,
  Layers,
  List,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { useTranslation, Trans } from "react-i18next";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Checkbox } from "@/components/ui/checkbox";
import { Chip } from "@/components/common/Chip";
import { StatusDot } from "@/components/common/StatusDot";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed, LoadFailedBanner, errorMessage } from "@/components/common/LoadFailed";
import { Skeleton } from "@/components/common/Skeleton";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  useQueueList,
  useToggleQueueItem,
  useRemoveQueueItem,
  useUpdateQueueInterval,
  useClearQueue,
  useCreateQueueItem,
  useUpdateQueueItem,
  type QueueItem,
  usePurchaseTimings,
  type PurchaseTiming,
} from "@/hooks/use-queue";
import { useServers } from "@/hooks/use-servers";
import { OVH_DATACENTERS as OVH_DC_LIST, apiDatacenterCode } from "@/lib/datacenters";
import { RETRY_INTERVAL, useSettings } from "@/hooks/use-settings";
import { useActiveAccount } from "@/hooks/use-active-account";
import { useAccounts, findAccountByID } from "@/hooks/use-accounts";
import { TimingChip } from "@/components/common/TimingChip";
import { AccountChip } from "@/components/common/AccountChip";
import { PlanCodeCombobox } from "@/components/common/PlanCodeCombobox";
import { OptionGroupSection } from "@/components/common/OptionGroupSection";
import { describeOptionCodes, groupOptions, type OptionGroupKey } from "@/lib/option-groups";
import { splitList } from "@/lib/split-list";
import { mergeStockPlans } from "@/lib/stock-plans";
import { clampOrderPlan, MAX_ORDER_QUANTITY, MAX_ORDER_FANOUT } from "@/lib/order-limits";
import {
  useAvailability,
  buildVariantIndex,
  hasStockWithOption,
} from "@/hooks/use-availability";
import { fmtDateTime } from "@/i18n/format";

/** 抢购队列：列表 + 暂停/恢复/删除/清空 + 新建抢购任务 */
export const Route = createFileRoute("/queue")({
  component: QueuePage,
  /** 支持 ?create=KS-A-1&options=ram-64g,softraid-2x450nvme 形式：自动打开新建对话框并预填 */
  validateSearch: (search): { create?: string; options?: string } => ({
    create: typeof search.create === "string" ? search.create : undefined,
    options: typeof search.options === "string" ? search.options : undefined,
  }),
});

/** OVH 数据中心列表：复用 lib/datacenters.ts 的共享常量 */
const OVH_DATACENTERS = OVH_DC_LIST;

/** 新建任务时的兜底间隔。真正的默认值来自设置（/api/settings.defaultRetryInterval），
 *  这个常量只在配置还没读到时占位 —— 和后端 types.DefaultTaskRetryInterval 一致 */
const FALLBACK_RETRY_INTERVAL = RETRY_INTERVAL.defaultTask;

/**
 * 队列卡片上那个可点的秒数。
 *
 * 点一下变输入框，回车/失焦提交。改的是这一条任务自己的间隔，
 * 处理器每轮都读任务上的值，所以下一轮就生效，不用重建任务。
 */
function IntervalEditor({ id, value }: { id: string; value: number }) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(String(value));
  const update = useUpdateQueueInterval();

  const commit = () => {
    setEditing(false);
    const n = Number(draft);
    if (!n || n === value) return setDraft(String(value));
    if (n < RETRY_INTERVAL.min || n > RETRY_INTERVAL.max) {
      toast.error(
        t("queue.intervalRangeError", { min: RETRY_INTERVAL.min, max: RETRY_INTERVAL.max })
      );
      return setDraft(String(value));
    }
    update.mutate({ id, retryInterval: n });
  };

  if (!editing) {
    return (
      <button
        type="button"
        onClick={() => {
          setDraft(String(value));
          setEditing(true);
        }}
        className="font-medium text-foreground underline decoration-dotted underline-offset-2 hover:text-primary"
        title={t("queue.intervalEditTitle")}
      >
        {value}
      </button>
    );
  }
  return (
    <input
      autoFocus
      type="text"
      inputMode="numeric"
      value={draft}
      onChange={(e) => {
        const v = e.target.value;
        if (v === "" || /^\d*$/.test(v)) setDraft(v);
      }}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") commit();
        if (e.key === "Escape") {
          setDraft(String(value));
          setEditing(false);
        }
      }}
      // 手机端 16px 防 iOS 聚焦缩放
      className="w-14 px-1 py-0.5 rounded border border-input bg-background text-base sm:text-[11px] text-center"
    />
  );
}

function QueuePage() {
  const { t } = useTranslation();
  // 行卡密度:紧凑/舒适,按页记忆(他的 view.ts 思路)
  const queue = useQueueList();
  // 每条链路上一轮的耗时,用来回答"我到底卡在哪一步"
  const timings = usePurchaseTimings();
  const toggle = useToggleQueueItem();
  const remove = useRemoveQueueItem();
  const clear = useClearQueue();
  const navigate = Route.useNavigate();
  const { create: createPlanCode, options: createOptions } = Route.useSearch();
  const [showClearDialog, setShowClearDialog] = useState(false);
  const [showCreateDialog, setShowCreateDialog] = useState(false);
  const [editingItem, setEditingItem] = useState<QueueItem | null>(null);
  const [prefillPlanCode, setPrefillPlanCode] = useState<string>("");
  const [prefillOptions, setPrefillOptions] = useState<string>("");

  // 从其它页跳到 /queue?create=KS-A-1&options=...，自动打开新建对话框并预填
  useEffect(() => {
    if (createPlanCode) {
      setPrefillPlanCode(createPlanCode);
      setPrefillOptions(createOptions || "");
      setShowCreateDialog(true);
    }
  }, [createPlanCode, createOptions]);

  const items = queue.data || [];
  // 默认看「运行中」:打开队列最关心的是正在抢的任务;已下单/失败记录在其它筛选里
  const [filter, setFilter] = useState<QueueFilter>("running");
  const [search, setSearch] = useState("");
  const keyword = search.trim().toLowerCase();
  // 刚在本页暂停/恢复过的任务留在当前筛选里,直到切换筛选:
  // 否则在「运行中」点暂停,任务立刻消失,想撤回还得去「已暂停」里找。
  const [sticky, setSticky] = useState<Set<string>>(() => new Set());
  const keepVisible = (ids: string[]) => setSticky((prev) => new Set([...prev, ...ids]));
  // 机房 / 账户多选筛选,与状态、搜索叠加
  const [dcFacet, setDcFacet] = useState<Set<string>>(() => new Set());
  const [accFacet, setAccFacet] = useState<Set<string>>(() => new Set());
  const [view, setView] = useState<"grouped" | "flat">(() => {
    try {
      return localStorage.getItem("ovh-queue-view") === "flat" ? "flat" : "grouped";
    } catch {
      return "grouped";
    }
  });
  const changeView = (v: "grouped" | "flat") => {
    setView(v);
    try { localStorage.setItem("ovh-queue-view", v); } catch { /* 隐私模式等读写失败时只在本次生效 */ }
  };
  const visibleItems = items.filter(
    (item) =>
      (filter === "all" || item.status === filter || (filter === "running" && item.status === "pending") || sticky.has(item.id)) &&
      (dcFacet.size === 0 || dcFacet.has(item.datacenter.toUpperCase())) &&
      (accFacet.size === 0 || accFacet.has(item.accountId)) &&
      (!keyword || item.planCode.toLowerCase().includes(keyword) || item.datacenter.toLowerCase().includes(keyword)),
  );

  // —— 批量操作 ——
  // 一条 /buy 或一次网页建单最多扇出 60 个任务(MAX_ORDER_FANOUT),
  // 而在这之前只能一个一个点暂停/删除。抢购结束后清理一批失败任务是高频动作,
  // 「清空」又太狠(会把还在跑的一起删掉)。
  // 筛选项基于当前状态筛选下的任务,数量随状态变化
  const statusScoped = items.filter((item) => filter === "all" || item.status === filter || (filter === "running" && item.status === "pending") || sticky.has(item.id));
  const dcOptions: FacetOption[] = [...statusScoped.reduce((m, i) => m.set(i.datacenter.toUpperCase(), (m.get(i.datacenter.toUpperCase()) || 0) + 1), new Map<string, number>())]
    .sort((a, b) => b[1] - a[1])
    .map(([value, count]) => ({ value, count, label: <span className="font-mono">{value}</span> }));
  const accOptions: FacetOption[] = [...statusScoped.reduce((m, i) => m.set(i.accountId, (m.get(i.accountId) || 0) + 1), new Map<string, number>())]
    .sort((a, b) => b[1] - a[1])
    .map(([value, count]) => ({ value, count, label: <AccountChip accountId={value} /> }));
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [batchRunning, setBatchRunning] = useState(false);
  const [showBatchDelete, setShowBatchDelete] = useState(false);

  // 任务被别处删掉(TG /cancel、监控自动清理)后,选中集合里会留下不存在的 id。
  // 不剪掉的话「已选 3 项」里可能有 2 项早就没了,批量操作会静默少做几件。
  useEffect(() => {
    setSelected((prev) => {
      if (prev.size === 0) return prev;
      const alive = new Set(visibleItems.map((i) => i.id));
      const next = new Set([...prev].filter((id) => alive.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [items]);

  const selectedItems = items.filter((i) => selected.has(i.id));
  const toggleSelect = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  const allSelected = visibleItems.length > 0 && visibleItems.every((item) => selected.has(item.id));

  /** 逐条执行并汇总成一条结果,不要弹 N 个 toast。label 传已翻译的动词 */
  const runBatch = async (
    label: string,
    targets: QueueItem[],
    fn: (it: QueueItem) => Promise<unknown>
  ) => {
    if (targets.length === 0) return;
    setBatchRunning(true);
    let ok = 0;
    let firstError = "";
    for (const it of targets) {
      try {
        await fn(it);
        ok++;
      } catch (e) {
        if (!firstError) firstError = errorMessage(e);
      }
    }
    setBatchRunning(false);
    setSelected(new Set());
    const failed = targets.length - ok;
    if (failed === 0) toast.success(t("queue.batchDone", { label, n: ok }));
    else toast.error(t("queue.batchPartial", { label, ok, failed, error: firstError }));
    queue.refetch();
  };

  /** 单条暂停/恢复:任务留在当前列表,提示条带「撤销」 */
  const toggleOne = async (q: QueueItem) => {
    const action = q.status === "running" || q.status === "pending" ? "pause" : "resume";
    keepVisible([q.id]);
    try {
      await toggle.mutateAsync({ id: q.id, action });
    } catch {
      return;
    }
    toast.success(t(action === "pause" ? "queue.pausedToast" : "queue.resumedToast", { plan: q.planCode, dc: q.datacenter.toUpperCase() }), {
      action: {
        label: t("queue.undo"),
        onClick: () => toggle.mutate({ id: q.id, action: action === "pause" ? "resume" : "pause" }),
      },
    });
  };

  /** 套餐组一键暂停/恢复:逐条执行,汇总成一条提示,同样可撤销 */
  const groupAction = async (targets: QueueItem[], action: "pause" | "resume", planCode: string, whole = false) => {
    if (targets.length === 0) return;
    keepVisible(targets.map((i) => i.id));
    setBatchRunning(true);
    const done: QueueItem[] = [];
    let firstError = "";
    for (const it of targets) {
      try {
        await toggle.mutateAsync({ id: it.id, action });
        done.push(it);
      } catch (e) {
        if (!firstError) firstError = errorMessage(e);
      }
    }
    setBatchRunning(false);
    queue.refetch();
    if (done.length === targets.length) {
      const key = whole
        ? action === "pause" ? "queue.allPausedToast" : "queue.allResumedToast"
        : action === "pause" ? "queue.groupPausedToast" : "queue.groupResumedToast";
      toast.success(t(key, { plan: planCode, n: done.length }), {
        action: {
          label: t("queue.undo"),
          onClick: () => {
            const back = action === "pause" ? "resume" : "pause";
            void runBatch(t(back === "pause" ? "queue.verbPause" : "queue.verbResume"), done, (it) => toggle.mutateAsync({ id: it.id, action: back }));
          },
        },
      });
    } else {
      const failed = targets.length - done.length;
      toast.error(t("queue.batchPartial", { label: t(action === "pause" ? "queue.verbPause" : "queue.verbResume"), ok: done.length, failed, error: firstError }));
    }
  };

  const [againItem, setAgainItem] = useState<QueueItem | null>(null);
  const [confirmAsk, confirmDialog] = useConfirm();
  const pausableAll = items.filter((i) => (i.status === "running" || i.status === "pending") && i.stopReason !== "groupComplete");
  const resumableAll = items.filter((i) => i.status === "paused" && i.stopReason !== "groupComplete");

  /** 单行的回调;编辑运行中的任务会先暂停,保存后需要手动恢复 */
  const rowProps = (q: QueueItem): QueueRowProps => ({
    item: q,
    timing: timings.data?.[q.id] ?? timings.data?.[`${q.planCode}@${q.datacenter}`],
    selected: selected.has(q.id),
    onSelect: () => toggleSelect(q.id),
    onToggle: () => void toggleOne(q),
    onBuyAgain: () => setAgainItem(q),
    onEdit: async () => {
      if (q.status === "running" || q.status === "pending") {
        try { await toggle.mutateAsync({ id: q.id, action: "pause" }); } catch { return; }
      }
      setEditingItem({ ...q, status: "paused" });
      setShowCreateDialog(true);
    },
    onDelete: async () => {
      const ok = await confirmAsk({
        title: t("queue.deleteOneTitle", { plan: q.planCode, dc: q.datacenter.toUpperCase() }),
        description: t("queue.deleteOneDesc"),
        tone: "danger",
        confirmText: t("queue.delete"),
      });
      if (ok) remove.mutate(q.id);
    },
    intervalEditor: <IntervalEditor id={q.id} value={q.retryInterval} />,
  });

  // 队列状态块:加载 / 失败 / 空态 / 表格
  const queueBody = queue.isPending ? (
    <div className="p-4 space-y-2">
      {Array.from({ length: 5 }).map((_, i) => (
        <Skeleton key={i} className="h-10 rounded-md" />
      ))}
    </div>
  ) : queue.isError ? (
    /* 这页是"我有哪些任务在跑"的唯一真相来源。请求挂了还画「暂无任务」,
       用户会以为任务压根没建成,回头再建一遍 —— 同一台机器排两条队,抢到就是两笔真实订单。
       所以失败必须自己占一支,不能跟空态共用。 */
    <LoadFailed icon={ClipboardList} title={t("queue.loadFailedTitle")} error={queue.error} onRetry={() => queue.refetch()} compact />
  ) : visibleItems.length === 0 ? (
    <EmptyState
      illustration={items.length === 0 ? "queue" : "search"}
      title={t(items.length === 0 ? "queue.emptyTitle" : "queue.filterEmpty")}
      description={t(items.length === 0 ? "queue.emptyDesc" : "queue.filterEmptyHint")}
      action={
        items.length === 0 ? (
          <>
            <Button onClick={() => setShowCreateDialog(true)}>{t("queue.emptyCreate")}</Button>
            <Button variant="outline" asChild>
              <Link to="/servers">{t("queue.emptyBrowse")}</Link>
            </Button>
          </>
        ) : (
          <Button variant="outline" onClick={() => { setFilter("all"); setSearch(""); setSticky(new Set()); }}>{t("queue.emptyShowAll")}</Button>
        )
      }
    />
  ) : (
    <QueueTable
      items={visibleItems}
      rowProps={rowProps}
      allSelected={allSelected}
      onSelectAll={() => setSelected(allSelected ? new Set() : new Set(visibleItems.map((i) => i.id)))}
      onGroupAction={(targets, action, plan) => void groupAction(targets, action, plan)}
      busy={batchRunning}
      view={view}
    />
  );

  // 批量操作条:只在有选中项时出现,贴在表格上方
  const batchBar = selected.size > 0 && (
    <div className="flex flex-wrap items-center gap-2 px-4 py-2 border-b border-border bg-muted/40">
      <span className="text-[13px] font-medium">{t("queue.selectedCount", { n: selected.size })}</span>
      <div className="flex flex-wrap gap-1.5 ml-auto">
        <Button
          size="sm"
          variant="outline"
          disabled={batchRunning}
          onClick={() =>
            runBatch(t("queue.verbPause"), selectedItems.filter((i) => i.status === "running" || i.status === "pending"), (it) =>
              toggle.mutateAsync({ id: it.id, action: "pause" })
            )
          }
        >
          <PauseCircle className="w-3.5 h-3.5" />{t("queue.pause")}
        </Button>
        <Button
          size="sm"
          variant="outline"
          disabled={batchRunning}
          onClick={() =>
            runBatch(t("queue.verbResume"), selectedItems.filter((i) => i.status === "paused"), (it) =>
              toggle.mutateAsync({ id: it.id, action: "resume" })
            )
          }
        >
          <PlayCircle className="w-3.5 h-3.5" />{t("queue.resume")}
        </Button>
        {/* 删除是不可逆的,单独走二次确认,不能和暂停放同一个手势层级 */}
        <Button size="sm" variant="destructive" disabled={batchRunning} onClick={() => setShowBatchDelete(true)}>
          {batchRunning ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Trash2 className="w-3.5 h-3.5" />}
          {t("queue.delete")}
        </Button>
        <Button size="sm" variant="ghost" disabled={batchRunning} onClick={() => setSelected(new Set())}>
          {t("queue.deselect")}
        </Button>
      </div>
    </div>
  );

  return (
    <div className="space-y-3 sm:space-y-6">
      <PageHeader
        icon={ClipboardList}
        title={t("queue.title")}
        description={t("queue.description")}
        action={
          // 必须 flex-wrap:PageHeader 外层允许换行,内层不换的话整排按钮保持
          // max-content 宽度,在 390px 上会被挤出左边界(实测 left=-19px)。
          <div className="flex flex-wrap justify-end gap-2">
            <Button onClick={() => setShowCreateDialog(true)}>
              <Plus className="w-4 h-4" />
              {t("queue.createTask")}
            </Button>
            {/* 全局一键暂停 / 恢复:抢购工具最常用的两个按钮,作用于全部任务(不受筛选影响) */}
            {pausableAll.length > 0 && (
              <Button variant="outline" disabled={batchRunning} onClick={() => void groupAction(pausableAll, "pause", "", true)}>
                <PauseCircle className="w-4 h-4" />
                {t("queue.pauseAllTasks")}
                <span className="tabular-nums opacity-60">{pausableAll.length}</span>
              </Button>
            )}
            {resumableAll.length > 0 && (
              <Button variant="outline" className="text-primary border-primary/40" disabled={batchRunning} onClick={() => void groupAction(resumableAll, "resume", "", true)}>
                <PlayCircle className="w-4 h-4" />
                {t("queue.resumeAllTasks")}
                <span className="tabular-nums opacity-70">{resumableAll.length}</span>
              </Button>
            )}
            <Button variant="outline" onClick={() => queue.refetch()} disabled={queue.isFetching}>
              <RefreshCw className={`w-4 h-4 ${queue.isFetching ? "animate-spin" : ""}`} />
              {t("queue.refresh")}
            </Button>
            <Button
              variant="outline"
              onClick={() => setShowClearDialog(true)}
              disabled={items.length === 0}
            >
              <Trash2 className="w-4 h-4" />
              {t("queue.clear")}
            </Button>
          </div>
        }
      />

      {/* 每条链路上一轮的耗时/结论读不到时,行里的 TimingChip 和「上一轮 无货/已下单/出错」
          会整块消失,看起来像"这条任务从来没跑过"。这块只是辅助信息,任务本身照常在跑,
          给一条提示、别让用户误读那片空白就够。 */}
      {timings.isError && items.length > 0 && (
        <LoadFailedBanner
          title={t("queue.timingsFailedTitle")}
          error={timings.error}
          onRetry={() => timings.refetch()}
        />
      )}

      <StandbyCarts />

      {/* 状态卡片兼筛选 */}
      <QueueStatusFilter items={items} value={filter} onChange={(next) => { setFilter(next); setSelected(new Set()); setSticky(new Set()); }} />

      <Card className="overflow-hidden">
        {/* 工具条:搜索 + 机房/账户筛选 …… 实时刷新 + 视图切换 */}
        <div className="flex flex-wrap items-center gap-2 px-4 py-3 border-b border-border">
          <div className="relative w-full sm:w-60">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" aria-hidden />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t("queue.searchPlaceholder")}
              aria-label={t("queue.searchPlaceholder")}
              className="h-8 pl-8 text-[13px]"
            />
          </div>
          <FacetFilter title={t("queue.row.facetDc")} options={dcOptions} value={dcFacet} onChange={setDcFacet} />
          <FacetFilter title={t("queue.row.facetAccount")} options={accOptions} value={accFacet} onChange={setAccFacet} />
          {(dcFacet.size > 0 || accFacet.size > 0 || search) && (
            <Button variant="ghost" size="sm" className="h-8 px-2 text-[12.5px]" onClick={() => { setDcFacet(new Set()); setAccFacet(new Set()); setSearch(""); }}>
              {t("queue.row.reset")}
              <X className="w-3.5 h-3.5" />
            </Button>
          )}
          <div className="ml-auto flex items-center gap-3">
            <span className="hidden md:inline-flex items-center gap-1.5 text-[12px] text-muted-foreground" title={t("queue.row.refreshEvery", { s: 5 })}>
              <span className="relative flex w-2 h-2">
                <span className="absolute inset-0 rounded-full bg-success/60 animate-ping" />
                <span className="relative w-2 h-2 rounded-full bg-success" />
              </span>
              {queue.isFetching ? t("queue.row.refreshing") : t("queue.row.live")}
            </span>
            <div className="inline-flex rounded-lg bg-muted p-[3px]" role="group" aria-label={t("queue.row.viewLabel")}>
              {(["grouped", "flat"] as const).map((v) => (
                <button
                  key={v}
                  type="button"
                  aria-pressed={view === v}
                  onClick={() => changeView(v)}
                  title={t(v === "grouped" ? "queue.row.viewGrouped" : "queue.row.viewFlat")}
                  className={cn(
                    "inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md text-[12.5px] transition-colors",
                    view === v ? "bg-card text-primary shadow-sm font-medium" : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {v === "grouped" ? <Layers className="w-3.5 h-3.5" /> : <List className="w-3.5 h-3.5" />}
                  <span className="hidden sm:inline">{t(v === "grouped" ? "queue.row.viewGrouped" : "queue.row.viewFlat")}</span>
                </button>
              ))}
            </div>
          </div>
        </div>
        {batchBar}
        {queueBody}
        {filter === "completed" && visibleItems.length > 0 && (
          <p className="px-4 py-2.5 border-t border-border text-[11px] text-muted-foreground">{t("queue.retainedHint")}</p>
        )}
      </Card>

      <BuyAgainDialog item={againItem} onClose={() => setAgainItem(null)} />
      {confirmDialog}

      <Dialog open={showBatchDelete} onOpenChange={setShowBatchDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("queue.batchDeleteTitle", { n: selected.size })}</DialogTitle>
            <DialogDescription>
              {t("queue.batchDeleteDesc")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowBatchDelete(false)} disabled={batchRunning}>
              {t("queue.cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={batchRunning}
              onClick={() => {
                setShowBatchDelete(false);
                void runBatch(t("queue.verbDelete"), selectedItems, (it) => remove.mutateAsync(it.id));
              }}
            >
              {t("queue.confirmDelete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={showClearDialog} onOpenChange={setShowClearDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("queue.clearTitle")}</DialogTitle>
            <DialogDescription>{t("queue.clearDesc")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowClearDialog(false)}>
              {t("queue.cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                clear.mutate();
                setShowClearDialog(false);
              }}
            >
              {t("queue.confirmClear")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <CreateQueueDialog
        key={editingItem?.id || "new"}
        editing={editingItem}
        open={showCreateDialog}
        onOpenChange={(v) => {
          setShowCreateDialog(v);
          if (!v) {
            setEditingItem(null);
            setPrefillPlanCode("");
            setPrefillOptions("");
            // 清掉 URL 上的 create / options 参数
            navigate({ search: () => ({}) as any, replace: true });
          }
        }}
        initialPlanCode={editingItem?.planCode || prefillPlanCode}
        initialOptions={editingItem ? (editingItem.options || []).join(",") : prefillOptions}
      />
    </div>
  );
}

/** 创建抢购任务对话框 */
function CreateQueueDialog({
  open,
  onOpenChange,
  initialPlanCode,
  initialOptions,
  editing,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  initialPlanCode?: string;
  initialOptions?: string;
  editing?: QueueItem | null;
}) {
  const { t } = useTranslation();
  const create = useCreateQueueItem();
  const update = useUpdateQueueItem();
  const saving = create.isPending || update.isPending;
  // 默认跟随全局账户,本次任务可以另选账户。
  const [globalAccountId] = useActiveAccount();
  const [selectedAccountId, setSelectedAccountId] = useState("");
  // 库存按"实际下单的那个账户"所在站点查:EU/US/CA 三站的 availabilities 互不相通
  // (实测 US 站 423 个 planCode,只有 134 个与 EU 重合),用别区的库存点红绿灯,
  // 用户会照着不存在的货建任务,然后在抢购时才被 OVH 拒。
  // 留整个 query 而不是只解构 data:账户列表挂了 → accountId 为空 → canSubmit 恒 false
  // → 创建按钮永久灰着。不把 isError 摆出来,用户只看到一个点不动的按钮,不知道是自己没填够
  // 还是我们没读到账户。
  const accountsQ = useAccounts();
  const accounts = accountsQ.data;
  const accountId = selectedAccountId ||
    findAccountByID(accounts, globalAccountId)?.id ||
    accounts?.find((a) => a.isDefault)?.id || accounts?.[0]?.id || "";
  const activeAcc = findAccountByID(accounts, accountId);
  const servers = useServers(true, accountId);
  const orderEndpoint = activeAcc?.endpoint || "";
  const availQ = useAvailability(orderEndpoint || undefined);
  const variantIndex = useMemo(() => buildVariantIndex(availQ.data), [availQ.data]);
  const searchServers = useMemo(
    () => mergeStockPlans(servers.data || [], availQ.data),
    [servers.data, availQ.data]
  );
  useEffect(() => {
    if (open && orderEndpoint) void availQ.refetch();
  }, [open, orderEndpoint, availQ.refetch]);
  const [planCode, setPlanCode] = useState(initialPlanCode || "");
  const [datacenters, setDatacenters] = useState<string[]>([]);
  const [quantity, setQuantity] = useState("1");
  const [chosenScope, setChosenScope] = useState<PurchaseMode | "">("");
  // 默认间隔跟着设置页走(配置没读到时用兜底常量)。
  // 以前这里硬编码 60,而后端四条入队路径写的是 30 —— 弹窗显示的和实际用的对不上。
  const settingsQ = useSettings();
  const cfgDefault = settingsQ.data?.defaultRetryInterval || FALLBACK_RETRY_INTERVAL;
  const [retryInterval, setRetryInterval] = useState("");
  // 配置到手后填进去(用户还没动过输入框才填,不覆盖他正在打的字)
  const touchedRef = useRef(false);
  useEffect(() => {
    if (!touchedRef.current) setRetryInterval(String(cfgDefault));
  }, [cfgDefault]);
  // 默认不自动付款:自动扣钱必须显式打开。
  // 这个对话框和服务器卡片弹的那个是两条建任务入口,开关两边都要有 ——
  // 上一版只加了卡片那边,这边漏了
  const [autoPay, setAutoPay] = useState(false);
  const [duration, setDuration] = useState("P1M");
  const [coupon, setCoupon] = useState("");
  // 用户选的 addon,按组索引。每次切 planCode 自动清空(让用户重新选)。
  const [picked, setPicked] = useState<Partial<Record<OptionGroupKey, string>>>({});
  // 手填的额外 addon planCode(catalog 里没分组覆盖到的、或用户想加的特殊 addon)
  const [extraInput, setExtraInput] = useState("");

  useEffect(() => {
    if (open) {
      setSelectedAccountId(editing?.accountId || "");
      if (initialPlanCode) setPlanCode(initialPlanCode);
      if (editing) {
        setDatacenters([OVH_DATACENTERS.find((dc) => apiDatacenterCode(dc.code) === apiDatacenterCode(editing.datacenter))?.code || editing.datacenter]);
        setQuantity(String(editing.groupLimit || 1));
        setRetryInterval(String(editing.retryInterval || cfgDefault));
        touchedRef.current = true;
        setAutoPay(!!editing.autoPay);
        setDuration(editing.duration || "P1M");
        setCoupon(editing.coupon || "");
      }
    }
  }, [initialPlanCode, open, editing]);

  /** planCode 匹配到的服务器（用于显示名称提示） */
  const matchedServer = useMemo(
    () => searchServers.find((s) => s.planCode === planCode.trim()),
    [searchServers, planCode]
  );
  const [preorderPlan, setPreorderPlan] = useState("");
  const preorder = !!editing?.preorder || !!matchedServer?.prerelease || (!!planCode.trim() && preorderPlan === planCode.trim());
  const purchaseMode: PurchaseMode = editing ? (editing.orderGroup ? "total" : "perDatacenter") : chosenScope || (preorder ? "total" : "perDatacenter");
  useEffect(() => setPreorderPlan((previous) => retainPreorderPlan(previous, planCode.trim(), !!matchedServer?.prerelease)), [planCode, matchedServer?.prerelease]);

  // 切 planCode 清空 picked —— 之前选的 addon 对新机型多半不适用。
  // initialOptions 由外部传入时(从其它入口"快速添加"过来),解析后塞进 picked 让用户能看到。
  const prevPlanCodeRef = useRef("");
  useEffect(() => {
    const code = planCode.trim();
    const selectionKey = `${code}|${!!matchedServer}`;
    if (selectionKey === prevPlanCodeRef.current) return;
    prevPlanCodeRef.current = selectionKey;
    if (initialOptions && code === (initialPlanCode || "").trim()) {
      // 走"外部带 initialOptions 进来"分支:
      //   - 能映射到 chip 组的塞进 picked
      //   - 剩下没匹配上的(chip 没覆盖到的 addon)塞进 extraInput
      const wantedList = splitList(initialOptions);
      const consumed = new Set<string>();
      const next: Partial<Record<OptionGroupKey, string>> = {};
      const groupedMap = matchedServer ? groupOptions(matchedServer.availableOptions) : null;
      if (groupedMap) {
        for (const g of Object.keys(groupedMap) as OptionGroupKey[]) {
          const hit = groupedMap[g].find((o) => wantedList.includes(o.value));
          if (hit) {
            next[g] = hit.value;
            consumed.add(hit.value);
          }
        }
      }
      setPicked(next);
      const leftover = wantedList.filter((v) => !consumed.has(v));
      setExtraInput(leftover.join(", "));
    } else {
      setPicked({});
      setExtraInput("");
    }
  }, [planCode, initialOptions, initialPlanCode, matchedServer]);

  /** 按组拆分该机型的所有可选 addon */
  const grouped = useMemo(
    () => (matchedServer ? groupOptions(matchedServer.availableOptions) : null),
    [matchedServer]
  );
  const defaultValueSet = useMemo(
    () => new Set((matchedServer?.defaultOptions || []).map((o) => o.value)),
    [matchedServer]
  );

  /** 提交给后端的 addon planCode 列表。
   *  matchedServer 在 → 走 chip 选择(picked);不在 → 走手填(extraInput)。
   *  二选一,不混用。 */
  const parsedOptions = useMemo(() => {
    const options = matchedServer ? [...new Set([...(Object.values(picked).filter(Boolean) as string[]), ...splitList(extraInput)])] : splitList(extraInput);
    return preorder ? preorderOptions(options, grouped?.cpu.map((option) => option.value)) : options;
  }, [matchedServer, picked, extraInput, preorder, grouped]);

  // option chip 的绿/红点:跟服务器列表对话框同一套逻辑
  const variants = matchedServer ? variantIndex[matchedServer.planCode] : undefined;
  const prereleaseMismatch = preorder && parsedOptions.length > 0 &&
    !!variants?.length && !variants.some((v) => preorderVariantCoversAll(v.fqn, parsedOptions));
  const optionHasStock = (groupKey: OptionGroupKey, value: string): boolean => {
    if (groupKey === "bandwidth" || groupKey === "vrack" || groupKey === "cpu" || groupKey === "other") {
      return true;
    }
    return hasStockWithOption(
      variants,
      picked as Record<string, string>,
      groupKey,
      value,
      datacenters.length > 0 ? datacenters : undefined
    );
  };

  // 和 servers.tsx 用同一套上限:预览行必须说真会创建的数,
  // 否则会出现"提示 5000 个任务、实际建 60 个"。
  const orderPlan = clampOrderPlan(datacenters.length, Number(quantity) || 1);
  const qty = orderPlan.quantity;
  const totalTasks = orderPlan.total;
  const durationOptions = useOrderDurations(accountId, planCode.trim(), "eco", open);
  const durationSupported = supportsDuration(durationOptions.data, duration);
  const canSubmit = durationSupported && !!activeAcc && planCode.trim().length > 0 && datacenters.length > 0 && qty > 0 && !prereleaseMismatch && (!preorder || hasPreorderHardware(parsedOptions));
  const preparation = useOrderPrewarm(open && !editing && canSubmit, {
    account_id: accountId, planCode: planCode.trim(), datacenters, options: parsedOptions,
    quantity: qty, autoPay, coupon, duration, preorder,
  });

  const reset = () => {
    setPlanCode("");
    setPreorderPlan("");
    setDatacenters([]);
    setQuantity("1");
    setChosenScope("");
    setAutoPay(false);
    setDuration("P1M");
    setCoupon("");
    touchedRef.current = false;
    setRetryInterval(String(cfgDefault));
    setPicked({});
    setExtraInput("");
    prevPlanCodeRef.current = "";
  };

  const handleClose = () => {
    if (saving) return;
    reset();
    onOpenChange(false);
  };

  const toggleDC = (code: string) => {
    setDatacenters((prev) =>
      prev.includes(code) ? prev.filter((c) => c !== code) : editing ? [code] : [...prev, code]
    );
  };

  const selectAllDC = () => setDatacenters(OVH_DATACENTERS.map((d) => d.code));
  const clearAllDC = () => setDatacenters([]);

  const handleSubmit = async () => {
    if (!canSubmit) {
      toast.error(t("queue.toast.fillForm"));
      return;
    }
    if (editing) {
      try {
        await update.mutateAsync({ id: editing.id, datacenter: datacenters[0], options: parsedOptions,
          retryInterval: Number(retryInterval) || cfgDefault, autoPay, coupon: coupon.trim(), duration });
        reset(); onOpenChange(false);
      } catch { /* Error is shown by the mutation hook; keep the form open. */ }
      return;
    }
    const result = await create.mutateAsync({
      account_id: accountId,
      planCode: planCode.trim(),
      datacenters,
      quantity: qty,
      retryInterval: Number(retryInterval) || cfgDefault,
      options: parsedOptions,
      autoPay,
      coupon,
      duration,
      preorder,
      purchaseMode,
    });
    if (result.success > 0) {
      toast.success(t("queue.toast.created", { success: result.success, total: result.total }));
    }
    if (result.failed > 0) {
      toast.error(t("queue.toast.failedCount", { n: result.failed }));
    }
    if (result.success > 0) {
      reset();
      preparation.submitted();
      onOpenChange(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{editing ? t("queue.editTitle") : t("queue.createTitle")}</DialogTitle>
          <DialogDescription>
            {editing ? t("queue.editDesc") : t("queue.createDesc")}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-5 py-2">
          <div>
            <label htmlFor="queue-order-account" className="block text-[13px] font-medium mb-1.5">
              {t("queue.accountLabel")}
            </label>
            <Select
              value={activeAcc?.id || ""}
              disabled={!!editing || accountsQ.isPending || !accounts?.length || saving}
              onValueChange={(id) => {
                setSelectedAccountId(id);
                setCoupon("");
                // 换账户后目录可能属于另一个站点,配置和机房须重新选择。
                setPlanCode("");
                setDatacenters([]);
                setPicked({});
                setExtraInput("");
                prevPlanCodeRef.current = "";
              }}
            >
              <SelectTrigger id="queue-order-account" aria-describedby="queue-account-rule">
                <SelectValue placeholder={t("queue.accountPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                {(accounts || []).map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.name} · {a.zone} · {a.endpoint}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p id="queue-account-rule" className="text-[11px] text-muted-foreground mt-1.5">
              {editing ? t("queue.editIdentity") : t("queue.accountRule")}
            </p>
            {/* 没有账户就没法下单,底下的创建按钮会一直灰着 —— 必须讲清是"没读到"还是"真没有" */}
            {accountsQ.isError && (
              <div className="mt-2">
                <LoadFailedBanner
                  title={t("queue.accountsFailedTitle")}
                  error={accountsQ.error}
                  onRetry={() => accountsQ.refetch()}
                />
              </div>
            )}
            {!accountsQ.isPending && !accountsQ.isError && !accounts?.length && (
              <p className="text-[11px] text-destructive mt-1">
                {t("queue.noAccounts")}
              </p>
            )}
          </div>

          {/* 服务器计划代码 */}
          <div>
            <div className="flex items-center justify-between gap-2 mb-1.5">
              <label className="block text-[13px] font-medium">{t("queue.planCodeLabel")}</label>
              <Button type="button" variant="ghost" size="sm"
                disabled={servers.isFetching || availQ.isFetching}
                onClick={() => { void servers.forceRefresh(); void availQ.refetch(); }}>
                <RefreshCw className={`w-3.5 h-3.5 ${servers.isFetching || availQ.isFetching ? "animate-spin" : ""}`} />
                {t("queue.refreshModels")}
              </Button>
            </div>
            {editing ? <Input value={planCode} readOnly aria-label={t("queue.planCodeLabel")} /> : <PlanCodeCombobox
              value={planCode}
              onChange={setPlanCode}
              servers={searchServers}
              loading={servers.isPending || availQ.isFetching}
              placeholder={t("queue.planCodePlaceholder")}
            />}
            {matchedServer && (
              <p className="text-[11px] text-muted-foreground mt-1 truncate">
                {matchedServer.cpu} · {matchedServer.memory} · {matchedServer.storage}
              </p>
            )}
            {preorder && (
              <p className="text-xs text-warning mt-2" role="status">
                {t("queue.preorderHint")}
              </p>
            )}
            {availQ.isError && (
              <div className="mt-2">
                <LoadFailedBanner
                  title={t("queue.stockFailedTitle")}
                  error={availQ.error}
                  onRetry={() => availQ.refetch()}
                />
              </div>
            )}
            {/* 目录没拉到 ≠ 这个站点没有机型。目录挂了下拉列表就是空的,matchedServer 也必为空,
                界面会连着说两句假话:"没机型可选" + "catalog 里没这个型号"。
                planCode 仍可手输,所以这里只提示,不禁用。 */}
            {servers.isError && (
              <div className="mt-2">
                <LoadFailedBanner
                  title={t("queue.catalogFailedTitle")}
                  error={servers.error}
                  onRetry={() => servers.refetch()}
                />
              </div>
            )}
          </div>

          {/* 数据中心多选 */}
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="block text-[13px] font-medium">
                {t("queue.dcLabel")}
                {datacenters.length > 0 && (
                  <span className="text-muted-foreground ml-2 font-normal">
                    {t("queue.dcSelected", { n: datacenters.length })}
                  </span>
                )}
              </label>
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={selectAllDC}
                  disabled={!!editing}
                  className="text-[11px] text-muted-foreground hover:text-foreground transition-colors"
                >
                  {t("queue.selectAll")}
                </button>
                <span className="text-muted-foreground text-[11px]">/</span>
                <button
                  type="button"
                  onClick={clearAllDC}
                  className="text-[11px] text-muted-foreground hover:text-foreground transition-colors"
                >
                  {t("queue.clear")}
                </button>
              </div>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-2 border border-border rounded-2xl p-3 max-h-56 overflow-y-auto">
              {OVH_DATACENTERS.map((dc) => {
                const checked = datacenters.includes(dc.code);
                return (
                  <label
                    key={dc.code}
                    className="flex items-center gap-2 cursor-pointer text-[13px] py-1"
                  >
                    <Checkbox
                      checked={checked}
                      onCheckedChange={() => toggleDC(dc.code)}
                    />
                    <span className="truncate" title={`${t(`commons.dc.${dc.code}`, { defaultValue: dc.name })} (${dc.code})`}>
                      <span className="font-mono uppercase">{dc.code}</span>
                      <span className="text-muted-foreground ml-1">{t(`commons.dc.${dc.code}`, { defaultValue: dc.name })}</span>
                    </span>
                  </label>
                );
              })}
            </div>
          </div>

          <OrderPurchaseScope id="queue-purchase-scope" value={purchaseMode} onChange={setChosenScope} disabled={!!editing} />

          {/* 数量 + 重试间隔 */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div>
              <label className="block text-[13px] font-medium mb-1.5">
                {t(purchaseMode === "total" ? "queue.qtyTotal" : "queue.qtyPerDc")}
              </label>
              <Input
                type="text"
                inputMode="numeric"
                value={quantity}
                disabled={!!editing}
                onChange={(e) => {
                  const v = e.target.value;
                  if (v === "" || /^\d*$/.test(v)) setQuantity(v);
                }}
                placeholder={t("queue.qtyDefault")}
              />
              <p className="text-[11px] text-muted-foreground mt-1">
                {t("queue.qtyHint", { perDc: MAX_ORDER_QUANTITY, perRun: MAX_ORDER_FANOUT })}
              </p>
            </div>
            <div>
              <label className="block text-[13px] font-medium mb-1.5">
                {t("queue.retryInterval")}
              </label>
              <Input
                type="text"
                inputMode="numeric"
                value={retryInterval}
                onChange={(e) => {
                  const v = e.target.value;
                  touchedRef.current = true;
                  if (v === "" || /^\d*$/.test(v)) setRetryInterval(v);
                }}
                placeholder={t("queue.retryDefault", { n: cfgDefault })}
              />
              <p className="text-[11px] text-muted-foreground mt-1">
                {t("queue.retryHint")}
              </p>
            </div>
          </div>

          <OrderDurationField value={duration} onChange={setDuration} query={durationOptions} />

          <div>
            <label htmlFor="queue-coupon" className="block text-[13px] font-medium mb-1.5">
              {t("queue.couponLabel")}
            </label>
            <Input id="queue-coupon" value={coupon} onChange={(e) => setCoupon(e.target.value)}
              placeholder={t("queue.couponPlaceholder")} autoCapitalize="none" spellCheck={false}
              aria-describedby="queue-coupon-hint" />
            <p id="queue-coupon-hint" className="text-[11px] text-muted-foreground mt-1">
              {t("queue.couponHint")}
            </p>
          </div>

          <AutoPayOption
            checked={autoPay}
            onChange={setAutoPay}
            accountIds={[accountId]}
            title={t("queue.autoPay")}
            description={autoPay ? t("queue.autoPayOn") : t("queue.autoPayOff")}
          />

          {/* 可选配置:planCode 在 catalog 里 → 走 chip 选择;
                planCode 自定义不在 catalog → 走手填。两者互斥不同时存在。 */}
          <div>
            <label className="block text-[13px] font-medium mb-1.5">
              {t("queue.optionsLabel")}
              <span className="text-muted-foreground ml-2 font-normal">
                {/* "catalog 里没找到这个型号"是个结论,只有目录确实读到了才下得了。
                    目录还没到手 / 读失败时照说这句,用户会以为是自己型号填错了。 */}
                {matchedServer?.prerelease
                  ? t("queue.optionsPrerelease")
                  : grouped
                  ? t("queue.optionsChipHint")
                  : !planCode.trim()
                    ? t("queue.optionsPickFirst")
                    : servers.isError
                      ? t("queue.optionsCatalogFailed")
                      : servers.isPending
                        ? t("queue.optionsCatalogLoading")
                        : t("queue.optionsNotFound")}
              </span>
            </label>
            {grouped ? (
              <div className="space-y-4">
                {(["cpu", "memory", "systemStorage", "storage", "bandwidth", "vrack", "other"] as OptionGroupKey[])
                  .filter((g) => grouped[g].length > 0 && !(preorder && g === "cpu"))
                  .map((g) => (
                    <OptionGroupSection
                      key={g}
                      groupKey={g}
                      options={grouped[g]}
                      picked={picked[g] || ""}
                      defaultValueSet={defaultValueSet}
                      hasStock={variants && variants.length > 0 ? (value) => optionHasStock(g, value) : undefined}
                      onPick={(value) =>
                        setPicked((p) => ({
                          ...p,
                          [g]: p[g] === value ? "" : value, // 再点一次取消选中
                        }))
                      }
                    />
                  ))}
              </div>
            ) : (
              // planCode 不在 catalog 里(用户手填了自定义型号) → 走手动输入
              <Input
                placeholder={t("queue.extraOptionsPlaceholder")}
                value={extraInput}
                onChange={(e) => setExtraInput(e.target.value)}
              />
            )}

            {grouped && (editing || extraInput) && <Input className="mt-3" aria-label={t("queue.extraOptionsPlaceholder")}
              placeholder={t("queue.extraOptionsPlaceholder")} value={extraInput} onChange={(e) => setExtraInput(e.target.value)} />}
            {parsedOptions.length > 0 && (
              <div className="flex flex-wrap gap-1.5 mt-3 pt-3 border-t border-border">
                <span className="text-[11px] text-muted-foreground">{t("queue.optionsSelected")}</span>
                {parsedOptions.map((opt, i) => (
                  <Chip key={`${opt}-${i}`} tone="default" className="font-mono">
                    {opt}
                  </Chip>
                ))}
              </div>
            )}
            {preorder && !hasPreorderHardware(parsedOptions) && <p className="text-xs text-muted-foreground mt-2">{t("queue.selectHardwareFirst")}</p>}
            {prereleaseMismatch && (
              <p className="text-xs text-destructive mt-2" role="alert">{t("queue.preorderConfigMismatch")}</p>
            )}
          </div>


          {/* 汇总提示 */}
          {!editing && datacenters.length > 0 && (
            <div className="border border-border rounded-2xl p-3 text-[12px] text-muted-foreground">
              <Trans
                i18nKey={purchaseMode === "total" ? "queue.summaryTotal" : "queue.summary"}
                values={{
                  tasks: totalTasks,
                  dcs: datacenters.length,
                  qty,
                  extra:
                    parsedOptions.length > 0
                      ? t("queue.summaryExtra", { n: parsedOptions.length })
                      : "",
                }}
                components={{ b: <span className="font-semibold text-foreground" /> }}
                t={t}
              />
            </div>
          )}
        </div>

        <OrderPrewarm preparation={preparation} />

        <DialogFooter>
          <span className="mr-auto self-center text-sm text-muted-foreground">{t("orderDuration.label")} · <OrderDurationLabel value={duration} /></span>
          <Button variant="outline" onClick={handleClose} disabled={saving}>
            {t("queue.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={!canSubmit || saving}>
            {saving ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                {editing ? t("queue.saving") : t("queue.creating")}
              </>
            ) : (
              <>
                <Plus className="w-4 h-4" />
                {editing ? t("queue.save") : preorder
                  ? datacenters.length > 0 ? t("queue.createPreorder", { n: totalTasks }) : t("queue.createPreorderAction")
                  : datacenters.length > 0 ? t("queue.createN", { n: totalTasks }) : t("queue.createAction")}
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// 行组件已迁到 components/queue/QueueTable,保留原导出名供其它模块与测试使用
export { QueueRow } from "@/components/queue/QueueTable";

/**
 * 再抢一单:按原任务的账户、套餐、机房、配置、周期、优惠码、预售方式新建 1 个任务。
 * 自动付款涉及真实扣款,不默认沿用 —— 原任务开着时才出现勾选项,需要用户自己勾上。
 */
function BuyAgainDialog({ item, onClose }: { item: QueueItem | null; onClose: () => void }) {
  const { t } = useTranslation();
  const create = useCreateQueueItem();
  const [keepAutoPay, setKeepAutoPay] = useState(false);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    setKeepAutoPay(false);
    setBusy(false);
  }, [item?.id]);
  if (!item) return null;
  const dc = item.datacenter.toUpperCase();
  const submit = async () => {
    setBusy(true);
    const r = await create.mutateAsync({
      account_id: item.accountId,
      planCode: item.planCode,
      datacenters: [item.datacenter],
      options: item.options || [],
      quantity: 1,
      retryInterval: item.retryInterval || undefined,
      autoPay: !!item.autoPay && keepAutoPay,
      coupon: item.coupon,
      duration: item.duration,
      preorder: item.preorder,
    });
    setBusy(false);
    if (r.success > 0) {
      toast.success(t("queue.buyAgainDone", { plan: item.planCode, dc }));
      onClose();
    }
  };
  const row = (label: string, value: ReactNode) => (
    <div className="grid grid-cols-[88px_minmax(0,1fr)] gap-3 py-2 text-[13px]">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 break-words">{value}</span>
    </div>
  );
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("queue.buyAgainTitle")}</DialogTitle>
          <DialogDescription>{t("queue.buyAgainDesc")}</DialogDescription>
        </DialogHeader>
        <div className="divide-y divide-border rounded-lg border border-border px-3">
          {row(t("queue.buyAgainAccount"), <AccountChip accountId={item.accountId} />)}
          {row(t("queue.buyAgainPlan"), <span className="font-mono font-medium">{item.planCode} @ {dc}</span>)}
          {row(t("queue.buyAgainConfig"), item.options?.length ? describeOptionCodes(item.options) : t("history.defaultOptions"))}
          {row(t("queue.buyAgainDuration"), <OrderDurationLabel value={item.duration} />)}
        </div>
        {item.autoPay && (
          <label className="flex items-start gap-2.5 rounded-lg border border-warning/40 bg-warning/[.06] p-3 text-[13px] cursor-pointer">
            <Checkbox className="mt-0.5" checked={keepAutoPay} onCheckedChange={(v) => setKeepAutoPay(v === true)} />
            <span>{t("queue.buyAgainKeepAutoPay")}</span>
          </label>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={busy}>{t("history.cancel")}</Button>
          <Button onClick={() => void submit()} disabled={busy}>
            {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <RotateCw className="w-4 h-4" />}
            {t("queue.buyAgainConfirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
