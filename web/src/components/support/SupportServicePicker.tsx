import { useState } from "react";
import { Command } from "cmdk";
import { Check, ChevronsUpDown, Search, Server } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

/** 搜索已有服务,也可显式使用输入的服务名;不使用浏览器样式不一致的 datalist。 */
export function SupportServicePicker({
  value,
  onChange,
  services,
  disabled,
  loading,
}: {
  value: string;
  onChange: (value: string) => void;
  services: string[];
  disabled?: boolean;
  loading?: boolean;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const filtered = services.filter((service) =>
    service.toLowerCase().includes(query.toLowerCase()),
  );
  /** 选择后关闭浮层并清空搜索,防止上一次搜索影响下一次选择。 */
  const choose = (service: string) => {
    onChange(service);
    setOpen(false);
    setQuery("");
  };
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          id="support-service"
          type="button"
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className="h-11 w-full justify-between rounded-xl px-3 font-normal"
        >
          <span className="flex min-w-0 items-center gap-2">
            <Server aria-hidden="true" className="text-muted-foreground" />
            <span
              className={`truncate ${value ? "" : "text-muted-foreground"}`}
            >
              {value || t("support.serviceChoose")}
            </span>
          </span>
          <ChevronsUpDown
            aria-hidden="true"
            className="shrink-0 text-muted-foreground"
          />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        className="w-[var(--radix-popover-trigger-width)]"
        onWheel={(event) => event.stopPropagation()}
        onTouchMove={(event) => event.stopPropagation()}
      >
        <Command shouldFilter={false} label={t("support.serviceSuggestions")}>
          <div className="flex items-center gap-2 border-b px-3">
            <Search
              aria-hidden="true"
              className="h-4 w-4 text-muted-foreground"
            />
            <Command.Input
              value={query}
              onValueChange={setQuery}
              placeholder={t("support.searchService")}
              aria-label={t("support.searchService")}
              className="h-11 w-full bg-transparent text-sm outline-none"
            />
          </div>
          <Command.List
            label={t("support.serviceSuggestions")}
            className="max-h-60 overflow-y-auto p-1 overscroll-contain"
          >
            <Command.Item
              value="__none"
              onSelect={() => choose("")}
              className="flex min-h-11 cursor-pointer items-center gap-2 rounded-lg px-3 text-sm text-muted-foreground aria-selected:bg-muted"
            >
              <Check
                aria-hidden="true"
                className={`h-4 w-4 ${value ? "opacity-0" : ""}`}
              />
              {t("support.noService")}
            </Command.Item>
            {filtered.map((service) => (
              <Command.Item
                key={service}
                value={service}
                onSelect={() => choose(service)}
                className="flex min-h-11 cursor-pointer items-center gap-2 rounded-lg px-3 text-sm aria-selected:bg-muted"
              >
                <Check
                  aria-hidden="true"
                  className={`h-4 w-4 shrink-0 ${value === service ? "" : "opacity-0"}`}
                />
                <span className="break-all">{service}</span>
              </Command.Item>
            ))}
            {query.trim() && !services.includes(query.trim()) && (
              <Command.Item
                value="__custom"
                onSelect={() => choose(query.trim())}
                className="min-h-11 cursor-pointer rounded-lg px-3 py-2 text-sm aria-selected:bg-muted"
              >
                {t("support.useService", { name: query.trim() })}
              </Command.Item>
            )}
            {loading && (
              <p
                role="status"
                className="px-3 py-2 text-xs text-muted-foreground"
              >
                {t("support.loadingServices")}
              </p>
            )}
          </Command.List>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
