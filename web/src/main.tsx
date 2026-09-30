import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "sonner";
import { routeTree } from "./routeTree.gen";
import { queryClient } from "@/lib/query";
import "@/styles/globals.css";

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
      {/* top-center:右上角是账户 chip 和主题切换(可连点),bottom 是手机
          标签栏和对话框操作区 —— 两头都碰不得,top-center 是唯一不遮任何
          可点控件的位置。实测 top-right 时连点主题按钮会被自己的 toast 盖住。 */}
      <Toaster position="top-center" richColors closeButton />
    </QueryClientProvider>
  </StrictMode>
);
