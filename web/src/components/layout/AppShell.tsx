import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { PanelLeftClose, PanelLeftOpen, Menu, Shield, Github } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { MobileTabBar } from "./MobileTabBar";
import { NAV_GROUPS } from "@/components/layout/nav";
import { AccountSwitcher } from "@/components/layout/AccountSwitcher";
import { ThemeToggle } from "@/components/layout/ThemeToggle";
import { LanguageToggle } from "@/components/layout/LanguageToggle";
import { useAppVersion } from "@/hooks/use-system-metrics";

/**
 * 珍珠玻璃外壳(来自 scaffold-master 的布局体系):
 *
 *   .stage → 8px 舞台留白
 *   .app-frame → 10px 深色边框 + 38px 圆角的"设备框",画布上两团静态漫射光
 *     ├─ .rail   232px 导航轨(可收 76px):品牌 + 账户切换 + 分组导航 + 滑块高亮
 *     └─ .workspace
 *          ├─ .topbar  64px:面包屑 + 全局控制;内容滚动后"凝结"出玻璃面(只切 opacity)
 *          └─ .content 滚动宿主 > .page 页面
 *
 * 820px 以下导航轨变成抽屉(遮罩 + 位移过渡),替代旧的底部标签栏 ——
 * 单手可达性由"顶栏汉堡常驻"承担。导航滑块(nav-thumb)是一块玻璃材质
 * 在导航项之间滑动,图标文字不动;只用 transform 过渡,不用 GSAP。
 */

const RAIL_COLLAPSED_KEY = "ovh_rail_collapsed";

function useIsMobile() {
  const [mobile, setMobile] = useState(() => window.matchMedia("(max-width: 820px)").matches);
  useEffect(() => {
    const mq = window.matchMedia("(max-width: 820px)");
    const sync = () => setMobile(mq.matches);
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  }, []);
  return mobile;
}

function useBreadcrumb() {
  const { t } = useTranslation();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const group = NAV_GROUPS.find((g) =>
    g.items.some((it) => (it.to === "/" ? pathname === "/" : pathname.startsWith(it.to))),
  );
  const current = group?.items.find((it) =>
    it.to === "/" ? pathname === "/" : pathname.startsWith(it.to),
  );
  return {
    groupLabel: group ? t(group.title) : "",
    label: current ? t(current.label) : "",
  };
}

export function AppShell() {
  const { t } = useTranslation();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const mobile = useIsMobile();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(RAIL_COLLAPSED_KEY) === "1");
  const [scrolled, setScrolled] = useState(false);
  const navRef = useRef<HTMLElement | null>(null);
  const thumbRef = useRef<HTMLDivElement | null>(null);
  const contentRef = useRef<HTMLDivElement | null>(null);
  const { data: version } = useAppVersion();

  const isActive = (to: string) => (to === "/" ? pathname === "/" : pathname.startsWith(to));

  // 切页/收起/换语言后重测滑块位置 —— 只动 transform,过渡交给 CSS。
  // 用 rect 相对 rail-nav 计算:offsetTop 的参照系是最近的 positioned 祖先,
  // nav-list 也是 relative,层级一多就读出 0;rect 差值永远是对的。
  const syncThumb = () => {
    const nav = navRef.current;
    const thumb = thumbRef.current;
    if (!nav || !thumb) return;
    const el = nav.querySelector<HTMLElement>(".nav-item.is-active");
    if (!el) {
      thumb.style.opacity = "0";
      return;
    }
    const top = el.getBoundingClientRect().top - nav.getBoundingClientRect().top + nav.scrollTop;
    thumb.style.opacity = "1";
    thumb.style.height = `${el.offsetHeight}px`;
    thumb.style.transform = `translateY(${top}px)`;
  };
  useLayoutEffect(syncThumb, [pathname, collapsed, mobileOpen]);

  // 字体加载/窗口尺寸/滚动宿主变化都会让 rect 漂移,统一交给 ResizeObserver +
  // 字体就绪后补一次(系统字体加载完成前 rect 是错的,这是首帧滑块贴顶的根因)
  useEffect(() => {
    const nav = navRef.current;
    if (!nav) return;
    const ro = new ResizeObserver(syncThumb);
    ro.observe(nav);
    document.fonts?.ready.then(syncThumb).catch(() => {});
    return () => ro.disconnect();
  }, []);

  // 路由切换顺手关抽屉
  useEffect(() => setMobileOpen(false), [pathname]);

  const toggleCollapse = () => {
    if (mobile) {
      setMobileOpen((v) => !v);
      return;
    }
    setCollapsed((v) => {
      localStorage.setItem(RAIL_COLLAPSED_KEY, v ? "0" : "1");
      return !v;
    });
  };

  const crumbs = useBreadcrumb();

  return (
    <div className="stage">
      <div
        className={cn(
          "app-frame is-entrance-ready",
          collapsed && !mobile && "is-collapsed",
          mobile && mobileOpen && "is-mobile-open",
        )}
      >
        <aside className="rail m-rail">
          <Link to="/" className="rail-brand" aria-label={t("nav.appName")}>
            <span className="brand-mark">
              <Shield className="w-4 h-4" strokeWidth={2} />
              <span className="brand-dot" />
            </span>
            <span className="brand-text">
              <span className="brand-name">{t("nav.appName")}</span>
            </span>
          </Link>

          {/* 账户切换在导航最上面:它决定下面每页看到的是哪个站点的数据 */}
          <div className="rail-account px-3 flex-none">
            <AccountSwitcher iconOnly={collapsed && !mobile} onNavigate={() => setMobileOpen(false)} />
          </div>

          <nav className="rail-nav" ref={navRef}>
            <div className="nav-thumb" ref={thumbRef} aria-hidden="true" />
            {NAV_GROUPS.map((group) => (
              <div className="nav-group" key={group.title}>
                <div className="nav-group-title">{t(group.title)}</div>
                <div className="nav-list">
                  {group.items.map((item) => {
                    const Icon = item.icon;
                    return (
                      <Link
                        key={item.to}
                        to={item.to}
                        className={cn("nav-item", isActive(item.to) && "is-active")}
                      >
                        <Icon className="nav-ico" size={16} strokeWidth={1.9} />
                        <span className="nav-label">{t(item.label)}</span>
                      </Link>
                    );
                  })}
                </div>
              </div>
            ))}
          </nav>

          <div className="rail-foot">
            {/* 收起态也保留版本与 GitHub 图标(常显,不依赖 label) */}
            <button
              type="button"
              className="nav-item"
              onClick={toggleCollapse}
              aria-label={collapsed ? t("shell.expandRail") : t("shell.collapseRail")}
              title={collapsed ? t("shell.expandRail") : t("shell.collapseRail")}
            >
              {collapsed && !mobile ? (
                <PanelLeftOpen className="nav-ico" size={16} strokeWidth={1.9} />
              ) : (
                <PanelLeftClose className="nav-ico" size={16} strokeWidth={1.9} />
              )}
              <span className="nav-label">{collapsed && !mobile ? "" : t("shell.collapseRail")}</span>
            </button>
            {/* 冠名:GitHub 仓库 + 作者(可乐/cola)。收起态 GitHub 图标仍在,title 提示仓库地址 */}
            <a
              href="https://github.com/gokele/ovh"
              target="_blank"
              rel="noreferrer"
              className="nav-item"
              title="github.com/gokele/ovh"
            >
              <Github className="nav-ico" size={16} strokeWidth={1.9} />
              <span className="nav-label">{t("shell.cola")}</span>
              {!collapsed && version && (
                <span
                  className="tnum"
                  style={{ marginLeft: "auto", fontSize: 11, color: "var(--ink-faint)", fontWeight: 500 }}
                >
                  v{version}
                </span>
              )}
            </a>
            {collapsed && !mobile && version && (
              <div
                className="tnum"
                style={{ textAlign: "center", fontSize: 10.5, color: "var(--ink-faint)", padding: "2px 0 4px" }}
              >
                v{version}
              </div>
            )}
          </div>
        </aside>

        {mobile && mobileOpen && (
          <button
            type="button"
            aria-label={t("shell.closeMenu")}
            className="rail-scrim"
            onClick={() => setMobileOpen(false)}
          />
        )}

        <div className={cn("workspace", scrolled && "is-scrolled")}>
          <header className="topbar">
            {mobile && (
              <button
                type="button"
                className="icon-btn"
                onClick={() => setMobileOpen(true)}
                aria-label={t("shell.openMenu")}
              >
                <Menu size={17} strokeWidth={1.9} />
              </button>
            )}
            {mobile && <span className="m-title">{crumbs.label || t("nav.appName")}</span>}
            <nav className="crumbs" aria-label="breadcrumb">
              <span>{t("nav.appName")}</span>
              {crumbs.groupLabel && (
                <>
                  <span className="crumb-sep">/</span>
                  <span>{crumbs.groupLabel}</span>
                </>
              )}
              {crumbs.label && (
                <>
                  <span className="crumb-sep">/</span>
                  <span className="crumb-current">{crumbs.label}</span>
                </>
              )}
            </nav>
            <span className="spacer" />
            <LanguageToggle />
            <ThemeToggle />
          </header>

          <div
            className="content"
            ref={contentRef}
            onScroll={(e) => {
              const next = (e.target as HTMLDivElement).scrollTop > 8;
              if (next !== scrolled) setScrolled(next);
            }}
          >
            {/* 滚动宿主在 content 上:页面只管自己的节奏(.page 内边距由 layout.css 管) */}
            <div className="page is-entrance-ready">
              <Outlet />
            </div>
          </div>
          {mobile && <MobileTabBar onMore={() => setMobileOpen(true)} moreActive={mobileOpen} />}
        </div>
      </div>
    </div>
  );
}
