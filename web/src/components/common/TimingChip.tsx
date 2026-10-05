import { Timer } from "lucide-react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";

export interface PhaseTiming {
  name: string;
  ms: number;
}

/**
 * 抢购耗时。
 *
 * 为什么值得占一块地方：抢购输了之后，用户唯一能问的问题是"我慢在哪一步"。
 * 没有这串数字的时候，"OVH 那边就是没货""我这台机器网络慢""某一步卡了 8 秒"
 * 三种情况长得一模一样，而它们要采取的行动完全不同 —— 换机型 / 换机器 / 提 issue。
 *
 * 平时只显示总耗时，点开才看分解：绝大多数时候用户只想扫一眼"这单快不快"。
 */
export function TimingChip({
  totalMs,
  phases,
  className,
}: {
  totalMs?: number;
  phases?: PhaseTiming[];
  className?: string;
}) {
  const { t } = useTranslation();
  if (!totalMs) return null;

  const slowest = (phases || []).reduce<PhaseTiming | null>(
    (a, p) => (!a || p.ms > a.ms ? p : a),
    null
  );

  const detail = () => {
    if (!phases?.length) {
      toast.info(t("commons.timing.totalToast", { total: fmtMs(totalMs) }));
      return;
    }
    toast.info(
      t("commons.timing.detailToast", {
        total: fmtMs(totalMs),
        breakdown: phases.map((p) => `${p.name} ${fmtMs(p.ms)}`).join("\n"),
      }),
      { duration: 8000, style: { whiteSpace: "pre-line" } }
    );
  };

  return (
    <button
      type="button"
      onClick={detail}
      title={
        slowest
          ? t("commons.timing.titleSlowest", { name: slowest.name, ms: fmtMs(slowest.ms) })
          : t("commons.timing.title")
      }
      className={
        "inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-mono " +
        "bg-secondary text-muted-foreground hover:bg-muted transition-colors " +
        (className || "")
      }
    >
      <Timer className="w-3 h-3" />
      {fmtMs(totalMs)}
    </button>
  );
}

/** 1832 → "1.83s"；210 → "210ms"。抢购的量级跨度大，统一单位不如各自可读 */
export function fmtMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}
