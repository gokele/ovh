import { useState } from "react";
import { maskSensitive, useHideIp } from "@/hooks/use-hide-ip";

/** CIDR(1.2.3.0/24)带掩码,maskSensitive 的 IPv4 正则吃不下,先拆开再打 */
function maskBlockValue(v: string, hidden: boolean): string {
  const [ip, mask] = (v || "").split("/");
  return maskSensitive(ip, hidden) + (mask ? "/" + mask : "");
}
import {
  Zap, Shield, FolderArchive, Globe, Wifi, Network, ShoppingBag, Settings, MapPin,
  Power, AlertCircle, ShieldAlert, Plus, Trash2, KeyRound,
} from "lucide-react";
import type { OwnedServer } from "@/hooks/use-server-control";
import {
  useServerBurst, useSetBurst,
  useServerFirewall, useSetFirewall,
  useServerBackupFtp, useActivateBackupFtp, useDeleteBackupFtp, useResetBackupFtpPassword,
  useBackupFtpAuthorizableBlocks, useAddBackupFtpAccess, useDeleteBackupFtpAccess,
  useServerSecondaryDns,
  useServerVirtualMac,
  useServerVrack,
  useServerOrderable,
  useServerOptions,
  useServerIpSpecs,
  useMitigation, useEnableMitigation, useDisableMitigation,
} from "@/hooks/use-server-control";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Chip } from "@/components/common/Chip";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { PartialNotice, DetailErrorTag } from "@/components/common/PartialNotice";
import { useActiveAccountEndpoint } from "@/components/common/active-endpoint";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { errorMessage } from "@/components/common/LoadFailed";

/** 高级 Tab：旧前端的 9 个 sub-tab 全部接入 */
export function AdvancedTab({ server }: { server: OwnedServer }) {
  const { t } = useTranslation();
  return (
    <Tabs defaultValue="burst" className="space-y-4">
      <TabsList className="grid grid-cols-3 sm:grid-cols-5 lg:flex lg:flex-wrap h-auto gap-1 p-1">
        <TabsTrigger value="burst" className="text-[11px] sm:text-[12px] px-2"><Zap className="w-3.5 h-3.5 mr-1" />Burst</TabsTrigger>
        <TabsTrigger value="firewall" className="text-[11px] sm:text-[12px] px-2"><Shield className="w-3.5 h-3.5 mr-1" />{t("maint.advanced.tabs.firewall")}</TabsTrigger>
        <TabsTrigger value="ftp" className="text-[11px] sm:text-[12px] px-2"><FolderArchive className="w-3.5 h-3.5 mr-1" />FTP</TabsTrigger>
        <TabsTrigger value="dns" className="text-[11px] sm:text-[12px] px-2"><Globe className="w-3.5 h-3.5 mr-1" />{t("maint.advanced.tabs.dns")}</TabsTrigger>
        <TabsTrigger value="vmac" className="text-[11px] sm:text-[12px] px-2"><Wifi className="w-3.5 h-3.5 mr-1" />{t("maint.advanced.tabs.vmac")}</TabsTrigger>
        <TabsTrigger value="vrack" className="text-[11px] sm:text-[12px] px-2"><Network className="w-3.5 h-3.5 mr-1" />vRack</TabsTrigger>
        <TabsTrigger value="orderable" className="text-[11px] sm:text-[12px] px-2"><ShoppingBag className="w-3.5 h-3.5 mr-1" />{t("maint.advanced.tabs.orderable")}</TabsTrigger>
        <TabsTrigger value="options" className="text-[11px] sm:text-[12px] px-2"><Settings className="w-3.5 h-3.5 mr-1" />{t("maint.advanced.tabs.options")}</TabsTrigger>
        <TabsTrigger value="ip" className="text-[11px] sm:text-[12px] px-2"><MapPin className="w-3.5 h-3.5 mr-1" />{t("maint.advanced.tabs.ip")}</TabsTrigger>
        <TabsTrigger value="ddos" className="text-[11px] sm:text-[12px] px-2"><ShieldAlert className="w-3.5 h-3.5 mr-1" />DDoS</TabsTrigger>
      </TabsList>

      <TabsContent value="burst"><BurstPane serviceName={server.serviceName} /></TabsContent>
      <TabsContent value="firewall"><FirewallPane serviceName={server.serviceName} /></TabsContent>
      <TabsContent value="ftp"><BackupFtpPane serviceName={server.serviceName} /></TabsContent>
      <TabsContent value="dns"><SecondaryDnsPane serviceName={server.serviceName} /></TabsContent>
      <TabsContent value="vmac"><VirtualMacPane serviceName={server.serviceName} /></TabsContent>
      <TabsContent value="vrack"><VrackPane serviceName={server.serviceName} /></TabsContent>
      <TabsContent value="orderable"><OrderablePane serviceName={server.serviceName} /></TabsContent>
      <TabsContent value="options"><OptionsPane serviceName={server.serviceName} /></TabsContent>
      <TabsContent value="ip"><IpSpecsPane serviceName={server.serviceName} /></TabsContent>
      <TabsContent value="ddos"><MitigationPane serviceName={server.serviceName} /></TabsContent>
    </Tabs>
  );
}

// ─────────────────────────────── Burst ───────────────────────────────

function BurstPane({ serviceName }: { serviceName: string }) {
  const { t } = useTranslation();
  const q = useServerBurst(serviceName);
  const mut = useSetBurst();
  if (q.isPending) return <PaneSkeleton />;
  const data: any = q.data;
  if (!data || data.notAvailable) return <NotAvailable icon={Zap} title={t("maint.advanced.burst.unavailable")} message={data?.error} />;
  const burst = data.burst;
  if (!burst) return <EmptyState icon={Zap} title={t("maint.advanced.burst.empty")} />;
  // 旧前端：burst.status 是字符串 "active" / "inactive"
  const active = burst.status === "active";
  const capacityText =
    burst.capacity && typeof burst.capacity === "object"
      ? `${burst.capacity.value} ${burst.capacity.unit || ""}`.trim()
      : null;
  return (
    <Pane title={t("maint.advanced.burst.title")} icon={Zap}>
      <Row label={t("maint.advanced.burst.status")} value={<Chip tone={active ? "success" : "default"}>{active ? t("maint.advanced.burst.enabled") : t("maint.advanced.burst.disabled")}</Chip>} />
      {capacityText && <Row label={t("maint.advanced.burst.capacity")} value={capacityText} />}
      <div className="pt-2">
        <Button
          variant="outline"
          size="sm"
          disabled={mut.isPending}
          onClick={async () => {
            const next = active ? "inactive" : "active";
            try {
              await mut.mutateAsync({ serviceName, status: next });
              toast.success(active ? t("maint.advanced.burst.toastOff") : t("maint.advanced.burst.toastOn"));
            } catch (e: any) {
              toast.error(errorMessage(e));
            }
          }}
        >
          <Power className="w-3.5 h-3.5 mr-1" />
          {active ? t("maint.advanced.burst.disableBtn") : t("maint.advanced.burst.enableBtn")}
        </Button>
      </div>
    </Pane>
  );
}

// ─────────────────────────────── Firewall ───────────────────────────────

function FirewallPane({ serviceName }: { serviceName: string }) {
  const { t } = useTranslation();
  const q = useServerFirewall(serviceName);
  const mut = useSetFirewall();
  if (q.isPending) return <PaneSkeleton />;
  const data: any = q.data;
  if (!data || data.notAvailable) return <NotAvailable icon={Shield} title={t("maint.advanced.firewall.unavailable")} message={data?.error} />;
  const fw = data.firewall;
  if (!fw) return <EmptyState icon={Shield} title={t("maint.advanced.firewall.empty")} />;
  // 旧前端：直接读 firewall.enabled（boolean）
  const enabled = !!fw.enabled;
  return (
    <Pane title={t("maint.advanced.firewall.title")} icon={Shield}>
      <Row label={t("maint.advanced.firewall.status")} value={<Chip tone={enabled ? "success" : "default"}>{enabled ? t("maint.advanced.firewall.on") : t("maint.advanced.firewall.off")}</Chip>} />
      {fw.mode && <Row label={t("maint.advanced.firewall.mode")} value={String(fw.mode)} />}
      {fw.model && <Row label={t("maint.advanced.firewall.model")} value={String(fw.model)} />}
      <div className="pt-2">
        <Button
          variant="outline"
          size="sm"
          disabled={mut.isPending}
          onClick={async () => {
            try {
              await mut.mutateAsync({ serviceName, enabled: !enabled });
              toast.success(enabled ? t("maint.advanced.firewall.toastOff") : t("maint.advanced.firewall.toastOn"));
            } catch (e: any) {
              toast.error(errorMessage(e));
            }
          }}
        >
          <Power className="w-3.5 h-3.5 mr-1" />
          {enabled ? t("maint.advanced.firewall.disableBtn") : t("maint.advanced.firewall.enableBtn")}
        </Button>
      </div>
    </Pane>
  );
}

// ─────────────────────────────── Backup FTP ───────────────────────────────

function BackupFtpPane({ serviceName }: { serviceName: string }) {
  const { t } = useTranslation();
  const { hidden } = useHideIp();
  const { isUS, ready } = useActiveAccountEndpoint();
  // US 区官方 schema 里 backupFTP 五条路径全都不存在，请求发出去只会拿到 notAvailable。
  // 账户信息一加载完就本地拦下，省掉一次注定失败的往返（enabled=false 让 hook 根本不发请求）。
  const usBlocked = ready && isUS;
  const q = useServerBackupFtp(usBlocked ? null : serviceName);
  const act = useActivateBackupFtp();
  const del = useDeleteBackupFtp();
  const resetPwd = useResetBackupFtpPassword();
  const addAccess = useAddBackupFtpAccess();
  const delAccess = useDeleteBackupFtpAccess();
  // 只有服务已激活时才去问"可授权的 IP 段"，否则未激活的机器每次打开都会白发一次必失败的请求
  const blocks = useBackupFtpAuthorizableBlocks(usBlocked ? null : serviceName, !!q.data?.backupFtp);
  const [newBlock, setNewBlock] = useState("");
  const [nfs, setNfs] = useState(false);
  const [cifs, setCifs] = useState(false);

  if (usBlocked) {
    return (
      <NotAvailable
        icon={FolderArchive}
        title={t("maint.advanced.ftp.usTitle")}
        message={t("maint.advanced.ftp.usMessage")}
      />
    );
  }
  if (q.isPending) return <PaneSkeleton />;
  const data = q.data;
  if (!data) return <EmptyState icon={FolderArchive} title={t("maint.advanced.ftp.empty")} />;
  // unknownService = 切错账户（这台机器不属于当前账户）。此时给「激活」按钮等于给一个必然
  // 失败的按钮，所以只显示原因，并提示去切账户
  if (data.unknownService) {
    return (
      <NotAvailable
        icon={FolderArchive}
        title={t("maint.advanced.ftp.unknownTitle")}
        message={data.reason ? t("maint.advanced.ftp.unknownReason", { err: data.error || "", reason: data.reason }) : data.error}
      />
    );
  }
  if (data.notAvailable) return <NotAvailable icon={FolderArchive} title={t("maint.advanced.ftp.unavailable")} message={data.error} />;
  if (data.notActivated) {
    return (
      <Pane title="Backup FTP" icon={FolderArchive}>
        <p className="text-[12px] text-muted-foreground">{t("maint.advanced.ftp.notActivatedDesc")}</p>
        <div className="pt-3">
          <Button
            size="sm"
            disabled={act.isPending}
            onClick={async () => {
              try {
                await act.mutateAsync(serviceName);
                toast.success(t("maint.advanced.ftp.activateSent"));
              } catch (e: any) {
                toast.error(errorMessage(e));
              }
            }}
          >
            {act.isPending ? t("maint.advanced.ftp.activating") : t("maint.advanced.ftp.activateBtn")}
          </Button>
        </div>
      </Pane>
    );
  }
  const ftp: Record<string, any> = data.backupFtp || {};
  const accessList = data.accessList || [];
  // 旧前端：quota 和 usage 都是 { value, unit } 对象
  const quotaText =
    ftp.quota && typeof ftp.quota === "object" ? `${ftp.quota.value} ${ftp.quota.unit || ""}`.trim() : null;
  const usageText =
    ftp.usage && typeof ftp.usage === "object" ? `${ftp.usage.value} ${ftp.usage.unit || ""}`.trim() : null;

  const handleAdd = async () => {
    const ipBlock = newBlock.trim();
    if (!ipBlock) {
      toast.error(t("maint.advanced.ftp.needBlock"));
      return;
    }
    try {
      await addAccess.mutateAsync({ serviceName, ipBlock, ftp: true, nfs, cifs });
      toast.success(t("maint.advanced.ftp.addedToast"));
      setNewBlock("");
      setNfs(false);
      setCifs(false);
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <Pane title="Backup FTP" icon={FolderArchive}>
      {quotaText && <Row label={t("maint.advanced.ftp.quota")} value={quotaText} />}
      {usageText && <Row label={t("maint.advanced.ftp.used")} value={usageText} />}
      {ftp.ftpBackupName && <Row label={t("maint.advanced.ftp.host")} value={<code className="font-mono text-[12px]">{ftp.ftpBackupName}</code>} />}

      <div className="flex flex-wrap gap-2 pt-2">
        <Button
          size="sm"
          variant="outline"
          disabled={resetPwd.isPending}
          onClick={async () => {
            try {
              await resetPwd.mutateAsync(serviceName);
              toast.success(t("maint.advanced.ftp.pwdToast"));
            } catch (e: any) {
              toast.error(errorMessage(e));
            }
          }}
        >
          <KeyRound className="w-3.5 h-3.5 mr-1" />
          {resetPwd.isPending ? t("maint.advanced.ftp.submitting") : t("maint.advanced.ftp.resetPwd")}
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="text-destructive"
          disabled={del.isPending}
          onClick={async () => {
            // 关停会连带删掉里面已有的备份，属于不可逆操作，必须先要用户明确同意
            if (!window.confirm(t("maint.advanced.ftp.closeConfirm"))) return;
            try {
              await del.mutateAsync(serviceName);
              toast.success(t("maint.advanced.ftp.closedToast"));
            } catch (e: any) {
              toast.error(errorMessage(e));
            }
          }}
        >
          <Trash2 className="w-3.5 h-3.5 mr-1" />
          {del.isPending ? t("maint.advanced.ftp.submitting") : t("maint.advanced.ftp.closeBtn")}
        </Button>
      </div>

      <div className="pt-3 space-y-2">
        <h4 className="text-[12px] font-semibold">{t("maint.advanced.ftp.aclTitle")}</h4>
        {/* 列表整体没拉到 ≠ 没配过 IP，必须说清楚，否则用户会重复添加已有的授权 */}
        {data.accessError && (
          <div className="flex items-start gap-2 rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2 text-[11px]">
            <AlertCircle className="w-3.5 h-3.5 text-destructive flex-shrink-0 mt-0.5" />
            <span>{t("maint.advanced.ftp.aclFailed", { err: data.accessError })}</span>
          </div>
        )}
        <PartialNotice failedCount={data.accessFailedCount || 0} what={t("maint.advanced.ftp.aclPartialWhat")} />

        {/* 添加授权 IP */}
        <div className="flex flex-wrap items-center gap-2">
          <Input
            value={newBlock}
            onChange={(e) => setNewBlock(e.target.value)}
            placeholder={t("maint.advanced.ftp.blockPlaceholder")}
            className="h-8 w-56 font-mono text-[12px]"
          />
          <label className="flex items-center gap-1 text-[11px] cursor-pointer">
            <input type="checkbox" checked={nfs} onChange={(e) => setNfs(e.target.checked)} className="w-4 h-4 sm:w-3.5 sm:h-3.5" />
            NFS
          </label>
          <label className="flex items-center gap-1 text-[11px] cursor-pointer">
            <input type="checkbox" checked={cifs} onChange={(e) => setCifs(e.target.checked)} className="w-4 h-4 sm:w-3.5 sm:h-3.5" />
            CIFS
          </label>
          <Button size="sm" variant="outline" className="h-8" onClick={handleAdd} disabled={addAccess.isPending}>
            <Plus className="w-3.5 h-3.5 mr-1" />
            {addAccess.isPending ? t("maint.advanced.ftp.submitting") : t("maint.advanced.ftp.authorize")}
          </Button>
        </div>
        {(blocks.data || []).length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {(blocks.data || []).map((b) => (
              <button
                key={b}
                type="button"
                onClick={() => setNewBlock(b)}
                className="text-[10.5px] font-mono px-1.5 py-0.5 rounded border border-border hover:bg-muted"
              >
                {b}
              </button>
            ))}
          </div>
        )}

        {accessList.length === 0 ? (
          <p className="text-[12px] text-muted-foreground">{t("maint.advanced.ftp.emptyAcl")}</p>
        ) : (
          <div className="border border-border rounded-2xl divide-y divide-border">
            {accessList.map((a, idx) => (
              <div key={a.ipBlock || idx} className="px-4 py-2.5 text-[13px] flex items-center gap-2 flex-wrap">
                <code className="font-mono">{maskBlockValue(a.ipBlock, hidden)}</code>
                {a.ftp && <Chip tone="default">FTP</Chip>}
                {a.nfs && <Chip tone="default">NFS</Chip>}
                {a.cifs && <Chip tone="default">CIFS</Chip>}
                {a.isApplied === false && <Chip tone="warning">{t("maint.advanced.ftp.applying")}</Chip>}
                {/* error 现在是 OVH 原始 message，直接透出来比 "fetch failed" 有用得多 */}
                <DetailErrorTag message={a.error} />
                <Button
                  size="sm"
                  variant="outline"
                  className="ml-auto h-7 px-2 text-destructive"
                  disabled={delAccess.isPending}
                  onClick={async () => {
                    if (!window.confirm(t("maint.advanced.ftp.deleteConfirm", { block: a.ipBlock }))) return;
                    try {
                      await delAccess.mutateAsync({ serviceName, ipBlock: a.ipBlock });
                      toast.success(t("maint.advanced.ftp.deletedToast"));
                    } catch (e: any) {
                      toast.error(errorMessage(e));
                    }
                  }}
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </Button>
              </div>
            ))}
          </div>
        )}
      </div>
    </Pane>
  );
}

// ─────────────────────────────── Secondary DNS ───────────────────────────────

function SecondaryDnsPane({ serviceName }: { serviceName: string }) {
  const { t } = useTranslation();
  const q = useServerSecondaryDns(serviceName);
  if (q.isPending) return <PaneSkeleton />;
  // 后端全挂时返 500 —— 显示错误而不是空列表，否则用户会以为「确实没配过」
  if (q.isError) return <LoadFailed icon={Globe} title={t("maint.advanced.dns.loadFailed")} error={q.error} onRetry={() => q.refetch()} />;
  const { items, failedCount } = q.data || { items: [], partial: false, failedCount: 0 };
  if (items.length === 0) return <EmptyState icon={Globe} title={t("maint.advanced.dns.empty")} />;
  return (
    <Pane title={t("maint.advanced.dns.title")} icon={Globe}>
      <PartialNotice failedCount={failedCount} what={t("maint.advanced.dns.partialWhat")} className="mb-2" />
      <div className="border border-border rounded-2xl divide-y divide-border">
        {items.map((d, idx) => (
          <div key={d.domain || idx} className="px-4 py-3 flex items-center justify-between gap-2 text-[13px]">
            <code className="font-mono">{d.domain || "—"}</code>
            <div className="flex items-center gap-2">
              {d.dns && <code className="font-mono text-muted-foreground text-[12px]">{d.dns}</code>}
              <DetailErrorTag message={d._detailError} />
            </div>
          </div>
        ))}
      </div>
    </Pane>
  );
}

// ─────────────────────────────── Virtual MAC ───────────────────────────────

function VirtualMacPane({ serviceName }: { serviceName: string }) {
  const { t } = useTranslation();
  const { hidden } = useHideIp();
  const q = useServerVirtualMac(serviceName);
  if (q.isPending) return <PaneSkeleton />;
  if (q.isError) return <LoadFailed icon={Wifi} title={t("maint.advanced.vmac.loadFailed")} error={q.error} onRetry={() => q.refetch()} />;
  const { items, failedCount } = q.data || { items: [], partial: false, failedCount: 0 };
  if (items.length === 0) return <EmptyState icon={Wifi} title={t("maint.advanced.vmac.empty")} />;
  return (
    <Pane title={t("maint.advanced.vmac.title")} icon={Wifi}>
      <PartialNotice failedCount={failedCount} what={t("maint.advanced.vmac.partialWhat")} className="mb-2" />
      <div className="border border-border rounded-2xl divide-y divide-border">
        {items.map((m, idx) => (
          <div key={m.macAddress || idx} className="px-3 sm:px-4 py-2.5 sm:py-3 grid grid-cols-1 sm:grid-cols-3 gap-1 sm:gap-2 sm:items-center text-[12px] sm:text-[13px]">
            <code className="font-mono break-all">{m.macAddress || "—"}</code>
            <span className="text-muted-foreground flex items-center gap-2">
              {m.type || "—"}
              <DetailErrorTag message={m._detailError} />
            </span>
            <code className="font-mono text-muted-foreground sm:text-right break-all">{m.ipAddress ? maskSensitive(m.ipAddress, hidden) : "—"}</code>
          </div>
        ))}
      </div>
    </Pane>
  );
}

// ─────────────────────────────── vRack ───────────────────────────────

function VrackPane({ serviceName }: { serviceName: string }) {
  const { t } = useTranslation();
  const q = useServerVrack(serviceName);
  if (q.isPending) return <PaneSkeleton />;
  if (q.isError) return <LoadFailed icon={Network} title={t("maint.advanced.vrack.loadFailed")} error={q.error} onRetry={() => q.refetch()} />;
  const { items, failedCount } = q.data || { items: [], partial: false, failedCount: 0 };
  if (items.length === 0) return <EmptyState icon={Network} title={t("maint.advanced.vrack.empty")} />;
  return (
    <Pane title={t("maint.advanced.vrack.title")} icon={Network}>
      <PartialNotice failedCount={failedCount} what={t("maint.advanced.vrack.partialWhat")} className="mb-2" />
      <div className="border border-border rounded-2xl divide-y divide-border">
        {items.map((v, idx) => (
          <div key={v.vrackName || idx} className="px-4 py-2.5 text-[13px] font-mono flex items-center justify-between gap-2">
            <span>{v.vrackName || "—"}</span>
            <DetailErrorTag message={v._detailError} />
          </div>
        ))}
      </div>
    </Pane>
  );
}

// ─────────────────────────────── Orderable ───────────────────────────────

/** 可订购：带宽（platinum/premium/ultimate 套餐数）+ 流量（数量）+ IPv4/IPv6 块 */
function OrderablePane({ serviceName }: { serviceName: string }) {
  const { t } = useTranslation();
  const q = useServerOrderable(serviceName);
  if (q.isPending) return <PaneSkeleton />;
  const data = q.data;
  if (!data || (!data.bandwidth && !data.traffic && !data.ip)) {
    return <EmptyState icon={ShoppingBag} title={t("maint.advanced.orderable.empty")} />;
  }

  return (
    <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
      {/* 带宽 */}
      <Pane title={t("maint.advanced.orderable.bwTitle")} icon={ShoppingBag} compact>
        {data.bandwidth ? (
          data.bandwidth.orderable ? (
            <div className="space-y-2">
              <TierLine name="Platinum" count={data.bandwidth.platinum?.length || 0} />
              <TierLine name="Premium" count={data.bandwidth.premium?.length || 0} />
              <TierLine name="Ultimate" count={data.bandwidth.ultimate?.length || 0} />
            </div>
          ) : (
            <NotOrderable />
          )
        ) : (
          <NotOrderable />
        )}
      </Pane>

      {/* 流量 */}
      <Pane title={t("maint.advanced.orderable.trafficTitle")} icon={ShoppingBag} compact>
        {data.traffic ? (
          data.traffic.orderable ? (
            <TierLine name={t("maint.advanced.orderable.trafficPlans")} count={data.traffic.traffic?.length || 0} />
          ) : (
            <NotOrderable />
          )
        ) : (
          <NotOrderable />
        )}
      </Pane>

      {/* IP 块 */}
      <Pane title={t("maint.advanced.orderable.ipTitle")} icon={ShoppingBag} compact>
        {data.ip ? (
          <IpBlockList ipv4={data.ip.ipv4} ipv6={data.ip.ipv6} />
        ) : (
          <NotOrderable />
        )}
      </Pane>
    </div>
  );
}

function TierLine({ name, count }: { name: string; count: number }) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center justify-between text-[12px]">
      <span className={count > 0 ? "font-semibold" : "text-muted-foreground"}>{name}</span>
      <span className={count > 0 ? "font-mono" : "text-muted-foreground font-mono"}>
        {count > 0 ? t("maint.advanced.orderable.plansCount", { n: count }) : "—"}
      </span>
    </div>
  );
}

function NotOrderable() {
  const { t } = useTranslation();
  return <p className="text-[12px] text-muted-foreground">{t("maint.advanced.orderable.notOrderable")}</p>;
}

// ─────────────────────────────── IP Specs / IP 块通用 ───────────────────────────────

function IpBlockList({ ipv4, ipv6 }: { ipv4?: any[]; ipv6?: any[] }) {
  const { t } = useTranslation();
  const has4 = ipv4 && ipv4.length > 0;
  const has6 = ipv6 && ipv6.length > 0;
  if (!has4 && !has6) {
    return <p className="text-[12px] text-muted-foreground">{t("maint.advanced.ip.noOptions")}</p>;
  }
  return (
    <div className="space-y-2.5">
      {has4 && (
        <div className="space-y-1.5">
          <div className="text-[11px] font-semibold text-muted-foreground">IPv4</div>
          {ipv4!.map((ip, idx) => (
            <IpBlock key={idx} ip={ip} family="v4" />
          ))}
        </div>
      )}
      {has6 && (
        <div className="space-y-1.5">
          <div className="text-[11px] font-semibold text-muted-foreground">IPv6</div>
          {ipv6!.map((ip, idx) => (
            <IpBlock key={idx} ip={ip} family="v6" />
          ))}
        </div>
      )}
    </div>
  );
}

function IpBlock({ ip, family }: { ip: any; family: "v4" | "v6" }) {
  const { t } = useTranslation();
  const typeLabel =
    ip.type === "failover"
      ? t("maint.advanced.ip.failover")
      : ip.type === "static"
        ? t("maint.advanced.ip.static")
        : ip.type || t("maint.advanced.ip.fallbackType", { family });
  return (
    <div className="border border-border rounded-xl p-2.5 text-[12px] space-y-1 bg-background">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <span className="font-semibold">{typeLabel}</span>
        {ip.included && <Chip tone="success">{t("maint.advanced.ip.included")}</Chip>}
        {ip.optionRequired && <Chip tone="warning">{t("maint.advanced.ip.optionRequired")}</Chip>}
      </div>
      {ip.blockSizes && ip.blockSizes.length > 0 && (
        <div className="text-muted-foreground">
          {t("maint.advanced.ip.blockSizes")}<code className="font-mono">{ip.blockSizes.join(", ")}</code>
        </div>
      )}
      {ip.ipNumber != null && (
        <div className="text-muted-foreground">
          {t("maint.advanced.ip.ipNumber")}<span className="text-foreground">{ip.ipNumber}</span>
          {ip.number != null && (
            <span className="ml-2">
              {t("maint.advanced.ip.number")}<span className="text-foreground">{ip.number}</span>
            </span>
          )}
        </div>
      )}
      {ip.optionRequired && (
        <div className="text-warning text-[11px]">{t("maint.advanced.ip.needOption", { what: ip.optionRequired })}</div>
      )}
    </div>
  );
}

// ─────────────────────────────── Options ───────────────────────────────

/** OVH 附加选项名 → 语言包 key(渲染处 t();没命中的透传原始值) */
const OPTION_NAME_KEYS: Record<string, string> = {
  BANDWIDTH: "maint.advanced.options.names.bandwidth",
  TRAFFIC: "maint.advanced.options.names.traffic",
  BACKUP_STORAGE: "maint.advanced.options.names.backupStorage",
  HARD_RAID: "maint.advanced.options.names.hardRaid",
  SLA: "maint.advanced.options.names.sla",
  SYSTEM_STORAGE: "maint.advanced.options.names.systemStorage",
  MEMORY: "maint.advanced.options.names.memory",
  CPU: "maint.advanced.options.names.cpu",
  PRIVATE_BANDWIDTH: "maint.advanced.options.names.privateBandwidth",
};

function OptionsPane({ serviceName }: { serviceName: string }) {
  const { t } = useTranslation();
  const q = useServerOptions(serviceName);
  if (q.isPending) return <PaneSkeleton />;
  if (q.isError) return <LoadFailed icon={Settings} title={t("maint.advanced.options.loadFailed")} error={q.error} onRetry={() => q.refetch()} />;
  const { items, failedCount } = q.data || { items: [], partial: false, failedCount: 0 };
  if (items.length === 0) return <EmptyState icon={Settings} title={t("maint.advanced.options.empty")} />;

  // OVH state 取值：subscribed / released / releasing / toDelete
  const stateTone = (state: string): "success" | "warning" | "default" => {
    const s = state?.toLowerCase();
    if (s === "subscribed") return "success";
    if (s === "releasing" || s === "todelete") return "warning";
    return "default";
  };

  return (
    <Pane title={t("maint.advanced.options.title")} icon={Settings}>
      <PartialNotice failedCount={failedCount} what={t("maint.advanced.options.partialWhat")} className="mb-2" />
      <div className="border border-border rounded-2xl divide-y divide-border">
        {items.map((o, idx) => {
          const nameKey = OPTION_NAME_KEYS[String(o.option || "").toUpperCase()];
          const label = nameKey
            ? t(nameKey)
            : o.option || t("maint.advanced.options.fallbackItem", { n: idx + 1 });
          return (
            <div key={o.option || idx} className="px-4 py-2.5 flex items-center justify-between text-[13px] gap-3">
              <span className="font-semibold truncate">{label}</span>
              <div className="text-right flex items-center gap-2">
                <DetailErrorTag message={o._detailError} />
                {o.state ? (
                  <Chip tone={stateTone(o.state)}>{o.state}</Chip>
                ) : (
                  <span className="text-muted-foreground text-[12px]">—</span>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </Pane>
  );
}

// ─────────────────────────────── IP Specs ───────────────────────────────

function IpSpecsPane({ serviceName }: { serviceName: string }) {
  const { t } = useTranslation();
  const q = useServerIpSpecs(serviceName);
  if (q.isPending) return <PaneSkeleton />;
  const data = q.data;
  if (!data) return <EmptyState icon={MapPin} title={t("maint.advanced.ip.noSpecs")} />;
  const ipv4 = Array.isArray(data.ipv4) ? data.ipv4 : [];
  const ipv6 = Array.isArray(data.ipv6) ? data.ipv6 : [];
  if (ipv4.length === 0 && ipv6.length === 0) return <EmptyState icon={MapPin} title={t("maint.advanced.ip.empty")} />;
  return (
    <Pane title={t("maint.advanced.ip.title")} icon={MapPin}>
      <IpBlockList ipv4={ipv4} ipv6={ipv6} />
    </Pane>
  );
}

// ─────────────────────────────── 通用小件 ───────────────────────────────

function Pane({
  title, icon: Icon, children, compact,
}: {
  title: string;
  icon: React.ComponentType<{ className?: string }>;
  children: React.ReactNode;
  compact?: boolean;
}) {
  return (
    <div className={`border border-border rounded-2xl ${compact ? "p-4" : "p-5"}`}>
      <div className="flex items-center gap-2 mb-3">
        <Icon className="w-4 h-4 text-muted-foreground" />
        <h3 className="text-[13px] font-semibold">{title}</h3>
      </div>
      <div className="space-y-1.5">{children}</div>
    </div>
  );
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex justify-between items-center text-[13px] gap-3">
      <span className="text-muted-foreground truncate">{label}</span>
      <div className="font-medium text-right min-w-0">{value}</div>
    </div>
  );
}

function NotAvailable({
  icon: Icon, title, message,
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  message?: string;
}) {
  const { t } = useTranslation();
  return (
    <div className="border border-border rounded-2xl p-6 flex flex-col items-center gap-2 text-center">
      <Icon className="w-8 h-8 text-muted-foreground" />
      <h4 className="text-[13px] font-semibold">{title}</h4>
      {message ? (
        <p className="text-[11px] text-muted-foreground">{message}</p>
      ) : (
        <p className="text-[11px] text-muted-foreground flex items-center gap-1">
          <AlertCircle className="w-3 h-3" />
          {t("maint.advanced.notAvailableDefault")}
        </p>
      )}
    </div>
  );
}

/**
 * 「读取失败」占位。
 * 后端现在会在详情全挂时返 500，前端如果还沿用空态文案（「未配置 xx」），用户会当成
 * OVH 里确实没有这项配置从而放弃重试 —— 这两种状态必须分开渲染。
 */
function LoadFailed({
  icon: Icon, title, error, onRetry,
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  error?: unknown;
  onRetry?: () => void;
}) {
  const { t } = useTranslation();
  const msg = errorMessage(error);
  return (
    <div className="border border-destructive/40 bg-destructive/5 rounded-2xl p-6 flex flex-col items-center gap-2 text-center">
      <Icon className="w-8 h-8 text-destructive" />
      <h4 className="text-[13px] font-semibold">{title}</h4>
      <p className="text-[11px] text-muted-foreground">{msg}</p>
      {onRetry && (
        <Button size="sm" variant="outline" className="mt-1" onClick={onRetry}>
          {t("common.retry")}
        </Button>
      )}
    </div>
  );
}

function PaneSkeleton() {
  return <Skeleton className="h-40 rounded-2xl" />;
}

// ─────────────────────────────── DDoS Mitigation ───────────────────────────────

/** OVH MitigationStateEnum(ok / creationPending / removalPending) → 语言包 key */
const MITIGATION_STATE_KEYS: Record<string, string> = {
  ok: "maint.advanced.ddos.stateOk",
  creationPending: "maint.advanced.ddos.stateCreating",
  removalPending: "maint.advanced.ddos.stateRemoving",
};

function MitigationPane({ serviceName }: { serviceName: string }) {
  const { t } = useTranslation();
  const list = useMitigation(serviceName);
  const enable = useEnableMitigation(serviceName);
  const disable = useDisableMitigation(serviceName);

  if (list.isPending) return <PaneSkeleton />;

  const blocks = list.data || [];
  if (blocks.length === 0) {
    return <EmptyState icon={ShieldAlert} title={t("maint.advanced.ddos.noIp")} />;
  }

  const handleToggle = async (ip: string, block: string, currentlyActive: boolean) => {
    try {
      if (currentlyActive) {
        await disable.mutateAsync({ ip, block });
        toast.success(t("maint.advanced.ddos.toast.off"));
      } else {
        await enable.mutateAsync({ ip, block });
        toast.success(t("maint.advanced.ddos.toast.on"));
      }
    } catch (e: any) {
      // raw 只用于识别 OVH 的两类已知错误;展示一律走 errorMessage 翻译层
      const raw = String(e?.response?.data?.error || e?.message || "");
      if (/state need to be ok/i.test(raw)) {
        toast.error(t("maint.advanced.ddos.toast.stateNotOk"), { duration: 6000 });
      } else if (/is not valid for type ipv4/i.test(raw)) {
        toast.error(t("maint.advanced.ddos.toast.ipv6Only"), { duration: 6000 });
      } else {
        toast.error(errorMessage(e));
      }
    }
  };

  return (
    <div className="space-y-3">
      <p className="text-[11px] text-muted-foreground">
        {t("maint.advanced.ddos.intro")}
        <br />
        <span className="text-warning">{t("maint.advanced.ddos.ipv6Note")}</span>
      </p>
      {blocks.map((blk) => {
        const isV6 = blk.ipBlock.includes(":") && !blk.ipBlock.includes(".");
        return (
        <div key={blk.ipBlock} className="border border-border rounded-2xl overflow-hidden">
          <div className="px-3.5 py-2.5 border-b border-border bg-secondary/30 flex items-center gap-2">
            <ShieldAlert className="w-3.5 h-3.5 text-muted-foreground" />
            <code className="text-[12px] font-mono font-semibold">{blk.ipBlock}</code>
            {isV6 && <span className="text-[10px] text-muted-foreground ml-1">IPv6</span>}
            {blk.error && <span className="text-[11px] text-destructive ml-auto">{blk.error}</span>}
          </div>
          {isV6 ? (
            <div className="px-3.5 py-3 text-[12px] text-muted-foreground">
              {t("maint.advanced.ddos.ipv6Row")}
            </div>
          ) : blk.mitigations.length === 0 ? (
            <div className="px-3.5 py-3 text-[12px] text-muted-foreground flex items-center gap-2 flex-wrap">
              <span>{t("maint.advanced.ddos.noneRow")}</span>
              <Button
                size="sm"
                variant="outline"
                className="ml-auto h-7"
                onClick={() => {
                  const ip = blk.ipBlock.split("/")[0];
                  handleToggle(ip, blk.ipBlock, false);
                }}
                disabled={enable.isPending}
              >
                {t("maint.advanced.ddos.enableBtn")}
              </Button>
            </div>
          ) : (
            <div className="divide-y divide-border">
              {blk.mitigations.map((m) => {
                // OVH MitigationStateEnum 只有 creationPending / ok / removalPending
                const isOk = m.state === "ok";
                const isCreating = m.state === "creationPending";
                const isRemoving = m.state === "removalPending";
                // 详情没拉到的 IP 后端也会保留占位(只有 ipOnMitigation + error)。
                // 不标出来的话它会显示成一个没有状态的空行，用户以为是 bug。
                // 注：MitigationIp 类型里还没有 error 字段（在 hooks 层，本次不改），先就地读。
                const rowErr = (m as unknown as { error?: string }).error;
                const stateKey = MITIGATION_STATE_KEYS[m.state];
                return (
                  <div key={m.ipOnMitigation} className="px-3.5 py-2.5 flex items-center gap-2 text-[12px]">
                    <code className="font-mono">{m.ipOnMitigation}</code>
                    {rowErr ? (
                      <DetailErrorTag message={rowErr} />
                    ) : (
                    <Chip tone={mitigationTone(m.state)}>{stateKey ? t(stateKey) : m.state}</Chip>
                    )}
                    {m.auto && <span className="text-[11px] text-muted-foreground">{t("maint.advanced.ddos.auto")}</span>}
                    {m.permanent && <span className="text-[11px] text-success">{t("maint.advanced.ddos.permanent")}</span>}
                    <Button
                      size="sm"
                      variant="outline"
                      className="ml-auto h-7"
                      onClick={() => handleToggle(m.ipOnMitigation, blk.ipBlock, true)}
                      disabled={disable.isPending || !isOk || !!rowErr}
                      title={
                        isCreating
                          ? t("maint.advanced.ddos.creatingTitle")
                          : isRemoving
                            ? t("maint.advanced.ddos.removingTitle")
                            : ""
                      }
                    >
                      {isCreating ? t("maint.advanced.ddos.creating") : isRemoving ? t("maint.advanced.ddos.removing") : t("maint.advanced.ddos.closeBtn")}
                    </Button>
                  </div>
                );
              })}
            </div>
          )}
        </div>
        );
      })}
    </div>
  );
}

function mitigationTone(state: string): "success" | "warning" | "default" {
  if (state === "ok") return "success";
  if (state === "creationPending" || state === "removalPending") return "warning";
  return "default";
}
