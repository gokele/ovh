import { Link, useRouterState } from "@tanstack/react-router";
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
  Shield,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";

interface NavItem {
  to: string;
  icon: LucideIcon;
  label: string;
}

interface NavGroup {
  title: string;
  items: NavItem[];
}

/**
 * 侧栏导航分组：概览 / 抢购 / 监控 / 实例 / 系统
 * VPS 控制台风格，每组之间留呼吸空间，active 用浅灰底 + 黑色左 2px border
 */
const NAV_GROUPS: NavGroup[] = [
  {
    title: "概览",
    items: [{ to: "/", icon: BarChart3, label: "仪表盘" }],
  },
  {
    title: "抢购",
    items: [
      { to: "/servers", icon: Server, label: "服务器列表" },
      { to: "/queue", icon: ClipboardList, label: "抢购队列" },
    ],
  },
  {
    title: "监控",
    items: [
      { to: "/monitor", icon: Bell, label: "服务器监控" },
      { to: "/vps-monitor", icon: Cloud, label: "VPS 补货" },
    ],
  },
  {
    title: "实例",
    items: [
      { to: "/server-control", icon: Terminal, label: "服务器控制" },
      { to: "/account", icon: User, label: "账户管理" },
    ],
  },
  {
    title: "系统",
    items: [
      { to: "/history", icon: Clock, label: "抢购历史" },
      { to: "/logs", icon: FileText, label: "详细日志" },
      { to: "/settings", icon: Settings, label: "API 设置" },
    ],
  },
];

export { NAV_GROUPS };

/** 导航主体 —— 桌面侧栏和移动抽屉共用。
 *  - onItemClick:移动抽屉里点条目要关抽屉,桌面端不传就 noop。
 */
export function SidebarContent({ onItemClick }: { onItemClick?: () => void }) {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const isActive = (to: string) => {
    if (to === "/") return pathname === "/";
    return pathname.startsWith(to);
  };
  return (
    <>
      {/* Logo header */}
      <Link
        to="/"
        onClick={onItemClick}
        className="flex items-center gap-3 px-4 h-16 border-b border-border hover:bg-muted transition-colors flex-shrink-0"
      >
        <div className="flex items-center justify-center w-8 h-8 rounded-lg bg-primary text-primary-foreground">
          <Shield className="w-4 h-4" />
        </div>
        <div className="min-w-0">
          <div className="text-[15px] font-semibold text-foreground leading-tight truncate">OVH 控制台</div>
        </div>
      </Link>

      {/* Menu groups */}
      <nav className="flex-1 overflow-y-auto py-4 px-3 space-y-5">
        {NAV_GROUPS.map((group) => (
          <div key={group.title}>
            <div className="px-2 mb-1.5 text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">
              {group.title}
            </div>
            <div className="space-y-0.5">
              {group.items.map((item) => {
                const active = isActive(item.to);
                const Icon = item.icon;
                return (
                  <Link
                    key={item.to}
                    to={item.to}
                    onClick={onItemClick}
                    className={cn(
                      "group relative flex items-center gap-2.5 px-2.5 py-1.5 rounded-md text-[14px] transition-colors border-l-2",
                      active
                        ? "bg-secondary text-foreground font-medium border-l-foreground"
                        : "text-foreground/80 hover:bg-muted hover:text-foreground border-l-transparent"
                    )}
                  >
                    <Icon
                      className={cn(
                        "w-4 h-4 flex-shrink-0",
                        active ? "text-foreground" : "text-muted-foreground group-hover:text-foreground"
                      )}
                      strokeWidth={active ? 2.25 : 1.75}
                    />
                    <span className="truncate">{item.label}</span>
                  </Link>
                );
              })}
            </div>
          </div>
        ))}
      </nav>

      {/* Footer */}
      <div className="px-4 h-10 flex items-center justify-end border-t border-border flex-shrink-0">
        <span className="text-[11px] text-muted-foreground">可乐</span>
      </div>
    </>
  );
}

/**
 * 左侧固定导航:lg 及以上显示,固定 256px 宽。
 * 移动 / 平板下用 <MobileMenu> 的抽屉版本。
 */
export function Sidebar() {
  return (
    <aside className="hidden lg:flex w-64 flex-col border-r border-border bg-background flex-shrink-0">
      <SidebarContent />
    </aside>
  );
}
