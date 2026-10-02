/**
 * 启动模式切换(netboot):列出这台机器的 boot 项(harddisk/network/rescue...),
 * 选中即 PUT boot-mode,随后弹「是否立即重启生效」—— 官方流程是切换必须重启才生效。
 */
import { Alert, Modal, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { X, Check, RefreshCw } from "lucide-react-native";

import type { ApiClient } from "@core/api-client";
import { useTokens } from "../theme/tokens";
import { useAction, usePoll } from "../api/hooks";

interface BootMode { id: number; bootType: string; description: string; kernel: string; active: boolean; error?: string }
interface Props { client: ApiClient; serviceName: string; onClose: () => void }

export default function BootModeSheet({ client, serviceName, onClose }: Props) {
  const t = useTokens();
  const q = usePoll<{ bootModes?: BootMode[] }>(client, `/server-control/${serviceName}/boot-mode`, 0);
  const act = useAction(client);
  const modes = q.data?.bootModes ?? [];

  /** 选一项 → PUT → 问是否立即重启(切换不重启不生效,官方流程) */
  const pick = (m: BootMode) => {
    if (m.active || act.pending) return;
    Alert.alert(`切换到 ${m.bootType}?`, `${m.description || m.kernel || ""}\n\n切换启动模式需要重启才生效。`, [
      { text: "取消", style: "cancel" },
      { text: "只切换,稍后自己重启", onPress: () => apply(m, false) },
      { text: "切换并立即重启", style: "destructive", onPress: () => apply(m, true) },
    ]);
  };

  const apply = async (m: BootMode, reboot: boolean) => {
    const r = await act.put(`/server-control/${serviceName}/boot-mode`, { bootId: m.id });
    if (!r.ok) { Alert.alert("失败", r.message); return; }
    if (reboot) {
      const r2 = await act.run(`/server-control/${serviceName}/reboot`);
      if (!r2.ok) { Alert.alert("启动模式已切换,但重启失败", r2.message); onClose(); return; }
    }
    Alert.alert("完成", reboot ? "已切换并提交重启" : "已切换,重启后生效");
    q.refresh();
    onClose();
  };

  return (
    <Modal visible transparent animationType="slide" onRequestClose={onClose}>
      <View style={styles.backdrop}>
        <View style={[styles.sheet, { backgroundColor: t.surface }]}>
          <View style={[styles.head, { borderBottomColor: t.border }]}>
            <Text style={{ fontSize: 15, fontWeight: "700", color: t.fg }}>启动模式</Text>
            <Pressable onPress={onClose} hitSlop={10}><X size={18} color={t.muted} /></Pressable>
          </View>
          <ScrollView style={{ maxHeight: 420 }}>
            {q.error ? (
              <Text style={{ fontSize: 12, color: t.danger, padding: 16 }}>{q.error}</Text>
            ) : q.loading ? (
              <Text style={{ fontSize: 12, color: t.muted, padding: 16 }}>读取中…</Text>
            ) : modes.length === 0 ? (
              <Text style={{ fontSize: 12, color: t.muted, padding: 16 }}>没有可用的启动项</Text>
            ) : (
              modes.map((m) => (
                <Pressable
                  key={m.id}
                  style={({ pressed }) => [styles.item, { borderColor: m.active ? t.fg : t.border }, pressed && { opacity: 0.6 }]}
                  onPress={() => pick(m)}
                >
                  <View style={{ flex: 1 }}>
                    <View style={{ flexDirection: "row", alignItems: "center", gap: 6 }}>
                      <Text style={{ fontFamily: "Menlo", fontSize: 12.5, fontWeight: "600", color: t.fg }}>{m.bootType}</Text>
                      {m.active && <View style={{ paddingHorizontal: 6, paddingVertical: 1.5, borderRadius: 99, backgroundColor: t.success + "18" }}><Text style={{ fontSize: 9, color: t.success, fontWeight: "700" }}>当前</Text></View>}
                    </View>
                    <Text style={{ fontSize: 10.5, color: t.muted, marginTop: 3 }} numberOfLines={1}>{m.description || m.kernel || "—"}</Text>
                  </View>
                  {m.active ? <Check size={15} color={t.success} /> : <View style={{ width: 15 }} />}
                </Pressable>
              ))
            )}
          </ScrollView>
          <Text style={{ fontSize: 10.5, color: t.faint, paddingHorizontal: 16, paddingVertical: 10, lineHeight: 15 }}>
            切换后需重启生效;救援模式建议用「一键救援」入口(它会带救援邮箱)
          </Text>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: { flex: 1, backgroundColor: "rgba(10,10,10,0.4)", justifyContent: "flex-end" },
  sheet: { borderTopLeftRadius: 22, borderTopRightRadius: 22, borderWidth: 1, borderColor: "#00000010" },
  head: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", paddingHorizontal: 16, paddingVertical: 14, borderBottomWidth: StyleSheet.hairlineWidth },
  item: { flexDirection: "row", alignItems: "center", gap: 10, marginHorizontal: 12, marginTop: 8, borderRadius: 13, borderWidth: 1, padding: 12 },
});
