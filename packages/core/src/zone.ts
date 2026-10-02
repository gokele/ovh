import { OVH_SUBSIDIARIES } from "./ovh-subsidiaries";
import { endpointRegion } from "./ovh-regions";

/**
 * 子公司 → 大区(EU / US / CA)与区域配色(语义 hex,不绑 Tailwind)。
 *
 * 网页端的 zone-color.ts 输出 Tailwind 类名,React Native 用不了 —— 这里输出
 * 语义 hex 对(浅色/深色各一套),网页后续接 core 时做一层类名映射即可。
 * 颜色编码的是「在哪套目录里」,不是账户身份;同区多账户同色,靠名字区分。
 */

export type ZoneRegion = "eu" | "us" | "ca";

/** 区域徽章的一套颜色(bg=底,fg=字),light/dark 分别校准(非反相) */
export interface ZonePalette {
  light: { bg: string; fg: string };
  dark: { bg: string; fg: string };
  label: string;
}

const REGION: Record<ZoneRegion, ZonePalette> = {
  eu: {
    light: { bg: "#E8EFFD", fg: "#1D4ED8" },
    dark: { bg: "#12203A", fg: "#7FB0F9" },
    label: "欧洲区",
  },
  us: {
    light: { bg: "#FBF0E2", fg: "#B45309" },
    dark: { bg: "#2A1F0F", fg: "#EFB876" },
    label: "美国区",
  },
  ca: {
    light: { bg: "#E8F4EC", fg: "#15803D" },
    dark: { bg: "#10251A", fg: "#6FD59A" },
    label: "加拿大区",
  },
};

/** 未知子公司兜底:中性灰。不猜成欧区 —— 猜错的代价是下单打在错误站点上 */
const UNKNOWN: ZonePalette = {
  light: { bg: "#F5F5F5", fg: "#666666" },
  dark: { bg: "#292929", fg: "#9A9A9A" },
  label: "未知区",
};

/** 子公司代码 → 所属大区;认不出返回 null(不猜) */
export function regionOf(zone: string): ZoneRegion | null {
  const sub = OVH_SUBSIDIARIES.find((s) => s.code === zone);
  if (!sub) return null;
  const r = endpointRegion(sub.endpoint);
  return r === "US" ? "us" : r === "CA" ? "ca" : "eu";
}

/** 子公司代码 → 徽章配色(传 colorScheme 取对应那套) */
export function zonePalette(zone: string, scheme: "light" | "dark" = "light"): { bg: string; fg: string; label: string } {
  const r = regionOf(zone);
  const p = r ? REGION[r] : UNKNOWN;
  return { bg: p[scheme].bg, fg: p[scheme].fg, label: p.label };
}

/** 子公司代码 → 中文地区名("IE" → "爱尔兰"),认不出原样返回代码 */
export function zoneName(zone: string): string {
  return OVH_SUBSIDIARIES.find((s) => s.code === zone)?.label?.split(" · ")[0] || zone;
}
