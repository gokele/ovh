import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowRight, CheckCircle2 } from "lucide-react";
import { useHistory } from "@/hooks/use-history";
import { OrderPaymentButton } from "@/components/account/OrderPaymentButton";

const ORDER_VALIDITY_MS = 15 * 24 * 60 * 60 * 1000;

/**
 * 抢购队列顶部:抢到但还没付款的订单(最紧急的一单 + 其余数量)。
 * 付款走与抢购历史相同的预检 + 勾选确认流程,不会自动扣款;没有待付款时不显示。
 */
export function UnpaidBanner() {
  const { t } = useTranslation();
  const history = useHistory();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 60_000);
    return () => window.clearInterval(id);
  }, []);
  const rows = (history.data || [])
    .filter((h) => h.status === "success" && Number(h.orderId) > 0 && !!h.accountId && (!h.orderStatus || h.orderStatus === "notPaid"))
    .map((h) => ({ h, left: (h.expirationTime ? new Date(h.expirationTime).getTime() : new Date(h.purchaseTime).getTime() + ORDER_VALIDITY_MS) - now }))
    .filter((r) => r.left > 0)
    .sort((a, b) => a.left - b.left);
  if (rows.length === 0) return null;
  const { h, left } = rows[0];
  const m = Math.floor(left / 60_000);
  const d = Math.floor(m / 1440);
  const hh = Math.floor((m % 1440) / 60);
  const leftText = d > 0 ? t("history.countdown.dhm", { d, h: hh, m: m % 60 }) : hh > 0 ? t("history.countdown.hm", { h: hh, m: m % 60 }) : t("history.countdown.m", { m });
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border border-success/30 bg-success/[.08] px-4 py-3">
      <CheckCircle2 className="w-5 h-5 text-success shrink-0" aria-hidden />
      <div className="min-w-0 flex-1 text-[13.5px]">
        <span className="font-medium text-foreground">{t("queue.bundle.unpaid", { plan: h.planCode, dc: (h.datacenter || "").toUpperCase() })}</span>
        {h.price?.withTax != null && (
          <span className="ml-1.5 font-semibold tabular-nums">
            {h.price.withTax}
            {h.price.currencyCode ? ` ${h.price.currencyCode}` : ""}
          </span>
        )}
        <span className={left < 24 * 3600_000 ? "ml-2 text-destructive" : "ml-2 text-muted-foreground"}>{t("queue.bundle.unpaidLeft", { left: leftText })}</span>
        {rows.length > 1 && (
          <Link to="/history" className="ml-2 inline-flex items-center gap-0.5 text-[12.5px] text-primary hover:underline">
            {t("queue.bundle.unpaidMore", { n: rows.length - 1 })}
            <ArrowRight className="w-3 h-3" />
          </Link>
        )}
      </div>
      <OrderPaymentButton compact accountId={h.accountId} orderId={Number(h.orderId)} managerUrl={h.orderUrl} />
    </div>
  );
}
