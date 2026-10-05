/**
 * English language pack. zh.ts is the source of truth — every key here must
 * mirror it (tsc enforces the shape via LangPack below). OVH terminology
 * follows OVHcloud's official English docs: bare metal, datacenter,
 * provisioning, retraction, engagement, mitigation, netboot…
 */
import type { zh } from "./zh";
import type { apiZh } from "./api-zh.generated";
import { apiEn } from "./api-en.translated";
import { snipePagesEn } from "./modules/snipe-pages.en";
import { settingsPagesEn } from "./modules/settings-pages.en";
import { ctrlDialogsEn } from "./modules/ctrl-dialogs.en";
import { watchPagesEn } from "./modules/watch-pages.en";
import { maintDialogsEn } from "./modules/maint-dialogs.en";
import { vpsDialogsEn } from "./modules/vps-dialogs.en";
import { commonLayerEn } from "./modules/common-layer.en";

export type LangPack = typeof zh;

const pack: LangPack = {
  nav: {
    appName: "OVH Console",
    group: {
      overview: "Overview",
      sniping: "Sniping",
      monitor: "Monitoring",
      instances: "Instances",
      system: "System",
    },
    dashboard: "Dashboard",
    servers: "Server Catalog",
    serversShort: "Servers",
    queue: "Order Queue",
    queueShort: "Queue",
    serverMonitor: "Server Monitor",
    monitorShort: "Monitor",
    vpsMonitor: "VPS Restock",
    serverControl: "Server Control",
    vpsControl: "VPS Control",
    account: "Accounts",
    history: "Order History",
    logs: "Logs",
    settings: "API Settings",
    home: "Home",
    overview: "Overview",
  },
  common: {
    refresh: "Refresh",
    retry: "Retry",
    cancel: "Cancel",
    confirm: "Confirm",
    close: "Close",
    save: "Save",
    delete: "Delete",
    edit: "Edit",
    loading: "Loading…",
    empty: "No data",
    readFailed: "Failed to load",
    copy: "Copy",
    copied: "Copied",
    search: "Search",
    all: "All",
    enabled: "Enabled",
    disabled: "Disabled",
    unknown: "Unknown",
    language: "Language",
    appearance: "Appearance",
    checkNetwork: "Check your network and API settings, then retry",
    networkError: "Network error: the request never reached the backend. Check your connection and retry.",
  },
  lang: {
    switched: "Language: {{name}}",
    current: "Current: {{name}}. Click to switch to {{next}}",
    toggle: "Switch language, currently {{name}}",
  },
  // Backend error codes → English (mirrors api.* keys in zh.ts)
  api: apiEn as unknown as typeof apiZh,
  ...snipePagesEn,
  ...settingsPagesEn,
  ...ctrlDialogsEn,
  ...watchPagesEn,
  ...maintDialogsEn,
  ...vpsDialogsEn,
  ...commonLayerEn,
};

export const en = pack;
