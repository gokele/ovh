import { defineConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";
import path from "path";

/**
 * Vite 配置：
 * - dev server 监听 19997，开发期把 /api 反代到 Go backend (19998)
 * - TanStack Router 文件路由插件自动生成 routeTree.gen.ts
 * - @ 路径别名指向 src
 */
export default defineConfig({
  server: {
    port: 19997,
    host: true,
    proxy: {
      "/api": {
        target: "http://localhost:19998",
        changeOrigin: true,
      },
    },
  },
  build: {
    // 直接输出到 server/web，配合 server/webembed_ui.go 的 //go:embed
    // 让 `go build -tags ui` 自动把整个前端打进二进制
    outDir: path.resolve(__dirname, "../server/web"),
    emptyOutDir: true,
  },
  plugins: [
    TanStackRouterVite({
      routesDirectory: "./src/routes",
      generatedRouteTree: "./src/routeTree.gen.ts",
      autoCodeSplitting: true,
    }),
    react(),
  ],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
});
