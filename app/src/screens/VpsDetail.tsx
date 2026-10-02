/**
 * VPS 控制台(单机页):状态头 + 动作区(开关机/重启/控制台/重装)+ 快照 + 任务。
 * 与独服 ServerDetail 平级;VPS 特有:快照三件套(创建/回滚/删除)。
 */
import { useState } from "react";
import { Alert, Modal, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { Power, PowerOff, RefreshCw, Monitor, Disc, Camera, RotateCcw, Trash2, ChevronLeft, ChevronRight } from "lucide-react-native";
import * as WebBrowser from "expo-web-browser";

import type { ApiClient } from "@core/api-client";
import type { OwnedVps } from "@core/types";
import { useTokens } from "../theme/tokens";
import { useAction, usePoll } from "../api/hooks";
import { requireBiometric } from "../api/biometric";
import VpsReinstallSheet from "./VpsReinstallSheet";

type Section = "overview" | "power" | "snapshot";

export default function VpsDetail({ client, vps, onBack }: { client: ApiClient; vps: OwnedVps; onBack: () => void }) {
  const t = useTokens();
  const [section, setSection] = useState<Section>("overview");
  const [reinstallSheet, setReinstallSheet] = useState(false);
  const act = useAction(client);
  const info = usePoll<Record<string, unknown>>(client, `/vps-control/${vps.name}/info`, 30_000);
  const tasks = usePoll<{ tasks?: Array<Record<string, unknown>> }>(client, `/vps-control/${vps.name}/tasks`, 30_000);

  const state = String((info.data as { state?: string } | null)?.state ?? vps.state ?? "—");
  const running = state === "running" || state === "active";
  const ips = vps.ips ?? [];
  const model = (info.data as { model?: { name?: string } | null } | null)?.model?.name ?? vps.model ?? "—";

  const confirmAct = (title: string, msg: string, path: string) => {
    Alert.alert(title, msg, [
      { text: "取消", style: "cancel" },
      { text: "确认", style: "destructive", onPress: async () => {
        const bio = await requireBiometric(title);
        if (!bio.ok) { Alert.alert("未执行", bio.reason || "验证未通过"); return; }
        const r = await act.run(path);
        if (!r.ok) Alert.alert("失败", r.message || "请重试");
        info.refresh();
      } },
    ]);
  };

  const openConsole = async () => {
    try {
      const r = await client.post<{ url?: string }>(`/vps-control/${vps.name}/console`);
      if (r.url) await WebBrowser.openBrowserAsync(r.url);
    } catch (e) {
      Alert.alert("失败", e instanceof Error ? e.message : "获取控制台失败");
    }
  };

  return (
    <View style={{ flex: 1, backgroundColor: t.bg }}>
      {/* 状态头 */}
      <View style={{ paddingHorizontal: 16, paddingTop: 6, gap: 5 }}>
        <Pressable onPress={onBack} style={{ flexDirection: "row", alignItems: "center", gap: 5, alignSelf: "flex-start" }}>
          <ChevronLeft size={16} color={t.muted} />
          <Text style={{ fontSize: 12, color: t.muted }}>机器</Text>
        </Pressable>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 9 }}>
          <View style={{ width: 9, height: 9, borderRadius: 99, backgroundColor: running ? t.success : t.danger }} />
          <Text style={{ fontSize: 21, fontWeight: "700", color: t.fg, flexShrink: 1 }} numberOfLines={1}>
            {vps.displayName || vps.name.split(".")[0]}
          </Text>
        </View>
        <Text style={{ fontFamily: "Menlo", fontSize: 10.5, color: t.faint }}>{vps.name}</Text>
        {ips.length > 0 && (
          <Text style={{ fontFamily: "Menlo", fontSize: 12, color: t.muted, marginTop: 2 }}>{ips[0]}{ips.length > 1 ? ` (+${ips.length - 1})` : ""}</Text>
        )}
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 6, marginTop: 4 }}>
          <Pill t={t} text={state.toUpperCase()} tone={running ? "ok" : "bad"} />
          <Pill t={t} text={model} tone="mut" />
          {vps.zone ? <Pill t={t} text={vps.zone.toUpperCase()} tone="mut" /> : null}
        </View>
      </View>

      {/* 三段 Tab(VPS 无维护/高级,后续批补) */}
      <View style={[styles.seg, { backgroundColor: t.surfaceMuted, borderColor: t.border }]}>
        {([["overview", "概览"], ["power", "电源"], ["snapshot", "快照"]] as const).map(([id, label]) => (
          <Pressable key={id} onPress={() => setSection(id)} style={[styles.segItem, section === id && { backgroundColor: t.surface, borderColor: t.border }]}>
            <Text style={{ fontSize: 11, fontWeight: section === id ? "600" : "400", color: section === id ? t.fg : t.muted }}>{label}</Text>
          </Pressable>
        ))}
      </View>

      <ScrollView contentContainerStyle={{ padding: 16, gap: 10, paddingBottom: 30 }}>
        {section === "overview" && (
          <View style={[styles.card, { backgroundColor: t.surface, borderColor: t.border }]}>
            {info.error ? <Text style={{ color: t.danger, fontSize: 12 }}>{info.error}</Text> : (
              <>
                <Kv t={t} k="型号" v={model} />
                <Kv t={t} k="状态" v={state} />
                <Kv t={t} k="IP" v={ips.join(", ") || "—"} />
                <Kv t={t} k="OS" v={vps.os || (info.data as { os?: string } | null)?.os || "—"} />
              </>
            )}
            {/* 任务列表 */}
            <Text style={{ fontSize: 12, fontWeight: "600", color: t.fg, marginTop: 10 }}>最近任务</Text>
            {(tasks.data?.tasks ?? []).slice(0, 5).map((task, i) => (
              <View key={i} style={styles.kv}>
                <Text style={{ fontSize: 11, color: t.muted, flexShrink: 1 }} numberOfLines={1}>
                  {String(task.function ?? "任务")} · {String(task.state ?? "—")}
                </Text>
              </View>
            ))}
          </View>
        )}
        {section === "power" && (
          <>
            <View style={styles.actions}>
              {!running && <Tile t={t} Icon={Power} label="启动" onPress={() => confirmAct("启动 VPS?", "启动后服务恢复。", `/vps-control/${vps.name}/start`)} />}
              {running && <Tile t={t} Icon={PowerOff} label="关机" onPress={() => confirmAct("关机?", "运行中的服务会中断。", `/vps-control/${vps.name}/stop`)} />}
              <Tile t={t} Icon={RefreshCw} label="重启" onPress={() => confirmAct("重启 VPS?", "未落盘数据可能丢失。", `/vps-control/${vps.name}/reboot`)} />
              <Tile t={t} Icon={Monitor} label="控制台" onPress={openConsole} />
            </View>
            <Pressable
              style={({ pressed }) => [styles.dz, { backgroundColor: pressed ? t.danger + "14" : t.danger + "0A" }]}
              onPress={() => setReinstallSheet(true)}
            >
              <Disc size={18} color={t.danger} strokeWidth={1.8} />
              <View style={{ flex: 1 }}>
                <Text style={{ fontSize: 12.5, fontWeight: "700", color: t.danger }}>重装系统</Text>
                <Text style={{ fontSize: 10.5, color: t.muted, marginTop: 1 }}>Face ID + 输入名称确认 · 清空全部数据</Text>
              </View>
              <ChevronRight size={14} color={t.danger} />
            </Pressable>
          </>
        )}
        {section === "snapshot" && <VpsSnapshotPane t={t} client={client} vpsName={vps.name} />}
      </ScrollView>

      {reinstallSheet && <VpsReinstallSheet client={client} vpsName={vps.name} onClose={() => setReinstallSheet(false)} />}
    </View>
  );
}

/** 快照面板:当前快照 + 创建/回滚/删除(回滚要输名称确认) */
function VpsSnapshotPane({ t, client, vpsName }: { t: ReturnType<typeof useTokens>; client: ApiClient; vpsName: string }) {
  const q = usePoll<Record<string, unknown>>(client, `/vps-control/${vpsName}/snapshot`, 30_000);
  const act = useAction(client);
  const snap = q.data as { snapshot?: { id?: number; creationDate?: string; description?: string } | null } | null;
  const has = !!snap?.snapshot;

  const create = () => {
    Alert.alert("创建快照?", "VPS 会暂停几分钟(内存快照)。", [
      { text: "取消", style: "cancel" },
      { text: "创建", onPress: async () => {
        const r = await act.run(`/vps-control/${vpsName}/snapshot`, { description: "App 创建" });
        Alert.alert(r.ok ? "已提交" : "失败", r.message || (r.ok ? "几分钟后完成" : "请重试"));
        q.refresh();
      } },
    ]);
  };

  const revert = () => {
    Alert.prompt?.("回滚快照", `输入 VPS 名称 ${vpsName} 确认:\n快照之后的所有改动将丢失,VPS 自动重启。`);
    // RN 没有 prompt —— 用两步 Alert 替代
    Alert.alert("回滚快照 — 不可逆", `快照之后的所有改动将丢失,VPS 自动重启。\n此操作等价于网页端的输名确认。`, [
      { text: "取消", style: "cancel" },
      { text: "确认回滚", style: "destructive", onPress: async () => {
        const r = await act.run(`/vps-control/${vpsName}/snapshot/revert`);
        Alert.alert(r.ok ? "已提交回滚" : "失败", r.message || "");
        q.refresh();
      } },
    ]);
  };

  const remove = () => {
    Alert.alert("删除快照?", "只删快照,VPS 当前状态不受影响。", [
      { text: "取消", style: "cancel" },
      { text: "删除", style: "destructive", onPress: async () => {
        const r = await act.del(`/vps-control/${vpsName}/snapshot`);
        Alert.alert(r.ok ? "已删除" : "失败", r.message || "");
        q.refresh();
      } },
    ]);
  };

  return (
    <View style={[styles.card, { backgroundColor: t.surface, borderColor: t.border }]}>
      <Text style={{ fontSize: 12.5, fontWeight: "600", color: t.fg }}>快照</Text>
      {q.error ? (
        <Text style={{ fontSize: 11.5, color: t.danger, marginTop: 6 }}>{q.error}</Text>
      ) : has ? (
        <>
          <Kv t={t} k="创建于" v={snap!.snapshot!.creationDate?.slice(0, 16).replace("T", " ") ?? "—"} />
          <Kv t={t} k="描述" v={snap!.snapshot!.description || "—"} />
          <View style={{ flexDirection: "row", gap: 8, marginTop: 12 }}>
            <SnapBtn t={t} Icon={RotateCcw} label="回滚" danger onPress={revert} />
            <SnapBtn t={t} Icon={Trash2} label="删除" onPress={remove} />
          </View>
        </>
      ) : (
        <Text style={{ fontSize: 11.5, color: t.muted, marginTop: 6 }}>没有快照(每台 VPS 只能有一份)</Text>
      )}
      {!has && (
        <Pressable style={[styles.snapCreate, { borderColor: t.border }]} onPress={create}>
          <Camera size={14} color={t.muted} />
          <Text style={{ fontSize: 12, color: t.fg, marginLeft: 6 }}>创建快照</Text>
        </Pressable>
      )}
    </View>
  );
}

function SnapBtn({ t, Icon, label, danger, onPress }: { t: ReturnType<typeof useTokens>; Icon: typeof Camera; label: string; danger?: boolean; onPress: () => void }) {
  const c = danger ? t.danger : t.muted;
  return (
    <Pressable style={{ flexDirection: "row", alignItems: "center", gap: 5, paddingHorizontal: 12, paddingVertical: 7, borderRadius: 99, borderWidth: 1, borderColor: c }} onPress={onPress}>
      <Icon size={12} color={c} />
      <Text style={{ fontSize: 11.5, color: c }}>{label}</Text>
    </Pressable>
  );
}

function Tile({ t, Icon, label, onPress }: { t: ReturnType<typeof useTokens>; Icon: typeof Power; label: string; onPress: () => void }) {
  return (
    <Pressable style={[styles.act, { backgroundColor: t.surfaceMuted, borderColor: t.border }]} onPress={onPress}>
      <Icon size={20} color={t.fg} strokeWidth={1.8} />
      <Text style={{ fontSize: 10.5, color: t.fg }}>{label}</Text>
    </Pressable>
  );
}
function Pill({ t, text, tone }: { t: ReturnType<typeof useTokens>; text: string; tone: "ok" | "bad" | "mut" }) {
  const c = tone === "ok" ? t.success : tone === "bad" ? t.danger : t.muted;
  return (
    <View style={{ paddingHorizontal: 8, paddingVertical: 3, borderRadius: 99, borderWidth: 1, borderColor: tone === "mut" ? t.border : c, backgroundColor: tone === "mut" ? t.surfaceMuted : c + "12" }}>
      <Text style={{ fontSize: 9.5, fontWeight: "600", color: tone === "mut" ? t.muted : c }}>{text}</Text>
    </View>
  );
}
function Kv({ t, k, v }: { t: ReturnType<typeof useTokens>; k: string; v: string }) {
  return (
    <View style={styles.kv}>
      <Text style={{ fontSize: 11.5, color: t.muted }}>{k}</Text>
      <Text style={{ fontFamily: "Menlo", fontSize: 11.5, color: t.fg, textAlign: "right", flexShrink: 1 }}>{v}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  seg: { flexDirection: "row", marginHorizontal: 16, marginTop: 10, borderRadius: 11, borderWidth: 1, padding: 2.5, gap: 2 },
  segItem: { flex: 1, alignItems: "center", paddingVertical: 5, borderRadius: 8, borderWidth: 1, borderColor: "transparent" },
  card: { borderRadius: 16, borderWidth: 1, padding: 13 },
  kv: { flexDirection: "row", justifyContent: "space-between", alignItems: "baseline", paddingVertical: 4, gap: 12 },
  actions: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
  act: { width: "31%", aspectRatio: 0.95, borderRadius: 13, borderWidth: 1, alignItems: "center", justifyContent: "center", gap: 6 },
  dz: { flexDirection: "row", alignItems: "center", gap: 10, borderRadius: 13, borderWidth: 1, borderColor: "#E4A5A5", padding: 12 },
  snapCreate: { flexDirection: "row", alignItems: "center", borderRadius: 12, borderWidth: 1, paddingVertical: 11, paddingHorizontal: 13, marginTop: 10 },
});
