import { useCallback, useEffect, useRef, useState } from "react";

/**
 * 观察容器实际宽度是否达到 minWidth。
 * 表格只在放得下时显示,放不下改用紧凑列表 —— 侧栏收起、分屏、窗口缩放都会改变可用宽度,
 * 只看屏幕断点不可靠,而且永远不出现横向滚动。
 * 返回回调 ref:容器晚于首屏挂载(加载态之后才出现)时同样能开始观察。
 */
export function useContainerWide<T extends HTMLElement = HTMLDivElement>(minWidth: number) {
  const [wide, setWide] = useState(false);
  const observer = useRef<ResizeObserver | null>(null);
  const offResize = useRef<(() => void) | null>(null);
  const ref = useCallback(
    (el: T | null) => {
      observer.current?.disconnect();
      observer.current = null;
      offResize.current?.();
      offResize.current = null;
      if (!el) return;
      const update = () => setWide(el.clientWidth >= minWidth);
      update();
      observer.current = new ResizeObserver(update);
      observer.current.observe(el);
      // 保险:窗口尺寸变化时也重测一次(个别环境下 ResizeObserver 会漏掉一次变化,停在窄版布局)
      const onResize = () => window.requestAnimationFrame(update);
      window.addEventListener("resize", onResize);
      offResize.current = () => window.removeEventListener("resize", onResize);
    },
    [minWidth],
  );
  useEffect(
    () => () => {
      observer.current?.disconnect();
      offResize.current?.();
    },
    [],
  );
  return [ref, wide] as const;
}
