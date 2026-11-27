import {
  BarChart3, Server, ClipboardList, Bell, Cloud, Terminal, User, Clock, FileText, Settings, PackageCheck,
  type LucideIcon,
} from "lucide-react";

/** 导航分组(原经典侧栏文件迁出):概览 / 抢购 / 监控 / 实例 / 系统 */
interface NavItem {
  to: string;
  icon: LucideIcon;
  label: string;
}

interface NavGroup {
  title: string;
  items: NavItem[];
}

/**
 * 侧栏导航分组：概览 / 抢购 / 监控 / 实例 / 系统
 * VPS 控制台风格，每组之间留呼吸空间，active 用浅灰底 + 黑色左 2px border
 * 多语言:文案从 i18n 取 —— NAV_GROUPS 在模块加载时求值一次,语言切换后
 * 由渲染处的 t() 重新读取(这里 title/label 存 key,渲染时翻译)
 */
const NAV_GROUPS: NavGroup[] = [
  {
    title: "nav.group.overview",
    items: [{ to: "/", icon: BarChart3, label: "nav.dashboard" }],
  },
  {
    title: "nav.group.sniping",
    items: [
      { to: "/servers", icon: Server, label: "nav.servers" },
      { to: "/queue", icon: ClipboardList, label: "nav.queue" },
      // 抢购历史属于抢购流程(下单结果、付款),和队列放在一起
      { to: "/history", icon: Clock, label: "nav.history" },
      { to: "/orders", icon: PackageCheck, label: "nav.orders" },
    ],
  },
  {
    title: "nav.group.monitor",
    items: [
      { to: "/monitor", icon: Bell, label: "nav.serverMonitor" },
      { to: "/vps-monitor", icon: Cloud, label: "nav.vpsMonitor" },
    ],
  },
  {
    title: "nav.group.instances",
    items: [
      { to: "/server-control", icon: Terminal, label: "nav.serverControl" },
      { to: "/vps-control", icon: Cloud, label: "nav.vpsControl" },
      { to: "/account", icon: User, label: "nav.account" },
    ],
  },
  {
    title: "nav.group.system",
    items: [
      { to: "/logs", icon: FileText, label: "nav.logs" },
      { to: "/settings", icon: Settings, label: "nav.settings" },
    ],
  },
];

export { NAV_GROUPS };