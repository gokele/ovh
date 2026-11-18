import { useRef, useState } from "react";
import { fmtMoney } from "@/i18n/format";
import { useTranslation } from "react-i18next";
import { AlertTriangle, CheckCircle2, CreditCard, ExternalLink, Info, Loader2, RefreshCw, Wallet } from "lucide-react";
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
  // 同步闸门:连点两下时 isPending 还没渲染出来,靠它保证一次打开只发一次付款请求
  const submittingRef = useRef(false);
  const info = useOrderPaymentInfo(accountId, orderId, open);
  const pay = usePayAccountOrder(accountId, orderId);
  const data = info.data;
  const methods = data?.methods || [];
  const balances = data?.balances || [];
  const methodKey = (m: (typeof methods)[number]) => `m:${m.type}:${m.id}`;
  const balanceKey = (b: (typeof balances)[number]) => `b:${b.name}`;
  // 可选项:绑定的支付方式 + 足够付清整单的余额(不做部分抵扣)
  type Choice =
    | { kind: "method"; key: string; m: (typeof methods)[number] }
    | { kind: "balance"; key: string; b: (typeof balances)[number] };
  const choices: Choice[] = [
    ...methods.map((m) => ({ kind: "method" as const, key: methodKey(m), m })),
    ...balances.filter((b) => b.enough).map((b) => ({ kind: "balance" as const, key: balanceKey(b), b })),
  ];
  const choice = selected
    ? choices.find((c) => c.key === selected)
    : choices.find((c) => c.kind === "method" && c.m.default) || choices[0];
  const balanceTypeLabel = (type: string) => t(`account.payment.balanceType.${type}`, { defaultValue: type });
  // 确认绑定实际账户、订单、金额、币种及所选方式/余额;后台刷新改变任一项都需重新勾选。
  const quote =
    data && choice
      ? JSON.stringify([
          data.accountId,
          data.orderId,
          data.amount,
          data.currency,
          choice.kind === "method" ? [choice.m.id, choice.m.type] : [choice.b.name, choice.b.amount, choice.b.currency],
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
    !!choice &&
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
    submittingRef.current = false;
    pay.reset();
  };
  /** 捕获付款结果供窗口呈现,不会自动重发。 */
  const submit = async () => {
    if (!ready || !confirmed || !choice || !data || data.amount == null || submittingRef.current) return;
    submittingRef.current = true;
    try {
      await pay.mutateAsync(
        choice.kind === "balance"
          ? { kind: "balance", accountId: data.accountId, balanceName: choice.b.name, amount: data.amount, currency: data.currency, confirm: true }
          : {
              kind: "method",
              accountId: data.accountId,
              paymentMethodId: choice.m.id,
              paymentType: choice.m.type,
              amount: data.amount,
              currency: data.currency,
              confirm: true,
            },
      );
    } catch {
      /* Inline error retains the order context. 明确被拒可重新确认;结果不明时服务端登记会拦住重发。 */
      submittingRef.current = false;
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
                ) : !choices.length ? (
                  <Notice tone="warning">{t("account.payment.noMethods")}</Notice>
                ) : (
                  <div className="space-y-3">
                    {/* 选项卡片:绑定的支付方式在上,账户余额在下;余额不足 / 币种不同的灰显不可选 */}
                    <div className="space-y-2">
                      <div id={`payment-method-${orderId}`} className="text-[13px] font-medium">{t("account.payment.method")}</div>
                      <div role="radiogroup" aria-labelledby={`payment-method-${orderId}`} className="space-y-2 max-h-[min(50vh,420px)] overflow-y-auto overscroll-contain -mx-1 px-1 py-0.5">
                        {methods.length > 0 && balances.length > 0 && (
                          <div className="text-[11.5px] text-muted-foreground">{t("account.payment.groupMethods")}</div>
                        )}
                        {methods.map((m) => (
                          <PayOption
                            key={methodKey(m)}
                            icon={<CreditCard className="w-4 h-4" />}
                            title={m.label}
                            sub={`#${m.id}${m.default ? ` · ${t("account.payment.default")}` : ""}`}
                            selected={choice?.key === methodKey(m)}
                            disabled={pay.isPending || pay.isSuccess}
                            onSelect={() => {
                              setSelected(methodKey(m));
                              setConfirmedQuote("");
                            }}
                          />
                        ))}
                        {balances.length > 0 && (
                          <div className={`text-[11.5px] text-muted-foreground ${methods.length ? "pt-1" : ""}`}>{t("account.payment.groupBalances")}</div>
                        )}
                        {balances.map((b) => {
                          const amount = Number.isFinite(b.amount) && b.currency ? fmtMoney(b.amount, b.currency) : b.text;
                          const left =
                            b.enough && data.amount != null && b.currency === data.currency
                              ? fmtMoney(Math.max(0, b.amount - data.amount), b.currency)
                              : "";
                          return (
                            <PayOption
                              key={balanceKey(b)}
                              icon={<Wallet className="w-4 h-4" />}
                              tone="balance"
                              title={balanceTypeLabel(b.type)}
                              sub={
                                b.enough
                                  ? t("account.payment.balanceLeft", { amount, left })
                                  : `${amount} · ${b.currency !== data.currency ? t("account.payment.balanceCurrency") : t("account.payment.balanceShort")}`
                              }
                              selected={choice?.key === balanceKey(b)}
                              disabled={!b.enough || pay.isPending || pay.isSuccess}
                              onSelect={() => {
                                setSelected(balanceKey(b));
                                setConfirmedQuote("");
                              }}
                            />
                          );
                        })}
                      </div>
                    </div>
                    {choice?.kind === "balance" && (
                      <p className="text-[12px] leading-5 text-muted-foreground">{t("account.payment.balanceNote")}</p>
                    )}
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
                      {choice?.kind === "balance"
                        ? t("account.payment.balanceConfirm", { name: balanceTypeLabel(choice.b.type) })
                        : t("account.payment.confirm")}
                    </label>
                  </div>
                )}
                {data.status === "notPaid" && !data.attempted && data.balanceError && (
                  <p className="text-[12px] leading-5 text-muted-foreground">{t("account.payment.balanceError", { detail: data.balanceError })}</p>
                )}
              </div>
            )
          )}
          {pay.isError && <Notice tone="danger">{errorMessage(pay.error)}</Notice>}
          {pay.isSuccess && (
            <Notice tone="success">
              {pay.variables?.kind === "balance"
                ? pay.data?.status === "paid"
                  ? t("account.payment.balancePaid", { status: pay.data.orderStatus || "" })
                  : t("account.payment.balanceSubmitted")
                : t("account.payment.submitted")}
            </Notice>
          )}
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

/** 付款选项卡片:图标 + 名称 + 说明 + 右侧单选圆点;键盘可用(role=radio) */
function PayOption({
  icon,
  title,
  sub,
  selected,
  disabled,
  onSelect,
  tone,
}: {
  icon: React.ReactNode;
  title: string;
  sub: string;
  selected: boolean;
  disabled?: boolean;
  onSelect: () => void;
  tone?: "balance";
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      disabled={disabled}
      onClick={onSelect}
      className={`w-full flex items-center gap-3 rounded-xl border px-3.5 py-2.5 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed ${
        selected
          ? "border-primary/60 bg-primary/[.05] ring-1 ring-primary/25"
          : "border-border hover:bg-muted/40 disabled:hover:bg-transparent"
      } ${disabled && !selected ? "opacity-55" : ""}`}
    >
      <span
        className={`w-9 h-9 rounded-lg flex items-center justify-center shrink-0 ${
          tone === "balance" ? "bg-success/10 text-success" : "bg-primary/10 text-primary"
        }`}
        aria-hidden
      >
        {icon}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-[13.5px] font-medium truncate">{title}</span>
        <span className="block text-[12px] text-muted-foreground truncate tabular-nums">{sub}</span>
      </span>
      <span
        className={`w-4 h-4 rounded-full border-2 shrink-0 flex items-center justify-center ${selected ? "border-primary" : "border-muted-foreground/40"}`}
        aria-hidden
      >
        {selected && <span className="w-2 h-2 rounded-full bg-primary" />}
      </span>
    </button>
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
