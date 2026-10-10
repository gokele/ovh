import { Cpu, HardDrive, MemoryStick, MapPin, Globe, Wifi, AlertTriangle, PartyPopper } from "lucide-react";
import type { OwnedServer } from "@/hooks/use-server-control";
import { useServerHardware, useServerIps, useServerNetworkInterfaces, useHardwareLottery } from "@/hooks/use-server-control";
import { useHideIp, maskSensitive } from "@/hooks/use-hide-ip";
import { Skeleton } from "@/components/common/Skeleton";
import { PartialNotice, DetailErrorTag } from "@/components/common/PartialNotice";
import { MrtgTrafficChart } from "./MrtgTrafficChart";
import { LotteryConfetti } from "./LotteryConfetti";
import { useTranslation } from "react-i18next";

/** 概览 Tab：硬件 + 网络（IP / 接口 / MRTG 流量）。服务信息胶囊条已上提到 ServerTabs 同行 */
export function OverviewTab({ server }: { server: OwnedServer }) {
  const { t } = useTranslation();
  const hw = useServerHardware(server.serviceName);
  const lottery = useHardwareLottery(server.serviceName);
  const ips = useServerIps(server.serviceName);
  const interfaces = useServerNetworkInterfaces(server.serviceName);
  const { hidden } = useHideIp();

  // 内存字段是 { value, unit } 对象
  const memText = hw.data?.memorySize
    ? `${hw.data?.memorySize?.value} ${hw.data?.memorySize?.unit}`
    : "—";

  // CPU 字段：processorName + 核线（旧前端写法照搬）
  const cpuText = hw.data?.processorName
    ? hw.data?.coresPerProcessor && hw.data?.threadsPerProcessor
      ? t("maint.overview.cpuCoresThreads", {
          name: (hw.data?.processorName ?? ""),
          cores: hw.data?.coresPerProcessor,
          threads: hw.data?.threadsPerProcessor,
        })
      : (hw.data?.processorName ?? "")
    : "—";

  // 磁盘：把所有 diskGroups 拼成 "N × Type Size" / "N × Type Size" 多组用 / 分隔
  const diskText =
    hw.data?.diskGroups && (hw.data?.diskGroups?.length ?? 0) > 0
      ? (hw.data?.diskGroups ?? [])
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
      {/* 列表接口这次没查到这台机器的 serviceInfos：续费状态 / 计费状态都不可信。
          「没查到」不能默默当成「没开自动续费」，否则用户会以为自己已经关过续费了。 */}
      {(server.svcInfoError || server.error) && (
        <div className="flex items-start gap-2 rounded-xl border border-warning/40 bg-warning/5 px-3 py-2 text-[11px] text-foreground/80">
          <AlertTriangle className="w-3.5 h-3.5 text-warning flex-shrink-0 mt-0.5" />
          <span>
            {server.error
              ? t("maint.overview.detailError", { err: server.error })
              : t("maint.overview.svcInfoError", { err: server.svcInfoError })}
          </span>
        </div>
      )}

      
        {/* 中奖横幅:订购配置 != 实际交付且更好。checked=false 时不显示。
            等级(1中奖/2大奖/3头奖)由后端按中奖项数+翻倍幅度算好;首次查看撒一次彩带。 */}
        {lottery.data?.checked && lottery.data.won && (
          <div className="relative border border-amber-400/40 bg-amber-400/5 rounded-2xl p-3 flex items-start gap-2">
            <LotteryConfetti sessionKey={server.serviceName} />
            <PartyPopper className={`w-4 h-4 mt-0.5 flex-shrink-0 ${lottery.data.tier === 3 ? "text-amber-500 w-5 h-5" : "text-amber-500"}`} />
            <div className="leading-relaxed min-w-0">
              <span className="text-[12px] font-semibold text-amber-600 dark:text-amber-300">
                {t(`maint.overview.lottery.title${Math.min(lottery.data.tier || 1, 3)}`)}
              </span>{" "}
              <span className="text-[12px] text-muted-foreground">
                {t("maint.overview.lottery.bannerDesc", {
                  items: lottery.data.items
                    .map((i) => {
                      const label = t(`maint.overview.lottery.kind.${i.kind}`);
                      if (i.gainPct && i.gainPct >= 1) {
                        return `${label} ${t("maint.overview.lottery.gainPct", { pct: Math.round(i.gainPct) })}`;
                      }
                      if (i.mediaUp) {
                        return `${label} ${t("maint.overview.lottery.gainMedia")}`;
                      }
                      return label;
                    })
                    .join(" · "),
                })}
              </span>
              <div className="mt-1.5 space-y-0.5">
                {lottery.data.items.map((i) => (
                  <p key={i.kind} className="text-[11px] text-muted-foreground">
                    <span className="font-medium text-foreground/80">{t(`maint.overview.lottery.kind.${i.kind}`)}</span>
                    ：{i.ordered} → <span className="font-semibold text-amber-600 dark:text-amber-300">{i.actual}</span>
                  </p>
                ))}
              </div>
            </div>
          </div>
        )}
<div className="grid grid-cols-2 md:grid-cols-4 gap-2.5 sm:gap-3">
        <InfoCard icon={<Cpu className="w-4 h-4" />} label={t("maint.overview.info.cpu")} value={cpuText} loading={hw.isPending} />
        <InfoCard icon={<MemoryStick className="w-4 h-4" />} label={t("maint.overview.info.mem")} value={memText} loading={hw.isPending} />
        <InfoCard icon={<HardDrive className="w-4 h-4" />} label={t("maint.overview.info.disk")} value={diskText} loading={hw.isPending} />
        <InfoCard icon={<MapPin className="w-4 h-4" />} label={t("maint.overview.info.dc")} value={(server.datacenter || "—").toUpperCase()} />
      </div>

      {/* 网络：IP 列表 + 接口 + MRTG 流量 */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 sm:gap-4">
        {/* IP 列表 */}
        <div className="border border-border rounded-2xl overflow-hidden">
          <div className="px-4 py-3 border-b border-border flex items-center gap-2">
            <Globe className="w-4 h-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">{t("maint.overview.ipTitle")}</h3>
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
            <h3 className="text-sm font-semibold">{t("maint.overview.nicTitle")}</h3>
          </div>
          {interfaces.isPending ? (
            <div className="p-4">
              <Skeleton className="h-20 rounded-md" />
            </div>
          ) : interfaces.isError ? (
            // 「读取失败」和「这台机器没网卡」是两回事，混成同一句会让用户放弃重试
            <p className="px-4 py-6 text-sm text-destructive text-center">{t("maint.overview.nicLoadFailed")}</p>
          ) : (interfaces.data?.items || []).length === 0 ? (
            <p className="px-4 py-6 text-sm text-muted-foreground text-center">{t("maint.overview.nicEmpty")}</p>
          ) : (
            <>
              <PartialNotice
                failedCount={interfaces.data?.failedCount || 0}
                what={t("maint.overview.nicPartialWhat")}
                className="mx-4 mt-3"
              />
              <div className="divide-y divide-border">
                {(interfaces.data?.items || []).map((nic) => (
                  <div key={nic.mac} className="px-4 py-3 flex items-center justify-between text-[13px]">
                    <code className="font-mono">{nic.mac}</code>
                    <span className="text-[11px] text-muted-foreground flex items-center gap-2">
                      <DetailErrorTag message={nic._detailError} />
                      {nic.linkType || "—"}
                    </span>
                  </div>
                ))}
              </div>
            </>
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
