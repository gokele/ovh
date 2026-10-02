/**
 * 重装系统(App 全量版):OS 模板选择 → 智能分区方案说明 → 输入机器名确认 → Face ID → 提交。
 * 与 web 同接口(POST /install);智能分区的完整编辑器在手机上不做(误触代价太高),
 * 用后端的智能方案 + 明确说明,自定义分区引导去网页端。
 */
import { useState } from "react";
import { Alert, Modal, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from "react-native";
import { X, Disc, AlertTriangle } from "lucide-react-native";

import type { ApiClient } from "@core/api-client";
import { useTokens } from "../theme/tokens";
import { useAction, usePoll } from "../api/hooks";
import { requireBiometric } from "../api/biometric";

interface OsTemplate { templateName: string; distribution: string; family: string; bitFormat: number }
interface Props { client: ApiClient; serviceName: string; onClose: () => void }

export default function ReinstallSheet({ client, serviceName, onClose }: Props) {
  const t = useTokens();
  const q = usePoll<{ templates?: OsTemplate[] }>(client, `/server-control/${serviceName}/templates`, 0);
  const hw = usePoll<Record<string, unknown>>(client, `/server-control/${serviceName}/hardware`, 0);
  const act = useAction(client);
  const [picked, setPicked] = useState<OsTemplate | null>(null);
  const [confirmName, setConfirmName] = useState("");

  const h = hw.data as { diskGroups?: Array<{ diskGroupId?: number; disks?: Array<{ size?: number; unit?: string; type?: string }> }> } | null;
  const ssdGroup = (h?.diskGroups ?? []).find((g) => (g.disks ?? [])[0]?.type?.toLowerCase() === "ssd") ?? (h?.diskGroups ?? [])[0];
  const diskText = ssdGroup ? `${(ssdGroup.disks ?? []).length}×${(ssdGroup.disks ?? [])[0]?.size ?? "?"}${(ssdGroup.disks ?? [])[0]?.unit || "GB"} ${(ssdGroup.disks ?? [])[0]?.type || ""}`.trim() : null;

  /** 提交:四闸 = 选模板 + 输机器名 + 确认对话 + Face ID */
  const submit = () => {
    if (!picked) { Alert.alert("先选系统", "从上面的列表选一个模板"); return; }
    if (confirmName.trim() !== serviceName) {
      Alert.alert("机器名不匹配", `请输入完整的机器名以确认:${serviceName}`);
      return;
    }
    Alert.alert(
      "重装系统 — 不可逆",
      `${picked.distribution}(${picked.templateName})\n清空系统盘所有数据。`,
      [
        { text: "取消", style: "cancel" },
        { text: "确认重装", style: "destructive", onPress: doInstall },
      ],
    );
  };

  const doInstall = async () => {
    const bio = await requireBiometric("重装系统");
    if (!bio.ok) { Alert.alert("未执行", bio.reason || "验证未通过"); return; }
    // 与 web 同 payload:不带 storageConfig = 后端用智能分区方案(最快磁盘组 + RAID1)
    const r = await act.run(`/server-control/${serviceName}/install`, { templateName: picked!.templateName });
    if (!r.ok) { Alert.alert("失败", r.message); return; }
    Alert.alert("重装任务已提交", "通常 5-10 分钟,进度在概览页的任务卡查看");
    onClose();
  };

  const inputStyle = { borderColor: t.border, backgroundColor: t.surfaceMuted, color: t.fg };

  return (
    <Modal visible transparent animationType="slide" onRequestClose={onClose}>
      <View style={styles.backdrop}>
        <View style={[styles.sheet, { backgroundColor: t.surface }]}>
          <View style={[styles.head, { borderBottomColor: t.border }]}>
            <View style={{ flexDirection: "row", alignItems: "center", gap: 7 }}>
              <Disc size={16} color={t.danger} />
              <Text style={{ fontSize: 15, fontWeight: "700", color: t.fg }}>重装系统</Text>
            </View>
            <Pressable onPress={onClose} hitSlop={10}><X size={18} color={t.muted} /></Pressable>
          </View>
          <ScrollView style={{ maxHeight: 460 }} contentContainerStyle={{ padding: 14, gap: 12 }}>
            {/* 不可逆警告 */}
            <View style={[styles.warn, { backgroundColor: t.danger + "0D", borderColor: t.danger + "44" }]}>
              <AlertTriangle size={14} color={t.danger} />
              <Text style={{ fontSize: 11, color: t.fg, flex: 1, lineHeight: 16 }}>
                清空系统盘所有数据,不可逆。默认使用智能分区方案{diskText ? `(装到 ${diskText})` : ""};自定义分区请到网页端。
              </Text>
            </View>

            {/* 模板列表 */}
            <Text style={{ fontSize: 12, fontWeight: "600", color: t.fg }}>选择系统</Text>
            {q.error ? (
              <Text style={{ fontSize: 12, color: t.danger }}>{q.error}</Text>
            ) : q.loading ? (
              <Text style={{ fontSize: 12, color: t.muted }}>模板列表读取中…</Text>
            ) : (
              <View style={{ gap: 6 }}>
                {(q.data?.templates ?? []).slice(0, 30).map((tpl) => {
                  const on = picked?.templateName === tpl.templateName;
                  return (
                    <Pressable
                      key={tpl.templateName}
                      style={[styles.tpl, { borderColor: on ? t.fg : t.border, backgroundColor: on ? t.surfaceMuted : t.surface }]}
                      onPress={() => setPicked(tpl)}
                    >
                      <View style={{ flex: 1 }}>
                        <Text style={{ fontSize: 12.5, fontWeight: "600", color: t.fg }}>{tpl.distribution}</Text>
                        <Text style={{ fontFamily: "Menlo", fontSize: 10, color: t.muted, marginTop: 2 }}>{tpl.templateName} · {tpl.bitFormat}-bit</Text>
                      </View>
                      {on && <View style={{ width: 16, height: 16, borderRadius: 99, backgroundColor: t.fg, alignItems: "center", justifyContent: "center" }}><View style={{ width: 6, height: 6, borderRadius: 99, backgroundColor: t.surface }} /></View>}
                    </Pressable>
                  );
                })}
              </View>
            )}

            {/* 输入机器名 */}
            <Text style={{ fontSize: 12, fontWeight: "600", color: t.fg }}>输入机器名确认</Text>
            <TextInput
              style={[styles.input, inputStyle]}
              placeholder={serviceName}
              placeholderTextColor={t.faint}
              autoCapitalize="none"
              autoCorrect={false}
              value={confirmName}
              onChangeText={setConfirmName}
            />

            <Pressable style={[styles.btn, { backgroundColor: t.danger }]} onPress={submit} disabled={act.pending}>
              <Text style={{ color: "#FFFFFF", fontSize: 15, fontWeight: "600" }}>{act.pending ? "提交中…" : "面容确认并重装"}</Text>
            </Pressable>
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: { flex: 1, backgroundColor: "rgba(10,10,10,0.4)", justifyContent: "flex-end" },
  sheet: { borderTopLeftRadius: 22, borderTopRightRadius: 22, borderWidth: 1, borderColor: "#00000010" },
  head: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", paddingHorizontal: 16, paddingVertical: 14, borderBottomWidth: StyleSheet.hairlineWidth },
  warn: { flexDirection: "row", alignItems: "flex-start", gap: 8, borderRadius: 11, borderWidth: 1, padding: 10 },
  tpl: { flexDirection: "row", alignItems: "center", gap: 8, borderRadius: 12, borderWidth: 1, padding: 11 },
  input: { height: 44, borderRadius: 11, borderWidth: 1, paddingHorizontal: 13, fontSize: 13, fontFamily: "Menlo" },
  btn: { height: 47, borderRadius: 14, alignItems: "center", justifyContent: "center", marginTop: 4 },
});
