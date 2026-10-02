/**
 * 维护 Tab(真数据):硬件规格 / 撤单资格 / 变更联系人状态 / 网络规格。
 * 全只读 —— 维护动作(硬件更换/撤单提交/联系人变更)在网页端做,这里给"知道现状"。
 */
import { StyleSheet, Text, View } from "react-native";
import { Cpu, ShieldX, Contact, Wrench } from "lucide-react-native";

import type { ApiClient } from "@core/api-client";
import { useTokens } from "../theme/tokens";
import { usePoll } from "../api/hooks";

interface Props { client: ApiClient; serviceName: string }

export default function ServerDetailMaintenance({ client, serviceName }: Props) {
  const t = useTokens();
  const hw = usePoll<Record<string, unknown>>(client, `/server-control/${serviceName}/hardware`, 0);
  const retr = usePoll<Record<string, unknown>>(client, `/server-control/${serviceName}/retraction`, 60_000);
  const contact = usePoll<Record<string, unknown>>(client, `/ovh/contact-change-requests`, 60_000);

  const h = hw.data as { processorName?: string; coresPerProcessor?: number; threadsPerProcessor?: number; memorySize?: { value: number; unit: string }; diskGroups?: Array<{ diskGroupId?: number; disks?: Array<{ size?: number; unit?: string; type?: string }> }> } | null;
  const r = retr.data as { eligible?: boolean; reason?: string; message?: string; hoursLeft?: number; retractionDate?: string } | null;
  const c = contact.data as { requests?: Array<Record<string, unknown>> } | null;

  const memText = h?.memorySize ? `${h.memorySize.value} ${h.memorySize.unit || "GB"}` : "—";
  const diskText = (h?.diskGroups ?? [])
    .map((g) => `${(g.disks ?? []).length}×${(g.disks ?? [])[0]?.size ?? "?"}${(g.disks ?? [])[0]?.unit || "GB"} ${(g.disks ?? [])[0]?.type || ""}`.trim())
    .join(" + ");

  return (
    <View style={{ gap: 10 }}>
      <Group t={t} Icon={Cpu} title="硬件规格">
        {hw.error ? <Err t={t} text={hw.error} /> : (
          <>
            <Kv t={t} k="处理器" v={h?.processorName || "—"} />
            <Kv t={t} k="核心/线程" v={h ? `${h.coresPerProcessor ?? "?"}C / ${h.threadsPerProcessor ?? "?"}T` : "—"} />
            <Kv t={t} k="内存" v={memText} />
            <Kv t={t} k="磁盘组" v={diskText || "—"} />
          </>
        )}
      </Group>

      <Group t={t} Icon={ShieldX} title="撤回订单(14 天内)">
        {retr.error ? <Err t={t} text={retr.error} /> : r?.eligible ? (
          <>
            <Text style={{ fontSize: 11.5, color: t.warning, lineHeight: 17, marginBottom: 4 }}>
              可撤单 · 剩 {r.hoursLeft != null ? Math.ceil(r.hoursLeft / 24) : "?"} 天(从下单日起算)
            </Text>
            <Kv t={t} k="截止" v={r.retractionDate ? r.retractionDate.slice(0, 16).replace("T", " ") : "—"} />
            <Text style={{ fontSize: 10.5, color: t.faint, marginTop: 4 }}>撤单 = 退款 + 服务器注销,不可逆;提交在网页端做</Text>
          </>
        ) : (
          <Text style={{ fontSize: 11.5, color: t.muted, lineHeight: 17 }}>{r?.message || "不在撤回期内"}</Text>
        )}
      </Group>

      <Group t={t} Icon={Contact} title="变更联系人">
        <Text style={{ fontSize: 11.5, color: t.muted, lineHeight: 17 }}>
          {(c?.requests ?? []).length > 0 ? `${(c?.requests ?? []).length} 个待确认(网页端处理)` : "无待确认请求"}
        </Text>
      </Group>

      <Group t={t} Icon={Wrench} title="硬件更换 / 任务改期 / 合同期">
        <Text style={{ fontSize: 11.5, color: t.muted, lineHeight: 17 }}>
          工单制操作,请到网页端控制台完成(数据与确认流程更完整)
        </Text>
      </Group>
    </View>
  );
}

function Group({ t, Icon, title, children }: { t: ReturnType<typeof useTokens>; Icon: typeof Cpu; title: string; children: React.ReactNode }) {
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
  kv: { flexDirection: "row", justifyContent: "space-between", alignItems: "baseline", paddingVertical: 4, gap: 12 },
});
