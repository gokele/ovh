import { createFileRoute, Link } from "@tanstack/react-router";
import {
  Cloud,
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
  HelpCircle,
} from "lucide-react";
import { useEffect, useState } from "react";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { toast } from "sonner";
import { useActiveAccount } from "@/hooks/use-active-account";
import { findAccountByID, useAccounts } from "@/hooks/use-accounts";
import { AccountChip } from "@/components/common/AccountChip";
import {
  defaultSubsidiaryForEndpoint,
  subsidiariesForEndpoint,
} from "@/lib/ovh-subsidiaries";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Chip } from "@/components/common/Chip";
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
  useVPSMonitorList,
  useVPSMonitorStatus,
  useToggleVPSMonitor,
  useRemoveVPSSubscription,
  useClearVPSMonitor,
  useCreateVPSMonitorSubscription,
  useUpdateVPSSubscription,
  useVPSModels,
  type VPSModel,
  useVPSMonitorHistory,
  type VPSSubscription,
} from "@/hooks/use-vps-monitor";
import { useNotifyGate } from "@/hooks/use-notify-channels";

/** VPS 补货通知 */
export const Route = createFileRoute("/vps-monitor")({
  component: VPSMonitorPage,
});

/**
 * 兜底型号表：只在 OVH 目录拉不动（断网、被限流）时用。
 *
 * 不能只靠它：型号会**整代下架**。这份写死的 2025 代在 2026-08 已经全线退出
 * OVH 下单目录，而当时界面上只有它 —— 订阅一个停售型号，库存接口老实返回
 * "全部无货"，永远不跳变也就永远不通知，症状和"这机器确实抢手"一模一样。
 */
const FALLBACK_MODELS = [
  { planCode: "vps-2027-model1", name: "VPS-1 2027", generation: "2027" },
  { planCode: "vps-2027-model2", name: "VPS-2 2027", generation: "2027" },
  { planCode: "vps-2027-model3", name: "VPS-3 2027", generation: "2027" },
  { planCode: "vps-2027-model4", name: "VPS-4 2027", generation: "2027" },
];

/** 型号的显示名。目录还没加载时退回 planCode —— 比显示一个猜的名字诚实 */
function modelLabel(code: string, models?: VPSModel[]): string {
  return models?.find((m) => m.planCode === code)?.name || code;
}

function VPSMonitorPage() {
  const list = useVPSMonitorList();
  const status = useVPSMonitorStatus();
  const toggle = useToggleVPSMonitor();
  const remove = useRemoveVPSSubscription();
  const clear = useClearVPSMonitor();
  const [confirmClear, setConfirmClear] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState<VPSSubscription | null>(null);
  const [openAdd, setOpenAdd] = useState(false);
  // 正在编辑的订阅。null = 新增模式,两种模式共用同一个对话框
  const [editing, setEditing] = useState<VPSSubscription | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);

  const subs = list.data || [];
  // 监控状态是三态:还在读 / 读失败 / 真数据。后两者绝不能混成一个 `!!status.data?.running`。
  // 状态接口挂了 ≠ 监控停了 —— 后端的 VPS 监控照跑,只是这一次 HTTP 没问到。
  // 而这一页比服务器监控更危险:running 猜成 false 之后,右上角那颗按钮会跟着
  // 变成「启动监控」,用户手一点,发出去的其实是对一个**正在跑**的监控做 start,
  // 后果不是"没反应",是把它按用户没打算的方式重启/弄停。
  // 所以未知状态下按钮不做启停,只让人先把状态读回来;数字也一律显示 —。
  const running = !!status.data?.running;
  const statusUnknown = status.isPending || status.isError;
  /** 状态里的数字:0 是有含义的真值（真的一条订阅都没有），不能拿它冒充"没读到" */
  const statNum = (v: number | undefined): number | string =>
    status.isPending ? "…" : status.isError || v === undefined ? "—" : v;

  return (
    <div className="space-y-3 sm:space-y-6">
      <PageHeader
        icon={Cloud}
        title="VPS 补货通知"
        description="选择 VPS 型号，自动监控所有数据中心的库存变化"
        action={
          <div className="flex gap-2 flex-wrap">
            <Button variant="outline" onClick={() => list.refetch()} disabled={list.isFetching}>
              <RefreshCw className={`w-4 h-4 ${list.isFetching ? "animate-spin" : ""}`} />
              刷新
            </Button>
            <Button onClick={() => setOpenAdd(true)}>
              <Plus className="w-4 h-4" />
              添加订阅
            </Button>
            {/* 状态未知时这颗按钮只负责"把状态读回来",不做启停:
                它的文案和它实际发的请求都是从 running 猜出来的,猜错就是误操作 */}
            <Button
              variant={statusUnknown || !running ? "outline" : "destructive"}
              onClick={() => (statusUnknown ? status.refetch() : toggle.mutate(running))}
              disabled={toggle.isPending || (statusUnknown && status.isFetching)}
              title={
                status.isError
                  ? `监控状态读取失败：${errorMessage(status.error)}。读不到状态就不知道该启还是该停,先重试`
                  : status.isPending
                    ? "正在读取监控状态…"
                    : undefined
              }
            >
              {statusUnknown ? (
                <RefreshCw className={`w-4 h-4 ${status.isFetching ? "animate-spin" : ""}`} />
              ) : running ? (
                <BellOff className="w-4 h-4" />
              ) : (
                <Bell className="w-4 h-4" />
              )}
              {status.isPending ? "读取状态…" : status.isError ? "状态未知 · 重试" : running ? "停止监控" : "启动监控"}
            </Button>
            <Button
              variant="outline"
              onClick={() => setConfirmClear(true)}
              disabled={subs.length === 0}
            >
              <Trash2 className="w-4 h-4" />
              清空
            </Button>
          </div>
        }
      />

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
              <div className="text-sm font-semibold">VPS 监控状态</div>
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
            <Stat
              label="检查间隔"
              value={
                statusUnknown
                  ? statNum(status.data?.check_interval)
                  : `${status.data?.check_interval ?? 0}s`
              }
            />
          </div>
        </CardContent>
      </Card>

      {list.isPending ? (
        <div className="space-y-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-24 rounded-2xl" />
          ))}
        </div>
      ) : list.isError ? (
        /* 列表没读到 ≠ 一条订阅都没有。以前直接掉进"暂无 VPS 订阅",
           而订阅其实还在后端跑着 —— 用户看见空列表会照着再加一遍,
           同一个型号被订两次,补货时就通知两次、自动下单两次。 */
        <Card>
          <LoadFailed
            icon={Cloud}
            title="VPS 订阅列表读取失败"
            error={list.error}
            onRetry={() => list.refetch()}
          />
        </Card>
      ) : subs.length === 0 ? (
        <Card>
          <EmptyState
            icon={Cloud}
            title="暂无 VPS 订阅"
            description='点击"添加订阅"按钮，选择 VPS 型号开始监控'
          />
        </Card>
      ) : (
        <div className="space-y-3">
          {subs.map((s) => (
            <VPSRow
              key={s.id}
              sub={s}
              expanded={expanded === s.id}
              onToggleExpand={() => setExpanded((c) => (c === s.id ? null : s.id))}
              onEdit={() => {
                setEditing(s);
                setOpenAdd(true);
              }}
              onDelete={() => setConfirmRemove(s)}
            />
          ))}
        </div>
      )}

      {/* 添加订阅 Dialog */}
      <AddVPSDialog
        open={openAdd}
        editing={editing}
        onOpenChange={(v) => {
          setOpenAdd(v);
          if (!v) setEditing(null);
        }}
      />

      {/* 删除确认 */}
      <Dialog open={!!confirmRemove} onOpenChange={(v) => !v && setConfirmRemove(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>取消订阅</DialogTitle>
            <DialogDescription>
              确定要取消订阅{" "}
              <span className="font-mono">{confirmRemove && modelLabel(confirmRemove.planCode)}</span>{" "}
              吗？
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmRemove(null)}>
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                if (confirmRemove) remove.mutate(confirmRemove.id);
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
            <DialogTitle>确认清空所有 VPS 订阅？</DialogTitle>
            <DialogDescription>此操作不可撤销。</DialogDescription>
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

/* -------------------------------- 行 / 历史 -------------------------------- */

function VPSRow({
  sub,
  expanded,
  onToggleExpand,
  onEdit,
  onDelete,
}: {
  sub: VPSSubscription;
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
              <span className="font-semibold text-sm">{modelLabel(sub.planCode)}</span>
              <span className="font-mono text-[11px] text-muted-foreground">{sub.planCode}</span>
              <Chip tone="default">{sub.ovhSubsidiary}</Chip>
              {sub.retired && (
                <Chip tone="danger" title="OVH 已经不卖这个型号了，这条订阅永远不会有货">
                  已停售
                </Chip>
              )}
              {sub.autoOrder && sub.autoOrderAccountId && (
                <Chip tone="solid" title={sub.autoPay ? "下单成功后自动付款" : "只下单,需自己付款"}>
                  自动下单{sub.quantity && sub.quantity > 1 ? ` ×${sub.quantity}` : ""}
                  {sub.autoPay ? " · 自动付款" : ""}
                </Chip>
              )}
            </div>
            <p className="text-xs text-muted-foreground mb-1.5">
              {sub.datacenters.length > 0
                ? `监控数据中心: ${sub.datacenters.join(", ")}`
                : "监控所有数据中心"}
            </p>
            <div className="flex gap-1.5 flex-wrap items-center">
              {sub.monitorLinux && <Chip tone="info">Linux</Chip>}
              {sub.monitorWindows && <Chip tone="info">Windows</Chip>}
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
              onClick={onToggleExpand}
              aria-label="查看历史"
            >
              {expanded ? (
                <ChevronUp className="w-4 h-4" />
              ) : (
                <HistoryIcon className="w-4 h-4" />
              )}
            </Button>
            <Button variant="ghost" size="icon" onClick={onEdit} aria-label="编辑订阅" title="改站点 / 机房 / 提醒方式">
              <Pencil className="w-4 h-4" />
            </Button>
            <Button variant="ghost" size="icon" onClick={onDelete} aria-label="删除">
              <X className="w-4 h-4" />
            </Button>
          </div>
        </div>

        {expanded && (
          <div className="mt-4 pt-4 border-t border-border">
            <VPSHistoryPanel id={sub.id} />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function VPSHistoryPanel({ id }: { id: string }) {
  const history = useVPSMonitorHistory(id);

  if (history.isPending) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-10 rounded-xl" />
        ))}
      </div>
    );
  }

  // "暂无历史记录"是一句结论:这个型号从订阅到现在一次都没补过货,
  // 用户会据此判断"别等了,换一个型号"。读失败时说这句话就是在给假消息。
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
                  <span className="font-medium">{e.datacenter}</span>
                  <Chip tone={e.changeType === "available" ? "success" : "danger"}>
                    {e.changeType === "available" ? "有货" : "无货"}
                  </Chip>
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

/* ---------------------------- 添加 VPS Dialog ---------------------------- */

/**
 * 新增 / 编辑 VPS 订阅共用一个对话框。
 * 编辑模式下型号只读（型号是订阅的身份），但**站点可以改** ——
 * 选错站点是这里最常见的配置错误，而它的症状是"永远无货"，很难自己看出来。
 */
function AddVPSDialog({
  open,
  onOpenChange,
  editing,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  editing?: VPSSubscription | null;
}) {
  const create = useCreateVPSMonitorSubscription();
  const update = useUpdateVPSSubscription();
  const isEdit = !!editing;
  // 门禁看的是「所有通道」,不是只看 Telegram —— 只配 webhook 的用户也该能订阅
  const [notifyBlocked, notifyReason, notifyChecking] = useNotifyGate();
  const [vpsModel, setVpsModel] = useState("");
  const [ovhSubsidiary, setOvhSubsidiary] = useState("");
  const [datacenters, setDatacenters] = useState("");
  const [monitorLinux, setMonitorLinux] = useState(true);
  const [monitorWindows, setMonitorWindows] = useState(true);
  const [notifyAvailable, setNotifyAvailable] = useState(true);
  const [notifyUnavailable, setNotifyUnavailable] = useState(false);
  const [autoOrder, setAutoOrder] = useState(false);
  const [quantity, setQuantity] = useState(1);
  // 空 = 用 OVH 默认镜像。VPS 的系统是下单时就要定的配置项,不是买完再装
  const [os, setOs] = useState("");
  // 默认不自动付款:自动扣钱必须显式打开
  const [autoPay, setAutoPay] = useState(false);
  // 订阅的下单账户 = 左侧菜单栏(手机端在顶栏)的全局账户,不再单独选
  const [globalAccountId] = useActiveAccount();
  const accountsQ = useAccounts();
  const allAccounts = accountsQ.data;
  const autoOrderAccountId =
    globalAccountId || allAccounts?.find((a) => a.isDefault)?.id || allAccounts?.[0]?.id || "";
  const activeAcc = findAccountByID(allAccounts, autoOrderAccountId);
  // 账户列表读失败 ≠ 一个账户都没配。两种情况以前都显示「未选择账户」,
  // 然后自动下单被静默拦掉 —— 用户看到的是一条没有理由的死路:
  // 去账户页明明有账户,回来还是"未选择"。而且这一页的站点列表也是按
  // activeAcc.endpoint 算的,账户没读到时那份列表同样不可信,得一起说清楚。
  const accountsFailed = accountsQ.isError && !activeAcc;
  // 订阅的站点必须和当前账户同区:三个站点的库存和购物车互不相通,
  // 拿美区账户订阅欧区子公司,补货时才发现根本买不了。不该给的选项就别给。
  const allowedSubsidiaries = subsidiariesForEndpoint(activeAcc?.endpoint);
  // 型号来自 OVH 实时目录(按站点),不写死 —— 型号会整代下架
  const models = useVPSModels(ovhSubsidiary);
  const modelList: VPSModel[] = models.data?.models?.length
    ? models.data.models
    : (FALLBACK_MODELS as VPSModel[]);

  const reset = () => {
    setVpsModel(modelList[0]?.planCode || "");
    setOvhSubsidiary(defaultSubsidiaryForEndpoint(activeAcc?.endpoint));
    setDatacenters("");
    setMonitorLinux(true);
    setMonitorWindows(true);
    setNotifyAvailable(true);
    setNotifyUnavailable(false);
    setAutoOrder(false);
    setQuantity(1);
    setOs("");
    setAutoPay(false);
  };

  // 打开时按 editing 重灌表单;带上 open 依赖,免得上次改了一半的内容留到下次
  useEffect(() => {
    if (!open) return;
    if (editing) {
      setVpsModel(editing.planCode);
      setOvhSubsidiary(editing.ovhSubsidiary);
      setDatacenters((editing.datacenters || []).join(", "));
      setMonitorLinux(editing.monitorLinux);
      setMonitorWindows(editing.monitorWindows);
      setNotifyAvailable(editing.notifyAvailable);
      setNotifyUnavailable(editing.notifyUnavailable);
      setAutoOrder(!!editing.autoOrder);
      setQuantity(editing.quantity && editing.quantity > 0 ? editing.quantity : 1);
      setOs(editing.os || "");
      setAutoPay(!!editing.autoPay);
    } else {
      reset();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, editing]);

  // 目录是异步到的:先前选中的型号可能不在新站点的在售列表里
  // (换了子公司,或兜底列表被真目录替换)。留着一个买不到的型号,
  // 用户要到下单那一刻才知道。
  const selected = modelList.find((m) => m.planCode === vpsModel);
  useEffect(() => {
    if (!open || isEdit) return;
    if (!modelList.length) return;
    if (!modelList.some((m) => m.planCode === vpsModel)) {
      setVpsModel(modelList[0].planCode);
    }
  }, [open, isEdit, modelList, vpsModel]);

  // 换了型号,原来选的系统可能不在新型号的可选列表里
  useEffect(() => {
    if (!os || !selected?.osChoices?.length) return;
    if (!selected.osChoices.includes(os)) setOs("");
  }, [selected, os]);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const dcs = datacenters
      .split(",")
      .map((d) => d.trim())
      .filter(Boolean);

    if (autoOrder && !autoOrderAccountId) {
      // 读失败和"真的没账户"要给不同的话:前者该重试,后者该去加账户
      toast.error(
        accountsFailed
          ? `账户列表读取失败(${errorMessage(accountsQ.error)}),先重试再开自动下单`
          : "开启自动下单时必须选 OVH 账户"
      );
      return;
    }
    const payload = {
      planCode: vpsModel,
      ovhSubsidiary,
      datacenters: dcs,
      monitorLinux,
      monitorWindows,
      notifyAvailable,
      notifyUnavailable,
      autoOrder,
      quantity: autoOrder ? quantity : undefined,
      os: autoOrder ? os : "",
      autoOrderAccountId: autoOrder ? autoOrderAccountId : "",
      autoPay: autoOrder ? autoPay : false,
    };
    const done = {
      onSuccess: () => {
        reset();
        onOpenChange(false);
      },
    };
    if (isEdit) update.mutate({ ...payload, id: editing!.id }, done);
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
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{isEdit ? "编辑 VPS 订阅" : "添加 VPS 订阅"}</DialogTitle>
          <DialogDescription>
            {isEdit ? "只改配置，历史记录不会重置" : "选择 VPS 型号与可选条件"}
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

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                VPS 型号 <span className="text-destructive">*</span>
              </label>
              <Select value={vpsModel} onValueChange={setVpsModel} disabled={isEdit}>
                <SelectTrigger>
                  <SelectValue placeholder={models.isPending ? "读取 OVH 目录…" : "选择型号"} />
                </SelectTrigger>
                <SelectContent>
                  {modelList.map((m) => (
                    <SelectItem key={m.planCode} value={m.planCode}>
                      {m.name}
                      {m.location ? ` · ${m.location}` : ""}
                      {m.price ? ` · ${m.price}/月` : ""}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-[11px] text-muted-foreground mt-1">
                {models.isError
                  ? "读不到 OVH 目录，下面是兜底列表，可能不是最新在售型号"
                  : `${ovhSubsidiary} 当前在售 ${modelList.length} 款`}
              </p>
            </div>
            <div>
              <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                OVH 子公司
              </label>
              <Select value={ovhSubsidiary} onValueChange={setOvhSubsidiary}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {allowedSubsidiaries.map((s) => (
                    <SelectItem key={s.code} value={s.code}>
                      {s.code} · {s.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {/* 可选站点是按当前账户的 endpoint 算的。账户没读到时这份列表
                  只是默认值,不是"你这个账户能用的站点" —— 选错站点的症状是
                  永远无货,必须提前说,不能让它看起来像已经按账户过滤过了 */}
              {accountsFailed && (
                <p className="text-[11px] text-destructive mt-1">
                  账户读取失败,下面的站点没按账户过滤,可能选到当前账户买不了的站点
                </p>
              )}
            </div>
          </div>

          <div>
            <label className="block text-xs font-medium text-muted-foreground mb-1.5">
              数据中心代码（可选，多个用逗号分隔）
            </label>
            <Input
              value={datacenters}
              onChange={(e) => setDatacenters(e.target.value)}
              placeholder="留空 = 监控所有机房"
            />
            {selected?.datacenters?.length ? (
              <p className="text-[11px] text-muted-foreground mt-1 break-all">
                {selected.name} 在 {ovhSubsidiary} 可选：{selected.datacenters.join("、")}
              </p>
            ) : null}
          </div>

          <div>
            <p className="text-xs font-medium text-muted-foreground mb-2">监控系统</p>
            <div className="grid grid-cols-2 gap-3">
              <label className="flex items-center gap-2.5 cursor-pointer rounded-xl border border-border px-3.5 py-2.5 hover:bg-muted/40 transition-colors">
                <Checkbox
                  checked={monitorLinux}
                  onCheckedChange={(v) => setMonitorLinux(!!v)}
                />
                <span className="text-sm">Linux</span>
              </label>
              <label className="flex items-center gap-2.5 cursor-pointer rounded-xl border border-border px-3.5 py-2.5 hover:bg-muted/40 transition-colors">
                <Checkbox
                  checked={monitorWindows}
                  onCheckedChange={(v) => setMonitorWindows(!!v)}
                />
                <span className="text-sm">Windows</span>
              </label>
            </div>
          </div>

          <div>
            <p className="text-xs font-medium text-muted-foreground mb-2">通知与下单</p>
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
                  <div className="flex items-center gap-2 px-3 py-2 rounded-xl border border-border bg-secondary/30">
                    <User className="w-3.5 h-3.5 text-muted-foreground" />
                    <span className="text-[13px] font-medium">
                      {accountsQ.isPending ? "读取账户…" : activeAcc?.name || "未选择账户"}
                    </span>
                    {activeAcc && <span className="text-[11px] text-muted-foreground">{activeAcc.zone}</span>}
                    <span className="ml-auto text-[10px] text-muted-foreground">在左侧菜单切换</span>
                  </div>
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
                />
              </div>
              <div className="sm:col-span-2">
                <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                  安装系统
                </label>
                <Select value={os || "__default__"} onValueChange={(v) => setOs(v === "__default__" ? "" : v)}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__default__">用 OVH 默认镜像</SelectItem>
                    {(selected?.osChoices || []).map((o) => (
                      <SelectItem key={o} value={o}>
                        {o}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className="text-[11px] text-muted-foreground mt-1">
                  VPS 和独服不同：系统是**下单时**就要定的，买完再换要重装。
                  不确定就留默认
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
              disabled={create.isPending || notifyBlocked || notifyChecking}
              title={
                notifyBlocked
                  ? notifyReason || "没有可用的通知通道,无法添加订阅"
                  : undefined
              }
            >
              {create.isPending ? "提交中…" : notifyChecking ? "校验通知…" : "确认添加"}
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
