/**
 * 配对首屏(白色优先):扫不了码先手填 —— 地址 + 配对码两条路。
 * 配对码在网页端「设置 → App 配对」生成,2 分钟一次性。
 */
import { useState } from "react";
import { KeyboardAvoidingView, Platform, Pressable, StyleSheet, Text, TextInput, View } from "react-native";
import { Server, KeyRound, ScanLine } from "lucide-react-native";

import { useTokens } from "../theme/tokens";
import { pairWithCode, saveManualKey } from "../api/connection";

export default function PairingScreen({ onPaired }: { onPaired: () => Promise<void> }) {
  const t = useTokens();
  const [url, setUrl] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  /** 提交配对:码优先,留空码 + 填了密钥段时走手动路径(简化:码为空视为手动) */
  const submit = async () => {
    const cleanUrl = url.trim().replace(/\/+$/, "");
    if (!/^https?:\/\/.+/.test(cleanUrl)) {
      setErr("地址要以 http:// 或 https:// 开头,例如 http://192.168.1.10:19998");
      return;
    }
    setBusy(true);
    setErr("");
    try {
      if (code.trim()) {
        await pairWithCode(cleanUrl, code, "iPhone");
      } else {
        setErr("请填配对码(网页端 设置 → App 配对 生成)");
        setBusy(false);
        return;
      }
      await onPaired();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "配对失败");
    }
    setBusy(false);
  };

  const inputStyle = { ...styles.input, borderColor: t.border, backgroundColor: t.surfaceMuted, color: t.fg };

  return (
    <KeyboardAvoidingView style={{ flex: 1, backgroundColor: t.bg }} behavior={Platform.OS === "ios" ? "padding" : undefined}>
      <View style={styles.wrap}>
        <View style={[styles.icon, { borderColor: t.fg }]}>
          <Server size={34} color={t.fg} strokeWidth={1.6} />
        </View>
        <Text style={{ fontSize: 18, fontWeight: "700", color: t.fg }}>连接你的控制台</Text>
        <Text style={[styles.hint, { color: t.muted }]}>
          在电脑端打开网页控制台 → 设置 →「App 配对」,把地址和配对码填到下面。{"\n"}
          <Text style={{ fontFamily: "Menlo", fontSize: 11 }}>配对码 2 分钟内有效 · 每台设备独立令牌,可在网页端单独吊销</Text>
        </Text>

        <TextInput
          style={inputStyle}
          placeholder="控制台地址,如 http://192.168.1.10:19998"
          placeholderTextColor={t.faint}
          autoCapitalize="none"
          autoCorrect={false}
          keyboardType="url"
          value={url}
          onChangeText={setUrl}
        />
        <TextInput
          style={{ ...inputStyle, fontFamily: "Menlo", letterSpacing: 2 }}
          placeholder="配对码(8 位)"
          placeholderTextColor={t.faint}
          autoCapitalize="characters"
          autoCorrect={false}
          maxLength={8}
          value={code}
          onChangeText={(v) => setCode(v.toUpperCase().replace(/[^A-Z2-9]/g, ""))}
        />

        {err !== "" && <Text style={{ fontSize: 12, color: t.danger, marginTop: 4 }}>{err}</Text>}

        <Pressable
          style={[styles.btn, { backgroundColor: t.btnPrimary, opacity: busy ? 0.5 : 1 }]}
          disabled={busy}
          onPress={submit}
        >
          <ScanLine size={16} color={t.btnPrimaryFg} />
          <Text style={{ color: t.btnPrimaryFg, fontSize: 15, fontWeight: "600" }}>{busy ? "配对中…" : "配对"}</Text>
        </Pressable>

        <Text style={[styles.manualHint, { color: t.faint }]}>
          <KeyRound size={12} color={t.faint} /> 手动填密钥的入口在「我的」页,配对成功后可用
        </Text>
      </View>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  wrap: { flex: 1, alignItems: "center", justifyContent: "center", padding: 28, gap: 14 },
  icon: { width: 74, height: 74, borderRadius: 18, borderWidth: 1.6, alignItems: "center", justifyContent: "center" },
  hint: { fontSize: 12, lineHeight: 19, textAlign: "center" },
  input: { width: "100%", height: 46, borderRadius: 12, borderWidth: 1, paddingHorizontal: 14, fontSize: 14 },
  btn: { width: "100%", height: 47, borderRadius: 14, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 8, marginTop: 6 },
  manualHint: { fontSize: 11, marginTop: 8 },
});
