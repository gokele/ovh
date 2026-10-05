import { createFileRoute } from "@tanstack/react-router";
import { Settings as SettingsIcon, KeyRound, Globe, Send, Database, Save, AlertTriangle, CheckCircle2, Plus, Star, RotateCw, Trash2, Pencil, BellRing, RefreshCw, Radio, Network, Fingerprint, ShieldAlert, Radar, Ban, Activity, Timer, Palette, Sun, Moon, Monitor } from "lucide-react";
import { useTheme } from "@/hooks/use-theme";
import { Section } from "@/routes/settings-sections";
import { AppPairingSection } from "@/components/settings/AppPairingSection";
import { Smartphone } from "lucide-react";
import type { ThemeMode } from "@/lib/theme";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { fmtDate, fmtDateTime } from "@/i18n/format";
import { toast } from "sonner";
import { LoadFailed, LoadFailedBanner } from "@/components/common/LoadFailed";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/common/Skeleton";
import { Chip } from "@/components/common/Chip";
import { StatusDot } from "@/components/common/StatusDot";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import {
  RETRY_INTERVAL,
  useSettings,
  useSaveSettings,
  useCacheInfo,
  useClearCache,
  useTelegramPoller,
  type SettingsConfig,
} from "@/hooks/use-settings";
import { getApiSecretKey, setApiSecretKey } from "@/lib/api";
import { useNotifyChannels, useTestNotification } from "@/hooks/use-notify-channels";
import { cn } from "@/lib/utils";
import { OVH_SUBSIDIARIES, subsidiaryLabel } from "@/lib/ovh-subsidiaries";
import { apiBaseUrlForEndpoint } from "@/lib/ovh-regions";
import { OvhTokenGuide } from "@/components/common/OvhTokenGuide";
import {
  useAccounts,
  useCreateAccount,
  useUpdateAccount,
  useDeleteAccount,
  useSetDefaultAccount,
  useVerifyAccount,
  useProxyStatus,
  useProxyTest,
  useLastProxyTest,
  useLastProxyTests,
  useProxyCheck,
  useLastProxyCheck,
  accountChipColor,
  gradeLatency,
  isJittery,
  worstMinMs,
  proxyCheckTime,
  type OVHAccount,
  type AccountInput,
  type AccountProxyStatus,
  type ProxyTestRecord,
  type ProxyProbeTarget,
  type LatencyLevel,
} from "@/hooks/use-accounts";

/** 根据 zone 推 endpoint */
function endpointForZone(zone: string): string {
  return OVH_SUBSIDIARIES.find((s) => s.code === zone)?.endpoint || "ovh-eu";
}

/** API 设置：左 sub-nav 200px + 右 form sections */
export const Route = createFileRoute("/settings")({
  component: SettingsPage,
});

const SECTIONS = [
  { id: "password", icon: KeyRound, label: "settings.sections.password" },
  { id: "appearance", icon: Palette, label: "settings.sections.appearance" },
  { id: "accounts", icon: Globe, label: "settings.sections.accounts" },
  { id: "purchase", icon: Timer, label: "settings.sections.purchase" },
  { id: "telegram", icon: Send, label: "settings.sections.telegram" },
  { id: "notify", icon: BellRing, label: "settings.sections.notify" },
  { id: "app", icon: Smartphone, label: "settings.sections.app" },
  { id: "cache", icon: Database, label: "settings.sections.cache" },
] as const;

function SettingsPage() {
  const { t } = useTranslation();
  const cfg = useSettings();
  const save = useSaveSettings();
  const [active, setActive] = useState<typeof SECTIONS[number]["id"]>("password");
  const [form, setForm] = useState<SettingsConfig>({});
  const [apiKey, setApiKey] = useState("");
  // 配置到底有没有读到手。没读到就绝不能保存 —— 见 onSave 里的说明。
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    if (cfg.data) {
      setForm(cfg.data);
      setLoaded(true);
    }
  }, [cfg.data]);

  useEffect(() => {
    setApiKey(getApiSecretKey() || "");
  }, []);

  // 值类型按 key 取:抢购间隔是 number | undefined,其余是 string
  const set = <K extends keyof SettingsConfig>(k: K, v: SettingsConfig[K]) =>
    setForm((prev) => ({ ...prev, [k]: v }));

  const onSave = () => {
    // 配置没读到手的时候,form 还停在初始的 {} —— 所有输入框看上去都是"未配置"。
    // 这时候按保存,等于拿一份空配置去覆盖后端真实的 Telegram Token / Chat ID /
    // Webhook。这不是显示错误,是直接把用户的配置删了,而且他自己看不出来
    // (界面本来就显示空,保存完还是空)。所以读失败时这个按钮必须是禁用的。
    // 访问密码只写 localStorage,不经过后端配置,配置读失败也照存不误
    if (apiKey) setApiSecretKey(apiKey);
    if (!loaded) {
      toast.error(t("settings.notLoadedToast"));
      return;
    }
    // 提交前根据 zone 自动同步 endpoint，避免两者不一致
    const zone = form.zone || "IE";
    save.mutate({ ...form, zone, endpoint: endpointForZone(zone) });
  };

  // 访问密码那一节只写 localStorage,不碰后端配置,所以配置读失败也能改。
  const savableSection = active === "password" || loaded;

  return (
    <div className="space-y-3 sm:space-y-6">
      <PageHeader
        icon={SettingsIcon}
        title={t("settings.title")}
        description={t("settings.description")}
        action={
          <Button
            onClick={onSave}
            disabled={save.isPending || !savableSection}
            title={savableSection ? undefined : t("settings.saveDisabledHint")}
          >
            <Save className="w-4 h-4" />
            {save.isPending ? t("settings.saving") : t("settings.saveButton")}
          </Button>
        }
      />

      <div className="grid grid-cols-1 lg:grid-cols-[200px_1fr] gap-4">
        {/* sub-nav:桌面竖向左栏,手机横向滚动 tab */}
        {/* 手机端这是一条横滑的 tab 条。原来右边直接被裁断,「通知通道」只露半个字,
            看不出还能往右滑 —— 加一层右侧渐隐当作可滚动的提示,
            并且隐藏滚动条(移动端本来就不显示,桌面横滑时那条也碍眼)。 */}
        <div className="relative lg:contents">
          {/* nav 用了 -mx-3 出血到屏幕边,渐隐也要跟着 -right-3,
              否则它只盖到栅格格子的边界,真正被裁断的那几像素还是硬切 */}
          <div className="pointer-events-none absolute -right-3 top-0 bottom-0 w-10 bg-gradient-to-l from-background via-background/80 to-transparent lg:hidden z-10" />
        <nav className="lg:space-y-1 flex lg:flex-col overflow-x-auto lg:overflow-visible gap-1 lg:gap-0 -mx-3 px-3 lg:mx-0 lg:px-0 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
          {SECTIONS.map((s) => {
            const Icon = s.icon;
            const a = active === s.id;
            return (
              <button
                key={s.id}
                type="button"
                onClick={() => setActive(s.id)}
                className={cn(
                  "flex items-center gap-2 px-3 py-2 rounded-md text-[13px] transition-colors whitespace-nowrap flex-shrink-0",
                  "lg:w-full lg:border-l-2",
                  a
                    ? "bg-secondary text-foreground font-medium lg:border-l-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground lg:border-l-transparent"
                )}
              >
                <Icon className="w-4 h-4" />
                {t(s.label)}
              </button>
            );
          })}
        </nav>
        </div>

        {/* 右内容 */}
        <Card>
          <CardContent className="p-3 sm:p-6">
            {cfg.isPending ? (
              <Skeleton className="h-64 rounded-2xl" />
            ) : cfg.isError && active !== "password" && active !== "appearance" && active !== "app" && active !== "accounts" && active !== "cache" ? (
              // 配置读失败时,Telegram / 通知通道那些输入框会全渲染成空 ——
              // 看上去就是"你还没配过",而实际上后端存着真实配置。
              // 在这里直接换成失败态,顺便挡住"照着空表单点保存"这条把配置删干净的路。
              <LoadFailed
                icon={SettingsIcon}
                title={t("settings.loadFailedTitle")}
                error={cfg.error}
                onRetry={() => cfg.refetch()}
                compact
              />
            ) : active === "password" ? (
              <Section title={t("settings.passwordSection.title")}>
                <Field label={t("settings.passwordSection.label")} hint={t("settings.passwordSection.hint")}>
                  <Input
                    type="password"
                    value={apiKey}
                    onChange={(e) => setApiKey(e.target.value)}
                    placeholder={t("settings.passwordSection.placeholder")}
                  />
                </Field>
              </Section>
            ) : active === "appearance" ? (
              <AppearanceSection />
            ) : active === "app" ? (
              <AppPairingSection />
            ) : active === "accounts" ? (
              <AccountsSection />
            ) : active === "telegram" ? (
              <TelegramSection form={form} set={set} />
            ) : active === "notify" ? (
              <NotifySection form={form} set={set} />
            ) : active === "purchase" ? (
              <PurchaseSection form={form} set={set} />
            ) : (
              <CacheSection />
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

/**
 * 抢购参数。
 *
 * 这两个间隔以前是散在四条入队路径里的字面量（30 / 30 / 30 / 2），用户既改不了、
 * 前端弹窗显示的默认值（60）还跟后端实际用的（30）对不上。现在统一读这里。
 */
function PurchaseSection({
  form,
  set,
}: {
  form: SettingsConfig;
  set: <K extends keyof SettingsConfig>(k: K, v: SettingsConfig[K]) => void;
}) {
  const { t } = useTranslation();
  /** 秒数输入：允许中途空串（正在删改），失焦/提交时后端会把 0 夹回默认值 */
  const numField = (k: "defaultRetryInterval" | "quickOrderRetryInterval", fallback: number) => (
    <Input
      type="text"
      inputMode="numeric"
      value={form[k] === undefined ? "" : String(form[k])}
      placeholder={t("settings.purchase.defaultPlaceholder", { value: fallback })}
      onChange={(e) => {
        const v = e.target.value;
        if (v === "") return set(k, undefined);
        if (/^\d+$/.test(v)) set(k, Number(v));
      }}
    />
  );

  const invalid = (v?: number) =>
    v !== undefined && (v < RETRY_INTERVAL.min || v > RETRY_INTERVAL.max);

  return (
    <Section title={t("settings.purchase.title")}>
      <Field
        label={t("settings.purchase.defaultRetryLabel")}
        hint={t("settings.purchase.defaultRetryHint", {
          default: RETRY_INTERVAL.defaultTask,
          min: RETRY_INTERVAL.min,
          max: RETRY_INTERVAL.max,
        })}
      >
        {numField("defaultRetryInterval", RETRY_INTERVAL.defaultTask)}
        {invalid(form.defaultRetryInterval) && (
          <p className="text-[11px] text-destructive mt-1">
            {t("settings.purchase.rangeInvalid", { min: RETRY_INTERVAL.min, max: RETRY_INTERVAL.max })}
          </p>
        )}
      </Field>

      <Field
        label={t("settings.purchase.quickRetryLabel")}
        hint={t("settings.purchase.quickRetryHint", { default: RETRY_INTERVAL.defaultQuick })}
      >
        {numField("quickOrderRetryInterval", RETRY_INTERVAL.defaultQuick)}
        {invalid(form.quickOrderRetryInterval) && (
          <p className="text-[11px] text-destructive mt-1">
            {t("settings.purchase.rangeInvalid", { min: RETRY_INTERVAL.min, max: RETRY_INTERVAL.max })}
          </p>
        )}
      </Field>

      <div className="rounded-2xl border border-border bg-secondary/30 px-4 py-3 text-[12px] text-muted-foreground">
        {t("settings.purchase.onlyNewPre")}<b className="text-foreground">{t("settings.purchase.onlyNewBold")}</b>{t("settings.purchase.onlyNewPost")}
      </div>
    </Section>
  );
}

// Section 组件已抽到 settings-sections.tsx(App 配对面板也要用)

/** 外观:浅色 / 深色 / 跟随系统。纯前端偏好,存浏览器 localStorage,
 *  不走后端配置 —— 换个浏览器或设备要重选,但它也不该跟着后端走。 */
function AppearanceSection() {
  const { t } = useTranslation();
  const { mode, resolved, setMode } = useTheme();
  const options: { value: ThemeMode; label: string; desc: string; icon: React.ReactNode }[] = [
    { value: "system", label: t("settings.appearance.system"), desc: t("settings.appearance.systemDesc"), icon: <Monitor className="w-4 h-4" /> },
    { value: "light", label: t("settings.appearance.light"), desc: t("settings.appearance.lightDesc"), icon: <Sun className="w-4 h-4" /> },
    { value: "dark", label: t("settings.appearance.dark"), desc: t("settings.appearance.darkDesc"), icon: <Moon className="w-4 h-4" /> },
  ];
  return (
    <Section title={t("settings.sections.appearance")}>
      <div className="space-y-2">
        {options.map((o) => (
          <button
            key={o.value}
            type="button"
            onClick={() => setMode(o.value)}
            className={`w-full flex items-center gap-3 rounded-xl border px-4 py-3 text-left transition-colors ${
              mode === o.value
                ? "border-foreground/60 bg-accent text-accent-foreground"
                : "border-border hover:bg-accent/50"
            }`}
          >
            {/* 选中态底色是 accent,深色下它是浅色 —— 字和图标必须跟 accent-foreground,
                否则浅字浅底直接隐形(对比度 1.0,实测踩过) */}
            <span className={mode === o.value ? "text-accent-foreground" : "text-muted-foreground"}>{o.icon}</span>
            <span className="flex-1 min-w-0">
              <span className="block text-[13px] font-medium">{o.label}</span>
              <span className={`block text-[11px] ${mode === o.value ? "text-accent-foreground/70" : "text-muted-foreground"}`}>{o.desc}</span>
            </span>
            {mode === o.value && (
              <span className="text-[11px] text-accent-foreground/70 flex-shrink-0">
                {resolved === "dark" ? t("settings.appearance.currentDark") : t("settings.appearance.currentLight")}
              </span>
            )}
          </button>
        ))}
      </div>
      <p className="text-[11px] text-muted-foreground">
        {t("settings.appearance.footnote")}
      </p>
    </Section>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="block text-[13px] font-medium mb-1.5">{label}</label>
      {children}
      {hint && <p className="text-[11px] text-muted-foreground mt-1">{hint}</p>}
    </div>
  );
}

/**
 * 通知通道。
 *
 * 存在的理由：以前 Telegram 是唯一通道，而监控在 Telegram 校验失败时会**自动停止** ——
 * bot 被封、token 过期、机器连不上 api.telegram.org，任何一种情况下用户失去的
 * 不是一条消息，而是整个监控，且只有翻日志才知道。现在只要还有一条通道能用，监控就继续跑。
 */
function NotifySection({
  form,
  set,
}: {
  form: SettingsConfig;
  set: (k: keyof SettingsConfig, v: string) => void;
}) {
  const { t } = useTranslation();
  const channels = useNotifyChannels(true);
  const test = useTestNotification();

  return (
    <Section title={t("settings.notify.title")}>
      <div className="rounded-2xl border border-border p-4 space-y-2">
        <div className="flex items-center justify-between">
          <h3 className="text-[13px] font-medium">{t("settings.notify.currentStatus")}</h3>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => channels.refetch()}
            disabled={channels.isFetching}
          >
            <RefreshCw className={cn("w-3.5 h-3.5", channels.isFetching && "animate-spin")} />
            {t("settings.notify.recheck")}
          </Button>
        </div>
        {channels.isPending ? (
          <p className="text-[12px] text-muted-foreground">{t("settings.checking")}</p>
        ) : channels.isError ? (
          // 检测请求本身挂了。以前这里会渲染成一片空白 —— 既没有通道列表,
          // 下面那条"一条可用通道都没有"的警告也因为守卫里带了 channels.data 而不出现。
          // 用户看到的是"什么都没有",而不是"没检测成功"。
          <LoadFailedBanner
            title={t("settings.notify.detectFailedTitle")}
            error={channels.error}
            onRetry={() => channels.refetch()}
          />
        ) : (
          <div className="space-y-1.5">
            {(channels.data?.channels || []).map((c) => (
              <div key={c.name} className="flex items-start gap-2 text-[12px]">
                {!c.configured ? (
                  <span className="mt-1.5 w-1.5 h-1.5 rounded-full bg-muted-foreground/40 flex-shrink-0" />
                ) : c.ok ? (
                  <CheckCircle2 className="w-3.5 h-3.5 text-success flex-shrink-0 mt-0.5" />
                ) : (
                  <AlertTriangle className="w-3.5 h-3.5 text-warning flex-shrink-0 mt-0.5" />
                )}
                <span className="font-medium w-20 flex-shrink-0">{c.name}</span>
                <span className="text-muted-foreground break-all">
                  {!c.configured ? t("settings.notify.notConfigured") : c.ok ? t("settings.notify.available") : c.detail || t("settings.notify.unavailable")}
                </span>
              </div>
            ))}
            {channels.data && !channels.data.anyAvailable && (
              <p className="text-[11px] text-warning pt-1">
                {t("settings.notify.noneAvailable")}
              </p>
            )}
          </div>
        )}
      </div>

      <Field label={t("settings.notify.webhookLabel")}>
        <Input
          value={form.notifyWebhookUrl || ""}
          onChange={(e) => set("notifyWebhookUrl", e.target.value)}
          placeholder={t("settings.notify.webhookPlaceholder")}
        />
        <p className="text-[11px] text-muted-foreground mt-1">
          {t("settings.notify.webhookHintPre")}<b>{t("settings.notify.webhookHintBold")}</b>{t("settings.notify.webhookHintMid")}
          <code className="mx-1 px-1 rounded bg-muted">text</code>
          <code className="mr-1 px-1 rounded bg-muted">message</code>
          <code className="mr-1 px-1 rounded bg-muted">text_content.text</code>
          {t("settings.notify.webhookHintPost")}
        </p>
      </Field>

      <div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => test.mutate()}
          disabled={test.isPending}
        >
          <Send className={cn("w-3.5 h-3.5", test.isPending && "animate-pulse")} />
          {test.isPending ? t("settings.notify.sending") : t("settings.notify.sendTest")}
        </Button>
        <p className="text-[11px] text-muted-foreground mt-1.5">
          {t("settings.notify.testHint")}
        </p>
      </div>
    </Section>
  );
}

/**
 * 一条轮询错误是不是"另一个进程在抢同一个 Token"。
 * 判据跟后端日志里那段保持一致(Conflict / 409),两边说法不一致会把人绕晕。
 */
function isPollConflict(err: string): boolean {
  return err.includes("Conflict") || err.includes("409");
}

function TelegramSection({
  form,
  set,
}: {
  form: SettingsConfig;
  set: (k: keyof SettingsConfig, v: string) => void;
}) {
  const { t } = useTranslation();
  const poll = useTelegramPoller();

  // poller 整个对象可能缺(后端还没初始化) —— 那是"没问到状态",不是"停了"。
  // 混在一起会让用户去反复重启一个其实在正常跑的东西。
  const poller = poll.data?.poller;
  const hasToken = poll.data?.hasToken === true;

  return (
    <Section title={t("settings.telegram.title")}>
      {/* 收 update 只有长轮询一条路。webhook 那条已经删掉了:
          它要公网 HTTPS 域名 + 受信证书,还得把回调端点放进鉴权白名单,
          于是只能靠 secret_token 证明来源 —— 一整套只为解决"入站端点会被伪造"
          这一个问题的东西。没有入站端点,这些连同它们的出错面一起消失了。 */}
      <div className="rounded-2xl border border-border p-4 space-y-2.5 text-[13px]">
        <div className="flex items-center justify-between">
          <h3 className="text-[13px] font-medium flex items-center gap-1.5">
            <Radio className="w-3.5 h-3.5 text-muted-foreground" />
            {t("settings.telegram.pollerTitle")}
          </h3>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => poll.refetch()}
            disabled={poll.isFetching}
          >
            <RefreshCw className={cn("w-3.5 h-3.5", poll.isFetching && "animate-spin")} />
            {t("common.refresh")}
          </Button>
        </div>
        <p className="text-[11px] text-muted-foreground">
          {t("settings.telegram.pollerDescPre")}<b>{t("settings.telegram.pollerDescBold")}</b>{t("settings.telegram.pollerDescPost")}
        </p>

        {poll.isPending ? (
          <Skeleton className="h-20 rounded-xl" />
        ) : poll.isError ? (
          <LoadFailedBanner
            title={t("settings.telegram.statusFailedTitle")}
            error={poll.error}
            onRetry={() => poll.refetch()}
          />
        ) : !hasToken ? (
          <p className="text-[12px] text-warning">
            {t("settings.telegram.noTokenWarning")}
          </p>
        ) : !poller ? (
          // 后端没带 poller 回来 —— 看不到状态,不是停了
          <p className="text-[12px] text-muted-foreground">
            {t("settings.telegram.noPollerInfo")}
          </p>
        ) : (
          <>
            <InfoRow
              label={t("settings.telegram.runningState")}
              value={
                poller.running ? (
                  <Chip tone="success">
                    <CheckCircle2 className="w-3 h-3" />
                    {t("settings.telegram.running")}
                  </Chip>
                ) : (
                  <Chip tone="danger">
                    <AlertTriangle className="w-3 h-3" />
                    {t("settings.telegram.stopped")}
                  </Chip>
                )
              }
            />
            <InfoRow
              label={t("settings.telegram.lastPoll")}
              value={
                poller.lastPollAt ? (
                  <span className="font-mono text-[12px]">
                    {fmtDateTime(poller.lastPollAt)}
                  </span>
                ) : (
                  <span className="text-muted-foreground">{t("settings.telegram.neverPolled")}</span>
                )
              }
            />
            <InfoRow
              label={t("settings.telegram.confirmedOffset")}
              value={<span className="font-mono text-[12px]">{poller.offset}</span>}
            />
            {poller.lastError ? (
              <div className="rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2 text-[12px] flex items-start gap-2">
                <AlertTriangle className="w-4 h-4 text-destructive flex-shrink-0 mt-0.5" />
                <div className="min-w-0">
                  <p className="font-semibold text-destructive">{t("settings.telegram.lastErrorTitle")}</p>
                  <p className="mt-0.5 break-words">{poller.lastError}</p>
                  {isPollConflict(poller.lastError) && (
                    <p className="mt-1.5 text-destructive">
                      {t("settings.telegram.conflictPre")}<b>{t("settings.telegram.conflictBold")}</b>{t("settings.telegram.conflictPost")}
                    </p>
                  )}
                </div>
              </div>
            ) : (
              <InfoRow
                label={t("settings.telegram.errorState")}
                value={
                  <Chip tone="success">
                    <CheckCircle2 className="w-3 h-3" />
                    {t("settings.telegram.normal")}
                  </Chip>
                }
              />
            )}
            {!poller.running && (
              <p className="text-[11px] text-destructive">
                {t("settings.telegram.notRunningWarning")}
              </p>
            )}
          </>
        )}
      </div>

      <Field label="Bot Token">
        <Input
          type="password"
          value={form.tgToken || ""}
          onChange={(e) => set("tgToken", e.target.value)}
          placeholder="123456:ABCdef..."
        />
      </Field>
      <Field label="Chat ID">
        <Input
          value={form.tgChatId || ""}
          onChange={(e) => set("tgChatId", e.target.value)}
          placeholder="-1001234567890"
        />
      </Field>

    </Section>
  );
}

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex justify-between items-start gap-3">
      <span className="text-muted-foreground flex-shrink-0">{label}</span>
      <span className="font-medium text-right min-w-0">{value}</span>
    </div>
  );
}

function CacheSection() {
  const { t } = useTranslation();
  const info = useCacheInfo();
  const clear = useClearCache();
  const sqliteUpdated = info.data?.sqlite?.updatedAtMs
    ? fmtDateTime(info.data.sqlite.updatedAtMs)
    : t("settings.cache.neverRefreshed");
  return (
    <Section title={t("settings.cache.title")}>
      {info.isPending ? (
        <Skeleton className="h-32 rounded-2xl" />
      ) : info.isError ? (
        // 这五行以前会在请求失败时全部渲染成假读数:条数 0、状态"已过期"、
        // "从未刷新"、路径"—"。用户据此去点"清除全部",清的是一份他根本没看清的东西。
        <LoadFailed
          icon={Database}
          title={t("settings.cache.loadFailedTitle")}
          error={info.error}
          onRetry={() => info.refetch()}
          compact
        />
      ) : (
        <div className="border border-border rounded-2xl p-4 space-y-2.5 text-[13px]">
          <Row label={t("settings.cache.memCount")} value={info.data?.backend?.serverCount ?? 0} />
          <Row label={t("settings.cache.memState")} value={info.data?.backend?.cacheValid ? t("settings.cache.valid") : t("settings.cache.expired")} />
          <Row label={t("settings.cache.sqliteCount")} value={info.data?.sqlite?.serverCount ?? 0} />
          <Row label={t("settings.cache.sqliteRefreshed")} value={<span className="text-[12px]">{sqliteUpdated}</span>} />
          <Row
            label={t("settings.cache.dbLocation")}
            value={
              <code className="text-[11px] font-mono">
                {info.data?.sqlite?.path || info.data?.storage?.dataDir || "—"}
              </code>
            }
          />
        </div>
      )}
      <p className="text-[11px] text-muted-foreground">
        {t("settings.cache.scopeNote")}
      </p>
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" onClick={() => clear.mutate("memory")} disabled={clear.isPending}>
          {t("settings.cache.clearMemory")}
        </Button>
        <Button variant="outline" onClick={() => clear.mutate("sqlite")} disabled={clear.isPending}>
          {t("settings.cache.clearSqlite")}
        </Button>
        <Button variant="destructive" onClick={() => clear.mutate("all")} disabled={clear.isPending}>
          {t("settings.cache.clearAll")}
        </Button>
      </div>
    </Section>
  );
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex justify-between items-center gap-2">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-medium text-right">{value}</span>
    </div>
  );
}

// ─── 账户管理 ───────────────────────────────────────────────────────────────

function AccountsSection() {
  const { t } = useTranslation();
  const accounts = useAccounts();
  // 代理健康:30 秒一轮。跳闸的账户,它的抢购任务已经被后端停掉了 ——
  // 这件事不在界面上说,用户看到的现象只是"这个账户一直抢不到"。
  const health = useProxyStatus();
  const [showAdd, setShowAdd] = useState(false);
  const [editAcc, setEditAcc] = useState<OVHAccount | null>(null);
  const list = accounts.data || [];
  const healthByID = new Map((health.data?.accounts || []).map((h) => [h.id, h]));

  // 各账户最近一次测到的出口 IP,按 IP 归堆。撞在同一个 IP 上的必须挑出来说 ——
  // 这正是"隔离有没有生效"的唯一判据,而它只有把多个账户放在一起才看得出来。
  const tests = useLastProxyTests(list.map((a) => a.id));
  const byIP = new Map<string, { name: string; hasProxy: boolean }[]>();
  list.forEach((a, i) => {
    const r = tests[i]?.data;
    if (!r || !r.success) return;
    byIP.set(r.egressIP, [...(byIP.get(r.egressIP) || []), { name: a.name, hasProxy: !!a.proxyUrl }]);
  });
  const collisions = Array.from(byIP.entries())
    .filter(([, xs]) => xs.length > 1)
    // 撞车里有配了代理的账户,性质就完全不同了:那不是"正常共用出口",是代理没生效
    .map(([ip, xs]) => ({ ip, xs, proxied: xs.some((x) => x.hasProxy) }));
  const proxiedCollision = collisions.some((c) => c.proxied);

  return (
    <Section title={t("settings.accounts.title")}>
      <div className="flex items-start justify-between gap-3">
        <p className="text-[12px] text-muted-foreground">
          {t("settings.accounts.intro")}
        </p>
        <Button onClick={() => setShowAdd(true)} size="sm" className="flex-shrink-0">
          <Plus className="w-4 h-4" />
          {t("settings.accounts.addAccount")}
        </Button>
      </div>

      {/* 为什么每个账户要有自己的出口 —— 不讲清楚,代理这一栏看起来只是个可选项 */}
      <div className="rounded-xl border border-border bg-secondary/30 px-3 py-2.5 space-y-1.5 text-[11px] leading-relaxed">
        <p className="font-semibold flex items-center gap-1.5">
          <Network className="w-3.5 h-3.5" />
          {t("settings.accounts.proxyWhyTitle")}
        </p>
        <p className="text-muted-foreground">
          {t("settings.accounts.proxyWhyP1")}
        </p>
        <p className="text-muted-foreground">
          {t("settings.accounts.proxyWhyP2Pre")}<b className="text-warning">{t("settings.accounts.proxyWhyP2Bold")}</b>{t("settings.accounts.proxyWhyP2Post")}
        </p>
      </div>

      {/* 健康状态没问到时,下面各卡片"没有告警"并不等于"没问题" */}
      {health.isError && (
        <LoadFailedBanner
          title={t("settings.accounts.healthFailedTitle")}
          error={health.error}
          onRetry={() => health.refetch()}
        />
      )}

      {collisions.length > 0 && (
        <div
          className={cn(
            "rounded-xl border px-3 py-2.5 text-[11px] space-y-1",
            proxiedCollision ? "border-destructive/40 bg-destructive/5" : "border-warning/40 bg-warning/5"
          )}
        >
          <p className={cn("font-semibold flex items-center gap-1.5", proxiedCollision ? "text-destructive" : "text-warning")}>
            <AlertTriangle className="w-3.5 h-3.5" />
            {proxiedCollision
              ? t("settings.accounts.collisionProxiedTitle")
              : t("settings.accounts.collisionSharedTitle")}
          </p>
          {collisions.map((c) => (
            <p key={c.ip} className="text-muted-foreground">
              <span className="font-mono font-semibold text-foreground">{c.ip}</span> ←{" "}
              {c.xs.map((x) => `${x.name}${x.hasProxy ? t("settings.accounts.proxiedSuffix") : t("settings.accounts.directSuffix")}`).join(t("settings.accounts.listSeparator"))}
            </p>
          ))}
          <p className="text-muted-foreground">
            {t("settings.accounts.collisionNote")}
          </p>
        </div>
      )}

      {accounts.isError ? (
        // 说成"还没有账户"会让用户重新粘一遍 OVH 三件套凭据,
        // 而后端其实存着好好的 —— 重复添加只会多出一个重名账户。
        <LoadFailed
          icon={Globe}
          title={t("settings.accounts.loadFailedTitle")}
          error={accounts.error}
          onRetry={() => accounts.refetch()}
          compact
        />
      ) : accounts.isPending ? (
        <div className="space-y-2">
          {Array.from({ length: 2 }).map((_, i) => (
            <Skeleton key={i} className="h-24 rounded-2xl" />
          ))}
        </div>
      ) : list.length === 0 ? (
        <Card>
          <CardContent className="p-8 text-center text-sm text-muted-foreground">
            {t("settings.accounts.emptyHint")}
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-3">
          {list.map((a) => (
            <AccountCard
              key={a.id}
              acc={a}
              onEdit={() => setEditAcc(a)}
              health={healthByID.get(a.id)}
              healthUnknown={health.isError || health.isPending}
            />
          ))}
        </div>
      )}

      {showAdd && <AccountDialog onClose={() => setShowAdd(false)} />}
      {editAcc && <AccountDialog acc={editAcc} onClose={() => setEditAcc(null)} />}
    </Section>
  );
}

function AccountCard({
  acc,
  onEdit,
  health,
  healthUnknown,
}: {
  acc: OVHAccount;
  onEdit: () => void;
  /** proxy-status 里这个账户的健康状况。undefined = 没问到,不等于"健康" */
  health?: AccountProxyStatus;
  /** 上面那条查询失败或还没回来 —— 此时这张卡上没有告警说明不了任何事 */
  healthUnknown?: boolean;
}) {
  const { t } = useTranslation();
  const setDefault = useSetDefaultAccount();
  const del = useDeleteAccount();
  const verify = useVerifyAccount();
  // 链路检测放在卡片这一层而不是弹窗里:它要跑几秒,用户中途关掉弹窗很正常。
  // 放弹窗里一关就把 pending 状态弄丢了,回头再打开看起来像什么都没发生过。
  const check = useProxyCheck();
  const [confirming, setConfirming] = useState(false);
  const [checking, setChecking] = useState(false);
  // 最近一次出口测试(在编辑框里点的)。摆在卡片上是为了让几个账户的出口 IP 并排可比。
  const lastTest = useLastProxyTest(acc.id).data;

  // 后端 /accounts/:id/verify 除了 valid 还会带 subsidiaryWarning:
  // zone(决定目录站点/币种/下单 region)与 OVH /me 的 ovhSubsidiary 不一致时,凭据依然有效,
  // 但每一次调用都会打到错误的站点。toast 会消失,这里再常驻一条,免得用户点完验证就忘了。
  const subsidiaryWarning = verify.data?.subsidiaryWarning;

  return (
    <div className="border border-border rounded-2xl p-4 flex flex-col gap-3">
      <div className="flex flex-col sm:flex-row sm:items-center gap-3">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-1 flex-wrap">
          <span className="font-semibold text-sm">{acc.name}</span>
          <span className={cn("inline-flex items-center px-2 py-0.5 rounded text-[11px] font-medium", accountChipColor(acc.zone))}>
            {acc.zone}
          </span>
          {acc.isDefault && (
            <Chip tone="success">
              <Star className="w-3 h-3" />
              {t("settings.accounts.defaultChip")}
            </Chip>
          )}
        </div>
        <div className="text-[11px] text-muted-foreground flex items-center gap-2 flex-wrap font-mono">
          <span>{acc.endpoint}</span>
          <span>·</span>
          <span>{acc.iam}</span>
          <span>·</span>
          <span>{t("settings.accounts.builtAt", { date: fmtDate(acc.createdAt) })}</span>
        </div>
        {/* 出站配置 + 最近测到的出口 IP。IP 放在这里就是为了几个账户之间横向比对 */}
        <div className="flex items-center gap-1.5 flex-wrap mt-1.5">
          {acc.proxyUrl ? (
            <Chip tone="info">
              <Network className="w-3 h-3" />
              <span className="font-mono">{acc.proxyUrl}</span>
            </Chip>
          ) : (
            <Chip>{t("settings.accounts.direct")}</Chip>
          )}
          <Chip>
            <Fingerprint className="w-3 h-3" />
            {acc.fingerprint || "default"}
          </Chip>
          {lastTest && lastTest.success ? (
            <Chip tone="success" title={t("settings.accounts.testedAtTitle", { date: fmtDateTime(lastTest.testedAt) })}>
              {t("settings.accounts.egressLabel")} <span className="font-mono font-semibold">{lastTest.egressIP}</span>
            </Chip>
          ) : lastTest ? (
            <Chip tone="danger" title={lastTest.error}>{t("settings.accounts.egressFailedVia", { via: lastTest.via })}</Chip>
          ) : (
            <span className="text-[11px] text-muted-foreground">{t("settings.accounts.egressUntested")}</span>
          )}
        </div>
      </div>
      <div className="flex items-center gap-2 flex-shrink-0">
        {/* 凭据对不对、出口是哪个 IP,都不回答"这条链路快不快" —— 而抢购输赢就在这上面 */}
        <Button
          variant="outline"
          size="sm"
          onClick={() => setChecking(true)}
          title={t("settings.accounts.chainCheckTitle")}
        >
          <Activity className={cn("w-3.5 h-3.5", check.isPending && "animate-pulse")} />
          {check.isPending ? t("settings.checking") : t("settings.accounts.chainCheck")}
        </Button>
        <Button variant="ghost" size="icon" onClick={() => verify.mutate(acc.id)} disabled={verify.isPending} title={t("settings.accounts.reverifyTitle")}>
          <RotateCw className={cn("w-4 h-4", verify.isPending && "animate-spin")} />
        </Button>
        {!acc.isDefault && (
          <Button variant="ghost" size="icon" onClick={() => setDefault.mutate(acc.id)} disabled={setDefault.isPending} title={t("settings.accounts.setDefaultTitle")}>
            <Star className="w-4 h-4" />
          </Button>
        )}
        <Button variant="ghost" size="icon" onClick={onEdit} title={t("common.edit")}>
          <Pencil className="w-4 h-4" />
        </Button>
        <Button variant="ghost" size="icon" onClick={() => setConfirming(true)} title={t("common.delete")} className="text-destructive hover:text-destructive">
          <Trash2 className="w-4 h-4" />
        </Button>
      </div>
      </div>

      {/* 代理跳闸:这个账户的活儿已经被停了,必须用 destructive 说清楚,并说明不会自动恢复 */}
      {health?.tripped ? (
        <div className="rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2.5 text-[11px] space-y-1">
          <p className="font-semibold text-destructive flex items-center gap-1.5">
            <ShieldAlert className="w-3.5 h-3.5" />
            {t("settings.accounts.trippedTitle", { count: health.fails })}
          </p>
          <p className="text-muted-foreground">
            {t("settings.accounts.trippedSubsNote")}
            {health.trippedAt ? t("settings.accounts.trippedAtNote", { date: fmtDateTime(health.trippedAt) }) : ""}
          </p>
          <p className="text-muted-foreground">
            {t("settings.accounts.trippedRecoverPre")}<b>{t("settings.accounts.trippedRecoverBold")}</b>{t("settings.accounts.trippedRecoverPost")}
          </p>
        </div>
      ) : health && health.fails > 0 ? (
        <p className="text-[11px] text-warning border border-warning/40 bg-warning/5 rounded-xl px-3 py-2">
          {t("settings.accounts.proxyFailsWarning", {
            count: health.fails,
            lastFail: health.lastFailAt
              ? t("settings.accounts.lastFailNote", { time: fmtDateTime(health.lastFailAt) })
              : "",
          })}
        </p>
      ) : healthUnknown && acc.proxyUrl ? (
        <p className="text-[11px] text-muted-foreground">
          {t("settings.accounts.healthUnknownNote")}
        </p>
      ) : null}

      {subsidiaryWarning && (
        <p className="text-[11px] text-warning border border-warning/40 bg-warning/5 rounded-xl px-3 py-2">
          {t("settings.accounts.subsidiaryWarning", { detail: subsidiaryWarning })}
        </p>
      )}

      <LinkCheckDialog acc={acc} open={checking} onOpenChange={setChecking} check={check} />

      <Dialog open={confirming} onOpenChange={setConfirming}>
        <DialogContent className="w-[95vw] sm:w-full sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("settings.accounts.deleteConfirmTitle", { name: acc.name })}</DialogTitle>
            <DialogDescription className="text-destructive">
              {t("settings.accounts.deleteConfirmDesc")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirming(false)}>{t("common.cancel")}</Button>
            <Button
              variant="destructive"
              onClick={async () => {
                await del.mutateAsync(acc.id);
                setConfirming(false);
              }}
              disabled={del.isPending}
            >
              {t("settings.accounts.deleteConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/** 延迟档位 → 文字颜色。数字光摆着没用,用户要的是"这个数字算好还是算差" */
const latencyText: Record<LatencyLevel, string> = {
  fast: "text-success",
  slow: "text-warning",
  bad: "text-destructive",
};

const latencyBox: Record<LatencyLevel, string> = {
  fast: "border-success/40 bg-success/5",
  slow: "border-warning/40 bg-warning/5",
  bad: "border-destructive/40 bg-destructive/5",
};

const latencyLabel: Record<LatencyLevel, string> = {
  fast: "settings.accounts.latency.fast",
  slow: "settings.accounts.latency.slow",
  bad: "settings.accounts.latency.bad",
};

/**
 * 一个探测目标一行:● 名称 …… 最小 XXXms / 平均 XXXms  HTTP 200
 *
 * 最小值按档位着色 —— 一屏里要能一眼扫出哪条链路拖后腿,而不是回头自己去比数字。
 */
function ProbeRow({ t }: { t: ProxyProbeTarget }) {
  const { t: tt } = useTranslation();
  const g = t.ok && typeof t.minMs === "number" ? gradeLatency(t.minMs) : null;
  return (
    <div className="space-y-0.5">
      <div className="flex items-center gap-2">
        <StatusDot tone={t.ok ? "success" : "danger"} />
        <span className="text-[12px] font-medium truncate" title={t.url}>
          {t.name}
        </span>
        <span className="flex-1 min-w-[8px] border-b border-dashed border-border" />
        {t.ok ? (
          <span className="text-[11px] font-mono whitespace-nowrap">
            {tt("settings.accounts.probeMin")} <b className={g ? latencyText[g.level] : undefined}>{t.minMs}ms</b>
            <span className="text-muted-foreground">{tt("settings.accounts.probeAvg")}{t.avgMs}ms</span>
          </span>
        ) : (
          <span className="text-[11px] text-destructive whitespace-nowrap">{tt("settings.accounts.probeNoResponse")}</span>
        )}
        {t.status ? (
          <Chip className="whitespace-nowrap">HTTP {t.status}</Chip>
        ) : null}
      </div>
      {!t.ok && t.error && <p className="pl-4 text-[11px] text-destructive break-all">{t.error}</p>}
      {/* 通了却还带着 error:3 次采样里有失败的。既不是"通"也不是"不通",单独说清楚 */}
      {t.ok && t.error && (
        <p className="pl-4 text-[11px] text-warning break-all">
          {tt("settings.accounts.probeJitter", { error: t.error })}
        </p>
      )}
    </div>
  );
}

/**
 * 出站链路体检弹窗。
 *
 * 「测试出口 IP」回答的是"我从哪个 IP 出去",这里回答"从这个账户打到 OVH 要多久" ——
 * 出口对了不代表抢得到:1400ms 的代理和 300ms 的代理,在补货那一刻是两种结果。
 *
 * 打开不自动开测:检测会真的去打 OVH、要几秒。上一次的结果留在 query 缓存里,
 * 关掉再打开看到的是那一份 + 那一次的时间,要新的就自己点重测。
 */
function LinkCheckDialog({
  acc,
  open,
  onOpenChange,
  check,
}: {
  acc: OVHAccount;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  /** mutation 放在卡片那一层,中途关掉弹窗也不会把"正在检测"弄丢 */
  check: ReturnType<typeof useProxyCheck>;
}) {
  const { t } = useTranslation();
  const record = useLastProxyCheck(acc.id).data;
  // 三种状态必须分开:没测过 / 检测没做成(我们没问到) / 测完了(才有资格谈通不通、快不快)
  const done = record && record.success ? record : null;
  const run = () => check.mutate(acc.id);

  const targets = done?.targets || [];
  const down = targets.filter((tg) => !tg.ok);
  const worst = worstMinMs(targets);
  const grade = worst === undefined ? null : gradeLatency(worst);
  const jittery = targets.filter(isJittery);

  // 这份结果是**那一次**的配置跑出来的。代理换了之后数字就不再对应现在生效的配置 ——
  // 拿旧链路的成绩给新代理背书是最容易误判的一种。(两边都是同一个打码函数的产物,可以直接比。)
  const staleCfg = !!done && done.proxy !== (acc.proxyUrl || "");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Activity className="w-4 h-4" />
            {t("settings.accounts.linkCheckTitle", { name: acc.name })}
          </DialogTitle>
          <DialogDescription>
            {t("settings.accounts.linkCheckDescPre")}<b>{t("settings.accounts.linkCheckDescBold")}</b>{t("settings.accounts.linkCheckDescPost")}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3 py-1 max-h-[65vh] overflow-y-auto -mx-6 px-6">
          {/* 这一次走的是什么配置。代理地址是打过码的,和编辑框里那份写法一致,可以直接核对 */}
          <div className="rounded-xl border border-border bg-secondary/30 px-3 py-2.5 space-y-1.5">
            <div className="flex items-center gap-1.5 flex-wrap">
              <span className="font-semibold text-[13px]">{done ? done.accountName : acc.name}</span>
              <span
                className={cn(
                  "inline-flex items-center px-2 py-0.5 rounded text-[11px] font-medium",
                  accountChipColor(done ? done.region : acc.zone)
                )}
              >
                {done ? t("settings.accounts.regionChip", { region: done.region }) : acc.zone}
              </span>
              {(done ? done.usingProxy : !!acc.proxyUrl) ? (
                <Chip tone="info">
                  <Network className="w-3 h-3" />
                  <span className="font-mono">{(done ? done.proxy : acc.proxyUrl) || t("settings.accounts.proxyFallback")}</span>
                </Chip>
              ) : (
                <Chip>{t("settings.accounts.direct")}</Chip>
              )}
              <Chip>
                <Fingerprint className="w-3 h-3" />
                {done ? done.fingerprint : acc.fingerprint || "default"}
              </Chip>
            </div>
            <p className="text-[11px] text-muted-foreground leading-relaxed">
              {done
                ? t("settings.accounts.regionScopeNote", { region: done.region })
                : t("settings.accounts.savedRouteNote")}
            </p>
            {staleCfg && (
              <p className="text-[11px] text-warning">
                {t("settings.accounts.staleCfgPre")}
                <span className="font-mono">{done?.proxy || t("settings.accounts.direct")}</span>
                {t("settings.accounts.staleCfgPost")}
              </p>
            )}
          </div>

          {/* 请求没发出去 ≠ 链路不通:前者是我们什么都没测到,绝不能画成红点 */}
          {check.isError && (
            <LoadFailedBanner
              title={t("settings.accounts.linkCheckReqFailedTitle")}
              error={check.error}
              onRetry={run}
            />
          )}

          {check.isPending ? (
            <div className="space-y-2">
              <p className="text-[11px] text-muted-foreground flex items-center gap-1.5">
                <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                {t("settings.accounts.linkCheckingNote")}
              </p>
              <Skeleton className="h-20 rounded-2xl" />
              <Skeleton className="h-24 rounded-2xl" />
            </div>
          ) : record && !record.success ? (
            <div className="rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2.5 space-y-1 text-[11px]">
              <p className="font-semibold text-destructive flex items-center gap-1.5">
                <AlertTriangle className="w-3.5 h-3.5" />
                {t("settings.accounts.backendCheckFailedTitle")}
              </p>
              <p className="text-muted-foreground break-all">{record.error}</p>
              <p className="text-muted-foreground">
                {t("settings.accounts.backendCheckFailedNote")}
              </p>
            </div>
          ) : done ? (
            <>
              {/* 出口 IP:隔离到底生没生效,只能靠它 */}
              <div
                className={cn(
                  "rounded-xl border px-3 py-2.5 space-y-1",
                  done.egressIP ? "border-success/40 bg-success/5" : "border-destructive/40 bg-destructive/5"
                )}
              >
                <p className="text-[11px] text-muted-foreground">{t("settings.accounts.egressIpLabel")}</p>
                {done.egressIP ? (
                  <>
                    <p className="text-2xl font-mono font-semibold tracking-tight break-all">{done.egressIP}</p>
                    <p className="text-[11px] text-muted-foreground">
                      {t("settings.accounts.compareIpHintFull")}
                    </p>
                  </>
                ) : (
                  <>
                    <p className="text-[13px] font-semibold text-destructive flex items-center gap-1.5">
                      <AlertTriangle className="w-3.5 h-3.5" />
                      {t("settings.accounts.noEgressIp")}
                    </p>
                    <p className="text-[11px] text-destructive break-all">{done.egressError || t("settings.accounts.egressNoReason")}</p>
                    <p className="text-[11px] text-muted-foreground">
                      {t("settings.accounts.egressIpProbeNote")}
                    </p>
                  </>
                )}
                {done.warning && <p className="text-[11px] text-warning">⚠ {done.warning}</p>}
              </div>

              {/* 目标列表 + 结论。数字必须配结论:用户没法凭 620ms 这个数自己判断该不该换代理 */}
              <div className="space-y-2">
                <p className="text-[12px] font-semibold">{t("settings.accounts.targetsTitle")}</p>
                {targets.length === 0 ? (
                  <p className="text-[11px] text-muted-foreground">
                    {t("settings.accounts.noTargetsNote")}
                  </p>
                ) : (
                  <div className="space-y-1.5">
                    {targets.map((tg) => (
                      <ProbeRow key={tg.name + tg.url} t={tg} />
                    ))}
                  </div>
                )}

                {down.length > 0 && (
                  <div className="rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2 text-[11px] space-y-0.5">
                    <p className="font-semibold text-destructive">
                      {t("settings.accounts.downTitle", { count: down.length })}
                    </p>
                    <p className="text-muted-foreground">
                      {done.usingProxy
                        ? t("settings.accounts.downProxiedNote")
                        : t("settings.accounts.downDirectNote")}
                    </p>
                  </div>
                )}

                {grade && (
                  <div className={cn("rounded-xl border px-3 py-2 text-[11px] space-y-0.5", latencyBox[grade.level])}>
                    <p className={cn("font-semibold", latencyText[grade.level])}>
                      {t("settings.accounts.slowestLine", { ms: worst, label: t(latencyLabel[grade.level]) })}
                    </p>
                    <p className="text-muted-foreground">{grade.note}</p>
                  </div>
                )}

                {/* 抖动单独说:平均值本身不显眼,但"不定时慢一拍"恰恰是抢购最怕的 */}
                {jittery.length > 0 && (
                  <div className="rounded-xl border border-warning/40 bg-warning/5 px-3 py-2 text-[11px] space-y-0.5">
                    <p className="font-semibold text-warning">
                      {t("settings.accounts.jitterTitle")}{jittery.map((tg) => tg.name).join(t("settings.accounts.listSeparator"))}
                    </p>
                    <p className="text-muted-foreground">
                      {t("settings.accounts.jitterNote")}
                    </p>
                  </div>
                )}
              </div>

              {/* 读法:不写清楚的话,一个 404 会被当成"代理坏了"而去换一个好好的代理 */}
              <div className="rounded-xl border border-border bg-secondary/30 px-3 py-2.5 space-y-1 text-[11px] leading-relaxed text-muted-foreground">
                <p className="font-semibold text-foreground">{t("settings.accounts.howToReadTitle")}</p>
                <p>
                  {t("settings.accounts.howToReadPre")}<b>{t("settings.accounts.howToReadBold1")}</b>{t("settings.accounts.howToReadMid")}
                  <b>{t("settings.accounts.howToReadBold2")}</b>{t("settings.accounts.howToReadPost")}
                </p>
                <p>
                  {t("settings.accounts.howToReadP2")}
                </p>
              </div>
            </>
          ) : !check.isError ? (
            <div className="rounded-xl border border-border px-3 py-5 text-center space-y-1">
              <p className="text-[12px] font-medium">{t("settings.accounts.neverChecked")}</p>
              <p className="text-[11px] text-muted-foreground leading-relaxed">
                {t("settings.accounts.neverCheckedNote")}
              </p>
            </div>
          ) : null}
        </div>

        <DialogFooter className="flex-wrap gap-2 space-x-0 sm:justify-between items-center">
          <span className="text-[11px] text-muted-foreground">
            {check.isPending
              ? t("settings.checking")
              : record
                ? t("settings.accounts.lastCheckAt", { date: fmtDateTime(proxyCheckTime(record)) })
                : t("settings.accounts.neverCheckedShort")}
          </span>
          <div className="flex items-center gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              {t("common.close")}
            </Button>
            <Button onClick={run} disabled={check.isPending}>
              <RefreshCw className={cn("w-3.5 h-3.5", check.isPending && "animate-spin")} />
              {check.isPending ? t("settings.checking") : record ? t("settings.accounts.recheck") : t("settings.accounts.startCheck")}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * 代理地址的前置校验,规则跟后端 netfp.ValidateProxyURL 一致(协议 + 主机 + 端口)。
 * 后端才是权威,这里只是让用户在按保存之前就看见错在哪 ——
 * 代理写错的代价不是一句报错,是这个账户在补货那一刻一单都下不出去。
 * 返回的是 i18n key,渲染处用 t() 取译文(带变量的错误在 key 里插值)。
 */
function proxyInputError(raw: string, t: (key: string, vars?: Record<string, string | number>) => string): string {
  const v = raw.trim();
  if (!v) return "";
  let u: URL;
  try {
    u = new URL(v);
  } catch {
    return t("settings.accounts.proxyErr.parse");
  }
  const scheme = u.protocol.replace(":", "").toLowerCase();
  if (!["http", "https", "socks5", "socks5h"].includes(scheme)) {
    return t("settings.accounts.proxyErr.scheme", { scheme });
  }
  if (!u.hostname) return t("settings.accounts.proxyErr.host");
  const authorityMatch = v.match(/^[^:]+:\/\/([^/?#]+)/);
  const authorityPart = authorityMatch ? authorityMatch[1] : "";
  const hasExplicitPort = Boolean(u.port) || /:\d+$/.test(authorityPart);
  if (!hasExplicitPort) {
    return t("settings.accounts.proxyErr.port");
  }
  return "";
}

/**
 * 出口 IP 测试结果面板。
 *
 * 这是用户唯一能确认"隔离真的生效"的手段,所以 IP 要显眼到能一眼跟另一个账户比对。
 * 三种状态必须分开:请求没发出去(我们没问到)、测了但失败(出口断了)、测到了。
 */
function EgressPanel({
  record,
  pending,
  requestError,
  onRetry,
  expectProxy,
}: {
  record?: ProxyTestRecord | null;
  pending: boolean;
  /** mutation 本身失败(网络/后端 500)—— 跟"代理不通"是两回事 */
  requestError?: unknown;
  onRetry: () => void;
  /** 已保存的配置里到底有没有代理,用来识别"配了代理却没生效" */
  expectProxy: boolean;
}) {
  const { t } = useTranslation();
  if (pending) return <Skeleton className="h-24 rounded-2xl" />;
  if (requestError) {
    return (
      <LoadFailedBanner
        title={t("settings.accounts.egressReqFailedTitle")}
        error={requestError}
        onRetry={onRetry}
      />
    );
  }
  if (!record) return null;
  const at = fmtDateTime(record.testedAt);

  if (!record.success) {
    return (
      <div className="rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2.5 space-y-1 text-[11px]">
        <p className="font-semibold text-destructive flex items-center gap-1.5">
          <AlertTriangle className="w-3.5 h-3.5" />
          {t("settings.accounts.egressFailedVia", { via: record.via })}
        </p>
        <p className="text-muted-foreground break-all">{record.error}</p>
        <p className="text-muted-foreground">
          {record.usingProxy
            ? t("settings.accounts.egressFailedProxyNote")
            : t("settings.accounts.egressFailedDirectNote")}
        </p>
        <p className="text-muted-foreground/70">{t("settings.accounts.testedAtShort", { date: at })}</p>
      </div>
    );
  }

  return (
    <div className="rounded-xl border border-success/40 bg-success/5 px-3 py-2.5 space-y-1.5">
      <p className="text-[11px] text-muted-foreground">{t("settings.accounts.egressIpLabel")}</p>
      <p className="text-2xl font-mono font-semibold tracking-tight break-all">{record.egressIP}</p>
      <p className="text-[11px] text-muted-foreground">
        {t("settings.accounts.viaLabel")} {record.usingProxy ? <span className="font-mono">{record.proxy || t("settings.accounts.proxyFallback")}</span> : t("settings.accounts.direct")}
        {" · "}{t("settings.accounts.fingerprintShort")} {record.fingerprint}
        {" · "}{t("settings.accounts.testedAtShort", { date: at })}
      </p>
      {expectProxy && !record.usingProxy && (
        <p className="text-[11px] text-destructive border border-destructive/40 bg-destructive/5 rounded-lg px-2 py-1.5">
          {t("settings.accounts.proxyNotAppliedPre")}<b>{t("settings.accounts.proxyNotAppliedBold")}</b>{t("settings.accounts.proxyNotAppliedPost")}
        </p>
      )}
      {!record.usingProxy && !expectProxy && (
        <p className="text-[11px] text-muted-foreground">
          {t("settings.accounts.directEgressNote")}
        </p>
      )}
      {record.warning && <p className="text-[11px] text-warning">⚠ {record.warning}</p>}
      <p className="text-[11px] text-muted-foreground">
        {t("settings.accounts.compareIpHintShort")}
      </p>
    </div>
  );
}

function AccountDialog({ acc, onClose }: { acc?: OVHAccount; onClose: () => void }) {
  const { t } = useTranslation();
  const create = useCreateAccount();
  const update = useUpdateAccount();
  const test = useProxyTest();
  // 指纹下拉的选项来源(以及整站共用的代理健康查询,key 相同不会多发请求)
  const proxyStatus = useProxyStatus();
  const isEdit = !!acc;

  // 已经落库的那份出站配置。「测试出口 IP」打的是**已保存**的配置,
  // 所以必须拿得到 id 和保存后的值 —— 新建的账户保存成功后这里才会被填上。
  const [saved, setSaved] = useState<{ id: string; proxyUrl: string; fingerprint: string } | null>(
    acc ? { id: acc.id, proxyUrl: acc.proxyUrl || "", fingerprint: acc.fingerprint || "default" } : null
  );

  // 编辑时三个凭据一律留空。后端不再下发明文（只给掩码），
  // 留空 = 保持原值（UpdateAccount 本来就是这个语义）。
  // 以前这里回填明文，等价于让 GET /api/accounts 必须吐出可用的凭据 ——
  // 那正是「任意网页读走 OVH 凭据」那条链的最后一环。
  const [form, setForm] = useState({
    name: acc?.name || "",
    appKey: "",
    appSecret: "",
    consumerKey: "",
    zone: acc?.zone || "IE",
    // 代理地址同理,也**不预填**:GET 回来的是打过码的(密码变 ***),
    // 原样提交会把 *** 存成密码,下次抢购就连不上代理了。
    proxyUrl: "",
    fingerprint: acc?.fingerprint || "default",
  });
  // 「改回直连」标记。清代理只能靠显式提交空串(后端:不传 = 不改,"" = 清掉),
  // 而输入框留空是"保持不变" —— 没有这个开关,代理一旦配上就再也摘不掉了。
  const [clearProxy, setClearProxy] = useState(false);
  const set = (k: keyof typeof form, v: string) => setForm((p) => ({ ...p, [k]: v }));

  const lastTest = useLastProxyTest(saved?.id || "").data;
  const proxyErr = clearProxy ? "" : proxyInputError(form.proxyUrl, t);
  // 新建时三个凭据必填；编辑时可以全留空（只改名字/区域/出站配置）
  const canSubmit =
    !proxyErr &&
    (saved
      ? !!form.name.trim()
      : !!(form.name.trim() && form.appKey.trim() && form.appSecret.trim() && form.consumerKey.trim()));

  // 出站配置改了但还没保存。proxy-test 打的是已保存的配置,
  // 这种时候测出来的是旧出口 —— 让用户对着旧结果判断新配置是最坏的一种误导。
  const outboundDirty = clearProxy || !!form.proxyUrl.trim() || form.fingerprint !== (saved?.fingerprint || "default");

  const profiles = proxyStatus.data?.profiles || [];
  // 可选项只认后端给的清单,前端不写死:写死的清单迟早跟后端对不上,
  // 用户选了个后端不认的名字,后端会退回 default,而界面上还显示着他选的那个。
  // 再并上"这个账户当前存着的值" —— 清单里没有它的话,Radix 找不到匹配项会显示成
  // placeholder,等于把库里真实存着的配置从界面上抹掉。
  const fpOptions = Array.from(
    new Set([...profiles, form.fingerprint, saved?.fingerprint || "default"].filter(Boolean))
  );
  // 清单没问到时锁住:此刻我们并不知道后端认哪些名字,让用户在一份自己编的清单里选是骗他
  const fpLocked = profiles.length === 0;

  // 保存成功后把基准换成后端回来的那份:输入框回到"留空 = 不变",清除标记撤掉。
  // 不这么做的话刚保存完界面仍算"有未保存的改动",测试按钮会一直是禁用的。
  const applySaved = (a: OVHAccount) => {
    setSaved({ id: a.id, proxyUrl: a.proxyUrl || "", fingerprint: a.fingerprint || "default" });
    setForm((p) => ({ ...p, proxyUrl: "", fingerprint: a.fingerprint || "default" }));
    setClearProxy(false);
  };

  const buildPayload = (): Partial<AccountInput> => {
    const payload: Partial<AccountInput> = {
      name: form.name.trim(),
      // 留空的凭据不发 —— 后端见空即保持原值
      appKey: form.appKey.trim(),
      appSecret: form.appSecret.trim(),
      consumerKey: form.consumerKey.trim(),
      zone: form.zone,
      endpoint: endpointForZone(form.zone),
    };
    if (!saved) {
      // 新建:所填即所得,空 = 直连
      payload.proxyUrl = clearProxy ? "" : form.proxyUrl.trim();
      payload.fingerprint = form.fingerprint;
      return payload;
    }
    // 编辑:指针语义。空串只在用户明确点了「改回直连」时才发,
    // 输入框留空则整个 key 都不传 —— 否则会把已配好的代理悄悄清掉。
    if (clearProxy) {
      payload.proxyUrl = "";
    } else if (form.proxyUrl.trim()) {
      payload.proxyUrl = form.proxyUrl.trim();
    }
    if (form.fingerprint !== saved.fingerprint) {
      payload.fingerprint = form.fingerprint;
    }
    return payload;
  };

  /** 保存,返回账户 ID(失败返回 null,错误提示由 hooks 里的 toast 负责) */
  const save = async (): Promise<string | null> => {
    if (!canSubmit) return null;
    const payload = buildPayload();
    if (saved) {
      const res = await update.mutateAsync({ id: saved.id, input: payload });
      applySaved(res.account);
      return saved.id;
    }
    const res = await create.mutateAsync(payload as AccountInput);
    applySaved(res.account);
    return res.account.id;
  };

  const submit = async () => {
    try {
      const id = await save();
      if (id) onClose();
    } catch {
      // useCreateAccount / useUpdateAccount 的 onError 已经弹过 toast
    }
  };

  // 保存 → 立刻用刚落库的配置测出口,并且**不关对话框**:
  // 用户的动作就是"我刚填完这个代理,当场看看通不通、出口是不是我要的那个"。
  const saveAndTest = async () => {
    try {
      const id = await save();
      if (id) await test.mutateAsync(id);
    } catch {
      // 同上,toast 已经提示过
    }
  };

  const busy = create.isPending || update.isPending || test.isPending;

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{isEdit ? t("settings.accounts.editAccountTitle", { name: acc!.name }) : t("settings.accounts.addAccountTitle")}</DialogTitle>
          <DialogDescription>{t("settings.accounts.dialogDesc")}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-2 max-h-[65vh] overflow-y-auto -mx-6 px-6">
          <Field label={t("settings.accounts.nameLabel")}>
            <Input value={form.name} onChange={(e) => set("name", e.target.value)} placeholder={t("settings.accounts.namePlaceholder")} autoFocus />
            {isEdit && (
              <p className="text-[11px] text-muted-foreground mt-1">
                {t("settings.accounts.keepCredentialsHint")}
              </p>
            )}
          </Field>
          {/* 顺序和首次录入页一致:子公司在前 —— token 申请地址跟着它变 */}
          <Field
            label={t("settings.accounts.zoneLabel")}
            hint={t("settings.accounts.zoneHint", {
              endpoint: endpointForZone(form.zone),
              iam: `go-ovh-${form.zone.toLowerCase()}`,
            })}
          >
            <Select value={form.zone} onValueChange={(v) => set("zone", v)}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                {OVH_SUBSIDIARIES.map((s) => (
                  <SelectItem key={s.code} value={s.code}>
                    {s.code} · {subsidiaryLabel(s.code)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          {/* 改已有账户时不重复这块:那时用户手上早就有密钥了 */}
          {!isEdit && <OvhTokenGuide endpoint={endpointForZone(form.zone)} />}

          <Field label="APP KEY *" hint={isEdit ? undefined : t("settings.accounts.appKeyHint")}>
            <Input type="password" value={form.appKey} onChange={(e) => set("appKey", e.target.value)}
              placeholder={isEdit ? (acc?.appKey || t("settings.accounts.keepUnchangedPlaceholder")) : t("settings.accounts.copyFromOvhPlaceholder")} />
          </Field>
          <Field label="APP SECRET *" hint={isEdit ? undefined : t("settings.accounts.appSecretHint")}>
            <Input type="password" value={form.appSecret} onChange={(e) => set("appSecret", e.target.value)}
              placeholder={isEdit ? (acc?.appSecret || t("settings.accounts.keepUnchangedPlaceholder")) : t("settings.accounts.copyFromOvhPlaceholder")} />
          </Field>
          <Field label="CONSUMER KEY *" hint={isEdit ? undefined : t("settings.accounts.consumerKeyHint")}>
            <Input type="password" value={form.consumerKey} onChange={(e) => set("consumerKey", e.target.value)}
              placeholder={isEdit ? (acc?.consumerKey || t("settings.accounts.keepUnchangedPlaceholder")) : t("settings.accounts.copyFromOvhPlaceholder")} />
          </Field>

          {/* ── 出站配置 ───────────────────────────────────────────────── */}
          <div className="border-t border-border pt-4 space-y-4">
            <div>
              <p className="text-[13px] font-semibold flex items-center gap-1.5">
                <Network className="w-3.5 h-3.5" />
                {t("settings.accounts.outboundTitle")}
              </p>
              <p className="text-[11px] text-muted-foreground mt-1 leading-relaxed">
                {t("settings.accounts.outboundDesc")}
              </p>
            </div>

            <Field label={t("settings.accounts.proxyAddrLabel")}>
              <Input
                value={form.proxyUrl}
                onChange={(e) => set("proxyUrl", e.target.value)}
                disabled={clearProxy}
                placeholder={
                  clearProxy
                    ? t("settings.accounts.proxyPlaceholderMarked")
                    : saved
                      ? t("settings.accounts.proxyPlaceholderKeep")
                      : t("settings.accounts.proxyPlaceholderNew")
                }
                className="font-mono"
              />
              {proxyErr && <p className="text-[11px] text-destructive mt-1">{t("settings.accounts.proxyInvalid", { error: proxyErr })}</p>}

              {/* 已落库的账户:回显的是打过码的地址,绝不能预填进输入框;清代理要有明确动作 */}
              {saved && (
                <div className="mt-2 space-y-1.5">
                  <p className="text-[11px] text-muted-foreground">
                    {t("settings.accounts.currentLabel")}
                    {saved?.proxyUrl ? (
                      <code className="ml-1 font-mono">{saved.proxyUrl}</code>
                    ) : (
                      <span className="ml-1">{t("settings.accounts.directNoProxy")}</span>
                    )}
                    {saved?.proxyUrl ? t("settings.accounts.maskedNote") : ""}
                  </p>
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-[11px] text-muted-foreground">{t("settings.accounts.removeProxyHint")}</span>
                    {clearProxy ? (
                      <Button variant="outline" size="sm" onClick={() => setClearProxy(false)}>
                        {t("settings.accounts.undoDirect")}
                      </Button>
                    ) : (
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={!saved?.proxyUrl}
                        onClick={() => {
                          setClearProxy(true);
                          set("proxyUrl", "");
                        }}
                      >
                        <Ban className="w-3.5 h-3.5" />
                        {t("settings.accounts.backToDirect")}
                      </Button>
                    )}
                  </div>
                  {clearProxy && (
                    <p className="text-[11px] text-warning">
                      {t("settings.accounts.clearProxyWarning")}
                    </p>
                  )}
                </div>
              )}

              <div className="mt-2 space-y-1 text-[11px] leading-relaxed">
                <p className="text-muted-foreground">
                  {t("settings.accounts.proxyFormatPre")}<code className="font-mono">http://</code> <code className="font-mono">https://</code>{" "}
                  <code className="font-mono">socks5://</code> <code className="font-mono">socks5h://</code>{t("settings.accounts.proxyFormatComma")}
                  <b>{t("settings.accounts.proxyFormatMustPort")}</b>{t("settings.accounts.proxyFormatExample")}<code className="font-mono">socks5://user:pass@1.2.3.4:1080</code>{t("settings.accounts.proxyFormatEnd")}
                  {saved ? t("settings.accounts.keepSavedNote") : t("settings.accounts.directNewNote")}
                </p>
                <p className="text-warning">
                  {t("settings.accounts.proxyFailPre")}<b>{t("settings.accounts.proxyFailBold1")}</b>{t("settings.accounts.proxyFailMid")}
                  <b>{t("settings.accounts.proxyFailBold2")}</b>{t("settings.accounts.proxyFailPost")}
                </p>
              </div>
            </Field>

            {/* 测试出口:就放在代理输入框下面,填完当场点一下 */}
            <div className="space-y-2">
              <div className="flex items-center gap-2 flex-wrap">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => saved && test.mutate(saved.id)}
                  disabled={!saved || outboundDirty || busy}
                >
                  <Radar className={cn("w-3.5 h-3.5", test.isPending && "animate-pulse")} />
                  {test.isPending ? t("settings.accounts.testing") : t("settings.accounts.testEgress")}
                </Button>
                <span className="text-[11px] text-muted-foreground">
                  {!saved
                    ? t("settings.accounts.testHintNew")
                    : outboundDirty
                      ? t("settings.accounts.testHintDirty")
                      : t("settings.accounts.testHintSaved")}
                </span>
              </div>
              <EgressPanel
                record={lastTest}
                pending={test.isPending}
                requestError={test.isError ? test.error : undefined}
                onRetry={() => saved && test.mutate(saved.id)}
                expectProxy={!!saved?.proxyUrl}
              />
            </div>

            <Field label={t("settings.accounts.outboundFingerprintLabel")}>
              <Select value={form.fingerprint} onValueChange={(v) => set("fingerprint", v)} disabled={fpLocked}>
                <SelectTrigger><SelectValue placeholder="default" /></SelectTrigger>
                <SelectContent>
                  {fpOptions.map((p) => (
                    <SelectItem key={p} value={p}>
                      {p}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {/* 选项拉不到就锁住:此刻我们不知道后端认哪些名字,瞎给一份清单只会让用户选到一个
                  后端不认、会被退回 default 的值 —— 而界面上还显示着他选的那个。 */}
              {proxyStatus.isError && (
                <div className="mt-2">
                  <LoadFailedBanner
                    title={t("settings.accounts.fingerprintListFailedTitle")}
                    error={proxyStatus.error}
                    onRetry={() => proxyStatus.refetch()}
                  />
                </div>
              )}
              {proxyStatus.isPending && (
                <p className="text-[11px] text-muted-foreground mt-1">{t("settings.accounts.readingFingerprints")}</p>
              )}
              <p className="text-[11px] text-muted-foreground mt-2 leading-relaxed">
                <b>{t("settings.accounts.fingerprintNoteBold")}</b>{t("settings.accounts.fingerprintNoteMid")}<code className="font-mono">chrome-like</code>{" "}
                <b>{t("settings.accounts.fingerprintNoteNotEqual")}</b>{t("settings.accounts.fingerprintNotePost")}
              </p>
            </Field>
          </div>

          {/* 申请密钥的说明放在这里而不是只放首次进入的弹窗:
              日常加号 / 换号都走这个对话框,而"去哪申请、申请错站点会怎样"恰恰是
              这时候最容易踩的坑。链接必须跟着上面选的子公司走 —— 三站的 token 互不通用。 */}
          <div className="rounded-xl border border-border bg-secondary/30 px-3 py-2.5 space-y-1.5">
            <p className="text-[11px] font-semibold">{t("settings.accounts.noKeyTitle")}</p>
            <p className="text-[11px] text-muted-foreground leading-relaxed">
              {t("settings.accounts.keyGuideGo")}
              <a
                href={`${apiBaseUrlForEndpoint(endpointForZone(form.zone))}/createToken/`}
                target="_blank"
                rel="noreferrer"
                className="underline mx-1 text-primary"
              >
                {apiBaseUrlForEndpoint(endpointForZone(form.zone)).replace("https://", "")}/createToken
              </a>
              {t("settings.accounts.keyGuideApply")}<b>{form.zone}</b>{t("settings.accounts.keyGuideBelongs")}
              <span className="text-warning">{t("settings.accounts.keyGuideOtherSite")}</span>{t("settings.accounts.keyGuideNotShared")}
            </p>
            <p className="text-[11px] text-muted-foreground leading-relaxed">
              {t("settings.accounts.keyGuidePermsPre")}
              <code className="mx-1 px-1 py-0.5 rounded bg-background text-[10px]">GET POST PUT DELETE</code>
              {t("settings.accounts.keyGuidePermsMid")}<code className="px-1 py-0.5 rounded bg-background text-[10px]">/*</code>{t("settings.accounts.keyGuidePermsSep")}
              <b>Unlimited</b>{t("settings.accounts.keyGuideUnlimitedPost")}
            </p>
          </div>
        </div>
        {/* 三个按钮在窄屏上要能换行,否则「保存并验证」会被挤出对话框 */}
        <DialogFooter className="flex-wrap gap-2 space-x-0">
          <Button variant="outline" onClick={onClose}>{t("common.cancel")}</Button>
          <Button variant="outline" onClick={saveAndTest} disabled={!canSubmit || busy}>
            <Radar className="w-3.5 h-3.5" />
            {busy ? t("settings.accounts.processing") : t("settings.accounts.saveAndTest")}
          </Button>
          <Button onClick={submit} disabled={!canSubmit || busy}>
            {(create.isPending || update.isPending) ? t("settings.accounts.savingAccount") : t("settings.accounts.saveAndVerify")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
