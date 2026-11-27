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
 * 登录页插画:机柜 + 前方的盾牌(只有你能进) + 闪电(抢购) + 浮起的「有货」卡片。
 * 与 DashboardArt 同一套 .illo-* 主题类,明暗主题自动适配;纯装饰,读屏忽略。
 */
export function LoginArt({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 260 190" className={cn("illo", className)} aria-hidden focusable="false">
      <ellipse cx="130" cy="176" rx="92" ry="8" className="illo-shadow" />
      <circle cx="130" cy="92" r="78" className="illo-halo" />
      <circle cx="130" cy="92" r="60" className="illo-ring" />

      {/* 机柜 */}
      <rect x="92" y="34" width="76" height="128" rx="12" className="illo-card" />
      {[0, 1, 2, 3].map((i) => {
        const y = 44 + i * 28;
        return (
          <g key={i}>
            <rect x="100" y={y} width="60" height="20" rx="5" className="illo-panel-2" />
            <circle cx="109" cy={y + 10} r="3" className={i === 2 ? "illo-warm illo-pulse" : "illo-accent"} />
            <rect x="117" y={y + 6} width="24" height="3" rx="1.5" className="illo-line" />
            <rect x="117" y={y + 12} width="14" height="3" rx="1.5" className="illo-line" />
            <rect x="148" y={y + 6} width="5" height="8" rx="1.5" className="illo-panel" />
          </g>
        );
      })}

      {/* 盾牌 + 对勾:账号守着这扇门 */}
      <g transform="translate(150 104)">
        <path d="M24 0 L46 8 V26 C46 42 36 52 24 58 C12 52 2 42 2 26 V8 Z" className="illo-accent" />
        <path d="M24 6 L40 12 V26 C40 38 33 46 24 51 C15 46 8 38 8 26 V12 Z" className="illo-accent-soft" opacity=".35" />
        <path d="M15 29 l6.5 6.5 l12 -13" className="illo-stroke-card" />
      </g>

      {/* 浮起的「有货」卡片 */}
      <g className="illo-float">
        <rect x="22" y="58" width="74" height="46" rx="10" className="illo-card" />
        <circle cx="35" cy="71" r="4" className="illo-accent" />
        <rect x="44" y="68" width="30" height="5" rx="2.5" className="illo-ink" opacity=".75" />
        <rect x="32" y="83" width="50" height="3.5" rx="1.75" className="illo-line" />
        <rect x="32" y="91" width="32" height="3.5" rx="1.75" className="illo-line" />
      </g>

      {/* 闪电:抢购 */}
      <circle cx="178" cy="36" r="16" className="illo-warm" />
      <path d="M180 24 L170 38 H177 L174 49 L185 34 H178 Z" fill="#fff" />

      {sparkle(40, 32, 0.9)}
      {sparkle(228, 78, 0.75, "illo-warm")}
      {sparkle(60, 140, 0.6)}
    </svg>
  );
}
