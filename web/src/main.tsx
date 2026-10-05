import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { routeTree } from "./routeTree.gen";
import { queryClient } from "@/lib/query";
import { ThemedToaster } from "@/components/common/ThemedToaster";
import "@/i18n";
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
      <ThemedToaster />
    </QueryClientProvider>
  </StrictMode>
);
