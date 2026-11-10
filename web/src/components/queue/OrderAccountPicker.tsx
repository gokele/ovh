import { useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Check, Users } from "lucide-react";
import { accountChipColor, type OVHAccount } from "@/hooks/use-accounts";
import { endpointRegion } from "@/lib/ovh-regions";
import { cn } from "@/lib/utils";

const MEMORY_KEY = "order-accounts:";

/** 读取某型号上次使用的下单账户;浏览器存储不可用时安静地返回空。 */
export function rememberedAccounts(planCode: string): string[] {
  try {
    const raw = window.localStorage.getItem(MEMORY_KEY + planCode);
    const parsed = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === "string") : [];
  } catch {
    return [];
  }
}

export function rememberAccounts(planCode: string, ids: string[]) {
  try {
    window.localStorage.setItem(MEMORY_KEY + planCode, JSON.stringify(ids));
  } catch {
    // 只是便利功能,存不下不影响下单。
  }
}

/**
 * 下单账户多选。OVH 三个大区目录互不相通,只有和主账户同区的账户能下同一个型号,
 * 其他区账户置灰并说明原因。数字键 1-9 切换对应账户(输入框聚焦时不拦截)。
 */
export function OrderAccountPicker({
  accounts,
  primaryId,
  selected,
  onChange,
}: {
  accounts: OVHAccount[];
  primaryId: string;
  selected: string[];
  onChange: (ids: string[]) => void;
}) {
  const { t } = useTranslation();
  const primary = accounts.find((a) => a.id === primaryId);
  const region = endpointRegion(primary?.endpoint);
  const ordered = useMemo(
    () => [...accounts].sort((a, b) => Number(b.id === primaryId) - Number(a.id === primaryId)),
    [accounts, primaryId]
  );

  const toggle = (acc: OVHAccount) => {
    if (endpointRegion(acc.endpoint) !== region) return;
    const next = selected.includes(acc.id) ? selected.filter((id) => id !== acc.id) : [...selected, acc.id];
    // 至少保留一个账户,避免对话框进入"没有账户"的状态。
    onChange(next.length > 0 ? next : [acc.id]);
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null;
      if (e.metaKey || e.ctrlKey || e.altKey || !target || target.closest("input,textarea,select,[contenteditable=true]")) return;
      const n = Number(e.key);
      if (!Number.isInteger(n) || n < 1 || n > 9 || !ordered[n - 1]) return;
      e.preventDefault();
      toggle(ordered[n - 1]);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  if (accounts.length <= 1) return null;

  return (
    <div>
      <div className="flex items-center justify-between gap-2 mb-2">
        <h3 className="text-[13px] font-semibold flex items-center gap-1.5">
          <Users className="w-3.5 h-3.5 text-muted-foreground" />
          {t("queue.accountPicker.title", { n: selected.length })}
        </h3>
        <span className="text-[11px] text-muted-foreground hidden sm:inline">{t("queue.accountPicker.shortcut")}</span>
      </div>
      <div className="flex flex-wrap gap-1.5" role="group" aria-label={t("queue.accountPicker.title", { n: selected.length })}>
        {ordered.map((acc, index) => {
          const sameRegion = endpointRegion(acc.endpoint) === region;
          const on = selected.includes(acc.id);
          return (
            <button
              key={acc.id}
              type="button"
              disabled={!sameRegion}
              aria-pressed={on}
              title={sameRegion ? undefined : t("queue.accountPicker.otherRegion", { region: endpointRegion(acc.endpoint) })}
              onClick={() => toggle(acc)}
              className={cn(
                "inline-flex items-center gap-1.5 rounded-full border px-2.5 h-8 text-xs transition-colors",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                on ? "border-primary bg-primary/10 text-foreground" : "border-border hover:bg-secondary",
                !sameRegion && "opacity-45 cursor-not-allowed hover:bg-transparent"
              )}
            >
              {index < 9 && <kbd className="hidden sm:inline text-[10px] text-muted-foreground font-mono">{index + 1}</kbd>}
              {on ? <Check className="w-3.5 h-3.5 text-primary" /> : null}
              <span className="max-w-[9rem] truncate">{acc.name || acc.id}</span>
              <span className={cn("rounded px-1 text-[10px] font-mono", accountChipColor(acc.zone))}>
                {(acc.zone || endpointRegion(acc.endpoint)).toUpperCase()}
              </span>
            </button>
          );
        })}
      </div>
      <p className="mt-1.5 text-[11px] text-muted-foreground">
        {selected.length > 1 ? t("queue.accountPicker.sharedHint") : t("queue.accountPicker.singleHint")}
      </p>
    </div>
  );
}

export type PreorderTrigger = "onSale" | "inStock";

/** 预售触发方式:开售即下(默认)或有货再下。 */
export function PreorderTriggerPicker({ value, onChange }: { value: PreorderTrigger; onChange: (v: PreorderTrigger) => void }) {
  const { t } = useTranslation();
  const options: PreorderTrigger[] = ["onSale", "inStock"];
  return (
    <div>
      <h3 className="text-[13px] font-semibold mb-2">{t("queue.preorderTrigger.title")}</h3>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-2" role="radiogroup" aria-label={t("queue.preorderTrigger.title")}>
        {options.map((option) => (
          <button
            key={option}
            type="button"
            role="radio"
            aria-checked={value === option}
            onClick={() => onChange(option)}
            className={cn(
              "text-left rounded-xl border p-3 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              value === option ? "border-primary bg-primary/5" : "border-border hover:bg-secondary/60"
            )}
          >
            <div className="text-sm font-medium">{t(`queue.preorderTrigger.${option}`)}</div>
            <div className="text-[11px] text-muted-foreground mt-0.5">{t(`queue.preorderTrigger.${option}Hint`)}</div>
          </button>
        ))}
      </div>
    </div>
  );
}
