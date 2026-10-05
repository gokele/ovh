import { Moon, Sun, Monitor } from "lucide-react";
import { toast } from "sonner";
import { useTheme } from "@/hooks/use-theme";
import { type ThemeMode } from "@/lib/theme";
import { useTranslation } from "react-i18next";

/**
 * 顶栏主题快捷键:单击在 浅色 → 深色 → 跟随系统 之间循环。
 *
 * 放顶栏而不是埋进设置页 —— 夜里看库存时不想为切个主题翻三层面板。
 * 图标即当前模式(太阳/月亮/显示器),切换后弹一条 toast 说明现在是什么,
 * "跟随系统"对新手不是自解释的,循环到它时必须说清楚。
 */
const CYCLE: ThemeMode[] = ["light", "dark", "system"];

function Icon({ mode }: { mode: ThemeMode }) {
  const cls = "w-4 h-4";
  if (mode === "dark") return <Moon className={cls} />;
  if (mode === "light") return <Sun className={cls} />;
  return <Monitor className={cls} />;
}

export function ThemeToggle() {
  const { mode, setMode } = useTheme();
  const { t } = useTranslation();
  const next = CYCLE[(CYCLE.indexOf(mode) + 1) % CYCLE.length];
  const label = (m: ThemeMode) => t(`commons.theme.${m}`);

  return (
    <button
      type="button"
      onClick={() => {
        setMode(next);
        toast(t("commons.theme.toast", {
          name: label(next),
          hint: next === "system" ? t("commons.theme.systemHint") : "",
        }), { duration: 2200 });
      }}
      className="w-9 h-9 rounded-full flex items-center justify-center text-muted-foreground hover:text-accent-foreground hover:bg-accent transition-colors flex-shrink-0"
      title={t("commons.theme.title", { current: label(mode), next: label(next) })}
      aria-label={t("commons.theme.aria", { current: label(mode) })}
    >
      <Icon mode={mode} />
    </button>
  );
}
