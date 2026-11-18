import { useState } from "react";
import { fmtMoney } from "@/i18n/format";
import { useTranslation } from "react-i18next";
import { AlertTriangle, CheckCircle2, CreditCard, ExternalLink, Info, Loader2, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { LoadFailed, errorMessage } from "@/components/common/LoadFailed";
import { useOrderPaymentInfo, usePayAccountOrder } from "@/hooks/use-account";

/** 付款窗口只展示只读预检结果;明确确认后才调用付款 mutation。 */
export function OrderPaymentButton({
  accountId,
  orderId,
  managerUrl,
  compact,
}: {
  accountId: string;
  orderId: number;
  managerUrl?: string;
  /** 表格里用的小号按钮(抢购历史操作列) */
  compact?: boolean;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState("");
  const [confirmedQuote, setConfirmedQuote] = useState("");
  const info = useOrderPaymentInfo(accountId, orderId, open);
  const pay = usePayAccountOrder(accountId, orderId);
  const data = info.data;
  const methods = data?.methods || [];
  const methodKey = (m: (typeof methods)[number]) => `${m.type}:${m.id}`;
  const method = selected
    ? methods.find((m) => methodKey(m) === selected)
    : methods.find((m) => m.default) || methods[0];
  // 确认绑定实际账户、订单、金额、币种及方式;后台刷新改变任一项都需重新勾选。
  const quote =
    data && method
      ? JSON.stringify([
          data.accountId,
          data.orderId,
          data.amount,
          data.currency,
          method.id,
          method.type,
        ])
      : "";
  const confirmed = !!quote && confirmedQuote === quote;
  const displayAmount =
    data?.amount != null && data.currency
      ? fmtMoney(data.amount, data.currency)
      : data?.priceText || "—";
  const ready =
    !!data &&
    data.status === "notPaid" &&
    !data.attempted &&
    !!method &&
    data.amount != null &&
    Number.isFinite(data.amount) &&
    data.orderId === orderId &&
    !info.isFetching &&
    !pay.isPending &&
    !pay.isSuccess;

  /** 关闭后清除确认,下一次打开必须重新核对。 */
  const changeOpen = (value: boolean) => {
    if (pay.isPending) return;
    setOpen(value);
    setSelected("");
    setConfirmedQuote("");
    pay.reset();
  };
  /** 捕获付款结果供窗口呈现,不会自动重发。 */
  const submit = async () => {
    if (!ready || !confirmed || !method || !data || data.amount == null) return;
    try {
      await pay.mutateAsync({
        accountId: data.accountId,
        paymentMethodId: method.id,
        paymentType: method.type,
        amount: data.amount,
        currency: data.currency,
        confirm: true,
      });
    } catch {
      /* Inline error retains the order context. */
    }
  };
  return (
    <>
      <Button
        variant={compact ? "default" : "outline"}
        size="sm"
        className={compact ? "h-7 px-2.5 gap-1 text-[12px]" : undefined}
        onClick={() => changeOpen(true)}
      >
        <CreditCard className="w-3.5 h-3.5" />
        {compact ? t("history.pay") : t("account.payment.open")}
      </Button>
      <Dialog open={open} onOpenChange={changeOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="pr-6 leading-7">
              {t("account.payment.title", { id: orderId })}
            </DialogTitle>
            <DialogDescription>
              {t("account.payment.description")}
            </DialogDescription>
          </DialogHeader>
          {info.isPending ? (
            <div role="status" className="flex items-center gap-2 rounded-xl bg-muted/50 px-4 py-6 text-sm text-muted-foreground">
              <Loader2 className="w-4 h-4 animate-spin motion-reduce:animate-none" />
              {t("account.payment.loading")}
            </div>
          ) : info.isError ? (
            <LoadFailed
              title={t("account.payment.loadFailed")}
              error={info.error}
              onRetry={() => info.refetch()}
              compact
            />
          ) : (
            data && (
              <div className="space-y-4">
                {/* 金额卡:金额是这一步最要紧的信息;刷新收成右上角的小图标 */}
                <div className="rounded-xl border border-border bg-muted/40 px-4 py-3.5">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-[12.5px] text-muted-foreground">{t("account.payment.amount")}</span>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7 -mr-1.5"
                      onClick={() => info.refetch()}
                      disabled={info.isFetching || pay.isPending || pay.isSuccess}
                      aria-label={t("account.payment.reload")}
                      title={t("account.payment.reload")}
                    >
                      <RefreshCw
                        aria-hidden="true"
                        className={`w-3.5 h-3.5 ${info.isFetching ? "animate-spin motion-reduce:animate-none" : ""}`}
                      />
                    </Button>
                  </div>
                  <div className="mt-0.5 text-[26px] leading-8 font-semibold tracking-tight tabular-nums">{displayAmount}</div>
                  <div className="mt-2 flex items-center justify-between gap-3 text-[12.5px]">
                    <span className="text-muted-foreground">{t("account.payment.account")}</span>
                    <span className="font-medium break-all text-right">{data.accountName || data.accountId}</span>
                  </div>
                </div>

                {data.status !== "notPaid" ? (
                  <Notice tone="muted">{t("account.payment.notPayable")}</Notice>
                ) : data.attempted ? (
                  <Notice tone="warning">{t("account.payment.attempted")}</Notice>
                ) : !methods.length ? (
                  <Notice tone="warning">{t("account.payment.noMethods")}</Notice>
                ) : (
                  <div className="space-y-3">
                    <div className="space-y-1.5">
                      <label htmlFor={`payment-method-${orderId}`} className="text-[13px] font-medium">
                        {t("account.payment.method")}
                      </label>
                      <Select
                        value={method ? methodKey(method) : ""}
                        onValueChange={(v) => {
                          setSelected(v);
                          setConfirmedQuote("");
                        }}
                        disabled={pay.isPending || pay.isSuccess}
                      >
                        <SelectTrigger className="h-10" id={`payment-method-${orderId}`}>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {methods.map((m) => (
                            <SelectItem key={methodKey(m)} value={methodKey(m)}>
                              {m.label} · #{m.id}
                              {m.default ? ` · ${t("account.payment.default")}` : ""}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                    <label
                      className={`flex cursor-pointer items-start gap-3 rounded-xl border p-3.5 text-[13px] leading-5 transition-colors ${
                        confirmed ? "border-primary/50 bg-primary/[.05]" : "border-border hover:bg-muted/40"
                      }`}
                    >
                      <Checkbox
                        className="mt-0.5"
                        checked={confirmed}
                        onCheckedChange={(v) => setConfirmedQuote(v === true ? quote : "")}
                        disabled={!ready}
                      />
                      {t("account.payment.confirm")}
                    </label>
                  </div>
                )}
              </div>
            )
          )}
          {pay.isError && <Notice tone="danger">{errorMessage(pay.error)}</Notice>}
          {pay.isSuccess && <Notice tone="success">{t("account.payment.submitted")}</Notice>}
          <DialogFooter className="sm:items-center gap-2">
            {managerUrl && (
              <a
                href={managerUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="sm:mr-auto inline-flex items-center gap-1 text-[12.5px] text-muted-foreground hover:text-foreground"
              >
                {t("account.orderOpenManager")}
                <ExternalLink className="w-3 h-3" />
              </a>
            )}
            <Button variant="outline" onClick={() => changeOpen(false)} disabled={pay.isPending}>
              {t("common.close")}
            </Button>
            <Button onClick={submit} disabled={!ready || !confirmed}>
              {pay.isPending && <Loader2 className="w-4 h-4 animate-spin motion-reduce:animate-none" />}
              {pay.isPending ? t("account.payment.submitting") : t("account.payment.pay", { amount: displayAmount })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

/** 弹窗里的状态提示条:图标 + 文字,按语气着色 */
function Notice({ tone, children }: { tone: "muted" | "warning" | "danger" | "success"; children: React.ReactNode }) {
  const cls = {
    muted: "border-border bg-muted/40 text-foreground/80",
    warning: "border-warning/40 bg-warning/[.07] text-warning",
    danger: "border-destructive/40 bg-destructive/[.06] text-destructive",
    success: "border-success/40 bg-success/[.07] text-success",
  }[tone];
  const Icon = tone === "success" ? CheckCircle2 : tone === "muted" ? Info : AlertTriangle;
  return (
    <div role={tone === "danger" ? "alert" : "status"} className={`flex items-start gap-2 rounded-xl border px-3.5 py-2.5 text-[13px] leading-5 break-words ${cls}`}>
      <Icon className="w-4 h-4 mt-0.5 shrink-0" aria-hidden />
      <span className="min-w-0">{children}</span>
    </div>
  );
}
