/**
 * 高级 Tab(真数据):Backup FTP / DDoS 永久缓解 / 虚拟 MAC / vRack。
 * 全部只读展示 + 缓解开关(带确认);管理深水区回网页端。
 */
import { Alert, Pressable, StyleSheet, Text, View } from "react-native";
import { Box, Shield, Cable, Network } from "lucide-react-native";

import type { ApiClient } from "@core/api-client";
import { useTokens } from "../theme/tokens";
import { useAction, usePoll } from "../api/hooks";

interface Props { client: ApiClient; serviceName: string }

export default function ServerDetailAdvanced({ client, serviceName }: Props) {
  const t = useTokens();
  const ftp = usePoll<Record<string, unknown>>(client, `/server-control/${serviceName}/backup-ftp`, 60_000);
  const mit = usePoll<{ ips?: Array<{ ipBlock: string; mitigations: Array<{ ipOnMitigation: string; state: string; permanent: boolean }> }> }>(client, `/server-control/${serviceName}/mitigation`, 30_000);
  const vmac = usePoll<{ virtualMacs?: Array<Record<string, unknown>> }>(client, `/server-control/${serviceName}/virtual-mac`, 60_000);
  const vrack = usePoll<{ vracks?: Array<Record<string, unknown>> }>(client, `/server-control/${serviceName}/vrack`, 60_000);
  const act = useAction(client);

  const ftpData = ftp.data as { success?: boolean; error?: string; quota?: { value: number; unit: string }; usage?: { value: number; unit: string }; ftpUrl?: string } | null;
  const ftpBlocked = ftpData?.success === false;

  // 后端契约:POST = 启用、DELETE = 关闭,两者都必须带 ?block=<该 IP 所属 ipBlock>
  const toggleMitigation = (ip: string, block: string, permanent: boolean) => {
    Alert.alert(
      permanent ? "关闭永久缓解?" : "开启永久缓解?",
      permanent ? "关闭后该 IP 不再常驻 DDoS 缓解(OVH 自动缓解仍在)。" : "开启后该 IP 常驻 DDoS 缓解,攻击流量在 OVH 边缘清洗。",
      [
        { text: "取消", style: "cancel" },
        { text: "确认", style: "destructive", onPress: async () => {
          const base = `/server-control/${serviceName}/mitigation/${encodeURIComponent(ip)}`;
          const q = `?block=${encodeURIComponent(block)}`;
          const r = permanent ? await act.del(base + q) : await act.run(base + q);
          if (!r.ok) Alert.alert("失败", r.message);
          mit.refresh();
        } },
      ],
    );
  };

  return (
    <View style={{ gap: 10 }}>
      {/* Backup FTP */}
      <Group t={t} Icon={Box} title="Backup FTP">
        {ftp.error ? <Err t={t} text={ftp.error} /> : ftpBlocked ? (
          <Text style={[styles.v, { color: t.muted }]}>{ftpData?.error || "此区域不可用"}</Text>
        ) : ftpData?.quota ? (
          <>
            <Kv t={t} k="配额" v={`${ftpData.quota.value} ${ftpData.quota.unit || ""}`} />
            <Kv t={t} k="已用" v={ftpData.usage ? `${ftpData.usage.value} ${ftpData.usage.unit || ""}` : "—"} />
            {ftpData.ftpUrl ? <Kv t={t} k="地址" v={String(ftpData.ftpUrl).replace(/^ftp:\/\//, "")} /> : null}
          </>
        ) : (
          <Text style={[styles.v, { color: t.muted }]}>{ftp.loading ? "读取中…" : "未激活(网页端可激活)"}</Text>
        )}
      </Group>

      {/* DDoS 永久缓解 */}
      <Group t={t} Icon={Shield} title="DDoS 永久缓解">
        {mit.error ? <Err t={t} text={mit.error} /> : (mit.data?.ips ?? []).length === 0 ? (
          <Text style={[styles.v, { color: t.muted }]}>无 IP 信息</Text>
        ) : (
          (mit.data?.ips ?? []).flatMap((b) =>
            b.mitigations.map((m) => (
              <Pressable key={m.ipOnMitigation} style={styles.kv} onPress={() => toggleMitigation(m.ipOnMitigation, b.ipBlock, m.permanent)}>
                <Text style={{ fontFamily: "Menlo", fontSize: 11.5, color: t.fg, flexShrink: 1 }}>{m.ipOnMitigation}</Text>
                <Text style={{ fontSize: 11.5, color: m.permanent ? t.success : t.faint, fontWeight: "600" }}>
                  {m.permanent ? "开 · 点关" : "关 · 点开"}
                </Text>
              </Pressable>
            )),
          )
        )}
      </Group>

      {/* 虚拟 MAC */}
      <Group t={t} Icon={Cable} title="虚拟 MAC">
        <Text style={[styles.v, { color: t.muted }]}>
          {(vmac.data?.virtualMacs ?? []).length} 个(管理在网页端)
        </Text>
      </Group>

      {/* vRack */}
      <Group t={t} Icon={Network} title="vRack 私有网络">
        <Text style={[styles.v, { color: t.muted }]}>
          {(vrack.data?.vracks ?? []).length > 0 ? `${(vrack.data?.vracks ?? []).length} 个成员` : "未加入"}
        </Text>
      </Group>
    </View>
  );
}

function Group({ t, Icon, title, children }: { t: ReturnType<typeof useTokens>; Icon: typeof Box; title: string; children: React.ReactNode }) {
  return (
    <View style={[styles.group, { backgroundColor: t.surface, borderColor: t.border }]}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: 8, marginBottom: 6 }}>
        <Icon size={15} color={t.muted} strokeWidth={1.8} />
        <Text style={{ fontSize: 12.5, fontWeight: "600", color: t.fg }}>{title}</Text>
      </View>
      {children}
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
function Err({ t, text }: { t: ReturnType<typeof useTokens>; text: string }) {
  return <Text style={{ fontSize: 11.5, color: t.danger }}>{text}</Text>;
}

const styles = StyleSheet.create({
  group: { borderRadius: 16, borderWidth: 1, padding: 13 },
  kv: { flexDirection: "row", justifyContent: "space-between", alignItems: "center", paddingVertical: 4, gap: 12 },
  v: { fontSize: 11.5, lineHeight: 17 },
});
