/**
 * 三态主题:浅色 / 深色 / 跟随系统。
 *
 * 存储键 "ovh-theme",取值 light | dark | system(默认 system)。
 * 实际生效靠 <html> 上的 dark class —— tailwind.config 里 darkMode: ["class"],
 * globals.css 的 .dark 块定义了整套翻转后的 token。
 *
 * 防闪烁的另一半在 index.html:那里有一段内联脚本在首帧之前就把 class 挂好,
 * 两个文件里的解析规则必须保持一致 —— 改这里时同步改那里。
 */

export type ThemeMode = "light" | "dark" | "system";
export type ResolvedTheme = "light" | "dark";

export const STORAGE_KEY = "ovh-theme";
export const THEME_MODES: ThemeMode[] = ["system", "light", "dark"];

export const THEME_LABELS: Record<ThemeMode, string> = {
  system: "跟随系统",
  light: "浅色",
  dark: "深色",
};

export function isThemeMode(v: unknown): v is ThemeMode {
  return v === "light" || v === "dark" || v === "system";
}

export function getStoredTheme(): ThemeMode {
  if (typeof localStorage === "undefined") return "system";
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return isThemeMode(v) ? v : "system";
  } catch {
    return "system";
  }
}

function systemPrefersDark(): boolean {
  return (
    typeof window !== "undefined" &&
    window.matchMedia("(prefers-color-scheme: dark)").matches
  );
}

export function resolveTheme(mode: ThemeMode): ResolvedTheme {
  if (mode === "system") return systemPrefersDark() ? "dark" : "light";
  return mode;
}

/** 把解析结果挂到 <html>:dark class + color-scheme + 移动端浏览器 chrome 的 theme-color */
function applyResolved(resolved: ResolvedTheme) {
  const root = document.documentElement;
  root.classList.toggle("dark", resolved === "dark");
  // theme-color 跟背景同值,手机浏览器地址栏区域才不会在深色下泛白。
  // 与 index.html 内联脚本里的两个 hex 保持同步(那里无法读 CSS 变量)
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute("content", resolved === "dark" ? "#0f0f0f" : "#ffffff");
}

export function applyTheme(mode: ThemeMode) {
  applyResolved(resolveTheme(mode));
}

export function setTheme(mode: ThemeMode) {
  try {
    localStorage.setItem(STORAGE_KEY, mode);
  } catch {
    // localStorage 被禁用(隐私模式等)时静默降级 —— 主题仍然当场生效,只是不跨会话记忆
  }
  applyTheme(mode);
  listeners.forEach((cb) => cb());
}

/* ---- 订阅:React 侧靠它重渲染 ---- */

const listeners = new Set<() => void>();

export function subscribeTheme(cb: () => void): () => void {
  listeners.add(cb);
  return () => listeners.delete(cb);
}

/**
 * system 模式下监听系统切换。非 system 模式时监听器留着也无妨 ——
 * applyResolved 只会被 resolveTheme 的结果驱动,mode 不是 system 时
 * 系统偏好变化不产生任何效果。
 */
if (typeof window !== "undefined") {
  window
    .matchMedia("(prefers-color-scheme: dark)")
    .addEventListener("change", () => {
      if (getStoredTheme() === "system") {
        applyTheme("system");
        listeners.forEach((cb) => cb());
      }
    });
}
