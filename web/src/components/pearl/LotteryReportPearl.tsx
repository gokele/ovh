import { ArrowRight, TrendingUp } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { HardwareLottery } from "@/hooks/use-server-control";
import { cn } from "@/lib/utils";

/**
 * 「你中奖了」卡片:实际交付的硬件优于订购配置。
 *
 * 左侧是自绘插画(礼盒打开、升级后的芯片升起),右侧只列真正有差异的部件,
 * 订购值弱化、实际值高亮并标出涨幅。没有差异的部件不显示,不再出现一排「—」。
 */
export function LotteryReportPearl({ lottery }: { lottery: HardwareLottery }) {
  const { t } = useTranslation();
  if (!lottery.checked || !lottery.won || lottery.items.length === 0) return null;
  const tier = Math.min(Math.max(lottery.tier || 1, 1), 3);

  const gainText = (item: { gainPct?: number; mediaUp?: boolean }) => {
    if (item.gainPct && item.gainPct >= 1) return t("maint.overview.lottery.gainPct", { pct: Math.round(item.gainPct) });
    if (item.mediaUp) return t("maint.overview.lottery.gainMedia");
    return t("maint.overview.lottery.upgraded");
  };

  return (
    <section
      aria-label={t(`maint.overview.lottery.title${tier}`)}
      className="relative overflow-hidden rounded-2xl border border-amber-300/60 dark:border-amber-400/25 bg-gradient-to-br from-amber-50 via-background to-background dark:from-amber-400/10 dark:via-background dark:to-background"
    >
      <div className="flex flex-col sm:flex-row sm:items-center gap-4 p-4 sm:p-5">
        <LotteryIllustration tier={tier} className="w-24 h-20 sm:w-32 sm:h-28 flex-shrink-0 self-center sm:self-auto" />

        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-2 flex-wrap">
            <h3 className={cn("font-semibold text-amber-700 dark:text-amber-300", tier === 3 ? "text-lg" : "text-base")}>
              {t(`maint.overview.lottery.title${tier}`)}
            </h3>
            {lottery.planName && <span className="text-xs text-muted-foreground truncate">{lottery.planName}</span>}
          </div>
          <p className="text-[13px] text-muted-foreground mt-0.5">{t("maint.overview.lottery.lead")}</p>

          <ul className="mt-3 divide-y divide-border/70 rounded-xl border border-border/70 bg-background/70 dark:bg-background/40">
            {lottery.items.map((item) => (
              <li key={item.kind} className="grid grid-cols-[3.5rem_1fr] sm:grid-cols-[4rem_1fr_auto] items-center gap-x-3 gap-y-1 px-3 py-2.5">
                <span className="text-xs font-medium text-muted-foreground">{t(`maint.overview.lottery.kind.${item.kind}`)}</span>
                <div className="min-w-0 flex items-center gap-2 flex-wrap text-[13px]">
                  <span className="text-muted-foreground line-through decoration-muted-foreground/40 break-all" title={t("maint.overview.lottery.plateOrdered")}>
                    {item.ordered || "—"}
                  </span>
                  <ArrowRight className="w-3.5 h-3.5 text-amber-500 flex-shrink-0" aria-hidden />
                  <span className="font-semibold text-foreground break-all" title={t("maint.overview.lottery.plateActual")}>
                    {item.actual}
                  </span>
                </div>
                <span className="col-start-2 sm:col-start-auto justify-self-start sm:justify-self-end inline-flex items-center gap-1 rounded-full bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 px-2 py-0.5 text-[11px] font-medium whitespace-nowrap">
                  <TrendingUp className="w-3 h-3" aria-hidden />
                  {gainText(item)}
                </span>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </section>
  );
}

/** 自绘插画:打开的礼盒里升起一枚芯片。颜色走主题类,明暗模式都可读;tier 越高星光越多。 */
function LotteryIllustration({ tier, className }: { tier: number; className?: string }) {
  return (
    <svg viewBox="0 0 128 112" className={className} role="img" aria-hidden>
      {/* 背景光晕 */}
      <circle cx="64" cy="58" r="48" className="fill-amber-200/50 dark:fill-amber-400/10" />
      <circle cx="64" cy="58" r="34" className="fill-amber-100 dark:fill-amber-300/10" />

      {/* 升起的芯片 */}
      <g className="lottery-chip">
        <g className="stroke-amber-600 dark:stroke-amber-300" strokeWidth="2.2" strokeLinecap="round">
          {[48, 56, 64, 72, 80].map((x) => (
            <g key={x}>
              <line x1={x} y1="16" x2={x} y2="22" />
              <line x1={x} y1="58" x2={x} y2="64" />
            </g>
          ))}
          {[30, 38, 46].map((y) => (
            <g key={y}>
              <line x1="38" y1={y} x2="44" y2={y} />
              <line x1="84" y1={y} x2="90" y2={y} />
            </g>
          ))}
        </g>
        <rect x="44" y="22" width="40" height="36" rx="6" className="fill-slate-800 dark:fill-slate-900 stroke-amber-500 dark:stroke-amber-300" strokeWidth="2" />
        <rect x="52" y="30" width="24" height="20" rx="3" className="fill-amber-400 dark:fill-amber-300" />
        {/* 芯片上的升级箭头 */}
        <path d="M64 33 L71 41 H67 V47 H61 V41 H57 Z" className="fill-slate-800 dark:fill-slate-900" />
      </g>

      {/* 礼盒盖(掀开) */}
      <g transform="rotate(-14 30 66)">
        <rect x="22" y="60" width="50" height="10" rx="3" className="fill-rose-500 dark:fill-rose-400" />
        <rect x="42" y="60" width="9" height="10" className="fill-amber-300 dark:fill-amber-200" />
      </g>
      {/* 礼盒盒身 */}
      <rect x="34" y="72" width="60" height="32" rx="4" className="fill-rose-500 dark:fill-rose-400" />
      <rect x="34" y="72" width="60" height="7" className="fill-rose-600/70 dark:fill-rose-500/70" />
      <rect x="59" y="72" width="10" height="32" className="fill-amber-300 dark:fill-amber-200" />

      {/* 星光:等级越高越多 */}
      <Sparkle x={22} y={30} s={1} className="fill-amber-400 dark:fill-amber-300" />
      <Sparkle x={104} y={24} s={0.8} className="fill-sky-400 dark:fill-sky-300" />
      {tier >= 2 && <Sparkle x={108} y={70} s={0.7} className="fill-emerald-400 dark:fill-emerald-300" />}
      {tier >= 3 && <Sparkle x={16} y={84} s={0.6} className="fill-fuchsia-400 dark:fill-fuchsia-300" />}
    </svg>
  );
}

function Sparkle({ x, y, s, className }: { x: number; y: number; s: number; className?: string }) {
  return (
    <path
      className={cn("lottery-sparkle", className)}
      transform={`translate(${x} ${y}) scale(${s})`}
      d="M0 -9 C1.5 -2.5 2.5 -1.5 9 0 C2.5 1.5 1.5 2.5 0 9 C-1.5 2.5 -2.5 1.5 -9 0 C-2.5 -1.5 -1.5 -2.5 0 -9 Z"
    />
  );
}
