import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { Link, useRouterState } from "@tanstack/react-router";
import {
  BarChart3,
  Bell,
  ClipboardList,
  MoreHorizontal,
  Server,
  X,
  type LucideIcon,
} from "lucide-react";
import { NAV_GROUPS } from "./Sidebar";
import { cn } from "@/lib/utils";

interface Tab {
  to: string;
  icon: LucideIcon;
  label: string;
}

/**
 * 底部标签栏里的四个主入口。
 *
 * 挑选依据是抢购这条主线的实际动线:看状态(仪表盘) → 找机器(服务器列表) →
 * 盯库存(监控) → 看任务跑得怎么样(队列)。剩下 7 个页面(控制台、账户、历史、
 * 日志、设置、VPS 两页)都是低频或一次性配置,收进「更多」。
 *
 * 为什么不是汉堡菜单:390×844 的手机单手握持时,左上角是拇指最难够到的位置,
 * 而那里恰好是汉堡按钮。底部这条带正好落在拇指自然扫过的弧线上。
 */
const TABS: Tab[] = [
  { to: "/", icon: BarChart3, label: "仪表盘" },
  { to: "/servers", icon: Server, label: "服务器" },
  { to: "/queue", icon: ClipboardList, label: "队列" },
  { to: "/monitor", icon: Bell, label: "监控" },
];

/** TABS 里已有的路由，「更多」面板要把它们排除掉 */
const TAB_PATHS = new Set(TABS.map((t) => t.to));

/**
 * 手机端底部标签栏（lg 以下显示）。
 *
 * 高度 52px + 底部安全区 —— iPhone 的 home indicator 区域必须让出来,
 * 否则最后一个标签的点击区会被系统手势条吃掉一半。
 */
export function BottomTabs() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const [moreOpen, setMoreOpen] = useState(false);

  // 路由一变就关面板：点完条目应该看到页面，而不是还挡着一层
  useEffect(() => {
    setMoreOpen(false);
  }, [pathname]);

  const isActive = (to: string) => (to === "/" ? pathname === "/" : pathname.startsWith(to));
  // 当前页不在四个主标签里 → 高亮「更多」，让用户知道自己在哪一层
  const inMore = !TAB_PATHS.has(pathname) && ![...TAB_PATHS].some((p) => p !== "/" && pathname.startsWith(p));

  return (
    <>
      <nav
        className="lg:hidden fixed bottom-0 inset-x-0 z-40 bg-background/95 backdrop-blur-sm border-t border-border"
        style={{ paddingBottom: "env(safe-area-inset-bottom)" }}
        aria-label="主导航"
      >
        <div className="grid grid-cols-5">
          {TABS.map((t) => (
            <TabButton key={t.to} to={t.to} icon={t.icon} label={t.label} active={isActive(t.to)} />
          ))}
          <button
            type="button"
            onClick={() => setMoreOpen(true)}
            aria-label="更多页面"
            aria-expanded={moreOpen}
            className={cn(
              // min-h-[52px] 保证点击区不低于 44px 的可达性下限
              "flex flex-col items-center justify-center gap-0.5 min-h-[52px] transition-colors",
              inMore || moreOpen ? "text-foreground" : "text-muted-foreground"
            )}
          >
            <MoreHorizontal className="w-[22px] h-[22px]" strokeWidth={inMore ? 2.2 : 1.8} />
            <span className={cn("text-[10px] leading-none", inMore && "font-semibold")}>更多</span>
          </button>
        </div>
      </nav>

      {moreOpen && <MoreSheet onClose={() => setMoreOpen(false)} />}
    </>
  );
}

/** 单个标签。active 用字重 + 描边粗细区分，不只靠颜色（色觉差异用户也要分得出） */
function TabButton({
  to,
  icon: Icon,
  label,
  active,
}: Tab & { active: boolean }) {
  return (
    <Link
      to={to}
      className={cn(
        "flex flex-col items-center justify-center gap-0.5 min-h-[52px] transition-colors",
        active ? "text-foreground" : "text-muted-foreground"
      )}
      aria-current={active ? "page" : undefined}
    >
      <Icon className="w-[22px] h-[22px]" strokeWidth={active ? 2.2 : 1.8} />
      <span className={cn("text-[10px] leading-none", active && "font-semibold")}>{label}</span>
    </Link>
  );
}

/**
 * 「更多」底部面板：列出不在标签栏里的页面。
 *
 * 从底部升起而不是从左侧滑入 —— 触发点在底部,面板也从底部来,
 * 手指不用跨过整个屏幕去够刚打开的东西。
 */
function MoreSheet({ onClose }: { onClose: () => void }) {
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  // 打开时锁 body 滚动 + Esc 关闭
  useEffect(() => {
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      document.body.style.overflow = prev;
      window.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  if (typeof document === "undefined") return null;

  return createPortal(
    <div className="lg:hidden">
      <div
        className="fixed inset-0 z-[60] bg-black/50 animate-in fade-in-0 duration-150"
        onClick={onClose}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-label="更多页面"
        className="fixed inset-x-0 bottom-0 z-[61] max-h-[80vh] overflow-y-auto rounded-t-2xl bg-background border-t border-border animate-in slide-in-from-bottom duration-200"
        style={{ paddingBottom: "calc(env(safe-area-inset-bottom) + 12px)" }}
      >
        {/* 抓手条：告诉用户这是个可以从底部拉动的面板 */}
        <div className="sticky top-0 bg-background pt-2.5 pb-1">
          <div className="mx-auto w-9 h-1 rounded-full bg-border" />
          <div className="flex items-center justify-between px-4 pt-2">
            <span className="text-[15px] font-semibold">更多</span>
            <button
              type="button"
              onClick={onClose}
              aria-label="关闭"
              className="inline-flex items-center justify-center w-11 h-11 -mr-2 rounded-full text-muted-foreground hover:bg-muted"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        <div className="px-3 pb-2">
          {NAV_GROUPS.map((g) => {
            const items = g.items.filter((i) => !TAB_PATHS.has(i.to));
            if (items.length === 0) return null;
            return (
              <div key={g.title} className="mb-1.5">
                <div className="px-2 pt-2.5 pb-1 text-[11px] font-semibold tracking-wide text-muted-foreground">
                  {g.title}
                </div>
                {items.map(({ to, icon: Icon, label }) => {
                  const active = pathname.startsWith(to);
                  return (
                    <Link
                      key={to}
                      to={to}
                      onClick={onClose}
                      className={cn(
                        // min-h-[48px]：面板里的条目是主要点击目标，给足高度
                        "flex items-center gap-3 px-2 min-h-[48px] rounded-xl transition-colors",
                        active ? "bg-secondary font-semibold" : "hover:bg-muted"
                      )}
                      aria-current={active ? "page" : undefined}
                    >
                      <Icon className="w-[18px] h-[18px] flex-shrink-0" strokeWidth={1.9} />
                      <span className="text-[14px]">{label}</span>
                    </Link>
                  );
                })}
              </div>
            );
          })}
        </div>
      </div>
    </div>,
    document.body
  );
}
