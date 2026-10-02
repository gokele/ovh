/**
 * MRTG 迷你流量图(概览 Tab)— 用组件库(victory-native)渲染,不手写 SVG。
 * 数据来自 /server-control/:sn/mrtg(后端已按网卡聚合),画第一张有数据网卡的下载曲线。
 * 面积图(VictoryArea)比折线更适合流量形态;轴隐藏(手机上峰值数字更有用)。
 */
import { StyleSheet, Text, View } from "react-native";
import { VictoryChart, VictoryArea, VictoryAxis } from "victory-native";

import type { ApiClient } from "@core/api-client";
import { useTokens } from "../theme/tokens";
import { usePoll } from "../api/hooks";

interface MrtgPoint { timestamp?: string; value?: number }
interface MrtgIface { mac?: string; data?: MrtgPoint[]; error?: string }
interface MrtgResp { interfaces?: MrtgIface[]; message?: string }

/** 序列峰值(卡片右上角的数字) */
function peak(series: MrtgPoint[] | undefined): number {
  if (!series || series.length === 0) return 0;
  return Math.max(...series.map((p) => Number(p.value) || 0));
}

export default function MrtgSpark({ client, serviceName }: { client: ApiClient; serviceName: string }) {
  const t = useTokens();
  const q = usePoll<MrtgResp>(client, `/server-control/${serviceName}/mrtg?period=daily&type=traffic:download`, 60_000);

  const iface = (q.data?.interfaces ?? []).find((i) => (i.data ?? []).length > 1) ?? (q.data?.interfaces ?? [])[0];
  const data = (iface?.data ?? []).map((p, i) => ({ x: i, y: Number(p.value) || 0 }));

  return (
    <View style={[styles.card, { backgroundColor: t.surface, borderColor: t.border }]}>
      <View style={styles.row}>
        <Text style={{ fontSize: 12.5, fontWeight: "600", color: t.fg }}>流量监控</Text>
        <Text style={{ fontSize: 10.5, color: t.muted }}>
          24 小时 · 下载峰值 {peak(iface?.data)} Mbps{iface?.mac ? ` · ${iface.mac}` : ""}
        </Text>
      </View>
      {q.error ? (
        <Text style={{ fontSize: 11, color: t.danger, marginTop: 6 }}>{q.error}</Text>
      ) : q.loading || data.length < 2 ? (
        <Text style={{ fontSize: 11, color: t.muted, marginTop: 6 }}>
          {q.loading ? "读取中…" : q.data?.message || "OVH 未返回网卡数据(极老机型可能未接入)"}
        </Text>
      ) : (
        <VictoryChart height={64} padding={{ top: 6, left: 0, right: 0, bottom: 2 }}>
          <VictoryAxis dependentAxis={false} standalone={false} style={{ axis: { stroke: "transparent" }, ticks: { stroke: "transparent" } }} tickValues={[]} />
          <VictoryArea
            data={data}
            interpolation="monotoneX"
            style={{ data: { fill: t.info + "22", stroke: t.info, strokeWidth: 1.6 } }}
            standalone={false}
          />
        </VictoryChart>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  card: { borderRadius: 16, borderWidth: 1, padding: 13 },
  row: { flexDirection: "row", alignItems: "baseline", justifyContent: "space-between", gap: 8, flexWrap: "wrap" },
});
