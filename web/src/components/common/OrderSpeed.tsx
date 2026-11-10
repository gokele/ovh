import { Hourglass, Zap } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Chip } from "@/components/common/Chip";

/** 亚毫秒保留两位小数,否则按毫秒显示。 */
export function formatMicros(us: number): string {
  return us < 1000 ? `${(us / 1000).toFixed(2)}ms` : `${Math.round(us / 1000)}ms`;
}

/** 等待时长:秒 / 分 / 时 / 天,只取两级。 */
export function formatWait(ms: number, t: (k: string, o?: Record<string, unknown>) => string): string {
  const s = Math.floor(ms / 1000);
  if (s < 60) return t("commons.timing.waitS", { s });
  const m = Math.floor(s / 60);
  if (m < 60) return t("commons.timing.waitM", { m, s: s % 60 });
  const h = Math.floor(m / 60);
  if (h < 24) return t("commons.timing.waitH", { h, m: m % 60 });
  return t("commons.timing.waitD", { d: Math.floor(h / 24), h: h % 24 });
}

/**
 * 抢到订单的两个关键耗时:发现库存到发出结账(本机反应速度),以及从创建任务到抢到(等了多久)。
 * 没有测到的项不显示。
 */
export function OrderSpeed({
  detectionToWriteUs,
  prewarmed,
  connectionReused,
  waitMs,
}: {
  detectionToWriteUs?: number;
  prewarmed?: boolean;
  connectionReused?: boolean;
  waitMs?: number;
}) {
  const { t } = useTranslation();
  const notes = [prewarmed && t("commons.timing.prewarmed"), connectionReused && t("commons.timing.reused")].filter(Boolean).join(" · ");
  return (
    <>
      {detectionToWriteUs ? (
        <Chip tone="success" title={[t("commons.timing.dispatchTitle"), notes].filter(Boolean).join("\n")}>
          <Zap className="w-3 h-3" aria-hidden />
          {t("commons.timing.dispatch", { value: formatMicros(detectionToWriteUs) })}
        </Chip>
      ) : null}
      {waitMs ? (
        <Chip tone="default" title={t("commons.timing.waitTitle")}>
          <Hourglass className="w-3 h-3" aria-hidden />
          {formatWait(waitMs, t)}
        </Chip>
      ) : null}
    </>
  );
}
