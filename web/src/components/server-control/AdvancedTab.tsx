import {
  Zap, Shield, FolderArchive, Globe, Wifi, Network, ShoppingBag, Settings, MapPin,
  Power, AlertCircle,
} from "lucide-react";
import type { OwnedServer } from "@/hooks/use-server-control";
import {
  useServerBurst, useSetBurst,
  useServerFirewall, useSetFirewall,
  useServerBackupFtp, useActivateBackupFtp,
  useServerSecondaryDns,
  useServerVirtualMac,
  useServerVrack,
  useServerOrderable,
  useServerOptions,
  useServerIpSpecs,
} from "@/hooks/use-server-control";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Chip } from "@/components/common/Chip";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
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
  const q = useServerBackupFtp(serviceName);
  const act = useActivateBackupFtp();
  if (q.isPending) return <PaneSkeleton />;
  const data: any = q.data;
  if (!data) return <EmptyState icon={FolderArchive} title="暂无 Backup FTP 数据" />;
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
  const ftp = data.backupFtp || {};
  const accessList: any[] = data.accessList || [];
  // 旧前端：quota 和 usage 都是 { value, unit } 对象
  const quotaText =
    ftp.quota && typeof ftp.quota === "object" ? `${ftp.quota.value} ${ftp.quota.unit || ""}`.trim() : null;
  const usageText =
    ftp.usage && typeof ftp.usage === "object" ? `${ftp.usage.value} ${ftp.usage.unit || ""}`.trim() : null;
  return (
    <Pane title="Backup FTP" icon={FolderArchive}>
      {quotaText && <Row label="配额" value={quotaText} />}
      {usageText && <Row label="已用" value={usageText} />}

      <div className="pt-3">
        <h4 className="text-[12px] font-semibold mb-2">访问控制列表（允许的 IP 块）</h4>
        {accessList.length === 0 ? (
          <p className="text-[12px] text-muted-foreground">尚未配置访问 IP。</p>
        ) : (
          <div className="border border-border rounded-2xl divide-y divide-border">
            {accessList.map((a, idx) => (
              <div key={idx} className="px-4 py-2.5 text-[13px]">
                <code className="font-mono">{a.ipBlock}</code>
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
  const data = q.data || [];
  if (data.length === 0) return <EmptyState icon={Globe} title="未配置二级 DNS" />;
  return (
    <Pane title="二级 DNS 域名" icon={Globe}>
      <div className="border border-border rounded-2xl divide-y divide-border">
        {data.map((d: any, idx: number) => (
          <div key={idx} className="px-4 py-3 grid grid-cols-2 gap-2 items-center text-[13px]">
            <code className="font-mono">{d.domain || "—"}</code>
            {d.dns && <code className="font-mono text-muted-foreground text-right text-[12px]">{d.dns}</code>}
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
  const data = q.data || [];
  if (data.length === 0) return <EmptyState icon={Wifi} title="未分配虚拟 MAC" />;
  return (
    <Pane title="虚拟 MAC（VMware / Hyper-V 等使用）" icon={Wifi}>
      <div className="border border-border rounded-2xl divide-y divide-border">
        {data.map((m: any, idx: number) => (
          <div key={idx} className="px-3 sm:px-4 py-2.5 sm:py-3 grid grid-cols-1 sm:grid-cols-3 gap-1 sm:gap-2 sm:items-center text-[12px] sm:text-[13px]">
            <code className="font-mono break-all">{m.macAddress || "—"}</code>
            <span className="text-muted-foreground">{m.type || "—"}</span>
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
  const data = q.data || [];
  if (data.length === 0) return <EmptyState icon={Network} title="未加入 vRack 私有网络" />;
  return (
    <Pane title="vRack 私有网络成员" icon={Network}>
      <div className="border border-border rounded-2xl divide-y divide-border">
        {data.map((v: any, idx: number) => (
          <div key={idx} className="px-4 py-2.5 text-[13px] font-mono">{v.vrackName || "—"}</div>
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
  const data = q.data || [];
  if (data.length === 0) return <EmptyState icon={Settings} title="无附加选项" />;

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
      <div className="border border-border rounded-2xl divide-y divide-border">
        {data.map((o: any, idx: number) => {
          const key = o.option || `选项 ${idx + 1}`;
          const label = NAMES[String(key).toUpperCase()] || key;
          return (
            <div key={idx} className="px-4 py-2.5 flex items-center justify-between text-[13px] gap-3">
              <span className="font-semibold truncate">{label}</span>
              <div className="text-right">
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

function PaneSkeleton() {
  return <Skeleton className="h-40 rounded-2xl" />;
}
