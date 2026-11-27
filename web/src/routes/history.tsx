import { OrderPaymentButton } from "@/components/account/OrderPaymentButton";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { describeOptionCodes } from "@/lib/option-groups";
import { OrderDurationLabel } from "@/components/queue/OrderDurationField";
import { Link, useNavigate } from "@tanstack/react-router";
import { createFileRoute } from "@tanstack/react-router";
import { Clock, RefreshCw, Trash2, AlertCircle, Hourglass, Copy, RotateCw, ArrowUp, ArrowDown, ArrowUpDown, Filter, Check } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { fmtDateTime } from "@/i18n/format";
import { PageHeader } from "@/components/common/PageHeader";
import { ListToolbar, SearchBox } from "@/components/common/ListToolbar";
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
import { comparePrices, currencyUnknownHint } from "@/lib/money";

const HISTORY_STATUS_ORDER = [
  "history.st.notPaid.label", "history.st.documentsRequested.label", "history.st.checking.label",
  "history.st.delivering.label", "history.st.delivered.label", "history.uncertain",
  "history.st.cancelling.label", "history.st.cancelled.label", "history.failed",
  "history.st.unknown.label", "history.st.notFound.label",
];

type HistorySortField = "model" | "dc" | "price" | "status" | "time" | "remaining";
type HistorySort = `${HistorySortField}${"Asc" | "Desc"}`;

export function nextHistorySort(current: HistorySort, field: HistorySortField): HistorySort {
  if (current.startsWith(field)) return `${field}${current.endsWith("Asc") ? "Desc" : "Asc"}`;
  return `${field}${field === "time" ? "Desc" : "Asc"}`;
}

function historyStatusRank(key: string): number {
  const rank = HISTORY_STATUS_ORDER.indexOf(key);
  return rank < 0 ? HISTORY_STATUS_ORDER.length : rank;
}

export function toggleHistoryFilterValue(current: string[], value: string): string[] {
  return current.includes(value) ? current.filter((item) => item !== value) : [...current, value];
}

export function normalizeHistoryDc(dc?: string): string {
  return (dc || "").trim().toUpperCase();
}

export function normalizeHistoryModel(planCode?: string): string {
  return (planCode || "").trim();
}

export function getAvailableStatusOptions(items: PurchaseHistory[], selected: string[]) {
  const counts = new Map<string, number>();
  for (const item of items) {
    const key = historyStatusKey(item);
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  for (const value of selected) if (!counts.has(value)) counts.set(value, 0);
  return [...counts].sort(([a], [b]) => historyStatusRank(a) - historyStatusRank(b))
    .map(([key, count]) => ({ value: key, labelKey: key, count }));
}

export function getAvailableModelOptions(items: PurchaseHistory[], selected: string[]) {
  const counts = new Map<string, number>();
  for (const item of items) {
    const value = normalizeHistoryModel(item.planCode);
    if (value) counts.set(value, (counts.get(value) ?? 0) + 1);
  }
  for (const model of selected) {
    const value = normalizeHistoryModel(model);
    if (value && !counts.has(value)) counts.set(value, 0);
  }
  return [...counts].sort(([a], [b]) => a.localeCompare(b, undefined, { numeric: true, sensitivity: "base" }))
    .map(([value, count]) => ({ value, label: value, count }));
}

export function getAvailableDcOptions(items: PurchaseHistory[], selected: string[]) {
  const counts = new Map<string, number>();
  for (const item of items) {
    const value = normalizeHistoryDc(item.datacenter);
    if (value) counts.set(value, (counts.get(value) ?? 0) + 1);
  }
  for (const dc of selected) {
    const value = normalizeHistoryDc(dc);
    if (value && !counts.has(value)) counts.set(value, 0);
  }
  return [...counts].sort(([a], [b]) => a.localeCompare(b, undefined, { numeric: true, sensitivity: "base" }))
    .map(([value, count]) => ({ value, label: value, count }));
}

function parseTimeMs(value?: string): number | null {
  if (!value) return null;
  const ms = new Date(value).getTime();
  return Number.isFinite(ms) ? ms : null;
}

function compareTimestamps(aMs: number | null, bMs: number | null, dir: "asc" | "desc"): number {
  if (aMs === null || bMs === null) return aMs === null ? (bMs === null ? 0 : 1) : -1;
  return dir === "asc" ? aMs - bMs : bMs - aMs;
}

export function filterAndSortHistory(
  items: PurchaseHistory[],
  { search, statusFilter, modelFilter = [], dcFilter = [], sort }: {
    search: string; statusFilter: string[]; modelFilter?: string[]; dcFilter?: string[]; sort: HistorySort;
  },
): PurchaseHistory[] {
  const query = search.trim().toLowerCase();
  return items.filter((item) => {
    if (statusFilter.length > 0 && !statusFilter.includes(historyStatusKey(item))) return false;
    if (modelFilter.length > 0 && !modelFilter.includes(normalizeHistoryModel(item.planCode))) return false;
    if (dcFilter.length > 0 && !dcFilter.includes(normalizeHistoryDc(item.datacenter))) return false;
    return !query || `${item.planCode} ${item.datacenter} ${item.orderId || ""}`.toLowerCase().includes(query);
  }).sort((a, b) => {
    const aMs = parseTimeMs(a.purchaseTime);
    const bMs = parseTimeMs(b.purchaseTime);
    const newestFirst = compareTimestamps(aMs, bMs, "desc");
    const dir = sort.endsWith("Asc") ? "asc" : "desc";
    if (sort === "timeAsc") return compareTimestamps(aMs, bMs, "asc");
    if (sort === "timeDesc") return newestFirst;
    if (sort.startsWith("price")) return comparePrices(a.price?.withTax, b.price?.withTax, dir) || newestFirst;
    if (sort.startsWith("remaining")) {
      return compareTimestamps(getCountdownExpirationMs(a), getCountdownExpirationMs(b), dir) || newestFirst;
    }
    const byField = sort.startsWith("status")
      ? historyStatusRank(historyStatusKey(a)) - historyStatusRank(historyStatusKey(b))
      : (sort.startsWith("model") ? normalizeHistoryModel(a.planCode) : normalizeHistoryDc(a.datacenter))
        .localeCompare(sort.startsWith("model") ? normalizeHistoryModel(b.planCode) : normalizeHistoryDc(b.datacenter), undefined, { numeric: true, sensitivity: "base" });
    return (dir === "asc" ? byField : -byField) || newestFirst;
  });
}

function historyStatusKey(item: PurchaseHistory): string {
  return item.status === "success" ? orderStatusView(item).labelKey : `history.${item.status}`;
}

function ColumnMultiFilter({ label, allLabel, totalCount, options, selected, onToggle, onClear }: {
  label: string;
  allLabel: string;
  totalCount: number;
  options: { value: string; label: string; count: number }[];
  selected: string[];
  onToggle: (value: string) => void;
  onClear: () => void;
}) {
  const selectedLabels = selected.map((value) => options.find((option) => option.value === value)?.label ?? value);
  const singleLabel = selectedLabels[0];
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={`inline-flex items-center gap-1 rounded-sm px-1 py-0.5 text-[11px] leading-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${selected.length > 0 ? "text-primary" : "hover:text-foreground"}`}
          aria-label={label}
          title={selected.length > 0 ? selectedLabels.join(", ") : label}
        >
          <Filter className="h-3 w-3 shrink-0" aria-hidden />
          {selected.length === 1 && <span className="max-w-[7rem] truncate normal-case">{singleLabel}</span>}
          {selected.length >= 2 && <span className="tabular-nums normal-case">{selected.length}</span>}
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-56 p-1.5" align="start">
        <button
          type="button"
          aria-pressed={selected.length === 0}
          onClick={onClear}
          className={`flex w-full items-center gap-2 rounded-sm px-2 py-2 sm:py-1.5 text-sm sm:text-xs hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${selected.length === 0 ? "bg-accent text-primary" : ""}`}
        >
          <span className={`flex h-4 w-4 shrink-0 items-center justify-center rounded-sm border ${selected.length === 0 ? "border-primary bg-primary text-primary-foreground" : "border-input"}`} aria-hidden>
            {selected.length === 0 && <Check className="h-3 w-3" />}
          </span>
          <span className="flex-1 text-left">{allLabel}</span>
          <span className="tabular-nums text-muted-foreground">{totalCount}</span>
        </button>
        <div className="my-1 h-px bg-border" />
        <div className="max-h-64 overflow-y-auto">
          {options.map((option) => {
            const checked = selected.includes(option.value);
            return (
              <button
                key={option.value}
                type="button"
                aria-pressed={checked}
                onClick={() => onToggle(option.value)}
                className={`flex w-full items-center gap-2 rounded-sm px-2 py-2 sm:py-1.5 text-sm sm:text-xs hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${checked ? "bg-accent text-primary" : ""}`}
              >
                <span className={`flex h-4 w-4 shrink-0 items-center justify-center rounded-sm border ${checked ? "border-primary bg-primary text-primary-foreground" : "border-input"}`} aria-hidden>
                  {checked && <Check className="h-3 w-3" />}
                </span>
                <span className="min-w-0 flex-1 truncate text-left">{option.label}</span>
                <span className="tabular-nums text-muted-foreground">{option.count}</span>
              </button>
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
}

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

function getExpirationMs(item: PurchaseHistory): number | null {
  const purchaseMs = parseTimeMs(item.purchaseTime);
  return parseTimeMs(item.expirationTime) ?? (purchaseMs !== null ? purchaseMs + ORDER_VALIDITY_MS : null);
}

function getCountdownExpirationMs(item: PurchaseHistory): number | null {
  const st = orderStatusView(item);
  if (item.status !== "success" || !item.orderId || st.paid || st.closed) return null;
  return getExpirationMs(item);
}

function HistoryPage() {
  const { t } = useTranslation();
  const list = useHistory();
  const clear = useClearHistory();
  const refreshStatus = useRefreshOrderStatus();
  const navigate = useNavigate();
  const buyAgain = (item: PurchaseHistory) =>
    void navigate({
      to: "/queue",
      search: {
        create: item.planCode,
        options: (item.options || []).join(","),
        account: item.accountId || undefined,
        dc: item.datacenter || undefined,
        duration: item.duration || undefined,
      },
    });
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<string[]>([]);
  const [modelFilter, setModelFilter] = useState<string[]>([]);
  const [dcFilter, setDcFilter] = useState<string[]>([]);
  const [sort, setSort] = useState<HistorySort>("timeDesc");
  const [confirmClear, setConfirmClear] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  // 每分钟刷新一次 now，让所有行的倒计时同步推进
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(id);
  }, []);

  const items = list.data || [];
  const filtered = useMemo(() => filterAndSortHistory(items, { search, statusFilter, modelFilter, dcFilter, sort }), [items, search, statusFilter, modelFilter, dcFilter, sort]);
  const statusOptions = useMemo(() => getAvailableStatusOptions(items, statusFilter), [items, statusFilter]);
  const modelOptions = useMemo(() => getAvailableModelOptions(items, modelFilter), [items, modelFilter]);
  const dcOptions = useMemo(() => getAvailableDcOptions(items, dcFilter), [items, dcFilter]);
  const [tableRef, wide] = useContainerWide(HISTORY_TABLE_MIN_WIDTH);
  const sortHeader = (field: HistorySortField, label: string) => {
    const active = sort.startsWith(field);
    const ascending = active && sort.endsWith("Asc");
    const next = nextHistorySort(sort, field);
    const clickToSort = t("history.sort.clickToSort", { next: t(`history.sort.${next}`) });
    const Icon = active ? (ascending ? ArrowUp : ArrowDown) : ArrowUpDown;
    return (
      <button
        type="button"
        className={`inline-flex items-center gap-1 rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${active ? "text-primary" : "hover:text-foreground"}`}
        onClick={() => setSort(next)}
        aria-label={t(`history.sort.${next}`)}
        title={field === "price" ? `${clickToSort} · ${t("history.sort.priceHint")}` : field === "remaining" ? `${clickToSort} · ${t("history.col.remainingTitle")}` : clickToSort}
      >
        {label}<Icon className="h-3 w-3" aria-hidden />
      </button>
    );
  };
  const sortAria = (field: HistorySortField) =>
    sort.startsWith(field) ? (sort.endsWith("Asc") ? "ascending" as const : "descending" as const) : "none" as const;
  const statusHeader = (
    <div className="inline-flex items-center gap-1">
      {sortHeader("status", t("history.col.status"))}
      <ColumnMultiFilter
        label={t("history.filterStatus")} allLabel={t("history.allStatuses")} totalCount={items.length}
        options={statusOptions.map((option) => ({ ...option, label: t(option.labelKey) }))} selected={statusFilter}
        onToggle={(value) => setStatusFilter((current) => toggleHistoryFilterValue(current, value))}
        onClear={() => setStatusFilter([])}
      />
    </div>
  );
  const modelHeader = (
    <div className="inline-flex items-center gap-1">
      {sortHeader("model", t("history.col.model"))}
      <ColumnMultiFilter
        label={t("history.filterModel")} allLabel={t("history.allModels")} totalCount={items.length}
        options={modelOptions} selected={modelFilter}
        onToggle={(value) => setModelFilter((current) => toggleHistoryFilterValue(current, value))}
        onClear={() => setModelFilter([])}
      />
    </div>
  );
  const dcHeader = (
    <div className="inline-flex items-center gap-1">
      {sortHeader("dc", t("history.col.dc"))}
      <ColumnMultiFilter
        label={t("history.filterDc")} allLabel={t("history.allDcs")} totalCount={items.length}
        options={dcOptions} selected={dcFilter}
        onToggle={(value) => setDcFilter((current) => toggleHistoryFilterValue(current, value))}
        onClear={() => setDcFilter([])}
      />
    </div>
  );
  const emptyResults = (
    <EmptyState
      illustration="search"
      title={t("history.emptyTitle")}
      action={
        <Button variant="outline" onClick={() => { setSearch(""); setStatusFilter([]); setModelFilter([]); setDcFilter([]); }}>
          {t("history.clearFilters")}
        </Button>
      }
    />
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
          <ListToolbar
            end={<SearchBox value={search} onChange={setSearch} placeholder={t("history.searchPlaceholder")} className="w-full sm:w-64" />}
          />
          <div ref={tableRef}>
            {wide ? (
              <table className="w-full">
                <thead>
                  <tr className="text-left text-[11px] font-medium text-muted-foreground uppercase tracking-wide border-b border-border whitespace-nowrap">
                    <th className="pl-4 pr-3 py-2.5" aria-sort={sortAria("model")}>{modelHeader}</th>
                    <th className="px-3 py-2.5" aria-sort={sortAria("dc")}>{dcHeader}</th>
                    <th className="px-3 py-2.5">{t("history.col.options")}</th>
                    <th className="px-3 py-2.5 text-right" aria-sort={sortAria("price")}>{sortHeader("price", t("history.col.price"))}</th>
                    <th className="px-3 py-2.5" aria-sort={sortAria("status")}>{statusHeader}</th>
                    <th className="px-3 py-2.5" aria-sort={sortAria("time")}>{sortHeader("time", t("history.col.time"))}</th>
                    <th className="px-3 py-2.5" aria-sort={sortAria("remaining")}>{sortHeader("remaining", t("history.col.remaining"))}</th>
                    <th className="pl-3 pr-4 py-2.5 text-right">{t("history.col.action")}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {filtered.length === 0
                    ? <tr><td colSpan={8}>{emptyResults}</td></tr>
                    : filtered.map((item) => <HistoryRow key={item.id} item={item} now={now} onAgain={buyAgain} />)}
                </tbody>
              </table>
            ) : (
              <div className="divide-y divide-border">
                <div className="flex flex-wrap items-center gap-x-5 gap-y-1 bg-muted/40 px-4 py-2 text-[11px] font-medium text-muted-foreground">
                  {modelHeader}
                  {dcHeader}
                  {sortHeader("price", t("history.col.price"))}
                  {statusHeader}
                  {sortHeader("time", t("history.col.time"))}
                  {sortHeader("remaining", t("history.col.remaining"))}
                </div>
                {filtered.length === 0 ? emptyResults : filtered.map((item) => <HistoryCard key={item.id} item={item} now={now} onAgain={buyAgain} />)}
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

function HistoryRow({ item, now, onAgain }: { item: PurchaseHistory; now: number; onAgain?: (item: PurchaseHistory) => void }) {
  const { t } = useTranslation();
  const st = orderStatusView(item);
  // 倒计时是"付款窗口":付了、取消了、交付了都不再显示
  const expirationMs = getCountdownExpirationMs(item);
  const showCountdown = expirationMs !== null;
  const remainingMs = expirationMs !== null ? expirationMs - now : 0;
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
        <HistoryActions item={item} payable={canPay(item, st, isExpired)} onAgain={onAgain} />
      </td>
    </tr>
  );
}

/** 手机端的订单卡片渲染。跟 HistoryRow 字段一一对应,但堆叠成卡片。 */
function HistoryCard({ item, now, onAgain }: { item: PurchaseHistory; now: number; onAgain?: (item: PurchaseHistory) => void }) {
  const { t } = useTranslation();
  const st = orderStatusView(item);
  const expirationMs = getCountdownExpirationMs(item);
  const showCountdown = expirationMs !== null;
  const remainingMs = expirationMs !== null ? expirationMs - now : 0;
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
        {/* 底部一行:日期 · 付款倒计时 …… 价格 + 操作(支付 / 错误) */}
        <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5 text-[12px]">
          <span className="text-muted-foreground tabular-nums whitespace-nowrap">{fmtDateTime(item.purchaseTime)}</span>
          {showCountdown && (
            <Chip tone={isExpired ? "danger" : isUrgent ? "warning" : "info"}>
              <Hourglass className="w-3 h-3" />
              {formatCountdown(remainingMs, t)}
            </Chip>
          )}
          <span className="ml-auto inline-flex items-center gap-2.5">
            {item.price?.withTax != null ? <HistoryPrice item={item} strike={isExpired} /> : null}
            <HistoryActions item={item} payable={canPay(item, st, isExpired)} onAgain={onAgain} />
          </span>
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
function HistoryActions({ item, payable, onAgain }: { item: PurchaseHistory; payable: boolean; onAgain?: (item: PurchaseHistory) => void }) {
  const { t } = useTranslation();
  // 再抢一单:带着这条记录的型号和配置打开新建任务弹窗(机房、数量在弹窗里确认),不直接下单。
  // 跳转由页面传入,这一行本身不依赖路由。
  const again = onAgain ? (
    <Button variant="ghost" size="icon" className="h-7 w-7 text-muted-foreground hover:text-primary" onClick={() => onAgain(item)} title={t("queue.buyAgain")} aria-label={t("queue.buyAgain")}>
      <RotateCw className="w-3.5 h-3.5" />
    </Button>
  ) : null;
  return (
    <span className="inline-flex items-center justify-end gap-1.5">
      {item.status !== "success"
        ? item.errorMessage && <ErrorDetail message={item.errorMessage} label={t("history.errorLink")} />
        : payable && <OrderPaymentButton compact accountId={item.accountId} orderId={Number(item.orderId)} managerUrl={item.orderUrl} />}
      {again}
    </span>
  );
}
