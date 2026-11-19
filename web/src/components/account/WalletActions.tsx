import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckCircle2, Gift, Loader2, Plus, ExternalLink } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { LoadFailed, errorMessage } from "@/components/common/LoadFailed";
import { OrderPaymentButton } from "@/components/account/OrderPaymentButton";
import { fmtMoney } from "@/i18n/format";
import { useCreateTopup, useRedeemCode, useTopupInfo } from "@/hooks/use-account";

const QUICK_AMOUNTS = [20, 50, 100, 200, 500];

/** 钱包卡上的两个按钮:充值(只在支持预付款账户的 EU/CA 显示)与兑换券码 */
export function WalletActions({ accountId }: { accountId: string }) {
  const { t } = useTranslation();
  const info = useTopupInfo(accountId);
  const [topupOpen, setTopupOpen] = useState(false);
  const [redeemOpen, setRedeemOpen] = useState(false);
  const canTopup = !!info.data?.supported && (info.data.accounts || []).some((a) => a.canBeCredited);
  const btn =
    "inline-flex items-center gap-1.5 h-8 px-3 rounded-full text-[12.5px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/70";
  return (
    <>
      <div className="mt-3 flex flex-wrap gap-2">
        {canTopup && (
          <button type="button" className={`${btn} bg-white text-[#1f5fe6] hover:bg-white/90`} onClick={() => setTopupOpen(true)}>
            <Plus className="w-3.5 h-3.5" aria-hidden />
            {t("account.topup.open")}
          </button>
        )}
        <button type="button" className={`${btn} bg-white/15 text-white hover:bg-white/25`} onClick={() => setRedeemOpen(true)}>
          <Gift className="w-3.5 h-3.5" aria-hidden />
          {t("account.redeem.open")}
        </button>
      </div>
      {topupOpen && <TopupDialog accountId={accountId} onClose={() => setTopupOpen(false)} />}
      {redeemOpen && <RedeemDialog accountId={accountId} onClose={() => setRedeemOpen(false)} />}
    </>
  );
}

/** 充值:选金额 → 勾选确认 → 生成待付款充值订单 → 在同一窗口里直接付款(复用订单付款窗口,再确认一次) */
function TopupDialog({ accountId, onClose }: { accountId: string; onClose: () => void }) {
  const { t } = useTranslation();
  const info = useTopupInfo(accountId);
  const create = useCreateTopup(accountId);
  const target = (info.data?.accounts || []).find((a) => a.canBeCredited);
  const currency = target?.currency || "";
  const [amount, setAmount] = useState("");
  const [confirmedFor, setConfirmedFor] = useState("");
  const sending = useRef(false);
  const value = Number(amount);
  const valid = amount.trim() !== "" && Number.isFinite(value) && value > 0 && value <= 100000 && /^\d+(\.\d{1,2})?$/.test(amount.trim());
  const quote = target && valid ? `${target.id}:${value}` : "";
  const confirmed = !!quote && confirmedFor === quote;
  const money = (v: number) => (currency ? fmtMoney(v, currency) : String(v));
  const order = create.data;

  const submit = async () => {
    if (!target || !confirmed || sending.current || create.isPending || order) return;
    sending.current = true;
    try {
      await create.mutateAsync({ ovhAccountId: target.id, amount: value });
    } catch {
      sending.current = false;
    }
  };

  return (
    <Dialog open onOpenChange={(v) => !v && !create.isPending && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("account.topup.title")}</DialogTitle>
          <DialogDescription>{t("account.topup.description")}</DialogDescription>
        </DialogHeader>
        {info.isPending ? (
          <div role="status" className="flex items-center gap-2 rounded-xl bg-muted/50 px-4 py-6 text-sm text-muted-foreground">
            <Loader2 className="w-4 h-4 animate-spin motion-reduce:animate-none" />
            {t("account.topup.loading")}
          </div>
        ) : info.isError ? (
          <LoadFailed title={t("account.topup.loadFailed")} error={info.error} onRetry={() => info.refetch()} compact />
        ) : !target ? (
          <p className="rounded-xl border border-border bg-muted/40 px-4 py-3 text-[13px]">{t("account.topup.unavailable")}</p>
        ) : order ? (
          // 订单已生成:展示订单号与金额,直接在这里发起付款
          <div className="space-y-3">
            <div className="flex items-start gap-2.5 rounded-xl border border-success/40 bg-success/[.07] px-4 py-3 text-[13px] text-success">
              <CheckCircle2 className="w-4 h-4 mt-0.5 shrink-0" aria-hidden />
              <span>{t("account.topup.created", { id: order.orderId })}</span>
            </div>
            <div className="rounded-xl border border-border bg-muted/40 px-4 py-3.5">
              <div className="text-[12.5px] text-muted-foreground">{t("account.topup.orderAmount")}</div>
              <div className="mt-0.5 text-[24px] leading-8 font-semibold tabular-nums">
                {order.amount != null && order.currency ? fmtMoney(order.amount, order.currency) : order.priceText || "—"}
              </div>
              <div className="mt-1 text-[12px] text-muted-foreground">{t("account.topup.payHint")}</div>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <OrderPaymentButton accountId={order.accountId || accountId} orderId={order.orderId} managerUrl={order.url} />
              {order.url && (
                <a href={order.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-[12.5px] text-muted-foreground hover:text-foreground">
                  {t("account.orderOpenManager")}
                  <ExternalLink className="w-3 h-3" />
                </a>
              )}
            </div>
          </div>
        ) : (
          <div className="space-y-4">
            <div className="flex items-center justify-between gap-3 rounded-xl border border-border bg-muted/40 px-4 py-3 text-[13px]">
              <span className="text-muted-foreground">{t("account.topup.current")}</span>
              <span className="font-semibold tabular-nums">
                {target.balance != null && currency ? money(target.balance) : target.balanceText || "—"}
              </span>
            </div>
            <div className="space-y-2">
              <label htmlFor="topup-amount" className="text-[13px] font-medium">
                {t("account.topup.amount")}
                {currency && <span className="ml-1 text-muted-foreground font-normal">({currency})</span>}
              </label>
              <div className="flex flex-wrap gap-1.5" role="group" aria-label={t("account.topup.quick")}>
                {QUICK_AMOUNTS.map((v) => (
                  <button
                    key={v}
                    type="button"
                    onClick={() => {
                      setAmount(String(v));
                      setConfirmedFor("");
                    }}
                    disabled={create.isPending}
                    className={`h-8 px-3 rounded-full border text-[12.5px] tabular-nums transition-colors ${
                      amount === String(v) ? "border-primary bg-primary/10 text-primary font-medium" : "border-border hover:bg-muted/60"
                    }`}
                  >
                    {money(v)}
                  </button>
                ))}
              </div>
              <Input
                id="topup-amount"
                inputMode="decimal"
                value={amount}
                onChange={(e) => {
                  setAmount(e.target.value);
                  setConfirmedFor("");
                }}
                placeholder={t("account.topup.placeholder")}
                disabled={create.isPending}
                aria-invalid={amount !== "" && !valid}
              />
              {amount !== "" && !valid && <p className="text-[12px] text-destructive">{t("account.topup.invalid")}</p>}
            </div>
            <label
              className={`flex cursor-pointer items-start gap-3 rounded-xl border p-3.5 text-[13px] leading-5 transition-colors ${
                confirmed ? "border-primary/50 bg-primary/[.05]" : "border-border hover:bg-muted/40"
              }`}
            >
              <Checkbox className="mt-0.5" checked={confirmed} disabled={!quote || create.isPending} onCheckedChange={(v) => setConfirmedFor(v === true ? quote : "")} />
              {t("account.topup.confirm", { amount: valid ? money(value) : "—" })}
            </label>
          </div>
        )}
        {create.isError && (
          <p role="alert" className="rounded-xl border border-destructive/40 bg-destructive/[.06] px-3.5 py-2.5 text-[13px] text-destructive break-words">
            {errorMessage(create.error)}
          </p>
        )}
        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={onClose} disabled={create.isPending}>
            {t("common.close")}
          </Button>
          {target && !order && (
            <Button onClick={submit} disabled={!confirmed || create.isPending}>
              {create.isPending && <Loader2 className="w-4 h-4 animate-spin motion-reduce:animate-none" />}
              {t("account.topup.create")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 兑换券码:输入 → 兑换 → 显示到账金额与余额名称 */
function RedeemDialog({ accountId, onClose }: { accountId: string; onClose: () => void }) {
  const { t } = useTranslation();
  const redeem = useRedeemCode(accountId);
  const [code, setCode] = useState("");
  const sending = useRef(false);
  const trimmed = code.trim();
  const valid = trimmed.length > 0 && trimmed.length <= 128 && !/\s/.test(trimmed);
  const res = redeem.data;

  const submit = async () => {
    if (!valid || sending.current || redeem.isPending || res) return;
    sending.current = true;
    try {
      await redeem.mutateAsync(trimmed);
    } catch {
      sending.current = false;
    }
  };

  return (
    <Dialog open onOpenChange={(v) => !v && !redeem.isPending && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("account.redeem.title")}</DialogTitle>
          <DialogDescription>{t("account.redeem.description")}</DialogDescription>
        </DialogHeader>
        {res ? (
          <div className="flex items-start gap-2.5 rounded-xl border border-success/40 bg-success/[.07] px-4 py-3 text-[13px] text-success">
            <CheckCircle2 className="w-4 h-4 mt-0.5 shrink-0" aria-hidden />
            <span>
              {t("account.redeem.done", {
                amount: res.amount != null && res.currency ? fmtMoney(res.amount, res.currency) : res.text || "—",
                name: t(`account.payment.balanceType.${res.type}`, { defaultValue: res.type || res.balanceName }),
              })}
            </span>
          </div>
        ) : (
          <form
            className="space-y-2"
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            <label htmlFor="redeem-code" className="text-[13px] font-medium">
              {t("account.redeem.code")}
            </label>
            <Input
              id="redeem-code"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder={t("account.redeem.placeholder")}
              autoCapitalize="characters"
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
              disabled={redeem.isPending}
            />
            <p className="text-[12px] text-muted-foreground">{t("account.redeem.hint")}</p>
          </form>
        )}
        {redeem.isError && (
          <p role="alert" className="rounded-xl border border-destructive/40 bg-destructive/[.06] px-3.5 py-2.5 text-[13px] text-destructive break-words">
            {errorMessage(redeem.error)}
          </p>
        )}
        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={onClose} disabled={redeem.isPending}>
            {t("common.close")}
          </Button>
          {!res && (
            <Button onClick={() => void submit()} disabled={!valid || redeem.isPending}>
              {redeem.isPending && <Loader2 className="w-4 h-4 animate-spin motion-reduce:animate-none" />}
              {t("account.redeem.submit")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
