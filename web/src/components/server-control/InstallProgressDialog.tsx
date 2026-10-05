import { Activity, Check, X as XIcon, Loader2 } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed } from "@/components/common/LoadFailed";
import { useInstallStatus, type InstallStep } from "@/hooks/use-server-control";
import { useTranslation } from "react-i18next";

/** 安装进度面板：每 5s 轮询 /install/status，展示 step 列表和整体进度（对齐旧前端） */
export function InstallProgressDialog({
  serviceName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const q = useInstallStatus(serviceName, open);
  const { t } = useTranslation();
  const data = q.data;
  const status = data?.status;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl max-h-[85vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Activity className="w-5 h-5" />
            {t("ctrl.progress.title")}
          </DialogTitle>
          <DialogDescription>
            {t("ctrl.progress.desc")}
          </DialogDescription>
        </DialogHeader>

        <div className="overflow-y-auto -mx-6 px-6 space-y-3 flex-1">
          {q.isPending ? (
            <Skeleton className="h-40 rounded-2xl" />
          ) : q.isError ? (
            // 这个面板是重装进行中盯进度用的。查询失败要是也渲染成「当前无安装任务」,
            // 用户看到的就是"装机任务已经不在了" —— 于是当成装完了去重启,
            // 或者干脆再发一次重装,把正在跑的装机打断。失败必须说成失败。
            // (查询本身每 5s 自动重试,重试按钮只是给用户一个立刻再试的入口)
            <LoadFailed
              icon={Activity}
              title={t("ctrl.progress.loadFailed")}
              error={q.error}
              onRetry={() => q.refetch()}
            />
          ) : !data?.hasInstallation || !status ? (
            <EmptyState icon={Activity} title={t("ctrl.progress.empty")} />
          ) : (
            <>
              {/* 整体进度条。
                  progressUnknown = OVH 这次压根没返回 progress（schema 里它可空），
                  此时 progressPercentage 恒 0、allDone 恒 false，但那不代表"一步都没做"。
                  照常画一个 0% 的进度条会让用户以为装机卡死，所以这里换成明确的"进度暂不可用"。 */}
              <div className="border border-border rounded-2xl p-4 space-y-2">
                {status.progressUnknown ? (
                  <>
                    <div className="text-[12px] font-semibold">{t("ctrl.progress.unavailable")}</div>
                    <p className="text-[11px] text-muted-foreground">
                      {t("ctrl.progress.unavailableDesc")}
                    </p>
                    {status.elapsedTime ? (
                      <div className="text-[11px] text-muted-foreground">
                        {t("ctrl.progress.elapsed", { n: Math.floor(status.elapsedTime) })}
                      </div>
                    ) : null}
                  </>
                ) : (
                  <>
                    <div className="flex justify-between text-[12px]">
                      <span className="text-muted-foreground">
                        {t("ctrl.progress.steps", { done: status.completedSteps ?? 0, total: status.totalSteps ?? 0 })}
                      </span>
                      <span className="font-semibold">{Math.floor(status.progressPercentage || 0)}%</span>
                    </div>
                    <div className="h-2 bg-secondary rounded-full overflow-hidden">
                      <div
                        className={`h-full transition-all ${status.hasError ? "bg-destructive" : "bg-foreground"}`}
                        style={{ width: `${Math.min(100, Math.max(0, status.progressPercentage || 0))}%` }}
                      />
                    </div>
                    <div className="flex justify-between text-[11px] text-muted-foreground">
                      <span>
                        {status.allDone
                          ? t("ctrl.progress.statusDone")
                          : status.hasError
                            ? t("ctrl.progress.statusError")
                            : status.stopping
                              ? t("ctrl.progress.statusStopping")
                              : t("ctrl.progress.statusRunning")}
                      </span>
                      {status.elapsedTime ? (
                        <span>{t("ctrl.progress.took", { n: Math.floor(status.elapsedTime) })}</span>
                      ) : null}
                    </div>
                  </>
                )}
              </div>

              {/* Step 列表 */}
              {(status.steps || []).length > 0 && (
                <div className="border border-border rounded-2xl divide-y divide-border">
                  {status.steps.map((step, idx) => (
                    <StepRow key={idx} step={step} />
                  ))}
                </div>
              )}
            </>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.close")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function StepRow({ step }: { step: InstallStep }) {
  const icon =
    step.status === "done" ? (
      <Check className="w-3.5 h-3.5 text-success" />
    ) : step.status === "error" ? (
      <XIcon className="w-3.5 h-3.5 text-destructive" />
    ) : step.status === "doing" ? (
      <Loader2 className="w-3.5 h-3.5 animate-spin" />
    ) : (
      <div className="w-3.5 h-3.5 rounded-full border border-border" />
    );

  return (
    <div className="px-4 py-2 text-[13px] flex items-start gap-2.5">
      <div className="mt-0.5 flex-shrink-0">{icon}</div>
      <div className="min-w-0 flex-1">
        <div className={step.status === "done" ? "text-foreground" : step.status === "error" ? "text-destructive" : "text-foreground/80"}>
          {/* comment 是后端翻译过的中文，翻译表没覆盖时才回退 OVH 原文 */}
          {step.comment || step.commentOriginal}
        </div>
        {step.error && <p className="text-[11px] text-destructive mt-0.5">{step.error}</p>}
      </div>
    </div>
  );
}
