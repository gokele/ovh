import { createFileRoute } from "@tanstack/react-router";
import { Settings as SettingsIcon, KeyRound, Globe, Send, Database, Save, AlertTriangle, CheckCircle2, Plus, Star, RotateCw, Trash2, Pencil, BellRing, RefreshCw, Radio, Network, Fingerprint, ShieldAlert, Radar, Ban, Activity, Timer } from "lucide-react";
import { useEffect, useState } from "react";
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
import { OVH_SUBSIDIARIES } from "@/lib/ovh-subsidiaries";
import { apiBaseUrlForEndpoint } from "@/lib/ovh-regions";
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
  { id: "password", icon: KeyRound, label: "访问密码" },
  { id: "accounts", icon: Globe, label: "OVH 账户" },
  { id: "purchase", icon: Timer, label: "抢购" },
  { id: "telegram", icon: Send, label: "Telegram" },
  { id: "notify", icon: BellRing, label: "通知通道" },
  { id: "cache", icon: Database, label: "缓存管理" },
] as const;

function SettingsPage() {
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
      toast.error("配置还没读取成功，已跳过后端配置的保存（避免用空值覆盖）");
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
        title="API 设置"
        description="配置 OVH API 和通知设置"
        action={
          <Button
            onClick={onSave}
            disabled={save.isPending || !savableSection}
            title={savableSection ? undefined : "配置尚未读取成功,现在保存会用空值覆盖后端已有的配置"}
          >
            <Save className="w-4 h-4" />
            {save.isPending ? "保存中..." : "保存设置"}
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
                {s.label}
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
            ) : cfg.isError && active !== "password" && active !== "accounts" && active !== "cache" ? (
              // 配置读失败时,Telegram / 通知通道那些输入框会全渲染成空 ——
              // 看上去就是"你还没配过",而实际上后端存着真实配置。
              // 在这里直接换成失败态,顺便挡住"照着空表单点保存"这条把配置删干净的路。
              <LoadFailed
                icon={SettingsIcon}
                title="配置读取失败"
                error={cfg.error}
                onRetry={() => cfg.refetch()}
                compact
              />
            ) : active === "password" ? (
              <Section title="访问密码 / API Secret Key">
                <Field label="访问密码 *" hint="后端 .env 中的 API_SECRET_KEY，本地仅保存在 localStorage">
                  <Input
                    type="password"
                    value={apiKey}
                    onChange={(e) => setApiKey(e.target.value)}
                    placeholder="输入访问密码"
                  />
                </Field>
              </Section>
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
  /** 秒数输入：允许中途空串（正在删改），失焦/提交时后端会把 0 夹回默认值 */
  const numField = (k: "defaultRetryInterval" | "quickOrderRetryInterval", fallback: number) => (
    <Input
      type="text"
      inputMode="numeric"
      value={form[k] === undefined ? "" : String(form[k])}
      placeholder={`默认 ${fallback}`}
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
    <Section title="抢购参数">
      <Field
        label="新任务默认重试间隔（秒）"
        hint={`网页新建任务、Telegram /buy、上架通知里的一键下单按钮都用它。留空 = ${RETRY_INTERVAL.defaultTask} 秒。范围 ${RETRY_INTERVAL.min} ~ ${RETRY_INTERVAL.max}。`}
      >
        {numField("defaultRetryInterval", RETRY_INTERVAL.defaultTask)}
        {invalid(form.defaultRetryInterval) && (
          <p className="text-[11px] text-destructive mt-1">
            要在 {RETRY_INTERVAL.min} ~ {RETRY_INTERVAL.max} 之间
          </p>
        )}
      </Field>

      <Field
        label="监控自动下单间隔（秒）"
        hint={`/watch 自动抢触发的任务用这个。货刚出现那一刻窗口可能只有几十秒，所以默认比普通任务激进（${RETRY_INTERVAL.defaultQuick} 秒）；但太密会吃 OVH 的 429，自己权衡。`}
      >
        {numField("quickOrderRetryInterval", RETRY_INTERVAL.defaultQuick)}
        {invalid(form.quickOrderRetryInterval) && (
          <p className="text-[11px] text-destructive mt-1">
            要在 {RETRY_INTERVAL.min} ~ {RETRY_INTERVAL.max} 之间
          </p>
        )}
      </Field>

      <div className="rounded-2xl border border-border bg-secondary/30 px-4 py-3 text-[12px] text-muted-foreground">
        只影响<b className="text-foreground">之后新建</b>的任务。已经在队列里跑的任务各自带着自己的间隔，
        要改单个任务去「抢购队列」页点那条任务的秒数。
      </div>
    </Section>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="space-y-5">
      <h2 className="text-base font-semibold">{title}</h2>
      <div className="space-y-4">{children}</div>
    </div>
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
  const channels = useNotifyChannels(true);
  const test = useTestNotification();

  return (
    <Section title="通知通道">
      <div className="rounded-2xl border border-border p-4 space-y-2">
        <div className="flex items-center justify-between">
          <h3 className="text-[13px] font-medium">当前状态</h3>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => channels.refetch()}
            disabled={channels.isFetching}
          >
            <RefreshCw className={cn("w-3.5 h-3.5", channels.isFetching && "animate-spin")} />
            重新检测
          </Button>
        </div>
        {channels.isPending ? (
          <p className="text-[12px] text-muted-foreground">检测中…</p>
        ) : channels.isError ? (
          // 检测请求本身挂了。以前这里会渲染成一片空白 —— 既没有通道列表,
          // 下面那条"一条可用通道都没有"的警告也因为守卫里带了 channels.data 而不出现。
          // 用户看到的是"什么都没有",而不是"没检测成功"。
          <LoadFailedBanner
            title="通道检测失败，下面的状态不代表通道真的不可用"
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
                  <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600 flex-shrink-0 mt-0.5" />
                ) : (
                  <AlertTriangle className="w-3.5 h-3.5 text-amber-600 flex-shrink-0 mt-0.5" />
                )}
                <span className="font-medium w-20 flex-shrink-0">{c.name}</span>
                <span className="text-muted-foreground break-all">
                  {!c.configured ? "未配置" : c.ok ? "可用" : c.detail || "不可用"}
                </span>
              </div>
            ))}
            {channels.data && !channels.data.anyAvailable && (
              <p className="text-[11px] text-amber-700 dark:text-amber-300 pt-1">
                一条可用通道都没有 —— 监控会跑不起来，也发不出补货提醒
              </p>
            )}
          </div>
        )}
      </div>

      <Field label="自定义 Webhook 地址（可选）">
        <Input
          value={form.notifyWebhookUrl || ""}
          onChange={(e) => set("notifyWebhookUrl", e.target.value)}
          placeholder="https://your.server/notify 或钉钉/飞书机器人地址"
        />
        <p className="text-[11px] text-muted-foreground mt-1">
          方向是 <b>本程序 → 这个地址</b>。发的是一个 JSON POST，同一条文本同时放进
          <code className="mx-1 px-1 rounded bg-muted">text</code>
          <code className="mr-1 px-1 rounded bg-muted">message</code>
          <code className="mr-1 px-1 rounded bg-muted">text_content.text</code>
          几个字段 —— 不猜你的接收端用哪个协议，钉钉/飞书/Bark/自建都能取到其中一个。
          注意「一键下单」按钮只有 Telegram 有，webhook 收到的是纯文本。
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
          {test.isPending ? "发送中…" : "发一条测试通知"}
        </Button>
        <p className="text-[11px] text-muted-foreground mt-1.5">
          会往所有已配置的通道各发一条。先保存设置再测 —— 测的是已保存的配置，不是输入框里的
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
  const poll = useTelegramPoller();

  // poller 整个对象可能缺(后端还没初始化) —— 那是"没问到状态",不是"停了"。
  // 混在一起会让用户去反复重启一个其实在正常跑的东西。
  const poller = poll.data?.poller;
  const hasToken = poll.data?.hasToken === true;

  return (
    <Section title="Telegram 通知">
      {/* 收 update 只有长轮询一条路。webhook 那条已经删掉了:
          它要公网 HTTPS 域名 + 受信证书,还得把回调端点放进鉴权白名单,
          于是只能靠 secret_token 证明来源 —— 一整套只为解决"入站端点会被伪造"
          这一个问题的东西。没有入站端点,这些连同它们的出错面一起消失了。 */}
      <div className="rounded-2xl border border-border p-4 space-y-2.5 text-[13px]">
        <div className="flex items-center justify-between">
          <h3 className="text-[13px] font-medium flex items-center gap-1.5">
            <Radio className="w-3.5 h-3.5 text-muted-foreground" />
            消息收取（长轮询）
          </h3>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => poll.refetch()}
            disabled={poll.isFetching}
          >
            <RefreshCw className={cn("w-3.5 h-3.5", poll.isFetching && "animate-spin")} />
            刷新
          </Button>
        </div>
        <p className="text-[11px] text-muted-foreground">
          程序主动去 Telegram 拉消息，<b>不需要公网地址和证书</b>，只要这台机器能访问
          api.telegram.org。家宽、NAT 后面、没域名的机器都能用一键下单。
        </p>

        {poll.isPending ? (
          <Skeleton className="h-20 rounded-xl" />
        ) : poll.isError ? (
          <LoadFailedBanner
            title="长轮询状态没读到 —— 下面是空的不代表它停了"
            error={poll.error}
            onRetry={() => poll.refetch()}
          />
        ) : !hasToken ? (
          <p className="text-[12px] text-warning">
            还没保存 Bot Token，收取器不会启动。填好下面的 Token 和 Chat ID 再点「保存设置」。
          </p>
        ) : !poller ? (
          // 后端没带 poller 回来 —— 看不到状态,不是停了
          <p className="text-[12px] text-muted-foreground">
            后端没有返回收取器状态，这里看不出它是不是真的在收消息（一般是后端版本太老或刚启动）。
          </p>
        ) : (
          <>
            <InfoRow
              label="运行状态"
              value={
                poller.running ? (
                  <Chip tone="success">
                    <CheckCircle2 className="w-3 h-3" />
                    运行中
                  </Chip>
                ) : (
                  <Chip tone="danger">
                    <AlertTriangle className="w-3 h-3" />
                    已停止
                  </Chip>
                )
              }
            />
            <InfoRow
              label="最近一次拉取"
              value={
                poller.lastPollAt ? (
                  <span className="font-mono text-[12px]">
                    {new Date(poller.lastPollAt).toLocaleString("zh-CN")}
                  </span>
                ) : (
                  <span className="text-muted-foreground">还没拉到过</span>
                )
              }
            />
            <InfoRow
              label="已确认 update_id"
              value={<span className="font-mono text-[12px]">{poller.offset}</span>}
            />
            {poller.lastError ? (
              <div className="rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2 text-[12px] flex items-start gap-2">
                <AlertTriangle className="w-4 h-4 text-destructive flex-shrink-0 mt-0.5" />
                <div className="min-w-0">
                  <p className="font-semibold text-destructive">上次拉取报错</p>
                  <p className="mt-0.5 break-words">{poller.lastError}</p>
                  {isPollConflict(poller.lastError) && (
                    <p className="mt-1.5 text-destructive">
                      这是<b>同一个 Bot Token 有另一个进程也在收</b>：两边会互相把对方踢下线，
                      表现就是「一键下单」按钮时灵时不灵、消息随机丢。
                      先停掉另一份程序（另一台机器 / 另一个容器 / 本地调试进程），
                      或者给这一份换一个 Bot Token。
                    </p>
                  )}
                </div>
              </div>
            ) : (
              <InfoRow
                label="错误状态"
                value={
                  <Chip tone="success">
                    <CheckCircle2 className="w-3 h-3" />
                    正常
                  </Chip>
                }
              />
            )}
            {!poller.running && (
              <p className="text-[11px] text-destructive">
                收取器没在跑 —— 现在一条命令、一个按钮都收不到。看上面的报错，或者重启程序。
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
  const info = useCacheInfo();
  const clear = useClearCache();
  const sqliteUpdated = info.data?.sqlite?.updatedAtMs
    ? new Date(info.data.sqlite.updatedAtMs).toLocaleString("zh-CN")
    : "从未刷新";
  return (
    <Section title="缓存管理">
      {info.isPending ? (
        <Skeleton className="h-32 rounded-2xl" />
      ) : info.isError ? (
        // 这五行以前会在请求失败时全部渲染成假读数:条数 0、状态"已过期"、
        // "从未刷新"、路径"—"。用户据此去点"清除全部",清的是一份他根本没看清的东西。
        <LoadFailed
          icon={Database}
          title="缓存信息读取失败"
          error={info.error}
          onRetry={() => info.refetch()}
          compact
        />
      ) : (
        <div className="border border-border rounded-2xl p-4 space-y-2.5 text-[13px]">
          <Row label="内存缓存条数" value={info.data?.backend?.serverCount ?? 0} />
          <Row label="内存缓存状态" value={info.data?.backend?.cacheValid ? "有效" : "已过期"} />
          <Row label="SQLite 缓存条数" value={info.data?.sqlite?.serverCount ?? 0} />
          <Row label="SQLite 最近刷新" value={<span className="text-[12px]">{sqliteUpdated}</span>} />
          <Row
            label="数据库位置"
            value={
              <code className="text-[11px] font-mono">
                {info.data?.sqlite?.path || info.data?.storage?.dataDir || "—"}
              </code>
            }
          />
        </div>
      )}
      <p className="text-[11px] text-muted-foreground">
        缓存只指 OVH 服务器目录。订阅 / 队列 / 历史 等业务数据不在此清理范围内。
      </p>
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" onClick={() => clear.mutate("memory")} disabled={clear.isPending}>
          清除内存缓存
        </Button>
        <Button variant="outline" onClick={() => clear.mutate("sqlite")} disabled={clear.isPending}>
          清除 SQLite 缓存
        </Button>
        <Button variant="destructive" onClick={() => clear.mutate("all")} disabled={clear.isPending}>
          清除全部
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
    <Section title="OVH 账户管理">
      <div className="flex items-start justify-between gap-3">
        <p className="text-[12px] text-muted-foreground">
          每个 OVH 账户(凭据)单独保存,抢购队列 / 狙击 / 订阅创建时各自指定账户。删账户会一并清除关联的 queue / history / sniper tasks。
        </p>
        <Button onClick={() => setShowAdd(true)} size="sm" className="flex-shrink-0">
          <Plus className="w-4 h-4" />
          添加账户
        </Button>
      </div>

      {/* 为什么每个账户要有自己的出口 —— 不讲清楚,代理这一栏看起来只是个可选项 */}
      <div className="rounded-xl border border-border bg-secondary/30 px-3 py-2.5 space-y-1.5 text-[11px] leading-relaxed">
        <p className="font-semibold flex items-center gap-1.5">
          <Network className="w-3.5 h-3.5" />
          每个账户可以配自己的出站代理
        </p>
        <p className="text-muted-foreground">
          OVH 的限流按来源 IP 算。多个账户共用一个出口时,一个账户被限流会把其它账户一起拖下水 ——
          而这恰好发生在补货那一刻,也就是唯一要紧的时刻。
        </p>
        <p className="text-muted-foreground">
          代理配错或连不上时,后端<b className="text-warning">不会</b>退回直连,请求直接失败。这是故意的 ——
          悄悄直连的表现是一切正常、隔离却已经没了,而你无从察觉。
        </p>
      </div>

      {/* 健康状态没问到时,下面各卡片"没有告警"并不等于"没问题" */}
      {health.isError && (
        <LoadFailedBanner
          title="代理健康状态读取失败 —— 各账户有没有因为代理故障被暂停,现在是未知"
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
              ? "配了代理的账户和别人撞在同一个出口 IP 上 —— 隔离没生效"
              : "这些账户测出来是同一个出口 IP"}
          </p>
          {collisions.map((c) => (
            <p key={c.ip} className="text-muted-foreground">
              <span className="font-mono font-semibold text-foreground">{c.ip}</span> ←{" "}
              {c.xs.map((x) => `${x.name}${x.hasProxy ? "(配了代理)" : "(直连)"}`).join("、")}
            </p>
          ))}
          <p className="text-muted-foreground">
            它们在 OVH 眼里是同一个来源,限流会互相拖累 —— 一个被限,其它一起被限。
            都是直连的话这是正常的(直连本来就共用一个出口);配了代理却还撞在一起,说明那个代理没生效,
            去编辑里确认代理地址保存上了、再测一次。
          </p>
        </div>
      )}

      {accounts.isError ? (
        // 说成"还没有账户"会让用户重新粘一遍 OVH 三件套凭据,
        // 而后端其实存着好好的 —— 重复添加只会多出一个重名账户。
        <LoadFailed
          icon={Globe}
          title="账户列表读取失败"
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
            还没有账户,点右上角"添加账户"创建一个
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
              默认
            </Chip>
          )}
        </div>
        <div className="text-[11px] text-muted-foreground flex items-center gap-2 flex-wrap font-mono">
          <span>{acc.endpoint}</span>
          <span>·</span>
          <span>{acc.iam}</span>
          <span>·</span>
          <span>建于 {new Date(acc.createdAt).toLocaleDateString("zh-CN")}</span>
        </div>
        {/* 出站配置 + 最近测到的出口 IP。IP 放在这里就是为了几个账户之间横向比对 */}
        <div className="flex items-center gap-1.5 flex-wrap mt-1.5">
          {acc.proxyUrl ? (
            <Chip tone="info">
              <Network className="w-3 h-3" />
              <span className="font-mono">{acc.proxyUrl}</span>
            </Chip>
          ) : (
            <Chip>直连</Chip>
          )}
          <Chip>
            <Fingerprint className="w-3 h-3" />
            {acc.fingerprint || "default"}
          </Chip>
          {lastTest && lastTest.success ? (
            <Chip tone="success" title={`测于 ${new Date(lastTest.testedAt).toLocaleString("zh-CN")}`}>
              出口 <span className="font-mono font-semibold">{lastTest.egressIP}</span>
            </Chip>
          ) : lastTest ? (
            <Chip tone="danger" title={lastTest.error}>出口测试失败 · 经由{lastTest.via}</Chip>
          ) : (
            <span className="text-[11px] text-muted-foreground">出口 IP 未测(编辑里点「测试出口 IP」)</span>
          )}
        </div>
      </div>
      <div className="flex items-center gap-2 flex-shrink-0">
        {/* 凭据对不对、出口是哪个 IP,都不回答"这条链路快不快" —— 而抢购输赢就在这上面 */}
        <Button
          variant="outline"
          size="sm"
          onClick={() => setChecking(true)}
          title="实测这个账户到 OVH 的连通性与延迟"
        >
          <Activity className={cn("w-3.5 h-3.5", check.isPending && "animate-pulse")} />
          {check.isPending ? "检测中…" : "链路检测"}
        </Button>
        <Button variant="ghost" size="icon" onClick={() => verify.mutate(acc.id)} disabled={verify.isPending} title="重新验证凭据">
          <RotateCw className={cn("w-4 h-4", verify.isPending && "animate-spin")} />
        </Button>
        {!acc.isDefault && (
          <Button variant="ghost" size="icon" onClick={() => setDefault.mutate(acc.id)} disabled={setDefault.isPending} title="设为默认">
            <Star className="w-4 h-4" />
          </Button>
        )}
        <Button variant="ghost" size="icon" onClick={onEdit} title="编辑">
          <Pencil className="w-4 h-4" />
        </Button>
        <Button variant="ghost" size="icon" onClick={() => setConfirming(true)} title="删除" className="text-destructive hover:text-destructive">
          <Trash2 className="w-4 h-4" />
        </Button>
      </div>
      </div>

      {/* 代理跳闸:这个账户的活儿已经被停了,必须用 destructive 说清楚,并说明不会自动恢复 */}
      {health?.tripped ? (
        <div className="rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2.5 text-[11px] space-y-1">
          <p className="font-semibold text-destructive flex items-center gap-1.5">
            <ShieldAlert className="w-3.5 h-3.5" />
            出站代理连续失败 {health.fails} 次 —— 该账户的抢购任务已被暂停
          </p>
          <p className="text-muted-foreground">
            订阅的自动下单也一并关掉了(订阅本身还在,补货通知照常发)。
            {health.trippedAt ? ` 停于 ${new Date(health.trippedAt).toLocaleString("zh-CN")}。` : ""}
          </p>
          <p className="text-muted-foreground">
            代理修好后任务<b>不会</b>自动恢复:去队列页把被暂停的任务改回运行,订阅的自动下单也要重新打开。
          </p>
        </div>
      ) : health && health.fails > 0 ? (
        <p className="text-[11px] text-warning border border-warning/40 bg-warning/5 rounded-xl px-3 py-2">
          ⚠ 出站代理最近连续失败 {health.fails} 次
          {health.lastFailAt ? `(最后一次 ${new Date(health.lastFailAt).toLocaleTimeString("zh-CN")})` : ""},
          还没到停任务的阈值。再连着失败下去,这个账户的抢购任务就会被暂停 —— 现在去编辑里点一下「测试出口 IP」看代理还通不通。
        </p>
      ) : healthUnknown && acc.proxyUrl ? (
        <p className="text-[11px] text-muted-foreground">
          代理健康状态未问到,这里没有告警不代表代理正常。
        </p>
      ) : null}

      {subsidiaryWarning && (
        <p className="text-[11px] text-warning border border-warning/40 bg-warning/5 rounded-xl px-3 py-2">
          ⚠ 子公司配置与 OVH 实际归属不一致：{subsidiaryWarning}
        </p>
      )}

      <LinkCheckDialog acc={acc} open={checking} onOpenChange={setChecking} check={check} />

      <Dialog open={confirming} onOpenChange={setConfirming}>
        <DialogContent className="w-[95vw] sm:w-full sm:max-w-md">
          <DialogHeader>
            <DialogTitle>确认删除账户 {acc.name}?</DialogTitle>
            <DialogDescription className="text-destructive">
              将级联删除该账户的所有 queue 任务、history 历史、config_sniper 任务。
              监控订阅的 auto_order 引用此账户的会清空。该操作不可逆。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirming(false)}>取消</Button>
            <Button
              variant="destructive"
              onClick={async () => {
                await del.mutateAsync(acc.id);
                setConfirming(false);
              }}
              disabled={del.isPending}
            >
              确认删除
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
  fast: "正常",
  slow: "偏慢",
  bad: "太慢",
};

/**
 * 一个探测目标一行:● 名称 …… 最小 XXXms / 平均 XXXms  HTTP 200
 *
 * 最小值按档位着色 —— 一屏里要能一眼扫出哪条链路拖后腿,而不是回头自己去比数字。
 */
function ProbeRow({ t }: { t: ProxyProbeTarget }) {
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
            最小 <b className={g ? latencyText[g.level] : undefined}>{t.minMs}ms</b>
            <span className="text-muted-foreground"> / 平均 {t.avgMs}ms</span>
          </span>
        ) : (
          <span className="text-[11px] text-destructive whitespace-nowrap">没拿到响应</span>
        )}
        {t.status ? (
          <Chip className="whitespace-nowrap">HTTP {t.status}</Chip>
        ) : null}
      </div>
      {!t.ok && t.error && <p className="pl-4 text-[11px] text-destructive break-all">{t.error}</p>}
      {/* 通了却还带着 error:3 次采样里有失败的。既不是"通"也不是"不通",单独说清楚 */}
      {t.ok && t.error && (
        <p className="pl-4 text-[11px] text-warning break-all">
          3 次采样里有失败的:{t.error} —— 这条链路会偶发抽风,补货那一刻正好撞上就没了。
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
  const record = useLastProxyCheck(acc.id).data;
  // 三种状态必须分开:没测过 / 检测没做成(我们没问到) / 测完了(才有资格谈通不通、快不快)
  const done = record && record.success ? record : null;
  const run = () => check.mutate(acc.id);

  const targets = done?.targets || [];
  const down = targets.filter((t) => !t.ok);
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
            链路检测 · {acc.name}
          </DialogTitle>
          <DialogDescription>
            用这个账户<b>已保存</b>的出站配置实测到 OVH 的连通性与延迟,每个目标采样 3 次。
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
                {done ? `大区 ${done.region}` : acc.zone}
              </span>
              {(done ? done.usingProxy : !!acc.proxyUrl) ? (
                <Chip tone="info">
                  <Network className="w-3 h-3" />
                  <span className="font-mono">{(done ? done.proxy : acc.proxyUrl) || "代理"}</span>
                </Chip>
              ) : (
                <Chip>直连</Chip>
              )}
              <Chip>
                <Fingerprint className="w-3 h-3" />
                {done ? done.fingerprint : acc.fingerprint || "default"}
              </Chip>
            </div>
            <p className="text-[11px] text-muted-foreground leading-relaxed">
              {done
                ? `只测这个账户真正会打的那个大区(${done.region})。它根本不会去访问另外两个区,把那些也列出来只会让人对着不相干的红点发愁。`
                : "走的是这个账户已保存的出站配置,和真实下单同一条链路。"}
            </p>
            {staleCfg && (
              <p className="text-[11px] text-warning">
                ⚠ 下面这份结果是用{" "}
                <span className="font-mono">{done?.proxy || "直连"}</span>{" "}
                跑的,跟这个账户现在的配置对不上了 —— 重新检测一次再下结论。
              </p>
            )}
          </div>

          {/* 请求没发出去 ≠ 链路不通:前者是我们什么都没测到,绝不能画成红点 */}
          {check.isError && (
            <LoadFailedBanner
              title="链路检测请求没发出去 —— 这不代表链路有问题,是我们没问到"
              error={check.error}
              onRetry={run}
            />
          )}

          {check.isPending ? (
            <div className="space-y-2">
              <p className="text-[11px] text-muted-foreground flex items-center gap-1.5">
                <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                正在检测:每个目标真打 3 次再取最小 / 平均,要几秒。
              </p>
              <Skeleton className="h-20 rounded-2xl" />
              <Skeleton className="h-24 rounded-2xl" />
            </div>
          ) : record && !record.success ? (
            <div className="rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2.5 space-y-1 text-[11px]">
              <p className="font-semibold text-destructive flex items-center gap-1.5">
                <AlertTriangle className="w-3.5 h-3.5" />
                后端没做成这次检测
              </p>
              <p className="text-muted-foreground break-all">{record.error}</p>
              <p className="text-muted-foreground">
                这不是"链路不通" —— 检测压根没跑起来,链路是好是坏现在仍然未知。
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
                <p className="text-[11px] text-muted-foreground">这个账户实际用的出口 IP</p>
                {done.egressIP ? (
                  <>
                    <p className="text-2xl font-mono font-semibold tracking-tight break-all">{done.egressIP}</p>
                    <p className="text-[11px] text-muted-foreground">
                      拿它跟别的账户比一比:两个账户测出同一个 IP,OVH 就把它们算作同一个来源,限流互相拖累。
                    </p>
                  </>
                ) : (
                  <>
                    <p className="text-[13px] font-semibold text-destructive flex items-center gap-1.5">
                      <AlertTriangle className="w-3.5 h-3.5" />
                      没查到出口 IP
                    </p>
                    <p className="text-[11px] text-destructive break-all">{done.egressError || "后端没给原因"}</p>
                    <p className="text-[11px] text-muted-foreground">
                      查出口 IP 用的是第三方站点,它自己挂掉不代表到 OVH 的链路有问题 —— 以下面的目标为准。
                    </p>
                  </>
                )}
                {done.warning && <p className="text-[11px] text-warning">⚠ {done.warning}</p>}
              </div>

              {/* 目标列表 + 结论。数字必须配结论:用户没法凭 620ms 这个数自己判断该不该换代理 */}
              <div className="space-y-2">
                <p className="text-[12px] font-semibold">到 OVH 的连通性与延迟</p>
                {targets.length === 0 ? (
                  <p className="text-[11px] text-muted-foreground">
                    这次后端一个目标都没返回 —— 链路好坏无从判断,重测一次。
                  </p>
                ) : (
                  <div className="space-y-1.5">
                    {targets.map((t) => (
                      <ProbeRow key={t.name + t.url} t={t} />
                    ))}
                  </div>
                )}

                {down.length > 0 && (
                  <div className="rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2 text-[11px] space-y-0.5">
                    <p className="font-semibold text-destructive">
                      {down.length} 个目标连响应都没拿到 —— 这个账户现在下不出单
                    </p>
                    <p className="text-muted-foreground">
                      {done.usingProxy
                        ? "配了代理就不会退回直连,链路断着等于该账户此刻一单也下不出去。修代理,或者改回直连。"
                        : "直连都打不到 OVH,说明是这台机器本身出不去网,跟代理无关。"}
                    </p>
                  </div>
                )}

                {grade && (
                  <div className={cn("rounded-xl border px-3 py-2 text-[11px] space-y-0.5", latencyBox[grade.level])}>
                    <p className={cn("font-semibold", latencyText[grade.level])}>
                      最慢的一条 {worst}ms · {latencyLabel[grade.level]}
                    </p>
                    <p className="text-muted-foreground">{grade.note}</p>
                  </div>
                )}

                {/* 抖动单独说:平均值本身不显眼,但"不定时慢一拍"恰恰是抢购最怕的 */}
                {jittery.length > 0 && (
                  <div className="rounded-xl border border-warning/40 bg-warning/5 px-3 py-2 text-[11px] space-y-0.5">
                    <p className="font-semibold text-warning">
                      抖动大:{jittery.map((t) => t.name).join("、")}
                    </p>
                    <p className="text-muted-foreground">
                      平均耗时是最小耗时的一倍以上 —— 这条链路会不定时慢一拍,而那一拍就决定抢不抢得到。
                      平时看着快没有用,补货那一刻撞上就没了。
                    </p>
                  </div>
                )}
              </div>

              {/* 读法:不写清楚的话,一个 404 会被当成"代理坏了"而去换一个好好的代理 */}
              <div className="rounded-xl border border-border bg-secondary/30 px-3 py-2.5 space-y-1 text-[11px] leading-relaxed text-muted-foreground">
                <p className="font-semibold text-foreground">怎么看这几行</p>
                <p>
                  拿到<b>任何</b> HTTP 响应就算连通。上面的 HTTP 404 / 302 只说明那个路径不存在或者要跳转,
                  <b>不代表代理有问题</b>。真正的不通是连响应都没有:红点 + 一条错误信息。
                </p>
                <p>
                  延迟取 3 次采样的最小值和平均值。最小值是这条链路的最好情况,平均值比最小值大一倍以上就是抖动大。
                </p>
              </div>
            </>
          ) : !check.isError ? (
            <div className="rounded-xl border border-border px-3 py-5 text-center space-y-1">
              <p className="text-[12px] font-medium">还没检测过这个账户的链路</p>
              <p className="text-[11px] text-muted-foreground leading-relaxed">
                检测会真的去打 OVH,所以打开弹窗不会自动开始。点下面的「开始检测」,
                结果会留着 —— 下次打开还看得到这一次的数字和时间。
              </p>
            </div>
          ) : null}
        </div>

        <DialogFooter className="flex-wrap gap-2 space-x-0 sm:justify-between items-center">
          <span className="text-[11px] text-muted-foreground">
            {check.isPending
              ? "检测中…"
              : record
                ? `上次检测 ${proxyCheckTime(record).toLocaleString("zh-CN")}`
                : "尚未检测"}
          </span>
          <div className="flex items-center gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              关闭
            </Button>
            <Button onClick={run} disabled={check.isPending}>
              <RefreshCw className={cn("w-3.5 h-3.5", check.isPending && "animate-spin")} />
              {check.isPending ? "检测中…" : record ? "立即重新检测" : "开始检测"}
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
 */
function proxyInputError(raw: string): string {
  const v = raw.trim();
  if (!v) return "";
  let u: URL;
  try {
    u = new URL(v);
  } catch {
    return "解析不了。格式:socks5://用户名:密码@主机:端口";
  }
  const scheme = u.protocol.replace(":", "").toLowerCase();
  if (!["http", "https", "socks5", "socks5h"].includes(scheme)) {
    return `不支持的协议 ${scheme}:只支持 http / https / socks5 / socks5h`;
  }
  if (!u.hostname) return "缺少主机名";
  if (!u.port) return "缺少端口 —— 必须显式写出来,例如 :1080";
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
  if (pending) return <Skeleton className="h-24 rounded-2xl" />;
  if (requestError) {
    return (
      <LoadFailedBanner
        title="出口测试请求没发出去(这不代表代理有问题)"
        error={requestError}
        onRetry={onRetry}
      />
    );
  }
  if (!record) return null;
  const at = new Date(record.testedAt).toLocaleString("zh-CN");

  if (!record.success) {
    return (
      <div className="rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2.5 space-y-1 text-[11px]">
        <p className="font-semibold text-destructive flex items-center gap-1.5">
          <AlertTriangle className="w-3.5 h-3.5" />
          出口测试失败 · 经由{record.via}
        </p>
        <p className="text-muted-foreground break-all">{record.error}</p>
        <p className="text-muted-foreground">
          {record.usingProxy
            ? "配了代理就不会退回直连 —— 这条失败等于该账户此刻一单也下不出去。修好代理,或者改回直连。"
            : "直连都失败,说明是这台机器本身出不去网,跟代理无关。"}
        </p>
        <p className="text-muted-foreground/70">{at} 测</p>
      </div>
    );
  }

  return (
    <div className="rounded-xl border border-success/40 bg-success/5 px-3 py-2.5 space-y-1.5">
      <p className="text-[11px] text-muted-foreground">这个账户实际用的出口 IP</p>
      <p className="text-2xl font-mono font-semibold tracking-tight break-all">{record.egressIP}</p>
      <p className="text-[11px] text-muted-foreground">
        经由 {record.usingProxy ? <span className="font-mono">{record.proxy || "代理"}</span> : "直连"}
        {" · "}指纹 {record.fingerprint}
        {" · "}{at} 测
      </p>
      {expectProxy && !record.usingProxy && (
        <p className="text-[11px] text-destructive border border-destructive/40 bg-destructive/5 rounded-lg px-2 py-1.5">
          这个账户配了代理,但后端这次是按<b>直连</b>发出去的 —— 上面这个 IP 是本机出口,代理没保存上。
          回到上面重新填一次代理再保存。
        </p>
      )}
      {!record.usingProxy && !expectProxy && (
        <p className="text-[11px] text-muted-foreground">
          这是直连出口:所有没配代理的账户都是这个 IP,OVH 会把它们算作同一个来源。
        </p>
      )}
      {record.warning && <p className="text-[11px] text-warning">⚠ {record.warning}</p>}
      <p className="text-[11px] text-muted-foreground">
        拿它跟别的账户比一比:两个账户测出同一个 IP,就说明隔离没生效。
      </p>
    </div>
  );
}

function AccountDialog({ acc, onClose }: { acc?: OVHAccount; onClose: () => void }) {
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
  const proxyErr = clearProxy ? "" : proxyInputError(form.proxyUrl);
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
          <DialogTitle>{isEdit ? `编辑账户 ${acc!.name}` : "添加 OVH 账户"}</DialogTitle>
          <DialogDescription>填三个 OVH 密钥 + 选子公司,保存时会自动调 /me 验证凭据。</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-2 max-h-[65vh] overflow-y-auto -mx-6 px-6">
          <Field label="账户名称 *">
            <Input value={form.name} onChange={(e) => set("name", e.target.value)} placeholder="主号 / 小号 A" autoFocus />
            {isEdit && (
              <p className="text-[11px] text-muted-foreground mt-1">
                下面三个凭据留空即保持不变。出于安全考虑，后端不再把已保存的凭据发回浏览器
                （只显示掩码），要更换请重新填写完整值。
              </p>
            )}
          </Field>
          <Field label="APP KEY *">
            <Input type="password" value={form.appKey} onChange={(e) => set("appKey", e.target.value)}
              placeholder={isEdit ? (acc?.appKey || "留空 = 不修改") : "xxxxxxxxxxxxxxxx"} />
          </Field>
          <Field label="APP SECRET *">
            <Input type="password" value={form.appSecret} onChange={(e) => set("appSecret", e.target.value)}
              placeholder={isEdit ? (acc?.appSecret || "留空 = 不修改") : "xxxxxxxxxxxxxxxx"} />
          </Field>
          <Field label="CONSUMER KEY *">
            <Input type="password" value={form.consumerKey} onChange={(e) => set("consumerKey", e.target.value)}
              placeholder={isEdit ? (acc?.consumerKey || "留空 = 不修改") : "xxxxxxxxxxxxxxxx"} />
          </Field>
          <Field
            label="OVH 子公司 (Zone)"
            hint={`Endpoint ${endpointForZone(form.zone)} · IAM go-ovh-${form.zone.toLowerCase()} 由子公司自动派生`}
          >
            <Select value={form.zone} onValueChange={(v) => set("zone", v)}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                {OVH_SUBSIDIARIES.map((s) => (
                  <SelectItem key={s.code} value={s.code}>
                    {s.code} · {s.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          {/* ── 出站配置 ───────────────────────────────────────────────── */}
          <div className="border-t border-border pt-4 space-y-4">
            <div>
              <p className="text-[13px] font-semibold flex items-center gap-1.5">
                <Network className="w-3.5 h-3.5" />
                出站代理与指纹
              </p>
              <p className="text-[11px] text-muted-foreground mt-1 leading-relaxed">
                OVH 的限流按来源 IP 算,几个账户共用一个出口时会互相拖累,而这恰好发生在补货那一刻。
                给这个账户配一个自己的出口,它就不会被别的账户连累。
              </p>
            </div>

            <Field label="出站代理地址">
              <Input
                value={form.proxyUrl}
                onChange={(e) => set("proxyUrl", e.target.value)}
                disabled={clearProxy}
                placeholder={
                  clearProxy
                    ? "已标记改回直连"
                    : saved
                      ? "留空 = 保持不变"
                      : "socks5://user:pass@1.2.3.4:1080(留空 = 直连)"
                }
                className="font-mono"
              />
              {proxyErr && <p className="text-[11px] text-destructive mt-1">代理地址不合法:{proxyErr}</p>}

              {/* 已落库的账户:回显的是打过码的地址,绝不能预填进输入框;清代理要有明确动作 */}
              {saved && (
                <div className="mt-2 space-y-1.5">
                  <p className="text-[11px] text-muted-foreground">
                    当前:
                    {saved?.proxyUrl ? (
                      <code className="ml-1 font-mono">{saved.proxyUrl}</code>
                    ) : (
                      <span className="ml-1">直连(没配代理)</span>
                    )}
                    {saved?.proxyUrl ? "(密码已打码,所以这里不预填 —— 把 *** 原样提交会把它存成真密码)" : ""}
                  </p>
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-[11px] text-muted-foreground">留空 = 保持不变;要摘掉代理:</span>
                    {clearProxy ? (
                      <Button variant="outline" size="sm" onClick={() => setClearProxy(false)}>
                        撤销「改回直连」
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
                        改回直连
                      </Button>
                    )}
                  </div>
                  {clearProxy && (
                    <p className="text-[11px] text-warning">
                      保存后这个账户会清掉代理、改成直连出口 —— 它将和其它直连账户共用同一个出口 IP。
                    </p>
                  )}
                </div>
              )}

              <div className="mt-2 space-y-1 text-[11px] leading-relaxed">
                <p className="text-muted-foreground">
                  支持 <code className="font-mono">http://</code> <code className="font-mono">https://</code>{" "}
                  <code className="font-mono">socks5://</code> <code className="font-mono">socks5h://</code>,
                  <b>必须带端口</b>(例如 <code className="font-mono">socks5://user:pass@1.2.3.4:1080</code>)。
                  {saved ? "留空 = 保持不变(要摘掉代理用上面的「改回直连」)。" : "留空 = 直连。"}
                </p>
                <p className="text-warning">
                  代理配错或连不上时<b>不会</b>退回直连,该账户的请求直接失败;连续失败到阈值后,后端会
                  <b>暂停这个账户的抢购任务</b>并关掉订阅的自动下单(修好也不自动恢复)。
                  这是故意的 —— 悄悄直连的表现是一切正常、隔离却已经没了。
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
                  {test.isPending ? "测试中…" : "测试出口 IP"}
                </Button>
                <span className="text-[11px] text-muted-foreground">
                  {!saved
                    ? "新账户要先保存才能测 —— 测的是已保存的配置。用下面的「保存并测试出口」一步到位。"
                    : outboundDirty
                      ? "上面的代理/指纹改了还没保存,现在测到的是旧配置的出口 —— 用下面的「保存并测试出口」。"
                      : "测的是这个账户已保存的配置,走的和真实下单同一条出站链路。"}
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

            <Field label="出站指纹">
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
                    title="指纹配置清单读取失败 —— 暂时只能保持原值"
                    error={proxyStatus.error}
                    onRetry={() => proxyStatus.refetch()}
                  />
                </div>
              )}
              {proxyStatus.isPending && (
                <p className="text-[11px] text-muted-foreground mt-1">正在读可选的指纹配置…</p>
              )}
              <p className="text-[11px] text-muted-foreground mt-2 leading-relaxed">
                <b>这不是完整的浏览器指纹模拟。</b>Go 标准库不允许控制 JA3 的主要构成要素
                (套件顺序被忽略、TLS 1.3 套件不可配、扩展顺序固定),所以这个选项改的只是
                TLS 版本区间、ALPN / 是否走 h2、User-Agent 这类。选 <code className="font-mono">chrome-like</code>{" "}
                <b>不等于</b> Chrome 的 JA3 —— 要做到那个得换 uTLS 重写握手,这里做不到。
              </p>
            </Field>
          </div>

          {/* 申请密钥的说明放在这里而不是只放首次进入的弹窗:
              日常加号 / 换号都走这个对话框,而"去哪申请、申请错站点会怎样"恰恰是
              这时候最容易踩的坑。链接必须跟着上面选的子公司走 —— 三站的 token 互不通用。 */}
          <div className="rounded-xl border border-border bg-secondary/30 px-3 py-2.5 space-y-1.5">
            <p className="text-[11px] font-semibold">还没有密钥?</p>
            <p className="text-[11px] text-muted-foreground leading-relaxed">
              去
              <a
                href={`${apiBaseUrlForEndpoint(endpointForZone(form.zone))}/createToken/`}
                target="_blank"
                rel="noreferrer"
                className="underline mx-1 text-primary"
              >
                {apiBaseUrlForEndpoint(endpointForZone(form.zone)).replace("https://", "")}/createToken
              </a>
              申请。<b>{form.zone}</b> 属于这个站点,
              <span className="text-warning">在别的站点申请的密钥登不进去</span>(三站互不通用)。
            </p>
            <p className="text-[11px] text-muted-foreground leading-relaxed">
              权限最省事是四条全给:
              <code className="mx-1 px-1 py-0.5 rounded bg-background text-[10px]">GET POST PUT DELETE</code>
              各配 <code className="px-1 py-0.5 rounded bg-background text-[10px]">/*</code>；
              有效期选 <b>Unlimited</b> —— 到期后抢购和监控会静默失效。
            </p>
          </div>
        </div>
        {/* 三个按钮在窄屏上要能换行,否则「保存并验证」会被挤出对话框 */}
        <DialogFooter className="flex-wrap gap-2 space-x-0">
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button variant="outline" onClick={saveAndTest} disabled={!canSubmit || busy}>
            <Radar className="w-3.5 h-3.5" />
            {busy ? "处理中…" : "保存并测试出口"}
          </Button>
          <Button onClick={submit} disabled={!canSubmit || busy}>
            {(create.isPending || update.isPending) ? "保存中…" : "保存并验证"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
