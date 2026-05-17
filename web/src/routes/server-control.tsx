import { createFileRoute } from "@tanstack/react-router";
import { Terminal, Server, RefreshCw, Eye, EyeOff, CalendarClock, CalendarPlus, Repeat, Activity, Network } from "lucide-react";
import { useEffect, useState } from "react";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Chip } from "@/components/common/Chip";
import { StatusDot } from "@/components/common/StatusDot";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import {
  useOwnedServers,
  useServerServiceInfo,
  useServerMonitoring,
  useToggleMonitoring,
  type OwnedServer,
} from "@/hooks/use-server-control";
import { useHideIp, maskSensitive } from "@/hooks/use-hide-ip";
import { OverviewTab } from "@/components/server-control/OverviewTab";
import { PowerTab } from "@/components/server-control/PowerTab";
import { MaintenanceTab } from "@/components/server-control/MaintenanceTab";
import { AdvancedTab } from "@/components/server-control/AdvancedTab";
import { NetworkSpecsDialog } from "@/components/server-control/NetworkSpecsDialog";
import { toast } from "sonner";

/** 服务器控制中心：顶部下拉切换服务器 + 4 tab 详情 */
export const Route = createFileRoute("/server-control")({
  component: ServerControlPage,
});

function ServerControlPage() {
  const q = useOwnedServers();
  const { hidden, toggle } = useHideIp();
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const servers = q.data || [];

  // 自动选中第一台（首次加载或切换列表后）
  useEffect(() => {
    if (servers.length === 0) {
      setSelectedName(null);
      return;
    }
    if (!selectedName || !servers.some((s) => s.serviceName === selectedName)) {
      setSelectedName(servers[0].serviceName);
    }
  }, [servers, selectedName]);

  const selected = servers.find((s) => s.serviceName === selectedName) || null;

  return (
    <div className="space-y-6">
      <PageHeader
        icon={Terminal}
        title="服务器控制"
        description="管理您的 OVH 独立服务器"
        action={
          <div className="flex items-center gap-2">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="outline" size="icon" onClick={toggle} aria-label={hidden ? "显示 IP / MAC" : "隐藏 IP"}>
                  {hidden ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{hidden ? "已隐藏敏感信息 · 点击显示" : "隐藏 IP"}</TooltipContent>
            </Tooltip>
            <Button variant="outline" onClick={() => q.refetch()} disabled={q.isFetching}>
              <RefreshCw className={`w-4 h-4 ${q.isFetching ? "animate-spin" : ""}`} />
              刷新
            </Button>
          </div>
        }
      />

      {q.isPending ? (
        <Skeleton className="h-[500px] rounded-2xl" />
      ) : servers.length === 0 ? (
        <Card>
          <EmptyState
            icon={Server}
            title="暂无服务器"
            description="您的 OVH 账户下还没有独立服务器，或 API 没拿到数据"
          />
        </Card>
      ) : (
        <Card>
          <CardContent className="p-6 space-y-5">
            {/* 服务器切换器 + 当前选中卡概览 */}
            <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 pb-5 border-b border-border">
              <div className="flex items-center gap-3 min-w-0">
                <ServerSelector
                  servers={servers}
                  selected={selected}
                  onChange={(name) => setSelectedName(name)}
                  hidden={hidden}
                />
                {selected && (
                  <Chip tone={selected.state === "ok" ? "success" : "warning"}>
                    <StatusDot tone={selected.state === "ok" ? "success" : "warning"} pulse={selected.state === "ok"} size="xs" />
                    {selected.state}
                  </Chip>
                )}
              </div>
              {selected && (
                <div className="text-[12px] text-muted-foreground truncate font-mono">
                  {selected.commercialRange} · {selected.datacenter.toUpperCase()} · {maskSensitive(selected.ip, hidden)}
                </div>
              )}
            </div>

            {/* Tabs */}
            {selected && <ServerTabs server={selected} />}
          </CardContent>
        </Card>
      )}
    </div>
  );
}

/**
 * 顶部服务器选择器：胶囊样式 Select，选项显示 planCode + commercialRange · DC
 */
function ServerSelector({
  servers,
  selected,
  onChange,
  hidden,
}: {
  servers: OwnedServer[];
  selected: OwnedServer | null;
  onChange: (serviceName: string) => void;
  hidden: boolean;
}) {
  return (
    <Select value={selected?.serviceName || ""} onValueChange={onChange}>
      <SelectTrigger className="rounded-full min-w-[280px] sm:min-w-[340px] h-10 font-mono text-sm">
        <SelectValue placeholder="选择服务器" />
      </SelectTrigger>
      <SelectContent className="max-h-[400px]">
        {servers.map((s) => (
          <SelectItem key={s.serviceName} value={s.serviceName} className="font-mono">
            <div className="flex items-center gap-2">
              <StatusDot tone={s.state === "ok" ? "success" : "warning"} size="xs" />
              <span className="font-semibold">{maskSensitive(s.name, hidden)}</span>
              <span className="text-[11px] text-muted-foreground font-sans ml-1">
                {s.commercialRange} · {s.datacenter.toUpperCase()}
              </span>
            </div>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function ServerTabs({ server }: { server: OwnedServer }) {
  const info = useServerServiceInfo(server.serviceName);
  const monitoring = useServerMonitoring(server.serviceName);
  const toggleMon = useToggleMonitoring();
  const [netSpecsOpen, setNetSpecsOpen] = useState(false);

  const handleToggleMonitoring = async () => {
    try {
      await toggleMon.mutateAsync({ serviceName: server.serviceName, enabled: !monitoring.data });
      toast.success(monitoring.data ? "OVH 监控已关闭" : "OVH 监控已开启");
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "操作失败");
    }
  };

  return (
    <>
      <Tabs defaultValue="overview">
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <TabsList>
            <TabsTrigger value="overview">概览</TabsTrigger>
            <TabsTrigger value="power">电源与系统</TabsTrigger>
            <TabsTrigger value="maintenance">维护</TabsTrigger>
            <TabsTrigger value="advanced">高级</TabsTrigger>
          </TabsList>

          {/* 服务信息胶囊条 + 全局开关 */}
          {info.isPending ? (
            <div className="flex flex-wrap gap-2">
              <Skeleton className="h-7 w-28 rounded-full" />
              <Skeleton className="h-7 w-28 rounded-full" />
              <Skeleton className="h-7 w-20 rounded-full" />
              <Skeleton className="h-7 w-32 rounded-full" />
            </div>
          ) : (
            <div className="flex flex-wrap gap-2 items-center">
              {info.data && (
                <>
                  <InfoPill
                    icon={<CalendarClock className="w-3.5 h-3.5" />}
                    label="到期"
                    value={info.data.expiration ? new Date(info.data.expiration).toLocaleDateString("zh-CN") : "—"}
                  />
                  <InfoPill
                    icon={<CalendarPlus className="w-3.5 h-3.5" />}
                    label="开通"
                    value={info.data.creation ? new Date(info.data.creation).toLocaleDateString("zh-CN") : "—"}
                  />
                  <InfoPill
                    icon={<Repeat className="w-3.5 h-3.5" />}
                    label="续费"
                    value={info.data.renewalType ? "自动" : "手动"}
                  />
                  <InfoPill icon={<Terminal className="w-3.5 h-3.5" />} label="OS" value={server.os || "—"} />
                </>
              )}

              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 rounded-full"
                    onClick={handleToggleMonitoring}
                    disabled={toggleMon.isPending}
                  >
                    <Activity className={`w-3.5 h-3.5 mr-1 ${monitoring.data ? "text-success" : "text-muted-foreground"}`} />
                    {monitoring.data ? "监控 已开" : "监控 已关"}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>OVH 自动监控（异常会邮件通知）</TooltipContent>
              </Tooltip>

              <Tooltip>
                <TooltipTrigger asChild>
                  <Button variant="outline" size="sm" className="h-7 rounded-full" onClick={() => setNetSpecsOpen(true)}>
                    <Network className="w-3.5 h-3.5 mr-1" />
                    网络规格
                  </Button>
                </TooltipTrigger>
                <TooltipContent>带宽四档 + IPv4 / IPv6 路由</TooltipContent>
              </Tooltip>
            </div>
          )}
        </div>

        <TabsContent value="overview">
          <OverviewTab server={server} />
        </TabsContent>
        <TabsContent value="power">
          <PowerTab server={server} />
        </TabsContent>
        <TabsContent value="maintenance">
          <MaintenanceTab server={server} />
        </TabsContent>
        <TabsContent value="advanced">
          <AdvancedTab server={server} />
        </TabsContent>
      </Tabs>

      <NetworkSpecsDialog
        serviceName={server.serviceName}
        open={netSpecsOpen}
        onOpenChange={setNetSpecsOpen}
      />
    </>
  );
}

/** 紧凑胶囊：服务信息条的单元素 */
function InfoPill({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="inline-flex items-center gap-1.5 h-7 pl-2.5 pr-3 rounded-full border border-border bg-secondary/50 text-[12px]">
      <span className="flex items-center gap-1 text-muted-foreground">
        {icon}
        {label}
      </span>
      <span className="font-medium text-foreground">{value}</span>
    </div>
  );
}
