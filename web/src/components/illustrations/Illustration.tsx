import { cn } from "@/lib/utils";

export type IllustrationName = "queue" | "monitor" | "history" | "search" | "servers" | "logs" | "error" | "orders" | "control";

/**
 * 自绘空态 / 错误插画。只用主题变量着色(.illo-* 类在 globals.css 定义),
 * 明暗主题自动适配;不加载外部图片,不含文字,屏幕阅读器忽略。
 */
export function Illustration({ name, className }: { name: IllustrationName; className?: string }) {
  return (
    <svg viewBox="0 0 160 120" className={cn("illo", className)} aria-hidden focusable="false">
      <ellipse cx="80" cy="104" rx="52" ry="7" className="illo-shadow" />
      {SCENES[name]}
    </svg>
  );
}

const sparkle = (x: number, y: number, s = 1, cls = "illo-accent") => (
  <path
    key={`${x}-${y}`}
    className={cn("illo-twinkle", cls)}
    transform={`translate(${x} ${y}) scale(${s})`}
    d="M0 -6 C1 -1.6 1.6 -1 6 0 C1.6 1 1 1.6 0 6 C-1 1.6 -1.6 1 -6 0 C-1.6 -1 -1 -1.6 0 -6 Z"
  />
);

const SCENES: Record<IllustrationName, React.ReactNode> = {
  // 发射台上待命的火箭:还没有抢购任务
  queue: (
    <g>
      <circle cx="80" cy="56" r="40" className="illo-halo" />
      <rect x="52" y="90" width="56" height="10" rx="3" className="illo-panel" />
      <rect x="60" y="84" width="40" height="8" rx="2" className="illo-panel-2" />
      <g className="illo-float">
        <path d="M80 22 C92 32 95 50 92 70 H68 C65 50 68 32 80 22 Z" className="illo-card" />
        <circle cx="80" cy="46" r="7" className="illo-accent" />
        <circle cx="80" cy="46" r="3.5" className="illo-card" />
        <path d="M68 60 L58 76 L68 72 Z" className="illo-warm" />
        <path d="M92 60 L102 76 L92 72 Z" className="illo-warm" />
        <path d="M74 70 H86 L83 80 H77 Z" className="illo-accent-soft" />
      </g>
      {sparkle(40, 34, 1)}
      {sparkle(122, 28, 0.8, "illo-warm")}
    </g>
  ),
  // 雷达与铃铛:还没有监控订阅
  monitor: (
    <g>
      <circle cx="80" cy="58" r="40" className="illo-halo" />
      <circle cx="80" cy="58" r="30" className="illo-ring" />
      <circle cx="80" cy="58" r="18" className="illo-ring" />
      <g className="illo-sweep">
        <path d="M80 58 L80 28 A30 30 0 0 1 106 43 Z" className="illo-accent-soft" />
      </g>
      <path d="M70 66 V56 a10 10 0 0 1 20 0 V66 l4 4 H66 Z" className="illo-card" />
      <circle cx="80" cy="74" r="3" className="illo-warm" />
      <circle cx="104" cy="40" r="3.5" className="illo-accent illo-pulse" />
      {sparkle(124, 70, 0.8)}
    </g>
  ),
  // 收据卡片:还没有抢购记录
  history: (
    <g>
      <circle cx="80" cy="56" r="40" className="illo-halo" />
      <rect x="58" y="26" width="48" height="62" rx="6" className="illo-panel-2" transform="rotate(-8 82 57)" />
      <path d="M54 24 H102 V90 L96 86 L90 90 L84 86 L78 90 L72 86 L66 90 L60 86 L54 90 Z" className="illo-card" />
      <rect x="62" y="34" width="24" height="5" rx="2.5" className="illo-accent" />
      <rect x="62" y="46" width="32" height="3.5" rx="1.75" className="illo-line" />
      <rect x="62" y="54" width="26" height="3.5" rx="1.75" className="illo-line" />
      <rect x="62" y="62" width="30" height="3.5" rx="1.75" className="illo-line" />
      <circle cx="108" cy="78" r="12" className="illo-warm" />
      <path d="M103 78 l3.5 3.5 l6.5 -7" className="illo-stroke-card" />
      {sparkle(40, 40, 0.9)}
    </g>
  ),
  // 放大镜找不到结果:筛选后为空
  search: (
    <g>
      <circle cx="80" cy="56" r="40" className="illo-halo" />
      <rect x="44" y="34" width="56" height="44" rx="8" className="illo-panel-2" />
      <rect x="52" y="44" width="30" height="4" rx="2" className="illo-line" />
      <rect x="52" y="54" width="22" height="4" rx="2" className="illo-line" />
      <circle cx="96" cy="62" r="17" className="illo-card" />
      <circle cx="96" cy="62" r="11" className="illo-halo-strong" />
      <path d="M108 74 L120 86" className="illo-stroke-accent" />
      <path d="M93 58 q3 -4 6 0 q0 3 -3 4 v2" className="illo-stroke-ink" />
      <circle cx="96" cy="68.5" r="1.3" className="illo-ink" />
    </g>
  ),
  // 服务器机柜:还没有机型
  servers: (
    <g>
      <circle cx="80" cy="56" r="40" className="illo-halo" />
      {[30, 50, 70].map((y, i) => (
        <g key={y}>
          <rect x="52" y={y} width="56" height="16" rx="4" className="illo-card" />
          <circle cx="62" cy={y + 8} r="2.5" className={i === 1 ? "illo-warm illo-pulse" : "illo-accent"} />
          <rect x="70" y={y + 6.5} width="22" height="3" rx="1.5" className="illo-line" />
        </g>
      ))}
      {sparkle(124, 34, 0.9)}
    </g>
  ),
  // 终端窗口:还没有日志
  logs: (
    <g>
      <circle cx="80" cy="56" r="40" className="illo-halo" />
      <rect x="44" y="30" width="72" height="54" rx="8" className="illo-card" />
      <rect x="44" y="30" width="72" height="11" rx="8" className="illo-panel-2" />
      <circle cx="52" cy="35.5" r="2" className="illo-warm" />
      <circle cx="59" cy="35.5" r="2" className="illo-accent-soft" />
      <path d="M54 52 l6 5 l-6 5" className="illo-stroke-accent" />
      <rect x="64" y="60" width="12" height="3" rx="1.5" className="illo-accent illo-blink" />
      <rect x="54" y="70" width="40" height="3" rx="1.5" className="illo-line" />
    </g>
  ),
  // 拔掉的插头:读取失败
  error: (
    <g>
      <circle cx="80" cy="56" r="40" className="illo-halo-danger" />
      <path d="M30 70 C44 70 46 56 60 56" className="illo-stroke-ink" />
      <rect x="58" y="48" width="16" height="16" rx="3" className="illo-card" />
      <path d="M74 52 h6 M74 60 h6" className="illo-stroke-ink" />
      <rect x="92" y="48" width="16" height="16" rx="3" className="illo-card" />
      <path d="M92 52 h-4 M92 60 h-4" className="illo-stroke-ink" />
      <path d="M108 56 C120 56 118 42 132 42" className="illo-stroke-ink" />
      <path d="M84 40 l-3 8 h4 l-3 8" className="illo-stroke-danger" />
    </g>
  ),
  // 购物袋:账户下没有订单
  orders: (
    <g>
      <circle cx="80" cy="56" r="40" className="illo-halo" />
      <path d="M56 42 H104 L100 88 H60 Z" className="illo-card" />
      <path d="M68 46 V38 a12 12 0 0 1 24 0 V46" className="illo-stroke-accent" />
      <circle cx="68" cy="47" r="2.5" className="illo-accent" />
      <circle cx="92" cy="47" r="2.5" className="illo-accent" />
      <rect x="70" y="62" width="20" height="4" rx="2" className="illo-line" />
      {sparkle(118, 38, 0.9, "illo-warm")}
    </g>
  ),
  // 控制面板与扳手:没有可管理的服务器
  control: (
    <g>
      <circle cx="80" cy="56" r="40" className="illo-halo" />
      <rect x="46" y="34" width="68" height="46" rx="8" className="illo-card" />
      <rect x="54" y="44" width="22" height="26" rx="4" className="illo-panel-2" />
      <rect x="82" y="44" width="24" height="4" rx="2" className="illo-line" />
      <rect x="82" y="54" width="18" height="4" rx="2" className="illo-line" />
      <rect x="82" y="64" width="22" height="4" rx="2" className="illo-accent" />
      <circle cx="65" cy="57" r="6" className="illo-accent-soft" />
      <path d="M114 78 l10 10" className="illo-stroke-warm" />
      <circle cx="112" cy="76" r="5" className="illo-warm" />
    </g>
  ),
};
