import { Moon, Sun, Monitor } from "lucide-react";
import { toast } from "sonner";
import { useTheme } from "@/hooks/use-theme";
import { THEME_LABELS, type ThemeMode } from "@/lib/theme";

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
  const next = CYCLE[(CYCLE.indexOf(mode) + 1) % CYCLE.length];

  return (
    <button
      type="button"
      onClick={() => {
        setMode(next);
        toast(`外观:${THEME_LABELS[next]}${next === "system" ? "(跟随系统的浅色/深色设置)" : ""}`, {
          duration: 2200,
        });
      }}
      className="w-9 h-9 rounded-full flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-accent transition-colors flex-shrink-0"
      title={`当前:${THEME_LABELS[mode]}。点击切换到${THEME_LABELS[next]}`}
      aria-label={`切换外观,当前${THEME_LABELS[mode]}`}
    >
      <Icon mode={mode} />
    </button>
  );
}
