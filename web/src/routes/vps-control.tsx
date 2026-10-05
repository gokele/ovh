import { createFileRoute } from "@tanstack/react-router";
import { useTranslation, Trans } from "react-i18next";
import { terminationLabel } from "@/hooks/use-server-control";
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
import { LoadFailed, LoadFailedBanner, errorMessage } from "@/components/common/LoadFailed";
import { fmtDate } from "@/i18n/format";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  useOwnedVps, useVpsServiceInfo, useVpsIps, useVpsCurrentOS,
  useVpsStart, useVpsStop, useVpsReboot, useVpsConsoleUrl,
  useUpdateVpsRenewal, useChangeVpsContact,
  useTerminateVps, useConfirmTerminateVps,
  useVpsEngagement, useVpsEngagementAvailable, useVpsEngagementRequest,
  useCreateVpsEngagementRequest, useDeleteVpsEngagementRequest, useUpdateVpsEngagementEndRule,
  useVpsOptions,
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

/** 模块级文案助手共用的翻译函数类型 */
type TFn = ReturnType<typeof useTranslation>["t"];

export const Route = createFileRoute("/vps-control")({
  component: VpsControlPage,
});

function VpsControlPage() {
  const { t } = useTranslation();
  const q = useOwnedVps();
  const { hidden, toggle } = useHideIp();
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [activeAccount, setActiveAccount] = useActiveServerControlAccount();
  // 拿整个 query 而不是只解构 data:账户列表读失败时,下面「账户」那一格必须说清是没读到,
  // 不能跟「还没选账户」共用「未选择」这一句 —— 两者要用户做的事完全不同。
  const accountsQ = useAccounts();
  const accounts = accountsQ.data;
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
  // 别名读失败时 aliasOf 会退回 serviceName / displayName。
  // 这条属于诚实降级:别名只是本地附加的一层装饰,退回原名不会让用户误判 OVH 侧的任何事实,
  // 所以不单独报错 —— 真在这里加提示,反而会盖过同页更要命的那几处失败。
  const aliases = useServerAliases();
  const setAlias = useSetServerAlias();

  return (
    <div className="space-y-3 sm:space-y-4">
      <PageHeader
        title={t("vpsList.title")}
        description={t("vpsList.description")}
        icon={Cloud}
        action={
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="outline" size="sm" onClick={toggle}>
              {hidden ? <EyeOff className="w-4 h-4 mr-1" /> : <Eye className="w-4 h-4 mr-1" />}
              {hidden ? t("vpsList.showIp") : t("vpsList.hideIp")}
            </Button>
            <Button variant="outline" size="sm" onClick={() => q.refetch()} disabled={q.isFetching}>
              <RefreshCw className={"w-4 h-4 mr-1" + (q.isFetching ? " animate-spin" : "")} />
              {t("common.refresh")}
            </Button>
          </div>
        }
      />

      {/* 账户 + VPS 选择 */}
      <Card>
        <CardContent className="p-4 flex flex-wrap items-center gap-3">
          {/* 当前账户不在这里重复显示 —— 顶栏(手机)/侧栏(桌面)的切换器是全站唯一的账户显示位。
              但「读取失败」必须留在本页:整页的大区门控(isUS / region)全靠这份列表里的
              endpoint,读不到就默认按欧区渲染 —— 用户会对着一份**错误区域**的界面操作,
              而切换器那边只说得出"账户没读到",说不出这一页会因此渲染成什么样。 */}
          {accountsQ.isError && (
            <div
              className="flex items-center gap-2 h-9 px-3 rounded-md border border-destructive/40 bg-destructive/5 text-[12px]"
              title={errorMessage(accountsQ.error)}
            >
              <User className="w-3.5 h-3.5 text-destructive" />
              <span className="text-destructive">{t("vpsList.accountsFailed")}</span>
              <button type="button" className="underline text-muted-foreground" onClick={() => accountsQ.refetch()}>
                {t("common.retry")}
              </button>
            </div>
          )}
          <div className="flex items-center gap-2 flex-1 min-w-[280px]">
            <span className="text-[12px] text-muted-foreground">VPS</span>
            <Select value={selectedName || ""} onValueChange={setSelectedName}>
              <SelectTrigger className="h-9 w-full max-w-md">
                {/* 「无 VPS」是一句业务结论 —— 只有真问到了、结果确实是空,才配这么写 */}
                <SelectValue
                  placeholder={
                    q.isError
                      ? t("vpsList.select.placeholderFailed")
                      : q.isPending
                        ? t("vpsList.select.loading")
                        : vpsList.length === 0
                          ? t("vpsList.select.empty")
                          : t("vpsList.select.placeholder")
                  }
                />
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
            {/* 读失败时的「共 0 台」是个假数字,不能跟真的"这个账户没有机器"长成一个样 */}
            {q.isError && vpsList.length === 0 ? (
              <span className="text-destructive">{t("vpsList.count.notRead")}</span>
            ) : (
              <>
                {t("vpsList.count.total", { n: vpsList.length })}
                {q.isError && t("vpsList.count.stale")}
              </>
            )}
            {q.isFetching && t("vpsList.count.syncing")}
          </div>
        </CardContent>
      </Card>

      {/* 内容区。
          列表没问到 ≠ 该账户名下没有 VPS。以前两种情况共用「该账户下暂无 VPS」+「可以去 OVH 官网下单,
          或换个有 VPS 的账户」,配合上面的「无 VPS」占位和「共 0 台」,三处一起把一次请求失败讲成了
          一条账户事实 —— 用户会去换账户,甚至把已经买过的机器再买一台。
          react-query 报错后仍保留上次的结果,所以有旧数据时保留详情,只在顶上挂条幅说明这次没刷新上。 */}
      {q.isError && vpsList.length === 0 ? (
        <Card>
          <CardContent className="p-4">
            <LoadFailed icon={Cloud} title={t("vpsList.list.failed")} error={q.error} onRetry={() => q.refetch()} />
          </CardContent>
        </Card>
      ) : !q.isPending && vpsList.length === 0 ? (
        <Card>
          <CardContent className="py-12">
            <EmptyState
              icon={Cloud}
              title={t("vpsList.list.empty")}
              description={t("vpsList.list.emptyDesc")}
            />
          </CardContent>
        </Card>
      ) : selected ? (
        <>
          {q.isError && (
            <LoadFailedBanner
              title={t("vpsList.list.failedBanner")}
              error={q.error}
              onRetry={() => q.refetch()}
            />
          )}
          <VpsDetail
            server={selected}
            aliases={aliases}
            onSetAlias={setAlias}
            isUS={isUsEndpoint(activeEndpoint)}
            region={regionLabel(endpointRegion(activeEndpoint))}
          />
        </>
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
  const { t } = useTranslation();
  const q = useVpsOptions(serviceName);
  const list = q.data || [];

  return (
    <div className="border border-border rounded-2xl p-4 space-y-3">
      <h3 className="text-sm font-semibold">{t("vpsList.options.title")}</h3>
      {q.isPending ? (
        <Skeleton className="h-12 rounded-md" />
      ) : q.isError ? (
        <p className="text-[12px] text-destructive">{t("vpsList.options.failed")}</p>
      ) : list.length === 0 ? (
        <p className="text-[12px] text-muted-foreground">{t("vpsList.options.empty")}</p>
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
                      {t("vpsList.options.noManage", { region })}
                    </Chip>
                  )}
                </div>
                {!manageable && opt.unsupportedReason && (
                  <p className="text-[11px] text-muted-foreground">
                    {opt.unsupportedReason}
                    {/* 2026-10 起 OVH 的「取消选项」API 已整个下线(无替代),
                        所有区域都只能到控制台退订 —— 这块说明对全区适用了 */}
                    <span className="block mt-0.5">{t("vpsList.options.unsupportedNote")}</span>
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

/* (VpsServiceStatusPanel 已删:/vps/{sn}/status 被 OVH 废弃(2026-10-15 删除),
   全 schema 无替代端口探测端点 —— 没有可切换的新接口。后端路由保留,固定回 removed:true。) */

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
  const { t } = useTranslation();
  const info = useVpsServiceInfo(server.serviceName);
  const ips = useVpsIps(server.serviceName);
  const currentOS = useVpsCurrentOS(server.serviceName);
  const { hidden } = useHideIp();
  const start = useVpsStart(server.serviceName);
  const stop = useVpsStop(server.serviceName);
  const reboot = useVpsReboot(server.serviceName);
  const console_ = useVpsConsoleUrl(server.serviceName);
  const terminate = useTerminateVps();
  const confirmTerm = useConfirmTerminateVps();

  const [reinstallOpen, setReinstallOpen] = useState(false);
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
  // 文案走语言包(key 表 + 渲染处 t()),认不出的州名原样显示
  const sLabel = VPS_STATE_TONES[server.state]
    ? { text: t(`vpsList.state.${server.state}`), tone: VPS_STATE_TONES[server.state] }
    : { text: server.state, tone: "default" as const };

  const handleStart = async () => {
    try {
      await start.mutateAsync();
      toast.success(t("vpsList.toast.startSubmitted"));
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };
  const handleStop = async () => {
    try {
      await stop.mutateAsync();
      toast.success(t("vpsList.toast.stopSubmitted"));
      setStopOpen(false);
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };
  const handleReboot = async () => {
    if (!confirm(t("vpsList.power.confirmReboot"))) return;
    try {
      await reboot.mutateAsync();
      toast.success(t("vpsList.toast.rebootSubmitted"));
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };
  const handleConsole = async () => {
    try {
      const url = await console_.mutateAsync();
      if (url) {
        window.open(url, "_blank", "noopener,noreferrer");
        toast.success(t("vpsList.toast.consoleOk"));
      } else {
        toast.error(t("vpsList.toast.consoleFail"));
      }
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };
  const handleTerminate = async () => {
    try {
      const res = await terminate.mutateAsync({ serviceName: server.serviceName });
      toast.success(t("vpsList.toast.terminateSubmitted"));
      // 服务端响应里会带 token,但实际操作 OVH 用邮件 token 才能真正确认,这里保留邮件 token 输入
      console.log("[VPS] terminate response:", res);
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };
  const handleConfirmTerm = async () => {
    if (!termToken) return toast.error(t("vpsList.toast.needToken"));
    try {
      await confirmTerm.mutateAsync({ serviceName: server.serviceName, token: termToken });
      toast.success(t("vpsList.toast.terminated"));
      setTerminateOpen(false);
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };

  // 主 IP 摘要:失败和"确实没有 IP"必须给不同的字。以前两种情况都写「—」,
  // 用户会当成这台 VPS 没分到公网地址,转头去开工单。
  const ipDisplay = ips.isError
    ? t("vpsList.ip.readFailed")
    : ips.isPending
      ? "…"
      : ips.data && ips.data.length > 0
        ? maskSensitive(ips.data[0].ipAddress, hidden)
        : "—";

  return (
    <>
      <Tabs defaultValue="overview">
        {/* 顶部信息条 + 工具按钮 */}
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <TabsList className="grid grid-cols-4 sm:flex h-auto gap-1 p-1">
            <TabsTrigger value="overview" className="text-[12px] sm:text-sm px-2 sm:px-3">{t("vpsList.tabs.overview")}</TabsTrigger>
            <TabsTrigger value="snapshot" className="text-[12px] sm:text-sm px-2 sm:px-3">{t("vpsList.tabs.snapshot")}</TabsTrigger>
            <TabsTrigger value="ddos" className="text-[12px] sm:text-sm px-2 sm:px-3">{t("vpsList.tabs.ddos")}</TabsTrigger>
            <TabsTrigger value="maintenance" className="text-[12px] sm:text-sm px-2 sm:px-3">{t("vpsList.tabs.maintenance")}</TabsTrigger>
          </TabsList>

          <div className="flex flex-wrap gap-2 items-center">
            <Chip tone={sLabel.tone}>{sLabel.text}</Chip>
            {currentOS.data && (
              <button
                type="button"
                onClick={() => setReinstallOpen(true)}
                className="inline-flex items-center gap-1.5 h-7 pl-2.5 pr-3 rounded-full border border-border bg-background hover:bg-muted cursor-pointer transition-colors shadow-sm text-[12px]"
                title={t("vpsList.osPill.title")}
              >
                <Terminal className="w-3.5 h-3.5 text-muted-foreground" />
                <span className="text-muted-foreground">{t("vpsList.osPill.label")}</span>
                <span className="font-medium truncate max-w-[220px]">{currentOS.data.name}</span>
              </button>
            )}
            {/* 当前系统没读到时,原来是整颗胶囊消失 —— 页面上再也没有「系统」这一项,看起来像这台 VPS
                没装系统 / 面板不支持,点胶囊进重装的入口也一起没了。
                后端只在 OVH 明说"没有当前镜像记录"(404)时才返 null,其余限流 / 5xx 一律返错,
                所以走到 isError 一定是"没问到",不是"这台没系统"。 */}
            {currentOS.isError && (
              <button
                type="button"
                onClick={() => currentOS.refetch()}
                className="inline-flex items-center gap-1.5 h-7 pl-2.5 pr-3 rounded-full border border-destructive/40 bg-destructive/5 hover:bg-destructive/10 cursor-pointer transition-colors text-[12px]"
                title={errorMessage(currentOS.error)}
              >
                <AlertTriangle className="w-3.5 h-3.5 text-destructive" />
                <span className="text-destructive">{t("vpsList.osPill.failed")}</span>
                <span className="text-muted-foreground">{t("vpsList.retryHint")}</span>
              </button>
            )}
            {info.data?.expiration && (
              <span className="inline-flex items-center gap-1.5 h-7 pl-2.5 pr-3 rounded-full border border-border bg-secondary/50 text-[12px]">
                <CalendarClock className="w-3.5 h-3.5 text-muted-foreground" />
                <span className="text-muted-foreground">{t("vpsList.expirationLabel")}</span>
                <span className="font-medium">{fmtDate(info.data.expiration)}</span>
              </span>
            )}
            {info.data && (
              <button
                type="button"
                onClick={() => setRenewalOpen(true)}
                className="inline-flex items-center gap-1.5 h-7 pl-2.5 pr-3 rounded-full border border-border bg-background hover:bg-muted cursor-pointer transition-colors shadow-sm text-[12px]"
              >
                <Repeat className="w-3.5 h-3.5 text-muted-foreground" />
                <span className="text-muted-foreground">{t("vpsList.renewalLabel")}</span>
                <span className="font-medium">
                  {terminationLabel(info.data)
                    ? terminationLabel(info.data)!.text
                    : info.data.renewalForced
                      ? t("vpsList.renewal.forced")
                      : info.data.renewalType
                        ? t("vpsList.renewal.auto")
                        : t("vpsList.renewal.manual")}
                  {info.data.renewalPeriod > 0
                    ? t("vpsList.renewal.period", { n: info.data.renewalPeriod })
                    : ""}
                </span>
              </button>
            )}
            {/* serviceinfo 挂掉时「到期」「续费」两颗胶囊会一起消失。页面上一点到期信息都没有,
                跟"这台机器没有到期日、不用管续费"长得一模一样,用户就此错过续费窗口。
                必须占位讲明是没问到,并把重试放在同一个位置。 */}
            {info.isError && (
              <button
                type="button"
                onClick={() => info.refetch()}
                className="inline-flex items-center gap-1.5 h-7 pl-2.5 pr-3 rounded-full border border-destructive/40 bg-destructive/5 hover:bg-destructive/10 cursor-pointer transition-colors text-[12px]"
                title={errorMessage(info.error)}
              >
                <AlertTriangle className="w-3.5 h-3.5 text-destructive" />
                <span className="text-destructive">{t("vpsList.renewal.failed")}</span>
                <span className="text-muted-foreground">{t("vpsList.retryHint")}</span>
              </button>
            )}
          </div>
        </div>

        {/* 列表接口这次没拿到这台 VPS 的详情/计费信息。
            「没查到」不能显示成「没开自动续费」或「unknown 状态」，那会让用户按错误前提去操作。
            这条 warning 原来只看列表返回的 server.error，不看 info.isError —— 于是兜底那句
            「请以下方「续费」胶囊为准」在 serviceinfo 自己也挂掉时成了空话:那颗胶囊根本不会渲染。
            所以 info.isError 也要进条件，并且优先说它。 */}
        {(server.error || server.renewalType === null || server.status === null || info.isError) && (
          <div className="flex items-start gap-2 rounded-xl border border-warning/40 bg-warning/5 px-3 py-2 text-[11px] text-foreground/80 mt-3">
            <AlertTriangle className="w-3.5 h-3.5 text-warning flex-shrink-0 mt-0.5" />
            <span>
              {info.isError
                ? t("vpsList.warn.infoFailed", { err: errorMessage(info.error) })
                : server.error
                  ? t("vpsList.warn.detailFailed", { err: server.error })
                  : t("vpsList.warn.renewalUnknown")}
            </span>
          </div>
        )}

        {/* 顶部硬件信息卡(VPS 简化版) */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-2.5 sm:gap-3 mt-4">
          <InfoCard icon={<Cpu className="w-4 h-4" />} label="vCore" value={String(server.vcore || "—")} />
          <InfoCard
            icon={<MemoryStick className="w-4 h-4" />}
            label={t("vpsList.hw.memory")}
            value={server.memoryMB ? `${(server.memoryMB / 1024).toFixed(0)} GB` : "—"}
          />
          <InfoCard
            icon={<HardDrive className="w-4 h-4" />}
            label={t("vpsList.hw.disk")}
            value={server.diskGB ? `${server.diskGB} GB` : "—"}
          />
          <InfoCard
            icon={<MapPin className="w-4 h-4" />}
            label={t("vpsList.hw.region")}
            value={humanZoneShort(server.zone, t)}
          />
        </div>

        {/* 电源 / 控制台 / 重装 / 改密 按钮行 */}
        <div className="mt-4 border border-border rounded-2xl p-3 flex flex-wrap gap-2">
          {isStopped && (
            <Button onClick={handleStart} disabled={start.isPending}>
              <Power className="w-4 h-4 mr-1" />
              {t("vpsList.power.start")}
            </Button>
          )}
          {isRunning && (
            <Button variant="outline" onClick={() => setStopOpen(true)} disabled={stop.isPending}>
              <PowerOff className="w-4 h-4 mr-1" />
              {t("vpsList.power.stop")}
            </Button>
          )}
          <Button variant="outline" onClick={handleReboot} disabled={reboot.isPending || !isRunning}>
            <RefreshCw className="w-4 h-4 mr-1" />
            {t("vpsList.power.reboot")}
          </Button>
          <Button variant="outline" onClick={handleConsole} disabled={console_.isPending}>
            <Monitor className="w-4 h-4 mr-1" />
            {t("vpsList.power.console")}
          </Button>
          <Button variant="outline" onClick={() => setReinstallOpen(true)}>
            <HardDrive className="w-4 h-4 mr-1" />
            {t("vpsList.power.reinstall")}
          </Button>
          {/* 重置密码按钮已删:底层 /vps/{sn}/setPassword 被 OVH 废弃(2026-10-15 删除,
              无替代)。改密码走 Web 控制台(noVNC)里的 passwd。 */}
          <Button variant="outline" onClick={() => setEngagementOpen(true)}>
            <CalendarPlus className="w-4 h-4 mr-1" />
            {t("vpsList.power.engagement")}
          </Button>
          <Button variant="outline" onClick={() => setTasksOpen(true)}>
            <ListTodo className="w-4 h-4 mr-1" />
            {t("vpsList.power.tasks")}
          </Button>
        </div>

        {/* 概览 Tab */}
        <TabsContent value="overview" className="mt-4 space-y-4">
          {/* 异常状态警示:锁定 / 救援模式 */}
          {server.lockStatus && server.lockStatus !== "unlocked" && (
            <div className="border border-destructive/40 bg-destructive/5 rounded-xl p-3 flex items-start gap-2.5">
              <AlertTriangle className="w-4 h-4 text-destructive mt-0.5 flex-shrink-0" />
              <div className="text-[12px]">
                <p className="font-semibold text-destructive">{t("vpsList.lock.title")}</p>
                <p className="text-muted-foreground mt-0.5">
                  <Trans
                    i18nKey="vpsList.lock.desc"
                    values={{ status: server.lockStatus }}
                    components={{ code: <code className="font-mono" /> }}
                    t={t}
                  />
                </p>
              </div>
            </div>
          )}
          {server.netbootMode === "rescue" && (
            <div className="border border-warning/40 bg-warning/5 rounded-xl p-3 flex items-start gap-2.5">
              <AlertTriangle className="w-4 h-4 text-warning mt-0.5 flex-shrink-0" />
              <div className="text-[12px]">
                <p className="font-semibold text-warning">{t("vpsList.rescue.title")}</p>
                <p className="text-muted-foreground mt-0.5">
                  <Trans
                    i18nKey="vpsList.rescue.desc"
                    components={{ code: <code /> }}
                    t={t}
                  />
                </p>
              </div>
            </div>
          )}

          {/* IP 列表 */}
          <div className="border border-border rounded-2xl overflow-hidden">
            <div className="px-4 py-3 border-b border-border flex items-center gap-2">
              <Globe className="w-4 h-4 text-muted-foreground" />
              <h3 className="text-sm font-semibold">{t("vpsList.ip.title")}</h3>
              <span className="text-[11px] text-muted-foreground ml-auto">
                {t("vpsList.ip.main", { ip: ipDisplay })}
              </span>
            </div>
            {ips.isPending ? (
              <div className="p-4">
                <Skeleton className="h-16 rounded-md" />
              </div>
            ) : ips.isError ? (
              // 「无 IP」是在断言这台 VPS 没有公网地址 —— 那是要开工单找 OVH 的大事。
              // 接口没问到的时候写这三个字,等于凭空造一条业务事实。
              <div className="p-4">
                <LoadFailed
                  icon={Globe}
                  title={t("vpsList.ip.failed")}
                  error={ips.error}
                  onRetry={() => ips.refetch()}
                  compact
                />
              </div>
            ) : (ips.data || []).length === 0 ? (
              <p className="px-4 py-6 text-sm text-muted-foreground text-center">{t("vpsList.ip.empty")}</p>
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

          {/* 一行底部:型号 + 开通日期 + 集群,放轻量 */}
          <p className="text-[11px] text-muted-foreground px-1">
            {server.model && <>{t("vpsList.meta.model")} <code className="font-mono">{server.model}</code></>}
            {info.data?.creation && (
              <> · {t("vpsList.meta.created", { date: fmtDate(info.data.creation) })}</>
            )}
            {server.cluster && <> · {t("vpsList.meta.cluster")} <code className="font-mono">{server.cluster}</code></>}
            {server.slaMonitoring && <> · {t("vpsList.meta.sla")}</>}
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
            <h3 className="text-sm font-semibold">{t("vpsList.alias.title")}</h3>
            <p className="text-[12px] text-muted-foreground">
              {t("vpsList.alias.hint", { sn: server.serviceName })}
            </p>
            <AliasEditor serviceName={server.serviceName} aliases={aliases} onSetAlias={onSetAlias} />
          </div>

          <VpsOptionsPanel serviceName={server.serviceName} region={region} />

          {!isUS ? (
            <div className="border border-border rounded-2xl p-4 space-y-3">
              <h3 className="text-sm font-semibold">{t("vpsList.contact.title")}</h3>
              <p className="text-[12px] text-muted-foreground">{t("vpsList.contact.desc")}</p>
              <Button variant="outline" size="sm" onClick={() => setContactOpen(true)}>
                <Repeat className="w-3.5 h-3.5 mr-1" />
                {t("vpsList.contact.title")}
              </Button>
            </div>
          ) : (
            <div className="border border-border rounded-2xl p-4 space-y-2 bg-secondary/30">
              <h3 className="text-sm font-semibold text-muted-foreground">{t("vpsList.usLimit.title")}</h3>
              <p className="text-[12px] text-muted-foreground">
                <Trans
                  i18nKey="vpsList.usLimit.desc"
                  components={{ code: <code /> }}
                  t={t}
                />
              </p>
            </div>
          )}

          <div className="border border-destructive/40 bg-destructive/5 rounded-2xl p-4 space-y-2">
            <div className="flex items-center gap-2">
              <AlertTriangle className="w-4 h-4 text-destructive" />
              <h3 className="text-sm font-semibold text-destructive">{t("vpsList.terminate.title")}</h3>
            </div>
            <p className="text-[12px] text-muted-foreground">{t("vpsList.terminate.desc")}</p>
            <div className="flex gap-2 flex-wrap">
              <Button variant="destructive" size="sm" onClick={handleTerminate} disabled={terminate.isPending}>
                <CalendarPlus className="w-3.5 h-3.5 mr-1" />
                {t("vpsList.terminate.submit")}
              </Button>
              <Button variant="outline" size="sm" onClick={() => setTerminateOpen(true)}>
                {t("vpsList.terminate.confirmBtn")}
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
            <DialogTitle>{t("vpsList.stopDialog.title")}</DialogTitle>
            <DialogDescription>{t("vpsList.stopDialog.desc")}</DialogDescription>
          </DialogHeader>
          <p className="text-[12px] text-muted-foreground">
            {t("vpsList.stopDialog.note")}
          </p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setStopOpen(false)}>{t("common.cancel")}</Button>
            <Button variant="destructive" onClick={handleStop} disabled={stop.isPending}>
              {stop.isPending ? t("vpsList.submitting") : t("vpsList.stopDialog.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 确认终止(已收到邮件 token) */}
      <Dialog open={terminateOpen} onOpenChange={setTerminateOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("vpsList.terminate.dialogTitle")}</DialogTitle>
            <DialogDescription>{t("vpsList.terminate.dialogDesc")}</DialogDescription>
          </DialogHeader>
          <Input
            value={termToken}
            onChange={(e) => setTermToken(e.target.value)}
            placeholder={t("vpsList.terminate.placeholder")}
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setTerminateOpen(false)}>{t("common.cancel")}</Button>
            <Button variant="destructive" onClick={handleConfirmTerm} disabled={!termToken || confirmTerm.isPending}>
              {confirmTerm.isPending ? t("vpsList.terminate.confirming") : t("vpsList.terminate.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

/** vps.VpsStateEnum → 胶囊配色(文案 key 走 vpsList.state.*)。
 *  rescued 用 danger 警示用户还没退出救援 */
const VPS_STATE_TONES: Record<string, "success" | "warning" | "danger"> = {
  running: "success",
  stopped: "warning",
  stopping: "warning",
  rebooting: "warning",
  installing: "warning",
  backuping: "warning",
  upgrading: "warning",
  maintenance: "warning",
  rescued: "danger",
};

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

/** humanZoneShort OVH zone 字段转可读名,InfoCard 用短版只显示本地化标签。
 *  2025 cloud VPS 给 "Region OpenStack: os-us-west-or-2",老款给 "bhs"/"gra"/"sbg" 之类机房代号。
 *  代号 → i18n key,渲染处 t();认不出时原样大写。 */
function humanZoneShort(zone: string, t: TFn): string {
  const key = zoneKey(zone);
  if (!key) return (zone || "—").toUpperCase();
  return t(key);
}

function zoneKey(zone: string): string | null {
  if (!zone) return null;
  const m = zone.match(/os-[a-z0-9-]+/i);
  const code = (m ? m[0] : zone.trim().toLowerCase().split(/\s+/).pop() || zone).toLowerCase();
  const key = OS_ZONE_KEYS[code] || LEGACY_DC_KEYS[code.slice(0, 3)];
  return key || null;
}

// OpenStack 区域代号(2025+ cloud VPS / OVH Public Cloud 同款命名)→ 语言包 key
const OS_ZONE_KEYS: Record<string, string> = {
  "os-us-west-or-1": "vpsList.zone.osUsWestOr",
  "os-us-west-or-2": "vpsList.zone.osUsWestOr",
  "os-us-east-va-1": "vpsList.zone.osUsEastVa",
  "os-eu-west-fr-1": "vpsList.zone.osEuWestFr",
  "os-eu-west-de-1": "vpsList.zone.osEuWestDe",
  "os-eu-west-pl-1": "vpsList.zone.osEuWestPl",
  "os-eu-west-uk-1": "vpsList.zone.osEuWestUk",
  "os-eu-south-it-1": "vpsList.zone.osEuSouthIt",
  "os-eu-north-fi-1": "vpsList.zone.osEuNorthFi",
  "os-ca-east-bhs-1": "vpsList.zone.osCaEastBhs",
  "os-asia-southeast-sg-1": "vpsList.zone.osAsiaSg",
  "os-asia-south-in-1": "vpsList.zone.osAsiaIn",
  "os-au-southeast-syd-1": "vpsList.zone.osAuSyd",
};

// 老款 VPS / Dedicated 机房三字母代号 → 语言包 key
const LEGACY_DC_KEYS: Record<string, string> = {
  bhs: "vpsList.zone.dc.bhs",
  gra: "vpsList.zone.dc.gra",
  rbx: "vpsList.zone.dc.rbx",
  sbg: "vpsList.zone.dc.sbg",
  waw: "vpsList.zone.dc.waw",
  fra: "vpsList.zone.dc.fra",
  lon: "vpsList.zone.dc.lon",
  lim: "vpsList.zone.dc.lim",
  eri: "vpsList.zone.dc.eri",
  vin: "vpsList.zone.dc.vin",
  hil: "vpsList.zone.dc.hil",
  sgp: "vpsList.zone.dc.sgp",
  syd: "vpsList.zone.dc.syd",
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
  const { t } = useTranslation();
  const [admin, setAdmin] = useState("");
  const [tech, setTech] = useState("");
  const [billing, setBilling] = useState("");

  const handleSubmit = async () => {
    if (!admin && !tech && !billing) {
      toast.error(t("vpsList.toast.needContact"));
      return;
    }
    try {
      await mutation.mutateAsync({
        serviceName,
        admin: admin || undefined,
        tech: tech || undefined,
        billing: billing || undefined,
      });
      toast.success(t("vpsList.toast.contactSubmitted"));
      onClose();
      setAdmin(""); setTech(""); setBilling("");
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <DialogContent className="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>{t("vpsList.contact.title")}</DialogTitle>
        <DialogDescription>{t("vpsList.contact.dialogDesc")}</DialogDescription>
      </DialogHeader>
      <div className="space-y-3 py-1">
        <div>
          <label className="text-[12px] font-semibold block mb-1.5">{t("vpsList.contact.admin")}</label>
          <Input value={admin} onChange={(e) => setAdmin(e.target.value)} placeholder={t("vpsList.contact.adminPlaceholder")} />
        </div>
        <div>
          <label className="text-[12px] font-semibold block mb-1.5">{t("vpsList.contact.tech")}</label>
          <Input value={tech} onChange={(e) => setTech(e.target.value)} placeholder={t("vpsList.contact.placeholder")} />
        </div>
        <div>
          <label className="text-[12px] font-semibold block mb-1.5">{t("vpsList.contact.billing")}</label>
          <Input value={billing} onChange={(e) => setBilling(e.target.value)} placeholder={t("vpsList.contact.placeholder")} />
        </div>
        <p className="text-[11px] text-muted-foreground">{t("vpsList.contact.hint")}</p>
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>{t("common.cancel")}</Button>
        <Button onClick={handleSubmit} disabled={mutation.isPending}>
          {mutation.isPending ? t("vpsList.submitting") : t("vpsList.contact.submit")}
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
  const { t } = useTranslation();
  const [v, setV] = useState(aliasOf(aliases.data, serviceName, ""));
  return (
    <div className="flex gap-2">
      <Input value={v} onChange={(e) => setV(e.target.value)} placeholder={t("vpsList.alias.placeholder")} />
      <Button
        size="sm"
        onClick={async () => {
          try {
            await onSetAlias.mutateAsync({ serviceName, alias: v });
            toast.success(t("vpsList.alias.saved"));
          } catch (e: any) {
            toast.error(errorMessage(e));
          }
        }}
        disabled={onSetAlias.isPending}
      >
        {t("common.save")}
      </Button>
    </div>
  );
}
