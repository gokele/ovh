import { Link, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { BarChart3, Bell, ClipboardList, LayoutGrid, Server } from "lucide-react";
import { useQueueList } from "@/hooks/use-queue";
import { cn } from "@/lib/utils";

const TABS = [
  { to: "/", icon: BarChart3, label: "nav.dashboard" },
  { to: "/servers", icon: Server, label: "nav.servers" },
  { to: "/queue", icon: ClipboardList, label: "nav.queue" },
  { to: "/monitor", icon: Bell, label: "nav.serverMonitor" },
] as const;

/**
 * 手机底部标签栏:抢购最常用的四页常驻拇指区,其余页面经「更多」打开侧栏抽屉。
 * 队列标签显示运行中任务数;底部留出 iOS 安全区。
 */
export function MobileTabBar({ onMore, moreActive }: { onMore: () => void; moreActive: boolean }) {
  const { t } = useTranslation();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const { data: queue } = useQueueList();
  const running = queue?.filter((q) => q.status === "running").length ?? 0;
  const primary = TABS.some((tab) => (tab.to === "/" ? pathname === "/" : pathname.startsWith(tab.to)));

  return (
    <nav className="m-tabbar" aria-label={t("shell.tabbar")}>
      {TABS.map(({ to, icon: Icon, label }) => {
        const active = to === "/" ? pathname === "/" : pathname.startsWith(to);
        return (
          <Link key={to} to={to} className={cn("m-tab", active && "is-active")} aria-current={active ? "page" : undefined}>
            <span className="m-tab-ico">
              <Icon size={20} strokeWidth={active ? 2.2 : 1.8} />
              {to === "/queue" && running > 0 && (
                <span className="m-tab-badge tnum" aria-label={t("shell.runningTasks", { n: running })}>
                  {running > 99 ? "99+" : running}
                </span>
              )}
            </span>
            <span className="m-tab-label">{t(label)}</span>
          </Link>
        );
      })}
      <button type="button" className={cn("m-tab", (moreActive || !primary) && "is-active")} onClick={onMore} aria-haspopup="dialog">
        <span className="m-tab-ico">
          <LayoutGrid size={20} strokeWidth={!primary ? 2.2 : 1.8} />
        </span>
        <span className="m-tab-label">{t("shell.more")}</span>
      </button>
    </nav>
  );
}
