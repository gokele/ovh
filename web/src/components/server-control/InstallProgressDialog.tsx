import { Activity, Check, X as XIcon, Loader2 } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { useInstallStatus } from "@/hooks/use-server-control";

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
  const data = q.data;
  const status = data?.status as any;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl max-h-[85vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Activity className="w-5 h-5" />
            安装进度
          </DialogTitle>
          <DialogDescription>
            实时跟踪当前重装任务进度（每 5 秒刷新）。完成后可手动关闭。
          </DialogDescription>
        </DialogHeader>

        <div className="overflow-y-auto -mx-6 px-6 space-y-3 flex-1">
          {q.isPending ? (
            <Skeleton className="h-40 rounded-2xl" />
          ) : !data?.hasInstallation || !status ? (
            <EmptyState icon={Activity} title="当前无安装任务" />
          ) : (
            <>
              {/* 整体进度条 */}
              <div className="border border-border rounded-2xl p-4 space-y-2">
                <div className="flex justify-between text-[12px]">
                  <span className="text-muted-foreground">
                    {status.completedSteps ?? 0} / {status.totalSteps ?? 0} 步
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
                  <span>{status.allDone ? "已完成" : status.hasError ? "出错" : "进行中"}</span>
                  {status.elapsedTime ? <span>耗时 {Math.floor(status.elapsedTime)}s</span> : null}
                </div>
              </div>

              {/* Step 列表 */}
              {(status.steps || []).length > 0 && (
                <div className="border border-border rounded-2xl divide-y divide-border">
                  {(status.steps as any[]).map((step, idx) => (
                    <StepRow key={idx} step={step} />
                  ))}
                </div>
              )}
            </>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function StepRow({ step }: { step: { comment: string; status: string; error?: string } }) {
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
          {step.comment}
        </div>
        {step.error && <p className="text-[11px] text-destructive mt-0.5">{step.error}</p>}
      </div>
    </div>
  );
}
