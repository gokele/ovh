/**
 * 根组件:配对闸 → 四 Tab(机器 / 雷达 / 队列 / 我的)。
 * 白色优先(useColorScheme 跟随系统);Tab 图标用 lucide-react-native(零 Emoji)。
 */
import { useCallback, useEffect, useState } from "react";
import { Text, View } from "react-native";
import { StatusBar } from "expo-status-bar";
import { Server, Radar, ClipboardList, User } from "lucide-react-native";
import * as SecureStore from "expo-secure-store";

import { useTokens } from "./theme/tokens";
import { loadConnection, makeClient } from "./api/connection";
import PairingScreen from "./screens/PairingScreen";
import TabBar from "./components/TabBar";
import MachinesScreen from "./screens/MachinesScreen";
import RadarScreen from "./screens/RadarScreen";
import QueueScreen from "./screens/QueueScreen";
import ProfileScreen from "./screens/ProfileScreen";

type TabId = "machines" | "radar" | "queue" | "profile";

export default function App() {
  const t = useTokens();
  const [conn, setConn] = useState<{ serverUrl: string; token: string; accountId: string } | null>(null);
  const [booting, setBooting] = useState(true);
  const [tab, setTab] = useState<TabId>("machines");

  // 启动:读 SecureStore 里的连接;没有就走配对
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
    <View style={{ flex: 1, backgroundColor: t.bg, paddingTop: 56 }}>
      <StatusBar style="auto" />
      {tab === "machines" && <MachinesScreen client={client} />}
      {tab === "radar" && <RadarScreen client={client} />}
      {tab === "queue" && <QueueScreen client={client} />}
      {tab === "profile" && (
        <ProfileScreen
          client={client}
          serverUrl={conn.serverUrl}
          onAccountChange={async (id) => {
            await SecureStore.setItemAsync("ovh_active_account", id);
            setConn({ ...conn, accountId: id });
          }}
          onDisconnected={() => setConn(null)}
        />
      )}
      <TabBar
        tabs={[
          { id: "machines", label: "机器", Icon: Server },
          { id: "radar", label: "雷达", Icon: Radar },
          { id: "queue", label: "队列", Icon: ClipboardList },
          { id: "profile", label: "我的", Icon: User },
        ]}
        active={tab}
        onChange={(id) => setTab(id as TabId)}
      />
    </View>
  );
}
