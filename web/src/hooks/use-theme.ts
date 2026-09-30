import { useCallback, useEffect, useState } from "react";
import {
  getStoredTheme,
  resolveTheme,
  setTheme,
  subscribeTheme,
  type ResolvedTheme,
  type ThemeMode,
} from "@/lib/theme";

/**
 * 主题状态钩子。mode 是用户选择(含 system),resolved 是实际生效值 ——
 * 图表这类需要 JS 侧取色的地方用 resolved,UI 切换器展示/修改 mode。
 */
export function useTheme(): {
  mode: ThemeMode;
  resolved: ResolvedTheme;
  setMode: (m: ThemeMode) => void;
} {
  const [mode, setModeState] = useState<ThemeMode>(() => getStoredTheme());

  useEffect(() => {
    const cb = () => setModeState(getStoredTheme());
    // 系统偏好变化时 subscribeTheme 不一定触发(只有 system 模式会),
    // 但 resolved 是渲染期算的,mode state 不变也会重算 —— 这里再订阅一次
    // matchMedia 事件,保证 system 模式下系统切换的当帧就重渲染。
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    mq.addEventListener("change", cb);
    const unsub = subscribeTheme(cb);
    return () => {
      mq.removeEventListener("change", cb);
      unsub();
    };
  }, []);

  const setMode = useCallback((m: ThemeMode) => {
    setTheme(m);
    setModeState(m);
  }, []);

  return { mode, resolved: resolveTheme(mode), setMode };
}
