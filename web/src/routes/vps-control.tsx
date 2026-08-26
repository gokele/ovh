import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import {
  Cloud, Power, PowerOff, RefreshCw, Monitor, KeyRound, HardDrive, Cpu, MemoryStick,
  MapPin, Globe, CalendarClock, CalendarPlus, Repeat, Eye, EyeOff,
  AlertTriangle, ListTodo, Terminal, User } from "lucide-react";
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
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  useOwnedVps, useVpsServiceInfo, useVpsIps, useVpsCurrentOS,
  useVpsStart, useVpsStop, useVpsReboot, useVpsConsoleUrl, useVpsSetPassword,
  useUpdateVpsRenewal, useChangeVpsContact,
  useTerminateVps, useConfirmTerminateVps,
  useVpsEngagement, useVpsEngagementAvailable, useVpsEngagementRequest,
  useCreateVpsEngagementRequest, useDeleteVpsEngagementRequest, useUpdateVpsEngagementEndRule,
  useVpsOptions, useVpsServiceStatus,
  type OwnedVps,
} from "@/hooks/use-vps-control";
import { isUsEndpoint, regionLabel, regionLabelOf, endpointRegion } from "@/lib/ovh-regions";
import { useHideIp, maskSensitive } from "@/hooks/use-hide-ip";
import { useActiveServerControlAccount } from "@/hooks/use-active-account";
import { useAccounts } from "@/hooks/use-accounts";
import { useServerAliases, useSetServerAlias, aliasOf } from "@/hooks/use-server-aliases";
import { VpsSnapshotPane } from "@/components/vps-control/VpsSnapshotPane";
import { VpsReinstallDialog } from "@/components/vps-control/VpsReinstallDialog";
import { VpsMitigationPane } from "@/components/vps-control/VpsMitigationPane";
import { VpsTasksDialog } from "@/components/vps-control/VpsTasksDialog";
import { useUpdateVpsTerminationPolicy } from "@/hooks/use-vps-control";
import { RenewalDialog } from "@/components/server-control/RenewalDialog";
import { EngagementDialog, type EngagementHooks } from "@/components/server-control/EngagementDialog";
import { toast } from "sonner";

export const Route = createFileRoute("/vps-control")({
  component: VpsControlPage,
});

function VpsControlPage() {
  const q = useOwnedVps();
  const { hidden, toggle } = useHideIp();
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [activeAccount, setActiveAccount] = useActiveServerControlAccount();
  const { data: accounts } = useAccounts();
  const vpsList = q.data || [];

  useEffect(() => {
    if (!activeAccount && accounts && accounts.length > 0) {
      const def = accounts.find((a) => a.isDefault) || accounts[0];
      setActiveAccount(def.id || "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accounts]);

  useEffect(() => {
    setSelectedName(null);
  }, [activeAccount]);

  // 自动选第一台
  useEffect(() => {
    if (!selectedName && vpsList.length > 0) {
      setSelectedName(vpsList[0].serviceName);
    }
  }, [vpsList, selectedName]);

  const selected = vpsList.find((v) => v.serviceName === selectedName) || null;
  // 大区判定统一走 lib/ovh-regions(对齐后端 ovh.EndpointRegion):endpoint 是用户可填的自由字符串,
  // 还有 kimsufi-* / soyoustart-* 品牌别名,散装 `=== "ovh-us"` 漏一种写法门控就失效。
  const activeEndpoint = (accounts || []).find((a) => a.id === activeAccount)?.endpoint || "";
  const aliases = useServerAliases();
  const setAlias = useSetServerAlias();

  return (
    <div className="space-y-4">
      <PageHeader
        title="VPS 控制"
        description="已购 VPS 的电源 / 快照 / 重装 / 控制台管理"
        icon={Cloud}
        action={
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={toggle}>
              {hidden ? <EyeOff className="w-4 h-4 mr-1" /> : <Eye className="w-4 h-4 mr-1" />}
              {hidden ? "显示 IP" : "隐藏 IP"}
            </Button>
            <Button variant="outline" size="sm" onClick={() => q.refetch()} disabled={q.isFetching}>
              <RefreshCw className={"w-4 h-4 mr-1" + (q.isFetching ? " animate-spin" : "")} />
              刷新
            </Button>
          </div>
        }
      />

      {/* 账户 + VPS 选择 */}
      <Card>
        <CardContent className="p-4 flex flex-wrap items-center gap-3">
          {/* 账户在左侧菜单栏统一切换,这里只显示 */}
          <div className="flex items-center gap-2">
            <span className="text-[12px] text-muted-foreground">账户</span>
            <span className="inline-flex items-center gap-1.5 h-9 px-3 rounded-md border border-border text-[12px]">
              <User className="w-3.5 h-3.5 text-muted-foreground" />
              {(accounts || []).find((a) => a.id === activeAccount)?.name || "未选择"}
            </span>
          </div>
          <div className="flex items-center gap-2 flex-1 min-w-[280px]">
            <span className="text-[12px] text-muted-foreground">VPS</span>
            <Select value={selectedName || ""} onValueChange={setSelectedName}>
              <SelectTrigger className="h-9 w-full max-w-md">
                <SelectValue placeholder={vpsList.length === 0 ? "无 VPS" : "选择 VPS"} />
              </SelectTrigger>
              <SelectContent>
                {vpsList.map((v) => {
                  const label = aliasOf(aliases.data, v.serviceName, v.displayName || v.serviceName);
                  return (
                    <SelectItem key={v.serviceName} value={v.serviceName}>
                      <span className="flex items-center gap-2">
                        <StatusDot tone={v.state === "running" ? "success" : v.state === "stopped" ? "warning" : "muted"} />
                        <span className="truncate">{label}</span>
                        <span className="text-[10px] text-muted-foreground font-mono">{v.serviceName}</span>
                      </span>
                    </SelectItem>
                  );
                })}
              </SelectContent>
            </Select>
          </div>
          <div className="text-[11px] text-muted-foreground">
            共 {vpsList.length} 台
            {q.isFetching && " · 同步中…"}
          </div>
        </CardContent>
      </Card>

      {/* 内容区 */}
      {!q.isPending && vpsList.length === 0 ? (
        <Card>
          <CardContent className="py-12">
            <EmptyState icon={Cloud} title="该账户下暂无 VPS" description="可以去 OVH 官网下单,或换个有 VPS 的账户" />
          </CardContent>
        </Card>
      ) : selected ? (
        <VpsDetail
          server={selected}
          aliases={aliases}
          onSetAlias={setAlias}
          isUS={isUsEndpoint(activeEndpoint)}
          region={regionLabel(endpointRegion(activeEndpoint))}
        />
      ) : q.isPending ? (
        <Skeleton className="h-96 rounded-2xl" />
      ) : null}
    </div>
  );
}

/**
 * VPS 附加选项。
 *
 * 为什么要单独展示 manageEndpointsAvailable:三区的 vps.VpsOptionEnum 完全一致,
 * 所以美区账户照样会在 /vps/{sn}/option 里列出 ftpbackup / veeam,
 * 但管理它们的整套端点(/vps/{sn}/backupftp、/vps/{sn}/veeam)在 api.us.ovhcloud.com 上不存在。
 * 后端(handlers/vps_control_misc.go)给这两行打了 manageEndpointsAvailable:false + unsupportedReason,
 * 这里如实显示 —— 否则用户只看到"选项已开通",却永远找不到入口,以为是面板漏做了。
 */
function VpsOptionsPanel({ serviceName, region }: { serviceName: string; region: string }) {
  const q = useVpsOptions(serviceName);
  const list = q.data || [];

  return (
    <div className="border border-border rounded-2xl p-4 space-y-3">
      <h3 className="text-sm font-semibold">附加选项</h3>
      {q.isPending ? (
        <Skeleton className="h-12 rounded-md" />
      ) : q.isError ? (
        <p className="text-[12px] text-destructive">附加选项读取失败,请刷新重试</p>
      ) : list.length === 0 ? (
        <p className="text-[12px] text-muted-foreground">该 VPS 没有附加选项</p>
      ) : (
        <div className="space-y-2">
          {list.map((opt) => {
            const manageable = opt.manageEndpointsAvailable !== false;
            return (
              <div key={opt.option} className="border border-border rounded-xl px-3 py-2 space-y-1">
                <div className="flex items-center gap-2 flex-wrap">
                  <code className="font-mono text-[12px]">{opt.option}</code>
                  {opt.state && <Chip tone="default" className="text-[10px]">{opt.state}</Chip>}
                  {!manageable && (
                    <Chip tone="warning" className="text-[10px]">
                      {region}无管理接口
                    </Chip>
                  )}
                </div>
                {!manageable && opt.unsupportedReason && (
                  <p className="text-[11px] text-muted-foreground">
                    {opt.unsupportedReason}
                    {/* 只是"没有专属管理端点",退订接口三区都在,所以别让用户以为这项取消不掉 */}
                    <span className="block mt-0.5">（该选项仍在计费和生效中，退订接口不受影响）</span>
                  </p>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

/**
 * VPS 网络服务探测(ping / dns / http / https / smtp / ssh)。
 * /vps/{sn}/status 只在 EU 与 CA 两站注册,US 站点整片 vps 命名空间里没有这条路径,
 * 后端返 200 + status:null + unsupported:true + region:"US"。
 * 这里直接消费后端结论(unsupported / region),不自己再判一次 endpoint。
 */
function VpsServiceStatusPanel({ serviceName, region }: { serviceName: string; region: string }) {
  const q = useVpsServiceStatus(serviceName);
  const data = q.data;
  if (q.isPending) return <Skeleton className="h-12 rounded-2xl" />;
  if (q.isError) return null;

  if (data?.unsupported) {
    return (
      <div className="border border-border rounded-2xl p-3 bg-secondary/30 text-[11px] text-muted-foreground">
        网络服务探测:{data.message || `${regionLabelOf(data.region) || region}没有这个 OVH 端点`}
      </div>
    );
  }
  const entries = Object.entries(data?.status || {});
  if (entries.length === 0) return null;
  return (
    <div className="border border-border rounded-2xl overflow-hidden">
      <div className="px-4 py-3 border-b border-border flex items-center gap-2">
        <Terminal className="w-4 h-4 text-muted-foreground" />
        <h3 className="text-sm font-semibold">网络服务探测</h3>
        <span className="text-[11px] text-muted-foreground ml-auto">OVH 侧端口存活,与 VPS 电源状态无关</span>
      </div>
      <div className="px-4 py-3 flex flex-wrap gap-x-4 gap-y-2 text-[12px]">
        {entries.map(([k, v]) => (
          <span key={k} className="flex items-center gap-1.5">
            {typeof v === "boolean" ? (
              <StatusDot tone={v ? "success" : "danger"} size="xs" />
            ) : null}
            <span className="text-muted-foreground">{k}</span>
            {typeof v !== "boolean" && <span className="font-mono">{String(v)}</span>}
          </span>
        ))}
      </div>
    </div>
  );
}

/* ────────────── VPS 详情区 ────────────── */

function VpsDetail({
  server,
  aliases,
  onSetAlias,
  isUS,
  region,
}: {
  server: OwnedVps;
  aliases: ReturnType<typeof useServerAliases>;
  onSetAlias: ReturnType<typeof useSetServerAlias>;
  isUS: boolean;
  /** 账户所在大区的中文名（欧区 / 美区 / 加区），只用于文案 */
  region: string;
}) {
  const info = useVpsServiceInfo(server.serviceName);
  const ips = useVpsIps(server.serviceName);
  const currentOS = useVpsCurrentOS(server.serviceName);
  const { hidden } = useHideIp();
  const start = useVpsStart(server.serviceName);
  const stop = useVpsStop(server.serviceName);
  const reboot = useVpsReboot(server.serviceName);
  const console_ = useVpsConsoleUrl(server.serviceName);
  const setPwd = useVpsSetPassword(server.serviceName);
  const terminate = useTerminateVps();
  const confirmTerm = useConfirmTerminateVps();

  const [reinstallOpen, setReinstallOpen] = useState(false);
  const [setPwdOpen, setSetPwdOpen] = useState(false);
  const [stopOpen, setStopOpen] = useState(false);
  const [terminateOpen, setTerminateOpen] = useState(false);
  const [termToken, setTermToken] = useState("");
  const [renewalOpen, setRenewalOpen] = useState(false);
  // 终止策略端点 VPS 和独服不同，必须把 VPS 自己的注入给共用对话框，
  // 否则会打到 /server-control 上去
  const termPolicyVps = useUpdateVpsTerminationPolicy();
  const vpsTermination = {
    policy: {
      mutateAsync: (vars: { policy: string }) =>
        termPolicyVps.mutateAsync({ serviceName: server.serviceName, policy: vars.policy }),
      isPending: termPolicyVps.isPending,
    },
  };
  const [contactOpen, setContactOpen] = useState(false);
  const [engagementOpen, setEngagementOpen] = useState(false);
  const [tasksOpen, setTasksOpen] = useState(false);
  const renewalMutation = useUpdateVpsRenewal(server.serviceName);
  const contactMutation = useChangeVpsContact();

  // 把 VPS engagement hooks 打包成 EngagementHooks bundle 传给共用对话框
  const vpsEngagementHooks: EngagementHooks = {
    useEngagement: useVpsEngagement,
    useEngagementAvailable: useVpsEngagementAvailable,
    useEngagementRequest: useVpsEngagementRequest,
    useCreateEngagementRequest: useCreateVpsEngagementRequest,
    useDeleteEngagementRequest: useDeleteVpsEngagementRequest,
    useUpdateEngagementEndRule: useUpdateVpsEngagementEndRule,
  };

  const isRunning = server.state === "running";
  const isStopped = server.state === "stopped";

  // 状态文案 + 配色 —— 全集来自 OVH vps.VpsStateEnum:
  //   backuping / installing / maintenance / rebooting / rescued / running / stopped / stopping / upgrading
  const stateLabel: Record<string, { text: string; tone: "success" | "warning" | "danger" | "default" }> = {
    running: { text: "运行中", tone: "success" },
    stopped: { text: "已关机", tone: "warning" },
    stopping: { text: "关机中", tone: "warning" },
    rebooting: { text: "重启中", tone: "warning" },
    installing: { text: "装机中", tone: "warning" },
    backuping: { text: "备份中", tone: "warning" },
    upgrading: { text: "升级中", tone: "warning" },
    maintenance: { text: "维护中", tone: "warning" },
    rescued: { text: "救援模式", tone: "danger" }, // 用 danger 警示用户还没退出救援
  };
  const sLabel = stateLabel[server.state] || { text: server.state, tone: "default" as const };

  const handleStart = async () => {
    try {
      await start.mutateAsync();
      toast.success("启动任务已提交");
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "启动失败");
    }
  };
  const handleStop = async () => {
    try {
      await stop.mutateAsync();
      toast.success("关机任务已提交");
      setStopOpen(false);
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "关机失败");
    }
  };
  const handleReboot = async () => {
    if (!confirm("确认重启 VPS?")) return;
    try {
      await reboot.mutateAsync();
      toast.success("重启任务已提交");
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "重启失败");
    }
  };
  const handleConsole = async () => {
    try {
      const url = await console_.mutateAsync();
      if (url) {
        window.open(url, "_blank", "noopener,noreferrer");
        toast.success("控制台 URL 已生成,5 分钟内有效");
      } else {
        toast.error("获取控制台 URL 失败");
      }
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "获取失败");
    }
  };
  const handleSetPwd = async () => {
    try {
      await setPwd.mutateAsync();
      toast.success("新密码已发送至邮箱");
      setSetPwdOpen(false);
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "重置失败");
    }
  };
  const handleTerminate = async () => {
    try {
      const res = await terminate.mutateAsync({ serviceName: server.serviceName });
      toast.success("终止请求已提交,请查邮件获取 token");
      // 服务端响应里会带 token,但实际操作 OVH 用邮件 token 才能真正确认,这里保留邮件 token 输入
      console.log("[VPS] terminate response:", res);
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "提交失败");
    }
  };
  const handleConfirmTerm = async () => {
    if (!termToken) return toast.error("请输入邮件里的 token");
    try {
      await confirmTerm.mutateAsync({ serviceName: server.serviceName, token: termToken });
      toast.success("VPS 已确认终止");
      setTerminateOpen(false);
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "确认失败");
    }
  };

  const ipDisplay = ips.data && ips.data.length > 0 ? maskSensitive(ips.data[0].ipAddress, hidden) : "—";

  return (
    <>
      <Tabs defaultValue="overview">
        {/* 顶部信息条 + 工具按钮 */}
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <TabsList className="grid grid-cols-4 sm:flex h-auto gap-1 p-1">
            <TabsTrigger value="overview" className="text-[12px] sm:text-sm px-2 sm:px-3">概览</TabsTrigger>
            <TabsTrigger value="snapshot" className="text-[12px] sm:text-sm px-2 sm:px-3">快照</TabsTrigger>
            <TabsTrigger value="ddos" className="text-[12px] sm:text-sm px-2 sm:px-3">DDoS</TabsTrigger>
            <TabsTrigger value="maintenance" className="text-[12px] sm:text-sm px-2 sm:px-3">维护</TabsTrigger>
          </TabsList>

          <div className="flex flex-wrap gap-2 items-center">
            <Chip tone={sLabel.tone}>{sLabel.text}</Chip>
            {currentOS.data && (
              <button
                type="button"
                onClick={() => setReinstallOpen(true)}
                className="inline-flex items-center gap-1.5 h-7 pl-2.5 pr-3 rounded-full border border-border bg-background hover:bg-muted cursor-pointer transition-colors shadow-sm text-[12px]"
                title="点击进入重装系统"
              >
                <Terminal className="w-3.5 h-3.5 text-muted-foreground" />
                <span className="text-muted-foreground">系统</span>
                <span className="font-medium truncate max-w-[220px]">{currentOS.data.name}</span>
              </button>
            )}
            {info.data?.expiration && (
              <span className="inline-flex items-center gap-1.5 h-7 pl-2.5 pr-3 rounded-full border border-border bg-secondary/50 text-[12px]">
                <CalendarClock className="w-3.5 h-3.5 text-muted-foreground" />
                <span className="text-muted-foreground">到期</span>
                <span className="font-medium">{new Date(info.data.expiration).toLocaleDateString("zh-CN")}</span>
              </span>
            )}
            {info.data && (
              <button
                type="button"
                onClick={() => setRenewalOpen(true)}
                className="inline-flex items-center gap-1.5 h-7 pl-2.5 pr-3 rounded-full border border-border bg-background hover:bg-muted cursor-pointer transition-colors shadow-sm text-[12px]"
              >
                <Repeat className="w-3.5 h-3.5 text-muted-foreground" />
                <span className="text-muted-foreground">续费</span>
                <span className="font-medium">
                  {(info.data.terminationScheduled ?? info.data.renewalDeleteAtExpiration)
                    ? "到期终止"
                    : info.data.renewalForced
                      ? "强制自动"
                      : info.data.renewalType
                        ? "自动"
                        : "手动"}
                  {info.data.renewalPeriod > 0 ? ` · ${info.data.renewalPeriod}月` : ""}
                </span>
              </button>
            )}
          </div>
        </div>

        {/* 列表接口这次没拿到这台 VPS 的详情/计费信息。
            「没查到」不能显示成「没开自动续费」或「unknown 状态」，那会让用户按错误前提去操作。 */}
        {(server.error || server.renewalType === null || server.status === null) && (
          <div className="flex items-start gap-2 rounded-xl border border-warning/40 bg-warning/5 px-3 py-2 text-[11px] text-foreground/80 mt-3">
            <AlertTriangle className="w-3.5 h-3.5 text-warning flex-shrink-0 mt-0.5" />
            <span>
              {server.error
                ? `该 VPS 的详情未能获取（${server.error}），下方配置信息可能不完整。`
                : "该 VPS 的续费 / 计费信息这次没查到，列表里的续费状态显示为「未知」而非「手动」，请以下方「续费」胶囊为准。"}
            </span>
          </div>
        )}

        {/* 顶部硬件信息卡(VPS 简化版) */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-2.5 sm:gap-3 mt-4">
          <InfoCard icon={<Cpu className="w-4 h-4" />} label="vCore" value={String(server.vcore || "—")} />
          <InfoCard
            icon={<MemoryStick className="w-4 h-4" />}
            label="内存"
            value={server.memoryMB ? `${(server.memoryMB / 1024).toFixed(0)} GB` : "—"}
          />
          <InfoCard
            icon={<HardDrive className="w-4 h-4" />}
            label="磁盘"
            value={server.diskGB ? `${server.diskGB} GB` : "—"}
          />
          <InfoCard
            icon={<MapPin className="w-4 h-4" />}
            label="区域"
            value={humanZoneShort(server.zone)}
          />
        </div>

        {/* 电源 / 控制台 / 重装 / 改密 按钮行 */}
        <div className="mt-4 border border-border rounded-2xl p-3 flex flex-wrap gap-2">
          {isStopped && (
            <Button onClick={handleStart} disabled={start.isPending}>
              <Power className="w-4 h-4 mr-1" />
              启动
            </Button>
          )}
          {isRunning && (
            <Button variant="outline" onClick={() => setStopOpen(true)} disabled={stop.isPending}>
              <PowerOff className="w-4 h-4 mr-1" />
              关机
            </Button>
          )}
          <Button variant="outline" onClick={handleReboot} disabled={reboot.isPending || !isRunning}>
            <RefreshCw className="w-4 h-4 mr-1" />
            重启
          </Button>
          <Button variant="outline" onClick={handleConsole} disabled={console_.isPending}>
            <Monitor className="w-4 h-4 mr-1" />
            Web 控制台
          </Button>
          <Button variant="outline" onClick={() => setReinstallOpen(true)}>
            <HardDrive className="w-4 h-4 mr-1" />
            重装系统
          </Button>
          {!isUS && (
            <Button variant="outline" onClick={() => setSetPwdOpen(true)}>
              <KeyRound className="w-4 h-4 mr-1" />
              重置密码
            </Button>
          )}
          <Button variant="outline" onClick={() => setEngagementOpen(true)}>
            <CalendarPlus className="w-4 h-4 mr-1" />
            合同期
          </Button>
          <Button variant="outline" onClick={() => setTasksOpen(true)}>
            <ListTodo className="w-4 h-4 mr-1" />
            任务历史
          </Button>
        </div>

        {/* 概览 Tab */}
        <TabsContent value="overview" className="mt-4 space-y-4">
          {/* 异常状态警示:锁定 / 救援模式 */}
          {server.lockStatus && server.lockStatus !== "unlocked" && (
            <div className="border border-destructive/40 bg-destructive/5 rounded-xl p-3 flex items-start gap-2.5">
              <AlertTriangle className="w-4 h-4 text-destructive mt-0.5 flex-shrink-0" />
              <div className="text-[12px]">
                <p className="font-semibold text-destructive">VPS 已锁定</p>
                <p className="text-muted-foreground mt-0.5">
                  状态: <code className="font-mono">{server.lockStatus}</code> ·
                  通常因投诉(abuse)被 OVH 临时冻结,联系 OVH 客服处理
                </p>
              </div>
            </div>
          )}
          {server.netbootMode === "rescue" && (
            <div className="border border-amber-500/40 bg-amber-500/5 rounded-xl p-3 flex items-start gap-2.5">
              <AlertTriangle className="w-4 h-4 text-amber-600 dark:text-amber-400 mt-0.5 flex-shrink-0" />
              <div className="text-[12px]">
                <p className="font-semibold text-amber-700 dark:text-amber-300">救援模式</p>
                <p className="text-muted-foreground mt-0.5">
                  下次重启会进入 OVH 救援镜像。修完故障后需要把 netboot 改回 <code>local</code> 再重启回正常系统
                </p>
              </div>
            </div>
          )}

          {/* IP 列表 */}
          <div className="border border-border rounded-2xl overflow-hidden">
            <div className="px-4 py-3 border-b border-border flex items-center gap-2">
              <Globe className="w-4 h-4 text-muted-foreground" />
              <h3 className="text-sm font-semibold">IP 地址</h3>
              <span className="text-[11px] text-muted-foreground ml-auto">主 IP: {ipDisplay}</span>
            </div>
            {ips.isPending ? (
              <div className="p-4">
                <Skeleton className="h-16 rounded-md" />
              </div>
            ) : (ips.data || []).length === 0 ? (
              <p className="px-4 py-6 text-sm text-muted-foreground text-center">无 IP</p>
            ) : (
              <div className="divide-y divide-border">
                {(ips.data || []).map((ip) => (
                  <div key={ip.ipAddress} className="px-4 py-3 flex items-center gap-2 text-[13px] flex-wrap">
                    <code className="font-mono">{maskSensitive(ip.ipAddress, hidden)}</code>
                    {ip.version && <span className="text-[10px] text-muted-foreground">{ip.version}</span>}
                    {ip.type && <span className="text-[10px] text-muted-foreground">{ip.type}</span>}
                    {ip.geolocation && <span className="text-[10px] text-muted-foreground">{ip.geolocation}</span>}
                    {ip.reverse && (
                      // 反向 DNS 里就编码着完整 IP(ip-54-38-222.eu 形态),
                      // IP 打了码、reverse 原样显示等于没打;title 同理
                      <span
                        className="ml-auto text-[11px] text-muted-foreground font-mono truncate"
                        title={maskSensitive(ip.reverse, hidden)}
                      >
                        ↩ {maskSensitive(ip.reverse, hidden)}
                      </span>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* 网络服务探测(EU/CA 有,US 没有这条 OVH 端点) */}
          <VpsServiceStatusPanel serviceName={server.serviceName} region={region} />

          {/* 一行底部:型号 + 开通日期 + 集群,放轻量 */}
          <p className="text-[11px] text-muted-foreground px-1">
            {server.model && <>型号 <code className="font-mono">{server.model}</code></>}
            {info.data?.creation && (
              <> · 开通 {new Date(info.data.creation).toLocaleDateString("zh-CN")}</>
            )}
            {server.cluster && <> · 集群 <code className="font-mono">{server.cluster}</code></>}
            {server.slaMonitoring && <> · 已开 SLA 监控</>}
          </p>
        </TabsContent>

        {/* 快照 Tab */}
        <TabsContent value="snapshot" className="mt-4">
          <VpsSnapshotPane serviceName={server.serviceName} />
        </TabsContent>

        {/* DDoS Tab */}
        <TabsContent value="ddos" className="mt-4">
          <VpsMitigationPane serviceName={server.serviceName} />
        </TabsContent>

        {/* 维护 Tab(终止 / 别名 / 后续可扩展) */}
        <TabsContent value="maintenance" className="mt-4 space-y-4">
          <div className="border border-border rounded-2xl p-4 space-y-3">
            <h3 className="text-sm font-semibold">服务器别名</h3>
            <p className="text-[12px] text-muted-foreground">本地别名,不影响 OVH 真实名(`{server.serviceName}`)</p>
            <AliasEditor serviceName={server.serviceName} aliases={aliases} onSetAlias={onSetAlias} />
          </div>

          <VpsOptionsPanel serviceName={server.serviceName} region={region} />

          {!isUS ? (
            <div className="border border-border rounded-2xl p-4 space-y-3">
              <h3 className="text-sm font-semibold">变更联系人</h3>
              <p className="text-[12px] text-muted-foreground">切换 OVH admin / tech / billing NIC(过户、子账户托管常用)</p>
              <Button variant="outline" size="sm" onClick={() => setContactOpen(true)}>
                <Repeat className="w-3.5 h-3.5 mr-1" />
                变更联系人
              </Button>
            </div>
          ) : (
            <div className="border border-border rounded-2xl p-4 space-y-2 bg-secondary/30">
              <h3 className="text-sm font-semibold text-muted-foreground">美区限制</h3>
              <p className="text-[12px] text-muted-foreground">
                US OVHcloud 是独立公司,以下功能不可用:**变更联系人 / 重置密码 / Backup FTP / IPMI 测试**。
                密码可在 Web 控制台进系统后用 <code>passwd</code> 自助改;过户需直接联系 OVH US 客服。
              </p>
            </div>
          )}

          <div className="border border-destructive/40 bg-destructive/5 rounded-2xl p-4 space-y-2">
            <div className="flex items-center gap-2">
              <AlertTriangle className="w-4 h-4 text-destructive" />
              <h3 className="text-sm font-semibold text-destructive">终止 VPS</h3>
            </div>
            <p className="text-[12px] text-muted-foreground">提交终止请求后 OVH 邮件发 token,确认后立即销毁。**数据不可恢复**。</p>
            <div className="flex gap-2 flex-wrap">
              <Button variant="destructive" size="sm" onClick={handleTerminate} disabled={terminate.isPending}>
                <CalendarPlus className="w-3.5 h-3.5 mr-1" />
                提交终止请求(收 token)
              </Button>
              <Button variant="outline" size="sm" onClick={() => setTerminateOpen(true)}>
                有 token,直接确认终止
              </Button>
            </div>
          </div>
        </TabsContent>
      </Tabs>

      {/* 重装对话框 */}
      <VpsReinstallDialog
        serviceName={server.serviceName}
        open={reinstallOpen}
        onOpenChange={setReinstallOpen}
      />

      {/* 续费策略弹窗 — 复用 server-control 的 RenewalDialog,传 VPS 自己的 mutation */}
      {info.data && (
        <RenewalDialog
          serviceName={server.serviceName}
          info={info.data}
          open={renewalOpen}
          onOpenChange={setRenewalOpen}
          mutation={renewalMutation}
          termination={vpsTermination}
        />
      )}

      {/* 变更联系人弹窗 */}
      <Dialog open={contactOpen} onOpenChange={setContactOpen}>
        <ChangeContactInline
          serviceName={server.serviceName}
          onClose={() => setContactOpen(false)}
          mutation={contactMutation}
        />
      </Dialog>

      {/* 合同期弹窗 — 复用 server-control 的 EngagementDialog,传 VPS hooks bundle */}
      <EngagementDialog
        serviceName={server.serviceName}
        open={engagementOpen}
        onOpenChange={setEngagementOpen}
        hooks={vpsEngagementHooks}
      />

      {/* 任务历史弹窗 */}
      <VpsTasksDialog
        serviceName={server.serviceName}
        open={tasksOpen}
        onOpenChange={setTasksOpen}
      />

      {/* 关机确认 */}
      <Dialog open={stopOpen} onOpenChange={setStopOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>确认关机?</DialogTitle>
            <DialogDescription>VPS 将立即停机,业务中断直到下次启动</DialogDescription>
          </DialogHeader>
          <p className="text-[12px] text-muted-foreground">
            注意:OVH 不会因为关机停止计费,VPS 仍占用 hypervisor 配额,只是物理上不再消耗 CPU/磁盘 IO。
          </p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setStopOpen(false)}>取消</Button>
            <Button variant="destructive" onClick={handleStop} disabled={stop.isPending}>
              {stop.isPending ? "提交中…" : "确认关机"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 重置密码确认 */}
      <Dialog open={setPwdOpen} onOpenChange={setSetPwdOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>重置 root/admin 密码?</DialogTitle>
            <DialogDescription>OVH 会生成新密码并发送至账户邮箱</DialogDescription>
          </DialogHeader>
          <p className="text-[12px] text-muted-foreground">
            如果你设置过 SSH key 登录,可以不动密码;新密码会立即覆盖旧密码,继续登录需要等邮件。
          </p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setSetPwdOpen(false)}>取消</Button>
            <Button onClick={handleSetPwd} disabled={setPwd.isPending}>
              {setPwd.isPending ? "提交中…" : "确认重置"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 确认终止(已收到邮件 token) */}
      <Dialog open={terminateOpen} onOpenChange={setTerminateOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>确认终止 VPS</DialogTitle>
            <DialogDescription>粘贴邮件里的 token,确认后 VPS 立即销毁</DialogDescription>
          </DialogHeader>
          <Input
            value={termToken}
            onChange={(e) => setTermToken(e.target.value)}
            placeholder="邮件中的 token 字符串"
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setTerminateOpen(false)}>取消</Button>
            <Button variant="destructive" onClick={handleConfirmTerm} disabled={!termToken || confirmTerm.isPending}>
              {confirmTerm.isPending ? "确认中…" : "确认终止"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

function InfoCard({
  icon, label, value,
}: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="border border-border rounded-xl px-3.5 py-3 flex items-center gap-3 min-w-0">
      <div className="w-9 h-9 rounded-lg bg-secondary flex items-center justify-center flex-shrink-0">{icon}</div>
      <div className="min-w-0">
        <div className="text-[11px] text-muted-foreground">{label}</div>
        <div className="text-[13px] font-semibold truncate" title={value}>{value}</div>
      </div>
    </div>
  );
}

/** humanZoneShort OVH zone 字段转可读名,InfoCard 用短版只显示中文标签。
 *  2025 cloud VPS 给 "Region OpenStack: os-us-west-or-2",老款给 "bhs"/"gra"/"sbg" 之类机房代号。 */
function humanZoneShort(zone: string): string {
  const friendly = zoneFriendly(zone);
  if (!friendly) return (zone || "—").toUpperCase();
  return friendly.label;
}

function zoneFriendly(zone: string): { label: string; code: string } | null {
  if (!zone) return null;
  const m = zone.match(/os-[a-z0-9-]+/i);
  const code = (m ? m[0] : zone.trim().toLowerCase().split(/\s+/).pop() || zone).toLowerCase();
  const label = OS_ZONE_MAP[code] || LEGACY_DC_MAP[code.slice(0, 3)];
  return label ? { label, code } : null;
}

// OpenStack 区域代号(2025+ cloud VPS / OVH Public Cloud 同款命名)
const OS_ZONE_MAP: Record<string, string> = {
  "os-us-west-or-1": "美国西部·俄勒冈",
  "os-us-west-or-2": "美国西部·俄勒冈",
  "os-us-east-va-1": "美国东部·弗吉尼亚",
  "os-eu-west-fr-1": "法国·格拉夫林",
  "os-eu-west-de-1": "德国·法兰克福",
  "os-eu-west-pl-1": "波兰·华沙",
  "os-eu-west-uk-1": "英国·伦敦",
  "os-eu-south-it-1": "意大利·米兰",
  "os-eu-north-fi-1": "芬兰·赫尔辛基",
  "os-ca-east-bhs-1": "加拿大·博阿尔诺",
  "os-asia-southeast-sg-1": "新加坡",
  "os-asia-south-in-1": "印度·孟买",
  "os-au-southeast-syd-1": "澳大利亚·悉尼",
};

// 老款 VPS / Dedicated 机房三字母代号
const LEGACY_DC_MAP: Record<string, string> = {
  bhs: "加拿大·博阿尔诺 (BHS)",
  gra: "法国·格拉夫林 (GRA)",
  rbx: "法国·鲁贝 (RBX)",
  sbg: "法国·斯特拉斯堡 (SBG)",
  waw: "波兰·华沙 (WAW)",
  fra: "德国·法兰克福 (FRA)",
  lon: "英国·伦敦 (LON)",
  lim: "德国·林堡 (LIM)",
  eri: "英国·埃里斯 (ERI)",
  vin: "美国·弗吉尼亚 (VIN)",
  hil: "美国·俄勒冈·希尔斯伯勒 (HIL)",
  sgp: "新加坡 (SGP)",
  syd: "澳大利亚·悉尼 (SYD)",
};


/** 简化版变更联系人(只提交新 NIC,不展示待审列表 —— VPS 频率低,不需要完整 UI) */
function ChangeContactInline({
  serviceName,
  onClose,
  mutation,
}: {
  serviceName: string;
  onClose: () => void;
  mutation: ReturnType<typeof useChangeVpsContact>;
}) {
  const [admin, setAdmin] = useState("");
  const [tech, setTech] = useState("");
  const [billing, setBilling] = useState("");

  const handleSubmit = async () => {
    if (!admin && !tech && !billing) {
      toast.error("请至少填一个联系人");
      return;
    }
    try {
      await mutation.mutateAsync({
        serviceName,
        admin: admin || undefined,
        tech: tech || undefined,
        billing: billing || undefined,
      });
      toast.success("变更请求已提交,等邮件确认");
      onClose();
      setAdmin(""); setTech(""); setBilling("");
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "提交失败");
    }
  };

  return (
    <DialogContent className="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>变更联系人</DialogTitle>
        <DialogDescription>切换 admin / tech / billing NIC。OVH 会发邮件给当前联系人确认。</DialogDescription>
      </DialogHeader>
      <div className="space-y-3 py-1">
        <div>
          <label className="text-[12px] font-semibold block mb-1.5">Admin 联系人</label>
          <Input value={admin} onChange={(e) => setAdmin(e.target.value)} placeholder="ab12345-ovh 或 someone@example.com" />
        </div>
        <div>
          <label className="text-[12px] font-semibold block mb-1.5">Tech 联系人</label>
          <Input value={tech} onChange={(e) => setTech(e.target.value)} placeholder="ab12345-ovh 或邮箱" />
        </div>
        <div>
          <label className="text-[12px] font-semibold block mb-1.5">Billing 联系人</label>
          <Input value={billing} onChange={(e) => setBilling(e.target.value)} placeholder="ab12345-ovh 或邮箱" />
        </div>
        <p className="text-[11px] text-muted-foreground">留空保持原联系人;待审请求可在 server-control 页面统一管理</p>
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>取消</Button>
        <Button onClick={handleSubmit} disabled={mutation.isPending}>
          {mutation.isPending ? "提交中…" : "提交变更"}
        </Button>
      </DialogFooter>
    </DialogContent>
  );
}

function AliasEditor({
  serviceName,
  aliases,
  onSetAlias,
}: {
  serviceName: string;
  aliases: ReturnType<typeof useServerAliases>;
  onSetAlias: ReturnType<typeof useSetServerAlias>;
}) {
  const [v, setV] = useState(aliasOf(aliases.data, serviceName, ""));
  return (
    <div className="flex gap-2">
      <Input value={v} onChange={(e) => setV(e.target.value)} placeholder="给这台 VPS 起个易记的名字" />
      <Button
        size="sm"
        onClick={async () => {
          try {
            await onSetAlias.mutateAsync({ serviceName, alias: v });
            toast.success("别名已保存");
          } catch (e: any) {
            toast.error(e?.message || "保存失败");
          }
        }}
        disabled={onSetAlias.isPending}
      >
        保存
      </Button>
    </div>
  );
}
