import { createRootRoute, Outlet } from "@tanstack/react-router";
import { Sidebar } from "@/components/layout/Sidebar";
import { TopBar } from "@/components/layout/TopBar";
import { BottomTabs } from "@/components/layout/BottomTabs";
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
 * - lg 以上：左侧 Sidebar 固定 256px
 * - lg 以下：底部标签栏(BottomTabs),4 个主入口 + 「更多」面板 ——
 *   手机单手握持时左上角的汉堡是拇指最难够到的位置,导航因此挪到底部
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
              {/* 底部内边距要同时让出标签栏(52px)和 iPhone 的 home 指示条,
                  否则页面最后一行内容会被压在标签栏底下读不到 */}
              <main // 只设 pt,不用 py —— py-3 会生成 padding-bottom,跟 .pb-tabbar 同为单类选择器,
              // 谁生效取决于 Tailwind 的输出顺序,结果是底部内容被标签栏盖住。
              className="flex-1 px-3 sm:px-6 lg:px-10 pt-3 sm:pt-8 overflow-y-auto pb-tabbar">
                <div className="max-w-7xl mx-auto">
                  <Outlet />
                </div>
              </main>
            </div>
            <BottomTabs />
            <CommandPalette />
          </div>
        </OvhCredsGate>
      </AuthGate>
    </TooltipProvider>
  ),
});
