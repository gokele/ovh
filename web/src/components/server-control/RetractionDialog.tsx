import { useRef, useState } from "react";
import { AlertTriangle, Undo2, Loader2 } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { useRequestRetraction, type RetractionInfo } from "@/hooks/use-server-control";
import { useAccountScope } from "@/hooks/use-account";
import { Trans, useTranslation } from "react-i18next";
import { fmtDate, fmtDateTime } from "@/i18n/format";

type RetractionDialogProps = {
  serviceName: string; displayName: string; info: RetractionInfo;
  open: boolean; onOpenChange: (v: boolean) => void;
};

/** 与官网一致:说明、独立勾选、确认;后台固定传 reason:other。 */
export function RetractionDialog(props: RetractionDialogProps) {
  const accountId = useAccountScope();
  return props.open && props.info.accountId === accountId && props.info.orderId ? (
    <RetractionSession key={`${accountId}:${props.info.orderId}`} {...props} accountId={accountId} />
  ) : null;
}

/** 确认与页面显示的账户和订单绑定,连点与请求失败均不自动重试。 */
function RetractionSession({ serviceName, displayName, info, onOpenChange, accountId }: RetractionDialogProps & { accountId: string }) {
  const { t } = useTranslation();
  const request = useRequestRetraction(serviceName, accountId);
  const [confirmed, setConfirmed] = useState(false);
  const submitted = useRef(false);
  const expired = !info.retractionDate || !Number.isFinite(Date.parse(info.retractionDate)) || Date.parse(info.retractionDate) <= Date.now();

  // 服务端会再次检查撤回期限与服务归属,这里仅防止误触和重复提交。
  async function submit() {
    if (!confirmed || expired || submitted.current || !info.orderId) return;
    submitted.current = true;
    try {
      await request.mutateAsync({ orderId: info.orderId, confirm: true });
      onOpenChange(false);
    } catch {
      setConfirmed(false);
    } finally {
      submitted.current = false;
    }
  }

  return (
    <Dialog open onOpenChange={(v) => { if (!request.isPending) onOpenChange(v); }}>
      <DialogContent className="max-w-lg" onEscapeKeyDown={(e) => { if (request.isPending) e.preventDefault(); }} onPointerDownOutside={(e) => { if (request.isPending) e.preventDefault(); }}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Undo2 className="h-4 w-4" />{t("maint.retraction.title")}</DialogTitle>
          <DialogDescription className="break-all font-mono">{displayName}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 text-sm leading-relaxed">
          <div className="rounded-xl border bg-muted/40 p-3.5">
            <p className="font-medium">{t("maint.retraction.order", { id: info.orderId })}</p>
            {info.retractionDate && <p className="mt-1 text-xs text-muted-foreground">{t("maint.retraction.deadline", { date: fmtDateTime(info.retractionDate) })}</p>}
            {info.orderDate && <p className="mt-1 text-xs text-muted-foreground">{t("maint.retraction.deadlineNote", { date: fmtDate(info.orderDate) })}</p>}
            {info.orderUrl && <a href={info.orderUrl} target="_blank" rel="noopener noreferrer" className="mt-2 inline-block text-xs underline underline-offset-4">{t("maint.retraction.viewOrder")}</a>}
          </div>
          <p>{t("maint.retraction.refund")}</p>
          <p className="font-medium">{t("maint.retraction.scope")}</p>
          <div className="flex items-start gap-2.5 rounded-xl border border-destructive/40 bg-destructive/5 p-3.5">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
            <div><Trans i18nKey="maint.retraction.warn" components={{ b: <b /> }} /></div>
          </div>
          <div className="flex min-h-11 items-start gap-3 rounded-xl border p-3.5">
            <Checkbox id="withdraw-confirm" className="mt-0.5" checked={confirmed} disabled={request.isPending || expired} onCheckedChange={(v) => setConfirmed(v === true)} />
            <label htmlFor="withdraw-confirm" className="cursor-pointer">{t("maint.retraction.acknowledge", { id: info.orderId })}</label>
          </div>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" disabled={request.isPending} onClick={() => onOpenChange(false)}>{t("common.cancel")}</Button>
          <Button type="button" variant="destructive" disabled={!confirmed || request.isPending || expired} onClick={submit}>
            {request.isPending && <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />}{t(request.isPending ? "maint.retraction.submitting" : "maint.retraction.confirmBtn")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
