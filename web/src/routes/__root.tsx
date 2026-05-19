import { createRootRoute, Outlet } from "@tanstack/react-router";
import { Sidebar } from "@/components/layout/Sidebar";
import { TopBar } from "@/components/layout/TopBar";
import { CommandPalette } from "@/components/common/CommandPalette";
import { AuthGate } from "@/components/common/AuthGate";
import { OvhCredsGate } from "@/components/common/OvhCredsGate";
import { TooltipProvider } from "@/components/ui/tooltip";

/**
 * 根路由：所有页面共享的 Layout 容器
 * - 两层 gate（外到内）：
 *   1. AuthGate     —— 验证后端访问密码（X-API-Key）
 *   2. OvhCredsGate —— 强制要求 OVH API 凭据（appKey / appSecret / consumerKey）
 *   两层都过才能见到任何业务路由 / 点击
 * - 左侧 Sidebar 固定 256px（lg 以上）
 * - 右侧主区：sticky TopBar 56px + 滚动 main
 * - 全局 ⌘K 命令面板和 Radix Tooltip Provider
 */
export const Route = createRootRoute({
  component: () => (
    <TooltipProvider delayDuration={300}>
      <AuthGate>
        <OvhCredsGate>
          <div className="min-h-screen flex bg-background text-foreground">
            <Sidebar />
            <div className="flex-1 flex flex-col min-w-0">
              <TopBar />
              <main className="flex-1 px-3 sm:px-6 lg:px-10 py-4 sm:py-8 overflow-y-auto">
                <div className="max-w-7xl mx-auto">
                  <Outlet />
                </div>
              </main>
            </div>
            <CommandPalette />
          </div>
        </OvhCredsGate>
      </AuthGate>
    </TooltipProvider>
  ),
});
