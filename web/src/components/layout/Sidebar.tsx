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
  Github,
  type LucideIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { AccountSwitcher } from "@/components/layout/AccountSwitcher";

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
 * 多语言:文案从 i18n 取 —— NAV_GROUPS 在模块加载时求值一次,语言切换后
 * 由渲染处的 t() 重新读取(这里 title/label 存 key,渲染时翻译)
 */
const NAV_GROUPS: NavGroup[] = [
  {
    title: "nav.group.overview",
    items: [{ to: "/", icon: BarChart3, label: "nav.dashboard" }],
  },
  {
    title: "nav.group.sniping",
    items: [
      { to: "/servers", icon: Server, label: "nav.servers" },
      { to: "/queue", icon: ClipboardList, label: "nav.queue" },
    ],
  },
  {
    title: "nav.group.monitor",
    items: [
      { to: "/monitor", icon: Bell, label: "nav.serverMonitor" },
      { to: "/vps-monitor", icon: Cloud, label: "nav.vpsMonitor" },
    ],
  },
  {
    title: "nav.group.instances",
    items: [
      { to: "/server-control", icon: Terminal, label: "nav.serverControl" },
      { to: "/vps-control", icon: Cloud, label: "nav.vpsControl" },
      { to: "/account", icon: User, label: "nav.account" },
    ],
  },
  {
    title: "nav.group.system",
    items: [
      { to: "/history", icon: Clock, label: "nav.history" },
      { to: "/logs", icon: FileText, label: "nav.logs" },
      { to: "/settings", icon: Settings, label: "nav.settings" },
    ],
  },
];

export { NAV_GROUPS };

/** 导航主体 —— 桌面侧栏和移动抽屉共用。
 *  - onItemClick:移动抽屉里点条目要关抽屉,桌面端不传就 noop。
 */
export function SidebarContent({ onItemClick }: { onItemClick?: () => void }) {
  const { t } = useTranslation();
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
          <div className="text-[15px] font-semibold text-foreground leading-tight truncate">{t("nav.appName")}</div>
        </div>
      </Link>

      {/* 全站唯一的账户切换器。放在导航最上面 —— 它决定了下面每个页面看到的是哪个站点的数据 */}
      <AccountSwitcher onNavigate={onItemClick} />

      {/* Menu groups */}
      <nav className="flex-1 overflow-y-auto py-4 px-3 space-y-5">
        {NAV_GROUPS.map((group) => (
          <div key={group.title}>
            <div className="px-2 mb-1.5 text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">
              {t(group.title)}
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
                    <span className="truncate">{t(item.label)}</span>
                  </Link>
                );
              })}
            </div>
          </div>
        ))}
      </nav>

      {/* Footer:项目仓库入口 + 署名 */}
      <div className="px-3 h-10 flex items-center justify-between gap-2 border-t border-border flex-shrink-0">
        <a
          href="https://github.com/gokele/ovh"
          target="_blank"
          rel="noreferrer noopener"
          className="inline-flex items-center gap-1.5 px-1.5 py-1 rounded text-[11px] text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
          title={t("commons.githubTitle")}
        >
          <Github className="w-3.5 h-3.5" />
          GitHub
        </a>
        <span className="text-[11px] text-muted-foreground">可乐</span>
      </div>
    </>
  );
}

/**
 * 左侧固定导航:lg 及以上显示,固定 256px 宽。
 * 移动 / 平板下用 <MobileMenu> 的抽屉版本。
 *
 * sticky top-0 + h-screen:页面往下滚时导航留在原地。
 * 外层容器是 min-h-screen(不是 h-screen),内容一长整页就会滚,
 * 不加 sticky 的话导航会跟着滚出视野,长列表页要回顶部才能换页面。
 * 菜单本身过长时由内部 nav 的 overflow-y-auto 单独滚。
 */
export function Sidebar() {
  return (
    <aside className="hidden lg:flex w-64 flex-col border-r border-border bg-background flex-shrink-0 sticky top-0 h-screen">
      <SidebarContent />
    </aside>
  );
}
