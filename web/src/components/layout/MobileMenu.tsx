import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { Menu, X } from "lucide-react";
import { useRouterState } from "@tanstack/react-router";
import { SidebarContent } from "./Sidebar";

/**
 * 移动 / 平板抽屉式菜单(lg 以下显示)。
 *
 * - 汉堡按钮悬在 TopBar 同高度,点击展开左侧滑入的抽屉
 * - 抽屉内复用 SidebarContent,点击任一菜单项自动关抽屉(onItemClick)
 * - 路由变化时也关抽屉(保险)
 * - Esc 关闭 + 半透明 backdrop 点击关闭
 * - 打开时锁住 body 滚动
 */
export function MobileMenu() {
  const [open, setOpen] = useState(false);
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  // 路由变化自动关
  useEffect(() => {
    setOpen(false);
  }, [pathname]);

  // 锁 body 滚动 + Esc 关闭
  useEffect(() => {
    if (!open) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => {
      document.body.style.overflow = prev;
      window.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <>
      {/* 汉堡按钮:固定左上,只在 lg 以下显示。z-index 高于 TopBar 但低于抽屉 */}
      <button
        type="button"
        aria-label="打开菜单"
        onClick={() => setOpen(true)}
        className="lg:hidden inline-flex items-center justify-center w-9 h-9 rounded-md hover:bg-muted transition-colors flex-shrink-0"
      >
        <Menu className="w-5 h-5" />
      </button>

      {/* 抽屉 + backdrop 通过 portal 直接挂到 <body>。
          原因:TopBar 用了 backdrop-blur-sm,产生 CSS containing block,
          fixed 子元素会被锚到 TopBar 而不是 viewport,导致抽屉变成 56px 高、backdrop 也只罩 TopBar 那条。
          createPortal 让抽屉 / backdrop 跳出 TopBar 的 stacking 链,直接相对 viewport 定位。 */}
      {open && typeof document !== "undefined" && createPortal(
        <div className="lg:hidden">
          <div
            className="fixed inset-0 z-[60] bg-black/50 backdrop-blur-sm animate-in fade-in-0 duration-150"
            onClick={() => setOpen(false)}
          />
          <aside className="fixed inset-y-0 left-0 z-[70] w-64 max-w-[85vw] bg-background border-r border-border flex flex-col animate-in slide-in-from-left duration-200 shadow-2xl">
            <button
              type="button"
              aria-label="关闭菜单"
              onClick={() => setOpen(false)}
              className="absolute top-3 right-3 inline-flex items-center justify-center w-8 h-8 rounded-md hover:bg-muted transition-colors z-10"
            >
              <X className="w-4 h-4" />
            </button>
            <SidebarContent onItemClick={() => setOpen(false)} />
          </aside>
        </div>,
        document.body
      )}
    </>
  );
}
