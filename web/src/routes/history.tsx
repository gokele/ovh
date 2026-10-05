import { createFileRoute } from "@tanstack/react-router";
import { Clock, RefreshCw, Trash2, Search, ExternalLink, AlertCircle, Hourglass } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { fmtDateTime } from "@/i18n/format";
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
 * 订单支付状态 → 展示用的 key。取值是 OVH 的 billing.order.OrderStatusEnum,三区一致。
 * 这才是用户真正关心的:「下单成功」只说明订单建了,付没付、过没过期,看这里。
 * 文案键存 history.st.*,渲染处用 t() 出译文。
 */
function orderStatusView(item: PurchaseHistory): {
  labelKey: string;
  tone: "success" | "warning" | "danger" | "info" | "default";
  paid: boolean;
  closed: boolean;
  titleKey: string;
} {
  switch (item.orderStatus) {
    case "notPaid":
      return { labelKey: "history.st.notPaid.label", tone: "warning", paid: false, closed: false, titleKey: "history.st.notPaid.title" };
    case "checking":
      return { labelKey: "history.st.checking.label", tone: "info", paid: true, closed: false, titleKey: "history.st.checking.title" };
    case "delivering":
      return { labelKey: "history.st.delivering.label", tone: "success", paid: true, closed: false, titleKey: "history.st.delivering.title" };
    case "delivered":
      return { labelKey: "history.st.delivered.label", tone: "success", paid: true, closed: true, titleKey: "history.st.delivered.title" };
    case "cancelling":
      return { labelKey: "history.st.cancelling.label", tone: "danger", paid: false, closed: true, titleKey: "history.st.cancelling.title" };
    case "cancelled":
      return { labelKey: "history.st.cancelled.label", tone: "danger", paid: false, closed: true, titleKey: "history.st.cancelled.title" };
    case "documentsRequested":
      return { labelKey: "history.st.documentsRequested.label", tone: "warning", paid: false, closed: false, titleKey: "history.st.documentsRequested.title" };
    case "unknown":
      return { labelKey: "history.st.unknown.label", tone: "default", paid: false, closed: false, titleKey: "history.st.unknown.title" };
    default:
      return { labelKey: "history.st.notFound.label", tone: "default", paid: false, closed: false, titleKey: "history.st.notFound.title" };
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
  const { t } = useTranslation();
  const value = item.price?.withTax;
  if (value == null) return <span className="text-muted-foreground">—</span>;
  const currency = (item.price?.currencyCode || "").trim();
  return (
    <span
      className={`font-mono font-medium text-success ${strike ? "line-through" : ""}`}
      title={currency ? undefined : CURRENCY_UNKNOWN_HINT}
    >
      {value}
      {currency ? ` ${currency}` : <span className="text-muted-foreground">{t("history.currencyUnknown")}</span>}
    </span>
  );
}

/** 订单有效期 15 天，未提供 expirationTime 时用 purchaseTime + 15d 兜底 */
const ORDER_VALIDITY_MS = 15 * 24 * 60 * 60 * 1000;

/** 把毫秒倒计时格式化为 `2天5时12分` / `12分` / `已过期`(文案跟随语言) */
function formatCountdown(remainingMs: number, t: TFunction): string {
  if (remainingMs <= 0) return t("history.countdown.expired");
  const totalMinutes = Math.floor(remainingMs / 60_000);
  const days = Math.floor(totalMinutes / (24 * 60));
  const hours = Math.floor((totalMinutes % (24 * 60)) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return t("history.countdown.dhm", { d: days, h: hours, m: minutes });
  if (hours > 0) return t("history.countdown.hm", { h: hours, m: minutes });
  return t("history.countdown.m", { m: minutes });
}

function getExpirationMs(item: PurchaseHistory): number {
  if (item.expirationTime) return new Date(item.expirationTime).getTime();
  return new Date(item.purchaseTime).getTime() + ORDER_VALIDITY_MS;
}

function HistoryPage() {
  const { t } = useTranslation();
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
        title={t("history.title")}
        description={t("history.description")}
        action={
          <div className="flex flex-wrap justify-end gap-2">
            <Button
              variant="outline"
              onClick={() => refreshStatus.mutate()}
              disabled={list.isFetching || refreshStatus.isPending}
              title={t("history.refreshStatusTitle")}
            >
              <RefreshCw
                className={`w-4 h-4 ${list.isFetching || refreshStatus.isPending ? "animate-spin" : ""}`}
              />
              {t("history.refreshStatus")}
            </Button>
            <Button variant="outline" onClick={() => setConfirmClear(true)} disabled={items.length === 0}>
              <Trash2 className="w-4 h-4" />
              {t("history.clear")}
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
                placeholder={t("history.searchPlaceholder")}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-9 rounded-full"
              />
            </div>
            <Select value={statusFilter} onValueChange={(v) => setStatusFilter(v as any)}>
              <SelectTrigger className="rounded-full">
                <SelectValue placeholder={t("history.allStatuses")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("history.allStatuses")}</SelectItem>
                <SelectItem value="success">{t("history.success")}</SelectItem>
                <SelectItem value="failed">{t("history.failed")}</SelectItem>
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
            title={t("history.loadFailedTitle")}
            error={list.error}
            onRetry={() => list.refetch()}
          />
          <p className="px-6 pb-5 text-[11px] text-muted-foreground text-center">
            {t("history.loadFailedHint")}
          </p>
        </Card>
      ) : filtered.length === 0 ? (
        <Card>
          <EmptyState icon={Clock} title={t("history.emptyTitle")} />
        </Card>
      ) : (
        <>
          {/* 桌面 / 平板:横向表格 */}
          <Card className="hidden md:block overflow-x-auto">
            <table className="w-full min-w-[760px]">
              <thead>
                <tr className="text-left text-[11px] font-medium text-muted-foreground border-b border-border">
                  <th className="px-4 py-3">{t("history.col.model")}</th>
                  <th className="px-4 py-3">{t("history.col.dc")}</th>
                  <th className="px-4 py-3">{t("history.col.options")}</th>
                  <th className="px-4 py-3">{t("history.col.price")}</th>
                  <th className="px-4 py-3">{t("history.col.status")}</th>
                  <th className="px-4 py-3">{t("history.col.time")}</th>
                  <th className="px-4 py-3" title={t("history.col.remainingTitle")}>
                {t("history.col.remaining")}
              </th>
                  <th className="px-4 py-3">{t("history.col.action")}</th>
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
            <DialogTitle>{t("history.clearTitle")}</DialogTitle>
            <DialogDescription>{t("history.clearDesc")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmClear(false)}>{t("history.cancel")}</Button>
            <Button variant="destructive" onClick={() => { clear.mutate(); setConfirmClear(false); }}>
              {t("history.confirmClear")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function HistoryRow({ item, now }: { item: PurchaseHistory; now: number }) {
  const { t } = useTranslation();
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
        {item.options && item.options.length > 0 ? item.options.join(", ") : t("history.defaultOptions")}
      </td>
      <td className="px-4 py-3">
        <HistoryPrice item={item} strike={isExpired} />
      </td>
      <td className="px-4 py-3">
        {item.status === "success" ? (
          <Chip tone={st.tone} title={t(st.titleKey)}>
            {t(st.labelKey)}
          </Chip>
        ) : (
          <Chip tone="danger">{t("history.failed")}</Chip>
        )}
      </td>
      <td className="px-4 py-3 text-[11px] text-muted-foreground font-mono whitespace-nowrap">
        {fmtDateTime(item.purchaseTime)}
      </td>
      <td className="px-4 py-3 whitespace-nowrap">
        {showCountdown ? (
          <Chip tone={isExpired ? "danger" : isUrgent ? "warning" : "info"}>
            <Hourglass className="w-3 h-3" />
            {formatCountdown(remainingMs, t)}
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
            {t("history.orderLink")}
          </a>
        ) : item.status === "failed" && item.errorMessage ? (
          <button
            type="button"
            onClick={() => toast.info(item.errorMessage)}
            className="inline-flex items-center gap-1 text-destructive hover:underline text-[12px]"
          >
            <AlertCircle className="w-3 h-3" />
            {t("history.errorLink")}
          </button>
        ) : "—"}
      </td>
    </tr>
  );
}

/** 手机端的订单卡片渲染。跟 HistoryRow 字段一一对应,但堆叠成卡片。 */
function HistoryCard({ item, now }: { item: PurchaseHistory; now: number }) {
  const { t } = useTranslation();
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
            <Chip tone={st.tone} title={t(st.titleKey)}>
            {t(st.labelKey)}
          </Chip>
          ) : (
            <Chip tone="danger">{t("history.failed")}</Chip>
          )}
        </div>
        <div className={`text-[11px] text-muted-foreground break-all ${isExpired ? "line-through" : ""}`}>
          {item.options && item.options.length > 0 ? item.options.join(", ") : t("history.defaultOptions")}
        </div>
        <div className="flex items-center justify-between gap-2 text-[11px]">
          <span className="text-muted-foreground font-mono">
            {fmtDateTime(item.purchaseTime)}
          </span>
          {item.price?.withTax != null ? <HistoryPrice item={item} strike={isExpired} /> : null}
        </div>
        <div className="flex items-center justify-between gap-2">
          {showCountdown ? (
            <Chip tone={isExpired ? "danger" : isUrgent ? "warning" : "info"}>
              <Hourglass className="w-3 h-3" />
              {formatCountdown(remainingMs, t)}
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
              {t("history.orderLink")}
            </a>
          ) : item.status === "failed" && item.errorMessage ? (
            <button
              type="button"
              onClick={() => toast.info(item.errorMessage)}
              className="inline-flex items-center gap-1 text-destructive hover:underline text-[12px]"
            >
              <AlertCircle className="w-3 h-3" />
              {t("history.errorDetail")}
            </button>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}
