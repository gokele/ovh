import { createFileRoute } from "@tanstack/react-router";
import { Settings as SettingsIcon, KeyRound, Globe, Send, Database, Save, Webhook, AlertTriangle, CheckCircle2, Plus, Star, RotateCw, Trash2, Pencil } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/common/Skeleton";
import { Chip } from "@/components/common/Chip";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
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
import {
  useAccounts,
  useCreateAccount,
  useUpdateAccount,
  useDeleteAccount,
  useSetDefaultAccount,
  useVerifyAccount,
  accountChipColor,
  type OVHAccount,
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
        {/* sub-nav:桌面竖向左栏,手机横向滚动 tab */}
        <nav className="lg:space-y-1 flex lg:flex-col overflow-x-auto lg:overflow-visible gap-1 lg:gap-0 -mx-3 px-3 lg:mx-0 lg:px-0">
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

        {/* 右内容 */}
        <Card>
          <CardContent className="p-4 sm:p-6">
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
            ) : active === "accounts" ? (
              <AccountsSection />
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

// ─── 账户管理 ───────────────────────────────────────────────────────────────

function AccountsSection() {
  const accounts = useAccounts();
  const [showAdd, setShowAdd] = useState(false);
  const [editAcc, setEditAcc] = useState<OVHAccount | null>(null);
  const list = accounts.data || [];

  return (
    <Section title="OVH 账户管理">
      <div className="flex items-center justify-between">
        <p className="text-[12px] text-muted-foreground">
          每个 OVH 账户(凭据)单独保存,抢购队列 / 狙击 / 订阅创建时各自指定账户。删账户会一并清除关联的 queue / history / sniper tasks。
        </p>
        <Button onClick={() => setShowAdd(true)} size="sm">
          <Plus className="w-4 h-4" />
          添加账户
        </Button>
      </div>

      {accounts.isPending ? (
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
            <AccountCard key={a.id} acc={a} onEdit={() => setEditAcc(a)} />
          ))}
        </div>
      )}

      {showAdd && <AccountDialog onClose={() => setShowAdd(false)} />}
      {editAcc && <AccountDialog acc={editAcc} onClose={() => setEditAcc(null)} />}
    </Section>
  );
}

function AccountCard({ acc, onEdit }: { acc: OVHAccount; onEdit: () => void }) {
  const setDefault = useSetDefaultAccount();
  const del = useDeleteAccount();
  const verify = useVerifyAccount();
  const [confirming, setConfirming] = useState(false);

  return (
    <div className="border border-border rounded-2xl p-4 flex flex-col sm:flex-row sm:items-center gap-3">
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
      </div>
      <div className="flex items-center gap-2 flex-shrink-0">
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

function AccountDialog({ acc, onClose }: { acc?: OVHAccount; onClose: () => void }) {
  const create = useCreateAccount();
  const update = useUpdateAccount();
  const isEdit = !!acc;
  const [form, setForm] = useState({
    name: acc?.name || "",
    appKey: acc?.appKey || "",
    appSecret: acc?.appSecret || "",
    consumerKey: acc?.consumerKey || "",
    zone: acc?.zone || "IE",
  });
  const set = (k: keyof typeof form, v: string) => setForm((p) => ({ ...p, [k]: v }));
  const canSubmit = form.name.trim() && form.appKey.trim() && form.appSecret.trim() && form.consumerKey.trim();

  const submit = async () => {
    if (!canSubmit) return;
    const payload = {
      name: form.name.trim(),
      appKey: form.appKey.trim(),
      appSecret: form.appSecret.trim(),
      consumerKey: form.consumerKey.trim(),
      zone: form.zone,
      endpoint: endpointForZone(form.zone),
    };
    if (isEdit) {
      await update.mutateAsync({ id: acc!.id, input: payload });
    } else {
      await create.mutateAsync(payload);
    }
    onClose();
  };

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{isEdit ? `编辑账户 ${acc!.name}` : "添加 OVH 账户"}</DialogTitle>
          <DialogDescription>填三个 OVH 密钥 + 选子公司,保存时会自动调 /me 验证凭据。</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-2">
          <Field label="账户名称 *">
            <Input value={form.name} onChange={(e) => set("name", e.target.value)} placeholder="主号 / 小号 A" autoFocus />
          </Field>
          <Field label="APP KEY *">
            <Input type="password" value={form.appKey} onChange={(e) => set("appKey", e.target.value)} placeholder="xxxxxxxxxxxxxxxx" />
          </Field>
          <Field label="APP SECRET *">
            <Input type="password" value={form.appSecret} onChange={(e) => set("appSecret", e.target.value)} placeholder="xxxxxxxxxxxxxxxx" />
          </Field>
          <Field label="CONSUMER KEY *">
            <Input type="password" value={form.consumerKey} onChange={(e) => set("consumerKey", e.target.value)} placeholder="xxxxxxxxxxxxxxxx" />
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
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button onClick={submit} disabled={!canSubmit || create.isPending || update.isPending}>
            {(create.isPending || update.isPending) ? "保存中…" : "保存并验证"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
