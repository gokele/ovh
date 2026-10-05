import { useEffect, useState } from "react";
import { Repeat, AlertCircle, Lock } from "lucide-react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  useUpdateRenewal,
  useUpdateTerminationPolicy,
  type ServiceInfo,
  terminationLabel,
} from "@/hooks/use-server-control";
import { toast } from "sonner";
import { useTranslation, Trans } from "react-i18next";
import { errorMessage } from "@/components/common/LoadFailed";
import { fmtDate } from "@/i18n/format";

type RenewMode = "auto" | "manual" | "delete";

/** 三选一选项表:存文案 key,渲染处 t() */
const MODE_OPTIONS: Array<{ value: RenewMode; labelKey: string; descKey: string }> = [
  { value: "auto", labelKey: "maint.renewal.mode.auto", descKey: "maint.renewal.mode.autoDesc" },
  { value: "manual", labelKey: "maint.renewal.mode.manual", descKey: "maint.renewal.mode.manualDesc" },
  { value: "delete", labelKey: "maint.renewal.mode.delete", descKey: "maint.renewal.mode.deleteDesc" },
];

/** hooks 的 terminationLabel 返回中文;这里按同样的分支派生 i18n key(text/title 成对) */
const TERM_BASE_BY_ACTION: Record<string, string> = {
  terminate: "maint.renewal.termTerminateNow",
  terminateAtEngagementDate: "maint.renewal.termAtEngagement",
  terminateAtExpirationDate: "maint.renewal.termAtExpiration",
  deleteAtExpiration: "maint.renewal.termAtExpiration",
};

/** 用 VPS / dedicated 各自的 update hook 都行,Dialog 只关心 mutation 接口形状 */
export type RenewalMutation = {
  mutateAsync: (vars: { mode: RenewMode; period?: number }) => Promise<any>;
  isPending: boolean;
};

/** 终止流程的两步。VPS 和独服的端点不同(/vps-control 与 /server-control),
 *  所以必须由调用方注入 —— 写死一边会让另一边打到错误的端点上。 */
export type TerminationMutations = {
  policy: {
    mutateAsync: (vars: { policy: string }) => Promise<any>;
    isPending: boolean;
  };
};

/** 续费策略修改对话框:三选一 + 周期选择;forced 套餐禁用全部操作。
 *  默认用 dedicated 的 useUpdateRenewal,VPS 调用方传入自己的 mutation 即可复用。 */
export function RenewalDialog({
  serviceName,
  info,
  open,
  onOpenChange,
  mutation,
  termination,
}: {
  serviceName: string;
  info: ServiceInfo | (Omit<ServiceInfo, "possibleRenewPeriod"> & { possibleRenewPeriod?: number[] });
  open: boolean;
  onOpenChange: (v: boolean) => void;
  /** 可选:不传则用 dedicated 的 useUpdateRenewal(serviceName) */
  mutation?: RenewalMutation;
  /** 可选:不传则用 dedicated 的终止端点。VPS 必须传自己的 */
  termination?: TerminationMutations;
}) {
  const { t } = useTranslation();
  // 终止状态以 lifecycle.pendingActions 为准(文档指定的读回路径)。
  // term 还带着"是哪一种终止" —— 立即终止不能显示成到期终止
  const term = terminationLabel(info);
  const terminationOn = !!term && !info.terminationStateUnknown;
  const currentMode: RenewMode = terminationOn
    ? "delete"
    : info.renewalType
      ? "auto"
      : "manual";
  const [mode, setMode] = useState<RenewMode>(currentMode);
  const [period, setPeriod] = useState<number>(info.renewalPeriod || 1);
  const defaultUpdate = useUpdateRenewal(serviceName);
  const update = mutation ?? defaultUpdate;

  // 展示用的终止状态文案:跟 terminationLabel 同一套分支,但走语言包
  let termTextKey: string | null = null;
  let termTitleKey: string | null = null;
  if (term) {
    if (info.terminationStateUnknown) {
      termTextKey = "maint.renewal.termUnknownText";
      termTitleKey = "maint.renewal.termUnknownTitle";
    } else {
      const base = TERM_BASE_BY_ACTION[info.terminationAction || ""] || "maint.renewal.termScheduled";
      termTextKey = `${base}Text`;
      termTitleKey = `${base}Title`;
    }
  }

  // 到期终止走 PUT /services/{serviceId} 的 terminationPolicy。
  //
  // **不要**用 POST /terminate —— 那是「立即终止」,提交后 OVH 当场把服务器暂停,
  // 并邮件通知「5 天内不付款就彻底清除硬盘数据」。这个坑真实踩过一次。
  // OVH 的生命周期动作枚举里 terminate 与 terminateAtExpirationDate 是两个不同的动作,
  // /terminate 端点只对应前者,没有「到期」这个选项可选。
  const defaultPolicy = useUpdateTerminationPolicy(serviceName);
  const policyMut = termination?.policy ?? defaultPolicy;

  // 弹窗每次打开同步当前状态
  useEffect(() => {
    if (open) {
      setMode(currentMode);
      setPeriod(info.renewalPeriod || 1);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const periods = info.possibleRenewPeriod && info.possibleRenewPeriod.length > 0
    ? info.possibleRenewPeriod
    : [1, 3, 6, 12];

  const handleSubmit = async () => {
    if (mode === "delete") {
      try {
        const res = await policyMut.mutateAsync({ policy: "terminateAtExpirationDate" });
        toast.success(res?.message || t("maint.renewal.toast.setDelete"), { duration: 6000 });
        onOpenChange(false);
      } catch (e: any) {
        toast.error(errorMessage(e), { duration: 8000 });
      }
      return;
    }
    // 从「到期终止」切回自动/手动续费时，先把终止策略撤掉，
    // 否则续费模式改了、终止标记还挂着，到期照样销毁。
    try {
      if (currentMode === "delete") {
        await policyMut.mutateAsync({ policy: "empty" });
      }
      await update.mutateAsync({ mode, period });
      toast.success(
        currentMode === "delete" ? t("maint.renewal.toast.cancelAndUpdated") : t("maint.renewal.toast.updated")
      );
      onOpenChange(false);
    } catch (e: any) {
      toast.error(errorMessage(e), { duration: 6000 });
    }
  };

  const busy = update.isPending || policyMut.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Repeat className="w-5 h-5" />
            {t("maint.renewal.title")}
          </DialogTitle>
          <DialogDescription>{serviceName}</DialogDescription>
        </DialogHeader>

        {term && termTextKey && termTitleKey && (
          <div
            className={`rounded-xl p-3 flex gap-2.5 border ${
              term.danger ? "border-destructive/40 bg-destructive/5" : "border-warning/40 bg-warning/10"
            }`}
            title={t(termTitleKey)}
          >
            <AlertCircle
              className={`w-4 h-4 flex-shrink-0 mt-0.5 ${term.danger ? "text-destructive" : "text-warning"}`}
            />
            <div className="text-[12px]">
              <p className="font-semibold mb-0.5">
                {t("maint.renewal.currentPrefix")}
                {t(termTextKey)}
              </p>
              <p className="text-muted-foreground">{t(termTitleKey)}</p>
            </div>
          </div>
        )}

        {info.renewalForced ? (
          <div className="border border-warning/40 bg-warning/10 rounded-xl p-3 flex gap-2.5">
            <Lock className="w-4 h-4 text-warning flex-shrink-0 mt-0.5" />
            <div className="text-[12px]">
              <p className="font-semibold text-warning mb-1">{t("maint.renewal.forcedTitle")}</p>
              <p className="text-muted-foreground">{t("maint.renewal.forcedDesc")}</p>
            </div>
          </div>
        ) : (
          <div className="space-y-3 py-1">
            {/* 三选一 */}
            <div className="space-y-1.5">
              {MODE_OPTIONS.map((opt) => {
                const selected = mode === opt.value;
                return (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => setMode(opt.value)}
                    className={[
                      "w-full text-left rounded-xl border px-3.5 py-2.5 transition-colors",
                      selected
                        ? "border-primary bg-primary/5"
                        : "border-border bg-secondary/30 hover:bg-secondary/50",
                    ].join(" ")}
                  >
                    <div className="flex items-center gap-2">
                      <div
                        className={[
                          "w-4 h-4 rounded-full border-2 flex items-center justify-center flex-shrink-0",
                          selected ? "border-primary" : "border-muted-foreground/40",
                        ].join(" ")}
                      >
                        {selected && <div className="w-2 h-2 rounded-full bg-primary" />}
                      </div>
                      <span className="text-[13px] font-semibold">{t(opt.labelKey)}</span>
                      {currentMode === opt.value && (
                        <span className="ml-auto text-[10px] text-muted-foreground">
                          {t("maint.renewal.current")}
                        </span>
                      )}
                    </div>
                    <p className="text-[11px] text-muted-foreground mt-1 ml-6">{t(opt.descKey)}</p>
                  </button>
                );
              })}
            </div>

            {/* 续费周期(到期注销时隐藏) */}
            {mode !== "delete" && (
              <div className="pt-1">
                <label className="text-[12px] font-semibold block mb-1.5">{t("maint.renewal.periodLabel")}</label>
                <Select value={String(period)} onValueChange={(v) => setPeriod(Number(v))}>
                  <SelectTrigger className="h-9">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {periods.map((p) => (
                      <SelectItem key={p} value={String(p)}>
                        {t("maint.renewal.monthOption", { n: p })}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            {mode === "delete" && (
              <div className="border border-destructive/40 bg-destructive/5 rounded-xl p-2.5 flex gap-2">
                <AlertCircle className="w-3.5 h-3.5 text-destructive flex-shrink-0 mt-0.5" />
                <div className="text-[11px] text-muted-foreground space-y-1">
                  <p>
                    <Trans
                      i18nKey="maint.renewal.deleteWarn"
                      values={{ date: info.expiration ? fmtDate(info.expiration) : "—" }}
                      components={{ b: <b /> }}
                    />
                  </p>
                  <p>{t("maint.renewal.deleteUndo")}</p>
                </div>
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          {!info.renewalForced && (
            <Button
              onClick={handleSubmit}
              disabled={
                busy || (mode !== "delete" && mode === currentMode && period === info.renewalPeriod)
              }
              variant={mode === "delete" ? "destructive" : "default"}
            >
              {busy ? t("maint.renewal.submitting") : t("common.save")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
