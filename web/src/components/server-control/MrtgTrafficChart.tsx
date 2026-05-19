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
import { useMrtgTraffic, type MrtgPeriod, type MrtgInterface } from "@/hooks/use-mrtg";

const PERIOD_LABEL: Record<MrtgPeriod, string> = {
  hourly: "过去 1 小时",
  daily: "过去 24 小时",
  weekly: "过去 7 天",
  monthly: "过去 30 天",
  yearly: "过去 1 年",
};

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
  const [period, setPeriod] = useState<MrtgPeriod>("daily");
  const { download, upload, isPending, isFetching, refetch } = useMrtgTraffic(serviceName, period);

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
            <h3 className="text-sm font-semibold">流量监控</h3>
          </div>
          <div className="flex items-center gap-2">
            <Select value={period} onValueChange={(v) => setPeriod(v as MrtgPeriod)}>
              <SelectTrigger className="rounded-full h-9 w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(PERIOD_LABEL) as MrtgPeriod[]).map((p) => (
                  <SelectItem key={p} value={p}>
                    {PERIOD_LABEL[p]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isFetching}>
              <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? "animate-spin" : ""}`} />
              刷新
            </Button>
          </div>
        </div>

        {isPending ? (
          <Skeleton className="h-[420px] rounded-2xl" />
        ) : merged.length === 0 ? (
          <EmptyState icon={Wifi} title="暂无流量数据" description="该服务器尚未上报 MRTG 数据，或周期内没有流量。" />
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
  // 合并双线：按下载的时间序列对齐
  const chartData = useMemo(
    () =>
      download.data.map((dp, i) => {
        const up = upload.data[i];
        return {
          time: new Date(dp.timestamp * 1000).toLocaleString("zh-CN", {
            month: "2-digit",
            day: "2-digit",
            hour: "2-digit",
            minute: "2-digit",
          }),
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

  const summary = `${PERIOD_LABEL[period]}，平均 ${formatBandwidth(stats.dlAvg + stats.ulAvg)}（↓${formatBandwidth(stats.dlAvg)} ↑${formatBandwidth(stats.ulAvg)}），峰值 ${formatBandwidth(stats.totMax)}`;

  return (
    <div className="border border-border rounded-2xl p-4">
      <div className="flex items-center gap-2 mb-3 flex-wrap">
        <Wifi className="w-4 h-4 text-muted-foreground" />
        <span className="text-[13px] font-semibold">网卡</span>
        <code className="font-mono text-[12px] bg-secondary px-2 py-0.5 rounded-full">{mac}</code>
      </div>

      {/* 摘要 */}
      <p className="text-[12px] text-muted-foreground mb-3">{summary}</p>

      {/* 双向统计卡 */}
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-4">
        <StatBlock label="下载" tone="success" icon={<ArrowDown className="w-3.5 h-3.5" />} cur={stats.dlCur} avg={stats.dlAvg} max={stats.dlMax} />
        <StatBlock label="上传" tone="warning" icon={<ArrowUp className="w-3.5 h-3.5" />} cur={stats.ulCur} avg={stats.ulAvg} max={stats.ulMax} />
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
                name === "download" ? "↓ 下载" : "↑ 上传",
              ]}
            />
            <Legend
              wrapperStyle={{ paddingTop: 8, fontSize: 12 }}
              formatter={(value) => (value === "download" ? "↓ 下载带宽" : "↑ 上传带宽")}
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
        数据点 <span className="font-semibold text-foreground">{stats.points}</span> · 周期 {PERIOD_LABEL[period]}
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
  const toneText = tone === "success" ? "text-success" : "text-warning";
  return (
    <div className={`border border-border rounded-xl p-3 ${toneText}`}>
      <div className="flex items-center gap-1.5 text-[12px] font-semibold mb-2">
        {icon}
        {label}带宽
      </div>
      <div className="grid grid-cols-3 gap-1.5 sm:gap-2 text-[11px]">
        <Slot label="当前" value={formatBandwidth(cur)} />
        <Slot label="平均" value={formatBandwidth(avg)} bold />
        <Slot label="峰值" value={formatBandwidth(max)} />
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
