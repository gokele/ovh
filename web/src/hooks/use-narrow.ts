import { useEffect, useState } from "react";

/** Tailwind 的 sm 断点。跟 tailwind.config 保持一致,改了那边这里也要改 */
const SM = 640;

/**
 * 当前是不是窄屏(< sm)。
 *
 * 给那些"光靠 CSS 改不动"的地方用 —— 比如 SVG 的 width/height 属性、
 * 图表的像素尺寸。纯排版能用 Tailwind 断点解决的就别用这个 hook。
 *
 * 必须监听 resize:只在渲染时读一次 window.innerWidth 的话,
 * 手机横竖屏切换、iPad 分屏拖动之后尺寸就停在旧值上了。
 */
export function useIsNarrow(): boolean {
  const [narrow, setNarrow] = useState(
    () => typeof window !== "undefined" && window.innerWidth < SM
  );
  useEffect(() => {
    const mq = window.matchMedia(`(max-width: ${SM - 1}px)`);
    const onChange = () => setNarrow(mq.matches);
    onChange();
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);
  return narrow;
}
