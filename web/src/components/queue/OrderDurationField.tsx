import { useId } from "react";
import { useTranslation } from "react-i18next";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { durationMonths, RESERVATION_DURATIONS, supportsDuration } from "@/lib/order-duration";
import type { useOrderDurations } from "@/hooks/use-order-durations";

export function OrderDurationLabel({ value }: { value?: string }) {
  const { t } = useTranslation();
  const months = durationMonths(value);
  return <>{t(months % 12 === 0 ? "orderDuration.years" : "orderDuration.months", { count: months % 12 === 0 ? months / 12 : months })}</>;
}

export function OrderDurationField({ value, onChange, query }: {
  value: string; onChange: (value: string) => void; query: ReturnType<typeof useOrderDurations>;
}) {
  const { t } = useTranslation();
  const id = useId();
  const valid = supportsDuration(query.data, value);
  const choices = query.data?.length ? query.data : RESERVATION_DURATIONS;
  const options = [...new Set([...choices, value])].sort((a, b) => durationMonths(a) - durationMonths(b));
  return <div className="space-y-1.5">
    <label htmlFor={id} className="block text-[13px] font-medium">{t("orderDuration.label")}</label>
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger id={id} aria-describedby={`${id}-hint`} aria-invalid={!valid} className="min-h-11">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>{options.map((option) => <SelectItem key={option} value={option} disabled={!supportsDuration(query.data, option)}>
        <OrderDurationLabel value={option} />{option === "P1M" ? ` · ${t("orderDuration.default")}` : ""}
      </SelectItem>)}</SelectContent>
    </Select>
    <p id={`${id}-hint`} role={!valid ? "alert" : "status"} className={`text-xs leading-relaxed ${!valid ? "text-destructive" : "text-muted-foreground"}`}>
      {!valid ? t("orderDuration.unsupported") : query.isFetching ? t("orderDuration.loading") : query.isError || !query.data?.length ? t("orderDuration.unverified") : t("orderDuration.hint")}
    </p>
    {durationMonths(value) > 1 && <p className="text-xs leading-relaxed text-muted-foreground">{t("orderDuration.prepaid")}</p>}
  </div>;
}
