import { useEffect, useState } from "react";
import { Command } from "cmdk";
import { useNavigate } from "@tanstack/react-router";
import {
  BarChart3,
  Server,
  ClipboardList,
  Bell,
  Cloud,
  Terminal,
  User,
  Clock,
  FileText,
  Settings,
  Search,
} from "lucide-react";
import { useTranslation } from "react-i18next";

interface NavEntry {
  to: string;
  /** i18n key:页面名(复用主语言包 nav.*) */
  labelKey: string;
  /** i18n key:分组名(复用主语言包 nav.group.*) */
  groupKey: string;
  icon: React.ComponentType<{ className?: string }>;
  shortcut?: string;
}

const NAV_ENTRIES: NavEntry[] = [
  { to: "/", labelKey: "nav.dashboard", groupKey: "nav.group.overview", icon: BarChart3, shortcut: "G D" },
  { to: "/servers", labelKey: "nav.servers", groupKey: "nav.group.sniping", icon: Server, shortcut: "G S" },
  { to: "/queue", labelKey: "nav.queue", groupKey: "nav.group.sniping", icon: ClipboardList, shortcut: "G Q" },
  { to: "/monitor", labelKey: "nav.serverMonitor", groupKey: "nav.group.monitor", icon: Bell, shortcut: "G M" },
  { to: "/vps-monitor", labelKey: "nav.vpsMonitor", groupKey: "nav.group.monitor", icon: Cloud, shortcut: "G V" },
  { to: "/server-control", labelKey: "nav.serverControl", groupKey: "nav.group.instances", icon: Terminal, shortcut: "G C" },
  { to: "/account", labelKey: "nav.account", groupKey: "nav.group.instances", icon: User },
  { to: "/history", labelKey: "nav.history", groupKey: "nav.group.system", icon: Clock, shortcut: "G H" },
  { to: "/logs", labelKey: "nav.logs", groupKey: "nav.group.system", icon: FileText, shortcut: "G L" },
  { to: "/settings", labelKey: "nav.settings", groupKey: "nav.group.system", icon: Settings },
];

/**
 * ⌘K 命令面板（cmdk）：
 * - ⌘K / Ctrl+K 唤出
 * - 模糊搜索 11 个页面，回车跳转
 * - 全局快捷键 G + 字母 跳页面
 */
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();
  const { t } = useTranslation();

  useEffect(() => {
    const isMac = navigator.platform.toLowerCase().includes("mac");
    const handler = (e: KeyboardEvent) => {
      // ⌘K / Ctrl+K
      if (e.key === "k" && (isMac ? e.metaKey : e.ctrlKey)) {
        e.preventDefault();
        setOpen((v) => !v);
      }
      // ESC 关闭
      if (e.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  // G + 字母 全局跳转（无修饰键）
  useEffect(() => {
    let waiting = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const handler = (e: KeyboardEvent) => {
      // 仅在不在输入框时响应
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || (e.target as HTMLElement)?.isContentEditable) return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;

      if (!waiting && e.key.toLowerCase() === "g") {
        waiting = true;
        timer && clearTimeout(timer);
        timer = setTimeout(() => (waiting = false), 1200);
        return;
      }
      if (waiting) {
        const map: Record<string, string> = {
          d: "/", s: "/servers", q: "/queue",
          m: "/monitor", v: "/vps-monitor", c: "/server-control",
          h: "/history", l: "/logs",
        };
        const target = map[e.key.toLowerCase()];
        if (target) {
          e.preventDefault();
          navigate({ to: target });
        }
        waiting = false;
        timer && clearTimeout(timer);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [navigate]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 bg-black/40 flex items-start justify-center pt-[12vh] px-4" onClick={() => setOpen(false)}>
      <Command
        className="w-full max-w-xl rounded-2xl bg-background border border-border overflow-hidden"
        onClick={(e) => e.stopPropagation()}
        loop
      >
        <div className="flex items-center gap-2 px-4 border-b border-border">
          <Search className="w-4 h-4 text-muted-foreground" />
          <Command.Input
            autoFocus
            placeholder={t("commons.palette.searchPlaceholder")}
            className="flex-1 h-12 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
          <kbd className="px-1.5 py-0.5 rounded bg-muted text-[10px] font-mono text-muted-foreground">ESC</kbd>
        </div>
        <Command.List className="max-h-[60vh] overflow-y-auto p-2">
          <Command.Empty className="py-8 text-center text-sm text-muted-foreground">{t("commons.palette.noResults")}</Command.Empty>
          {Object.entries(
            NAV_ENTRIES.reduce<Record<string, NavEntry[]>>((acc, e) => {
              (acc[e.groupKey] = acc[e.groupKey] || []).push(e);
              return acc;
            }, {})
          ).map(([groupKey, entries]) => (
            <Command.Group key={groupKey} heading={t(groupKey)} className="text-[11px] text-muted-foreground [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5">
              {entries.map((entry) => {
                const Icon = entry.icon;
                return (
                  <Command.Item
                    key={entry.to}
                    value={`${t(entry.labelKey)} ${entry.to}`}
                    onSelect={() => {
                      navigate({ to: entry.to });
                      setOpen(false);
                    }}
                    className="flex items-center gap-2 px-2 py-2 rounded-md text-sm text-foreground cursor-pointer data-[selected=true]:bg-muted"
                  >
                    <Icon className="w-4 h-4 text-muted-foreground" />
                    <span className="flex-1">{t(entry.labelKey)}</span>
                    {entry.shortcut && (
                      <span className="text-[10px] text-muted-foreground font-mono">{entry.shortcut}</span>
                    )}
                  </Command.Item>
                );
              })}
            </Command.Group>
          ))}
        </Command.List>
      </Command>
    </div>
  );
}
