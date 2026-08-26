import { Network, RefreshCw, ArrowUp, ArrowDown, ArrowLeftRight, Cable } from "lucide-react";
import { maskSensitive, useHideIp } from "@/hooks/use-hide-ip";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { Chip } from "@/components/common/Chip";
import { useServerNetworkSpecs } from "@/hooks/use-server-control";

/**
 * Network specs：完全按 OVH 实际字段渲染
 * - bandwidth: OvhToInternet / InternetToOvh / OvhToOvh
 * - connection: 端口速率
 * - routing: ipv4 / ipv6（gateway / ip / network）
 * - switching: name
 * - traffic: quota / used / isThrottled
 * - vmac: quota / supported
 * - vrack: bandwidth / type
 * - ola: available / supportedModes
 */
export function NetworkSpecsDialog({
  serviceName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const { hidden } = useHideIp();
  // 路由信息里有 CIDR 和 IPv6,maskSensitive 的精确正则接不住,统一宽松打码:
  // 保留首段,其余遮掉。开关关着时原样返回。
  const maskAny = (v?: string) => {
    if (!v) return v ?? "";
    if (!hidden) return v;
    const m4 = v.match(/^(\d{1,3})\./);
    if (m4) return `${m4[1]}.***`;
    const i = v.indexOf(":");
    if (i > 0) return v.slice(0, i) + ":****";
    return maskSensitive(v, hidden);
  };
  const q = useServerNetworkSpecs(serviceName, open);
  const data = q.data;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-3xl max-h-[85vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Network className="w-5 h-5" />
            网络规格
          </DialogTitle>
          <DialogDescription>带宽 / 端口速率 / IPv4-v6 路由 / 流量配额 / 交换机 / vMAC / vRack。</DialogDescription>
        </DialogHeader>

        <div className="overflow-y-auto -mx-6 px-6 space-y-4 flex-1">
          {q.isPending ? (
            <Skeleton className="h-60 rounded-2xl" />
          ) : !data ? (
            <EmptyState icon={Network} title="无网络规格数据" />
          ) : (
            <>
              {/* 带宽四档 */}
              <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5">
                <BwCard
                  icon={<ArrowUp className="w-3.5 h-3.5" />}
                  label="出向 (OVH → 互联网)"
                  value={fmtBandwidth(data.bandwidth?.OvhToInternet)}
                />
                <BwCard
                  icon={<ArrowDown className="w-3.5 h-3.5" />}
                  label="入向 (互联网 → OVH)"
                  value={fmtBandwidth(data.bandwidth?.InternetToOvh)}
                />
                <BwCard
                  icon={<ArrowLeftRight className="w-3.5 h-3.5" />}
                  label="内部 (OVH → OVH)"
                  value={fmtBandwidth(data.bandwidth?.OvhToOvh)}
                />
                <BwCard
                  icon={<Cable className="w-3.5 h-3.5" />}
                  label="端口速率"
                  value={fmtBandwidth(data.connection)}
                />
              </div>

              {/* 带宽类型 */}
              {data.bandwidth?.type && (
                <p className="text-[11px] text-muted-foreground">
                  带宽类型：<span className="font-mono">{data.bandwidth.type}</span>
                </p>
              )}

              {/* 路由 IPv4 */}
              {data.routing?.ipv4 && (
                <div className="border border-border rounded-2xl overflow-hidden">
                  <div className="px-4 py-2 bg-secondary/50 text-[12px] font-semibold">IPv4 路由</div>
                  <KvRows
                    rows={[
                      ["IP 地址", maskAny(data.routing.ipv4.ip)],
                      ["网关", maskAny(data.routing.ipv4.gateway)],
                      ["网段", maskAny(data.routing.ipv4.network)],
                    ]}
                  />
                </div>
              )}

              {/* 路由 IPv6 */}
              {data.routing?.ipv6 && (
                <div className="border border-border rounded-2xl overflow-hidden">
                  <div className="px-4 py-2 bg-secondary/50 text-[12px] font-semibold">IPv6 路由</div>
                  <KvRows
                    rows={[
                      ["IP 地址", maskAny(data.routing.ipv6.ip)],
                      ["网关", maskAny(data.routing.ipv6.gateway)],
                      ["网段", maskAny(data.routing.ipv6.network)],
                    ]}
                  />
                </div>
              )}

              {/* 交换机 / vMAC / vRack / 流量 / OLA */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                {data.switching?.name && (
                  <InfoBlock title="交换机">
                    <code className="font-mono text-[12px]">{data.switching.name}</code>
                  </InfoBlock>
                )}

                {data.vmac && (
                  <InfoBlock title="虚拟 MAC (vMAC)">
                    <div className="flex items-center gap-2 text-[13px]">
                      <Chip tone={data.vmac.supported ? "success" : "default"}>
                        {data.vmac.supported ? "支持" : "不支持"}
                      </Chip>
                      {data.vmac.quota != null && (
                        <span className="text-muted-foreground">配额：{data.vmac.quota}</span>
                      )}
                    </div>
                  </InfoBlock>
                )}

                {data.vrack && (data.vrack.bandwidth != null || data.vrack.type) && (
                  <InfoBlock title="vRack 私有网络">
                    <div className="text-[13px] space-y-0.5">
                      {data.vrack.type && (
                        <div>
                          <span className="text-muted-foreground">类型：</span>
                          <span className="font-mono">{data.vrack.type}</span>
                        </div>
                      )}
                      {data.vrack.bandwidth != null && (
                        <div>
                          <span className="text-muted-foreground">带宽：</span>
                          <span className="font-mono">{fmtBandwidth(data.vrack.bandwidth)}</span>
                        </div>
                      )}
                    </div>
                  </InfoBlock>
                )}

                {data.traffic && (
                  <InfoBlock title="流量配额">
                    <div className="text-[13px] space-y-0.5">
                      <Row label="入向配额" value={data.traffic.inputQuotaSize ? fmtBytes(data.traffic.inputQuotaSize) : "无限"} />
                      <Row label="出向配额" value={data.traffic.outputQuotaSize ? fmtBytes(data.traffic.outputQuotaSize) : "无限"} />
                      <Row
                        label="限速状态"
                        value={
                          <Chip tone={data.traffic.isThrottled ? "warning" : "success"}>
                            {data.traffic.isThrottled ? "已限速" : "正常"}
                          </Chip>
                        }
                      />
                      {data.traffic.resetQuotaDate && (
                        <Row label="重置日期" value={new Date(data.traffic.resetQuotaDate).toLocaleDateString("zh-CN")} />
                      )}
                    </div>
                  </InfoBlock>
                )}

                {data.ola && (
                  <InfoBlock title="OLA (OVH Link Aggregation)">
                    <div className="text-[13px] space-y-0.5">
                      <Chip tone={data.ola.available ? "success" : "default"}>
                        {data.ola.available ? "可用" : "不可用"}
                      </Chip>
                      {Array.isArray(data.ola.supportedModes) && data.ola.supportedModes.length > 0 && (
                        <p className="text-[11px] text-muted-foreground">
                          支持模式：{data.ola.supportedModes.join(", ")}
                        </p>
                      )}
                    </div>
                  </InfoBlock>
                )}
              </div>
            </>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => q.refetch()} disabled={q.isFetching}>
            <RefreshCw className={`w-3.5 h-3.5 mr-1 ${q.isFetching ? "animate-spin" : ""}`} />
            刷新
          </Button>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function BwCard({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="border border-border rounded-xl px-3 py-2.5">
      <div className="flex items-center gap-1 text-[11px] text-muted-foreground mb-0.5">
        {icon}
        <span className="truncate">{label}</span>
      </div>
      <div className="text-[13px] font-semibold">{value}</div>
    </div>
  );
}

function InfoBlock({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="border border-border rounded-2xl p-4">
      <h4 className="text-[12px] font-semibold mb-2">{title}</h4>
      {children}
    </div>
  );
}

function KvRows({ rows }: { rows: [string, React.ReactNode][] }) {
  return (
    <table className="w-full text-[13px]">
      <tbody>
        {rows.map(([k, v], idx) => (
          <tr key={idx} className="border-t border-border first:border-t-0">
            <td className="py-2 px-4 text-muted-foreground w-1/4">{k}</td>
            <td className="py-2 px-4 font-mono break-all text-[12px]">{v ?? "—"}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex justify-between items-center gap-3">
      <span className="text-muted-foreground">{label}</span>
      <div className="font-medium">{value}</div>
    </div>
  );
}

/** OVH 字段格式：{ value, unit } | number | null */
function fmtBandwidth(v: any): string {
  if (v == null) return "—";
  if (typeof v === "object" && v?.value != null) {
    return `${v.value} ${v.unit || ""}`.trim();
  }
  if (typeof v === "number") {
    if (v >= 1_000_000_000) return `${(v / 1_000_000_000).toFixed(1)} Gbps`;
    if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(0)} Mbps`;
    return `${v}`;
  }
  return String(v);
}

/** 字节数格式化:最小单位 GB,1024 二进制进位 (GB → TB → PB)。
 *  接受裸数字 或 OVH 形式 { value, unit }(unit="B"/"KB"/"MB"/"GB"/"TB" 都先归一到字节再换算)。
 *  流量配额这种用,不要用 fmtBandwidth(bps 进位 1000 + 最小 bps,数值大时含义不对)。 */
function fmtBytes(v: any): string {
  if (v == null) return "—";
  let bytes: number;
  if (typeof v === "object") {
    if (v?.value == null) return "—";
    // 先把 OVH 给的 { value, unit } 归一到字节
    const n = Number(v.value);
    if (!Number.isFinite(n)) return String(v.value);
    const unit = String(v.unit || "B").toUpperCase();
    const mult =
      unit === "PB" ? 1024 ** 5 :
      unit === "TB" ? 1024 ** 4 :
      unit === "GB" ? 1024 ** 3 :
      unit === "MB" ? 1024 ** 2 :
      unit === "KB" ? 1024 :
      1; // B 或未识别
    bytes = n * mult;
  } else if (typeof v === "number") {
    bytes = v;
  } else {
    return String(v);
  }
  if (bytes === 0) return "0 GB";
  const GB = 1024 ** 3;
  const TB = 1024 ** 4;
  const PB = 1024 ** 5;
  const fmt = (x: number) => (Number.isInteger(x) ? String(x) : x.toFixed(2));
  if (bytes >= PB) return `${fmt(bytes / PB)} PB`;
  if (bytes >= TB) return `${fmt(bytes / TB)} TB`;
  return `${fmt(bytes / GB)} GB`;
}
