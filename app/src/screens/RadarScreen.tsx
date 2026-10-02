/**
 * 雷达(补货):订阅机型优先,机房格显示原始可用性枚举(1H-low 这类)。
 * 红绿只认 \d+H 白名单 —— 与后端 IsAvailableForOrder 同规则(core/availability)。
 */
import { ActivityIndicator, FlatList, RefreshControl, StyleSheet, Text, View } from "react-native";
import { Star } from "lucide-react-native";

import type { ApiClient } from "@core/api-client";
import type { ServerPlan } from "@core/types";
import { anyOrderable, isOrderable } from "@core/availability";
import { useTokens } from "../theme/tokens";
import { usePoll } from "../api/hooks";

interface AvailResp { availability?: Record<string, Record<string, string>> }

export default function RadarScreen({ client }: { client: ApiClient }) {
  const t = useTokens();
  const plans = usePoll<{ servers: ServerPlan[] }>(client, "/servers", 60_000);
  const avail = usePoll<AvailResp>(client, "/availability", 10_000);

  const list = plans.data?.servers ?? [];
  // 订阅的排在前面(v1 用“本地曾下单”信号简化:有货的优先,全部无货在后)
  const sorted = [...list].sort((a, b) => score(b) - score(a));
  function score(p: ServerPlan): number {
    const dcs = avail.data?.availability?.[p.planCode] ?? {};
    return anyOrderable(Object.values(dcs)) ? 1 : 0;
  }

  return (
    <FlatList
      style={{ flex: 1 }}
      contentContainerStyle={{ padding: 16, gap: 10, paddingBottom: 24 }}
      data={sorted}
      keyExtractor={(p) => p.planCode}
      refreshControl={<RefreshControl refreshing={plans.loading} tintColor={t.muted} onRefresh={plans.refresh} />}
      ListHeaderComponent={<Text style={{ fontSize: 24, fontWeight: "700", color: t.fg, marginBottom: 4 }}>雷达</Text>}
      ListEmptyComponent={
        plans.error ? (
          <Text style={{ color: t.danger, fontSize: 12, textAlign: "center", marginTop: 40 }}>{plans.error}</Text>
        ) : plans.loading ? (
          <ActivityIndicator color={t.muted} style={{ marginTop: 40 }} />
        ) : (
          <Text style={{ color: t.muted, fontSize: 12, textAlign: "center", marginTop: 40 }}>目录为空 —— 检查账户与后端缓存</Text>
        )
      }
      renderItem={({ item }) => {
        const dcs = avail.data?.availability?.[item.planCode] ?? {};
        const hasStock = Object.values(dcs).some(isOrderable);
        return (
          <View style={[styles.card, { backgroundColor: t.surface, borderColor: t.border }]}>
            <View style={styles.row}>
              <View style={{ flexDirection: "row", alignItems: "center", gap: 6 }}>
                {hasStock && <Star size={12} color={t.warning} fill={t.warning} />}
                <Text style={{ fontFamily: "Menlo", fontSize: 12.5, color: t.fg }}>{item.planCode}</Text>
              </View>
              <Text style={{ fontSize: 10.5, color: hasStock ? t.success : t.faint, fontWeight: "600" }}>
                {hasStock ? "有货" : "无货"}
              </Text>
            </View>
            {Object.keys(dcs).length > 0 && (
              <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 6, marginTop: 9 }}>
                {Object.entries(dcs).map(([dc, st]) => (
                  <Text
                    key={dc}
                    style={{
                      fontFamily: "Menlo",
                      fontSize: 10,
                      paddingHorizontal: 8,
                      paddingVertical: 4,
                      borderRadius: 8,
                      borderWidth: 1,
                      color: isOrderable(st) ? t.success : t.faint,
                      borderColor: isOrderable(st) ? t.success : t.border,
                      backgroundColor: isOrderable(st) ? t.surfaceMuted : "transparent",
                      overflow: "hidden",
                    }}
                  >
                    {dc.toUpperCase()} {st}
                  </Text>
                ))}
              </View>
            )}
            {item.pricings?.[0]?.price?.amount != null && (
              <Text style={{ fontSize: 10.5, color: t.muted, marginTop: 8 }}>
                {item.memory || ""} / {item.storage || ""} · €{item.pricings[0].price.amount}/月起
              </Text>
            )}
          </View>
        );
      }}
    />
  );
}

const styles = StyleSheet.create({
  card: { borderRadius: 16, borderWidth: 1, padding: 13 },
  row: { flexDirection: "row", alignItems: "center", justifyContent: "space-between" },
});
