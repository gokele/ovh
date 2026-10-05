import { ListTodo, RefreshCw } from "lucide-react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed } from "@/components/common/LoadFailed";
import { Chip } from "@/components/common/Chip";
import { useVpsTasks, type VpsTask } from "@/hooks/use-vps-control";
import { useTranslation } from "react-i18next";
import { fmtDateTime } from "@/i18n/format";

/** VPS 任务管理:显示最近 10 个任务(reboot/start/stop/reinstall/createSnapshot/revert 等)+ 状态 + 进度。
 *  打开时每 5 秒轮询一次,关闭时停止 —— 进行中的任务能实时看到进度。 */
export function VpsTasksDialog({
  serviceName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const q = useVpsTasks(open ? serviceName : null);
  const { t } = useTranslation();
  // refetchInterval 在 q 选项里设不上,简单做法:用户点刷新

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ListTodo className="w-5 h-5" />
            {t("vps.tasks.title")}
          </DialogTitle>
          <DialogDescription>{t("vps.tasks.desc")}</DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto -mx-6 px-6">
          {q.isPending ? (
            <Skeleton className="h-40 rounded-2xl" />
          ) : q.isError ? (
            // 没问到任务列表却写「暂无任务历史」,用户会以为刚提交的重装 / 回滚 / 改密根本没建起来,
            // 于是回去再点一次 —— 破坏性操作被重复下发。失败必须说成失败。
            <LoadFailed
              icon={ListTodo}
              title={t("vps.tasks.loadFailed")}
              error={q.error}
              onRetry={() => q.refetch()}
              compact
            />
          ) : (q.data || []).length === 0 ? (
            <EmptyState icon={ListTodo} title={t("vps.tasks.empty")} />
          ) : (
            <div className="space-y-2 py-1">
              {(q.data || []).slice().reverse().map((task) => (
                <TaskRow key={task.id} task={task} />
              ))}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => q.refetch()} disabled={q.isFetching}>
            <RefreshCw className={"w-3.5 h-3.5 mr-1" + (q.isFetching ? " animate-spin" : "")} />
            {t("common.refresh")}
          </Button>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.close")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TaskRow({ task }: { task: VpsTask }) {
  const { t } = useTranslation();
  const tone = taskTone(task.state);
  return (
    <div className="border border-border rounded-xl p-3 space-y-1.5">
      <div className="flex items-center gap-2 flex-wrap">
        <code className="text-[11px] font-mono text-muted-foreground">#{task.id}</code>
        <span className="text-[13px] font-semibold">{t(translateTaskType(task.type))}</span>
        <Chip tone={tone}>{t(translateTaskState(task.state))}</Chip>
        {task.progress > 0 && task.progress < 100 && (
          <span className="text-[11px] text-muted-foreground">{task.progress}%</span>
        )}
        <span className="ml-auto text-[11px] text-muted-foreground">
          {task.date ? fmtDateTime(task.date) : "—"}
        </span>
      </div>
      {/* 进度条 */}
      {task.state === "doing" && (
        <div className="h-1 bg-secondary rounded overflow-hidden">
          <div
            className="h-full bg-primary transition-all"
            style={{ width: `${Math.max(5, task.progress)}%` }}
          />
        </div>
      )}
    </div>
  );
}

function taskTone(state: string): "default" | "success" | "warning" | "danger" | "info" {
  switch (state.toLowerCase()) {
    case "done":
      return "success";
    case "doing":
    case "todo":
    case "waitingack":
      return "warning";
    case "cancelled":
    case "error":
    case "blocked":
      return "danger";
    case "paused":
      return "info";
    default:
      return "default";
  }
}

/** OVH vps.TaskStateEnum: blocked / cancelled / doing / done / error / paused / todo / waitingAck。
 *  表里存 i18n key(vps.tasks.state.*),渲染处统一 t();没收录的枚举原样透传。 */
function translateTaskState(s: string): string {
  return ({
    blocked: "vps.tasks.state.blocked",
    cancelled: "vps.tasks.state.cancelled",
    doing: "vps.tasks.state.doing",
    done: "vps.tasks.state.done",
    error: "vps.tasks.state.error",
    paused: "vps.tasks.state.paused",
    todo: "vps.tasks.state.todo",
    waitingack: "vps.tasks.state.waitingack",
  } as Record<string, string>)[s.toLowerCase()] || s;
}

/** OVH vps.TaskTypeEnum 全集 —— 注意 OVH 命名大多带 Vm 后缀(rebootVm 不是 reboot)。
 *  表里存 i18n key(vps.tasks.type.*),渲染处统一 t();没收录的枚举原样透传。 */
function translateTaskType(t: string): string {
  return ({
    addVeeamBackupJob: "vps.tasks.type.addVeeamBackupJob",
    changeRootPassword: "vps.tasks.type.changeRootPassword",
    createSnapshot: "vps.tasks.type.createSnapshot",
    deleteSnapshot: "vps.tasks.type.deleteSnapshot",
    deliverVm: "vps.tasks.type.deliverVm",
    getConsoleUrl: "vps.tasks.type.getConsoleUrl",
    internalTask: "vps.tasks.type.internalTask",
    migrate: "vps.tasks.type.migrate",
    openConsoleAccess: "vps.tasks.type.openConsoleAccess",
    provisioningAdditionalIp: "vps.tasks.type.provisioningAdditionalIp",
    reOpenVm: "vps.tasks.type.reOpenVm",
    rebootVm: "vps.tasks.type.rebootVm",
    reinstallVm: "vps.tasks.type.reinstallVm",
    removeVeeamBackup: "vps.tasks.type.removeVeeamBackup",
    rescheduleAutoBackup: "vps.tasks.type.rescheduleAutoBackup",
    restoreFullVeeamBackup: "vps.tasks.type.restoreFullVeeamBackup",
    restoreVeeamBackup: "vps.tasks.type.restoreVeeamBackup",
    restoreVm: "vps.tasks.type.restoreVm",
    revertSnapshot: "vps.tasks.type.revertSnapshot",
    setMonitoring: "vps.tasks.type.setMonitoring",
    setNetboot: "vps.tasks.type.setNetboot",
    startVm: "vps.tasks.type.startVm",
    stopVm: "vps.tasks.type.stopVm",
    upgradeVm: "vps.tasks.type.upgradeVm",
  } as Record<string, string>)[t] || t;
}
