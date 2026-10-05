import { Languages } from "lucide-react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import i18n, { LANGUAGE_LABELS, SUPPORTED_LANGUAGES, type AppLanguage } from "@/i18n";

/**
 * 顶栏语言切换:单击在支持的语言之间循环(与 ThemeToggle 同一交互模式 ——
 * 放顶栏,不为切个语言翻三层面板)。图标是通用"地球/语言"符号而非国旗:
 * 语言不等于国家。
 */
export function LanguageToggle() {
  const { t } = useTranslation();
  const cur = (i18n.language as AppLanguage) || "zh";
  const idx = Math.max(0, SUPPORTED_LANGUAGES.indexOf(cur));
  const next = SUPPORTED_LANGUAGES[(idx + 1) % SUPPORTED_LANGUAGES.length];

  return (
    <button
      type="button"
      onClick={async () => {
        // 先切完语言再弹 toast,不然提示文本用的是切换前的语言(会出"Language: 中文"混排)
        await i18n.changeLanguage(next);
        // localStorage 持久化由 i18next detector 的 caches 负责
        toast(t("lang.switched", { name: LANGUAGE_LABELS[next] }), { duration: 2200 });
      }}
      className="w-9 h-9 rounded-full flex items-center justify-center text-muted-foreground hover:text-accent-foreground hover:bg-accent transition-colors flex-shrink-0"
      title={t("lang.current", { name: LANGUAGE_LABELS[cur], next: LANGUAGE_LABELS[next] })}
      aria-label={t("lang.toggle", { name: LANGUAGE_LABELS[cur] })}
    >
      <Languages className="w-4 h-4" />
    </button>
  );
}
