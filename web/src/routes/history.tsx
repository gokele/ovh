import { createFileRoute } from "@tanstack/react-router";
import { Clock, RefreshCw, Trash2, Search, ExternalLink, AlertCircle, Hourglass } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Chip } from "@/components/common/Chip";
import { AccountChip } from "@/components/common/AccountChip";
import { TimingChip } from "@/components/common/TimingChip";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed } from "@/components/common/LoadFailed";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  useHistory,
  useClearHistory,
  useRefreshOrderStatus,
  type PurchaseHistory,
} from "@/hooks/use-history";

/**
 * 订单支付状态 → 标签。取值是 OVH 的 billing.order.OrderStatusEnum,三区一致。
 * 这才是用户真正关心的:「下单成功」只说明订单建了,付没付、过没过期,看这里。
 */
function orderStatusView(item: PurchaseHistory): {
  label: string;
  tone: "success" | "warning" | "danger" | "info" | "default";
  paid: boolean;
  closed: boolean;
  title: string;
} {
  switch (item.orderStatus) {
    case "notPaid":
      return { label: "待付款", tone: "warning", paid: false, closed: false, title: "订单已创建,尚未付款;倒计时结束前未付款会作废" };
    case "checking":
      return { label: "付款核验中", tone: "info", paid: true, closed: false, title: "OVH 已收到付款,正在核验" };
    case "delivering":
      return { label: "已付款·交付中", tone: "success", paid: true, closed: false, title: "已付款,OVH 正在交付服务器" };
    case "delivered":
      return { label: "已付款·已交付", tone: "success", paid: true, closed: true, title: "已付款并交付" };
    case "cancelling":
      return { label: "取消中", tone: "danger", paid: false, closed: true, title: "订单正在取消" };
    case "cancelled":
      return { label: "已取消", tone: "danger", paid: false, closed: true, title: "订单已取消(过期未付款也会走到这里)" };
    case "documentsRequested":
      return { label: "需补材料", tone: "warning", paid: false, closed: false, title: "OVH 要求补充证件/材料后才处理" };
    case "unknown":
      return { label: "状态未知", tone: "default", paid: false, closed: false, title: "OVH 返回 unknown" };
    default:
      return { label: "状态未查到", tone: "default", paid: false, closed: false, title: "还没从 OVH 读到订单状态,点「刷新」再试" };
  }
}
import { CURRENCY_UNKNOWN_HINT } from "@/lib/money";

/** 抢购历史：表格 + 搜索 + 状态过滤 */
export const Route = createFileRoute("/history")({
  component: HistoryPage,
});

/**
 * 成交价 + 币种。
 *
 * 币种缺失时只显示金额并在 title 里说明,绝不补 "EUR":后端(internal/purchase/purchase.go)
 * 现在拿不到 currencyCode 就如实留空,而币种是按子公司定的 ——
 * 实测目录 locale.currencyCode:IE=EUR / CA=QC=CAD / US=WE=WS=USD / SG=SGD / AU=AUD。
 * 前端再兜底成欧元,美区/加区的订单会被标成 €,用户按错的币种对账。
 */
function HistoryPrice({ item, strike }: { item: PurchaseHistory; strike: boolean }) {
  const value = item.price?.withTax;
  if (value == null) return <span className="text-muted-foreground">—</span>;
  const currency = (item.price?.currencyCode || "").trim();
  return (
    <span
      className={`font-mono font-medium text-success ${strike ? "line-through" : ""}`}
      title={currency ? undefined : CURRENCY_UNKNOWN_HINT}
    >
      {value}
      {currency ? ` ${currency}` : <span className="text-muted-foreground"> (币种未知)</span>}
    </span>
  );
}

/** 订单有效期 15 天，未提供 expirationTime 时用 purchaseTime + 15d 兜底 */
const ORDER_VALIDITY_MS = 15 * 24 * 60 * 60 * 1000;

/** 把毫秒倒计时格式化为 `2天5时12分` / `12分` / `已过期` */
function formatCountdown(remainingMs: number): string {
  if (remainingMs <= 0) return "已过期";
  const totalMinutes = Math.floor(remainingMs / 60_000);
  const days = Math.floor(totalMinutes / (24 * 60));
  const hours = Math.floor((totalMinutes % (24 * 60)) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return `${days}天${hours}时${minutes}分`;
  if (hours > 0) return `${hours}时${minutes}分`;
  return `${minutes}分`;
}

function getExpirationMs(item: PurchaseHistory): number {
  if (item.expirationTime) return new Date(item.expirationTime).getTime();
  return new Date(item.purchaseTime).getTime() + ORDER_VALIDITY_MS;
}

function HistoryPage() {
  const list = useHistory();
  const clear = useClearHistory();
  const refreshStatus = useRefreshOrderStatus();
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<"all" | "success" | "failed">("all");
  const [confirmClear, setConfirmClear] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  // 每分钟刷新一次 now，让所有行的倒计时同步推进
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(id);
  }, []);

  const items = list.data || [];
  const filtered = useMemo(() => {
    const s = search.trim().toLowerCase();
    return items.filter((i) => {
      if (statusFilter !== "all" && i.status !== statusFilter) return false;
      if (s && !`${i.planCode} ${i.datacenter} ${i.orderId || ""}`.toLowerCase().includes(s)) return false;
      return true;
    });
  }, [items, search, statusFilter]);

  return (
    <div className="space-y-3 sm:space-y-6">
      <PageHeader
        icon={Clock}
        title="抢购历史"
        description="查看服务器购买历史记录"
        action={
          <div className="flex flex-wrap justify-end gap-2">
            <Button
              variant="outline"
              onClick={() => refreshStatus.mutate()}
              disabled={list.isFetching || refreshStatus.isPending}
              title="向 OVH 查询未到终态订单的支付状态,然后重载列表"
            >
              <RefreshCw
                className={`w-4 h-4 ${list.isFetching || refreshStatus.isPending ? "animate-spin" : ""}`}
              />
              刷新状态
            </Button>
            <Button variant="outline" onClick={() => setConfirmClear(true)} disabled={items.length === 0}>
              <Trash2 className="w-4 h-4" />
              清空
            </Button>
          </div>
        }
      />

      <Card>
        <CardContent className="p-3 sm:p-5">
          {/* 手机端两个控件并排:搜索框和状态下拉各占一整行时白吃 ~70px,
              而「所有状态」这种下拉本来就不需要整行宽 */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 sm:gap-3">
            <div className="relative">
              <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
              <Input
                placeholder="搜索型号 / 机房 / 订单号..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-9 rounded-full"
              />
            </div>
            <Select value={statusFilter} onValueChange={(v) => setStatusFilter(v as any)}>
              <SelectTrigger className="rounded-full">
                <SelectValue placeholder="所有状态" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">所有状态</SelectItem>
                <SelectItem value="success">成功</SelectItem>
                <SelectItem value="failed">失败</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </CardContent>
      </Card>

      {list.isPending ? (
        <Card>
          <CardContent className="p-4 space-y-2">
            {Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-16 rounded-xl" />)}
          </CardContent>
        </Card>
      ) : list.isError ? (
        /* 这页管的是订单的付款倒计时。请求挂了还画「没有匹配的订单」,用户会以为自己压根没下过单,
           于是不去付款 —— 未付款订单 15 天到点自动作废,钱和机器一起没了。全站误导里就数这一条
           后果最实在,所以失败态必须自己占一支,并且要把"这不是说你没有订单"写在脸上。 */
        <Card>
          <LoadFailed
            icon={Clock}
            title="抢购历史读取失败 —— 不是「你没有订单」,是我们没读到"
            error={list.error}
            onRetry={() => list.refetch()}
          />
          <p className="px-6 pb-5 text-[11px] text-muted-foreground text-center">
            未付款的订单仍在走 15 天倒计时,别把这片空白当成"没有订单"。
            请重试,或直接去 OVH 管理面板确认待付款的订单。
          </p>
        </Card>
      ) : filtered.length === 0 ? (
        <Card>
          <EmptyState icon={Clock} title="没有匹配的订单" />
        </Card>
      ) : (
        <>
          {/* 桌面 / 平板:横向表格 */}
          <Card className="hidden md:block overflow-x-auto">
            <table className="w-full min-w-[760px]">
              <thead>
                <tr className="text-left text-[11px] font-medium text-muted-foreground border-b border-border">
                  <th className="px-4 py-3">型号</th>
                  <th className="px-4 py-3">机房</th>
                  <th className="px-4 py-3">配置</th>
                  <th className="px-4 py-3">价格</th>
                  <th className="px-4 py-3">状态</th>
                  <th className="px-4 py-3">时间</th>
                  <th className="px-4 py-3" title="付款窗口:下单不会自动扣款,倒计时结束前未付款订单作废">
                付款剩余
              </th>
                  <th className="px-4 py-3">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {filtered.map((item) => <HistoryRow key={item.id} item={item} now={now} />)}
              </tbody>
            </table>
          </Card>

          {/* 手机:卡片堆叠,每条订单一张卡 */}
          <div className="md:hidden space-y-2">
            {filtered.map((item) => <HistoryCard key={item.id} item={item} now={now} />)}
          </div>
        </>
      )}

      <Dialog open={confirmClear} onOpenChange={setConfirmClear}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认清空所有历史？</DialogTitle>
            <DialogDescription>所有抢购历史将被删除，此操作不可撤销。</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmClear(false)}>取消</Button>
            <Button variant="destructive" onClick={() => { clear.mutate(); setConfirmClear(false); }}>
              确认清空
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function HistoryRow({ item, now }: { item: PurchaseHistory; now: number }) {
  const st = orderStatusView(item);
  // 倒计时是"付款窗口":付了、取消了、交付了都不再显示
  const showCountdown = item.status === "success" && !!item.orderId && !st.paid && !st.closed;
  const remainingMs = showCountdown ? getExpirationMs(item) - now : 0;
  const isExpired = showCountdown && remainingMs <= 0;
  // 24 小时内进入告警色
  const isUrgent = showCountdown && !isExpired && remainingMs < 24 * 60 * 60 * 1000;
  return (
    <tr className={`text-[13px] hover:bg-muted ${isExpired ? "opacity-60" : ""}`}>
      <td className={`px-4 py-3 font-mono font-semibold ${isExpired ? "line-through" : ""}`}>
        <div className="flex items-center gap-2 flex-wrap">
          {item.planCode}
          <AccountChip accountId={item.accountId} />
          <TimingChip totalMs={item.totalMs} phases={item.timing} />
        </div>
      </td>
      <td className={`px-4 py-3 ${isExpired ? "line-through" : ""}`}>{item.datacenter.toUpperCase()}</td>
      <td className={`px-4 py-3 text-muted-foreground max-w-[200px] truncate ${isExpired ? "line-through" : ""}`}>
        {item.options && item.options.length > 0 ? item.options.join(", ") : "默认配置"}
      </td>
      <td className="px-4 py-3">
        <HistoryPrice item={item} strike={isExpired} />
      </td>
      <td className="px-4 py-3">
        {item.status === "success" ? (
          <Chip tone={st.tone} title={st.title}>
            {st.label}
          </Chip>
        ) : (
          <Chip tone="danger">失败</Chip>
        )}
      </td>
      <td className="px-4 py-3 text-[11px] text-muted-foreground font-mono whitespace-nowrap">
        {new Date(item.purchaseTime).toLocaleString("zh-CN", {
          month: "2-digit",
          day: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
        })}
      </td>
      <td className="px-4 py-3 whitespace-nowrap">
        {showCountdown ? (
          <Chip tone={isExpired ? "danger" : isUrgent ? "warning" : "info"}>
            <Hourglass className="w-3 h-3" />
            {formatCountdown(remainingMs)}
          </Chip>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </td>
      <td className="px-4 py-3">
        {item.status === "success" && item.orderUrl ? (
          <a
            href={item.orderUrl}
            target="_blank"
            rel="noopener noreferrer"
            aria-disabled={isExpired}
            className={`inline-flex items-center gap-1 text-foreground hover:underline text-[12px] ${
              isExpired ? "pointer-events-none opacity-50" : ""
            }`}
          >
            <ExternalLink className="w-3 h-3" />
            订单
          </a>
        ) : item.status === "failed" && item.errorMessage ? (
          <button
            type="button"
            onClick={() => toast.info(item.errorMessage)}
            className="inline-flex items-center gap-1 text-destructive hover:underline text-[12px]"
          >
            <AlertCircle className="w-3 h-3" />
            错误
          </button>
        ) : "—"}
      </td>
    </tr>
  );
}

/** 手机端的订单卡片渲染。跟 HistoryRow 字段一一对应,但堆叠成卡片。 */
function HistoryCard({ item, now }: { item: PurchaseHistory; now: number }) {
  const st = orderStatusView(item);
  const showCountdown = item.status === "success" && !!item.orderId && !st.paid && !st.closed;
  const remainingMs = showCountdown ? getExpirationMs(item) - now : 0;
  const isExpired = showCountdown && remainingMs <= 0;
  const isUrgent = showCountdown && !isExpired && remainingMs < 24 * 60 * 60 * 1000;
  return (
    <Card className={isExpired ? "opacity-60" : ""}>
      <CardContent className="p-3 space-y-2">
        <div className="flex items-start justify-between gap-2 flex-wrap">
          <div className="flex items-center gap-2 flex-wrap min-w-0">
            <span className={`font-mono font-semibold text-[13px] ${isExpired ? "line-through" : ""}`}>{item.planCode}</span>
            <AccountChip accountId={item.accountId} />
            <Chip tone="default" className="text-[10px]">{item.datacenter.toUpperCase()}</Chip>
            <TimingChip totalMs={item.totalMs} phases={item.timing} />
          </div>
          {item.status === "success" ? (
            <Chip tone={st.tone} title={st.title}>
            {st.label}
          </Chip>
          ) : (
            <Chip tone="danger">失败</Chip>
          )}
        </div>
        <div className={`text-[11px] text-muted-foreground break-all ${isExpired ? "line-through" : ""}`}>
          {item.options && item.options.length > 0 ? item.options.join(", ") : "默认配置"}
        </div>
        <div className="flex items-center justify-between gap-2 text-[11px]">
          <span className="text-muted-foreground font-mono">
            {new Date(item.purchaseTime).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })}
          </span>
          {item.price?.withTax != null ? <HistoryPrice item={item} strike={isExpired} /> : null}
        </div>
        <div className="flex items-center justify-between gap-2">
          {showCountdown ? (
            <Chip tone={isExpired ? "danger" : isUrgent ? "warning" : "info"}>
              <Hourglass className="w-3 h-3" />
              {formatCountdown(remainingMs)}
            </Chip>
          ) : <span />}
          {item.status === "success" && item.orderUrl ? (
            <a
              href={item.orderUrl}
              target="_blank"
              rel="noopener noreferrer"
              className={`inline-flex items-center gap-1 text-foreground hover:underline text-[12px] ${isExpired ? "pointer-events-none opacity-50" : ""}`}
            >
              <ExternalLink className="w-3 h-3" />
              订单
            </a>
          ) : item.status === "failed" && item.errorMessage ? (
            <button
              type="button"
              onClick={() => toast.info(item.errorMessage)}
              className="inline-flex items-center gap-1 text-destructive hover:underline text-[12px]"
            >
              <AlertCircle className="w-3 h-3" />
              错误详情
            </button>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}
