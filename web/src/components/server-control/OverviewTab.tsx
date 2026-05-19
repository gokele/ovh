import { Cpu, HardDrive, MemoryStick, MapPin, Globe, Wifi } from "lucide-react";
import type { OwnedServer } from "@/hooks/use-server-control";
import { useServerHardware, useServerIps, useServerNetworkInterfaces } from "@/hooks/use-server-control";
import { useHideIp, maskSensitive } from "@/hooks/use-hide-ip";
import { Skeleton } from "@/components/common/Skeleton";
import { MrtgTrafficChart } from "./MrtgTrafficChart";

/** 概览 Tab：硬件 + 网络（IP / 接口 / MRTG 流量）。服务信息胶囊条已上提到 ServerTabs 同行 */
export function OverviewTab({ server }: { server: OwnedServer }) {
  const hw = useServerHardware(server.serviceName);
  const ips = useServerIps(server.serviceName);
  const interfaces = useServerNetworkInterfaces(server.serviceName);
  const { hidden } = useHideIp();

  // 内存字段是 { value, unit } 对象
  const memText = hw.data?.memorySize
    ? `${hw.data.memorySize.value} ${hw.data.memorySize.unit}`
    : "—";

  // CPU 字段：processorName + 核线（旧前端写法照搬）
  const cpuText = hw.data?.processorName
    ? hw.data.coresPerProcessor && hw.data.threadsPerProcessor
      ? `${hw.data.processorName} (${hw.data.coresPerProcessor}核/${hw.data.threadsPerProcessor}线程)`
      : hw.data.processorName
    : "—";

  // 磁盘：把所有 diskGroups 拼成 "N × Type Size" / "N × Type Size" 多组用 / 分隔
  const diskText =
    hw.data?.diskGroups && hw.data.diskGroups.length > 0
      ? hw.data.diskGroups
          .map((g: any) => {
            const count = g.numberOfDisks ?? 1;
            const type = g.diskType ?? "";
            const size = g.diskSize ? `${g.diskSize.value} ${g.diskSize.unit}` : "";
            return [`${count} × ${type}`, size].filter(Boolean).join(" ");
          })
          .join(" / ")
      : "—";

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 md:grid-cols-4 gap-2.5 sm:gap-3">
        <InfoCard icon={<Cpu className="w-4 h-4" />} label="处理器" value={cpuText} loading={hw.isPending} />
        <InfoCard icon={<MemoryStick className="w-4 h-4" />} label="内存" value={memText} loading={hw.isPending} />
        <InfoCard icon={<HardDrive className="w-4 h-4" />} label="磁盘" value={diskText} loading={hw.isPending} />
        <InfoCard icon={<MapPin className="w-4 h-4" />} label="数据中心" value={server.datacenter.toUpperCase()} />
      </div>

      {/* 网络：IP 列表 + 接口 + MRTG 流量 */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 sm:gap-4">
        {/* IP 列表 */}
        <div className="border border-border rounded-2xl overflow-hidden">
          <div className="px-4 py-3 border-b border-border flex items-center gap-2">
            <Globe className="w-4 h-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">IP 地址</h3>
          </div>
          {ips.isPending ? (
            <div className="p-4">
              <Skeleton className="h-20 rounded-md" />
            </div>
          ) : (
            <div className="divide-y divide-border">
              {(ips.data && ips.data.length > 0 ? ips.data : [{ ip: server.ip, type: "IPv4" }]).map((entry) => (
                <div key={entry.ip} className="px-4 py-3 flex items-center justify-between text-[13px]">
                  <code className="font-mono">{maskSensitive(entry.ip, hidden)}</code>
                  <span className="text-[11px] text-muted-foreground">{entry.type}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* 网卡接口 */}
        <div className="border border-border rounded-2xl overflow-hidden">
          <div className="px-4 py-3 border-b border-border flex items-center gap-2">
            <Wifi className="w-4 h-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">网卡接口</h3>
          </div>
          {interfaces.isPending ? (
            <div className="p-4">
              <Skeleton className="h-20 rounded-md" />
            </div>
          ) : (interfaces.data || []).length === 0 ? (
            <p className="px-4 py-6 text-sm text-muted-foreground text-center">未发现网卡</p>
          ) : (
            <div className="divide-y divide-border">
              {(interfaces.data || []).map((nic: any) => (
                <div key={nic.mac} className="px-4 py-3 flex items-center justify-between text-[13px]">
                  <code className="font-mono">{nic.mac}</code>
                  <span className="text-[11px] text-muted-foreground">{nic.linkType || "—"}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* MRTG 流量监控 */}
      <MrtgTrafficChart serviceName={server.serviceName} />
    </div>
  );
}

function InfoCard({
  icon,
  label,
  value,
  loading,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  loading?: boolean;
}) {
  return (
    <div className="border border-border rounded-xl px-3.5 py-3 flex items-center gap-3 min-w-0">
      <div className="w-9 h-9 rounded-lg bg-secondary flex items-center justify-center flex-shrink-0">{icon}</div>
      <div className="min-w-0">
        <div className="text-[11px] text-muted-foreground">{label}</div>
        {loading ? (
          <Skeleton className="h-4 w-24 mt-1" />
        ) : (
          <div className="text-[13px] font-semibold truncate" title={value}>
            {value}
          </div>
        )}
      </div>
    </div>
  );
}

