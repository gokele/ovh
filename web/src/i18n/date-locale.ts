import { zhCN, enUS } from "date-fns/locale";

/** 语言 → date-fns locale(zh-* 都落 zhCN,en-* 都落 enUS) */
export function dateFnsLocale(lang: string) {
  return lang?.startsWith("zh") ? zhCN : enUS;
}
