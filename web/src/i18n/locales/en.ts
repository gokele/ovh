import { serverSafetyApiEn } from "./modules/server-safety.en";
import { liveChatEn, liveChatApiEn } from "./modules/live-chat.en";
import { supportEn, supportApiEn } from "./modules/support.en";
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
  orderDuration: {
    label: "Purchase period", default: "Default", months: "{{count}} month", months_other: "{{count}} months", years: "{{count}} year", years_other: "{{count}} years",
    loading: "Loading periods offered by OVH…",
    hint: "Available periods come from the selected account’s OVH offers.",
    unverified: "Periods cannot be verified yet. You can save a reservation; the selected period will be checked before ordering.",
    unsupported: "This account’s offer does not support the selected period. Choose another period.",
    prepaid: "The selected period is charged in full, including the full year for annual orders. OVH determines the final amount and renewal terms.",
  },
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
  api: { RENEWAL_INVALID_REQUEST: "Enter a valid renewal mode and period.", RENEWAL_MODE_UNAVAILABLE: "OVH does not currently allow this renewal mode for the service. Refresh and try again.", ...(apiEn as unknown as typeof apiZh), ...supportApiEn, ...liveChatApiEn, ...serverSafetyApiEn, QUEUE_ALREADY_ORDERED: "This task already placed an order and cannot be restarted. Create a new task to purchase again." },
  ...snipePagesEn,
  ...settingsPagesEn,
  ...supportEn,
  ...liveChatEn,
  ...ctrlDialogsEn,
  ...watchPagesEn,
  ...maintDialogsEn,
  ...vpsDialogsEn,
  ...commonLayerEn,
};

export const en = pack;
