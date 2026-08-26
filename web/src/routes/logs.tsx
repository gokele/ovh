import { createFileRoute } from "@tanstack/react-router";
import { useHideIp } from "@/hooks/use-hide-ip";
import { FileText, RefreshCw, Trash2, Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Checkbox } from "@/components/ui/checkbox";
import { Chip } from "@/components/common/Chip";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useLogs, useClearLogs, type LogEntry } from "@/hooks/use-logs";

/** 详细日志：Vercel deploy logs 风时间流 */
export const Route = createFileRoute("/logs")({
  component: LogsPage,
});


/** 日志是自由文本,IP 和 serviceName(ns123.ip-54-38-222.eu,内嵌完整 IP)混在句子里。
 *  隐私模式开着时对整行做子串级打码 —— 这个页面截图发群里排障是常态,
 *  别的页面都打了码、日志页裸奔等于全部白打。 */
function maskLogLine(text: string, hidden: boolean): string {
  if (!hidden || !text) return text;
  return text
    .replace(/ip[-.]\d{1,3}[-.]\d{1,3}[-.]\d{1,3}[-.]\d{1,3}/gi, "ip-***-***-***-***")
    .replace(/\b(\d{1,3})\.\d{1,3}\.\d{1,3}\.\d{1,3}\b/g, "$1.***.***.***")
    .replace(/\b([0-9a-fA-F]{1,4}:){3,}[0-9a-fA-F:]+\b/g, "ipv6:****");
}

function LogsPage() {
  const { hidden } = useHideIp();
  const [autoRefresh, setAutoRefresh] = useState(true);
  const logs = useLogs(autoRefresh);
  const clear = useClearLogs();
  const [search, setSearch] = useState("");
  const [levelFilter, setLevelFilter] = useState<string>("all");
  const [confirmClear, setConfirmClear] = useState(false);

  const items = logs.data || [];
  const filtered = useMemo(() => {
    const s = search.trim().toLowerCase();
    return items.filter((l) => {
      if (levelFilter !== "all" && l.level !== levelFilter) return false;
      if (s && !`${l.message} ${l.source}`.toLowerCase().includes(s)) return false;
      return true;
    });
  }, [items, search, levelFilter]);

  // 自动滚到底部：仅当 autoRefresh 开启且没有筛选条件（避免破坏用户的浏览位置）
  const logEndRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (autoRefresh && !search && levelFilter === "all") {
      logEndRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
    }
  }, [filtered, autoRefresh, search, levelFilter]);

  return (
    <div className="space-y-6">
      <PageHeader
        icon={FileText}
        title="详细日志"
        description="查看系统运行日志记录"
        action={
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => logs.refetch()} disabled={logs.isFetching}>
              <RefreshCw className={`w-4 h-4 ${logs.isFetching ? "animate-spin" : ""}`} />
              刷新
            </Button>
            <Button variant="outline" onClick={() => setConfirmClear(true)} disabled={items.length === 0}>
              <Trash2 className="w-4 h-4" />
              清空
            </Button>
          </div>
        }
      />

      <Card>
        <CardContent className="p-5">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 items-center">
            <div className="relative">
              <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
              <Input
                placeholder="搜索日志内容..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-9 rounded-full"
              />
            </div>
            <Select value={levelFilter} onValueChange={setLevelFilter}>
              <SelectTrigger className="rounded-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">所有级别</SelectItem>
                <SelectItem value="INFO">INFO</SelectItem>
                <SelectItem value="WARNING">WARNING</SelectItem>
                <SelectItem value="ERROR">ERROR</SelectItem>
                <SelectItem value="DEBUG">DEBUG</SelectItem>
              </SelectContent>
            </Select>
            <label className="inline-flex items-center gap-2 text-sm cursor-pointer">
              <Checkbox checked={autoRefresh} onCheckedChange={(c) => setAutoRefresh(c === true)} />
              <span>自动刷新（5 秒）</span>
            </label>
          </div>
        </CardContent>
      </Card>

      <Card>
        <div className="px-4 py-2.5 border-b border-border flex items-center justify-between text-[12px]">
          <span className="font-semibold">系统日志</span>
          <span className="text-muted-foreground">{filtered.length} 条</span>
        </div>
        {logs.isPending && items.length === 0 ? (
          <div className="p-4 space-y-2">
            {Array.from({ length: 8 }).map((_, i) => <Skeleton key={i} className="h-8" />)}
          </div>
        ) : filtered.length === 0 ? (
          <EmptyState icon={FileText} title="没有日志" />
        ) : (
          <div className="max-h-[calc(100vh-340px)] overflow-y-auto divide-y divide-border">
            {filtered.map((log) => <LogRow key={log.id} log={log} />)}
            <div ref={logEndRef} />
          </div>
        )}
      </Card>

      <Dialog open={confirmClear} onOpenChange={setConfirmClear}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认清空日志？</DialogTitle>
            <DialogDescription>此操作不可撤销。</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmClear(false)}>取消</Button>
            <Button variant="destructive" onClick={() => { clear.mutate(); setConfirmClear(false); }}>
              确认清空
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function LogRow({ log }: { log: LogEntry }) {
  const { hidden } = useHideIp();
  const tone =
    log.level === "ERROR" ? "danger" :
    log.level === "WARNING" ? "warning" :
    log.level === "DEBUG" ? "default" : "info";
  return (
    <div className="px-4 py-2 flex items-start gap-3 text-[12px] hover:bg-muted">
      <span className="font-mono text-muted-foreground w-32 flex-shrink-0">
        {new Date(log.timestamp).toLocaleString("zh-CN", {
          month: "2-digit",
          day: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        })}
      </span>
      <Chip tone={tone as any} className="font-mono w-16 justify-center">{log.level}</Chip>
      <span className="font-mono text-muted-foreground w-28 truncate flex-shrink-0">[{log.source}]</span>
      <span className="flex-1 break-words">{maskLogLine(log.message, hidden)}</span>
    </div>
  );
}
