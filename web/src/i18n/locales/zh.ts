/**
 * 中文语言包(默认语言,文案的唯一事实来源 —— 英文包对照这里翻译)。
 * 按模块前缀组织;`api.*` 命名空间对应后端错误码(lib/api-error.ts),
 * 由 tools/gen-api-codes.py 生成的 api-zh.generated.ts 提供。
 */
import { apiZh } from "./api-zh.generated";
import { snipePagesZh } from "./modules/snipe-pages.zh";
import { settingsPagesZh } from "./modules/settings-pages.zh";
import { ctrlDialogsZh } from "./modules/ctrl-dialogs.zh";
import { watchPagesZh } from "./modules/watch-pages.zh";
import { maintDialogsZh } from "./modules/maint-dialogs.zh";
import { vpsDialogsZh } from "./modules/vps-dialogs.zh";
import { commonLayerZh } from "./modules/common-layer.zh";

export const zh = {
  nav: {
    appName: "OVH 控制台",
    group: {
      overview: "概览",
      sniping: "抢购",
      monitor: "监控",
      instances: "实例",
      system: "系统",
    },
    dashboard: "仪表盘",
    servers: "服务器列表",
    serversShort: "服务器",
    queue: "抢购队列",
    queueShort: "队列",
    serverMonitor: "服务器监控",
    monitorShort: "监控",
    vpsMonitor: "VPS 补货",
    serverControl: "服务器控制",
    vpsControl: "VPS 控制",
    account: "账户管理",
    history: "抢购历史",
    logs: "详细日志",
    settings: "API 设置",
    home: "首页",
    overview: "总览",
  },
  common: {
    refresh: "刷新",
    retry: "重试",
    cancel: "取消",
    confirm: "确认",
    close: "关闭",
    save: "保存",
    delete: "删除",
    edit: "编辑",
    loading: "加载中…",
    empty: "暂无数据",
    readFailed: "读取失败",
    copy: "复制",
    copied: "已复制",
    search: "搜索",
    all: "全部",
    enabled: "已启用",
    disabled: "已停用",
    unknown: "未知",
    language: "语言",
    appearance: "外观",
    checkNetwork: "请检查网络与 API 设置后重试",
    networkError: "网络错误:请求没有到达后端,检查网络后重试",
  },
  lang: {
    switched: "语言:{{name}}",
    current: "当前:{{name}}。点击切换到{{next}}",
    toggle: "切换语言,当前{{name}}",
  },
  // 后端错误码 → 中文(由脚本生成,与 Go 端 message 默认值一致)
  api: apiZh,
  ...snipePagesZh,
  ...settingsPagesZh,
  ...ctrlDialogsZh,
  ...watchPagesZh,
  ...maintDialogsZh,
  ...vpsDialogsZh,
  ...commonLayerZh,
};
