/**
 * VPS 重装(镜像制):模板(images 列表)→ SSH key 可选 → 输名确认 → Face ID → POST /reinstall。
 * 契约:body { templateId: string(imageId), sshKey?: string[](单 key), doNotSendPassword?: bool }
 */
import { useState } from "react";
import { Alert, Modal, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from "react-native";
import { X, Disc } from "lucide-react-native";

import type { ApiClient } from "@core/api-client";
import { useTokens } from "../theme/tokens";
import { useAction, usePoll } from "../api/hooks";
import { requireBiometric } from "../api/biometric";

interface Props { client: ApiClient; vpsName: string; onClose: () => void }

export default function VpsReinstallSheet({ client, vpsName, onClose }: Props) {
  const t = useTokens();
  const q = usePoll<{ templates?: Array<{ id: string | number; name: string; distribution?: string }> }>(client, `/vps-control/${vpsName}/templates`, 0);
  const act = useAction(client);
  const [picked, setPicked] = useState<{ id: string | number; name: string } | null>(null);
  const [sshKey, setSshKey] = useState("");
  const [noMail, setNoMail] = useState(false);
  const [confirmName, setConfirmName] = useState("");

  const submit = () => {
    if (!picked) { Alert.alert("先选系统", "从列表选一个镜像"); return; }
    if (confirmName.trim() !== vpsName) { Alert.alert("名称不匹配", `输入完整名称确认:${vpsName}`); return; }
    Alert.alert("重装 VPS — 不可逆", `${picked.name}\n清空全部数据。`, [
      { text: "取消", style: "cancel" },
      { text: "确认重装", style: "destructive", onPress: doInstall },
    ]);
  };

  const doInstall = async () => {
    const bio = await requireBiometric("重装 VPS");
    if (!bio.ok) { Alert.alert("未执行", bio.reason || "验证未通过"); return; }
    const body: Record<string, unknown> = { templateId: picked!.id, doNotSendPassword: noMail };
    const k = sshKey.trim();
    if (k) body.sshKey = [k]; // rebuild 契约是数组,后端取第一个
    const r = await act.run(`/vps-control/${vpsName}/reinstall`, body);
    if (!r.ok) { Alert.alert("失败", r.message); return; }
    Alert.alert("重装任务已提交", "通常 5-10 分钟完成");
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
              <Text style={{ fontSize: 15, fontWeight: "700", color: t.fg }}>重装 VPS</Text>
            </View>
            <Pressable onPress={onClose} hitSlop={10}><X size={18} color={t.muted} /></Pressable>
          </View>
          <ScrollView style={{ maxHeight: 460 }} contentContainerStyle={{ padding: 14, gap: 12 }}>
            <Text style={{ fontSize: 12, fontWeight: "600", color: t.fg }}>选择镜像</Text>
            {q.error ? (
              <Text style={{ fontSize: 12, color: t.danger }}>{q.error}</Text>
            ) : q.loading ? (
              <Text style={{ fontSize: 12, color: t.muted }}>镜像列表读取中…</Text>
            ) : (
              <View style={{ gap: 6 }}>
                {(q.data?.templates ?? []).map((tpl) => {
                  const on = String(picked?.id) === String(tpl.id);
                  return (
                    <Pressable key={String(tpl.id)} style={[styles.tpl, { borderColor: on ? t.fg : t.border }]} onPress={() => setPicked({ id: tpl.id, name: tpl.name })}>
                      <View style={{ flex: 1 }}>
                        <Text style={{ fontSize: 12.5, fontWeight: "600", color: t.fg }}>{tpl.name}</Text>
                        {tpl.distribution ? <Text style={{ fontSize: 10, color: t.muted, marginTop: 2 }}>{tpl.distribution}</Text> : null}
                      </View>
                      {on && <View style={{ width: 16, height: 16, borderRadius: 99, backgroundColor: t.fg, alignItems: "center", justifyContent: "center" }}><View style={{ width: 6, height: 6, borderRadius: 99, backgroundColor: t.surface }} /></View>}
                    </Pressable>
                  );
                })}
              </View>
            )}
            <TextInput style={[styles.input, inputStyle]} placeholder="SSH key 名称(可选,装后免密)" placeholderTextColor={t.faint} autoCapitalize="none" autoCorrect={false} value={sshKey} onChangeText={setSshKey} />
            <Pressable style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between" }} onPress={() => setNoMail(!noMail)}>
              <Text style={{ fontSize: 12.5, color: t.fg }}>不发送密码邮件(配 SSH key 用)</Text>
              <View style={{ width: 40, height: 23, borderRadius: 99, backgroundColor: noMail ? t.success : "#D8D8D8", justifyContent: "center", paddingHorizontal: 2 }}>
                <View style={{ width: 19, height: 19, borderRadius: 99, backgroundColor: "#FFF", alignSelf: noMail ? "flex-end" : "flex-start" }} />
              </View>
            </Pressable>
            <Text style={{ fontSize: 12, fontWeight: "600", color: t.fg }}>输入名称确认</Text>
            <TextInput style={[styles.input, inputStyle, { fontFamily: "Menlo" }]} placeholder={vpsName} placeholderTextColor={t.faint} autoCapitalize="none" autoCorrect={false} value={confirmName} onChangeText={setConfirmName} />
            <Pressable style={[styles.btn, { backgroundColor: t.danger }]} onPress={submit} disabled={act.pending}>
              <Text style={{ color: "#FFF", fontSize: 15, fontWeight: "600" }}>{act.pending ? "提交中…" : "面容确认并重装"}</Text>
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
  tpl: { flexDirection: "row", alignItems: "center", gap: 8, borderRadius: 12, borderWidth: 1, padding: 11 },
  input: { height: 44, borderRadius: 11, borderWidth: 1, paddingHorizontal: 13, fontSize: 13 },
  btn: { height: 47, borderRadius: 14, alignItems: "center", justifyContent: "center", marginTop: 4 },
});
