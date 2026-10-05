import { Link, useRouterState } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";
import { MobileMenu } from "./MobileMenu";
import { AccountSwitcher } from "@/components/layout/AccountSwitcher";
import { ThemeToggle } from "@/components/layout/ThemeToggle";
import { LanguageToggle } from "@/components/layout/LanguageToggle";
import { useTranslation } from "react-i18next";

/**
 * 顶部 56px 细 bar：只显示面包屑。⌘K 命令面板入口已移除，
 * 但全局快捷键仍由 CommandPalette 组件挂在 __root 上接管。
 */

const PAGE_META: Record<string, { group: string; label: string }> = {
  "/": { group: "nav.group.overview", label: "nav.dashboard" },
  "/servers": { group: "nav.group.sniping", label: "nav.servers" },
  "/queue": { group: "nav.group.sniping", label: "nav.queue" },
  "/monitor": { group: "nav.group.monitor", label: "nav.serverMonitor" },
  "/vps-monitor": { group: "nav.group.monitor", label: "nav.vpsMonitor" },
  "/server-control": { group: "nav.group.instances", label: "nav.serverControl" },
  "/vps-control": { group: "nav.group.instances", label: "nav.vpsControl" },
  "/account": { group: "nav.group.instances", label: "nav.account" },
  "/history": { group: "nav.group.system", label: "nav.history" },
  "/logs": { group: "nav.group.system", label: "nav.logs" },
  "/settings": { group: "nav.group.system", label: "nav.settings" },
};

export function TopBar() {
  const { t } = useTranslation();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const meta =
    PAGE_META[pathname] ||
    Object.entries(PAGE_META).find(([p]) => p !== "/" && pathname.startsWith(p))?.[1] ||
    { group: "", label: "" };

  return (
    <header className="sticky top-0 z-30 h-12 sm:h-14 flex items-center gap-2 px-3 sm:px-8 bg-background/95 backdrop-blur-sm border-b border-border">
      <MobileMenu />
      {/* 手机端顶栏:左边页名,右边账户 chip。
          页名放这儿之后 PageHeader 里那个标题就是重复的,已经在手机端隐掉。
          账户切换器必须常驻:三区(EU/US/CA)目录互不相通,切错账户后面每一步
          都打在错误的站点上 —— 而它原来只在侧栏里,手机端侧栏隐藏、汉堡又收给了
          平板,不挪上来的话手机上根本切不了。放右边是因为它是全局控制,
          跟"当前在哪一页"不是一类东西。 */}
      <span className="sm:hidden text-[15px] font-semibold text-foreground truncate">
        {t(meta.label)}
      </span>
      <div className="sm:hidden ml-auto flex-shrink-0 max-w-[52%]">
        <AccountSwitcher compact />
      </div>
      {/* 主题/语言快捷键在两种布局下都靠最右:手机排在账户 chip 后,
          桌面排在面包屑后。它们是全局控制,和"当前在哪一页"无关。 */}
      <div className="ml-auto flex-shrink-0 flex items-center">
        <LanguageToggle />
        <ThemeToggle />
      </div>
      <div className="hidden sm:flex items-center gap-2.5 min-w-0">
        <Link to="/" className="text-sm text-muted-foreground hover:text-foreground transition-colors whitespace-nowrap">
          {t("nav.home")}
        </Link>
        {meta.group && (
          <>
            <ChevronRight className="w-3.5 h-3.5 text-muted-foreground/60 flex-shrink-0" />
            <span className="text-sm text-muted-foreground whitespace-nowrap">{t(meta.group)}</span>
          </>
        )}
        {meta.label && (
          <>
            <ChevronRight className="w-3.5 h-3.5 text-muted-foreground/60 flex-shrink-0" />
            <span className="text-sm font-semibold text-foreground truncate">{t(meta.label)}</span>
          </>
        )}
      </div>
    </header>
  );
}
