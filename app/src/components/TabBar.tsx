/**
 * 底部 Tab 栏:四枚 lucide 图标 + 文字,激活态用前景色。
 * 触达高度 58pt(≥44pt 标准),顶部分隔线。
 */
import { Pressable, StyleSheet, Text, View } from "react-native";
import type { LucideIcon } from "lucide-react-native";

import { useTokens } from "../theme/tokens";

export interface TabDef {
  id: string;
  label: string;
  Icon: LucideIcon;
}

export default function TabBar({ tabs, active, onChange }: { tabs: TabDef[]; active: string; onChange: (id: string) => void }) {
  const t = useTokens();
  return (
    <View style={[styles.bar, { backgroundColor: t.surface, borderColor: t.border }]}>
      {tabs.map(({ id, label, Icon }) => {
        const on = id === active;
        return (
          <Pressable key={id} style={styles.tab} onPress={() => onChange(id)} accessibilityRole="tab" accessibilityState={{ selected: on }}>
            <Icon size={22} color={on ? t.fg : t.faint} strokeWidth={on ? 2.1 : 1.8} />
            <Text style={{ fontSize: 10, color: on ? t.fg : t.faint, fontWeight: on ? "600" : "400" }}>{label}</Text>
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  bar: { flexDirection: "row", height: 58, borderTopWidth: StyleSheet.hairlineWidth },
  tab: { flex: 1, alignItems: "center", justifyContent: "center", gap: 3 },
});
