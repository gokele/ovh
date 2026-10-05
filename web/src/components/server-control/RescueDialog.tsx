import { useEffect, useState } from "react";
import { LifeBuoy, AlertTriangle, Loader2 } from "lucide-react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { LoadFailed } from "@/components/common/LoadFailed";
import { useRescueStatus, useEnterRescue, useExitRescue } from "@/hooks/use-server-control";
import { Trans, useTranslation } from "react-i18next";

/**
 * 一键救援系统。
 *
 * 手动做这件事要在 OVH 后台点四步:进服务器 → 改 netboot 为救援 → 填收密码的邮箱
 * → 回去重启服务器。漏掉最后一步是最常见的错误 —— netboot 改了但没重启,
 * 机器还在正常系统里跑,用户对着 SSH 连不上发懵。
 * 这里把三个 API 调用合成一个按钮,并且明确告诉用户重启已经发出去了。
 */
export function RescueDialog({
  serviceName,
  displayName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  displayName?: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const status = useRescueStatus(serviceName, open);
  const enter = useEnterRescue(serviceName);
  const exit = useExitRescue(serviceName);
  const { t } = useTranslation();
  const [email, setEmail] = useState("");
  const [confirming, setConfirming] = useState(false);

  // 每次打开都重置二次确认,并把上次用过的邮箱带出来省得重填
  useEffect(() => {
    if (!open) return;
    setConfirming(false);
    setEmail(status.data?.rescueMail || "");
  }, [open, status.data?.rescueMail]);

  const inRescue = status.data?.inRescue === true;
  const busy = enter.isPending || exit.isPending;
  // 邮箱可以留空(用账户默认联系邮箱),填了就得像个邮箱 —— 填错等于收不到密码
  const emailBad = email.trim() !== "" && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <LifeBuoy className="w-4 h-4" />
            {t("ctrl.rescue.title")}
          </DialogTitle>
          <DialogDescription>
            {displayName ? <span className="font-mono">{displayName}</span> : serviceName}
          </DialogDescription>
        </DialogHeader>

        {status.isError ? (
          <LoadFailed
            compact
            icon={LifeBuoy}
            title={t("ctrl.rescue.loadFailed")}
            error={status.error}
            onRetry={() => status.refetch()}
          />
        ) : (
          <div className="space-y-4">
            <div className="rounded-xl border border-border bg-muted/40 px-3.5 py-3">
              <p className="text-[12px]">
                {t("ctrl.rescue.currentStatus")}
                {status.isPending ? (
                  <span className="text-muted-foreground">{t("ctrl.rescue.statusLoading")}</span>
                ) : inRescue ? (
                  <b className="text-warning">{t("ctrl.rescue.modeRescue")}</b>
                ) : (
                  <b>{t("ctrl.rescue.modeNormal")}</b>
                )}
              </p>
              <p className="text-[11px] text-muted-foreground mt-1 leading-relaxed">
                <Trans i18nKey="ctrl.rescue.whatIsRescue" components={{ b: <b /> }} />
              </p>
            </div>

            {inRescue ? (
              <div className="rounded-xl border border-border px-3.5 py-3 text-[12px] leading-relaxed">
                <Trans i18nKey="ctrl.rescue.alreadyInRescue" components={{ b: <b /> }} />
              </div>
            ) : (
              <>
                <div>
                  <label className="block text-[13px] font-medium mb-1.5">
                    {t("ctrl.rescue.mailLabel")}
                    <span className="text-muted-foreground font-normal">{t("ctrl.rescue.optional")}</span>
                  </label>
                  <Input
                    value={email}
                    onChange={(e) => { setEmail(e.target.value); setConfirming(false); }}
                    placeholder={t("ctrl.rescue.mailPlaceholder")}
                    inputMode="email"
                  />
                  {emailBad ? (
                    <p className="text-[11px] text-destructive mt-1">
                      {t("ctrl.rescue.mailBad")}
                    </p>
                  ) : (
                    <p className="text-[11px] text-muted-foreground mt-1">
                      {t("ctrl.rescue.mailHint")}
                    </p>
                  )}
                </div>

                <div className="flex items-start gap-2.5 rounded-xl border border-warning/40 bg-warning/5 px-3.5 py-3">
                  <AlertTriangle className="w-4 h-4 text-warning mt-0.5 flex-shrink-0" />
                  <div className="text-[12px] leading-relaxed">
                    <Trans i18nKey="ctrl.rescue.rebootWarn" components={{ b: <b /> }} />
                  </div>
                </div>

                {confirming && (
                  <p className="text-[12px] text-warning">
                    {t("ctrl.rescue.confirmHint")}
                  </p>
                )}
              </>
            )}
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            {t("common.close")}
          </Button>
          {inRescue ? (
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => exit.mutate(undefined, { onSuccess: () => onOpenChange(false) })}
            >
              {exit.isPending && <Loader2 className="w-4 h-4 animate-spin mr-1.5" />}
              {t("ctrl.rescue.exitBtn")}
            </Button>
          ) : (
            <Button
              disabled={busy || status.isPending || emailBad}
              onClick={() => {
                if (!confirming) { setConfirming(true); return; }
                enter.mutate(
                  { email: email.trim() || undefined },
                  { onSuccess: () => onOpenChange(false) }
                );
              }}
            >
              {enter.isPending && <Loader2 className="w-4 h-4 animate-spin mr-1.5" />}
              {confirming ? t("ctrl.rescue.confirmBtn") : t("ctrl.rescue.enterBtn")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
