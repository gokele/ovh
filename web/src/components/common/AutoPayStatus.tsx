import { AlertTriangle, CheckCircle2, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useDefaultPaymentMethod } from "@/hooks/use-account";
import { errorMessage } from "@/components/common/LoadFailed";

/**
 * 自动付款开关下方的状态行:显示该账户的默认支付方式是否可用。
 * OVH 只会用默认支付方式(status=VALID 且未过期)自动扣款;不可用时下单后订单保持待付款。
 */
export function AutoPayStatus({ accountId, enabled }: { accountId: string; enabled: boolean }) {
  const { t } = useTranslation();
  const q = useDefaultPaymentMethod(accountId, enabled);
  if (!enabled || !accountId) return null;
  if (q.isPending) {
    return (
      <p className="flex items-center gap-1.5 text-[11px] text-muted-foreground" role="status">
        <Loader2 className="w-3 h-3 animate-spin" aria-hidden />
        {t("commons.autoPay.checking")}
      </p>
    );
  }
  if (q.isError) {
    return (
      <p className="flex items-start gap-1.5 text-[11px] text-warning" role="status">
        <AlertTriangle className="w-3 h-3 mt-0.5 shrink-0" aria-hidden />
        {t("commons.autoPay.unknown", { reason: errorMessage(q.error) })}
      </p>
    );
  }
  const info = q.data;
  if (info?.usable) {
    return (
      <p className="flex items-start gap-1.5 text-[11px] text-muted-foreground" role="status">
        <CheckCircle2 className="w-3 h-3 mt-0.5 shrink-0 text-success" aria-hidden />
        {t("commons.autoPay.usable", { label: info.label || info.paymentType || "-" })}
      </p>
    );
  }
  return (
    <p className="flex items-start gap-1.5 text-[11px] text-destructive" role="alert">
      <AlertTriangle className="w-3 h-3 mt-0.5 shrink-0" aria-hidden />
      {t("commons.autoPay.unusable", { reason: info?.reason || "" })}
    </p>
  );
}
