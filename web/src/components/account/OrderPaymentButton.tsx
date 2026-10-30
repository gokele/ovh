import { useState } from "react";
import { useTranslation } from "react-i18next";
import { CreditCard, Loader2 } from "lucide-react";
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
}: {
  accountId: string;
  orderId: number;
  managerUrl?: string;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const info = useOrderPaymentInfo(accountId, orderId, open);
  const pay = usePayAccountOrder(accountId, orderId);
  const data = info.data;
  const methods = data?.methods || [];
  const methodKey = (m: (typeof methods)[number]) => `${m.type}:${m.id}`;
  const method = selected
    ? methods.find((m) => methodKey(m) === selected)
    : methods.find((m) => m.default) || methods[0];
  const ready =
    !!data &&
    data.status === "notPaid" &&
    !data.attempted &&
    !!method &&
    data.amount != null &&
    !info.isFetching &&
    !pay.isPending &&
    !pay.isSuccess;

  /** 关闭后清除确认,下一次打开必须重新核对。 */
  const changeOpen = (value: boolean) => {
    if (pay.isPending) return;
    setOpen(value);
    setSelected("");
    setConfirmed(false);
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
      <Button variant="outline" size="sm" onClick={() => changeOpen(true)}>
        <CreditCard className="w-3.5 h-3.5" />
        {t("account.payment.open")}
      </Button>
      <Dialog open={open} onOpenChange={changeOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {t("account.payment.title", { id: orderId })}
            </DialogTitle>
            <DialogDescription>
              {t("account.payment.description")}
            </DialogDescription>
          </DialogHeader>
          {info.isPending ? (
            <p role="status" className="text-sm text-muted-foreground">
              {t("account.payment.loading")}
            </p>
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
                <dl className="text-sm space-y-2">
                  <div className="flex justify-between gap-4">
                    <dt>{t("account.payment.account")}</dt>
                    <dd className="break-all">
                      {data.accountName || data.accountId}
                    </dd>
                  </div>
                  <div className="flex justify-between gap-4">
                    <dt>{t("account.payment.amount")}</dt>
                    <dd className="font-semibold tabular-nums">
                      {data.priceText ||
                        `${data.currency} ${data.amount ?? "—"}`}
                    </dd>
                  </div>
                </dl>
                {data.status !== "notPaid" ? (
                  <p role="status" className="text-sm">
                    {t("account.payment.notPayable")}
                  </p>
                ) : data.attempted ? (
                  <p role="status" className="text-sm text-warning">
                    {t("account.payment.attempted")}
                  </p>
                ) : !methods.length ? (
                  <p role="status" className="text-sm text-warning">
                    {t("account.payment.noMethods")}
                  </p>
                ) : (
                  <div className="space-y-2">
                    <label
                      htmlFor={`payment-method-${orderId}`}
                      className="text-sm font-medium"
                    >
                      {t("account.payment.method")}
                    </label>
                    <Select
                      value={method ? methodKey(method) : ""}
                      onValueChange={(v) => {
                        setSelected(v);
                        setConfirmed(false);
                      }}
                      disabled={pay.isPending || pay.isSuccess}
                    >
                      <SelectTrigger id={`payment-method-${orderId}`}>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {methods.map((m) => (
                          <SelectItem key={methodKey(m)} value={methodKey(m)}>
                            {m.label} · #{m.id}
                            {m.default
                              ? ` · ${t("account.payment.default")}`
                              : ""}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <label className="flex gap-2 items-start text-sm pt-2">
                      <Checkbox
                        checked={confirmed}
                        onCheckedChange={(v) => setConfirmed(v === true)}
                        disabled={!ready}
                      />
                      {t("account.payment.confirm")}
                    </label>
                  </div>
                )}
              </div>
            )
          )}
          {pay.isError && (
            <p role="alert" className="text-sm text-destructive break-words">
              {errorMessage(pay.error)}
            </p>
          )}
          {pay.isSuccess && (
            <p role="status" className="text-sm text-success">
              {t("account.payment.submitted")}
            </p>
          )}
          {managerUrl && (
            <a
              href={managerUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm underline underline-offset-4"
            >
              {t("account.orderOpenManager")}
            </a>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => changeOpen(false)}
              disabled={pay.isPending}
            >
              {t("common.close")}
            </Button>
            <Button onClick={submit} disabled={!ready || !confirmed}>
              {pay.isPending && <Loader2 className="w-4 h-4 animate-spin" />}
              {pay.isPending
                ? t("account.payment.submitting")
                : t("account.payment.pay", { amount: data?.priceText || "" })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
