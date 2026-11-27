import { useTranslation } from "react-i18next";
import type { OrderTracking } from "@/hooks/use-account";
import { fmtDateTime } from "@/i18n/format";
import { cn } from "@/lib/utils";

/** Both the orders page and single-order shortcuts use the same OVH events. */
export function OrderTimeline({ data }: { data: OrderTracking }) {
  const { t } = useTranslation();
  if (data.followUpError) return <p className="text-[12px] text-muted-foreground">{t("orderTrack.followUpFailed", { detail: data.followUpError })}</p>;
  const history = data.steps.flatMap((s) => s.history).sort((a, b) => Date.parse(b.date) - Date.parse(a.date));
  if (!history.length) return <p className="text-[12.5px] text-muted-foreground">{t("orderTrack.noTimeline")}</p>;
  return (
    <ol className="relative space-y-3 before:absolute before:left-[5px] before:top-1.5 before:bottom-1.5 before:w-px before:bg-border">
      {history.map((h, i) => (
        <li key={i} className="relative pl-5">
          <span className={cn("absolute left-0 top-1.5 w-[11px] h-[11px] rounded-full ring-2 ring-background", i === 0 ? "bg-primary" : "bg-muted-foreground/40")} />
          <div className="flex flex-wrap items-baseline gap-x-2 text-[13px]">
            <span className="font-medium">{t(`orderTrack.label.${h.label}`, { defaultValue: h.label })}</span>
            <span className="text-[11.5px] text-muted-foreground tabular-nums">{h.date ? fmtDateTime(h.date) : ""}</span>
          </div>
          {h.description && <p className="text-[12px] text-muted-foreground break-words">{h.description}</p>}
        </li>
      ))}
    </ol>
  );
}
