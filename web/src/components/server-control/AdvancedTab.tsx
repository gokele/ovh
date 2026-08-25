import { useState } from "react";
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

/** 高级 Tab：旧前端的 9 个 sub-tab 全部接入 */
export function AdvancedTab({ server }: { server: OwnedServer }) {
  return (
    <Tabs defaultValue="burst" className="space-y-4">
      <TabsList className="grid grid-cols-3 sm:grid-cols-5 lg:flex lg:flex-wrap h-auto gap-1 p-1">
        <TabsTrigger value="burst" className="text-[11px] sm:text-[12px] px-2"><Zap className="w-3.5 h-3.5 mr-1" />Burst</TabsTrigger>
        <TabsTrigger value="firewall" className="text-[11px] sm:text-[12px] px-2"><Shield className="w-3.5 h-3.5 mr-1" />防火墙</TabsTrigger>
        <TabsTrigger value="ftp" className="text-[11px] sm:text-[12px] px-2"><FolderArchive className="w-3.5 h-3.5 mr-1" />FTP</TabsTrigger>
        <TabsTrigger value="dns" className="text-[11px] sm:text-[12px] px-2"><Globe className="w-3.5 h-3.5 mr-1" />二级 DNS</TabsTrigger>
        <TabsTrigger value="vmac" className="text-[11px] sm:text-[12px] px-2"><Wifi className="w-3.5 h-3.5 mr-1" />虚拟 MAC</TabsTrigger>
        <TabsTrigger value="vrack" className="text-[11px] sm:text-[12px] px-2"><Network className="w-3.5 h-3.5 mr-1" />vRack</TabsTrigger>
        <TabsTrigger value="orderable" className="text-[11px] sm:text-[12px] px-2"><ShoppingBag className="w-3.5 h-3.5 mr-1" />可订购</TabsTrigger>
        <TabsTrigger value="options" className="text-[11px] sm:text-[12px] px-2"><Settings className="w-3.5 h-3.5 mr-1" />附加</TabsTrigger>
        <TabsTrigger value="ip" className="text-[11px] sm:text-[12px] px-2"><MapPin className="w-3.5 h-3.5 mr-1" />IP 规格</TabsTrigger>
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
  const q = useServerBurst(serviceName);
  const mut = useSetBurst();
  if (q.isPending) return <PaneSkeleton />;
  const data: any = q.data;
  if (!data || data.notAvailable) return <NotAvailable icon={Zap} title="此服务器无 Burst 服务" message={data?.error} />;
  const burst = data.burst;
  if (!burst) return <EmptyState icon={Zap} title="暂无 Burst 信息" />;
  // 旧前端：burst.status 是字符串 "active" / "inactive"
  const active = burst.status === "active";
  const capacityText =
    burst.capacity && typeof burst.capacity === "object"
      ? `${burst.capacity.value} ${burst.capacity.unit || ""}`.trim()
      : null;
  return (
    <Pane title="Burst 流量" icon={Zap}>
      <Row label="状态" value={<Chip tone={active ? "success" : "default"}>{active ? "已启用" : "未启用"}</Chip>} />
      {capacityText && <Row label="容量" value={capacityText} />}
      <div className="pt-2">
        <Button
          variant="outline"
          size="sm"
          disabled={mut.isPending}
          onClick={async () => {
            const next = active ? "inactive" : "active";
            try {
              await mut.mutateAsync({ serviceName, status: next });
              toast.success(active ? "Burst 已停用" : "Burst 已启用");
            } catch (e: any) {
              toast.error(e?.response?.data?.error || "操作失败");
            }
          }}
        >
          <Power className="w-3.5 h-3.5 mr-1" />
          {active ? "停用 Burst" : "启用 Burst"}
        </Button>
      </div>
    </Pane>
  );
}

// ─────────────────────────────── Firewall ───────────────────────────────

function FirewallPane({ serviceName }: { serviceName: string }) {
  const q = useServerFirewall(serviceName);
  const mut = useSetFirewall();
  if (q.isPending) return <PaneSkeleton />;
  const data: any = q.data;
  if (!data || data.notAvailable) return <NotAvailable icon={Shield} title="此服务器无防火墙服务" message={data?.error} />;
  const fw = data.firewall;
  if (!fw) return <EmptyState icon={Shield} title="暂无防火墙信息" />;
  // 旧前端：直接读 firewall.enabled（boolean）
  const enabled = !!fw.enabled;
  return (
    <Pane title="防火墙" icon={Shield}>
      <Row label="状态" value={<Chip tone={enabled ? "success" : "default"}>{enabled ? "启用" : "停用"}</Chip>} />
      {fw.mode && <Row label="模式" value={String(fw.mode)} />}
      {fw.model && <Row label="型号" value={String(fw.model)} />}
      <div className="pt-2">
        <Button
          variant="outline"
          size="sm"
          disabled={mut.isPending}
          onClick={async () => {
            try {
              await mut.mutateAsync({ serviceName, enabled: !enabled });
              toast.success(enabled ? "防火墙已停用" : "防火墙已启用");
            } catch (e: any) {
              toast.error(e?.response?.data?.error || "操作失败");
            }
          }}
        >
          <Power className="w-3.5 h-3.5 mr-1" />
          {enabled ? "停用防火墙" : "启用防火墙"}
        </Button>
      </div>
    </Pane>
  );
}

// ─────────────────────────────── Backup FTP ───────────────────────────────

function BackupFtpPane({ serviceName }: { serviceName: string }) {
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
        title="美区账户不提供备份FTP"
        message="OVHcloud US 没有 dedicated/server/backupFTP 系列接口，请在 OVHcloud US 控制台使用其它备份方案。"
      />
    );
  }
  if (q.isPending) return <PaneSkeleton />;
  const data = q.data;
  if (!data) return <EmptyState icon={FolderArchive} title="暂无 Backup FTP 数据" />;
  // unknownService = 切错账户（这台机器不属于当前账户）。此时给「激活」按钮等于给一个必然
  // 失败的按钮，所以只显示原因，并提示去切账户
  if (data.unknownService) {
    return (
      <NotAvailable
        icon={FolderArchive}
        title="服务器不存在或不属于当前账户"
        message={data.reason ? `${data.error || ""}（OVH 原文：${data.reason}）` : data.error}
      />
    );
  }
  if (data.notAvailable) return <NotAvailable icon={FolderArchive} title="此服务器无 Backup FTP" message={data.error} />;
  if (data.notActivated) {
    return (
      <Pane title="Backup FTP" icon={FolderArchive}>
        <p className="text-[12px] text-muted-foreground">尚未激活 Backup FTP 服务。激活后可获得用于离线备份的 FTP / NFS / CIFS 存储。</p>
        <div className="pt-3">
          <Button
            size="sm"
            disabled={act.isPending}
            onClick={async () => {
              try {
                await act.mutateAsync(serviceName);
                toast.success("激活请求已发送");
              } catch (e: any) {
                toast.error(e?.response?.data?.error || "激活失败");
              }
            }}
          >
            {act.isPending ? "激活中…" : "激活 Backup FTP"}
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
      toast.error("请填写要授权的 IP 段（CIDR，如 1.2.3.4/32）");
      return;
    }
    try {
      await addAccess.mutateAsync({ serviceName, ipBlock, ftp: true, nfs, cifs });
      toast.success("授权已提交，OVH 会异步生效");
      setNewBlock("");
      setNfs(false);
      setCifs(false);
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "添加授权失败");
    }
  };

  return (
    <Pane title="Backup FTP" icon={FolderArchive}>
      {quotaText && <Row label="配额" value={quotaText} />}
      {usageText && <Row label="已用" value={usageText} />}
      {ftp.ftpBackupName && <Row label="服务器地址" value={<code className="font-mono text-[12px]">{ftp.ftpBackupName}</code>} />}

      <div className="flex flex-wrap gap-2 pt-2">
        <Button
          size="sm"
          variant="outline"
          disabled={resetPwd.isPending}
          onClick={async () => {
            try {
              await resetPwd.mutateAsync(serviceName);
              toast.success("已请求重置密码，新密码由 OVH 发送到账户邮箱");
            } catch (e: any) {
              toast.error(e?.response?.data?.error || "重置密码失败");
            }
          }}
        >
          <KeyRound className="w-3.5 h-3.5 mr-1" />
          {resetPwd.isPending ? "提交中…" : "重置密码"}
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="text-destructive"
          disabled={del.isPending}
          onClick={async () => {
            // 关停会连带删掉里面已有的备份，属于不可逆操作，必须先要用户明确同意
            if (!window.confirm("关闭 Backup FTP 会删除其中已有的备份数据，且不可恢复。确认关闭？")) return;
            try {
              await del.mutateAsync(serviceName);
              toast.success("关闭请求已提交");
            } catch (e: any) {
              toast.error(e?.response?.data?.error || "关闭失败");
            }
          }}
        >
          <Trash2 className="w-3.5 h-3.5 mr-1" />
          {del.isPending ? "提交中…" : "关闭服务"}
        </Button>
      </div>

      <div className="pt-3 space-y-2">
        <h4 className="text-[12px] font-semibold">访问控制列表（允许的 IP 块）</h4>
        {/* 列表整体没拉到 ≠ 没配过 IP，必须说清楚，否则用户会重复添加已有的授权 */}
        {data.accessError && (
          <div className="flex items-start gap-2 rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2 text-[11px]">
            <AlertCircle className="w-3.5 h-3.5 text-destructive flex-shrink-0 mt-0.5" />
            <span>访问控制列表获取失败：{data.accessError}。下面的「尚未配置」只代表这次没查到。</span>
          </div>
        )}
        <PartialNotice failedCount={data.accessFailedCount || 0} what="访问控制列表" />

        {/* 添加授权 IP */}
        <div className="flex flex-wrap items-center gap-2">
          <Input
            value={newBlock}
            onChange={(e) => setNewBlock(e.target.value)}
            placeholder="1.2.3.4/32 或从下方候选选择"
            className="h-8 w-56 font-mono text-[12px]"
          />
          <label className="flex items-center gap-1 text-[11px] cursor-pointer">
            <input type="checkbox" checked={nfs} onChange={(e) => setNfs(e.target.checked)} className="w-3.5 h-3.5" />
            NFS
          </label>
          <label className="flex items-center gap-1 text-[11px] cursor-pointer">
            <input type="checkbox" checked={cifs} onChange={(e) => setCifs(e.target.checked)} className="w-3.5 h-3.5" />
            CIFS
          </label>
          <Button size="sm" variant="outline" className="h-8" onClick={handleAdd} disabled={addAccess.isPending}>
            <Plus className="w-3.5 h-3.5 mr-1" />
            {addAccess.isPending ? "提交中…" : "授权"}
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
          <p className="text-[12px] text-muted-foreground">尚未配置访问 IP。</p>
        ) : (
          <div className="border border-border rounded-2xl divide-y divide-border">
            {accessList.map((a, idx) => (
              <div key={a.ipBlock || idx} className="px-4 py-2.5 text-[13px] flex items-center gap-2 flex-wrap">
                <code className="font-mono">{a.ipBlock}</code>
                {a.ftp && <Chip tone="default">FTP</Chip>}
                {a.nfs && <Chip tone="default">NFS</Chip>}
                {a.cifs && <Chip tone="default">CIFS</Chip>}
                {a.isApplied === false && <Chip tone="warning">生效中</Chip>}
                {/* error 现在是 OVH 原始 message，直接透出来比 "fetch failed" 有用得多 */}
                <DetailErrorTag message={a.error} />
                <Button
                  size="sm"
                  variant="outline"
                  className="ml-auto h-7 px-2 text-destructive"
                  disabled={delAccess.isPending}
                  onClick={async () => {
                    if (!window.confirm(`删除对 ${a.ipBlock} 的备份FTP授权？`)) return;
                    try {
                      await delAccess.mutateAsync({ serviceName, ipBlock: a.ipBlock });
                      toast.success("已提交删除授权");
                    } catch (e: any) {
                      toast.error(e?.response?.data?.error || "删除授权失败");
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
  const q = useServerSecondaryDns(serviceName);
  if (q.isPending) return <PaneSkeleton />;
  // 后端全挂时返 500 —— 显示错误而不是空列表，否则用户会以为「确实没配过」
  if (q.isError) return <LoadFailed icon={Globe} title="二级 DNS 读取失败" error={q.error} onRetry={() => q.refetch()} />;
  const { items, failedCount } = q.data || { items: [], partial: false, failedCount: 0 };
  if (items.length === 0) return <EmptyState icon={Globe} title="未配置二级 DNS" />;
  return (
    <Pane title="二级 DNS 域名" icon={Globe}>
      <PartialNotice failedCount={failedCount} what="二级 DNS 域名" className="mb-2" />
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
  const q = useServerVirtualMac(serviceName);
  if (q.isPending) return <PaneSkeleton />;
  if (q.isError) return <LoadFailed icon={Wifi} title="虚拟 MAC 读取失败" error={q.error} onRetry={() => q.refetch()} />;
  const { items, failedCount } = q.data || { items: [], partial: false, failedCount: 0 };
  if (items.length === 0) return <EmptyState icon={Wifi} title="未分配虚拟 MAC" />;
  return (
    <Pane title="虚拟 MAC（VMware / Hyper-V 等使用）" icon={Wifi}>
      <PartialNotice failedCount={failedCount} what="虚拟 MAC" className="mb-2" />
      <div className="border border-border rounded-2xl divide-y divide-border">
        {items.map((m, idx) => (
          <div key={m.macAddress || idx} className="px-3 sm:px-4 py-2.5 sm:py-3 grid grid-cols-1 sm:grid-cols-3 gap-1 sm:gap-2 sm:items-center text-[12px] sm:text-[13px]">
            <code className="font-mono break-all">{m.macAddress || "—"}</code>
            <span className="text-muted-foreground flex items-center gap-2">
              {m.type || "—"}
              <DetailErrorTag message={m._detailError} />
            </span>
            <code className="font-mono text-muted-foreground sm:text-right break-all">{m.ipAddress || "—"}</code>
          </div>
        ))}
      </div>
    </Pane>
  );
}

// ─────────────────────────────── vRack ───────────────────────────────

function VrackPane({ serviceName }: { serviceName: string }) {
  const q = useServerVrack(serviceName);
  if (q.isPending) return <PaneSkeleton />;
  if (q.isError) return <LoadFailed icon={Network} title="vRack 读取失败" error={q.error} onRetry={() => q.refetch()} />;
  const { items, failedCount } = q.data || { items: [], partial: false, failedCount: 0 };
  if (items.length === 0) return <EmptyState icon={Network} title="未加入 vRack 私有网络" />;
  return (
    <Pane title="vRack 私有网络成员" icon={Network}>
      <PartialNotice failedCount={failedCount} what="vRack" className="mb-2" />
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
  const q = useServerOrderable(serviceName);
  if (q.isPending) return <PaneSkeleton />;
  const data = q.data;
  if (!data || (!data.bandwidth && !data.traffic && !data.ip)) {
    return <EmptyState icon={ShoppingBag} title="暂无可订购服务" />;
  }

  return (
    <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
      {/* 带宽 */}
      <Pane title="带宽升级" icon={ShoppingBag} compact>
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
      <Pane title="流量升级" icon={ShoppingBag} compact>
        {data.traffic ? (
          data.traffic.orderable ? (
            <TierLine name="可用套餐" count={data.traffic.traffic?.length || 0} />
          ) : (
            <NotOrderable />
          )
        ) : (
          <NotOrderable />
        )}
      </Pane>

      {/* IP 块 */}
      <Pane title="IP 块" icon={ShoppingBag} compact>
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
  return (
    <div className="flex items-center justify-between text-[12px]">
      <span className={count > 0 ? "font-semibold" : "text-muted-foreground"}>{name}</span>
      <span className={count > 0 ? "font-mono" : "text-muted-foreground font-mono"}>
        {count > 0 ? `${count} 个套餐` : "—"}
      </span>
    </div>
  );
}

function NotOrderable() {
  return <p className="text-[12px] text-muted-foreground">不可订购</p>;
}

// ─────────────────────────────── IP Specs / IP 块通用 ───────────────────────────────

function IpBlockList({ ipv4, ipv6 }: { ipv4?: any[]; ipv6?: any[] }) {
  const has4 = ipv4 && ipv4.length > 0;
  const has6 = ipv6 && ipv6.length > 0;
  if (!has4 && !has6) {
    return <p className="text-[12px] text-muted-foreground">无可用 IP 选项</p>;
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
  const typeLabel = ip.type === "failover" ? "故障转移" : ip.type === "static" ? "静态" : ip.type || `IP${family}`;
  return (
    <div className="border border-border rounded-xl p-2.5 text-[12px] space-y-1 bg-background">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <span className="font-semibold">{typeLabel}</span>
        {ip.included && <Chip tone="success">已包含</Chip>}
        {ip.optionRequired && <Chip tone="warning">需选项</Chip>}
      </div>
      {ip.blockSizes && ip.blockSizes.length > 0 && (
        <div className="text-muted-foreground">
          可用块：<code className="font-mono">{ip.blockSizes.join(", ")}</code>
        </div>
      )}
      {ip.ipNumber != null && (
        <div className="text-muted-foreground">
          IP 数量：<span className="text-foreground">{ip.ipNumber}</span>
          {ip.number != null && <span className="ml-2">数量：<span className="text-foreground">{ip.number}</span></span>}
        </div>
      )}
      {ip.optionRequired && (
        <div className="text-warning text-[11px]">需要选项：{ip.optionRequired}</div>
      )}
    </div>
  );
}

// ─────────────────────────────── Options ───────────────────────────────

function OptionsPane({ serviceName }: { serviceName: string }) {
  const q = useServerOptions(serviceName);
  if (q.isPending) return <PaneSkeleton />;
  if (q.isError) return <LoadFailed icon={Settings} title="附加选项读取失败" error={q.error} onRetry={() => q.refetch()} />;
  const { items, failedCount } = q.data || { items: [], partial: false, failedCount: 0 };
  if (items.length === 0) return <EmptyState icon={Settings} title="无附加选项" />;

  // 名称翻译（对齐旧前端 optionNames 表）
  const NAMES: Record<string, string> = {
    BANDWIDTH: "带宽",
    TRAFFIC: "流量",
    BACKUP_STORAGE: "备份存储",
    HARD_RAID: "硬件 RAID",
    SLA: "SLA",
    SYSTEM_STORAGE: "系统存储",
    MEMORY: "内存",
    CPU: "CPU",
    PRIVATE_BANDWIDTH: "私有带宽",
  };
  // OVH state 取值：subscribed / released / releasing / toDelete
  const stateTone = (state: string): "success" | "warning" | "default" => {
    const s = state?.toLowerCase();
    if (s === "subscribed") return "success";
    if (s === "releasing" || s === "todelete") return "warning";
    return "default";
  };

  return (
    <Pane title="附加选项" icon={Settings}>
      <PartialNotice failedCount={failedCount} what="附加选项" className="mb-2" />
      <div className="border border-border rounded-2xl divide-y divide-border">
        {items.map((o, idx) => {
          const key = o.option || `选项 ${idx + 1}`;
          const label = NAMES[String(key).toUpperCase()] || key;
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
  const q = useServerIpSpecs(serviceName);
  if (q.isPending) return <PaneSkeleton />;
  const data = q.data;
  if (!data) return <EmptyState icon={MapPin} title="无 IP 详细规格" />;
  const ipv4 = Array.isArray(data.ipv4) ? data.ipv4 : [];
  const ipv6 = Array.isArray(data.ipv6) ? data.ipv6 : [];
  if (ipv4.length === 0 && ipv6.length === 0) return <EmptyState icon={MapPin} title="暂无 IP 规格信息" />;
  return (
    <Pane title="IP 规格" icon={MapPin}>
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
  return (
    <div className="border border-border rounded-2xl p-6 flex flex-col items-center gap-2 text-center">
      <Icon className="w-8 h-8 text-muted-foreground" />
      <h4 className="text-[13px] font-semibold">{title}</h4>
      {message ? (
        <p className="text-[11px] text-muted-foreground">{message}</p>
      ) : (
        <p className="text-[11px] text-muted-foreground flex items-center gap-1">
          <AlertCircle className="w-3 h-3" />
          OVH 后端对该服务器未启用此功能
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
  const msg =
    (error as any)?.response?.data?.error || (error as any)?.message || "请稍后重试";
  return (
    <div className="border border-destructive/40 bg-destructive/5 rounded-2xl p-6 flex flex-col items-center gap-2 text-center">
      <Icon className="w-8 h-8 text-destructive" />
      <h4 className="text-[13px] font-semibold">{title}</h4>
      <p className="text-[11px] text-muted-foreground">{msg}</p>
      {onRetry && (
        <Button size="sm" variant="outline" className="mt-1" onClick={onRetry}>
          重试
        </Button>
      )}
    </div>
  );
}

function PaneSkeleton() {
  return <Skeleton className="h-40 rounded-2xl" />;
}

// ─────────────────────────────── DDoS Mitigation ───────────────────────────────

function MitigationPane({ serviceName }: { serviceName: string }) {
  const list = useMitigation(serviceName);
  const enable = useEnableMitigation(serviceName);
  const disable = useDisableMitigation(serviceName);

  if (list.isPending) return <PaneSkeleton />;

  const blocks = list.data || [];
  if (blocks.length === 0) {
    return <EmptyState icon={ShieldAlert} title="该服务器无 IP" />;
  }

  const handleToggle = async (ip: string, block: string, currentlyActive: boolean) => {
    try {
      if (currentlyActive) {
        await disable.mutateAsync({ ip, block });
        toast.success("已关闭永久 DDoS 缓解");
      } else {
        await enable.mutateAsync({ ip, block });
        toast.success("已启用永久 DDoS 缓解");
      }
    } catch (e: any) {
      const raw = String(e?.response?.data?.error || e?.message || "");
      if (/state need to be ok/i.test(raw)) {
        toast.error("当前 mitigation 状态不允许关闭(可能正在被自动启用或攻击中)。等状态变 ok 再试", { duration: 6000 });
      } else if (/is not valid for type ipv4/i.test(raw)) {
        toast.error("OVH anti-DDoS 只支持 IPv4。IPv6 默认有网络层防护,无需手动配置", { duration: 6000 });
      } else {
        toast.error(raw || "操作失败");
      }
    }
  };

  return (
    <div className="space-y-3">
      <p className="text-[11px] text-muted-foreground">
        OVH 自带「自动缓解」会在检测到攻击时自动启用,无需配置。下面是手动启用「永久缓解」的开关:开启后该 IP 全程过 Anti-DDoS 设备(延迟略增,持续防护)。
        <br />
        <span className="text-amber-600 dark:text-amber-400">仅支持 IPv4。IPv6 走 OVH 网络层默认免疫,无需手动配置。</span>
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
              IPv6 不适用 anti-DDoS Mitigation(OVH 网络层免疫)
            </div>
          ) : blk.mitigations.length === 0 ? (
            <div className="px-3.5 py-3 text-[12px] text-muted-foreground flex items-center gap-2 flex-wrap">
              <span>无永久缓解,自动缓解备用中</span>
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
                启用永久缓解
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
                return (
                  <div key={m.ipOnMitigation} className="px-3.5 py-2.5 flex items-center gap-2 text-[12px]">
                    <code className="font-mono">{m.ipOnMitigation}</code>
                    {rowErr ? (
                      <DetailErrorTag message={rowErr} />
                    ) : (
                    <Chip tone={mitigationTone(m.state)}>{stateText(m.state)}</Chip>
                    )}
                    {m.auto && <span className="text-[11px] text-muted-foreground">自动</span>}
                    {m.permanent && <span className="text-[11px] text-emerald-600 dark:text-emerald-400">永久</span>}
                    <Button
                      size="sm"
                      variant="outline"
                      className="ml-auto h-7"
                      onClick={() => handleToggle(m.ipOnMitigation, blk.ipBlock, true)}
                      disabled={disable.isPending || !isOk || !!rowErr}
                      title={
                        isCreating
                          ? "正在启用中,通常 30 秒-2 分钟,等状态变已生效再点关闭"
                          : isRemoving
                            ? "正在移除中,稍后会自动从列表消失"
                            : ""
                      }
                    >
                      {isCreating ? "应用中…" : isRemoving ? "移除中…" : "关闭永久"}
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

function stateText(state: string): string {
  return ({
    ok: "已生效",
    creationPending: "应用中",
    removalPending: "移除中",
  } as Record<string, string>)[state] || state;
}
