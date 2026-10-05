import { useState } from "react";
import { Activity, RefreshCw, Calendar } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed } from "@/components/common/LoadFailed";
import { useServerTasks, type ServerTask } from "@/hooks/use-server-control";
import { PartialNotice, DetailErrorTag } from "@/components/common/PartialNotice";
import { TimeslotsDialog } from "./TimeslotsDialog";
import { useTranslation } from "react-i18next";
import { fmtDateTime } from "@/i18n/format";

/** 任务列表对话框：表格 + 每行"可用时间段"按钮 → 弹出 TimeslotsDialog */
export function TasksDialog({
  serviceName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const q = useServerTasks(serviceName, open);
  const { t } = useTranslation();
  const [tsTask, setTsTask] = useState<ServerTask | null>(null);

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="w-[95vw] sm:w-full sm:max-w-3xl max-h-[85vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Activity className="w-5 h-5" />
              {t("ctrl.tasks.title")}
            </DialogTitle>
            <DialogDescription>{t("ctrl.tasks.desc")}</DialogDescription>
          </DialogHeader>

          <div className="overflow-y-auto flex-1">
            {q.isPending ? (
              <div className="space-y-2">
                {[0, 1, 2].map((i) => (
                  <Skeleton key={i} className="h-12 rounded-md" />
                ))}
              </div>
            ) : q.isError ? (
              // 列表没拉到 ≠ 这台机器没有任务。写「暂无任务记录」会让用户以为刚提交的
              // 重启 / 重装根本没建起来,从而再提交一遍 —— 同一台机器上跑两个运维任务。
              <LoadFailed icon={Activity} title={t("ctrl.tasks.loadFailed")} error={q.error} onRetry={() => q.refetch()} />
            ) : (q.data || []).length === 0 ? (
              <EmptyState icon={Activity} title={t("ctrl.tasks.empty")} />
            ) : (
              <div className="border border-border rounded-2xl overflow-hidden">
                {/* 后端对拉不到详情的任务保留占位行(function=N/A、status=unknown)并带 error。
                    不标出来的话，用户会以为任务真的卡在 unknown，从而重复提交同一个操作 */}
                <PartialNotice
                  failedCount={(q.data || []).filter((t) => !!t.error).length}
                  what={t("ctrl.tasks.partialWhat")}
                  className="m-3"
                />
                <table className="w-full text-[13px]">
                  <thead className="bg-secondary/50">
                    <tr className="text-left">
                      <th className="py-2.5 px-4 font-semibold">{t("ctrl.tasks.col.id")}</th>
                      <th className="py-2.5 px-4 font-semibold">{t("ctrl.tasks.col.fn")}</th>
                      <th className="py-2.5 px-4 font-semibold">{t("ctrl.tasks.col.status")}</th>
                      <th className="py-2.5 px-4 font-semibold">{t("ctrl.tasks.col.start")}</th>
                      <th className="py-2.5 px-4 font-semibold">{t("ctrl.tasks.col.done")}</th>
                      <th className="py-2.5 px-4 font-semibold"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {(q.data || []).map((task) => (
                      <tr key={task.taskId} className="border-t border-border">
                        <td className="py-2.5 px-4 font-mono">{task.taskId}</td>
                        <td className="py-2.5 px-4">
                          {task.error ? <span className="text-muted-foreground">—</span> : task.function}
                        </td>
                        <td className="py-2.5 px-4">
                          {task.error ? (
                            <DetailErrorTag message={task.error} />
                          ) : (
                            <span className={`text-[12px] capitalize ${statusColor(task.status)}`}>{task.status}</span>
                          )}
                        </td>
                        <td className="py-2.5 px-4 text-muted-foreground">
                          {task.startDate ? fmtDateTime(task.startDate) : "—"}
                        </td>
                        <td className="py-2.5 px-4 text-muted-foreground">
                          {task.doneDate ? fmtDateTime(task.doneDate) : "—"}
                        </td>
                        <td className="py-2.5 px-4 text-right">
                          {/* 详情没拉到的任务连 function 都是占位值，预约时间段没有意义 */}
                          <Button variant="outline" size="sm" disabled={!!task.error} onClick={() => setTsTask(task)}>
                            <Calendar className="w-3 h-3 mr-1" />
                            {t("ctrl.tasks.slotsBtn")}
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => q.refetch()} disabled={q.isFetching}>
              <RefreshCw className={`w-3.5 h-3.5 mr-1 ${q.isFetching ? "animate-spin" : ""}`} />
              {t("common.refresh")}
            </Button>
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              {t("common.close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <TimeslotsDialog
        serviceName={serviceName}
        task={tsTask}
        open={!!tsTask}
        onOpenChange={(v) => !v && setTsTask(null)}
      />
    </>
  );
}

function statusColor(status: string): string {
  const s = status?.toLowerCase();
  if (s === "done") return "text-success";
  if (s === "error" || s === "cancelled") return "text-destructive";
  return "text-warning";
}
