/**
 * 单机控制台(核心屏):状态头 + 四段 Tab(概览/电源/维护/高级)—— 与 web 对齐。
 * v1 落地读侧全量 + 电源动作;维护/高级先给入口占位(功能分批补齐,不做假按钮)。
 */
import { useState } from "react";
import { Alert, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { Zap, LifeBuoy, Monitor, Eye, ChevronLeft, Copy } from "lucide-react-native";
import * as WebBrowser from "expo-web-browser";
import * as Clipboard from "expo-clipboard";

import type { ApiClient } from "@core/api-client";
import type { OwnedServer } from "@core/types";
import { useTokens } from "../theme/tokens";
import { useAction, usePoll } from "../api/hooks";
import ServerDetailMaintenance from "./ServerDetailMaintenance";
import ServerDetailAdvanced from "./ServerDetailAdvanced";
import MrtgSpark from "../components/MrtgSpark";
import { requireBiometric } from "../api/biometric";

type Section = "overview" | "power" | "maintenance" | "advanced";

export default function ServerDetail({ client, server, onBack }: { client: ApiClient; server: OwnedServer; onBack: () => void }) {
  const t = useTokens();
  const [section, setSection] = useState<Section>("overview");
  const act = useAction(client);
  const info = usePoll<{ serviceInfo?: Record<string, unknown> }>(client, `/server-control/${server.serviceName}/serviceinfo`, 30_000);

  const rescue = server.netbootMode === "rescue";

  /** 危险动作三闸:确认对话(人话后果)→ Face ID → 执行。与 web 同文案,加一层生物识别 */
  const confirmAct = (title: string, msg: string, path: string, body?: unknown) => {
    Alert.alert(title, msg, [
      { text: "取消", style: "cancel" },
      {
        text: "确认",
        style: "destructive",
        onPress: async () => {
          const bio = await requireBiometric(title);
          if (!bio.ok) {
            Alert.alert("未执行", bio.reason || "验证未通过");
            return;
          }
          const r = await act.run(path, body);
          if (!r.ok) Alert.alert("失败", r.message || "请重试");
        },
      },
    ]);
  };

  /** KVM:拉会话 URL(后端要轮询 OVH 任务,约 20 秒)并用系统浏览器打开 */
  const openKvm = async () => {
    Alert.alert("正在申请 KVM 会话…", "OVH 侧需要约 20 秒,弹窗关闭后请稍候", [{ text: "知道了" }]);
    try {
      const r = await client.get<{ url?: string }>(`/server-control/${server.serviceName}/console`);
      if (r.url) await WebBrowser.openBrowserAsync(r.url);
      else Alert.alert("失败", "后端没有返回控制台 URL");
    } catch (e) {
      Alert.alert("失败", e instanceof Error ? e.message : "获取控制台失败");
    }
  };

  return (
    <View style={{ flex: 1, backgroundColor: t.bg }}>
      {/* 状态头:返回 → 别名大字+状态点 → serviceName 小字 → IP 行(可复制)→ 胶囊条(设计稿 v3) */}
      <View style={{ paddingHorizontal: 16, paddingTop: 6, gap: 5 }}>
        <Pressable onPress={onBack} style={{ flexDirection: "row", alignItems: "center", gap: 5, alignSelf: "flex-start" }}>
          <ChevronLeft size={16} color={t.muted} />
          <Text style={{ fontSize: 12, color: t.muted }}>机器</Text>
        </Pressable>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 9 }}>
          <View style={{ width: 9, height: 9, borderRadius: 99, backgroundColor: rescue ? t.warning : t.success }} />
          <Text style={{ fontSize: 21, fontWeight: "700", color: t.fg, flexShrink: 1 }} numberOfLines={1}>
            {server.name && server.name !== server.serviceName ? server.name.split(" | ")[0] : server.serviceName}
          </Text>
        </View>
        <Text style={{ fontFamily: "Menlo", fontSize: 10.5, color: t.faint }}>{server.serviceName}</Text>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 7, marginTop: 2 }}>
          <Text style={{ fontFamily: "Menlo", fontSize: 12, color: t.muted }}>{server.ip}</Text>
          <Pressable
            onPress={() => {
              Clipboard.setStringAsync(server.ip);
              Alert.alert("已复制", server.ip);
            }}
            hitSlop={8}
            style={{ flexDirection: "row", alignItems: "center", gap: 3 }}
          >
            <Copy size={11} color={t.faint} />
            <Text style={{ fontSize: 10, color: t.faint }}>复制</Text>
          </Pressable>
        </View>
        {/* 胶囊条:状态/机房/续费 —— web 端服务信息条的移动版 */}
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 6, marginTop: 4 }}>
          {rescue && <InfoPill t={t} text="救援模式" tone="warn" />}
          <InfoPill t={t} text={(server.state || "—").toUpperCase()} tone={rescue ? "warn" : "ok"} />
          <InfoPill t={t} text={(server.datacenter || "—").toUpperCase() + " 机房"} tone="mut" />
          <InfoPill t={t} text={server.renewalType === true ? "续费 自动" : server.renewalType === false ? "续费 手动" : "续费 未知"} tone="mut" />
        </View>
      </View>

      {/* 四段 Tab(与 web 同名同序) */}
      <View style={[styles.seg, { backgroundColor: t.surfaceMuted, borderColor: t.border }]}>
        {([["overview", "概览"], ["power", "电源"], ["maintenance", "维护"], ["advanced", "高级"]] as const).map(([id, label]) => (
          <Pressable
            key={id}
            onPress={() => setSection(id)}
            style={[styles.segItem, section === id && { backgroundColor: t.surface, borderColor: t.border }]}
          >
            <Text style={{ fontSize: 11, fontWeight: section === id ? "600" : "400", color: section === id ? t.fg : t.muted }}>{label}</Text>
          </Pressable>
        ))}
      </View>

      <ScrollView contentContainerStyle={{ padding: 16, gap: 10, paddingBottom: 30 }}>
        {section === "overview" && (
          <>
            <Overview t={t} server={server} info={info.data?.serviceInfo} />
            <MrtgSpark client={client} serviceName={server.serviceName} />
          </>
        )}
        {section === "power" && (
          <>
            <View style={styles.actions}>
              <ActTile t={t} Icon={Zap} label="硬重启" onPress={() =>
                confirmAct("硬重启?", "相当于按电源键强制重启,未落盘的数据会丢失;硬盘数据不受影响。", `/server-control/${server.serviceName}/reboot`)} />
              <ActTile t={t} Icon={LifeBuoy} label={rescue ? "退出救援" : "一键救援"} onPress={() =>
                rescue
                  ? confirmAct("退出救援模式?", "改回硬盘启动并重启,回到正常系统。", `/server-control/${server.serviceName}/rescue/exit`, { confirm: true })
                  : confirmAct(
                      "进入救援模式?",
                      "确认后立刻重启进入救援镜像;原系统数据不动,root 密码发到救援邮箱(下面填,留空用 OVH 账户邮箱)。",
                      `/server-control/${server.serviceName}/rescue`,
                      { confirm: true },
                    )} />
              <ActTile t={t} Icon={Monitor} label="KVM 屏幕" onPress={openKvm} />
              <ActTile t={t} Icon={Eye} label="监控探测" onPress={() => act.put(`/server-control/${server.serviceName}/monitoring`, { enabled: !server.monitoring, monitoring: !server.monitoring })} />
            </View>
            <Text style={{ fontSize: 11, color: t.faint, lineHeight: 17 }}>
              重装系统 / 硬件更换等深水区操作在网页端完成更稳(App 的完整版按阶段排期中);本屏电源动作与 web 同接口、同确认文案。
            </Text>
          </>
        )}
        {section === "maintenance" && <ServerDetailMaintenance client={client} serviceName={server.serviceName} />}
        {section === "advanced" && <ServerDetailAdvanced client={client} serviceName={server.serviceName} />}
      </ScrollView>
    </View>
  );
}

/** 概览:硬件规格 + 服务信息(胶囊条的文本版) */
function Overview({ t, server, info }: { t: ReturnType<typeof useTokens>; server: OwnedServer; info?: Record<string, unknown> }) {
  const expiration = typeof info?.expiration === "string" ? info.expiration.slice(0, 10) : "—";
  const rows: Array<[string, string]> = [
    ["处理器", server.name.includes("|") ? server.name.split("|")[1]?.trim() || "—" : "—"],
    ["机房", (server.datacenter || "").toUpperCase()],
    ["IP", server.ip],
    ["OS", server.os || "—"],
    ["到期", expiration],
    ["续费", server.renewalType === true ? "自动续费" : server.renewalType === false ? "手动续费" : "未知"],
  ];
  return (
    <View style={[styles.card, { backgroundColor: t.surface, borderColor: t.border }]}>
      {rows.map(([k, v]) => (
        <View key={k} style={styles.kv}>
          <Text style={{ fontSize: 11.5, color: t.muted }}>{k}</Text>
          <Text style={{ fontFamily: "Menlo", fontSize: 11.5, color: t.fg, textAlign: "right", flexShrink: 1 }}>{v}</Text>
        </View>
      ))}
    </View>
  );
}

/** 信息胶囊:状态/机房/续费的轻量展示(描边式,非彩色大块) */
function InfoPill({ t, text, tone }: { t: ReturnType<typeof useTokens>; text: string; tone: "ok" | "warn" | "mut" }) {
  const c = tone === "ok" ? t.success : tone === "warn" ? t.warning : t.muted;
  return (
    <View style={{ paddingHorizontal: 8, paddingVertical: 3, borderRadius: 99, borderWidth: 1, borderColor: tone === "mut" ? t.border : c, backgroundColor: tone === "mut" ? t.surfaceMuted : c + "12" }}>
      <Text style={{ fontSize: 9.5, fontWeight: "600", color: tone === "mut" ? t.muted : c }}>{text}</Text>
    </View>
  );
}

function ActTile({ t, Icon, label, onPress }: { t: ReturnType<typeof useTokens>; Icon: typeof Zap; label: string; onPress: () => void }) {
  return (
    <Pressable style={[styles.act, { backgroundColor: t.surfaceMuted, borderColor: t.border }]} onPress={onPress}>
      <Icon size={20} color={t.fg} strokeWidth={1.8} />
      <Text style={{ fontSize: 10.5, color: t.fg }}>{label}</Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  seg: { flexDirection: "row", marginHorizontal: 16, marginTop: 10, borderRadius: 11, borderWidth: 1, padding: 2.5, gap: 2 },
  segItem: { flex: 1, alignItems: "center", paddingVertical: 5, borderRadius: 8, borderWidth: 1, borderColor: "transparent" },
  actions: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
  act: { width: "31%", aspectRatio: 0.95, borderRadius: 13, borderWidth: 1, alignItems: "center", justifyContent: "center", gap: 6 },
  card: { borderRadius: 16, borderWidth: 1, padding: 13 },
  kv: { flexDirection: "row", justifyContent: "space-between", paddingVertical: 4, gap: 12 },
});
