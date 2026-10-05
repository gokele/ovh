import { useState, useMemo } from "react";
import { Wifi, ArrowDown, ArrowUp, RefreshCw } from "lucide-react";
import {
  ResponsiveContainer,
  LineChart,
  Line,
  CartesianGrid,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
} from "recharts";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed } from "@/components/common/LoadFailed";
import { useMrtgTraffic, type MrtgPeriod, type MrtgInterface } from "@/hooks/use-mrtg";
import { Trans, useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { fmtPattern } from "@/i18n/format";

/** 周期选项表:存文案 key,渲染处 t() */
const PERIOD_KEYS: Record<MrtgPeriod, string> = {
  hourly: "maint.mrtg.period.hourly",
  daily: "maint.mrtg.period.daily",
  weekly: "maint.mrtg.period.weekly",
  monthly: "maint.mrtg.period.monthly",
  yearly: "maint.mrtg.period.yearly",
};

function periodLabel(t: TFunction, p: MrtgPeriod): string {
  return t(PERIOD_KEYS[p]);
}

/** bps → 友好显示（Kbps / Mbps / Gbps） */
function formatBandwidth(bps: number): string {
  if (bps >= 1_000_000_000) return `${(bps / 1_000_000_000).toFixed(2)} Gbps`;
  if (bps >= 1_000_000) return `${(bps / 1_000_000).toFixed(2)} Mbps`;
  if (bps >= 1_000) return `${(bps / 1_000).toFixed(2)} Kbps`;
  return `${bps.toFixed(0)} bps`;
}

/**
 * MRTG 流量监控组件
 * - 选 period（hourly / daily / weekly / monthly / yearly）
 * - 每张网卡一张图（按 MAC 分组），同时画下载 + 上传双线
 * - 图上方有"当前 / 平均 / 峰值"统计栏
 */
export function MrtgTrafficChart({ serviceName }: { serviceName: string }) {
  const { t } = useTranslation();
  const [period, setPeriod] = useState<MrtgPeriod>("daily");
  // isError / error 以前漏解构了,拉失败时 merged 是空数组,界面就一路滑到「暂无流量数据」
  const { download, upload, isPending, isFetching, isError, error, refetch } = useMrtgTraffic(serviceName, period);

  // 把 download interfaces 与 upload interfaces 按 mac 合并
  const merged = useMemo(() => {
    if (!download?.interfaces || !upload?.interfaces) return [];
    return download.interfaces
      .map((d: MrtgInterface) => {
        const u = upload.interfaces.find((x) => x.mac === d.mac);
        if (!d.data?.length || !u?.data?.length) return null;
        return { mac: d.mac, download: d, upload: u };
      })
      .filter((x): x is { mac: string; download: MrtgInterface; upload: MrtgInterface } => x !== null);
  }, [download, upload]);

  return (
    <Card>
      <CardContent className="p-5 space-y-4">
        <div className="flex items-center justify-between gap-2 flex-wrap">
          <div className="flex items-center gap-2">
            <Wifi className="w-4 h-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">{t("maint.mrtg.title")}</h3>
          </div>
          <div className="flex items-center gap-2">
            <Select value={period} onValueChange={(v) => setPeriod(v as MrtgPeriod)}>
              <SelectTrigger className="rounded-full h-9 w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(PERIOD_KEYS) as MrtgPeriod[]).map((p) => (
                  <SelectItem key={p} value={p}>
                    {periodLabel(t, p)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isFetching}>
              <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? "animate-spin" : ""}`} />
              {t("common.refresh")}
            </Button>
          </div>
        </div>

        {isPending ? (
          <Skeleton className="h-[420px] rounded-2xl" />
        ) : isError ? (
          // 请求挂了 ≠ 机器没流量。空态那句「该服务器尚未上报 MRTG 数据,或周期内没有流量」
          // 会被读成"这机器没在跑 / 网卡没通",足以让人去重启甚至重装一台其实好好的机器。
          <LoadFailed icon={Wifi} title={t("maint.mrtg.loadFailed")} error={error} onRetry={() => refetch()} />
        ) : merged.length === 0 ? (
          <EmptyState icon={Wifi} title={t("maint.mrtg.emptyTitle")} description={t("maint.mrtg.emptyDesc")} />
        ) : (
          <div className="space-y-6">
            {merged.map(({ mac, download: d, upload: u }) => (
              <InterfaceChart key={mac} mac={mac} download={d} upload={u} period={period} />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

/** 单网卡的图表 + 统计栏 */
function InterfaceChart({
  mac,
  download,
  upload,
  period,
}: {
  mac: string;
  download: MrtgInterface;
  upload: MrtgInterface;
  period: MrtgPeriod;
}) {
  const { t } = useTranslation();
  // 合并双线：按下载的时间序列对齐
  const chartData = useMemo(
    () =>
      download.data.map((dp, i) => {
        const up = upload.data[i];
        return {
          time: fmtPattern(dp.timestamp * 1000, "MM/dd HH:mm"),
          download: dp.value?.value || 0,
          upload: up?.value?.value || 0,
        };
      }),
    [download, upload]
  );

  // 当前 / 平均 / 峰值 统计
  const stats = useMemo(() => {
    const dl = chartData.map((d) => d.download);
    const ul = chartData.map((d) => d.upload);
    const tot = chartData.map((d) => d.download + d.upload);
    const avg = (arr: number[]) => (arr.length ? arr.reduce((a, b) => a + b, 0) / arr.length : 0);
    return {
      dlCur: dl[dl.length - 1] || 0,
      dlAvg: avg(dl),
      dlMax: Math.max(0, ...dl),
      ulCur: ul[ul.length - 1] || 0,
      ulAvg: avg(ul),
      ulMax: Math.max(0, ...ul),
      totMax: Math.max(0, ...tot),
      points: chartData.length,
    };
  }, [chartData]);

  const summary = t("maint.mrtg.summary", {
    period: periodLabel(t, period),
    avg: formatBandwidth(stats.dlAvg + stats.ulAvg),
    down: formatBandwidth(stats.dlAvg),
    up: formatBandwidth(stats.ulAvg),
    peak: formatBandwidth(stats.totMax),
  });

  return (
    <div className="border border-border rounded-2xl p-4">
      <div className="flex items-center gap-2 mb-3 flex-wrap">
        <Wifi className="w-4 h-4 text-muted-foreground" />
        <span className="text-[13px] font-semibold">{t("maint.mrtg.nicLabel")}</span>
        <code className="font-mono text-[12px] bg-secondary px-2 py-0.5 rounded-full">{mac}</code>
      </div>

      {/* 摘要 */}
      <p className="text-[12px] text-muted-foreground mb-3">{summary}</p>

      {/* 双向统计卡 */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-4">
        <StatBlock label={t("maint.mrtg.downloadBandwidth")} tone="success" icon={<ArrowDown className="w-3.5 h-3.5" />} cur={stats.dlCur} avg={stats.dlAvg} max={stats.dlMax} />
        <StatBlock label={t("maint.mrtg.uploadBandwidth")} tone="warning" icon={<ArrowUp className="w-3.5 h-3.5" />} cur={stats.ulCur} avg={stats.ulAvg} max={stats.ulMax} />
      </div>

      {/* 图表 */}
      <div style={{ width: "100%", height: 320 }}>
        <ResponsiveContainer>
          <LineChart data={chartData} margin={{ top: 5, right: 10, left: -10, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
            <XAxis
              dataKey="time"
              stroke="hsl(var(--muted-foreground))"
              tickLine={false}
              axisLine={false}
              style={{ fontSize: 10 }}
              angle={-45}
              textAnchor="end"
              height={70}
            />
            <YAxis
              stroke="hsl(var(--muted-foreground))"
              tickLine={false}
              axisLine={false}
              style={{ fontSize: 10 }}
              tickFormatter={(v) => formatBandwidth(v).replace(/\s.*/, "")}
            />
            <Tooltip
              contentStyle={{
                backgroundColor: "hsl(var(--popover))",
                border: "1px solid hsl(var(--border))",
                borderRadius: 8,
                fontSize: 12,
              }}
              formatter={(value: any, name: string) => [
                formatBandwidth(Number(value)),
                name === "download" ? t("maint.mrtg.tooltipDl") : t("maint.mrtg.tooltipUl"),
              ]}
            />
            <Legend
              wrapperStyle={{ paddingTop: 8, fontSize: 12 }}
              formatter={(value) => (value === "download" ? t("maint.mrtg.legendDl") : t("maint.mrtg.legendUl"))}
            />
            <Line
              type="monotone"
              dataKey="download"
              stroke="hsl(var(--success))"
              strokeWidth={2}
              dot={false}
              name="download"
              animationDuration={600}
            />
            <Line
              type="monotone"
              dataKey="upload"
              stroke="hsl(var(--warning))"
              strokeWidth={2}
              dot={false}
              name="upload"
              animationDuration={600}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>

      <div className="mt-3 text-[11px] text-muted-foreground text-center">
        <Trans
          i18nKey="maint.mrtg.pointsLine"
          values={{ n: stats.points, period: periodLabel(t, period) }}
          components={{ b: <span className="font-semibold text-foreground" /> }}
        />
      </div>
    </div>
  );
}

function StatBlock({
  label,
  tone,
  icon,
  cur,
  avg,
  max,
}: {
  label: string;
  tone: "success" | "warning";
  icon: React.ReactNode;
  cur: number;
  avg: number;
  max: number;
}) {
  const { t } = useTranslation();
  const toneText = tone === "success" ? "text-success" : "text-warning";
  return (
    <div className={`border border-border rounded-xl p-3 ${toneText}`}>
      <div className="flex items-center gap-1.5 text-[12px] font-semibold mb-2">
        {icon}
        {label}
      </div>
      <div className="grid grid-cols-3 gap-1.5 sm:gap-2 text-[11px]">
        <Slot label={t("maint.mrtg.slot.current")} value={formatBandwidth(cur)} />
        <Slot label={t("maint.mrtg.slot.avg")} value={formatBandwidth(avg)} bold />
        <Slot label={t("maint.mrtg.slot.max")} value={formatBandwidth(max)} />
      </div>
    </div>
  );
}

function Slot({ label, value, bold }: { label: string; value: string; bold?: boolean }) {
  return (
    <div>
      <div className="text-muted-foreground mb-0.5">{label}</div>
      <div className={`font-mono ${bold ? "font-bold" : "font-semibold"}`}>{value}</div>
    </div>
  );
}
