/**
 * 机器列表(主 Tab)—— 按设计稿 v3 重制:
 * 卡片三层信息(身份行 / 地址行 / 状态胶囊行),别名大字、serviceName 小字等宽,
 * 救援模式整卡琥珀描边 + 横幅,正常态绿色 ok 胶囊,异常红。
 * VPS 与独服同列表,用图标区分。
 */
import { useState } from "react";
import { ActivityIndicator, FlatList, Pressable, RefreshControl, StyleSheet, Text, View } from "react-native";
import { Server, Box, AlertTriangle, ChevronRight, Copy } from "lucide-react-native";

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

  // 单机控制台全屏覆盖,返回时列表原地保留(轮询不停,回来数据是新的)
  if (open) return <ServerDetail client={client} server={open} onBack={() => setOpen(null)} />;

  const servers = q.data?.servers ?? [];
  const rescueCount = servers.filter((s) => s.netbootMode === "rescue").length;

  return (
    <FlatList
      style={{ flex: 1 }}
      contentContainerStyle={{ padding: 16, gap: 12, paddingBottom: 24 }}
      data={servers}
      keyExtractor={(s) => s.serviceName}
      refreshControl={<RefreshControl refreshing={q.loading} tintColor={t.muted} onRefresh={q.refresh} />}
      ListHeaderComponent={
        <View style={{ marginBottom: 4 }}>
          <Text style={{ fontSize: 24, fontWeight: "700", color: t.fg, letterSpacing: 0.3 }}>机器</Text>
          <Text style={{ fontSize: 11, color: t.muted, marginTop: 3 }}>
            {servers.length} 台{rescueCount > 0 ? ` · ${rescueCount} 台在救援模式` : ""} · 上次同步 {syncText(q.loading)}
          </Text>
        </View>
      }
      ListEmptyComponent={
        q.error ? (
          <View style={[styles.emptyCard, { backgroundColor: t.surfaceMuted, borderColor: t.border }]}>
            <AlertTriangle size={18} color={t.danger} />
            <Text style={{ fontSize: 12, color: t.danger, marginTop: 8, textAlign: "center", lineHeight: 18 }}>{q.error}</Text>
          </View>
        ) : q.loading ? (
          <ActivityIndicator color={t.muted} style={{ marginTop: 40 }} />
        ) : (
          <View style={[styles.emptyCard, { backgroundColor: t.surfaceMuted, borderColor: t.border }]}>
            <Server size={18} color={t.faint} />
            <Text style={{ fontSize: 12, color: t.muted, marginTop: 8, textAlign: "center" }}>
              没有已购服务器{"\n"}在「我的」页确认账户,或去网页端查看
            </Text>
          </View>
        )
      }
      renderItem={({ item }) => <MachineCard t={t} item={item} onPress={() => setOpen(item)} />}
    />
  );
}

/** 单张机器卡:身份行(图标+别名+胶囊) / 地址行(IP mono+机房+到期) / 救援横幅 */
function MachineCard({ t, item, onPress }: { t: ReturnType<typeof useTokens>; item: OwnedServer; onPress: () => void }) {
  const rescue = item.netbootMode === "rescue";
  const stateOk = ["ok", "active"].includes((item.state || "").toLowerCase());
  const dotColor = rescue ? t.warning : stateOk ? t.success : t.danger;
  const isVps = item.commercialRange === "" && (item.serviceName || "").startsWith("vps");

  return (
    <Pressable
      style={({ pressed }) => [
        styles.card,
        { backgroundColor: t.surface, borderColor: rescue ? t.warning : t.border },
        pressed && { opacity: 0.7 },
      ]}
      onPress={onPress}
    >
      {/* 第一行:图标 + 别名(大) + 状态胶囊 + 进入箭头 */}
      <View style={styles.row}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 9, flexShrink: 1 }}>
          <View style={[styles.icon, { backgroundColor: rescue ? t.warning : stateOk ? t.success : t.danger }]}>
            {isVps ? <Box size={13} color="#FFFFFF" strokeWidth={2} /> : <Server size={13} color="#FFFFFF" strokeWidth={2} />}
          </View>
          <View style={{ flexShrink: 1 }}>
            <Text style={{ fontSize: 15, fontWeight: "700", color: t.fg }} numberOfLines={1}>
              {displayName(item)}
            </Text>
            <Text style={{ fontFamily: "Menlo", fontSize: 10, color: t.faint }} numberOfLines={1}>
              {item.serviceName}
            </Text>
          </View>
        </View>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 6 }}>
          <Pill t={t} text={rescue ? "救援模式" : (item.state || "—").toUpperCase()} tone={rescue ? "warn" : stateOk ? "ok" : "bad"} />
          <ChevronRight size={15} color={t.faint} />
        </View>
      </View>

      {/* 第二行:IP(mono,可点复制)+ 机房 + 到期/续费 */}
      <View style={[styles.row, { marginTop: 10 }]}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 6, flexShrink: 1 }}>
          <View style={{ width: 6, height: 6, borderRadius: 99, backgroundColor: dotColor }} />
          <Text style={{ fontFamily: "Menlo", fontSize: 11.5, color: t.fg }}>{item.ip}</Text>
        </View>
        <Text style={{ fontSize: 11, color: t.muted }}>
          {(item.datacenter || "—").toUpperCase()} · {expiryText(item)}
        </Text>
      </View>

      {/* 救援横幅:一句治慌的话 */}
      {rescue && (
        <View style={[styles.banner, { backgroundColor: t.warning + "14", borderColor: t.warning + "55" }]}>
          <AlertTriangle size={13} color={t.warning} />
          <Text style={{ fontSize: 10.5, color: t.fg, flex: 1, lineHeight: 15 }}>
            下次重启进入救援镜像 · 原系统数据未动 · 修完点「退出救援」
          </Text>
        </View>
      )}
    </Pressable>
  );
}

/** 状态胶囊(圆角描边,与设计稿一致) */
function Pill({ t, text, tone }: { t: ReturnType<typeof useTokens>; text: string; tone: "ok" | "warn" | "bad" }) {
  const c = tone === "ok" ? t.success : tone === "warn" ? t.warning : t.danger;
  return (
    <View style={{ paddingHorizontal: 8, paddingVertical: 3, borderRadius: 99, borderWidth: 1, borderColor: c, backgroundColor: c + "12" }}>
      <Text style={{ fontSize: 9.5, fontWeight: "700", color: c, letterSpacing: 0.3 }}>{text}</Text>
    </View>
  );
}

/** 别名优先;name 是 "KS-6 | AMD Epyc 7351P" 这种带规格的,取竖线前 */
function displayName(s: OwnedServer): string {
  if (s.name && s.name !== s.serviceName) return s.name.split(" | ")[0];
  return s.serviceName;
}

function expiryText(s: OwnedServer): string {
  return s.renewalType === true ? "自动续费" : s.renewalType === false ? "手动续费" : "续费未知";
}

function syncText(loading: boolean): string {
  return loading ? "同步中…" : "5 秒内";
}

const styles = StyleSheet.create({
  card: { borderRadius: 16, borderWidth: 1, padding: 14 },
  row: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 },
  icon: { width: 28, height: 28, borderRadius: 9, alignItems: "center", justifyContent: "center" },
  banner: { flexDirection: "row", alignItems: "center", gap: 7, borderRadius: 10, borderWidth: 1, paddingHorizontal: 9, paddingVertical: 7, marginTop: 10 },
  emptyCard: { borderRadius: 16, borderWidth: 1, padding: 20, alignItems: "center", marginTop: 24 },
});
