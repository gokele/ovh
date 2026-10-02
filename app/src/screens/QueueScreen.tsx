/**
 * 队列:任务卡三行 —— 是什么(型号+机房+重试预算)/ 慢不慢(上轮耗时)/ 为什么停(人话)。
 * 轻操作:恢复 / 删除(有确认)。
 */
import { Alert, FlatList, Pressable, RefreshControl, StyleSheet, Text, View } from "react-native";
import { Play, Pause, Trash2 } from "lucide-react-native";

import type { ApiClient } from "@core/api-client";
import type { QueueItem } from "@core/types";
import { useTokens } from "../theme/tokens";
import { useAction, usePoll } from "../api/hooks";

export default function QueueScreen({ client, onClose }: { client: ApiClient; onClose: () => void }) {
  const t = useTokens();
  const q = usePoll<QueueItem[]>(client, "/queue", 5_000);
  const act = useAction(client);

  const items = Array.isArray(q.data) ? q.data : [];

  const pauseResume = async (item: QueueItem) => {
    // 后端契约:PUT /queue/:id/status + {status:"paused"|"running"}(没有独立的 pause/resume 路由)
    const r = await act.put(`/queue/${item.id}/status`, { status: item.status === "paused" ? "running" : "paused" });
    if (!r.ok) Alert.alert("失败", r.message);
    q.refresh();
  };

  const remove = (item: QueueItem) => {
    Alert.alert("删除任务?", `${item.planCode} @ ${(item.datacenter || "").toUpperCase()}\n删除后不再重试,此操作不可撤销。`, [
      { text: "取消", style: "cancel" },
      { text: "删除", style: "destructive", onPress: async () => {
        const r = await act.run(`/queue/${item.id}`);
        if (!r.ok) Alert.alert("失败", r.message);
        q.refresh();
      } },
    ]);
  };

  return (
    <FlatList
      style={{ flex: 1 }}
      contentContainerStyle={{ padding: 16, gap: 10, paddingBottom: 24 }}
      data={items}
      keyExtractor={(i) => i.id}
      refreshControl={<RefreshControl refreshing={q.loading} tintColor={t.muted} onRefresh={q.refresh} />}
      ListHeaderComponent={<View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", marginBottom: 4, paddingRight: 4 }}><Text style={{ fontSize: 24, fontWeight: "700", color: t.fg }}>队列</Text><Pressable onPress={onClose} hitSlop={12}><Text style={{ fontSize: 13, color: t.muted }}>完成</Text></Pressable></View>}
      ListEmptyComponent={
        q.error ? (
          <Text style={{ color: t.danger, fontSize: 12, textAlign: "center", marginTop: 40 }}>{q.error}</Text>
        ) : q.loading ? (
          <Text style={{ color: t.muted, fontSize: 12, textAlign: "center", marginTop: 40 }}>读取中…</Text>
        ) : (
          <Text style={{ color: t.muted, fontSize: 12, textAlign: "center", marginTop: 40 }}>队列为空 —— 去网页端或 TG 下单</Text>
        )
      }
      renderItem={({ item }) => {
        const st = (item.status || "").toLowerCase();
        const color = st === "running" ? t.success : st === "paused" ? t.warning : st === "failed" ? t.danger : t.muted;
        return (
          <View style={[styles.card, { backgroundColor: t.surface, borderColor: color }]}>
            <View style={styles.row}>
              <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
                <View style={{ width: 7, height: 7, borderRadius: 99, backgroundColor: color }} />
                <Text style={{ fontFamily: "Menlo", fontSize: 12.5, color: t.fg }}>{item.planCode}</Text>
              </View>
              <Text style={{ fontSize: 10.5, color, fontWeight: "600" }}>{statusLabel(st)}</Text>
            </View>
            <Text style={{ fontSize: 11, color: t.muted, marginTop: 7 }}>
              {(item.datacenter || "").toUpperCase()} · 重试 {item.failureCount ?? 0}/{item.maxRetries} · 间隔 {item.retryInterval}s
            </Text>
            {item.timings && Object.keys(item.timings).length > 0 && (
              <Text style={{ fontSize: 10.5, color: t.muted, marginTop: 3 }}>
                上轮 {totalMs(item.timings)}ms({Object.entries(item.timings).map(([k, v]) => `${stageCn(k)} ${ms(v)}`).join(" / ")})
              </Text>
            )}
            {item.lastError ? (
              <Text style={{ fontSize: 11, color: t.danger, marginTop: 6, lineHeight: 16 }}>{item.lastError}</Text>
            ) : null}
            {(st === "running" || st === "paused") && (
              <View style={{ flexDirection: "row", gap: 8, marginTop: 10 }}>
                <SmallBtn t={t} Icon={st === "paused" ? Play : Pause} label={st === "paused" ? "恢复" : "暂停"} onPress={() => pauseResume(item)} />
                <SmallBtn t={t} Icon={Trash2} label="删除" danger onPress={() => remove(item)} />
              </View>
            )}
          </View>
        );
      }}
    />
  );
}

function statusLabel(st: string): string {
  return { running: "运行中", paused: "已暂停", failed: "失败", success: "成功", pending: "等待" }[st] || st;
}
function stageCn(k: string): string {
  return { availability: "查库存", price: "验价", cart: "建车", checkout: "下单" }[k] || k;
}
function ms(v: number): string {
  return v >= 1000 ? (v / 1000).toFixed(1) + "s" : v + "ms";
}
function totalMs(timings: Record<string, number>): number {
  return Object.values(timings).reduce((a, b) => a + b, 0);
}

function SmallBtn({ t, Icon, label, danger, onPress }: { t: ReturnType<typeof useTokens>; Icon: typeof Play; label: string; danger?: boolean; onPress: () => void }) {
  const c = danger ? t.danger : t.muted;
  return (
    <Pressable
      style={{ flexDirection: "row", alignItems: "center", gap: 4, paddingHorizontal: 10, paddingVertical: 5, borderRadius: 99, borderWidth: 1, borderColor: c }}
      onPress={onPress}
    >
      <Icon size={11} color={c} />
      <Text style={{ fontSize: 11, color: c }}>{label}</Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  card: { borderRadius: 16, borderWidth: 1, padding: 13 },
  row: { flexDirection: "row", alignItems: "center", justifyContent: "space-between" },
});
