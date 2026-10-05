import { useState, useMemo } from "react";
import { Command } from "cmdk";
import { useTranslation } from "react-i18next";
import { Check, ChevronsUpDown, Search, X } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { ServerPlan } from "@/hooks/use-servers";

/** 服务器 planCode 选择器,Combobox(Popover + cmdk Command):
 *  - 触发器看起来像 Input,带 chevron;选了之后显示 planCode + 服务器名
 *  - 弹层里有搜索框 + 过滤列表,每行 planCode + 名称 + CPU 简介
 *  - 键盘导航(↑↓ + Enter),Esc 关
 *  - 也支持手动输入(选错列表时输入框 fallback 走 onChange)
 */
export function PlanCodeCombobox({
  value,
  onChange,
  servers,
  placeholder,
  className,
}: {
  value: string;
  onChange: (planCode: string) => void;
  servers: ServerPlan[];
  placeholder?: string;
  className?: string;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");

  const matched = useMemo(
    () => servers.find((s) => s.planCode === value),
    [servers, value]
  );

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return servers;
    return servers.filter((s) => {
      const hay = `${s.planCode} ${s.name} ${s.cpu} ${s.memory} ${s.storage}`.toLowerCase();
      return hay.includes(q);
    });
  }, [servers, query]);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          className={cn(
            "w-full justify-between font-normal h-10 px-3.5 rounded-xl",
            !value && "text-muted-foreground",
            className
          )}
        >
          {value ? (
            <span className="flex items-baseline gap-2 min-w-0">
              <code className="font-mono font-semibold text-foreground truncate">{value}</code>
              {matched?.name && (
                <span className="text-[12px] text-muted-foreground truncate">{matched.name}</span>
              )}
            </span>
          ) : (
            <span>{placeholder || t("commons.planCode.placeholder")}</span>
          )}
          <div className="flex items-center gap-1">
            {value && (
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  onChange("");
                }}
                className="rounded hover:bg-muted p-0.5"
                aria-label={t("commons.planCode.clear")}
              >
                <X className="w-3.5 h-3.5 text-muted-foreground" />
              </button>
            )}
            <ChevronsUpDown className="w-4 h-4 opacity-50 shrink-0" />
          </div>
        </Button>
      </PopoverTrigger>
      <PopoverContent
        className="w-[var(--radix-popover-trigger-width)] p-0"
        sideOffset={4}
        // Combobox 嵌在 Radix Dialog 里时,Dialog 会拦 pointerdown / wheel 事件,
        // 导致列表无法用鼠标滚轮 / 触摸板下滑。stopPropagation 让事件留在 Popover 里。
        onWheel={(e) => e.stopPropagation()}
        onTouchMove={(e) => e.stopPropagation()}
      >
        <Command shouldFilter={false}>
          <div className="flex items-center border-b border-border px-3">
            <Search className="w-3.5 h-3.5 text-muted-foreground mr-2 shrink-0" />
            <Command.Input
              value={query}
              onValueChange={setQuery}
              placeholder={t("commons.planCode.searchPlaceholder")}
              className="flex h-10 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              autoFocus
            />
          </div>
          <Command.List
            className="max-h-80 overflow-y-auto overscroll-contain p-1"
            // overscroll-contain 兜底:wheel 到达列表上下边界时不再冒泡到 Dialog
            onWheel={(e) => e.stopPropagation()}
          >
            <Command.Empty className="py-6 text-center text-[12px] text-muted-foreground">
              {t("commons.planCode.noMatch")}
            </Command.Empty>
            {filtered.map((s) => {
              const selected = s.planCode === value;
              return (
                <Command.Item
                  key={s.planCode}
                  value={s.planCode}
                  onSelect={() => {
                    onChange(s.planCode);
                    setOpen(false);
                    setQuery("");
                  }}
                  className={cn(
                    "flex items-center gap-2 rounded-md px-2 py-1.5 text-sm cursor-pointer",
                    "aria-selected:bg-muted aria-selected:text-foreground",
                    selected && "bg-muted/60"
                  )}
                >
                  <Check
                    className={cn(
                      "w-3.5 h-3.5 shrink-0",
                      selected ? "opacity-100" : "opacity-0"
                    )}
                  />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-baseline gap-2">
                      <code className="font-mono font-semibold text-[13px] truncate">{s.planCode}</code>
                      <span className="text-[12px] text-muted-foreground truncate">{s.name}</span>
                    </div>
                    {(s.cpu || s.memory) && (
                      <div className="text-[11px] text-muted-foreground truncate mt-0.5">
                        {[s.cpu, s.memory, s.storage].filter(Boolean).join(" · ")}
                      </div>
                    )}
                  </div>
                </Command.Item>
              );
            })}
          </Command.List>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
