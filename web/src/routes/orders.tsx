import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { useQueryClient } from "@tanstack/react-query";
import { PackageCheck, RefreshCw, Check, Loader2, X, Circle, ChevronDown, ExternalLink, FileWarning } from "lucide-react";
import { PageHeader } from "@/components/common/PageHeader";
import { ListToolbar, SearchBox, Segmented } from "@/components/common/ListToolbar";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Chip } from "@/components/common/Chip";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed, errorMessage } from "@/components/common/LoadFailed";
import { OrderPaymentButton } from "@/components/account/OrderPaymentButton";
import { fmtDateTime, fmtMoney } from "@/i18n/format";
import { qk } from "@/lib/query";
import { cn } from "@/lib/utils";
import { useAccountScope, useOrders, useOrderTracking, type OrderRecord, type OrderTracking } from "@/hooks/use-account";

export const Route = createFileRoute("/orders")({
  component: OrdersPage,
});

type Filter = "all" | "active" | "unpaid" | "delivered" | "cancelled";

/** 订单状态归类:进行中 = 未交付也未取消(含待付款、核验、交付、补资料) */
function bucketOf(status?: string): Exclude<Filter, "all"> {
  if (status === "delivered") return "delivered";
  // expired:OVH 仍报 notPaid 但已过付款期(后端按 expirationDate 换算),官方控制台算作已取消
  if (status === "cancelled" || status === "cancelling" || status === "expired") return "cancelled";
  if (status === "notPaid") return "unpaid";
  return "active";
}

const STEPS = ["VALIDATING", "VALIDATED", "DELIVERING", "AVAILABLE"] as const;

/**
 * 订单状态页:列出账户最近的 OVH 订单,显示付款状态和交付步骤。
 * 交付步骤来自 GET /me/order/{id}/followUp(BETA);进行中的订单自动读取并每分钟刷新,
 * 已交付 / 已取消的按状态直接画完成态,点开才读取明细和记录,避免一次打几十个请求。
 */
function OrdersPage() {
  const { t } = useTranslation();
  const accountId = useAccountScope();
  const list = useOrders();
  const qc = useQueryClient();
  const [filter, setFilter] = useState<Filter>("active");
  const [search, setSearch] = useState("");

  const items = useMemo(() => {
    const rows = [...(list.data?.items || [])];
    return rows.sort((a, b) => {
      const d = new Date(b.date).getTime() - new Date(a.date).getTime();
      return Number.isNaN(d) ? b.orderId - a.orderId : d || b.orderId - a.orderId;
    });
  }, [list.data]);

  const counts = useMemo(() => {
    const c: Record<Filter, number> = { all: items.length, active: 0, unpaid: 0, delivered: 0, cancelled: 0 };
    for (const o of items) {
      const b = bucketOf(o.orderStatus);
      c[b]++;
      if (b === "unpaid") c.active++; // 待付款也算进行中
    }
    return c;
  }, [items]);

  const shown = useMemo(() => {
    const s = search.trim().toLowerCase();
    return items.filter((o) => {
      const b = bucketOf(o.orderStatus);
      if (filter === "active" ? b !== "active" && b !== "unpaid" : filter !== "all" && b !== filter) return false;
      if (s && !String(o.orderId).includes(s)) {
        const cached = qc.getQueryData<OrderTracking>(qk.account.orderTracking(o.accountId || accountId, o.orderId));
        const text = (cached?.details || []).map((d) => d.description).join(" ").toLowerCase();
        if (!text.includes(s)) return false;
      }
      return true;
    });
  }, [items, filter, search, qc, accountId]);

  return (
    <div className="space-y-3 sm:space-y-5">
      <PageHeader
        icon={PackageCheck}
        title={t("orderTrack.title")}
        description={t("orderTrack.description")}
        action={
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              void list.refetch();
              void qc.invalidateQueries({ queryKey: ["account", "order-tracking", accountId] });
            }}
            disabled={list.isFetching}
          >
            <RefreshCw className={cn("w-4 h-4", list.isFetching && "animate-spin motion-reduce:animate-none")} />
            {t("common.refresh")}
          </Button>
        }
      />
      <ListToolbar end={<SearchBox value={search} onChange={setSearch} placeholder={t("orderTrack.search")} className="w-full sm:w-64" />}>
        <Segmented<Filter>
          label={t("orderTrack.filter.all")}
          value={filter}
          onChange={setFilter}
          options={(["active", "unpaid", "delivered", "cancelled", "all"] as const).map((f) => ({
            value: f,
            label: t(`orderTrack.filter.${f}`),
            count: counts[f],
          }))}
        />
      </ListToolbar>

      {list.isPending ? (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-32 w-full rounded-xl" />
          ))}
        </div>
      ) : list.isError ? (
        <LoadFailed title={t("orderTrack.loadFailed")} error={list.error} onRetry={() => list.refetch()} />
      ) : shown.length === 0 ? (
        <Card>
          <EmptyState icon={PackageCheck} title={items.length ? t("orderTrack.emptyFiltered") : t("orderTrack.empty")} />
        </Card>
      ) : (
        <div className="space-y-3">
          {shown.map((o) => (
            <OrderCard key={o.orderId} order={o} accountId={o.accountId || accountId} />
          ))}
        </div>
      )}
    </div>
  );
}

function statusTone(status?: string): "success" | "warning" | "danger" | "info" | "default" {
  switch (status) {
    case "delivered":
      return "success";
    case "notPaid":
      return "warning";
    case "documentsRequested":
      return "danger";
    case "checking":
    case "delivering":
      return "info";
    default:
      return "default";
  }
}

function OrderCard({ order, accountId }: { order: OrderRecord; accountId: string }) {
  const { t } = useTranslation();
  const status = order.orderStatus || "unknown";
  const bucket = bucketOf(order.orderStatus);
  const inProgress = bucket === "active" || bucket === "unpaid";
  const [open, setOpen] = useState(false);
  // 进行中的自动读取交付进度;已完成/已取消的展开时才读
  const track = useOrderTracking(accountId, order.orderId, inProgress || open);
  const data = track.data;
  const price =
    order.priceWithTax && Number.isFinite(order.priceWithTax.value) && order.priceWithTax.currencyCode
      ? fmtMoney(order.priceWithTax.value, order.priceWithTax.currencyCode)
      : order.priceWithTax?.text || "—";
  const products = (data?.details || []).filter((d) => d.description && !d.cancelled);
  // 标题行只数商品本身,安装费等费用行放到展开明细里
  const mainProducts = products.filter((d) => d.detailType !== "INSTALLATION");
  const steps = deriveSteps(data, status);
  const history = (data?.steps || [])
    .flatMap((s) => s.history.map((h) => ({ ...h, step: s.step })))
    .sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime());

  return (
    <Card className="overflow-hidden">
      <div className="px-4 sm:px-5 pt-4 pb-3 flex flex-wrap items-start gap-x-4 gap-y-2">
        <div className="min-w-0 flex-1 basis-full sm:basis-auto">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-[15px] font-semibold">#{order.orderId}</span>
            <Chip tone={statusTone(status)}>{t(`orderTrack.status.${status}`, { defaultValue: status })}</Chip>
          </div>
          <div className="mt-1 text-[12.5px] text-muted-foreground flex flex-wrap gap-x-2 gap-y-0.5">
            <span className="tabular-nums">{order.date ? fmtDateTime(order.date) : "—"}</span>
            {mainProducts.length > 0 && (
              <>
                <span aria-hidden>·</span>
                <span className="text-foreground/80 truncate max-w-full" title={products.map((p) => p.description).join("\n")}>
                  {mainProducts[0].description}
                  {mainProducts.length > 1 ? ` ${t("orderTrack.more", { n: mainProducts.length - 1 })}` : ""}
                </span>
              </>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <span className="text-[15px] font-semibold tabular-nums">{price}</span>
          {status === "notPaid" && <OrderPaymentButton accountId={accountId} orderId={order.orderId} managerUrl={order.url} compact />}
          {order.url && (
            <a
              href={order.url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center justify-center w-8 h-8 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted"
              title={t("orderTrack.open")}
              aria-label={t("orderTrack.open")}
            >
              <ExternalLink className="w-4 h-4" />
            </a>
          )}
        </div>
      </div>

      {status === "documentsRequested" && (
        <div className="mx-4 sm:mx-5 mb-3 flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/[.06] px-3 py-2 text-[12.5px] text-destructive">
          <FileWarning className="w-4 h-4 mt-0.5 shrink-0" aria-hidden />
          {t("orderTrack.docsHint")}
        </div>
      )}

      {/* 交付步骤 */}
      <div className="px-4 sm:px-5 pb-3">
        {track.isFetching && !data && inProgress ? (
          <div className="flex items-center gap-2 text-[12.5px] text-muted-foreground py-2">
            <Loader2 className="w-3.5 h-3.5 animate-spin motion-reduce:animate-none" />
            {t("orderTrack.loadingTrack")}
          </div>
        ) : (
          <Stepper steps={steps} t={t} />
        )}
        {track.isError && (
          <p className="mt-1 text-[12px] text-destructive">{t("orderTrack.trackFailed", { detail: errorMessage(track.error) })}</p>
        )}
        {data?.followUpError && (
          <p className="mt-1 text-[12px] text-muted-foreground">{t("orderTrack.followUpFailed", { detail: data.followUpError })}</p>
        )}
      </div>

      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center justify-center gap-1 border-t border-border py-2 text-[12.5px] text-muted-foreground hover:text-foreground hover:bg-muted/40"
        aria-expanded={open}
      >
        {open ? t("orderTrack.hideTimeline") : t("orderTrack.showTimeline")}
        <ChevronDown className={cn("w-3.5 h-3.5 transition-transform", open && "rotate-180")} />
      </button>
      {open && (
        <div className="border-t border-border bg-muted/20 px-4 sm:px-5 py-3">
          {track.isPending ? (
            <Skeleton className="h-16 w-full" />
          ) : history.length === 0 ? (
            <p className="text-[12.5px] text-muted-foreground">{t("orderTrack.noTimeline")}</p>
          ) : (
            <ol className="relative space-y-3 before:absolute before:left-[5px] before:top-1.5 before:bottom-1.5 before:w-px before:bg-border">
              {history.map((h, i) => (
                <li key={i} className="relative pl-5">
                  <span className={cn("absolute left-0 top-1.5 w-[11px] h-[11px] rounded-full ring-2 ring-background", i === 0 ? "bg-primary" : "bg-muted-foreground/40")} />
                  <div className="flex flex-wrap items-baseline gap-x-2 text-[13px]">
                    <span className="font-medium">{t(`orderTrack.label.${h.label}`, { defaultValue: h.label })}</span>
                    <span className="text-[11.5px] text-muted-foreground tabular-nums">{h.date ? fmtDateTime(h.date) : ""}</span>
                  </div>
                  {h.description && <p className="text-[12px] text-muted-foreground break-words">{h.description}</p>}
                </li>
              ))}
            </ol>
          )}
          {products.length > 1 && (
            <ul className="mt-3 pt-3 border-t border-border space-y-1 text-[12.5px]">
              {products.map((p, i) => (
                <li key={i} className="flex justify-between gap-3">
                  <span className="min-w-0 break-words">{p.description}</span>
                  {p.total && <span className="shrink-0 tabular-nums text-muted-foreground">{p.total}</span>}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </Card>
  );
}

type StepView = { step: (typeof STEPS)[number]; status: "DONE" | "DOING" | "TODO" | "ERROR" };

/** 有 followUp 用 OVH 的步骤;没有时按订单状态推一个大致进度(已交付 = 全部完成) */
function deriveSteps(data: OrderTracking | undefined, status: string): StepView[] {
  const fromApi = new Map((data?.steps || []).map((s) => [s.step, s.status]));
  if (fromApi.size > 0) {
    return STEPS.map((step) => ({ step, status: (fromApi.get(step) as StepView["status"]) || "TODO" }));
  }
  const doneUntil =
    status === "delivered" ? 4 : status === "delivering" ? 2 : status === "checking" ? 0 : status === "notPaid" ? 0 : -1;
  return STEPS.map((step, i) => ({
    step,
    status: status === "cancelled" || status === "cancelling" || status === "expired" ? "TODO" : i < doneUntil ? "DONE" : i === doneUntil && status !== "notPaid" ? "DOING" : "TODO",
  }));
}

function Stepper({ steps, t }: { steps: StepView[]; t: TFunction }) {
  return (
    <ol className="grid grid-cols-4 gap-1.5">
      {steps.map((s, i) => {
        const Icon = s.status === "DONE" ? Check : s.status === "DOING" ? Loader2 : s.status === "ERROR" ? X : Circle;
        const tone =
          s.status === "DONE"
            ? "bg-success text-white"
            : s.status === "DOING"
              ? "bg-primary text-white"
              : s.status === "ERROR"
                ? "bg-destructive text-white"
                : "bg-muted text-muted-foreground";
        const bar = s.status === "DONE" ? "bg-success" : s.status === "DOING" ? "bg-primary/50" : s.status === "ERROR" ? "bg-destructive/60" : "bg-border";
        return (
          <li key={s.step} className="min-w-0" title={`${t(`orderTrack.step.${s.step}`)} · ${t(`orderTrack.stepStatus.${s.status}`)}`}>
            <div className="flex items-center gap-1.5">
              <span className={cn("w-5 h-5 rounded-full flex items-center justify-center shrink-0", tone)}>
                <Icon className={cn("w-3 h-3", s.status === "DOING" && "animate-spin motion-reduce:animate-none", s.status === "TODO" && "w-2 h-2")} strokeWidth={3} />
              </span>
              {i < steps.length - 1 && <span className={cn("h-0.5 flex-1 rounded-full", bar)} />}
            </div>
            <div className={cn("mt-1 text-[11.5px] leading-tight truncate", s.status === "TODO" ? "text-muted-foreground" : "text-foreground font-medium")}>
              {t(`orderTrack.step.${s.step}`)}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
