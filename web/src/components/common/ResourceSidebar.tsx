import { useMemo, useState, type ReactNode } from "react";
import { StatusDot } from "@/components/common/StatusDot";
import { SearchBox } from "@/components/common/ListToolbar";
import { cn } from "@/lib/utils";

export interface ResourceItem {
  id: string;
  title: string;
  /** 第二行:型号 · 机房 */
  sub?: string;
  /** 第三行(等宽):IP 或服务名 */
  meta?: string;
  tone: "success" | "warning" | "muted" | "danger";
  badge?: ReactNode;
}

/**
 * 管理后台常见的「左侧资源列表」:宽屏下与详情并排,点选切换。
 * 超过 6 台显示搜索框;右键行可触发 onContextMenu(例如设别名)。
 */
export function ResourceSidebar({
  items,
  selected,
  onSelect,
  onContextMenu,
  label,
  searchPlaceholder,
  emptyText,
}: {
  items: ResourceItem[];
  selected: string | null;
  onSelect: (id: string) => void;
  onContextMenu?: (id: string) => void;
  label: string;
  searchPlaceholder: string;
  emptyText: string;
}) {
  const [query, setQuery] = useState("");
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return items;
    return items.filter((it) => [it.title, it.sub, it.meta, it.id].some((s) => s?.toLowerCase().includes(q)));
  }, [items, query]);

  return (
    <nav aria-label={label} className="flex flex-col min-h-0">
      <div className="flex items-center justify-between gap-2 px-3 py-2.5 border-b border-border">
        <span className="text-[13px] font-medium">{label}</span>
        <span className="text-xs text-muted-foreground tabular-nums">{items.length}</span>
      </div>
      {items.length > 6 && (
        <div className="px-3 py-2 border-b border-border">
          <SearchBox value={query} onChange={setQuery} placeholder={searchPlaceholder} />
        </div>
      )}
      <ul className="overflow-y-auto max-h-[calc(100dvh-220px)] py-1">
        {shown.length === 0 && <li className="px-3 py-6 text-center text-xs text-muted-foreground">{emptyText}</li>}
        {shown.map((it) => {
          const active = it.id === selected;
          return (
            <li key={it.id}>
              <button
                type="button"
                onClick={() => onSelect(it.id)}
                onContextMenu={
                  onContextMenu
                    ? (e) => {
                        e.preventDefault();
                        onContextMenu(it.id);
                      }
                    : undefined
                }
                aria-current={active ? "true" : undefined}
                className={cn(
                  "w-full text-left px-3 py-2 flex gap-2.5 items-start border-l-2 transition-colors",
                  "focus-visible:outline-none focus-visible:bg-muted",
                  active ? "border-l-primary bg-muted/70" : "border-l-transparent hover:bg-muted/50",
                )}
              >
                <StatusDot tone={it.tone} size="xs" className="mt-1.5" />
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-1.5 min-w-0">
                    <span className={cn("truncate text-[13px]", active ? "font-semibold" : "font-medium")}>{it.title}</span>
                    {it.badge}
                  </span>
                  {it.sub && <span className="block truncate text-[11.5px] text-muted-foreground">{it.sub}</span>}
                  {it.meta && <span className="block truncate font-mono text-[11px] text-muted-foreground/80">{it.meta}</span>}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
