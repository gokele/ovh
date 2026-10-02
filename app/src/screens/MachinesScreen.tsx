/**
 * 机器列表(主 Tab):已购独服 + VPS 卡片流。
 * 状态红黄绿点、救援模式琥珀描边、别名与 IP;点卡片进单机控制台。
 */
import { useState } from "react";
import { ActivityIndicator, FlatList, Pressable, RefreshControl, StyleSheet, Text, View } from "react-native";
import { Copy } from "lucide-react-native";

import type { ApiClient } from "@core/api-client";
import type { OwnedServer } from "@core/types";
import { useTokens } from "../theme/tokens";
import { usePoll } from "../api/hooks";
import ServerDetail from "./ServerDetail";

interface ListResp { servers: OwnedServer[] }

export default function MachinesScreen({ client }: { client: ApiClient }) {
  const t = useTokens();
  const q = usePoll<ListResp>(client, "/server-control/list", 15_000);
  const [open, setOpen] = useState<OwnedServer | null>(null);

  // 单机控制台是全屏覆盖层,返回时列表原地保留
  if (open) return <ServerDetail client={client} server={open} onBack={() => setOpen(null)} />;

  const servers = q.data?.servers ?? [];
  return (
    <FlatList
      style={{ flex: 1 }}
      contentContainerStyle={{ padding: 16, gap: 10, paddingBottom: 24 }}
      data={servers}
      keyExtractor={(s) => s.serviceName}
      refreshControl={<RefreshControl refreshing={q.loading} tintColor={t.muted} onRefresh={q.refresh} />}
      ListHeaderComponent={<Text style={{ fontSize: 24, fontWeight: "700", color: t.fg, marginBottom: 4 }}>机器</Text>}
      ListEmptyComponent={
        q.error ? (
          <Text style={{ color: t.danger, fontSize: 12, textAlign: "center", marginTop: 40 }}>{q.error}</Text>
        ) : q.loading ? (
          <ActivityIndicator color={t.muted} style={{ marginTop: 40 }} />
        ) : (
          <Text style={{ color: t.muted, fontSize: 12, textAlign: "center", marginTop: 40 }}>
            没有已购服务器 —— 后端切换账户或去网页端确认列表
          </Text>
        )
      }
      renderItem={({ item }) => {
        const rescue = item.netbootMode === "rescue";
        const ok = (item.state || "").toLowerCase() === "ok" || (item.state || "").toLowerCase() === "active";
        const dot = rescue ? t.warning : ok ? t.success : t.danger;
        return (
          <Pressable
            style={[styles.card, { backgroundColor: t.surface, borderColor: rescue ? t.warning : t.border }]}
            onPress={() => setOpen(item)}
          >
            <View style={styles.row}>
              <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
                <View style={{ width: 7, height: 7, borderRadius: 99, backgroundColor: dot }} />
                <Text style={{ fontSize: 14, fontWeight: "600", color: t.fg }}>{simplifyName(item)}</Text>
              </View>
              {rescue && <Pill text="救援模式" color={t.warning} bg={t.surfaceMuted} />}
              {!rescue && <Pill text={item.state || "?"} color={ok ? t.success : t.danger} bg={t.surfaceMuted} />}
            </View>
            <View style={[styles.row, { marginTop: 8 }]}>
              <Text style={{ fontFamily: "Menlo", fontSize: 11, color: t.muted }}>{item.ip}</Text>
              <Text style={{ fontSize: 11, color: t.muted }}>
                {(item.datacenter || "").toUpperCase()} · 到期 {expiryOf(item)}
              </Text>
            </View>
          </Pressable>
        );
      }}
    />
  );
}

function Pill({ text, color, bg }: { text: string; color: string; bg: string }) {
  return (
    <View style={{ paddingHorizontal: 8, paddingVertical: 2.5, borderRadius: 99, backgroundColor: bg, borderWidth: 1, borderColor: color }}>
      <Text style={{ fontSize: 10, fontWeight: "600", color }}>{text}</Text>
    </View>
  );
}

/** 列表卡显示别名(web 的 server_aliases 在 name 字段里已带) */
function simplifyName(s: OwnedServer): string {
  return s.name && s.name !== s.serviceName ? s.name.split(" | ")[0] : s.serviceName;
}

function expiryOf(s: OwnedServer): string {
  // 到期日在列表接口里没有直接给;先显示续费态,详情页有完整日期
  return s.renewalType === true ? "自动续费" : s.renewalType === false ? "手动续费" : "续费未知";
}

const styles = StyleSheet.create({
  card: { borderRadius: 16, borderWidth: 1, padding: 13 },
  row: { flexDirection: "row", alignItems: "center", gap: 10, justifyContent: "space-between" },
});
