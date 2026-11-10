import { createFileRoute } from "@tanstack/react-router";
import { useHideIp } from "@/hooks/use-hide-ip";
import { useTranslation } from "react-i18next";
import { fmtPattern } from "@/i18n/format";
import { FileText, RefreshCw, Trash2, Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { PageHeader } from "@/components/common/PageHeader";
import { PearlSearchForm, PearlPanel, PearlPanelHead } from "@/components/pearl/ListKit";
import { BarChart } from "@/components/pearl/BarChart";
import { Pager, usePaged } from "@/components/pearl/Pager";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Checkbox } from "@/components/ui/checkbox";
import { Chip } from "@/components/common/Chip";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed, LoadFailedBanner } from "@/components/common/LoadFailed";
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
  const { t } = useTranslation();
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

  // 分页:日志流式追加,total 变化时 usePaged 自动夹回末页
  const PAGE_SIZE = 60;
  const { page, setPage } = usePaged(filtered.length, PAGE_SIZE);
  const pageItems = filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);
  const levelCounts = ["INFO", "WARNING", "ERROR", "DEBUG"].map((lv) => {
    const n = items.filter((l) => l.level === lv).length;
    return { label: lv.slice(0, 4), value: n, failed: lv === "ERROR" ? n : 0, highlight: lv === "ERROR" };
  });

  // 自动滚到底部：仅当 autoRefresh 开启且没有筛选条件（避免破坏用户的浏览位置）
  const logEndRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const lastPage = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
    if (autoRefresh && !search && levelFilter === "all" && page >= lastPage) {
      logEndRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
    }
  }, [filtered, autoRefresh, search, levelFilter]);

  // 面板体(加载/失败/空态/日志列表)—— 两套皮肤共用,只换外壳
  const panelBody = (
    <>
        {logs.isError && items.length > 0 && (
          <div className="p-3 pb-0">
            <LoadFailedBanner
              title={t("logs.refreshFailedTitle")}
              error={logs.error}
              onRetry={() => logs.refetch()}
            />
          </div>
        )}
        {logs.isPending && items.length === 0 ? (
          <div className="p-4 space-y-2">
            {Array.from({ length: 8 }).map((_, i) => <Skeleton key={i} className="h-8" />)}
          </div>
        ) : logs.isError && items.length === 0 ? (
          /* 日志页是排错的最后一招。这里说"没有日志",用户读到的是"系统一切正常、
             没什么可看的",于是停止排查 —— 而实际情况是我们连日志都没拿到。
             失败必须写成失败,并把后端返回的原因一起摆出来。 */
          <div className="p-4">
            <LoadFailed
              icon={FileText}
              title={t("logs.loadFailedTitle")}
              error={logs.error}
              onRetry={() => logs.refetch()}
              compact
            />
          </div>
        ) : filtered.length === 0 ? (
          /* 到这儿才是真空态。再分一次:后端确实没日志,还是被搜索/级别筛掉了 */
          <EmptyState
            illustration={items.length === 0 ? "logs" : "search"}
            title={items.length === 0 ? t("logs.emptyNoLogs") : t("logs.emptyFiltered")}
            description={items.length === 0 ? undefined : t("logs.emptyFilteredDesc", { total: items.length })}
          />
        ) : (
          <div className="max-h-[calc(100vh-340px)] overflow-y-auto divide-y divide-border">
            {pageItems.map((log) => <LogRow key={log.id} log={log} />)}
            <div ref={logEndRef} />
          </div>
        )}
    </>
  );

  return (
    <div className="space-y-3 sm:space-y-6">
      <PageHeader
        icon={FileText}
        title={t("logs.title")}
        description={t("logs.description")}
        action={
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="outline" onClick={() => logs.refetch()} disabled={logs.isFetching}>
              <RefreshCw className={`w-4 h-4 ${logs.isFetching ? "animate-spin" : ""}`} />
              {t("common.refresh")}
            </Button>
            <Button variant="outline" onClick={() => setConfirmClear(true)} disabled={items.length === 0}>
              <Trash2 className="w-4 h-4" />
              {t("logs.clear")}
            </Button>
          </div>
        }
      />

        <PearlSearchForm>
          <div className="relative field is-wide" style={{ flex: "1 1 240px", maxWidth: 480 }}>
            <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
            <Input
              placeholder={t("logs.searchPlaceholder")}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="pl-9"
            />
          </div>
          <div className="field" style={{ width: 150, flex: "0 0 auto" }}>
            <Select value={levelFilter} onValueChange={setLevelFilter}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("logs.allLevels")}</SelectItem>
                <SelectItem value="INFO">INFO</SelectItem>
                <SelectItem value="WARNING">WARNING</SelectItem>
                <SelectItem value="ERROR">ERROR</SelectItem>
                <SelectItem value="DEBUG">DEBUG</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="search-actions">
            <label className="inline-flex items-center gap-2 text-sm cursor-pointer">
              <Checkbox checked={autoRefresh} onCheckedChange={(c) => setAutoRefresh(c === true)} />
              <span>{t("logs.autoRefresh")}</span>
            </label>
          </div>
        </PearlSearchForm>

        {/* 横带分层:概览读数条 + 级别分布 */}
      <div style={{ display: "flex", gap: 14, flexWrap: "wrap", alignItems: "stretch" }}>
        <div className="app-card" style={{ flex: "1 1 260px", padding: "14px 18px", display: "flex", gap: 22, alignItems: "center" }}>
          <div className="metric metric-s">
            <span className="metric-label">{t("logs.bandTotal")}</span>
            <span className="metric-figure tnum" style={{ fontSize: 26 }}>{items.length}</span>
          </div>
          <div className="metric metric-s">
            <span className="metric-label">{t("logs.bandErrors")}</span>
            <span className="metric-figure tnum" style={{ fontSize: 26, color: "var(--red-text)" }}>
              {items.filter((l) => l.level === "ERROR").length}
            </span>
          </div>
          <div className="metric metric-s">
            <span className="metric-label">{t("logs.bandWarnings")}</span>
            <span className="metric-figure tnum" style={{ fontSize: 26, color: "var(--warning, #ac4705)" }}>
              {items.filter((l) => l.level === "WARNING").length}
            </span>
          </div>
        </div>
        <div className="app-card" style={{ flex: "1.4 1 320px", padding: "12px 18px" }}>
          <BarChart items={levelCounts} height={72} />
        </div>
      </div>

      <PearlPanel>
          <PearlPanelHead eyebrow={t("logs.systemLogs")} meta={t("logs.count", { count: filtered.length })} />
          <div className="panel-body">{panelBody}</div>
          <Pager page={page} pageSize={PAGE_SIZE} total={filtered.length} onChange={setPage} />
        </PearlPanel>

      <Dialog open={confirmClear} onOpenChange={setConfirmClear}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("logs.clearConfirmTitle")}</DialogTitle>
            <DialogDescription>{t("logs.clearConfirmDesc")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmClear(false)}>{t("common.cancel")}</Button>
            <Button variant="destructive" onClick={() => { clear.mutate(); setConfirmClear(false); }}>
              {t("logs.clearConfirm")}
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
        {fmtPattern(log.timestamp, "MM/dd HH:mm:ss")}
      </span>
      <Chip tone={tone as any} className="font-mono w-16 justify-center">{log.level}</Chip>
      <span className="font-mono text-muted-foreground w-28 truncate flex-shrink-0">[{log.source}]</span>
      <span className="flex-1 break-words">{maskLogLine(log.message, hidden)}</span>
    </div>
  );
}
