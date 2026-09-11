import { createFileRoute } from "@tanstack/react-router";
import { Terminal, Server, RefreshCw, Eye, EyeOff, CalendarClock, CalendarPlus, Repeat, Activity, Network, CalendarRange } from "lucide-react";
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
import { LoadFailed, errorMessage } from "@/components/common/LoadFailed";
import {
  useOwnedServers,
  useServerServiceInfo,
  useServerMonitoring,
  useToggleMonitoring,
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
import { NetworkSpecsDialog } from "@/components/server-control/NetworkSpecsDialog";
import { RenewalDialog } from "@/components/server-control/RenewalDialog";
import { ReinstallDialog } from "@/components/server-control/ReinstallDialog";
import { EngagementDialog } from "@/components/server-control/EngagementDialog";
import { toast } from "sonner";

/** 服务器控制中心：顶部下拉切换服务器 + 4 tab 详情 */
export const Route = createFileRoute("/server-control")({
  component: ServerControlPage,
});

function ServerControlPage() {
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
  const activeAcc = accounts?.find((a) => a.id === activeAccount);

  return (
    <div className="space-y-3 sm:space-y-6">
      <PageHeader
        icon={Terminal}
        title="服务器控制"
        // 当前账户不在这里重复 —— 顶栏(手机)/侧栏(桌面)的切换器是全站唯一的账户显示位,
        // 它同时也负责把"账户列表读取失败"这件事说出来,所以这里不必再挂一份。
        description="管理 OVH 独立服务器"
        action={
          <div className="flex flex-wrap items-center gap-2">
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
      ) : q.isError ? (
        // 列表没拉到 ≠ 这个账户下没有机器。以前一律走「暂无服务器」+「您的 OVH 账户下还没有
        // 独立服务器」,用户看完就去别的账户找 / 以为机器被销毁了,而实际只是这次请求挂了。
        <Card>
          <div className="p-4 sm:p-6">
            <LoadFailed
              icon={Server}
              title="服务器列表读取失败"
              error={q.error}
              onRetry={() => q.refetch()}
            />
          </div>
        </Card>
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
          <CardContent className="p-4 sm:p-6 space-y-4 sm:space-y-5">
            {/* 服务器切换器 + 当前选中卡概览 */}
            <div className="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-3 pb-4 sm:pb-5 border-b border-border">
              <div className="flex flex-col sm:flex-row sm:items-center gap-2 sm:gap-3 min-w-0">
                <ServerSelector
                  servers={servers}
                  selected={selected}
                  onChange={(name) => setSelectedName(name)}
                  hidden={hidden}
                />
                {selected && (
                  <Chip tone={selected.state === "ok" ? "success" : "warning"} className="self-start sm:self-auto">
                    <StatusDot tone={selected.state === "ok" ? "success" : "warning"} pulse={selected.state === "ok"} size="xs" />
                    {selected.state}
                  </Chip>
                )}
              </div>
              {selected && (
                <div className="text-[11px] sm:text-[12px] text-muted-foreground break-all sm:truncate font-mono">
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
 * 顶部服务器选择器：胶囊样式 Select。
 * - 显示用 alias(有则用之,否则用原 name / service_name)
 * - 右键单击列表项弹"重命名"小菜单 → 进入 alias 编辑对话框
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
  // 别名读失败是这里唯一可以"静默降级"的一项:aliasOf 拿不到别名就回退显示原始名字
  // (service_name / OVH 的 display name),显示出来的东西依然真实,只是少了自定义标签,
  // 不会让用户对机器本身产生误判。所以不做失败提示,但下面重命名对话框要提醒一句 ——
  // 那里的输入框留空 + 保存等于删除别名,而读失败时它看起来也是空的。
  const aliasesQ = useServerAliases();
  const aliases = aliasesQ.data;
  const [ctxMenu, setCtxMenu] = useState<null | { server: OwnedServer; x: number; y: number }>(null);
  const [renaming, setRenaming] = useState<null | OwnedServer>(null);

  // 全局点击 / Esc 关闭 context menu
  useEffect(() => {
    if (!ctxMenu) return;
    const handler = () => setCtxMenu(null);
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") setCtxMenu(null); };
    window.addEventListener("click", handler);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("click", handler);
      window.removeEventListener("keydown", onKey);
    };
  }, [ctxMenu]);

  const displayName = (s: OwnedServer) => aliasOf(aliases, s.serviceName, s.name);

  return (
    <>
    <Select value={selected?.serviceName || ""} onValueChange={onChange}>
      <SelectTrigger
        className="rounded-full w-full sm:min-w-[280px] sm:w-auto lg:min-w-[340px] h-10 font-mono text-sm"
        // 拦截右键 pointerdown(button=2),不让 Radix Select 打开下拉
        onPointerDown={(e) => {
          if (e.button === 2) {
            e.preventDefault();
            e.stopPropagation();
          }
        }}
        // 右键当前选中那个胶囊 → 设别名(最直观)
        onContextMenu={(e) => {
          if (!selected) return;
          e.preventDefault();
          e.stopPropagation();
          setCtxMenu({ server: selected, x: e.clientX, y: e.clientY });
        }}
        title="左键打开列表;右键给当前服务器设别名"
      >
        <SelectValue placeholder="选择服务器">
          {selected && (
            <div className="flex items-center gap-2">
              <StatusDot tone={selected.state === "ok" ? "success" : "warning"} size="xs" />
              <span className="font-semibold">{maskSensitive(displayName(selected), hidden)}</span>
              <span className="text-[11px] text-muted-foreground font-sans ml-1">
                {selected.commercialRange} · {selected.datacenter.toUpperCase()}
              </span>
            </div>
          )}
        </SelectValue>
      </SelectTrigger>
      <SelectContent className="max-h-[400px]">
        {servers.map((s) => (
          <SelectItem
            key={s.serviceName}
            value={s.serviceName}
            className="font-mono"
          >
            {/* 内层 div 兜底右键 —— Radix SelectItem 自己的 onContextMenu 偶尔被吞 */}
            <div
              className="flex items-center gap-2"
              onContextMenu={(e) => {
                e.preventDefault();
                e.stopPropagation();
                setCtxMenu({ server: s, x: e.clientX, y: e.clientY });
              }}
            >
              <StatusDot tone={s.state === "ok" ? "success" : "warning"} size="xs" />
              <span className="font-semibold">{maskSensitive(displayName(s), hidden)}</span>
              <span className="text-[11px] text-muted-foreground font-sans ml-1">
                {s.commercialRange} · {s.datacenter.toUpperCase()}
              </span>
            </div>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>

    {/* 右键菜单:固定定位到鼠标位置,点空白 / Esc 关 */}
    {ctxMenu && (
      <div
        className="fixed z-[200] min-w-[140px] rounded-lg border border-border bg-popover shadow-md py-1 text-sm"
        style={{ left: ctxMenu.x, top: ctxMenu.y }}
        onClick={(e) => e.stopPropagation()}
        onContextMenu={(e) => e.preventDefault()}
      >
        <button
          type="button"
          className="w-full text-left px-3 py-1.5 hover:bg-muted text-foreground"
          onClick={() => {
            setRenaming(ctxMenu.server);
            setCtxMenu(null);
          }}
        >
          设置别名
        </button>
        {aliases?.[ctxMenu.server.serviceName] && (
          <button
            type="button"
            className="w-full text-left px-3 py-1.5 hover:bg-muted text-destructive"
            onClick={() => {
              setRenaming(ctxMenu.server);
              setCtxMenu(null);
            }}
          >
            清除别名…
          </button>
        )}
      </div>
    )}

    <RenameDialog
      server={renaming}
      currentAlias={renaming ? aliases?.[renaming.serviceName] || "" : ""}
      aliasesUnavailable={aliasesQ.isError}
      onClose={() => setRenaming(null)}
    />
    </>
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
          <DialogTitle>设置别名</DialogTitle>
          <DialogDescription className="font-mono text-[11px]">
            {server.serviceName}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-3">
          <Input
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder="例如:kele(留空清除别名)"
            autoFocus
            maxLength={64}
          />
          <p className="text-[11px] text-muted-foreground">
            别名仅在本程序里显示,不会下发到 OVH。
          </p>
          {aliasesUnavailable && (
            <p className="text-[11px] text-warning border border-warning/40 bg-warning/5 rounded-xl px-3 py-2">
              别名列表这次没读到,上面的输入框是空的并不代表这台机器原本没有别名。
              现在直接保存空值会把已有别名删掉,建议先刷新页面。
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              取消
            </Button>
            <Button type="submit" disabled={set.isPending}>
              {set.isPending ? "保存中…" : value.trim() === "" ? "清除并保存" : "保存"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ServerTabs({ server }: { server: OwnedServer }) {
  const info = useServerServiceInfo(server.serviceName);
  const monitoring = useServerMonitoring(server.serviceName);
  const toggleMon = useToggleMonitoring();
  const [netSpecsOpen, setNetSpecsOpen] = useState(false);
  const [renewalOpen, setRenewalOpen] = useState(false);
  const [reinstallOpen, setReinstallOpen] = useState(false);
  const [engagementOpen, setEngagementOpen] = useState(false);

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
      toast.success(monitoring.data ? "OVH 监控已关闭" : "OVH 监控已开启");
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "操作失败");
    }
  };

  return (
    <>
      <Tabs defaultValue="overview">
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <TabsList className="grid grid-cols-4 sm:flex h-auto gap-1 p-1">
            <TabsTrigger value="overview" className="text-[12px] sm:text-sm px-2 sm:px-3">概览</TabsTrigger>
            <TabsTrigger value="power" className="text-[12px] sm:text-sm px-2 sm:px-3">电源</TabsTrigger>
            <TabsTrigger value="maintenance" className="text-[12px] sm:text-sm px-2 sm:px-3">维护</TabsTrigger>
            <TabsTrigger value="advanced" className="text-[12px] sm:text-sm px-2 sm:px-3">高级</TabsTrigger>
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
                    value={formatRenewal(info.data)}
                    onClick={() => setRenewalOpen(true)}
                  />
                  <InfoPill
                    icon={<Terminal className="w-3.5 h-3.5" />}
                    label="OS"
                    value={server.os || "—"}
                    onClick={() => setReinstallOpen(true)}
                  />
                </>
              )}

              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="outline"
                    size="sm"
                    className={`h-7 rounded-full ${monitoringUnknown ? "border-destructive/40 text-destructive" : ""}`}
                    onClick={handleToggleMonitoring}
                    disabled={toggleMon.isPending || monitoring.isPending}
                  >
                    <Activity
                      className={`w-3.5 h-3.5 mr-1 ${
                        monitoringUnknown
                          ? "text-destructive"
                          : monitoring.data
                            ? "text-success"
                            : "text-muted-foreground"
                      }`}
                    />
                    {/* 三态分开写:读取中 / 读不到 / 真实开关状态。绝不能把"没读到"画成"已关" */}
                    {monitoring.isPending
                      ? "监控 读取中…"
                      : monitoringUnknown
                        ? "监控 状态未知 · 重试"
                        : monitoring.data
                          ? "监控 已开"
                          : "监控 已关"}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  {monitoringUnknown
                    ? `监控状态读取失败,点击重试：${errorMessage(monitoring.error)}`
                    : "OVH 自动监控（异常会邮件通知）"}
                </TooltipContent>
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

              <Tooltip>
                <TooltipTrigger asChild>
                  <Button variant="outline" size="sm" className="h-7 rounded-full" onClick={() => setEngagementOpen(true)}>
                    <CalendarRange className="w-3.5 h-3.5 mr-1" />
                    合同期
                  </Button>
                </TooltipTrigger>
                <TooltipContent>切换更长承诺期享受折扣 / 改到期策略</TooltipContent>
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

      {info.data && (
        <RenewalDialog
          serviceName={server.serviceName}
          info={info.data}
          open={renewalOpen}
          onOpenChange={setRenewalOpen}
        />
      )}

      <ReinstallDialog
        serviceName={server.serviceName}
        open={reinstallOpen}
        onOpenChange={setReinstallOpen}
      />

      <EngagementDialog
        serviceName={server.serviceName}
        open={engagementOpen}
        onOpenChange={setEngagementOpen}
      />
    </>
  );
}

/** 紧凑胶囊:服务信息条的单元素。
 *  传 onClick → 视觉与右侧 outline 按钮(监控/网络规格)对齐:bg-background + accent hover,
 *  跟纯展示的胶囊(到期/开通/OS,bg-secondary/50)在外观上明确区分。 */
function InfoPill({
  icon,
  label,
  value,
  onClick,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  onClick?: () => void;
}) {
  // 注意:本项目 --accent 在亮色模式被定义为近黑色(用作强调对比),不能用作 hover bg。
  // 跟旁边 Button outline 变体对齐(用 hover:bg-muted,见 button.tsx)。
  const cls = [
    "inline-flex items-center gap-1.5 h-7 pl-2.5 pr-3 rounded-full border text-[12px]",
    onClick
      ? "border-border bg-background hover:bg-muted cursor-pointer transition-colors shadow-sm"
      : "border-border bg-secondary/50",
  ].join(" ");
  const inner = (
    <>
      <span className="flex items-center gap-1 text-muted-foreground">
        {icon}
        {label}
      </span>
      <span className="font-medium text-foreground">{value}</span>
    </>
  );
  if (onClick) {
    return (
      <button type="button" onClick={onClick} className={cls}>
        {inner}
      </button>
    );
  }
  return <div className={cls}>{inner}</div>;
}

/** 续费状态友好文案。OVH 在 manager 后台标的 "Cancellation scheduled"
 *  其实就是 renew.deleteAtExpiration=true(到期不续 + 自动注销)。
 *
 *  - 到期注销         deleteAtExpiration=true (优先级最高,其它字段无意义)
 *  - 强制自动续费     forced=true (OVH 套餐限制,用户改不了)
 *  - 自动 / 手动      根据 automatic 显示,带 N 月周期
 */
function formatRenewal(info: {
  renewalType: boolean;
  renewalPeriod: number;
  renewalDeleteAtExpiration: boolean;
  renewalForced: boolean;
  terminationScheduled?: boolean;
  terminationAction?: string;
  terminationStateUnknown?: boolean;
}): string {
  // 终止状态以 lifecycle.pendingActions 为准,并且要区分是哪一种 ——
  // 「立即终止」和「到期终止」后果天差地别
  const term = terminationLabel(info);
  if (term) return term.text;
  const period = info.renewalPeriod > 0 ? ` · ${info.renewalPeriod}月` : "";
  if (info.renewalForced) return `强制自动${period}`;
  return (info.renewalType ? "自动" : "手动") + period;
}
