import { useId } from "react";
import { cn } from "@/lib/utils";

/**
 * 品牌标志:蓝色渐变圆角方块 + 白色圆环(延续原 App 图标的环) + 穿环而过的闪电(抢购要快)。
 * 与 public/favicon.svg 同一图形;id 用 useId 生成,同页多处使用不会串渐变。
 */
export function BrandLogo({ className, title }: { className?: string; title?: string }) {
  const id = useId().replace(/:/g, "");
  const bolt = "M35.5 11.5 L23.5 34.5 H31 L28.5 52.5 L40.5 29.5 H33 Z";
  return (
    <svg viewBox="0 0 64 64" className={cn("block", className)} role={title ? "img" : undefined} aria-hidden={title ? undefined : true}>
      {title && <title>{title}</title>}
      <defs>
        <linearGradient id={`${id}-bg`} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#4a8cff" />
          <stop offset="1" stopColor="#1a50d8" />
        </linearGradient>
        <radialGradient id={`${id}-hl`} cx="0.15" cy="0.05" r="0.75">
          <stop offset="0" stopColor="#fff" stopOpacity=".22" />
          <stop offset="1" stopColor="#fff" stopOpacity="0" />
        </radialGradient>
        <mask id={`${id}-cut`}>
          <rect width="64" height="64" fill="#fff" />
          <path d={bolt} fill="#000" stroke="#000" strokeWidth="7" strokeLinejoin="round" />
        </mask>
      </defs>
      <rect width="64" height="64" rx="14" fill={`url(#${id}-bg)`} />
      <rect width="64" height="64" rx="14" fill={`url(#${id}-hl)`} />
      <circle cx="32" cy="32" r="16.5" fill="none" stroke="#fff" strokeWidth="6" mask={`url(#${id}-cut)`} className="brand-ring" />
      <path d={bolt} fill="#fff" stroke="#fff" strokeWidth="1.2" strokeLinejoin="round" className="brand-bolt" />
    </svg>
  );
}
