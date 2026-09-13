import { createFileRoute } from "@tanstack/react-router";
import {
  ClipboardList,
  RefreshCw,
  Trash2,
  PauseCircle,
  PlayCircle,
  X,
  Clock,
  Plus,
  Loader2,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
  type QueueItem,
  usePurchaseTimings,
  type PurchaseTiming,
} from "@/hooks/use-queue";
import { useServers } from "@/hooks/use-servers";
import { OVH_DATACENTERS as OVH_DC_LIST } from "@/lib/datacenters";
import { RETRY_INTERVAL, useSettings } from "@/hooks/use-settings";
import { useActiveAccount } from "@/hooks/use-active-account";
import { useAccounts, findAccountByID } from "@/hooks/use-accounts";
import { TimingChip } from "@/components/common/TimingChip";
import { AccountChip } from "@/components/common/AccountChip";
import { PlanCodeCombobox } from "@/components/common/PlanCodeCombobox";
import { OptionGroupSection } from "@/components/common/OptionGroupSection";
import { describeOptionCodes, groupOptions, type OptionGroupKey } from "@/lib/option-groups";
import { splitList } from "@/lib/split-list";
import { clampOrderPlan, MAX_ORDER_QUANTITY, MAX_ORDER_FANOUT } from "@/lib/order-limits";
import {
  useAvailability,
  buildVariantIndex,
  hasStockWithOption,
} from "@/hooks/use-availability";

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
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(String(value));
  const update = useUpdateQueueInterval();

  const commit = () => {
    setEditing(false);
    const n = Number(draft);
    if (!n || n === value) return setDraft(String(value));
    if (n < RETRY_INTERVAL.min || n > RETRY_INTERVAL.max) {
      toast.error(`重试间隔要在 ${RETRY_INTERVAL.min} ~ ${RETRY_INTERVAL.max} 秒之间`);
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
        title="点击修改这条任务的重试间隔"
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

  // —— 批量操作 ——
  // 一条 /buy 或一次网页建单最多扇出 60 个任务(MAX_ORDER_FANOUT),
  // 而在这之前只能一个一个点暂停/删除。抢购结束后清理一批失败任务是高频动作,
  // 「清空」又太狠(会把还在跑的一起删掉)。
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [batchRunning, setBatchRunning] = useState(false);
  const [showBatchDelete, setShowBatchDelete] = useState(false);

  // 任务被别处删掉(TG /cancel、监控自动清理)后,选中集合里会留下不存在的 id。
  // 不剪掉的话「已选 3 项」里可能有 2 项早就没了,批量操作会静默少做几件。
  useEffect(() => {
    setSelected((prev) => {
      if (prev.size === 0) return prev;
      const alive = new Set(items.map((i) => i.id));
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
  const allSelected = items.length > 0 && selected.size === items.length;

  /** 逐条执行并汇总成一条结果,不要弹 N 个 toast */
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
    if (failed === 0) toast.success(`已${label} ${ok} 个任务`);
    else toast.error(`${label}:成功 ${ok} 个,失败 ${failed} 个。${firstError}`);
    queue.refetch();
  };

  return (
    <div className="space-y-3 sm:space-y-6">
      <PageHeader
        icon={ClipboardList}
        title="抢购队列"
        description="管理自动抢购服务器的队列"
        action={
          // 必须 flex-wrap:PageHeader 外层允许换行,内层不换的话整排按钮保持
          // max-content 宽度,在 390px 上会被挤出左边界(实测 left=-19px)。
          <div className="flex flex-wrap justify-end gap-2">
            <Button onClick={() => setShowCreateDialog(true)}>
              <Plus className="w-4 h-4" />
              新建抢购任务
            </Button>
            <Button variant="outline" onClick={() => queue.refetch()} disabled={queue.isFetching}>
              <RefreshCw className={`w-4 h-4 ${queue.isFetching ? "animate-spin" : ""}`} />
              刷新
            </Button>
            <Button
              variant="outline"
              onClick={() => setSelected(allSelected ? new Set() : new Set(items.map((i) => i.id)))}
              disabled={items.length === 0}
            >
              {allSelected ? "取消全选" : "全选"}
            </Button>
            <Button
              variant="outline"
              onClick={() => setShowClearDialog(true)}
              disabled={items.length === 0}
            >
              <Trash2 className="w-4 h-4" />
              清空
            </Button>
          </div>
        }
      />

      {/* 每条链路上一轮的耗时/结论读不到时,行里的 TimingChip 和「上一轮 无货/已下单/出错」
          会整块消失,看起来像"这条任务从来没跑过"。这块只是辅助信息,任务本身照常在跑,
          给一条提示、别让用户误读那片空白就够。 */}
      {timings.isError && items.length > 0 && (
        <LoadFailedBanner
          title="上一轮耗时/结果读取失败,任务照常在跑"
          error={timings.error}
          onRetry={() => timings.refetch()}
        />
      )}

      {queue.isPending ? (
        <div className="space-y-3">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-20 rounded-2xl" />
          ))}
        </div>
      ) : queue.isError ? (
        /* 这页是"我有哪些任务在跑"的唯一真相来源。请求挂了还画「暂无任务 · 点右上角新建」,
           用户会以为任务压根没建成,回头再建一遍 —— 同一台机器排两条队,抢到就是两笔真实订单。
           所以失败必须自己占一支,不能跟空态共用。 */
        <Card>
          <LoadFailed
            icon={ClipboardList}
            title="队列读取失败,不代表队列是空的"
            error={queue.error}
            onRetry={() => queue.refetch()}
          />
        </Card>
      ) : items.length === 0 ? (
        <Card>
          <EmptyState
            icon={ClipboardList}
            title="暂无任务"
            description="点击右上角“新建抢购任务”开始抢购"
          />
        </Card>
      ) : (
        <div className="space-y-3">
          {selected.size > 0 && (
            // 贴在列表顶部而不是浮在底部:手机上底部有 tab 栏,浮层会盖住它
            <div className="sticky top-2 z-20 flex flex-wrap items-center gap-2 rounded-2xl border border-border bg-background/95 backdrop-blur px-3 py-2 shadow-sm">
              <span className="text-[13px] font-medium">已选 {selected.size} 个</span>
              <div className="flex flex-wrap gap-1.5 ml-auto">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={batchRunning}
                  onClick={() =>
                    runBatch("暂停", selectedItems.filter((i) => i.status === "running"), (it) =>
                      toggle.mutateAsync({ id: it.id, action: "pause" })
                    )
                  }
                >
                  <PauseCircle className="w-3.5 h-3.5" />暂停
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={batchRunning}
                  onClick={() =>
                    runBatch("恢复", selectedItems.filter((i) => i.status === "paused"), (it) =>
                      toggle.mutateAsync({ id: it.id, action: "resume" })
                    )
                  }
                >
                  <PlayCircle className="w-3.5 h-3.5" />恢复
                </Button>
                {/* 删除是不可逆的,单独走二次确认,不能和暂停放同一个手势层级 */}
                <Button
                  size="sm"
                  variant="destructive"
                  disabled={batchRunning}
                  onClick={() => setShowBatchDelete(true)}
                >
                  {batchRunning ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Trash2 className="w-3.5 h-3.5" />}
                  删除
                </Button>
                <Button size="sm" variant="ghost" disabled={batchRunning} onClick={() => setSelected(new Set())}>
                  取消选择
                </Button>
              </div>
            </div>
          )}
          {items.map((q) => (
            <QueueRow
              key={q.id}
              item={q}
              timing={timings.data?.[`${q.planCode}@${q.datacenter}`]}
              selected={selected.has(q.id)}
              onSelect={() => toggleSelect(q.id)}
              onToggle={() =>
                toggle.mutate({
                  id: q.id,
                  action: q.status === "running" ? "pause" : "resume",
                })
              }
              onDelete={() => remove.mutate(q.id)}
            />
          ))}
        </div>
      )}

      <Dialog open={showBatchDelete} onOpenChange={setShowBatchDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>删除选中的 {selected.size} 个任务？</DialogTitle>
            <DialogDescription>
              此操作不可撤销。正在执行中的下单（已走到结账那几秒的）可能仍会完成并产生真实订单。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowBatchDelete(false)} disabled={batchRunning}>
              取消
            </Button>
            <Button
              variant="destructive"
              disabled={batchRunning}
              onClick={() => {
                setShowBatchDelete(false);
                void runBatch("删除", selectedItems, (it) => remove.mutateAsync(it.id));
              }}
            >
              确认删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={showClearDialog} onOpenChange={setShowClearDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认清空队列？</DialogTitle>
            <DialogDescription>所有任务将被删除，此操作不可撤销。正在执行中的下单(已走到结账那几秒的)可能仍会完成并产生真实订单。</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowClearDialog(false)}>
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                clear.mutate();
                setShowClearDialog(false);
              }}
            >
              确认清空
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <CreateQueueDialog
        open={showCreateDialog}
        onOpenChange={(v) => {
          setShowCreateDialog(v);
          if (!v) {
            setPrefillPlanCode("");
            setPrefillOptions("");
            // 清掉 URL 上的 create / options 参数
            navigate({ search: () => ({}) as any, replace: true });
          }
        }}
        initialPlanCode={prefillPlanCode}
        initialOptions={prefillOptions}
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
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  initialPlanCode?: string;
  initialOptions?: string;
}) {
  const servers = useServers();
  const create = useCreateQueueItem();
  // 下单账户 = 左侧菜单栏(手机端在顶栏)选的全局账户,本页不再单独选
  const [globalAccountId] = useActiveAccount();
  // 库存按"实际下单的那个账户"所在站点查:EU/US/CA 三站的 availabilities 互不相通
  // (实测 US 站 423 个 planCode,只有 134 个与 EU 重合),用别区的库存点红绿灯,
  // 用户会照着不存在的货建任务,然后在抢购时才被 OVH 拒。
  // 留整个 query 而不是只解构 data:账户列表挂了 → accountId 为空 → canSubmit 恒 false
  // → 创建按钮永久灰着。不把 isError 摆出来,用户只看到一个点不动的按钮,不知道是自己没填够
  // 还是我们没读到账户。
  const accountsQ = useAccounts();
  const accounts = accountsQ.data;
  const accountId = globalAccountId || accounts?.find((a) => a.isDefault)?.id || accounts?.[0]?.id || "";
  const activeAcc = findAccountByID(accounts, accountId);
  const orderEndpoint = activeAcc?.endpoint || "";
  const availQ = useAvailability(orderEndpoint || undefined);
  const variantIndex = useMemo(() => buildVariantIndex(availQ.data), [availQ.data]);
  const [planCode, setPlanCode] = useState(initialPlanCode || "");
  const [datacenters, setDatacenters] = useState<string[]>([]);
  const [quantity, setQuantity] = useState("1");
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
  // 用户选的 addon,按组索引。每次切 planCode 自动清空(让用户重新选)。
  const [picked, setPicked] = useState<Partial<Record<OptionGroupKey, string>>>({});
  // 手填的额外 addon planCode(catalog 里没分组覆盖到的、或用户想加的特殊 addon)
  const [extraInput, setExtraInput] = useState("");

  useEffect(() => {
    if (open) {
      if (initialPlanCode) setPlanCode(initialPlanCode);
    }
  }, [initialPlanCode, open]);

  /** planCode 匹配到的服务器（用于显示名称提示） */
  const matchedServer = useMemo(
    () => (servers.data || []).find((s) => s.planCode === planCode.trim()),
    [servers.data, planCode]
  );

  // 切 planCode 清空 picked —— 之前选的 addon 对新机型多半不适用。
  // initialOptions 由外部传入时(从其它入口"快速添加"过来),解析后塞进 picked 让用户能看到。
  const prevPlanCodeRef = useRef("");
  useEffect(() => {
    const code = planCode.trim();
    if (code === prevPlanCodeRef.current) return;
    prevPlanCodeRef.current = code;
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
    if (matchedServer) {
      return Object.values(picked).filter(Boolean) as string[];
    }
    return splitList(extraInput);
  }, [matchedServer, picked, extraInput]);

  // option chip 的绿/红点:跟服务器列表对话框同一套逻辑
  const variants = matchedServer ? variantIndex[matchedServer.planCode] : undefined;
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
  const canSubmit = !!accountId && planCode.trim().length > 0 && datacenters.length > 0 && qty > 0;

  const reset = () => {
    setPlanCode("");
    setDatacenters([]);
    setQuantity("1");
    touchedRef.current = false;
    setRetryInterval(String(cfgDefault));
    setPicked({});
    setExtraInput("");
    prevPlanCodeRef.current = "";
  };

  const handleClose = () => {
    if (create.isPending) return;
    onOpenChange(false);
  };

  const toggleDC = (code: string) => {
    setDatacenters((prev) =>
      prev.includes(code) ? prev.filter((c) => c !== code) : [...prev, code]
    );
  };

  const selectAllDC = () => setDatacenters(OVH_DATACENTERS.map((d) => d.code));
  const clearAllDC = () => setDatacenters([]);

  const handleSubmit = async () => {
    if (!canSubmit) {
      toast.error("请填写计划代码并至少选择一个数据中心");
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
    });
    if (result.success > 0) {
      toast.success(`已创建 ${result.success}/${result.total} 个抢购任务`);
    }
    if (result.failed > 0) {
      toast.error(`${result.failed} 个任务创建失败`);
    }
    if (result.success > 0) {
      reset();
      onOpenChange(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>新建抢购任务</DialogTitle>
          <DialogDescription>
            为每个数据中心创建指定数量的独立任务，每台服务器单独成单。
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-5 py-2">
          {/* 当前账户不在这里重复显示 —— 顶栏(手机)/侧栏(桌面)的切换器始终可见,
              对话框打开时它也没被盖住。
              但下面这两条要留着:一条是规则(拿错站点的 planCode 必然被拒),
              一条是提交会被拦掉的理由,都不是"当前账户是谁"的重复。 */}
          <div>
            <p className="text-[11px] text-muted-foreground">
              下单用当前账户的凭据,购物车 subsidiary 跟随账户 zone。planCode 也要是这个站点的 ——
              三区目录互不相通
            </p>
            {/* 没有账户就没法下单,底下的创建按钮会一直灰着 —— 必须讲清是"没读到"还是"真没有" */}
            {accountsQ.isError && (
              <div className="mt-2">
                <LoadFailedBanner
                  title="账户列表读取失败,创建按钮会一直灰着"
                  error={accountsQ.error}
                  onRetry={() => accountsQ.refetch()}
                />
              </div>
            )}
            {!accountsQ.isPending && !accountsQ.isError && !accountId && (
              <p className="text-[11px] text-destructive mt-1">
                还没有任何 OVH 账户,先到「设置 → OVH 账户」添加一个才能建任务。
              </p>
            )}
          </div>

          {/* 服务器计划代码 */}
          <div>
            <label className="block text-[13px] font-medium mb-1.5">服务器计划代码</label>
            <PlanCodeCombobox
              value={planCode}
              onChange={setPlanCode}
              servers={servers.data || []}
              placeholder="选择或搜索服务器型号"
            />
            {matchedServer && (
              <p className="text-[11px] text-muted-foreground mt-1 truncate">
                {matchedServer.cpu} · {matchedServer.memory} · {matchedServer.storage}
              </p>
            )}
            {/* 目录没拉到 ≠ 这个站点没有机型。目录挂了下拉列表就是空的,matchedServer 也必为空,
                界面会连着说两句假话:"没机型可选" + "catalog 里没这个型号"。
                planCode 仍可手输,所以这里只提示,不禁用。 */}
            {servers.isError && (
              <div className="mt-2">
                <LoadFailedBanner
                  title="机型目录读取失败,下拉列表是空的(不是这个站点没有机型)"
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
                选择数据中心
                {datacenters.length > 0 && (
                  <span className="text-muted-foreground ml-2 font-normal">
                    （已选 {datacenters.length}）
                  </span>
                )}
              </label>
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={selectAllDC}
                  className="text-[11px] text-muted-foreground hover:text-foreground transition-colors"
                >
                  全选
                </button>
                <span className="text-muted-foreground text-[11px]">/</span>
                <button
                  type="button"
                  onClick={clearAllDC}
                  className="text-[11px] text-muted-foreground hover:text-foreground transition-colors"
                >
                  清空
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
                    <span className="truncate" title={`${dc.name} (${dc.code})`}>
                      <span className="font-mono uppercase">{dc.code}</span>
                      <span className="text-muted-foreground ml-1">{dc.name}</span>
                    </span>
                  </label>
                );
              })}
            </div>
          </div>

          {/* 数量 + 重试间隔 */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div>
              <label className="block text-[13px] font-medium mb-1.5">
                每个数据中心数量
              </label>
              <Input
                type="text"
                inputMode="numeric"
                value={quantity}
                onChange={(e) => {
                  const v = e.target.value;
                  if (v === "" || /^\d*$/.test(v)) setQuantity(v);
                }}
                placeholder="默认: 1"
              />
              <p className="text-[11px] text-muted-foreground mt-1">
                每台服务器单独成单（每机房最多 {MAX_ORDER_QUANTITY} 台，单次最多 {MAX_ORDER_FANOUT} 个任务）
              </p>
            </div>
            <div>
              <label className="block text-[13px] font-medium mb-1.5">
                重试间隔（秒）
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
                placeholder={`默认: ${cfgDefault}`}
              />
              <p className="text-[11px] text-muted-foreground mt-1">
                抢购失败后等待秒数再重试（默认值在「设置 → 抢购」里改）
              </p>
            </div>
          </div>

          <label className="flex items-center gap-2 cursor-pointer text-[13px]">
            <Checkbox checked={autoPay} onCheckedChange={(v) => setAutoPay(!!v)} />
            抢到后自动付款
          </label>
          <p className="text-[11px] text-muted-foreground -mt-2">
            {autoPay
              ? "下单成功后用 OVH 默认支付方式自动扣款（需先在 OVH 设置好）"
              : "不勾则只下单：需在订单过期前自己付款"}
          </p>

          {/* 可选配置:planCode 在 catalog 里 → 走 chip 选择;
                planCode 自定义不在 catalog → 走手填。两者互斥不同时存在。 */}
          <div>
            <label className="block text-[13px] font-medium mb-1.5">
              可选配置
              <span className="text-muted-foreground ml-2 font-normal">
                {/* "catalog 里没找到这个型号"是个结论,只有目录确实读到了才下得了。
                    目录还没到手 / 读失败时照说这句,用户会以为是自己型号填错了。 */}
                {grouped
                  ? "（点击 chip 选择,留空走 OVH 默认下单）"
                  : !planCode.trim()
                    ? "（先选个型号,再挑可选配置）"
                    : servers.isError
                      ? "（机型目录没读到,列不出可选配置;可重试目录,或直接手填 addon planCode）"
                      : servers.isPending
                        ? "（机型目录读取中…）"
                        : "（catalog 里没找到这个型号,需要手填 addon planCode）"}
              </span>
            </label>
            {grouped ? (
              <div className="space-y-4">
                {(["cpu", "memory", "systemStorage", "storage", "bandwidth", "vrack", "other"] as OptionGroupKey[])
                  .filter((g) => grouped[g].length > 0)
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
                placeholder="addon planCode,逗号分隔。例如:ram-64g-ecc-2400, softraid-2x450nvme-24sk50"
                value={extraInput}
                onChange={(e) => setExtraInput(e.target.value)}
              />
            )}

            {parsedOptions.length > 0 && (
              <div className="flex flex-wrap gap-1.5 mt-3 pt-3 border-t border-border">
                <span className="text-[11px] text-muted-foreground">已选:</span>
                {parsedOptions.map((opt, i) => (
                  <Chip key={`${opt}-${i}`} tone="default" className="font-mono">
                    {opt}
                  </Chip>
                ))}
              </div>
            )}
          </div>


          {/* 汇总提示 */}
          {datacenters.length > 0 && (
            <div className="border border-border rounded-2xl p-3 text-[12px] text-muted-foreground">
              将创建 <span className="font-semibold text-foreground">{totalTasks}</span> 个独立任务
              （{datacenters.length} 个数据中心 × {qty} 台
              {parsedOptions.length > 0 ? ` · 含 ${parsedOptions.length} 个可选配置` : ""}）
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={create.isPending}>
            取消
          </Button>
          <Button onClick={handleSubmit} disabled={!canSubmit || create.isPending}>
            {create.isPending ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                创建中...
              </>
            ) : (
              <>
                <Plus className="w-4 h-4" />
                {datacenters.length > 0 ? `创建 ${totalTasks} 个任务` : "创建任务"}
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function QueueRow({
  item,
  timing,
  selected,
  onSelect,
  onToggle,
  onDelete,
}: {
  item: QueueItem;
  timing?: PurchaseTiming;
  selected: boolean;
  onSelect: () => void;
  onToggle: () => void;
  onDelete: () => void;
}) {
  const chip = (() => {
    if (item.status === "running")
      return (
        <Chip tone="success">
          <StatusDot tone="success" pulse size="xs" />运行中
        </Chip>
      );
    if (item.status === "pending")
      return (
        <Chip tone="warning">
          <StatusDot tone="warning" size="xs" />等待中
        </Chip>
      );
    if (item.status === "paused")
      return (
        <Chip tone="default">
          <StatusDot tone="muted" size="xs" />已暂停
        </Chip>
      );
    if (item.status === "completed")
      return (
        <Chip tone="info">
          <StatusDot tone="info" size="xs" />已完成
        </Chip>
      );
    return (
      <Chip tone="danger">
        <StatusDot tone="danger" size="xs" />失败
      </Chip>
    );
  })();

  return (
    <Card>
      <CardContent className="p-3 sm:p-5 flex flex-col sm:flex-row sm:items-start gap-3">
        <div className="flex items-start gap-3 flex-1 min-w-0">
          {/* 复选框单独占一列,点它不会触发行上的其它动作 */}
          <Checkbox
            checked={selected}
            onCheckedChange={onSelect}
            aria-label={`选择任务 ${item.planCode} @ ${item.datacenter}`}
            className="mt-0.5 flex-shrink-0"
          />
          <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-1 flex-wrap">
            <span className="font-mono font-semibold text-sm">{item.planCode}</span>
            <AccountChip accountId={item.accountId} />
            <Chip tone="default">DC {item.datacenter.toUpperCase()}</Chip>
            {item.options && item.options.length > 0 && (
              // 以前只显示个数。而一个型号底下几套配置的差别恰恰在这里 ——
              // 同时下了三单时,用户看到三张"含 2 个可选配置"的卡片,
              // 分不出哪一单抢的是 64G+NVMe、哪一单是 32G+HDD。
              // describeOptionCodes 纯靠 code 正则解析,不需要目录,
              // 机型下架或目录没拉到时也能显示。
              <Chip tone="default" title={item.options.join("\n")}>
                {describeOptionCodes(item.options)}
              </Chip>
            )}
            {item.autoPay && (
              <Chip tone="warning" title="下单成功后会用 OVH 默认支付方式自动扣款">
                自动付款
              </Chip>
            )}
            <TimingChip totalMs={timing?.totalMs} phases={timing?.phases} />
          </div>
          <div className="text-[11px] text-muted-foreground flex items-center gap-2 flex-wrap">
            <Clock className="w-3 h-3" />
            {/* failed / completed 是终态,不会再重试 —— 再显示"下次尝试"会让用户以为还在排队。
                失败原因写在抢购历史里,这里给一句指引。 */}
            {item.status === "failed" ? (
              <span>已停止重试（原因见抢购历史）</span>
            ) : item.status === "completed" ? (
              <span>已完成</span>
            ) : (
              <span className="inline-flex items-center gap-1">
                下次尝试
                {item.retryCount > 0 ? (
                  <>
                    <IntervalEditor id={item.id} value={item.retryInterval} />
                    秒后（第 {item.retryCount + 1} 次）
                  </>
                ) : (
                  "即将开始"
                )}
              </span>
            )}
            {timing && (
              <>
                <span>·</span>
                {/* 上一轮的结论。"没货"和"下单失败"是两件事:前者说明这台机器
                    OVH 就是没放货,后者说明我们这边有问题,该看历史里的错误信息 */}
                <span>
                  上一轮
                  {timing.outcome === "unavailable"
                    ? "无货"
                    : timing.outcome === "ordered"
                      ? "已下单"
                      : "出错"}
                </span>
              </>
            )}
            {typeof item.failureCount === "number" && item.failureCount > 0 && (
              <>
                <span>·</span>
                <span>下单失败 {item.failureCount} 次</span>
              </>
            )}
            <span>·</span>
            <span>{new Date(item.createdAt).toLocaleString()}</span>
          </div>
          </div>
        </div>
        <div className="flex items-center gap-2 flex-shrink-0">
          {chip}
          {item.status !== "completed" && item.status !== "failed" && (
            <Button variant="ghost" size="icon" onClick={onToggle} aria-label={item.status === "running" ? "暂停" : "恢复"}>
              {item.status === "running" ? <PauseCircle className="w-4 h-4" /> : <PlayCircle className="w-4 h-4" />}
            </Button>
          )}
          <Button variant="ghost" size="icon" onClick={onDelete} aria-label="删除">
            <X className="w-4 h-4" />
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
