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
  useClearQueue,
  useCreateQueueItem,
  type QueueItem,
} from "@/hooks/use-queue";
import { useServers } from "@/hooks/use-servers";
import { OVH_DATACENTERS as OVH_DC_LIST } from "@/lib/datacenters";
import { AccountSelect } from "@/components/common/AccountSelect";
import { AccountChip } from "@/components/common/AccountChip";
import { PlanCodeCombobox } from "@/components/common/PlanCodeCombobox";
import { OptionGroupSection } from "@/components/common/OptionGroupSection";
import { groupOptions, type OptionGroupKey } from "@/lib/option-groups";
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

/** 任务重试间隔默认值（秒），与后端 TASK_RETRY_INTERVAL 保持一致 */
const DEFAULT_RETRY_INTERVAL = 60;

function QueuePage() {
  const queue = useQueueList();
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

  return (
    <div className="space-y-6">
      <PageHeader
        icon={ClipboardList}
        title="抢购队列"
        description="管理自动抢购服务器的队列"
        action={
          <div className="flex gap-2">
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
              onClick={() => setShowClearDialog(true)}
              disabled={items.length === 0}
            >
              <Trash2 className="w-4 h-4" />
              清空
            </Button>
          </div>
        }
      />

      {queue.isPending ? (
        <div className="space-y-3">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-20 rounded-2xl" />
          ))}
        </div>
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
          {items.map((q) => (
            <QueueRow
              key={q.id}
              item={q}
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

      <Dialog open={showClearDialog} onOpenChange={setShowClearDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认清空队列？</DialogTitle>
            <DialogDescription>所有任务将被删除，此操作不可撤销。</DialogDescription>
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
  const availQ = useAvailability();
  const variantIndex = useMemo(() => buildVariantIndex(availQ.data), [availQ.data]);
  const create = useCreateQueueItem();
  const [accountId, setAccountId] = useState("");
  const [planCode, setPlanCode] = useState(initialPlanCode || "");
  const [datacenters, setDatacenters] = useState<string[]>([]);
  const [quantity, setQuantity] = useState("1");
  const [retryInterval, setRetryInterval] = useState(String(DEFAULT_RETRY_INTERVAL));
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
      const wantedList = initialOptions.split(",").map((v) => v.trim()).filter(Boolean);
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
    return extraInput
      .split(",")
      .map((v) => v.trim())
      .filter(Boolean);
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

  const qty = Number(quantity) || 1;
  const totalTasks = datacenters.length * qty;
  const canSubmit = !!accountId && planCode.trim().length > 0 && datacenters.length > 0 && qty > 0;

  const reset = () => {
    setPlanCode("");
    setDatacenters([]);
    setQuantity("1");
    setRetryInterval(String(DEFAULT_RETRY_INTERVAL));
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
      retryInterval: Number(retryInterval) || DEFAULT_RETRY_INTERVAL,
      options: parsedOptions,
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
          {/* OVH 账户 */}
          <div>
            <label className="block text-[13px] font-medium mb-1.5">OVH 账户 *</label>
            <AccountSelect value={accountId} onChange={setAccountId} />
            <p className="text-[11px] text-muted-foreground mt-1">下单时用该账户的凭据,购物车 subsidiary 跟随账户 zone</p>
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
                每台服务器单独成单
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
                  if (v === "" || /^\d*$/.test(v)) setRetryInterval(v);
                }}
                placeholder={`默认: ${DEFAULT_RETRY_INTERVAL}`}
              />
              <p className="text-[11px] text-muted-foreground mt-1">
                抢购失败后等待秒数再重试
              </p>
            </div>
          </div>

          {/* 可选配置:planCode 在 catalog 里 → 走 chip 选择;
                planCode 自定义不在 catalog → 走手填。两者互斥不同时存在。 */}
          <div>
            <label className="block text-[13px] font-medium mb-1.5">
              可选配置
              <span className="text-muted-foreground ml-2 font-normal">
                {grouped
                  ? "（点击 chip 选择,留空走 OVH 默认下单）"
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
  onToggle,
  onDelete,
}: {
  item: QueueItem;
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
      <CardContent className="p-3 sm:p-5 flex flex-col sm:flex-row sm:items-center gap-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-1 flex-wrap">
            <span className="font-mono font-semibold text-sm">{item.planCode}</span>
            <AccountChip accountId={item.accountId} />
            <Chip tone="default">DC {item.datacenter.toUpperCase()}</Chip>
            {item.options && item.options.length > 0 && (
              <Chip tone="default">含 {item.options.length} 个可选配置</Chip>
            )}
          </div>
          <div className="text-[11px] text-muted-foreground flex items-center gap-2 flex-wrap">
            <Clock className="w-3 h-3" />
            <span>
              下次尝试 {item.retryCount > 0 ? `${item.retryInterval}秒后（第 ${item.retryCount + 1} 次）` : "即将开始"}
            </span>
            <span>·</span>
            <span>{new Date(item.createdAt).toLocaleString()}</span>
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
