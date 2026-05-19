import { useState } from "react";
import { Activity, RefreshCw, Calendar } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { useServerTasks, type ServerTask } from "@/hooks/use-server-control";
import { TimeslotsDialog } from "./TimeslotsDialog";

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
  const [tsTask, setTsTask] = useState<ServerTask | null>(null);

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="w-[95vw] sm:w-full sm:max-w-3xl max-h-[85vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Activity className="w-5 h-5" />
              任务列表
            </DialogTitle>
            <DialogDescription>该服务器近期所有运维任务（重启 / 重装 / 硬件干预 等）。</DialogDescription>
          </DialogHeader>

          <div className="overflow-y-auto flex-1">
            {q.isPending ? (
              <div className="space-y-2">
                {[0, 1, 2].map((i) => (
                  <Skeleton key={i} className="h-12 rounded-md" />
                ))}
              </div>
            ) : (q.data || []).length === 0 ? (
              <EmptyState icon={Activity} title="暂无任务记录" />
            ) : (
              <div className="border border-border rounded-2xl overflow-hidden">
                <table className="w-full text-[13px]">
                  <thead className="bg-secondary/50">
                    <tr className="text-left">
                      <th className="py-2.5 px-4 font-semibold">任务 ID</th>
                      <th className="py-2.5 px-4 font-semibold">操作</th>
                      <th className="py-2.5 px-4 font-semibold">状态</th>
                      <th className="py-2.5 px-4 font-semibold">开始时间</th>
                      <th className="py-2.5 px-4 font-semibold">完成时间</th>
                      <th className="py-2.5 px-4 font-semibold"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {(q.data || []).map((task) => (
                      <tr key={task.taskId} className="border-t border-border">
                        <td className="py-2.5 px-4 font-mono">{task.taskId}</td>
                        <td className="py-2.5 px-4">{task.function}</td>
                        <td className="py-2.5 px-4">
                          <span className={`text-[12px] capitalize ${statusColor(task.status)}`}>{task.status}</span>
                        </td>
                        <td className="py-2.5 px-4 text-muted-foreground">
                          {task.startDate ? new Date(task.startDate).toLocaleString("zh-CN") : "—"}
                        </td>
                        <td className="py-2.5 px-4 text-muted-foreground">
                          {task.doneDate ? new Date(task.doneDate).toLocaleString("zh-CN") : "—"}
                        </td>
                        <td className="py-2.5 px-4 text-right">
                          <Button variant="outline" size="sm" onClick={() => setTsTask(task)}>
                            <Calendar className="w-3 h-3 mr-1" />
                            时间段
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
              刷新
            </Button>
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              关闭
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
