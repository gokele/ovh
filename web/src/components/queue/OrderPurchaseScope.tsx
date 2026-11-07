import { useTranslation } from "react-i18next";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

export type PurchaseMode = "perDatacenter" | "total";

export function OrderPurchaseScope({ id, value, onChange, disabled }: {
  id: string; value: PurchaseMode; onChange: (value: PurchaseMode) => void; disabled?: boolean;
}) {
  const { t } = useTranslation();
  return <div className="space-y-1.5">
    <label htmlFor={id} className="block text-[13px] font-medium">{t("queue.purchaseScope")}</label>
    <Select value={value} onValueChange={(v) => onChange(v as PurchaseMode)} disabled={disabled}>
      <SelectTrigger id={id} aria-describedby={`${id}-hint`} className="min-h-11"><SelectValue>{t(value === "total" ? "queue.scopeTotal" : "queue.scopePerDc")}</SelectValue></SelectTrigger>
      <SelectContent>
        <SelectItem value="total">{t("queue.scopeTotal")}</SelectItem>
        <SelectItem value="perDatacenter">{t("queue.scopePerDc")}</SelectItem>
      </SelectContent>
    </Select>
    <p id={`${id}-hint`} className="text-xs leading-relaxed text-muted-foreground">{t(value === "total" ? "queue.scopeTotalHint" : "queue.scopePerDcHint")}</p>
  </div>;
}
