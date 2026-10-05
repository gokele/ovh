import { OVH_SUBSIDIARIES } from "@/lib/ovh-subsidiaries";
import i18n from "@/i18n";

/**
 * 按 OVH 的三个 API endpoint 给账户配色。
 *
 * 为什么按 endpoint 而不是按国家码：IE / FR / DE / PL 这十几个子公司共用
 * 同一个 ovh-eu 目录，彼此的 planCode 通用；而 ovh-us / ovh-ca 是**另外两套
 * 目录**，同一台机器在欧区叫 24sk602、在美区叫 24sk602-v1-us，拿错了下单必失败。
 *
 * 所以这个颜色编码的是"你现在在哪套目录里"—— 这是切账户时唯一真正要紧的事，
 * 值得用一个常驻的色点表达，而不是让用户去认国家码。
 */
export type ZoneRegion = "eu" | "us" | "ca";

interface RegionStyle {
  /**
   * 子公司代码徽章的配色（底色 + 文字，亮/暗两套）。
   *
   * 注意这个颜色编码的是**区域**，不是账户身份 —— 同一个区里的多个账户
   * 颜色完全一样。区分账户靠的是名字，所以名字在任何布局里都必须完整可读，
   * 不能为了给徽章腾地方而被截断。
   */
  badge: string;
  /** 区域名(跟随语言)，用于下拉里的分组标题 */
  label: string;
}

const REGION_BADGE: Record<ZoneRegion, string> = {
  eu: "bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300",
  us: "bg-amber-50 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
  ca: "bg-emerald-50 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300",
};

/** 子公司代码 → 所属 API endpoint 区域。认不出来返回 null（不猜） */
export function regionOf(zone: string): ZoneRegion | null {
  const sub = OVH_SUBSIDIARIES.find((s) => s.code === zone);
  if (!sub) return null;
  if (sub.endpoint === "ovh-eu") return "eu";
  if (sub.endpoint === "ovh-us") return "us";
  if (sub.endpoint === "ovh-ca") return "ca";
  return null;
}

/** 未知子公司的兜底：中性灰。不要猜成欧区 —— 猜错的代价是下单打在错误的站点上 */
const UNKNOWN_BADGE = "bg-secondary text-muted-foreground";

/** 子公司代码 → 配色与区域名(label 每次调用时现取,跟随语言切换) */
export function zoneStyle(zone: string): RegionStyle {
  const r = regionOf(zone);
  if (!r) {
    return { badge: UNKNOWN_BADGE, label: i18n.t("commons.zoneRegion.unknown") };
  }
  const label = i18n.t(`commons.zoneRegion.${r}`) as string;
  return { badge: REGION_BADGE[r], label };
}

/** 子公司代码 → 地区名（"IE" → "爱尔兰"，跟随语言），认不出就原样返回代码 */
export function zoneName(zone: string): string {
  if (!OVH_SUBSIDIARIES.some((s) => s.code === zone)) return zone;
  return i18n.t(`commons.country.${zone.toLowerCase()}`, { defaultValue: zone }) as string;
}
