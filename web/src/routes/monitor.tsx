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
  User,
} from "lucide-react";
import { useEffect, useState } from "react";
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

  return (
    <div className="space-y-6">
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
              {status.data?.running ? (
                <Bell className="w-5 h-5 text-success" />
              ) : (
                <BellOff className="w-5 h-5 text-muted-foreground" />
              )}
            </div>
            <div>
              <div className="text-sm font-semibold">监控状态</div>
              <div className="text-xs text-muted-foreground inline-flex items-center gap-1.5">
                <StatusDot
                  tone={status.data?.running ? "success" : "muted"}
                  pulse={status.data?.running}
                  size="xs"
                />
                {status.data?.running ? "运行中" : "已停止"}
              </div>
            </div>
          </div>
          <div className="flex gap-6 text-sm">
            <Stat label="订阅数" value={status.data?.subscriptions_count ?? 0} />
            <IntervalStat current={status.data?.check_interval ?? 0} />
            <Stat label="已知服务器" value={status.data?.known_servers_count ?? 0} />
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
              {sub.notifyAvailable && <Chip tone="success">有货提醒</Chip>}
              {sub.notifyUnavailable && <Chip tone="warning">无货提醒</Chip>}
              {sub.autoOrder && sub.autoOrderAccountId ? (
                <>
                  <Chip tone="solid">
                    自动下单
                    {sub.quantity && sub.quantity > 1 ? ` ×${sub.quantity}` : ""}
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
  // 订阅的下单账户 = 左侧菜单栏的全局账户,不再单独选
  const [globalAccountId] = useActiveAccount();
  const { data: allAccounts } = useAccounts();
  const autoOrderAccountId =
    globalAccountId || allAccounts?.find((a) => a.isDefault)?.id || allAccounts?.[0]?.id || "";
  const activeAcc = findAccountByID(allAccounts, autoOrderAccountId);

  const reset = () => {
    setPlanCode("");
    setDatacenters("");
    setNotifyAvailable(true);
    setNotifyUnavailable(false);
    setAutoOrder(false);
    setQuantity(1);
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
    } else {
      reset();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, editing]);

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
      toast.error("开启自动下单时必须选择 OVH 账户(否则只通知不下单)");
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
                <div className="flex items-center gap-2 px-3 py-2 rounded-xl border border-border bg-secondary/30">
                  <User className="w-3.5 h-3.5 text-muted-foreground" />
                  <span className="text-[13px] font-medium">{activeAcc?.name || "未选择账户"}</span>
                  {activeAcc && <span className="text-[11px] text-muted-foreground">{activeAcc.zone}</span>}
                  <span className="ml-auto text-[10px] text-muted-foreground">在左侧菜单切换</span>
                </div>
                <p className="text-[11px] text-muted-foreground mt-1">
                  触发时用这个账户下单;关掉上面的开关 = 只通知不下单
                </p>
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
              title={notifyBlocked ? "没有可用的通知通道,无法添加订阅" : undefined}
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

/** 检查间隔：点一下就地改。后端合法区间 5-3600 秒，越界会被夹紧并回传真实生效值。 */
function IntervalStat({ current }: { current: number }) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(String(current));
  const mut = useSetMonitorInterval();

  useEffect(() => {
    if (!editing) setValue(String(current));
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
