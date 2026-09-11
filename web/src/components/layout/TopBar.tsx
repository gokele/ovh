import { Link, useRouterState } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";
import { MobileMenu } from "./MobileMenu";
import { AccountSwitcher } from "@/components/layout/AccountSwitcher";

/**
 * 顶部 56px 细 bar：只显示面包屑。⌘K 命令面板入口已移除，
 * 但全局快捷键仍由 CommandPalette 组件挂在 __root 上接管。
 */

const PAGE_META: Record<string, { group: string; label: string }> = {
  "/": { group: "概览", label: "仪表盘" },
  "/servers": { group: "抢购", label: "服务器列表" },
  "/queue": { group: "抢购", label: "抢购队列" },
  "/monitor": { group: "监控", label: "服务器监控" },
  "/vps-monitor": { group: "监控", label: "VPS 补货" },
  "/server-control": { group: "实例", label: "服务器控制" },
  "/vps-control": { group: "实例", label: "VPS 控制" },
  "/account": { group: "实例", label: "账户管理" },
  "/history": { group: "系统", label: "抢购历史" },
  "/logs": { group: "系统", label: "详细日志" },
  "/settings": { group: "系统", label: "API 设置" },
};

export function TopBar() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const meta =
    PAGE_META[pathname] ||
    Object.entries(PAGE_META).find(([p]) => p !== "/" && pathname.startsWith(p))?.[1] ||
    { group: "", label: "" };

  return (
    <header className="sticky top-0 z-30 h-12 sm:h-14 flex items-center gap-2 px-3 sm:px-8 bg-background/95 backdrop-blur-sm border-b border-border">
      <MobileMenu />
      {/* 手机端顶栏放账户切换器,不放页名 ——
          页名下面的 PageHeader 已经写了,而账户切换器原来只在侧栏里,
          手机端侧栏隐藏、汉堡又收给了平板,不挪上来的话手机上根本切不了账户。
          三区(EU/US/CA)目录互不相通,切错账户后面每一步都打在错误的站点上,
          所以这个入口在任何尺寸下都必须存在。 */}
      <div className="sm:hidden flex-1 min-w-0">
        <AccountSwitcher compact />
      </div>
      <div className="hidden sm:flex items-center gap-2.5 min-w-0">
        <Link to="/" className="text-sm text-muted-foreground hover:text-foreground transition-colors whitespace-nowrap">
          首页
        </Link>
        {meta.group && (
          <>
            <ChevronRight className="w-3.5 h-3.5 text-muted-foreground/60 flex-shrink-0" />
            <span className="text-sm text-muted-foreground whitespace-nowrap">{meta.group}</span>
          </>
        )}
        {meta.label && (
          <>
            <ChevronRight className="w-3.5 h-3.5 text-muted-foreground/60 flex-shrink-0" />
            <span className="text-sm font-semibold text-foreground truncate">{meta.label}</span>
          </>
        )}
      </div>
    </header>
  );
}
