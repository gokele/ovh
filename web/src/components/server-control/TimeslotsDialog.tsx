import { useState, useMemo } from "react";
import { Calendar, RefreshCw } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { useTaskTimeslots, type ServerTask } from "@/hooks/use-server-control";

/** 单个任务的可用时间段：默认查未来 14 天，可手动调时间窗 */
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
    return { start: now.toISOString(), end: end.toISOString() };
  }, []);

  const [start, setStart] = useState(defaultRange.start);
  const [end, setEnd] = useState(defaultRange.end);
  const q = useTaskTimeslots(serviceName, task?.taskId ?? null, start, end, open);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Calendar className="w-5 h-5" />
            可用时间段
          </DialogTitle>
          <DialogDescription>
            任务 #{task?.taskId} · {task?.function}。OVH 会按你选定的时间窗给出可执行时段。
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
            <div>
              <label className="text-[11px] text-muted-foreground block mb-1">开始（ISO8601）</label>
              <Input value={start} onChange={(e) => setStart(e.target.value)} className="font-mono text-[12px]" />
            </div>
            <div>
              <label className="text-[11px] text-muted-foreground block mb-1">结束（ISO8601）</label>
              <Input value={end} onChange={(e) => setEnd(e.target.value)} className="font-mono text-[12px]" />
            </div>
          </div>

          {q.isPending ? (
            <Skeleton className="h-40 rounded-2xl" />
          ) : q.data?.scheduleNotRequired ? (
            <div className="border border-info/40 bg-info/5 rounded-2xl p-4 text-[13px] text-foreground/80">
              该任务无需预约时间段。
            </div>
          ) : (q.data?.timeslots || []).length === 0 ? (
            <EmptyState icon={Calendar} title="无可用时间段" description="尝试扩大时间窗口或晚些再试。" />
          ) : (
            <div className="border border-border rounded-2xl max-h-[40vh] overflow-y-auto divide-y divide-border">
              {(q.data?.timeslots || []).map((ts: any, idx: number) => (
                <div key={idx} className="px-4 py-2 text-[13px] flex items-center justify-between gap-3">
                  <code className="font-mono text-[12px]">
                    {ts.startDate ? new Date(ts.startDate).toLocaleString("zh-CN") : "—"}
                  </code>
                  <span className="text-muted-foreground">→</span>
                  <code className="font-mono text-[12px]">
                    {ts.endDate ? new Date(ts.endDate).toLocaleString("zh-CN") : "—"}
                  </code>
                </div>
              ))}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => q.refetch()} disabled={q.isFetching}>
            <RefreshCw className={`w-3.5 h-3.5 mr-1 ${q.isFetching ? "animate-spin" : ""}`} />
            查询
          </Button>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
