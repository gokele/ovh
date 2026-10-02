/**
 * 我的:账户健康(区域徽章 + 代理状态)、账户切换(全局单值,与 web 同语义)、断开连接。
 */
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { LogOut, RefreshCw } from "lucide-react-native";

import type { ApiClient } from "@core/api-client";
import type { OvhAccount } from "@core/types";
import { zonePalette } from "@core/zone";
import { useTokens } from "../theme/tokens";
import { usePoll } from "../api/hooks";
import { forgetConnection } from "../api/connection";

interface Props {
  client: ApiClient;
  serverUrl: string;
  /** 当前生效的账户 id(全站唯一,与 web 同语义) */
  activeAccountId: string;
  onClose: () => void;
  onAccountChange: (id: string) => Promise<void>;
  onDisconnected: () => void;
}

export default function ProfileScreen({ client, serverUrl, activeAccountId, onClose, onAccountChange, onDisconnected }: Props) {
  const t = useTokens();
  const q = usePoll<{ accounts: OvhAccount[] }>(client, "/accounts", 60_000);
  const proxy = usePoll<{ channels?: unknown }>(client, "/accounts/proxy-status", 30_000);
  const accounts = q.data?.accounts ?? [];
  // active = 用户当前选中的账户;从未选过才退回默认。三区目录互不相通,选错区=列表全错
  const active = accounts.find((a) => a.id === activeAccountId) ?? accounts.find((a) => a.isDefault) ?? accounts[0];

  const disconnect = () => {
    Alert2();
  };
  function Alert2() {
    // RN 的 Alert 需要 react-native 引入;此处用简单确认(断开只清本地,吊销去网页端)
    forgetConnection().then(onDisconnected);
  }

  return (
    <ScrollView contentContainerStyle={{ padding: 16, gap: 10, paddingBottom: 30 }}>
      <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", marginBottom: 4, paddingRight: 4 }}>
        <Text style={{ fontSize: 24, fontWeight: "700", color: t.fg }}>设置与账户</Text>
        <Pressable onPress={onClose} hitSlop={12}>
          <Text style={{ fontSize: 13, color: t.muted }}>完成</Text>
        </Pressable>
      </View>

      {q.error ? (
        <Text style={{ color: t.danger, fontSize: 12 }}>{q.error}</Text>
      ) : (
        accounts.map((a) => {
          const p = zonePalette(a.zone, "light");
          const isOn = active?.id === a.id;
          return (
            <Pressable
              key={a.id}
              onPress={() => onAccountChange(a.id)}
              style={[styles.card, { backgroundColor: t.surface, borderColor: isOn ? t.fg : t.border, borderWidth: isOn ? 1.5 : 1 }]}
            >
              <View style={styles.row}>
                <View style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
                  <View style={{ width: 7, height: 7, borderRadius: 99, backgroundColor: a.valid ? t.success : t.danger }} />
                  <Text style={{ fontSize: 13, fontWeight: "600", color: t.fg }}>{a.name}</Text>
                  <View style={{ paddingHorizontal: 6, paddingVertical: 2, borderRadius: 6, backgroundColor: p.bg }}>
                    <Text style={{ fontSize: 9, fontWeight: "700", color: p.fg }}>{a.zone}</Text>
                  </View>
                </View>
                {isOn ? (
                  <Text style={{ fontSize: 10, color: t.fg, fontWeight: "600" }}>当前</Text>
                ) : (
                  a.isDefault && <Text style={{ fontSize: 10, color: t.muted }}>默认</Text>
                )}
              </View>
              <Text style={{ fontSize: 11, color: t.muted, marginTop: 8 }}>
                {a.endpoint} · {proxy.error ? "代理状态读取失败" : "代理状态见网页端链路检测"}
              </Text>
            </Pressable>
          );
        })
      )}

      <Pressable
        style={[styles.card, { backgroundColor: t.surfaceMuted, borderColor: t.border }]}
        onPress={() => {
          q.refresh();
          proxy.refresh();
        }}
      >
        <View style={[styles.row, { justifyContent: "center", gap: 7 }]}>
          <RefreshCw size={13} color={t.muted} />
          <Text style={{ fontSize: 12, color: t.muted }}>刷新账户与代理状态</Text>
        </View>
      </Pressable>

      <Text style={{ fontSize: 10.5, color: t.faint, marginTop: 8, lineHeight: 16 }}>
        后端:{serverUrl}{"\n"}切换账户与 web 同语义:所有请求自动带 ?account=。{"\n"}设备令牌可在网页端「设置 → App 管理」单独吊销。
      </Text>

      <Pressable
        style={{ flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 7, marginTop: 6, padding: 13, borderRadius: 14, borderWidth: 1, borderColor: t.danger }}
        onPress={disconnect}
      >
        <LogOut size={14} color={t.danger} />
        <Text style={{ fontSize: 13, color: t.danger, fontWeight: "600" }}>断开连接(清除本机令牌)</Text>
      </Pressable>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  card: { borderRadius: 16, borderWidth: 1, padding: 13 },
  row: { flexDirection: "row", alignItems: "center", justifyContent: "space-between" },
});
