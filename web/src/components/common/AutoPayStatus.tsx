import { AlertTriangle, CheckCircle2, CreditCard, Loader2 } from "lucide-react";
import { useId } from "react";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
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

/**
 * 自动付款选项卡片:标题 + 说明 + 开关,打开后逐个账户显示默认支付方式是否可用。
 * 涉及真实扣款,开启时卡片转为提醒色;抢购弹窗、新建任务、监控自动下单统一使用。
 */
export function AutoPayOption({
  checked,
  onChange,
  accountIds,
  title,
  description,
  className,
}: {
  checked: boolean;
  onChange: (next: boolean) => void;
  accountIds: string[];
  title: string;
  description: string;
  className?: string;
}) {
  const id = useId();
  return (
    <div
      className={cn(
        "rounded-xl border p-3.5 transition-colors",
        checked ? "border-warning/50 bg-warning/[.06]" : "border-border bg-muted/30",
        className,
      )}
    >
      <div className="flex items-start gap-3">
        <span
          className={cn(
            "w-9 h-9 rounded-lg flex items-center justify-center shrink-0 transition-colors",
            checked ? "bg-warning/15 text-warning" : "bg-card text-muted-foreground ring-1 ring-border",
          )}
        >
          <CreditCard className="w-[18px] h-[18px]" aria-hidden />
        </span>
        <label htmlFor={id} className="min-w-0 flex-1 cursor-pointer">
          <span className="block text-[13.5px] font-semibold">{title}</span>
          <span className="block mt-0.5 text-[12px] leading-relaxed text-muted-foreground">{description}</span>
        </label>
        <Switch
          id={id}
          checked={checked}
          onCheckedChange={(v) => onChange(v === true)}
          className="mt-1 data-[state=checked]:bg-warning data-[state=unchecked]:bg-input"
        />
      </div>
      {checked && accountIds.filter(Boolean).length > 0 && (
        <div className="mt-2.5 pl-12 space-y-1">
          {accountIds.filter(Boolean).map((a) => (
            <AutoPayStatus key={a} accountId={a} enabled />
          ))}
        </div>
      )}
    </div>
  );
}
