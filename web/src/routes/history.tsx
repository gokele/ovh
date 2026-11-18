import { OrderPaymentButton } from "@/components/account/OrderPaymentButton";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { describeOptionCodes } from "@/lib/option-groups";
import { OrderDurationLabel } from "@/components/queue/OrderDurationField";
import { Link } from "@tanstack/react-router";
import { createFileRoute } from "@tanstack/react-router";
import { Clock, RefreshCw, Trash2, AlertCircle, Hourglass, Copy } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { fmtDateTime } from "@/i18n/format";
import { PageHeader } from "@/components/common/PageHeader";
import { ListToolbar, SearchBox, Segmented } from "@/components/common/ListToolbar";
import { useContainerWide } from "@/hooks/use-container-wide";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Chip } from "@/components/common/Chip";
import { AccountChip } from "@/components/common/AccountChip";
import { TimingChip } from "@/components/common/TimingChip";
import { OrderSpeed } from "@/components/common/OrderSpeed";
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
import { currencyUnknownHint } from "@/lib/money";

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
      className={`font-medium tabular-nums text-foreground ${strike ? "line-through" : ""}`}
      title={currency ? undefined : currencyUnknownHint()}
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
  const [statusFilter, setStatusFilter] = useState<"all" | "success" | "failed" | "uncertain">("all");
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

  const counts = useMemo(() => {
    const c = { all: items.length, success: 0, failed: 0, uncertain: 0 };
    for (const i of items) if (i.status in c) c[i.status as "success" | "failed" | "uncertain"]++;
    return c;
  }, [items]);
  const [tableRef, wide] = useContainerWide(HISTORY_TABLE_MIN_WIDTH);
  const toolbar = (
    <ListToolbar
      end={<SearchBox value={search} onChange={setSearch} placeholder={t("history.searchPlaceholder")} className="w-full sm:w-64" />}
    >
      <Segmented<"all" | "success" | "failed" | "uncertain">
        label={t("history.allStatuses")}
        value={statusFilter}
        onChange={setStatusFilter}
        options={[
          { value: "all", label: t("history.allStatuses"), count: counts.all },
          { value: "success", label: t("history.success"), count: counts.success },
          { value: "failed", label: t("history.failed"), count: counts.failed },
          ...(counts.uncertain > 0 || statusFilter === "uncertain"
            ? [{ value: "uncertain" as const, label: t("history.uncertain"), count: counts.uncertain }]
            : []),
        ]}
      />
    </ListToolbar>
  );

  return (
    <div className="space-y-3 sm:space-y-5">
      <PageHeader
        icon={Clock}
        title={t("history.title")}
        description={t("history.description")}
        action={
          <div className="flex flex-wrap justify-end gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => refreshStatus.mutate()}
              disabled={list.isFetching || refreshStatus.isPending}
              title={t("history.refreshStatusTitle")}
            >
              <RefreshCw
                className={`w-4 h-4 ${list.isFetching || refreshStatus.isPending ? "animate-spin" : ""}`}
              />
              {t("history.refreshStatus")}
            </Button>
            <Button variant="outline" size="sm" onClick={() => setConfirmClear(true)} disabled={items.length === 0}>
              <Trash2 className="w-4 h-4" />
              {t("history.clear")}
            </Button>
          </div>
        }
      />


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
      ) : items.length === 0 ? (
        <Card>
          <EmptyState
            illustration="history"
            title={t("history.emptyNoneTitle")}
            description={t("history.emptyNoneDesc")}
            action={
              <Button variant="outline" asChild>
                <Link to="/servers">{t("queue.emptyBrowse")}</Link>
              </Button>
            }
          />
        </Card>
      ) : (
        <Card className="overflow-hidden">
          {toolbar}
          <div ref={tableRef}>
            {filtered.length === 0 ? (
              <EmptyState
                illustration="search"
                title={t("history.emptyTitle")}
                action={
                  <Button variant="outline" onClick={() => { setSearch(""); setStatusFilter("all"); }}>
                    {t("history.clearFilters")}
                  </Button>
                }
              />
            ) : wide ? (
              <table className="w-full">
                <thead>
                  <tr className="text-left text-[11px] font-medium text-muted-foreground uppercase tracking-wide border-b border-border whitespace-nowrap">
                    <th className="pl-4 pr-3 py-2.5">{t("history.col.model")}</th>
                    <th className="px-3 py-2.5">{t("history.col.dc")}</th>
                    <th className="px-3 py-2.5">{t("history.col.options")}</th>
                    <th className="px-3 py-2.5 text-right">{t("history.col.price")}</th>
                    <th className="px-3 py-2.5">{t("history.col.status")}</th>
                    <th className="px-3 py-2.5">{t("history.col.time")}</th>
                    <th className="px-3 py-2.5" title={t("history.col.remainingTitle")}>
                      {t("history.col.remaining")}
                    </th>
                    <th className="pl-3 pr-4 py-2.5 text-right">{t("history.col.action")}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {filtered.map((item) => <HistoryRow key={item.id} item={item} now={now} />)}
                </tbody>
              </table>
            ) : (
              <div className="divide-y divide-border">
                {filtered.map((item) => <HistoryCard key={item.id} item={item} now={now} />)}
              </div>
            )}
          </div>
          <div className="px-4 py-2.5 border-t border-border text-[11px] text-muted-foreground tabular-nums">
            {filtered.length} / {items.length}
          </div>
        </Card>
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
    <tr className={`text-[13px] hover:bg-muted/40 align-top ${isExpired ? "opacity-55" : ""}`}>
      <td className="pl-4 pr-3 py-3 whitespace-nowrap">
        <div className="flex items-center gap-2">
          <span className="font-mono font-semibold">{item.planCode}</span>
          <AccountChip accountId={item.accountId} />
        </div>
      </td>
      <td className="px-3 py-3 whitespace-nowrap font-mono text-[12px]">{item.datacenter.toUpperCase()}</td>
      {/* 配置列最宽:第一行配置,第二行周期 · 耗时 · 派发 · 等待,整条记录最多两行 */}
      <td className="px-3 py-3 w-full max-w-0">
        <div className="truncate text-[12px] text-foreground/80" title={item.options?.join("\n")}>
          {item.options && item.options.length > 0 ? describeOptionCodes(item.options) : t("history.defaultOptions")}
        </div>
        <div className="mt-1 flex items-center gap-x-3 text-[11px] text-muted-foreground overflow-hidden whitespace-nowrap">
          <span><OrderDurationLabel value={item.duration} /></span>
          <TimingChip totalMs={item.totalMs} phases={item.timing} />
          <OrderSpeed detectionToWriteUs={item.detectionToWriteUs} prewarmed={item.prewarmed} connectionReused={item.connectionReused} waitMs={item.waitMs} />
        </div>
        {item.couponWarning && <p className="mt-1 text-[11px] text-warning break-words whitespace-normal" role="status">{item.couponWarning}</p>}
      </td>
      <td className="px-3 py-3 whitespace-nowrap text-right">
        <HistoryPrice item={item} strike={isExpired} />
      </td>
      <td className="px-3 py-3 whitespace-nowrap">
        {item.status === "success" ? (
          <Chip tone={st.tone} title={t(st.titleKey)}>
            {t(st.labelKey)}
          </Chip>
        ) : (
          <Chip tone={item.status === "uncertain" ? "warning" : "danger"}>{t(item.status === "uncertain" ? "history.uncertain" : "history.failed")}</Chip>
        )}
      </td>
      <td className="px-3 py-3 text-[12px] text-muted-foreground tabular-nums whitespace-nowrap">
        {fmtDateTime(item.purchaseTime)}
      </td>
      <td className="px-3 py-3 whitespace-nowrap">
        {showCountdown ? (
          <Chip tone={isExpired ? "danger" : isUrgent ? "warning" : "info"}>
            <Hourglass className="w-3 h-3" />
            {formatCountdown(remainingMs, t)}
          </Chip>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </td>
      <td className="pl-3 pr-4 py-3 whitespace-nowrap text-right">
        {/* 待付款且未过期:直接在这里付款(复用账户页的预检 + 明确确认流程,不自动重试) */}
        <HistoryActions item={item} payable={canPay(item, st, isExpired)} />
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
    <div className={`px-4 py-3 space-y-2 ${isExpired ? "opacity-60" : ""}`}>
        <div className="flex items-start justify-between gap-2 flex-wrap">
          <div className="flex items-center gap-2 flex-wrap min-w-0">
            <span className="font-mono font-semibold text-[13px]">{item.planCode}</span>
            <span className="font-mono text-[11px] text-muted-foreground">{item.datacenter.toUpperCase()}</span>
            <AccountChip accountId={item.accountId} />
          </div>
          {item.status === "success" ? (
            <Chip tone={st.tone} title={t(st.titleKey)}>
            {t(st.labelKey)}
          </Chip>
          ) : (
            <Chip tone={item.status === "uncertain" ? "warning" : "danger"}>{t(item.status === "uncertain" ? "history.uncertain" : "history.failed")}</Chip>
          )}
        </div>
        <div className="truncate text-[11px] text-muted-foreground" title={item.options?.join("\n")}>
          {item.options && item.options.length > 0 ? describeOptionCodes(item.options) : t("history.defaultOptions")}
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
          <span className="whitespace-nowrap"><OrderDurationLabel value={item.duration} /></span>
          <TimingChip totalMs={item.totalMs} phases={item.timing} />
          <OrderSpeed detectionToWriteUs={item.detectionToWriteUs} prewarmed={item.prewarmed} connectionReused={item.connectionReused} waitMs={item.waitMs} />
        </div>
        {item.couponWarning && <p className="text-[11px] text-warning break-words" role="status">{item.couponWarning}</p>}
        <div className="flex items-center justify-between gap-2 text-[12px]">
          <span className="text-muted-foreground tabular-nums whitespace-nowrap">
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
          {(canPay(item, st, isExpired) || (item.status !== "success" && !!item.errorMessage)) && (
            <HistoryActions item={item} payable={canPay(item, st, isExpired)} />
          )}
        </div>
    </div>
  );
}

/** 历史表格完整显示所需的最小宽度;不足时改用紧凑列表。 */
const HISTORY_TABLE_MIN_WIDTH = 940;

/** 失败原因:点开小卡片看全文,可复制;不再用几秒就消失的提示条 */
function ErrorDetail({ message, label }: { message: string; label: string }) {
  const { t } = useTranslation();
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button type="button" className="inline-flex items-center gap-1 text-destructive hover:underline text-[12px]">
          <AlertCircle className="w-3 h-3" />
          {label}
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[min(380px,calc(100vw-32px))] p-3">
        <div className="flex items-center gap-1.5 text-[12px] font-semibold text-destructive">
          <AlertCircle className="w-3.5 h-3.5" />
          {t("history.errorTitle")}
        </div>
        <p className="mt-2 max-h-56 overflow-y-auto whitespace-pre-wrap break-words text-[12.5px] leading-relaxed text-foreground/90">{message}</p>
        <div className="mt-2.5 flex justify-end">
          <Button
            variant="outline"
            size="sm"
            className="h-7 gap-1 text-[12px]"
            onClick={() => void navigator.clipboard?.writeText(message).then(() => toast.success(t("history.errorCopied")))}
          >
            <Copy className="w-3.5 h-3.5" />
            {t("history.errorCopy")}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

/** 可在历史里直接付款:已下单、订单号有效、OVH 状态是待付款(或尚未读到状态)、未过付款期 */
function canPay(item: PurchaseHistory, st: { paid: boolean; closed: boolean }, isExpired: boolean): boolean {
  const id = Number(item.orderId);
  return (
    item.status === "success" &&
    Number.isFinite(id) &&
    id > 0 &&
    !st.paid &&
    !st.closed &&
    !isExpired &&
    (!item.orderStatus || item.orderStatus === "notPaid") &&
    !!item.accountId
  );
}

/** 操作列:只放要做的事 —— 待付款显示「支付」,失败显示「错误」详情,其余为「—」。 */
function HistoryActions({ item, payable }: { item: PurchaseHistory; payable: boolean }) {
  const { t } = useTranslation();
  if (item.status !== "success") {
    return item.errorMessage ? <ErrorDetail message={item.errorMessage} label={t("history.errorLink")} /> : <span className="text-muted-foreground">—</span>;
  }
  if (!payable) return <span className="text-muted-foreground">—</span>;
  return <OrderPaymentButton compact accountId={item.accountId} orderId={Number(item.orderId)} managerUrl={item.orderUrl} />;
}
