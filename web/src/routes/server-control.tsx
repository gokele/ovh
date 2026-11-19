import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Terminal, Server, RefreshCw, Eye, EyeOff, CalendarClock, Repeat, Activity, Undo2, PartyPopper, Pencil, ChevronsUpDown, ChevronRight, Copy, Receipt } from "lucide-react";
import { useEffect, useState } from "react";
import { PageHeader } from "@/components/common/PageHeader";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { StatusDot } from "@/components/common/StatusDot";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed, errorMessage } from "@/components/common/LoadFailed";
import { fmtDate, fmtDateTime, fmtMoney } from "@/i18n/format";
import {
  useOwnedServers,
  useServerServiceInfo,
  useServerPricing,
  useServerMonitoring,
  useToggleMonitoring,
  useRetraction,
  type OwnedServer,
  terminationLabel,
} from "@/hooks/use-server-control";
import { useHideIp, maskSensitive } from "@/hooks/use-hide-ip";
import { useActiveServerControlAccount } from "@/hooks/use-active-account";
import { useAccounts } from "@/hooks/use-accounts";
import { useServerAliases, useSetServerAlias, aliasOf } from "@/hooks/use-server-aliases";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { OverviewTab } from "@/components/server-control/OverviewTab";
import { PowerTab } from "@/components/server-control/PowerTab";
import { MaintenanceTab } from "@/components/server-control/MaintenanceTab";
import { AdvancedTab } from "@/components/server-control/AdvancedTab";
import { RenewalDialog } from "@/components/server-control/RenewalDialog";
import { RetractionDialog } from "@/components/server-control/RetractionDialog";
import { ReinstallDialog } from "@/components/server-control/ReinstallDialog";
import { toast } from "sonner";
import { ResourceSidebar } from "@/components/common/ResourceSidebar";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";
import { OsIcon } from "@/components/server-control/OsIcon";

/** 模块级文案助手共用的翻译函数类型 */
type TFn = ReturnType<typeof useTranslation>["t"];

/** 服务器控制中心：顶部下拉切换服务器 + 4 tab 详情 */
export const Route = createFileRoute("/server-control")({
  component: ServerControlPage,
});

function ServerControlPage() {
  const { t } = useTranslation();
  const q = useOwnedServers();
  const { hidden, toggle } = useHideIp();
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const [activeAccount, setActiveAccount] = useActiveServerControlAccount();
  const accountsQ = useAccounts();
  const accounts = accountsQ.data;
  const servers = q.data || [];

  // 首次没选过账户 → 自动选默认账户
  useEffect(() => {
    if (!activeAccount && accounts && accounts.length > 0) {
      const def = accounts.find((a) => a.isDefault) || accounts[0];
      setActiveAccount(def.id);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accounts]);

  // 切换账户时,选中的 service 也清空(不同账户的服务器不一样)
  useEffect(() => {
    setSelectedName(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeAccount]);

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
  const aliasesQ = useServerAliases();
  const [renaming, setRenaming] = useState<OwnedServer | null>(null);

  return (
    <div className="space-y-3 sm:space-y-6">
      <PageHeader
        icon={Terminal}
        title={t("serverList.title")}
        // 当前账户不在这里重复 —— 顶栏(手机)/侧栏(桌面)的切换器是全站唯一的账户显示位,
        // 它同时也负责把"账户列表读取失败"这件事说出来,所以这里不必再挂一份。
        description={t("serverList.description")}
        action={
          <div className="flex flex-wrap items-center gap-2">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="icon"
                  onClick={toggle}
                  aria-label={hidden ? t("serverList.showSensitive") : t("serverList.hideIp")}
                >
                  {hidden ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {hidden ? t("serverList.hiddenTip") : t("serverList.hideIp")}
              </TooltipContent>
            </Tooltip>
            <Button variant="outline" onClick={() => q.refetch()} disabled={q.isFetching}>
              <RefreshCw className={`w-4 h-4 ${q.isFetching ? "animate-spin" : ""}`} />
              {t("common.refresh")}
            </Button>
          </div>
        }
      />

      {q.isPending ? (
        <Skeleton className="h-[500px] rounded-2xl" />
      ) : q.isError ? (
        // 列表没拉到 ≠ 这个账户下没有机器。以前一律走「暂无服务器」+「您的 OVH 账户下还没有
        // 独立服务器」,用户看完就去别的账户找 / 以为机器被销毁了,而实际只是这次请求挂了。
        <Card>
          <div className="p-4 sm:p-6">
            <LoadFailed
              icon={Server}
              title={t("serverList.list.failed")}
              error={q.error}
              onRetry={() => q.refetch()}
            />
          </div>
        </Card>
      ) : servers.length === 0 ? (
        <Card>
          <EmptyState
            illustration="control"
            title={t("serverList.list.empty")}
            description={t("serverList.list.emptyDesc")}
          />
        </Card>
      ) : (
        <div>
          {selected && (
            <ServerTabs
              key={`${activeAccount}:${selected.serviceName}`}
              server={selected}
              title={maskSensitive(aliasOf(aliasesQ.data, selected.serviceName, selected.name), hidden)}
              hidden={hidden}
              onRename={() => setRenaming(selected)}
              switcher={
                servers.length > 1 ? (
                  <ResourceSidebar
                    label={t("serverList.sidebar.title")}
                    searchPlaceholder={t("serverList.sidebar.search")}
                    emptyText={t("serverList.sidebar.noMatch")}
                    selected={selectedName}
                    onSelect={setSelectedName}
                    onContextMenu={(id) => setRenaming(servers.find((s) => s.serviceName === id) || null)}
                    items={servers.map((s) => ({
                      id: s.serviceName,
                      title: maskSensitive(aliasOf(aliasesQ.data, s.serviceName, s.name), hidden),
                      sub: [s.commercialRange, (s.datacenter || "").toUpperCase()].filter(Boolean).join(" · "),
                      meta: maskSensitive(s.ip, hidden),
                      tone: s.state === "ok" ? "success" : "warning",
                      badge: s.lottery?.won ? (
                        <PartyPopper className="w-3 h-3 text-amber-500 flex-shrink-0" aria-label={t("serverList.lotteryBadge")} />
                      ) : undefined,
                    }))}
                  />
                ) : undefined
              }
              count={servers.length}
            />
          )}
          <RenameDialog
            server={renaming}
            currentAlias={renaming ? aliasesQ.data?.[renaming.serviceName] || "" : ""}
            aliasesUnavailable={aliasesQ.isError}
            onClose={() => setRenaming(null)}
          />
        </div>
      )}
    </div>
  );
}

/** 服务器别名编辑对话框。alias 留空 + 保存 = 删除别名,恢复显示原 service_name。 */
function RenameDialog({
  server,
  currentAlias,
  aliasesUnavailable,
  onClose,
}: {
  server: OwnedServer | null;
  currentAlias: string;
  /** 别名表这次没拉到 —— 输入框里的"空"不代表"原本就没设过别名" */
  aliasesUnavailable?: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const set = useSetServerAlias();
  const [value, setValue] = useState(currentAlias);
  useEffect(() => {
    setValue(currentAlias);
  }, [currentAlias, server?.serviceName]);

  if (!server) return null;
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    await set.mutateAsync({ serviceName: server.serviceName, alias: value });
    onClose();
  };

  return (
    <Dialog open={!!server} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("serverList.rename.title")}</DialogTitle>
          <DialogDescription className="font-mono text-[11px]">
            {server.serviceName}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-3">
          <Input
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder={t("serverList.rename.placeholder")}
            autoFocus
            maxLength={64}
          />
          <p className="text-[11px] text-muted-foreground">
            {t("serverList.rename.hint")}
          </p>
          {aliasesUnavailable && (
            <p className="text-[11px] text-warning border border-warning/40 bg-warning/5 rounded-xl px-3 py-2">
              {t("serverList.rename.unavailableWarn")}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={set.isPending}>
              {set.isPending
                ? t("serverList.rename.saving")
                : value.trim() === ""
                  ? t("serverList.rename.clearSave")
                  : t("common.save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ServerTabs({
  server,
  title,
  hidden,
  onRename,
  switcher,
  count,
}: {
  server: OwnedServer;
  /** 别名或主机名(已按「隐藏敏感信息」处理) */
  title: string;
  hidden: boolean;
  onRename: () => void;
  /** 多台服务器时的切换列表(点标题弹出);只有一台时为空 */
  switcher?: React.ReactNode;
  count: number;
}) {
  const { t } = useTranslation();
  const info = useServerServiceInfo(server.serviceName);
  const pricing = useServerPricing(server.serviceName);
  // 14 天无理由撤单的资格。只有 OVH 明确说还在窗口内才会渲染入口 ——
  // 判据是它返回的 retractionDate,不是自己算"开通不到 14 天":
  // v0.1.24 之前下的单在结账时就弃权了,自己算会给它们显示一个必然失败的按钮。
  const retraction = useRetraction(server.serviceName);
  const [retractOpen, setRetractOpen] = useState(false);
  const monitoring = useServerMonitoring(server.serviceName);
  const toggleMon = useToggleMonitoring();
  const [renewalOpen, setRenewalOpen] = useState(false);
  const [reinstallOpen, setReinstallOpen] = useState(false);

  /**
   * 监控开关下发的是「取反」,取的是 monitoring.data。
   * 读失败时 monitoring.data 是 undefined(等价 false),按钮会写「监控 已关」,
   * 而 OVH 那边监控其实开着 —— 用户以为在开启,实际一点就把它关了。
   * 所以状态未知时不发指令,先把状态读回来。
   */
  const monitoringUnknown = monitoring.isError;
  const handleToggleMonitoring = async () => {
    if (monitoringUnknown) {
      monitoring.refetch();
      return;
    }
    try {
      await toggleMon.mutateAsync({ serviceName: server.serviceName, enabled: !monitoring.data });
      toast.success(monitoring.data ? t("serverList.monitor.disabledToast") : t("serverList.monitor.enabledToast"));
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };

  const [switchOpen, setSwitchOpen] = useState(false);
  const exp = info.data?.expiration ? new Date(info.data.expiration).getTime() : NaN;
  const daysLeft = Number.isFinite(exp) ? Math.ceil((exp - Date.now()) / 86400000) : null;
  const expTone: FactTone | undefined = daysLeft === null ? undefined : daysLeft < 0 ? "danger" : daysLeft <= 7 ? "warning" : undefined;
  const ok = server.state === "ok";
  const titleNode = (
    <span className="flex items-center gap-2 min-w-0">
      <span className="text-[18px] sm:text-[20px] font-semibold tracking-tight truncate">{title}</span>
      {switcher && <ChevronsUpDown className="w-4 h-4 shrink-0 text-muted-foreground" aria-hidden />}
    </span>
  );

  return (
    <div className="space-y-4">
      {/* 标题卡:系统图标 + 名称(多台时点开切换)+ 型号·机房·IP + 状态;下半部分是四格关键信息 */}
      <Card className="overflow-hidden">
        <div className="relative px-4 sm:px-6 pt-5 pb-4">
          <div className="absolute inset-0 bg-gradient-to-r from-primary/[0.06] via-transparent to-transparent pointer-events-none" aria-hidden />
          <div className="relative flex flex-wrap sm:flex-nowrap items-start gap-x-4 gap-y-3">
            <span className="hidden sm:flex w-12 h-12 rounded-xl bg-card ring-1 ring-border shadow-sm items-center justify-center shrink-0">
              <OsIcon templateName={server.os || ""} size={26} />
            </span>
            <div className="min-w-0 flex-1 basis-full sm:basis-auto">
              <div className="flex items-center gap-1.5 min-w-0">
                {switcher ? (
                  <Popover open={switchOpen} onOpenChange={setSwitchOpen}>
                    <PopoverTrigger asChild>
                      <button
                        type="button"
                        className="min-w-0 -ml-1.5 px-1.5 py-0.5 rounded-lg hover:bg-muted/70 transition-colors text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        aria-label={t("serverList.hero.switch", { n: count })}
                        title={t("serverList.hero.switch", { n: count })}
                      >
                        {titleNode}
                      </button>
                    </PopoverTrigger>
                    <PopoverContent align="start" className="w-[min(340px,calc(100vw-32px))] p-0 overflow-hidden flex flex-col" onClick={() => setSwitchOpen(false)}>
                      {switcher}
                    </PopoverContent>
                  </Popover>
                ) : (
                  <h2 className="min-w-0">{titleNode}</h2>
                )}
                <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0" onClick={onRename} aria-label={t("serverList.sidebar.rename")} title={t("serverList.sidebar.rename")}>
                  <Pencil className="w-3.5 h-3.5" />
                </Button>
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[12.5px] text-muted-foreground">
                {title !== maskSensitive(server.serviceName, hidden) && <span className="font-mono">{maskSensitive(server.serviceName, hidden)}</span>}
                <span className="font-medium text-foreground/80">{server.commercialRange}</span>
                <span aria-hidden>·</span>
                <span className="font-mono">{(server.datacenter || "").toUpperCase()}</span>
                {server.ip && (
                  <>
                    <span aria-hidden>·</span>
                    <button
                      type="button"
                      className="inline-flex items-center gap-1 font-mono hover:text-primary transition-colors"
                      onClick={() => {
                        void navigator.clipboard?.writeText(server.ip).then(() => toast.success(t("serverList.hero.copied")));
                      }}
                      title={t("serverList.hero.copyIp")}
                    >
                      {maskSensitive(server.ip, hidden)}
                      <Copy className="w-3 h-3" />
                    </button>
                  </>
                )}
              </div>
            </div>
            <div className="shrink-0 flex flex-wrap items-center gap-1.5 sm:flex-col sm:items-end lg:flex-row lg:items-center">
              {/* 中奖标记:列表摘要里的 won/tier(详情横幅在「概览」里)。以前挂在左侧列表行上,列表去掉后放到这里 */}
              {server.lottery?.won && (
                <span
                  className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-full text-[12.5px] font-semibold text-amber-700 dark:text-amber-300 bg-gradient-to-r from-amber-100 to-yellow-50 dark:from-amber-500/20 dark:to-amber-400/10 ring-1 ring-amber-300/70 dark:ring-amber-400/30"
                  title={t("serverList.lotteryBadge")}
                >
                  <PartyPopper className="w-3.5 h-3.5" />
                  {t(`maint.overview.lottery.title${Math.min(Math.max(server.lottery.tier || 1, 1), 3)}`)}
                </span>
              )}
              <span
                className={`inline-flex items-center gap-1.5 h-7 px-2.5 rounded-full text-[12.5px] font-medium ${
                  ok ? "bg-success/10 text-success" : "bg-warning/[.12] text-warning"
                }`}
              >
                <StatusDot tone={ok ? "success" : "warning"} pulse={ok} size="xs" />
                {ok ? t("serverList.hero.stateOk") : server.state}
              </span>
            </div>
          </div>
        </div>

        {/* 关键信息:能点的(续费、系统)带箭头;到期快到时变色;监控用开关 */}
        <div className="grid grid-cols-2 lg:grid-cols-5 border-t border-border divide-x divide-y lg:divide-y-0 divide-border [&>*:nth-child(2n+1)]:border-l-0 lg:[&>*:nth-child(2n+1):not(:first-child)]:border-l">
          {info.isPending ? (
            Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="px-4 sm:px-6 py-3.5">
                <Skeleton className="h-3 w-12 mb-2" />
                <Skeleton className="h-4 w-24" />
              </div>
            ))
          ) : (
            <>
              <FactCell
                icon={<Repeat className="w-3.5 h-3.5" />}
                label={t("serverList.pill.renewal")}
                value={info.data ? formatRenewal(t, info.data) : "—"}
                onClick={info.data ? () => setRenewalOpen(true) : undefined}
              />
              <PriceCell pricing={pricing} />
              <FactCell
                icon={<Terminal className="w-3.5 h-3.5" />}
                label={t("serverList.hero.os")}
                value={server.os || "—"}
                mono
                onClick={() => setReinstallOpen(true)}
              />
              <FactCell
                icon={<CalendarClock className="w-3.5 h-3.5" />}
                label={t("serverList.pill.expiration")}
                value={info.data?.expiration ? fmtDate(info.data.expiration) : "—"}
                sub={daysLeft === null ? undefined : daysLeft < 0 ? t("serverList.hero.expired") : t("serverList.hero.daysLeft", { n: daysLeft })}
                tone={expTone}
              />
              <div className="col-span-2 lg:col-span-1 px-4 sm:px-6 py-3.5 min-w-0">
                <div className="flex items-center gap-1.5 text-[12px] text-muted-foreground">
                  <Activity className="w-3.5 h-3.5" />
                  {t("serverList.hero.monitor")}
                </div>
                <div className="mt-1 flex items-center gap-2 min-h-[24px]">
                  {monitoring.isPending ? (
                    <span className="text-[13.5px] text-muted-foreground">{t("serverList.hero.loading")}</span>
                  ) : monitoringUnknown ? (
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button type="button" onClick={handleToggleMonitoring} className="text-[13.5px] font-semibold text-destructive hover:underline">
                          {t("serverList.hero.monitorUnknown")}
                        </button>
                      </TooltipTrigger>
                      <TooltipContent>{t("serverList.monitor.unknownTip", { err: errorMessage(monitoring.error) })}</TooltipContent>
                    </Tooltip>
                  ) : (
                    <>
                      <Switch
                        checked={!!monitoring.data}
                        disabled={toggleMon.isPending}
                        onCheckedChange={() => void handleToggleMonitoring()}
                        className="data-[state=checked]:bg-success data-[state=unchecked]:bg-input"
                        aria-label={t("serverList.monitor.tip")}
                      />
                      <span className={`text-[13.5px] font-semibold ${monitoring.data ? "text-success" : "text-muted-foreground"}`} title={t("serverList.monitor.tip")}>
                        {monitoring.data ? t("serverList.hero.on") : t("serverList.hero.off")}
                      </span>
                    </>
                  )}
                </div>
              </div>
            </>
          )}
        </div>

        {/* 可撤单:有硬截止的不可逆权利,单独一条醒目提示,不和普通信息混在一起 */}
        {retraction.data?.eligible && (
          <button
            type="button"
            onClick={() => setRetractOpen(true)}
            title={retractionWindowText(t, retraction.data)}
            className="w-full flex items-center gap-2 px-4 sm:px-6 py-2.5 border-t border-warning/30 bg-warning/[.08] hover:bg-warning/[.14] text-left text-[13px] transition-colors"
          >
            <Undo2 className="w-4 h-4 text-warning shrink-0" />
            <span className="font-medium text-warning">{t("serverList.pill.retractable")}</span>
            <span className="text-foreground/80">
              {typeof retraction.data.hoursLeft === "number"
                ? retraction.data.hoursLeft >= 24
                  ? t("serverList.pill.daysLeft", { n: Math.floor(retraction.data.hoursLeft / 24) })
                  : t("serverList.pill.hoursLeft", { n: retraction.data.hoursLeft })
                : t("serverList.pill.inWindow")}
            </span>
            <ChevronRight className="w-4 h-4 ml-auto text-muted-foreground" />
          </button>
        )}
      </Card>

      <Card className="overflow-hidden">
      <Tabs defaultValue="overview">
        <div className="px-4 sm:px-6 pt-4">
          <TabsList className="grid grid-cols-4 sm:inline-flex h-auto gap-1 p-1">
            <TabsTrigger value="overview" className="text-[12px] sm:text-sm px-2 sm:px-4">{t("serverList.tabs.overview")}</TabsTrigger>
            <TabsTrigger value="power" className="text-[12px] sm:text-sm px-2 sm:px-4">{t("serverList.tabs.power")}</TabsTrigger>
            <TabsTrigger value="maintenance" className="text-[12px] sm:text-sm px-2 sm:px-4">{t("serverList.tabs.maintenance")}</TabsTrigger>
            <TabsTrigger value="advanced" className="text-[12px] sm:text-sm px-2 sm:px-4">{t("serverList.tabs.advanced")}</TabsTrigger>
          </TabsList>
        </div>
        <div className="px-4 sm:px-6 pb-5">
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
        </div>
      </Tabs>
      </Card>


      {info.data && (
        <RenewalDialog
          serviceName={server.serviceName}
          info={info.data}
          open={renewalOpen}
          onOpenChange={setRenewalOpen}
        />
      )}

      {/* 撤单对话框。只在 eligible 时才可能打开,所以这里直接用 retraction.data */}

      {retraction.data?.eligible && (

        <RetractionDialog

          serviceName={server.serviceName}

          displayName={server.serviceName}

          info={retraction.data}

          open={retractOpen}

          onOpenChange={setRetractOpen}

        />

      )}

      <ReinstallDialog
        serviceName={server.serviceName}
        open={reinstallOpen}
        onOpenChange={setReinstallOpen}
      />

    </div>
  );
}

type FactTone = "warning" | "danger";

/** 标题卡下半部分的一格:标签 + 值(+ 次要说明);可点的带箭头,整格可点。 */
function FactCell({
  icon,
  label,
  value,
  sub,
  onClick,
  tone,
  mono,
  title,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  sub?: string;
  title?: string;
  onClick?: () => void;
  tone?: FactTone;
  mono?: boolean;
}) {
  const valueCls = `max-w-full text-[13.5px] font-semibold truncate ${mono ? "font-mono" : ""} ${
    tone === "danger" ? "text-destructive" : tone === "warning" ? "text-warning" : "text-foreground"
  }`;
  const body = (
    <>
      <div className="flex items-center gap-1.5 text-[12px] text-muted-foreground">
        {icon}
        {label}
      </div>
      <div className="mt-1 flex flex-wrap items-center gap-x-1.5 gap-y-0.5 min-w-0 min-h-[24px]">
        <span className={valueCls}>{value}</span>
        {sub && (
          <span className={`text-[11.5px] whitespace-nowrap ${tone ? (tone === "danger" ? "text-destructive" : "text-warning") : "text-muted-foreground"}`}>
            {sub}
          </span>
        )}
        {onClick && <ChevronRight className="ml-auto w-4 h-4 shrink-0 text-muted-foreground/70 group-hover:text-primary transition-colors" />}
      </div>
    </>
  );
  return onClick ? (
    <button type="button" onClick={onClick} title={title} className="group px-4 sm:px-6 py-3.5 min-w-0 text-left hover:bg-primary/[.035] transition-colors">
      {body}
    </button>
  ) : (
    <div className="px-4 sm:px-6 py-3.5 min-w-0" title={title}>{body}</div>
  );
}

/** "P1M" → "/ 月";"P12M" → "/ 12 个月";"P1Y" → "/ 年" */
function perPeriod(t: (k: string, o?: Record<string, unknown>) => string, duration: string): string {
  const m = /^P(\d+)([MY])$/.exec(duration || "");
  if (!m) return "";
  const n = Number(m[1]);
  if (m[2] === "Y") return n === 1 ? t("serverList.hero.perYear") : t("serverList.hero.perYears", { n });
  if (n === 12) return t("serverList.hero.perYear");
  return n === 1 ? t("serverList.hero.perMonth") : t("serverList.hero.perMonths", { n });
}

/** 费用格:续费价格(主机 + 附加选项,不含税)+ 目录安装费;悬停说明来源与口径 */
function PriceCell({ pricing }: { pricing: ReturnType<typeof useServerPricing> }) {
  const { t } = useTranslation();
  const d = pricing.data;
  const r = d?.renewal;
  const inst = d?.installation;
  const value = pricing.isPending
    ? t("serverList.hero.loading")
    : r
      ? `${r.partial ? "≈ " : ""}${fmtMoney(r.amount, r.currency)} ${perPeriod(t, r.duration)}`.trim()
      : "—";
  const sub =
    inst?.ok && inst.amount != null && inst.currency
      ? t("serverList.hero.setup", { amount: `${inst.partial ? "≈ " : ""}${fmtMoney(inst.amount, inst.currency)}` })
      : d
        ? t("serverList.hero.setupUnknown")
        : undefined;
  const title = pricing.isError
    ? t("serverList.hero.priceFailed", { detail: errorMessage(pricing.error) })
    : [
        t("serverList.hero.priceTitle", {
          options: r && r.options > 0 ? t("serverList.hero.priceOptions", { n: r.options }) : "",
          withTax: inst?.ok && inst.amount != null && inst.currency ? fmtMoney(inst.amount + (inst.tax || 0), inst.currency) : "—",
        }),
        r?.partial ? t("serverList.hero.pricePartial") : "",
      ]
        .filter(Boolean)
        .join(" ");
  return <FactCell icon={<Receipt className="w-3.5 h-3.5" />} label={t("serverList.hero.price")} value={value} sub={sub} title={title} />;
}

/**
 * 撤回期的悬停说明:把起止都写出来。
 *
 * 只显示"还剩 N 天"的话没法自查 —— 用户会拿它去对「开通日 + 14 天」,
 * 对不上就以为程序算错了。而撤回期是从**下单**起算的,机器常常下单后
 * 几天才交付,两个日期差好几天,对不上才是正常的。
 */
function retractionWindowText(
  t: TFn,
  r: { orderDate?: string; retractionDate?: string }
): string {
  const fmt = (v?: string) => (v ? fmtDateTime(v) : "");
  const end = fmt(r.retractionDate);
  const start = fmt(r.orderDate);
  if (!end) return t("serverList.retraction.inWindow");
  if (!start) return t("serverList.retraction.endOnly", { end });
  return t("serverList.retraction.window", { start, end });
}

/** 续费状态友好文案。OVH 在 manager 后台标的 "Cancellation scheduled"
 *  其实就是 renew.deleteAtExpiration=true(到期不续 + 自动注销)。
 *
 *  - 到期注销         deleteAtExpiration=true (优先级最高,其它字段无意义)
 *  - 强制自动续费     forced=true (OVH 套餐限制,用户改不了)
 *  - 自动 / 手动      根据 automatic 显示,带 N 月周期
 */
function formatRenewal(
  t: TFn,
  info: {
    renewalType: boolean;
    renewalPeriod: number;
    renewalDeleteAtExpiration: boolean;
    renewalForced: boolean;
    terminationScheduled?: boolean;
    terminationAction?: string;
    terminationStateUnknown?: boolean;
  }
): string {
  // 终止状态以 lifecycle.pendingActions 为准,并且要区分是哪一种 ——
  // 「立即终止」和「到期终止」后果天差地别
  const term = terminationLabel(info);
  if (term) return term.text;
  const period = info.renewalPeriod > 0 ? t("serverList.renewal.period", { n: info.renewalPeriod }) : "";
  if (info.renewalForced) return t("serverList.renewal.forced") + period;
  return (info.renewalType ? t("serverList.renewal.auto") : t("serverList.renewal.manual")) + period;
}
