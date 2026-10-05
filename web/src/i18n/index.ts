import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import LanguageDetector from "i18next-browser-languagedetector";

import { zh } from "./locales/zh";
import { en } from "./locales/en";

/**
 * 多语言基建:
 * - 检测顺序:localStorage(ovh-lang) → 浏览器语言 → 默认中文
 * - 语言包按模块前缀组织(nav.* / servers.* / api.* …),全部打进主包;
 *   文案量增长到影响首屏时再拆 namespace 懒加载,key 结构不用动
 * - 后端错误消息走"错误码翻译层"(lib/api-error.ts):后端在 error/message 旁带
 *   稳定 code,前端按 api.<CODE> 查译文;没有 code 的透传原文,渐进迁移不破坏现状
 * - App 端(iOS)暂不接入:错误码方案让它以后接的时候不用改后端
 */
export const SUPPORTED_LANGUAGES = ["zh", "en"] as const;
export type AppLanguage = (typeof SUPPORTED_LANGUAGES)[number];

export const LANGUAGE_LABELS: Record<AppLanguage, string> = {
  zh: "中文",
  en: "English",
};

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      zh: { translation: zh },
      en: { translation: en },
    },
    supportedLngs: SUPPORTED_LANGUAGES,
    // 浏览器是 zh-TW/zh-HK 时落到 zh;en-US 落到 en
    nonExplicitSupportedLngs: true,
    fallbackLng: "zh",
    detection: {
      // 默认跟随浏览器语言(自动检测不落盘,浏览器改语言页面就跟着变);
      // 手动切换(顶栏/登录页按钮)才写 localStorage,之后以手动选择为准
      order: ["localStorage", "navigator"],
      lookupLocalStorage: "ovh-lang",
      caches: [],
    },
    interpolation: {
      // React 已经转义,不需要 i18next 再转义一次
      escapeValue: false,
    },
    returnEmptyString: false,
  });

export default i18n;
