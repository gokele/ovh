import { useRef, useState } from "react";
import { LifeBuoy, AlertTriangle, Loader2 } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { LoadFailed } from "@/components/common/LoadFailed";
import { useRescueStatus, useEnterRescue, useExitRescue, type RescueStatus } from "@/hooks/use-server-control";
import { useAccountScope } from "@/hooks/use-account";
import { Trans, useTranslation } from "react-i18next";

type RescueDialogProps = {
  serviceName: string;
  displayName?: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
};

/** 每次打开或切换账户都创建独立会话,不复用上一次的邮箱和确认。 */
export function RescueDialog(props: RescueDialogProps) {
  const accountId = useAccountScope();
  return props.open ? <RescueSession key={`${accountId}:${props.serviceName}`} {...props} accountId={accountId} /> : null;
}

/** 新鲜状态读取完成前不展示可编辑表单,查询失败时不允许重启。 */
function RescueSession({ serviceName, displayName, onOpenChange, accountId }: RescueDialogProps & { accountId: string }) {
  const status = useRescueStatus(serviceName, accountId);
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  return (
    <Dialog open onOpenChange={(value) => { if (!busy) onOpenChange(value); }}>
      <DialogContent className="max-w-lg" onEscapeKeyDown={(e) => { if (busy) e.preventDefault(); }} onPointerDownOutside={(e) => { if (busy) e.preventDefault(); }}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><LifeBuoy className="h-4 w-4" />{t("ctrl.rescue.title")}</DialogTitle>
          <DialogDescription className="break-all font-mono">{displayName || serviceName}</DialogDescription>
        </DialogHeader>
        {status.isPending || status.isFetching ? (
          <div role="status" className="flex items-center gap-2 py-8 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />{t("ctrl.rescue.statusLoading")}</div>
        ) : status.isError ? (
          <LoadFailed compact icon={LifeBuoy} title={t("ctrl.rescue.loadFailed")} error={status.error} onRetry={() => status.refetch()} />
        ) : (
          <RescueForm key={status.dataUpdatedAt} serviceName={serviceName} accountId={accountId} status={status.data} onOpenChange={onOpenChange} onBusyChange={setBusy} />
        )}
      </DialogContent>
    </Dialog>
  );
}

/** 邮箱变化会取消确认;只有有效收信方式与独立勾选同时满足才发送重启请求。 */
function RescueForm({ serviceName, accountId, status, onOpenChange, onBusyChange }: {
  serviceName: string; accountId: string; status: RescueStatus;
  onOpenChange: (v: boolean) => void; onBusyChange: (v: boolean) => void;
}) {
  const { t } = useTranslation();
  const enter = useEnterRescue(serviceName, accountId);
  const exit = useExitRescue(serviceName, accountId);
  const [email, setEmail] = useState(status.rescueMail || "");
  const [useDefaultEmail, setUseDefaultEmail] = useState(false);
  const [exiting, setExiting] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  const submitted = useRef(false);
  const busy = enter.isPending || exit.isPending;
  const emailValid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());
  const recipientReady = useDefaultEmail || emailValid;

  // ref 在 React 重渲染前也能挡住连点;失败后需重新勾选,不自动重试。
  async function submit() {
    if (submitted.current || !confirmed || (!exiting && !recipientReady)) return;
    submitted.current = true;
    onBusyChange(true);
    try {
      if (exiting) await exit.mutateAsync({ expectedBoot: status.currentBoot, confirm: true });
      else await enter.mutateAsync({ email: useDefaultEmail ? undefined : email.trim(), useDefaultEmail, expectedBoot: status.currentBoot, confirm: true });
      onOpenChange(false);
    } catch {
      setConfirmed(false);
    } finally {
      submitted.current = false;
      onBusyChange(false);
    }
  }

  return <>
    <div className="space-y-4">
      <div className="rounded-xl border bg-muted/40 p-3.5 text-sm leading-relaxed">
        <p>{t("ctrl.rescue.currentStatus")}<b>{t(status.inRescue ? "ctrl.rescue.modeRescue" : "ctrl.rescue.modeNormal")}</b></p>
        <p className="mt-1 text-xs text-muted-foreground">{t("ctrl.rescue.configOnly")}</p>
        <p className="mt-2 text-xs text-muted-foreground"><Trans i18nKey="ctrl.rescue.whatIsRescue" components={{ b: <b /> }} /></p>
      </div>
      {exiting ? (
        <p className="text-sm leading-relaxed">{t("ctrl.rescue.exitHint")}</p>
      ) : <div className="space-y-3">
        <div>
          <label htmlFor="rescue-email" className="mb-1.5 block text-sm font-medium">{t("ctrl.rescue.mailLabel")}</label>
          <Input id="rescue-email" type="email" inputMode="email" autoComplete="email" value={email} disabled={busy || useDefaultEmail}
            onChange={(e) => { setEmail(e.target.value); setConfirmed(false); }} placeholder={t("ctrl.rescue.mailPlaceholder")}
            aria-invalid={!useDefaultEmail && email !== "" && !emailValid} aria-describedby="rescue-mail-hint" />
          <p id="rescue-mail-hint" className={`mt-1.5 text-xs ${email !== "" && !emailValid && !useDefaultEmail ? "text-destructive" : "text-muted-foreground"}`}>
            {t(email !== "" && !emailValid && !useDefaultEmail ? "ctrl.rescue.mailBad" : "ctrl.rescue.mailHint")}
          </p>
        </div>
        <div className="flex min-h-11 items-center gap-3">
          <Checkbox id="rescue-default-email" checked={useDefaultEmail} disabled={busy} onCheckedChange={(v) => { setUseDefaultEmail(v === true); setConfirmed(false); }} />
          <label htmlFor="rescue-default-email" className="cursor-pointer text-sm">{t("ctrl.rescue.useDefaultEmail")}</label>
        </div>
      </div>}
      <div className="flex items-start gap-2.5 rounded-xl border border-warning/40 bg-warning/5 p-3.5 text-sm leading-relaxed">
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
        <div><Trans i18nKey="ctrl.rescue.rebootWarn" components={{ b: <b /> }} /></div>
      </div>
      <div className="flex min-h-11 items-start gap-3 rounded-xl border p-3.5">
        <Checkbox id="rescue-confirm" className="mt-0.5" checked={confirmed} disabled={busy || (!exiting && !recipientReady)} onCheckedChange={(v) => setConfirmed(v === true)} />
        <label htmlFor="rescue-confirm" className="cursor-pointer text-sm leading-relaxed">{t(exiting ? "ctrl.rescue.exitAcknowledge" : "ctrl.rescue.enterAcknowledge", { email: useDefaultEmail ? t("ctrl.rescue.defaultRecipient") : email.trim() || t("ctrl.rescue.mailLabel") })}</label>
      </div>
    </div>
    <DialogFooter className="sm:flex-wrap">
      <Button type="button" variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>{t("common.close")}</Button>
      {status.inRescue && <Button type="button" variant="outline" disabled={busy} onClick={() => { setExiting(!exiting); setConfirmed(false); }}>{t(exiting ? "ctrl.rescue.backToRescue" : "ctrl.rescue.exitBtn")}</Button>}
      <Button type="button" variant="destructive" disabled={busy || !confirmed || (!exiting && !recipientReady)} onClick={submit}>
        {busy && <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />}{t(exiting ? "ctrl.rescue.confirmExitBtn" : "ctrl.rescue.confirmBtn")}
      </Button>
    </DialogFooter>
  </>;
}
