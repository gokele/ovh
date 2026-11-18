import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { PlusCircle } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";

export interface FacetOption {
  value: string;
  label: ReactNode;
  count: number;
}

/**
 * 多选筛选按钮(参考 shadcn Tasks 的 faceted filter):
 * 未选时是虚线按钮「⊕ 机房」;选中后在按钮里显示已选项,点开是带数量的勾选列表。
 */
export function FacetFilter({
  title,
  options,
  value,
  onChange,
}: {
  title: string;
  options: FacetOption[];
  value: Set<string>;
  onChange: (next: Set<string>) => void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const toggle = (v: string) => {
    const next = new Set(value);
    if (next.has(v)) next.delete(v);
    else next.add(v);
    onChange(next);
  };
  const selected = options.filter((o) => value.has(o.value));
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            "inline-flex items-center gap-1.5 h-8 px-2.5 rounded-lg border text-[12.5px] transition-colors whitespace-nowrap",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            selected.length ? "border-primary/40 bg-primary/[.05] text-foreground" : "border-dashed border-border text-muted-foreground hover:text-foreground hover:border-foreground/30",
          )}
        >
          <PlusCircle className={cn("w-3.5 h-3.5", selected.length && "text-primary")} />
          {title}
          {selected.length > 0 && (
            <>
              <span className="mx-0.5 h-3.5 w-px bg-border" />
              {selected.length <= 2 ? (
                selected.map((o) => (
                  <span key={o.value} className="rounded px-1.5 py-px bg-primary/10 text-primary text-[11.5px] font-medium">
                    {o.label}
                  </span>
                ))
              ) : (
                <span className="rounded px-1.5 py-px bg-primary/10 text-primary text-[11.5px] font-medium tabular-nums">{selected.length}</span>
              )}
            </>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-56 p-1.5">
        {options.length === 0 ? (
          <p className="px-2.5 py-3 text-center text-[12.5px] text-muted-foreground">{t("queue.row.facetEmpty")}</p>
        ) : (
          <div className="max-h-72 overflow-y-auto">
            {options.map((o) => (
              <label
                key={o.value}
                className="flex items-center gap-2.5 px-2.5 h-9 rounded-md text-[13px] cursor-pointer hover:bg-muted transition-colors"
              >
                <Checkbox checked={value.has(o.value)} onCheckedChange={() => toggle(o.value)} />
                <span className="flex-1 min-w-0 truncate">{o.label}</span>
                <span className="text-[11.5px] text-muted-foreground tabular-nums">{o.count}</span>
              </label>
            ))}
          </div>
        )}
        {selected.length > 0 && (
          <>
            <div className="my-1 h-px bg-border" />
            <button
              type="button"
              className="w-full h-8 rounded-md text-[12.5px] text-muted-foreground hover:bg-muted hover:text-foreground"
              onClick={() => onChange(new Set())}
            >
              {t("queue.row.reset")}
            </button>
          </>
        )}
      </PopoverContent>
    </Popover>
  );
}
