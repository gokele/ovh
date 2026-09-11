import { createFileRoute, Link } from "@tanstack/react-router";
import {
  Bell,
  BellOff,
  RefreshCw,
  Trash2,
  X,
  History as HistoryIcon,
  ChevronUp,
  Plus,
  AlertTriangle,
  Pencil,
  HelpCircle,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { Chip } from "@/components/common/Chip";
import { useActiveAccount } from "@/hooks/use-active-account";
import { findAccountByID, useAccounts } from "@/hooks/use-accounts";
import { AccountChip } from "@/components/common/AccountChip";
import { StatusDot } from "@/components/common/StatusDot";
import { EmptyState } from "@/components/common/EmptyState";
import { Skeleton } from "@/components/common/Skeleton";
import { LoadFailed, errorMessage } from "@/components/common/LoadFailed";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  useMonitorList,
  useMonitorStatus,
  useRemoveMonitorSubscription,
  useClearMonitor,
  useCreateMonitorSubscription,
  useUpdateMonitorSubscription,
  useMonitorHistory,
  type MonitorSubscription,
  useSetMonitorInterval,
} from "@/hooks/use-monitor";
import { useNotifyGate } from "@/hooks/use-notify-channels";
import { toast } from "sonner";
import { useServers } from "@/hooks/use-servers";
import { describeOptionCodes, groupOptions, type OptionGroupKey } from "@/lib/option-groups";
import { OptionGroupSection } from "@/components/common/OptionGroupSection";

/** 服务器监控订阅 */
export const Route = createFileRoute("/monitor")({
  component: MonitorPage,
});

function MonitorPage() {
  const list = useMonitorList();
  const status = useMonitorStatus();
  const remove = useRemoveMonitorSubscription();
  const clear = useClearMonitor();
  const [confirmClear, setConfirmClear] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null);
  const [openAdd, setOpenAdd] = useState(false);
  // 正在编辑的订阅。null = 新增模式,两种模式共用同一个对话框
  const [editing, setEditing] = useState<MonitorSubscription | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);

  const subs = list.data || [];

  // 监控状态是三态:还在读 / 读失败 / 真数据。后两者绝不能混。
  // status 接口挂了 ≠ 监控停了 —— 后端的监控协程照跑、订阅照检查,只是这一次 HTTP 没问到。
  // 以前这里直接 `status.data?.running` 取到 undefined,界面就白纸黑字写「已停止」,
  // 订阅数 / 间隔 / 已知服务器还一律 ?? 0。用户据此去重加订阅、重启监控,
  // 反而把本来在跑的东西弄乱。所以未知就必须说未知,数字一律显示 —。
  const running = !!status.data?.running;
  const statusUnknown = status.isPending || status.isError;
  /** 状态里的数字:0 是有含义的真值（真的一条订阅都没有），不能拿它冒充"没读到" */
  const statNum = (v: number | undefined): number | string =>
    status.isPending ? "…" : status.isError || v === undefined ? "—" : v;

  return (
    <div className="space-y-3 sm:space-y-6">
      <PageHeader
        icon={Bell}
        title="服务器监控"
        description="自动监控服务器可用性变化并推送通知"
        action={
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => list.refetch()} disabled={list.isFetching}>
              <RefreshCw className={`w-4 h-4 ${list.isFetching ? "animate-spin" : ""}`} />
              刷新
            </Button>
            <Button onClick={() => setOpenAdd(true)}>
              <Plus className="w-4 h-4" />
              添加订阅
            </Button>
            <Button
              variant="outline"
              onClick={() => setConfirmClear(true)}
              disabled={subs.length === 0}
            >
              <Trash2 className="w-4 h-4" />
              清空全部
            </Button>
          </div>
        }
      />

      {/* 状态卡 */}
      <Card>
        <CardContent className="p-5 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-full bg-secondary flex items-center justify-center">
              {statusUnknown ? (
                <HelpCircle
                  className={`w-5 h-5 ${status.isError ? "text-warning" : "text-muted-foreground"}`}
                />
              ) : running ? (
                <Bell className="w-5 h-5 text-success" />
              ) : (
                <BellOff className="w-5 h-5 text-muted-foreground" />
              )}
            </div>
            <div>
              <div className="text-sm font-semibold">监控状态</div>
              <div className="text-xs text-muted-foreground inline-flex items-center gap-1.5">
                <StatusDot
                  tone={status.isError ? "warning" : running ? "success" : "muted"}
                  pulse={running && !statusUnknown}
                  size="xs"
                />
                {status.isPending ? "读取中…" : status.isError ? "状态未知" : running ? "运行中" : "已停止"}
              </div>
              {status.isError && (
                <button
                  type="button"
                  className="block text-left text-[11px] text-destructive underline underline-offset-2 mt-0.5 max-w-xs"
                  onClick={() => status.refetch()}
                  title="重新读取监控状态"
                >
                  读不到监控状态：{errorMessage(status.error)} · 点此重试
                </button>
              )}
            </div>
          </div>
          <div className="flex gap-6 text-sm">
            <Stat label="订阅数" value={statNum(status.data?.subscriptions_count)} />
            <IntervalStat
              current={status.isError ? undefined : status.data?.check_interval}
              unknownLabel={status.isPending ? "…" : "—"}
            />
            <Stat label="已知服务器" value={statNum(status.data?.known_servers_count)} />
          </div>
        </CardContent>
      </Card>

      {/* 订阅列表 */}
      {list.isPending ? (
        <div className="space-y-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-24 rounded-2xl" />
          ))}
        </div>
      ) : list.isError ? (
        /* 列表没读到 ≠ 一条订阅都没有。以前这里直接掉进"暂无订阅",
           而订阅其实还在后端跑着 —— 用户看见空列表会照着再加一遍,
           同一个型号被订两次,补货时就通知两次、自动下单两次。 */
        <Card>
          <LoadFailed
            icon={Bell}
            title="订阅列表读取失败"
            error={list.error}
            onRetry={() => list.refetch()}
          />
        </Card>
      ) : subs.length === 0 ? (
        <Card>
          <EmptyState
            icon={Bell}
            title="暂无订阅"
            description='点击"添加订阅"按钮开始监控服务器'
          />
        </Card>
      ) : (
        <div className="space-y-3">
          {subs.map((s) => (
            <SubRow
              key={s.planCode}
              sub={s}
              expanded={expanded === s.planCode}
              onToggleExpand={() =>
                setExpanded((curr) => (curr === s.planCode ? null : s.planCode))
              }
              onEdit={() => {
                setEditing(s);
                setOpenAdd(true);
              }}
              onDelete={() => setConfirmRemove(s.planCode)}
            />
          ))}
        </div>
      )}

      {/* 添加订阅 Dialog */}
      <AddSubscriptionDialog
        open={openAdd}
        editing={editing}
        onOpenChange={(v) => {
          setOpenAdd(v);
          // 关掉就退出编辑模式,否则下次点"添加订阅"会带出上一条的内容
          if (!v) setEditing(null);
        }}
      />

      {/* 删除确认 */}
      <Dialog open={!!confirmRemove} onOpenChange={(v) => !v && setConfirmRemove(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>取消订阅</DialogTitle>
            <DialogDescription>
              确定要取消订阅 <span className="font-mono">{confirmRemove}</span> 吗？
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmRemove(null)}>
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                if (confirmRemove) remove.mutate(confirmRemove);
                setConfirmRemove(null);
              }}
            >
              确定
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 清空确认 */}
      <Dialog open={confirmClear} onOpenChange={setConfirmClear}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认清空所有订阅？</DialogTitle>
            <DialogDescription>所有监控订阅将被删除，此操作不可撤销。</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmClear(false)}>
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                clear.mutate();
                setConfirmClear(false);
              }}
            >
              确认清空
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/* ------------------------------ 行 / 历史 ------------------------------ */

function SubRow({
  sub,
  expanded,
  onToggleExpand,
  onEdit,
  onDelete,
}: {
  sub: MonitorSubscription;
  expanded: boolean;
  onToggleExpand: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <Card>
      <CardContent className="p-5">
        <div className="flex flex-col sm:flex-row sm:items-center gap-3">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-1 flex-wrap">
              <span className="font-mono font-semibold text-sm">{sub.planCode}</span>
              {sub.serverName && (
                <span className="text-xs text-muted-foreground">| {sub.serverName}</span>
              )}
            </div>
            <p className="text-xs text-muted-foreground mb-1.5">
              {sub.datacenters.length > 0
                ? `监控数据中心: ${sub.datacenters.join(", ")}`
                : "监控所有数据中心"}
            </p>
            <div className="flex gap-1.5 flex-wrap items-center">
              {/* 盯全部配置 vs 只盯一套,是「会不会一次触发好几单」的分水岭,
                  必须在列表上一眼看得出来 */}
              {sub.options && sub.options.length > 0 ? (
                <Chip tone="default" title={sub.options.join("\n")}>
                  只盯 {describeOptionCodes(sub.options)}
                </Chip>
              ) : (
                <Chip tone="default" title="该型号的每套内存/存储组合都会各自触发通知与自动下单">
                  盯全部配置
                </Chip>
              )}
              {sub.notifyAvailable && <Chip tone="success">有货提醒</Chip>}
              {sub.notifyUnavailable && <Chip tone="warning">无货提醒</Chip>}
              {sub.autoOrder && sub.autoOrderAccountId ? (
                <>
                  <Chip tone="solid" title={sub.autoPay ? "下单成功后自动付款" : "只下单,需自己付款"}>
                    自动下单
                    {sub.quantity && sub.quantity > 1 ? ` ×${sub.quantity}` : ""}
                    {sub.autoPay ? " · 自动付款" : ""}
                  </Chip>
                  <span className="text-[11px] text-muted-foreground">→</span>
                  <AccountChip accountId={sub.autoOrderAccountId} />
                </>
              ) : sub.autoOrder ? (
                <Chip tone="warning">已勾自动下单但未选账户(只通知)</Chip>
              ) : null}
            </div>
          </div>
          <div className="flex items-center gap-1 flex-shrink-0">
            <Button
              variant="ghost"
              size="icon"
              aria-label="查看历史"
              onClick={onToggleExpand}
            >
              {expanded ? (
                <ChevronUp className="w-4 h-4" />
              ) : (
                <HistoryIcon className="w-4 h-4" />
              )}
            </Button>
            <Button variant="ghost" size="icon" onClick={onEdit} aria-label="编辑订阅" title="改机房 / 提醒方式 / 自动下单">
              <Pencil className="w-4 h-4" />
            </Button>
            <Button variant="ghost" size="icon" onClick={onDelete} aria-label="删除">
              <X className="w-4 h-4" />
            </Button>
          </div>
        </div>

        {expanded && (
          <div className="mt-4 pt-4 border-t border-border">
            <HistoryPanel planCode={sub.planCode} />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function HistoryPanel({ planCode }: { planCode: string }) {
  const history = useMonitorHistory(planCode);

  if (history.isPending) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-10 rounded-xl" />
        ))}
      </div>
    );
  }

  // "暂无历史记录"是一句结论:这台机器从订阅到现在一次都没变过货,
  // 用户会据此判断"这型号根本不补货,别等了"。读失败时说这句话就是在给假消息。
  if (history.isError) {
    return (
      <LoadFailed
        icon={HistoryIcon}
        title="变化历史读取失败"
        error={history.error}
        onRetry={() => history.refetch()}
        compact
      />
    );
  }

  const entries = history.data || [];

  return (
    <div>
      <div className="flex items-center gap-2 mb-3">
        <HistoryIcon className="w-4 h-4 text-muted-foreground" />
        <span className="text-sm font-medium">变化历史</span>
      </div>
      {entries.length === 0 ? (
        <p className="text-xs text-muted-foreground text-center py-4">暂无历史记录</p>
      ) : (
        <div className="space-y-2 max-h-64 overflow-y-auto">
          {entries.map((e, i) => (
            <div
              key={i}
              className="flex items-start gap-3 p-2.5 bg-muted/40 rounded-xl text-xs"
            >
              <StatusDot
                tone={e.changeType === "available" ? "success" : "danger"}
                size="sm"
                className="mt-1"
              />
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="font-medium">{e.datacenter?.toUpperCase()}</span>
                  <Chip tone={e.changeType === "available" ? "success" : "danger"}>
                    {e.changeType === "available" ? "有货" : "无货"}
                  </Chip>
                  {e.config?.display && (
                    <span className="px-2 py-0.5 rounded-full bg-secondary text-[11px]">
                      {e.config.display}
                    </span>
                  )}
                </div>
                <p className="text-muted-foreground mt-1">{formatTime(e.timestamp)}</p>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function formatTime(ts: string): string {
  const d = new Date(ts);
  if (isNaN(d.getTime())) return ts;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(
    d.getHours()
  )}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

/* ----------------------------- 添加订阅 Dialog ----------------------------- */

/**
 * 新增 / 编辑订阅共用一个对话框。
 *
 * 编辑模式下型号是只读的：型号就是订阅的身份，改它等于换一台机器，
 * 那该走"删掉旧的、加个新的"，而不是把一条带着历史记录的订阅偷偷指向别处。
 */
function AddSubscriptionDialog({
  open,
  onOpenChange,
  editing,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  editing?: MonitorSubscription | null;
}) {
  const create = useCreateMonitorSubscription();
  const update = useUpdateMonitorSubscription();
  const isEdit = !!editing;
  // 门禁看的是「所有通道」,不是只看 Telegram —— 只配 webhook 的用户也该能订阅
  const [notifyBlocked, notifyReason, notifyChecking] = useNotifyGate();
  const [planCode, setPlanCode] = useState("");
  const [datacenters, setDatacenters] = useState("");
  const [notifyAvailable, setNotifyAvailable] = useState(true);
  const [notifyUnavailable, setNotifyUnavailable] = useState(false);
  const [autoOrder, setAutoOrder] = useState(false);
  const [quantity, setQuantity] = useState(1);
  // 默认不自动付款:自动扣钱必须显式打开
  const [autoPay, setAutoPay] = useState(false);
  // 只盯哪一套配置。空 = 盯全部配置(老行为)。
  // 后端引擎一直支持,以前前端没接 —— 于是网页建的订阅永远是「盯全部」,
  // 而通知和自动下单是按配置逐套触发的:三套配置同时补货就是三份通知、三单。
  const [picked, setPicked] = useState<Partial<Record<OptionGroupKey, string>>>({});
  // 型号不在目录里(手输的冷门机型 / 目录没拉到)时退回手填,跟下单弹窗同一套策略
  const [extraOptions, setExtraOptions] = useState("");
  const serversQ = useServers();
  const matchedServer = useMemo(
    () => (serversQ.data || []).find((sv) => sv.planCode === planCode.trim()),
    [serversQ.data, planCode]
  );
  const grouped = useMemo(
    () => (matchedServer ? groupOptions(matchedServer.availableOptions) : null),
    [matchedServer]
  );
  const defaultValueSet = useMemo(
    () => new Set((matchedServer?.defaultOptions || []).map((o) => o.value)),
    [matchedServer]
  );
  /** 提交给后端的 addon 列表:目录里有这个型号就走 chip,没有就走手填,二选一不混用 */
  const chosenOptions = useMemo(() => {
    if (matchedServer) return Object.values(picked).filter(Boolean) as string[];
    return extraOptions.split(",").map((v) => v.trim()).filter(Boolean);
  }, [matchedServer, picked, extraOptions]);
  // 订阅的下单账户 = 左侧菜单栏的全局账户,不再单独选
  const [globalAccountId] = useActiveAccount();
  const accountsQ = useAccounts();
  const allAccounts = accountsQ.data;
  const autoOrderAccountId =
    globalAccountId || allAccounts?.find((a) => a.isDefault)?.id || allAccounts?.[0]?.id || "";
  const activeAcc = findAccountByID(allAccounts, autoOrderAccountId);
  // 账户列表读失败 ≠ 一个账户都没配。两种情况以前都显示「未选择账户」,
  // 然后自动下单被静默拦掉 —— 用户看到的是一条没有理由的死路:
  // 去账户页明明有账户,回来还是"未选择"。失败必须说出失败,并给重试。
  const accountsFailed = accountsQ.isError && !activeAcc;

  const reset = () => {
    setPlanCode("");
    setDatacenters("");
    setNotifyAvailable(true);
    setNotifyUnavailable(false);
    setAutoOrder(false);
    setQuantity(1);
    setAutoPay(false);
    setPicked({});
    setExtraOptions("");
  };

  // 每次打开都按当前 editing 重灌一次表单。依赖里带上 open,
  // 否则用户改了几个字段又取消,下次打开看到的还是上次改了一半的样子。
  useEffect(() => {
    if (!open) return;
    if (editing) {
      setPlanCode(editing.planCode);
      setDatacenters((editing.datacenters || []).join(", "));
      setNotifyAvailable(editing.notifyAvailable);
      setNotifyUnavailable(editing.notifyUnavailable);
      setAutoOrder(!!editing.autoOrder);
      setQuantity(editing.quantity && editing.quantity > 0 ? editing.quantity : 1);
      setAutoPay(!!editing.autoPay);
      // 回填已选配置:能映射到 chip 组的塞进 picked,剩下的塞进手填框。
      // 不回填的话,用户只想改个机房、保存时 options 就被空数组覆盖 ——
      // 订阅会从「只盯 64G+NVMe」悄悄变成「盯全部配置」。
      const want = editing.options || [];
      const g = matchedServer ? groupOptions(matchedServer.availableOptions) : null;
      const next: Partial<Record<OptionGroupKey, string>> = {};
      const used = new Set<string>();
      if (g) {
        for (const key of Object.keys(g) as OptionGroupKey[]) {
          const hit = g[key].find((o) => want.includes(o.value));
          if (hit) {
            next[key] = hit.value;
            used.add(hit.value);
          }
        }
      }
      setPicked(next);
      setExtraOptions(want.filter((v) => !used.has(v)).join(", "));
    } else {
      reset();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, editing, matchedServer]);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const code = planCode.trim();
    if (!code) {
      toast.error("请输入服务器型号");
      return;
    }
    const dcs = datacenters
      .split(",")
      .map((d) => d.trim())
      .filter(Boolean);

    if (autoOrder && !autoOrderAccountId) {
      // 读失败和"真的没账户"要给不同的话:前者该重试,后者该去加账户
      toast.error(
        accountsFailed
          ? `账户列表读取失败(${errorMessage(accountsQ.error)}),先重试再开自动下单`
          : "开启自动下单时必须选择 OVH 账户(否则只通知不下单)"
      );
      return;
    }
    const payload = {
      planCode: code,
      datacenters: dcs,
      notifyAvailable,
      notifyUnavailable,
      autoOrder,
      quantity: autoOrder ? quantity : undefined,
      autoOrderAccountId: autoOrder ? autoOrderAccountId : "",
      autoPay: autoOrder ? autoPay : false,
      // 始终显式传:PUT 用 *[]string 区分「没传」和「改成空数组」,
      // 不传的话后端保留旧值,用户在界面上清空了配置却不生效
      options: chosenOptions,
    };
    const done = {
      onSuccess: () => {
        reset();
        onOpenChange(false);
      },
    };
    if (isEdit) update.mutate(payload, done);
    else create.mutate(payload, done);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v) reset();
        onOpenChange(v);
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{isEdit ? "编辑订阅" : "添加订阅"}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? "只改配置，已记录的库存状态和历史不会重置"
              : "填写需要监控的服务器型号与可选条件"}
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={submit} className="space-y-4">
          {notifyBlocked && (
            <div className="flex items-start gap-2.5 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3.5 py-2.5">
              <AlertTriangle className="w-4 h-4 text-amber-600 mt-0.5 flex-shrink-0" />
              <div className="text-xs flex-1 min-w-0">
                <div className="font-medium text-amber-900 dark:text-amber-200">
                  没有可用的通知通道
                </div>
                <div className="text-amber-800/80 dark:text-amber-200/80 mt-0.5 break-words">
                  {notifyReason || "请先在设置页配置 Telegram 或自定义 Webhook,至少一条"}
                </div>
                <Link
                  to="/settings"
                  className="inline-block mt-1 text-amber-900 dark:text-amber-200 underline underline-offset-2"
                >
                  去配置 →
                </Link>
              </div>
            </div>
          )}

          <div>
            <label className="block text-xs font-medium text-muted-foreground mb-1.5">
              服务器型号 <span className="text-destructive">*</span>
            </label>
            <Input
              value={planCode}
              onChange={(e) => setPlanCode(e.target.value)}
              placeholder="例如: 24ska01"
              autoFocus={!isEdit}
              readOnly={isEdit}
              className={isEdit ? "bg-muted text-muted-foreground cursor-not-allowed" : undefined}
            />
            {isEdit && (
              <p className="text-[11px] text-muted-foreground mt-1">
                型号不可改。要换机型请删掉这条订阅再新建
              </p>
            )}
          </div>

          <div>
            <label className="block text-xs font-medium text-muted-foreground mb-1.5">
              数据中心（可选，多个用逗号分隔）
            </label>
            <Input
              value={datacenters}
              onChange={(e) => setDatacenters(e.target.value)}
              placeholder="例如: gra,rbx,sbg 或留空监控所有"
            />
          </div>

          {/* 盯哪一套配置。
              一个型号底下常有好几套内存/存储组合，而通知和自动下单是**按配置逐套**
              触发的 —— 不限定的话「自动抢 1 台」= 每套配置在每个机房各抢 1 台。
              留空保持老行为（盯全部），所以这一块默认是收起的提示而不是必填项。 */}
          <div>
            <label className="block text-xs font-medium text-muted-foreground mb-1.5">
              只盯这套配置（可选）
            </label>
            {serversQ.isPending ? (
              <Skeleton className="h-16 rounded-xl" />
            ) : grouped ? (
              <div className="space-y-3 rounded-xl border border-border p-3.5">
                {(["cpu", "memory", "systemStorage", "storage", "bandwidth", "vrack", "other"] as OptionGroupKey[])
                  .filter((g) => (grouped[g] || []).length > 0)
                  .map((g) => (
                    <OptionGroupSection
                      key={g}
                      groupKey={g}
                      options={grouped[g]}
                      picked={picked[g] || ""}
                      defaultValueSet={defaultValueSet}
                      onPick={(v) =>
                        setPicked((prev) => ({ ...prev, [g]: prev[g] === v ? "" : v }))
                      }
                    />
                  ))}
                {chosenOptions.length > 0 && (
                  <button
                    type="button"
                    onClick={() => setPicked({})}
                    className="text-[11px] text-muted-foreground underline underline-offset-2 hover:text-foreground"
                  >
                    清空选择（改回盯全部配置）
                  </button>
                )}
              </div>
            ) : (
              // 目录里没有这个型号（冷门机型 / 目录没拉到）→ 退回手填，跟下单弹窗同一策略
              <Input
                value={extraOptions}
                onChange={(e) => setExtraOptions(e.target.value)}
                placeholder="addon planCode，逗号分隔；留空 = 盯全部配置"
              />
            )}
            <p className="text-[11px] text-muted-foreground mt-1">
              {chosenOptions.length > 0
                ? `已选 ${chosenOptions.length} 项，只有完全匹配的配置才会触发通知与自动下单`
                : "留空 = 盯该型号的全部配置。多套配置同时补货时会逐套触发"}
            </p>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <label className="flex items-center gap-2.5 cursor-pointer rounded-xl border border-border px-3.5 py-2.5 hover:bg-muted/40 transition-colors">
              <Checkbox
                checked={notifyAvailable}
                onCheckedChange={(v) => setNotifyAvailable(!!v)}
              />
              <span className="text-sm">有货时提醒</span>
            </label>
            <label className="flex items-center gap-2.5 cursor-pointer rounded-xl border border-border px-3.5 py-2.5 hover:bg-muted/40 transition-colors">
              <Checkbox
                checked={notifyUnavailable}
                onCheckedChange={(v) => setNotifyUnavailable(!!v)}
              />
              <span className="text-sm">无货时提醒</span>
            </label>
            <label className="flex items-center gap-2.5 cursor-pointer rounded-xl border border-border px-3.5 py-2.5 hover:bg-muted/40 transition-colors sm:col-span-2">
              <Checkbox checked={autoOrder} onCheckedChange={(v) => setAutoOrder(!!v)} />
              <span className="text-sm">有货时自动下单</span>
            </label>
          </div>

          {autoOrder && (
            <div className="space-y-3">
              <div>
                <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                  下单账户
                </label>
                {/* 订阅的下单账户就绑当前账户 —— 页面上再放一个选择器,
                    就会出现"用 A 账户看库存、订阅却绑到 B 账户"的错配,
                    而三区库存互不相通,触发时才发现下不了单 */}
                {accountsFailed ? (
                  <div className="flex items-start gap-2 px-3 py-2 rounded-xl border border-destructive/40 bg-destructive/5">
                    <AlertTriangle className="w-3.5 h-3.5 text-destructive flex-shrink-0 mt-0.5" />
                    <span className="text-[12px] min-w-0 break-words">
                      账户列表读取失败：{errorMessage(accountsQ.error)}
                    </span>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      className="ml-auto flex-shrink-0"
                      onClick={() => accountsQ.refetch()}
                    >
                      重试
                    </Button>
                  </div>
                ) : (
                  // 当前账户不在这里重复 —— 切换器在顶栏(手机)/侧栏(桌面)始终可见。
                  // 只保留下面那句"触发时用这个账户下单",它说的是行为不是身份。
                  null
                )}
                <p className="text-[11px] text-muted-foreground mt-1">
                  触发时用这个账户下单;关掉上面的开关 = 只通知不下单
                </p>
                {/* 提交会被拦掉的两种理由写在这里,别让用户点了才知道 */}
                {accountsFailed ? (
                  <p className="text-[11px] text-destructive mt-1">
                    账户没读出来之前不能配自动下单 —— 提交了也只会变成"只通知"
                  </p>
                ) : !accountsQ.isPending && !autoOrderAccountId ? (
                  <p className="text-[11px] text-destructive mt-1">
                    还没有可用的 OVH 账户,自动下单会被拒绝。先去设置页添加账户
                  </p>
                ) : null}
              </div>
              <div>
              <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                下单数量
              </label>
              <Input
                type="number"
                min={1}
                max={100}
                value={quantity}
                onChange={(e) => {
                  const v = Number(e.target.value);
                  if (Number.isFinite(v)) {
                    setQuantity(Math.max(1, Math.min(100, Math.floor(v))));
                  }
                }}
                placeholder="默认 1"
              />
              <p className="text-[11px] text-muted-foreground mt-1.5">
                总下单量 = 检测出的配置数 × 可用数据中心数 × 数量
              </p>
              <label className="flex items-center gap-2 mt-2 cursor-pointer text-[12px]">
                <Checkbox checked={autoPay} onCheckedChange={(v) => setAutoPay(!!v)} />
                下单成功后自动付款
              </label>
              <p className="text-[11px] text-muted-foreground mt-1">
                用 OVH 账户的默认支付方式扣款（需先在 OVH 设置好）。不勾则只下单，
                需要在订单过期前自己付款
              </p>
            </div>
            </div>
          )}

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                reset();
                onOpenChange(false);
              }}
            >
              取消
            </Button>
            <Button
              type="submit"
              disabled={create.isPending || update.isPending || notifyBlocked || notifyChecking}
              title={
                notifyBlocked
                  ? notifyReason || "没有可用的通知通道,无法添加订阅"
                  : undefined
              }
            >
              {create.isPending || update.isPending
                ? "提交中…"
                : notifyChecking
                  ? "校验通知…"
                  : isEdit
                    ? "保存修改"
                    : "确认添加"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function Stat({ label, value }: { label: string; value: number | string }) {
  return (
    <div>
      <div className="text-[11px] text-muted-foreground">{label}</div>
      <div className="text-lg font-semibold">{value}</div>
    </div>
  );
}

/**
 * 检查间隔：点一下就地改。后端合法区间 5-3600 秒，越界会被夹紧并回传真实生效值。
 *
 * current 可能是 undefined —— 状态接口挂了的时候。这时候显示 0s 是在编数字
 * （0 秒是个根本不存在的间隔），点进去编辑框还会预填 0,一保存就把一个
 * 正在跑的监控改成用户压根没打算要的值。所以未知就显示 — 且不给改。
 */
function IntervalStat({
  current,
  unknownLabel = "—",
}: {
  current?: number;
  unknownLabel?: string;
}) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(current === undefined ? "" : String(current));
  const mut = useSetMonitorInterval();

  useEffect(() => {
    if (!editing) setValue(current === undefined ? "" : String(current));
  }, [current, editing]);

  const submit = async () => {
    const n = Number(value);
    if (!Number.isFinite(n) || n <= 0) {
      toast.error("请输入正整数秒数");
      return;
    }
    await mut.mutateAsync(Math.round(n));
    setEditing(false);
  };

  if (current === undefined) {
    return (
      <div>
        <div className="text-muted-foreground text-xs">检查间隔</div>
        <div
          className="font-semibold tabular-nums text-muted-foreground"
          title="监控状态没读到,当前间隔未知 —— 先把状态读回来再改"
        >
          {unknownLabel}
        </div>
      </div>
    );
  }

  if (!editing) {
    return (
      <div>
        <div className="text-muted-foreground text-xs">检查间隔</div>
        <button
          className="font-semibold tabular-nums hover:underline"
          onClick={() => setEditing(true)}
          title="点击修改（5-3600 秒）"
        >
          {current}s
        </button>
      </div>
    );
  }
  return (
    <div>
      <div className="text-muted-foreground text-xs">检查间隔（5-3600s）</div>
      <div className="flex items-center gap-1">
        <input
          autoFocus
          type="number"
          min={5}
          max={3600}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void submit();
            if (e.key === "Escape") setEditing(false);
          }}
          className="w-20 px-2 py-0.5 border border-border rounded text-sm bg-background"
        />
        <Button size="sm" variant="ghost" onClick={() => void submit()} disabled={mut.isPending}>
          {mut.isPending ? "…" : "保存"}
        </Button>
      </div>
    </div>
  );
}
