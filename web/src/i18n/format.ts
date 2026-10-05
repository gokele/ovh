import { dateFnsLocale } from "./date-locale";

/**
 * 日期/时间格式化的本地化出口。
 *
 * 项目里历史遗留三种写法:new Date(x).toLocaleDateString("zh-CN") /
 * date-fns format(x, "MMM d") / 手拼。多语言之后统一从这里走:
 * 语言切换时这些函数全部跟随(react-i18next 的语言变更会触发使用方重渲染,
 * 因为这里读了 i18n.language —— 用 useDateFormat() 包装保证组件内响应式)。
 */
import { format, formatDistanceToNow, parseISO } from "date-fns";
import i18n from "@/i18n";

function loc() {
  return dateFnsLocale(i18n.language);
}

/** 2024/5/1 式短日期(跟随语言) */
export function fmtDate(input: string | number | Date): string {
  const d = typeof input === "string" ? parseISO(input) : new Date(input);
  if (Number.isNaN(d.getTime())) return "—";
  return new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium" }).format(d);
}

/** 含时间的完整展示(跟随语言) */
export function fmtDateTime(input: string | number | Date): string {
  const d = typeof input === "string" ? parseISO(input) : new Date(input);
  if (Number.isNaN(d.getTime())) return "—";
  return new Intl.DateTimeFormat(i18n.language, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(d);
}

/** "3 minutes ago" / "3 分钟前"(跟随语言) */
export function fmtAgo(input: string | number | Date): string {
  const d = typeof input === "string" ? parseISO(input) : new Date(input);
  if (Number.isNaN(d.getTime())) return "—";
  return formatDistanceToNow(d, { addSuffix: true, locale: loc() });
}

/** date-fns 自定义 format 的本地化包装(MMM 月份名跟随语言) */
export function fmtPattern(input: string | number | Date, pattern: string): string {
  const d = typeof input === "string" ? parseISO(input) : new Date(input);
  if (Number.isNaN(d.getTime())) return "—";
  return format(d, pattern, { locale: loc() });
}

/** 金额:跟随语言决定小数点/千分位写法(€4.50 vs 4,50 €) */
export function fmtMoney(value: number, currency: string): string {
  if (!Number.isFinite(value)) return "—";
  try {
    return new Intl.NumberFormat(i18n.language, {
      style: currency ? "currency" : "decimal",
      currency: currency || undefined,
      maximumFractionDigits: 2,
    }).format(value);
  } catch {
    return currency ? `${value.toFixed(2)} ${currency}` : value.toFixed(2);
  }
}
