/**
 * 单机控制台(核心屏):状态头 + 四段 Tab(概览/电源/维护/高级)—— 与 web 对齐。
 * v1 落地读侧全量 + 电源动作;维护/高级先给入口占位(功能分批补齐,不做假按钮)。
 */
import { useState } from "react";
import { Alert, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { Zap, LifeBuoy, Monitor, Eye, ChevronLeft, Copy } from "lucide-react-native";

import type { ApiClient } from "@core/api-client";
import type { OwnedServer } from "@core/types";
import { useTokens } from "../theme/tokens";
import { useAction, usePoll } from "../api/hooks";

type Section = "overview" | "power" | "maintenance" | "advanced";

export default function ServerDetail({ client, server, onBack }: { client: ApiClient; server: OwnedServer; onBack: () => void }) {
  const t = useTokens();
  const [section, setSection] = useState<Section>("overview");
  const act = useAction(client);
  const info = usePoll<{ serviceInfo?: Record<string, unknown> }>(client, `/server-control/${server.serviceName}/serviceinfo`, 30_000);

  const rescue = server.netbootMode === "rescue";

  /** 电源动作统一走确认对话(后果写人话,与 web 同文案) */
  const confirmAct = (title: string, msg: string, path: string) => {
    Alert.alert(title, msg, [
      { text: "取消", style: "cancel" },
      {
        text: "确认",
        style: "destructive",
        onPress: async () => {
          const r = await act.run(path);
          if (!r.ok) Alert.alert("失败", r.message || "请重试");
        },
      },
    ]);
  };

  return (
    <View style={{ flex: 1, backgroundColor: t.bg }}>
      {/* 状态头 */}
      <View style={{ paddingHorizontal: 16, paddingTop: 8, gap: 4 }}>
        <Pressable onPress={onBack} style={{ flexDirection: "row", alignItems: "center", gap: 5, alignSelf: "flex-start" }}>
          <ChevronLeft size={16} color={t.muted} />
          <Text style={{ fontSize: 12, color: t.muted }}>机器</Text>
        </Pressable>
        <Text style={{ fontSize: 20, fontWeight: "700", color: t.fg }}>{server.serviceName}</Text>
        <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
          <Text style={{ fontFamily: "Menlo", fontSize: 11, color: t.muted }}>{server.ip}</Text>
          <Pressable onPress={() => { /* 复制到剪贴板 */ }} hitSlop={8}>
            <Copy size={12} color={t.faint} />
          </Pressable>
          {rescue && <Text style={{ fontSize: 11, color: t.warning, fontWeight: "600" }}>救援模式</Text>}
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
        {section === "overview" && <Overview t={t} server={server} info={info.data?.serviceInfo} />}
        {section === "power" && (
          <>
            <View style={styles.actions}>
              <ActTile t={t} Icon={Zap} label="硬重启" onPress={() =>
                confirmAct("硬重启?", "相当于按电源键强制重启,未落盘的数据会丢失;硬盘数据不受影响。", `/server-control/${server.serviceName}/reboot`)} />
              <ActTile t={t} Icon={LifeBuoy} label={rescue ? "退出救援" : "一键救援"} onPress={() =>
                confirmAct(rescue ? "退出救援模式?" : "进入救援模式?",
                  rescue ? "改回硬盘启动并重启,回到正常系统。" : "确认后立刻重启进入救援镜像;原系统数据不动,root 密码发到救援邮箱。",
                  `/server-control/${server.serviceName}/${rescue ? "boot/harddisk" : "rescue"}`)} />
              <ActTile t={t} Icon={Monitor} label="KVM 屏幕" onPress={() =>
                client.get<{ url?: string }>(`/server-control/${server.serviceName}/console`)
                  .then((r) => r.url && Alert.alert("KVM 控制台", "控制台 URL 已就绪,请在浏览器打开:\n" + r.url))
                  .catch((e) => Alert.alert("失败", e.message))} />
              <ActTile t={t} Icon={Eye} label="监控探测" onPress={() => act.run(`/server-control/${server.serviceName}/monitoring`, { monitoring: !server.monitoring })} />
            </View>
            <Text style={{ fontSize: 11, color: t.faint, lineHeight: 17 }}>
              重装系统 / 硬件更换等深水区操作在网页端完成更稳(App 的完整版按阶段排期中);本屏电源动作与 web 同接口、同确认文案。
            </Text>
          </>
        )}
        {section === "maintenance" && (
          <Placeholder t={t} text="维护:硬件更换 / 变更联系人 / 合同期 / 撤单 —— 下一批接入(数据接口后端已就绪)" />
        )}
        {section === "advanced" && (
          <Placeholder t={t} text="高级:Backup FTP / DDoS 缓解 / vRack / 虚拟 MAC —— 下一批接入" />
        )}
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

function ActTile({ t, Icon, label, onPress }: { t: ReturnType<typeof useTokens>; Icon: typeof Zap; label: string; onPress: () => void }) {
  return (
    <Pressable style={[styles.act, { backgroundColor: t.surfaceMuted, borderColor: t.border }]} onPress={onPress}>
      <Icon size={20} color={t.fg} strokeWidth={1.8} />
      <Text style={{ fontSize: 10.5, color: t.fg }}>{label}</Text>
    </Pressable>
  );
}

function Placeholder({ t, text }: { t: ReturnType<typeof useTokens>; text: string }) {
  return (
    <View style={[styles.card, { backgroundColor: t.surfaceMuted, borderColor: t.border }]}>
      <Text style={{ fontSize: 11.5, color: t.muted, lineHeight: 18 }}>{text}</Text>
    </View>
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
