import { Search } from "lucide-react";
import type { ReactNode } from "react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

export interface SegmentOption<T extends string> {
  value: T;
  label: string;
  count?: number;
}

/** 紧凑分段筛选:窄屏自动换行,不出现横向滚动。 */
export function Segmented<T extends string>({
  options,
  value,
  onChange,
  label,
}: {
  options: SegmentOption<NoInfer<T>>[];
  value: T;
  onChange: (value: T) => void;
  label: string;
}) {
  return (
    <div className="inline-flex flex-wrap max-w-full rounded-[10px] bg-muted p-[3px]" role="group" aria-label={label}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            "inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md px-3 h-8 text-[13px] transition-colors",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            value === o.value ? "bg-card text-primary shadow-sm font-medium" : "text-muted-foreground hover:text-foreground",
          )}
        >
          <span>{o.label}</span>
          {o.count !== undefined && <span className="tabular-nums text-[12px] opacity-70">{o.count}</span>}
        </button>
      ))}
    </div>
  );
}

/** 列表搜索框 */
export function SearchBox({
  value,
  onChange,
  placeholder,
  className,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  className?: string;
}) {
  return (
    <div className={cn("relative", className)}>
      <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" aria-hidden />
      <Input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        aria-label={placeholder}
        className="h-8 pl-8 text-[13px]"
      />
    </div>
  );
}

/** 卡片顶部工具条:左侧筛选,右侧搜索/附加操作;窄屏上下排列。 */
export function ListToolbar({ children, end }: { children?: ReactNode; end?: ReactNode }) {
  return (
    <div className="flex flex-col sm:flex-row sm:items-center gap-2 px-4 py-3 border-b border-border">
      <div className="min-w-0 flex flex-wrap items-center gap-2">{children}</div>
      {end && <div className="sm:ml-auto flex items-center gap-2 min-w-0">{end}</div>}
    </div>
  );
}
