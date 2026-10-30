import { CheckCircle2, CircleDot, HelpCircle } from "lucide-react";
import { useTranslation } from "react-i18next";

/** 工单状态同时提供图标和文字,颜色只作为辅助。 */
export function TicketState({ state }: { state?: string }) {
  const { t } = useTranslation();
  const Icon =
    state === "open"
      ? CircleDot
      : state === "closed"
        ? CheckCircle2
        : HelpCircle;
  return (
    <span className="inline-flex w-fit shrink-0 items-center gap-1.5 rounded-full bg-secondary px-2.5 py-1 text-xs font-medium text-foreground">
      <Icon
        aria-hidden="true"
        className={`h-3.5 w-3.5 ${state === "closed" ? "text-success" : "text-muted-foreground"}`}
      />
      {t(`support.statuses.${state || "unknown"}`, {
        defaultValue: state || t("support.statuses.unknown"),
      })}
    </span>
  );
}
