import { useState, useMemo } from "react";
import { Calendar, RefreshCw, CheckCircle2, AlertTriangle } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { useTaskTimeslots, useScheduleTask, type ServerTask } from "@/hooks/use-server-control";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { fmtDateTime } from "@/i18n/format";
import { errorMessage } from "@/components/common/LoadFailed";

/** Date → YYYY-MM-DD（按本地时区取"那一天"，跟后端 normalizeAPIDate 的取法一致） */
function toDateInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/**
 * 单个任务的可用时间段：默认查未来 14 天，可手动调时间窗，并可直接预约某个时段。
 *
 * periodStart / periodEnd 在 schema 里是 date 类型（后端也只会裁到 YYYY-MM-DD），
 * 所以这里用日期选择器而不是完整 ISO 时间戳文本框 —— 后者会让用户以为能精确到小时，
 * 填了时分秒也一样被截断。
 */
export function TimeslotsDialog({
  serviceName,
  task,
  open,
  onOpenChange,
}: {
  serviceName: string;
  task: ServerTask | null;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const defaultRange = useMemo(() => {
    const now = new Date();
    const end = new Date(now.getTime() + 14 * 24 * 60 * 60 * 1000);
    return { start: toDateInput(now), end: toDateInput(end) };
  }, []);

  const [start, setStart] = useState(defaultRange.start);
  const [end, setEnd] = useState(defaultRange.end);
  const q = useTaskTimeslots(serviceName, task?.taskId ?? null, start, end, open);
  const schedule = useScheduleTask();
  const { t } = useTranslation();

  /** 选中的时段起始时间（RFC3339，直接取 OVH 给的原值） */
  const [picked, setPicked] = useState<string | null>(null);
  /** 用户是否声明已完成备份。绝不替用户默认勾上：后端会把它原样转给 OVH，等于替他做担保 */
  const [hasBackup, setHasBackup] = useState(false);

  const handleSchedule = async () => {
    if (!task || !picked) {
      toast.error(t("ctrl.tasks.toast.pickFirst"));
      return;
    }
    try {
      await schedule.mutateAsync({
        serviceName,
        taskId: task.taskId,
        wantedBeginingDate: picked,
        hasPerformedBackup: hasBackup,
      });
      toast.success(t("ctrl.tasks.toast.scheduled"));
      onOpenChange(false);
      setPicked(null);
      setHasBackup(false);
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Calendar className="w-5 h-5" />
            {t("ctrl.tasks.slotsTitle")}
          </DialogTitle>
          <DialogDescription>
            {t("ctrl.tasks.slotsDesc", { id: task?.taskId, fn: task?.function })}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
            <div>
              <label className="text-[11px] text-muted-foreground block mb-1">{t("ctrl.tasks.startDate")}</label>
              <Input type="date" value={start} onChange={(e) => setStart(e.target.value)} className="text-base sm:text-[12px]" />
            </div>
            <div>
              <label className="text-[11px] text-muted-foreground block mb-1">{t("ctrl.tasks.endDate")}</label>
              <Input type="date" value={end} onChange={(e) => setEnd(e.target.value)} className="text-base sm:text-[12px]" />
            </div>
          </div>
          <p className="text-[11px] text-muted-foreground">
            {t("ctrl.tasks.rangeHint")}
          </p>

          {q.isPending ? (
            <Skeleton className="h-40 rounded-2xl" />
          ) : q.isError ? (
            <div className="border border-destructive/40 bg-destructive/5 rounded-2xl p-4 text-[13px] space-y-2">
              <p className="text-destructive">{t("ctrl.tasks.queryFailed")}</p>
              <p className="text-[12px] text-muted-foreground">{errorMessage(q.error)}</p>
            </div>
          ) : q.data?.scheduleNotRequired ? (
            <div className="border border-info/40 bg-info/5 rounded-2xl p-4 text-[13px] text-foreground/80">
              {t("ctrl.tasks.noScheduleNeeded")}
            </div>
          ) : (q.data?.timeslots || []).length === 0 ? (
            <EmptyState
              icon={Calendar}
              title={t("ctrl.tasks.noSlots")}
              description={t("ctrl.tasks.noSlotsDesc")}
            />
          ) : (
            <>
              <div className="border border-border rounded-2xl max-h-[40vh] overflow-y-auto divide-y divide-border">
                {(q.data?.timeslots || []).map((ts: any, idx: number) => {
                  const startDate: string | undefined = ts.startDate;
                  const active = !!startDate && picked === startDate;
                  return (
                    <button
                      key={startDate || idx}
                      type="button"
                      disabled={!startDate}
                      onClick={() => startDate && setPicked(startDate)}
                      className={
                        "w-full text-left px-4 py-2 text-[13px] flex items-center justify-between gap-3 hover:bg-muted transition-colors " +
                        (active ? "bg-secondary" : "")
                      }
                    >
                      <code className="font-mono text-[12px]">
                        {startDate ? fmtDateTime(startDate) : "—"}
                      </code>
                      <span className="text-muted-foreground">→</span>
                      <code className="font-mono text-[12px]">
                        {ts.endDate ? fmtDateTime(ts.endDate) : "—"}
                      </code>
                      {active && <CheckCircle2 className="w-4 h-4 text-success flex-shrink-0" />}
                    </button>
                  );
                })}
              </div>

              <div className="border border-warning/40 bg-warning/5 rounded-2xl p-3 space-y-2 text-[12px]">
                <div className="flex items-start gap-2">
                  <AlertTriangle className="w-4 h-4 text-warning flex-shrink-0 mt-0.5" />
                  <span>{t("ctrl.tasks.backupWarn")}</span>
                </div>
                <label className="flex items-center gap-2 cursor-pointer pl-6">
                  <input
                    type="checkbox"
                    checked={hasBackup}
                    onChange={(e) => setHasBackup(e.target.checked)}
                    className="w-4 h-4"
                  />
                  {t("ctrl.tasks.backupDone")}
                </label>
              </div>
            </>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => q.refetch()} disabled={q.isFetching}>
            <RefreshCw className={`w-3.5 h-3.5 mr-1 ${q.isFetching ? "animate-spin" : ""}`} />
            {t("ctrl.tasks.query")}
          </Button>
          {!q.data?.scheduleNotRequired && (q.data?.timeslots || []).length > 0 && (
            <Button onClick={handleSchedule} disabled={!picked || schedule.isPending}>
              {schedule.isPending ? t("ctrl.tasks.submitting") : t("ctrl.tasks.schedule")}
            </Button>
          )}
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.close")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
