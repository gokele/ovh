import { createFileRoute } from "@tanstack/react-router";
import { Settings as SettingsIcon, KeyRound, Globe, Send, Database, Save, Webhook, AlertTriangle, CheckCircle2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/common/Skeleton";
import { Chip } from "@/components/common/Chip";
import {
  useSettings,
  useSaveSettings,
  useCacheInfo,
  useClearCache,
  useTelegramWebhookInfo,
  type SettingsConfig,
} from "@/hooks/use-settings";
import { getApiSecretKey, setApiSecretKey } from "@/lib/api";
import { cn } from "@/lib/utils";
import { OVH_SUBSIDIARIES } from "@/lib/ovh-subsidiaries";

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
  { id: "ovh", icon: Globe, label: "OVH API" },
  { id: "telegram", icon: Send, label: "Telegram" },
  { id: "cache", icon: Database, label: "缓存管理" },
] as const;

function SettingsPage() {
  const cfg = useSettings();
  const save = useSaveSettings();
  const [active, setActive] = useState<typeof SECTIONS[number]["id"]>("password");
  const [form, setForm] = useState<SettingsConfig>({});
  const [apiKey, setApiKey] = useState("");

  useEffect(() => {
    if (cfg.data) setForm(cfg.data);
  }, [cfg.data]);

  useEffect(() => {
    setApiKey(getApiSecretKey() || "");
  }, []);

  const set = (k: keyof SettingsConfig, v: string) => setForm((prev) => ({ ...prev, [k]: v }));

  const onSave = () => {
    if (apiKey) setApiSecretKey(apiKey);
    // 提交前根据 zone 自动同步 endpoint，避免两者不一致
    const zone = form.zone || "IE";
    save.mutate({ ...form, zone, endpoint: endpointForZone(zone) });
  };

  return (
    <div className="space-y-6">
      <PageHeader
        icon={SettingsIcon}
        title="API 设置"
        description="配置 OVH API 和通知设置"
        action={
          <Button onClick={onSave} disabled={save.isPending}>
            <Save className="w-4 h-4" />
            {save.isPending ? "保存中..." : "保存设置"}
          </Button>
        }
      />

      <div className="grid grid-cols-1 lg:grid-cols-[200px_1fr] gap-4">
        {/* 左 sub-nav */}
        <nav className="space-y-1">
          {SECTIONS.map((s) => {
            const Icon = s.icon;
            const a = active === s.id;
            return (
              <button
                key={s.id}
                type="button"
                onClick={() => setActive(s.id)}
                className={cn(
                  "w-full flex items-center gap-2 px-3 py-2 rounded-md text-[13px] transition-colors border-l-2",
                  a ? "bg-secondary text-foreground font-medium border-l-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground border-l-transparent"
                )}
              >
                <Icon className="w-4 h-4" />
                {s.label}
              </button>
            );
          })}
        </nav>

        {/* 右内容 */}
        <Card>
          <CardContent className="p-6">
            {cfg.isPending ? (
              <Skeleton className="h-64 rounded-2xl" />
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
            ) : active === "ovh" ? (
              <Section title="OVH API 凭据">
                <Field label="APP KEY">
                  <Input type="password" value={form.appKey || ""} onChange={(e) => set("appKey", e.target.value)} placeholder="xxxxxxxxxxxxxxxx" />
                </Field>
                <Field label="APP SECRET">
                  <Input type="password" value={form.appSecret || ""} onChange={(e) => set("appSecret", e.target.value)} placeholder="xxxxxxxxxxxxxxxx" />
                </Field>
                <Field label="CONSUMER KEY">
                  <Input type="password" value={form.consumerKey || ""} onChange={(e) => set("consumerKey", e.target.value)} placeholder="xxxxxxxxxxxxxxxx" />
                </Field>
                <Field
                  label="OVH 子公司 (Zone)"
                  hint={`Endpoint ${endpointForZone(form.zone || "IE")} · IAM go-ovh-${(form.zone || "IE").toLowerCase()} 由子公司自动派生`}
                >
                  <Select value={form.zone || "IE"} onValueChange={(v) => set("zone", v)}>
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
              </Section>
            ) : active === "telegram" ? (
              <TelegramSection form={form} set={set} />
            ) : (
              <CacheSection />
            )}
          </CardContent>
        </Card>
      </div>
    </div>
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

function TelegramSection({
  form,
  set,
}: {
  form: SettingsConfig;
  set: (k: keyof SettingsConfig, v: string) => void;
}) {
  const webhook = useTelegramWebhookInfo();
  const onFetch = () => {
    if (!form.tgToken) {
      toast.error("请先填写并保存 Bot Token");
      return;
    }
    webhook.refetch();
  };
  return (
    <Section title="Telegram 通知">
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
      <Field label="Webhook URL（可选）">
        <Input
          value={form.webhookUrl || ""}
          onChange={(e) => set("webhookUrl", e.target.value)}
          placeholder="https://your.domain/webhook"
        />
      </Field>

      <div className="pt-2">
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-[13px] font-medium flex items-center gap-1.5">
            <Webhook className="w-3.5 h-3.5 text-muted-foreground" />
            Webhook 信息
          </h3>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onFetch}
            disabled={webhook.isFetching}
          >
            <Webhook className={cn("w-3.5 h-3.5", webhook.isFetching && "animate-pulse")} />
            {webhook.isFetching ? "查询中..." : "查看 webhook 信息"}
          </Button>
        </div>

        {webhook.isError ? (
          <div className="border border-border rounded-2xl p-4 text-[12px] text-destructive flex items-start gap-2">
            <AlertTriangle className="w-4 h-4 flex-shrink-0 mt-0.5" />
            <span>{(webhook.error as Error)?.message || "获取 webhook 信息失败"}</span>
          </div>
        ) : webhook.data ? (
          <div className="border border-border rounded-2xl p-4 space-y-2 text-[12px]">
            <InfoRow
              label="URL"
              value={
                webhook.data.url ? (
                  <code className="font-mono break-all text-foreground">{webhook.data.url}</code>
                ) : (
                  <Chip tone="warning">未设置</Chip>
                )
              }
            />
            <InfoRow
              label="待处理更新"
              value={
                <span className="font-mono">
                  {webhook.data.pending_update_count ?? 0}
                </span>
              }
            />
            {webhook.data.ip_address && (
              <InfoRow
                label="IP 地址"
                value={<code className="font-mono">{webhook.data.ip_address}</code>}
              />
            )}
            {webhook.data.max_connections != null && (
              <InfoRow
                label="最大连接数"
                value={<span className="font-mono">{webhook.data.max_connections}</span>}
              />
            )}
            {webhook.data.last_error_date ? (
              <InfoRow
                label="上次错误"
                value={
                  <div className="text-right">
                    <Chip tone="danger">
                      <AlertTriangle className="w-3 h-3" />
                      {new Date(webhook.data.last_error_date * 1000).toLocaleString("zh-CN")}
                    </Chip>
                    {webhook.data.last_error_message && (
                      <p className="mt-1 text-destructive break-words max-w-[280px]">
                        {webhook.data.last_error_message}
                      </p>
                    )}
                  </div>
                }
              />
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
          </div>
        ) : (
          <p className="text-[12px] text-muted-foreground">
            点击右上角按钮查询当前 Telegram Bot 的 webhook 状态
          </p>
        )}
      </div>
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
