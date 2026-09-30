import { Toaster } from "sonner";
import { useTheme } from "@/hooks/use-theme";

/**
 * 跟随应用主题的 Toaster。
 *
 * sonner 的 theme 默认是 "light" 且不吃 CSS 变量 —— 不显式传的话,
 * 深色界面下弹出来的通知是白底亮色,和周围割裂。
 * 传 resolved(实际生效值)而不是 mode:选"跟随系统"时它自己不会跟着系统翻。
 *
 * position 固定 top-center:右上角是账户 chip 和主题切换(可连点),bottom 是
 * 手机标签栏和对话框操作区 —— 两头都碰不得。实测 top-right 时连点主题按钮
 * 会被自己的 toast 盖住。
 */
export function ThemedToaster() {
  const { resolved } = useTheme();
  return <Toaster position="top-center" theme={resolved} richColors closeButton />;
}
