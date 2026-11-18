import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { routeTree } from "./routeTree.gen";
import { queryClient } from "@/lib/query";
import { ThemedToaster } from "@/components/common/ThemedToaster";
import "@/i18n";
import "@/lib/skin";
import "@/styles/globals.css";
// 珍珠玻璃设计体系(来自 scaffold-master):层序 = tokens → base → materials → components → instruments → layout → pages + 桥接/定制
import "@/styles/pearl-bundle.css";
// 收敛层:压低装饰(渐变、光晕、大圆角),必须在 pearl-bundle 之后加载
import "@/styles/calm.css";

/**
 * 应用入口：
 * - 装配 TanStack Router（文件路由产物）
 * - 全局 QueryClient 提供商
 * - Sonner toast 容器
 */
const router = createRouter({
  routeTree,
  defaultPreload: "intent",
  defaultPreloadStaleTime: 0,
  scrollRestoration: true,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
      <ThemedToaster />
    </QueryClientProvider>
  </StrictMode>
);
