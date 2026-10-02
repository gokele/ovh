/**
 * 根组件:机器控制台是唯一主体 —— 打开就是机器列表,点机器进控制台。
 * 抢购/雷达/队列/我的收进右上角菜单(次要入口,点开跳转查看)。
 * 用户定位:这是「服务器控制的随身终端」,不是抢购工具。
 */
import { useCallback, useEffect, useState } from "react";
import { Modal, Pressable, StyleSheet, Text, View } from "react-native";
import { StatusBar } from "expo-status-bar";
import { Server, Radar, ClipboardList, User, Menu, X } from "lucide-react-native";

import { useTokens } from "./theme/tokens";
import { loadConnection, makeClient, setAccountId } from "./api/connection";
import PairingScreen from "./screens/PairingScreen";
import MachinesScreen from "./screens/MachinesScreen";
import RadarScreen from "./screens/RadarScreen";
import QueueScreen from "./screens/QueueScreen";
import ProfileScreen from "./screens/ProfileScreen";

/** 次要页面(从菜单跳进去,返回键/关闭回到机器列表) */
type OverlayPage = "radar" | "queue" | "profile" | null;

export default function App() {
  const t = useTokens();
  const [conn, setConn] = useState<{ serverUrl: string; token: string; accountId: string } | null>(null);
  const [booting, setBooting] = useState(true);
  const [menuOpen, setMenuOpen] = useState(false);
  const [overlay, setOverlay] = useState<OverlayPage>(null);

  useEffect(() => {
    loadConnection().then((c: { serverUrl: string; token: string; accountId: string }) => {
      if (c.serverUrl && c.token) setConn(c);
      setBooting(false);
    });
  }, []);

  const onPaired = useCallback(async () => {
    setConn(await loadConnection());
  }, []);

  if (booting) return <View style={{ flex: 1, backgroundColor: t.bg }} />;
  if (!conn) return <PairingScreen onPaired={onPaired} />;

  const client = makeClient(conn);

  return (
    <View style={{ flex: 1, backgroundColor: t.bg, paddingTop: 54 }}>
      <StatusBar style="auto" />

      {/* 顶栏:标题 + 右上角菜单按钮(唯一的导航入口) */}
      <View style={[styles.topbar, { borderBottomColor: t.border, borderBottomWidth: StyleSheet.hairlineWidth }]}>
        <Text style={{ fontSize: 16, fontWeight: "700", color: t.fg }}>服务器控制台</Text>
        <Pressable onPress={() => setMenuOpen(true)} hitSlop={12} accessibilityLabel="菜单">
          <Menu size={22} color={t.fg} strokeWidth={1.9} />
        </Pressable>
      </View>

      {/* 主体:机器列表(或机器详情,由 MachinesScreen 内部管理) */}
      {overlay === null && <MachinesScreen client={client} hideHeader />}
      {overlay === "radar" && <RadarScreen client={client} onClose={() => setOverlay(null)} />}
      {overlay === "queue" && <QueueScreen client={client} onClose={() => setOverlay(null)} />}
      {overlay === "profile" && (
        <ProfileScreen
          client={client}
          serverUrl={conn.serverUrl}
          onClose={() => setOverlay(null)}
          onAccountChange={async (id: string) => {
            await setAccountId(id);
            setConn({ ...conn, accountId: id });
          }}
          onDisconnected={() => {
            setOverlay(null);
            setConn(null);
          }}
        />
      )}

      {/* 菜单:次要入口全在这里,点一个关一个 */}
      <Modal visible={menuOpen} transparent animationType="fade" onRequestClose={() => setMenuOpen(false)}>
        <Pressable style={styles.menuBackdrop} onPress={() => setMenuOpen(false)}>
          <View style={[styles.menuPanel, { backgroundColor: t.surface, borderColor: t.border }]}>
            <View style={[styles.menuHeader, { borderBottomColor: t.border }]}>
              <Text style={{ fontSize: 14, fontWeight: "700", color: t.fg }}>更多</Text>
              <Pressable onPress={() => setMenuOpen(false)} hitSlop={10}>
                <X size={18} color={t.muted} />
              </Pressable>
            </View>
            {([
              ["radar", "补货雷达", "机型 × 机房可用性", Radar],
              ["queue", "抢购队列", "任务状态与耗时", ClipboardList],
              ["profile", "设置与账户", "配对 / 账户 / 外观", User],
            ] as const).map(([id, label, desc, Icon]) => (
              <Pressable
                key={id}
                style={({ pressed }) => [styles.menuItem, pressed && { opacity: 0.6 }]}
                onPress={() => {
                  setMenuOpen(false);
                  setOverlay(id);
                }}
              >
                <Icon size={18} color={t.fg} strokeWidth={1.8} />
                <View style={{ flex: 1 }}>
                  <Text style={{ fontSize: 13.5, fontWeight: "600", color: t.fg }}>{label}</Text>
                  <Text style={{ fontSize: 10.5, color: t.muted, marginTop: 1 }}>{desc}</Text>
                </View>
              </Pressable>
            ))}
          </View>
        </Pressable>
      </Modal>
    </View>
  );
}

const styles = StyleSheet.create({
  topbar: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", paddingHorizontal: 16, paddingVertical: 10 },
  menuBackdrop: { flex: 1, backgroundColor: "rgba(10,10,10,0.35)", alignItems: "flex-end", justifyContent: "flex-start", paddingTop: 60, paddingRight: 14 },
  menuPanel: { borderRadius: 16, borderWidth: 1, width: 250, paddingVertical: 6 },
  menuHeader: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", paddingHorizontal: 14, paddingVertical: 10, borderBottomWidth: StyleSheet.hairlineWidth },
  menuItem: { flexDirection: "row", alignItems: "center", gap: 11, paddingHorizontal: 14, paddingVertical: 12 },
});
