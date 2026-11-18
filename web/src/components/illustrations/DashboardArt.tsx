import { cn } from "@/lib/utils";

const sparkle = (x: number, y: number, s = 1, cls = "illo-accent") => (
  <path
    key={`${x}-${y}`}
    className={cn("illo-twinkle", cls)}
    transform={`translate(${x} ${y}) scale(${s})`}
    d="M0 -6 C1 -1.6 1.6 -1 6 0 C1.6 1 1 1.6 0 6 C-1 1.6 -1.6 1 -6 0 C-1.6 -1 -1 -1.6 0 -6 Z"
  />
);

/**
 * 仪表盘横幅插画:机柜 + 浮起的行情卡片 + 闪电(抢购) + 对勾(下单成功)。
 * 自绘 SVG,只用 .illo-* 主题类着色,明暗主题自动适配;纯装饰,读屏忽略。
 */
export function DashboardArt({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 240 150" className={cn("illo", className)} aria-hidden focusable="false">
      <ellipse cx="146" cy="136" rx="78" ry="7" className="illo-shadow" />
      <circle cx="150" cy="76" r="64" className="illo-halo" />
      <circle cx="150" cy="76" r="50" className="illo-ring" />

      {/* 机柜 */}
      <rect x="112" y="30" width="76" height="98" rx="11" className="illo-card" />
      {[0, 1, 2].map((i) => {
        const y = 40 + i * 28;
        return (
          <g key={i}>
            <rect x="120" y={y} width="60" height="20" rx="5" className="illo-panel-2" />
            <circle cx="129" cy={y + 10} r="3" className={i === 1 ? "illo-warm illo-pulse" : "illo-accent"} />
            <rect x="137" y={y + 6} width="26" height="3" rx="1.5" className="illo-line" />
            <rect x="137" y={y + 12} width="16" height="3" rx="1.5" className="illo-line" />
            <rect x="168" y={y + 6} width="5" height="8" rx="1.5" className="illo-panel" />
          </g>
        );
      })}

      {/* 浮起的行情卡片 */}
      <g className="illo-float">
        <rect x="30" y="46" width="78" height="52" rx="10" className="illo-card" />
        <rect x="40" y="56" width="22" height="5" rx="2.5" className="illo-accent" />
        <rect x="40" y="66" width="34" height="3.5" rx="1.75" className="illo-line" />
        <path d="M40 88 L52 80 L62 84 L74 72 L86 76 L98 64" className="illo-stroke-accent" />
        <circle cx="98" cy="64" r="3" className="illo-accent" />
      </g>

      {/* 闪电:抢购 */}
      <circle cx="194" cy="34" r="15" className="illo-warm" />
      <path d="M196 23 L187 36 H194 L191 46 L201 32 H194 Z" fill="#fff" />

      {/* 对勾:下单成功 */}
      <circle cx="104" cy="114" r="11" className="illo-accent" />
      <path d="M99 114 l3.5 3.5 l6.5 -7" className="illo-stroke-card" />

      {sparkle(30, 24, 0.9)}
      {sparkle(218, 92, 0.75, "illo-warm")}
      {sparkle(70, 124, 0.6)}
    </svg>
  );
}
