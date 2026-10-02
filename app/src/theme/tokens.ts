/**
 * 语义 token:浅色为默认(白色优先),深色为可选主题 —— 与 web 端两套 token 一一对应,
 * 均已按 WCAG AA 校准(web/src/styles/globals.css)。App 侧落在 RN 的 StyleSheet 里。
 */
import { useColorScheme } from "react-native";

export interface Tokens {
  bg: string;
  surface: string;
  surfaceMuted: string;
  border: string;
  fg: string;
  muted: string;
  faint: string;
  success: string;
  warning: string;
  danger: string;
  info: string;
  /** 主操作:黑白反转胶囊(浅色 = 黑底白字,深色 = 白底黑字) */
  btnPrimary: string;
  btnPrimaryFg: string;
}

const light: Tokens = {
  bg: "#FFFFFF",
  surface: "#FFFFFF",
  surfaceMuted: "#F5F5F5",
  border: "#E5E5E5",
  fg: "#0A0A0A",
  muted: "#666666",
  faint: "#8F8F8F",
  success: "#157F3A",
  warning: "#AC4705",
  danger: "#BE1B1B",
  info: "#1E6EE8",
  btnPrimary: "#111111",
  btnPrimaryFg: "#FFFFFF",
};

const dark: Tokens = {
  bg: "#0F0F0F",
  surface: "#161616",
  surfaceMuted: "#1D1D1D",
  border: "#2E2E2E",
  fg: "#F5F5F5",
  muted: "#9A9A9A",
  faint: "#6B6B6B",
  success: "#4CC97E",
  warning: "#E8A33D",
  danger: "#F26D6D",
  info: "#5B9CF8",
  btnPrimary: "#F5F5F5",
  btnPrimaryFg: "#111111",
};

/** 当前主题 token(跟随系统;白色优先 → 系统没说暗就是浅) */
export function useTokens(): Tokens {
  return useColorScheme() === "dark" ? dark : light;
}
