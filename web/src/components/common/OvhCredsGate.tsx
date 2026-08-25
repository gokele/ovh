import { useEffect, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import axios from "axios";
import { KeyRound, Loader2, Globe, Settings as SettingsIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { OVH_SUBSIDIARIES } from "@/lib/ovh-subsidiaries";
import { apiBaseUrlForEndpoint, endpointRegion } from "@/lib/ovh-regions";

const PREFETCH_STALE = 2 * 60 * 60_000;

/** 凭据存好后立刻预热三件套. 用户切到 servers 页时直接命中,不会再"加载中" */
function prefetchAfterCredsSaved(qc: ReturnType<typeof useQueryClient>, zone: string) {
  // key 必须跟 useServers 完全一致(含 accountId 维度),否则预热的是一条谁也读不到的缓存。
  // 刚建完账户时还没有"活跃账户",useServers 那边也是空字符串 → 默认视角。
  void qc.prefetchQuery({
    queryKey: qk.servers.list(true, ""),
    queryFn: async () => {
      const res = await api.get("/servers", { params: { showApiServers: true } });
      return res.data.servers || res.data || [];
    },
    staleTime: PREFETCH_STALE,
  });
  void qc.prefetchQuery({
    queryKey: ["ovh-catalog", "eco", zone || "auto"] as const,
    queryFn: async () => {
      const params: Record<string, string> = {};
      if (zone) params.subsidiary = zone;
      const res = await api.get("/catalog", { params });
      return res.data;
    },
    staleTime: PREFETCH_STALE,
  });
  // 站点/大区判定统一走 lib/ovh-regions,不再在这里另写一张 endpoint→URL 表;
  // queryKey 也必须跟 useAvailability 一致(按大区分桶),否则预热等于白拉一份 ~9MB 数据。
  const endpoint = OVH_SUBSIDIARIES.find((s) => s.code === zone)?.endpoint;
  const baseUrl = apiBaseUrlForEndpoint(endpoint);
  void qc.prefetchQuery({
    queryKey: qk.availability.all(endpointRegion(endpoint)),
    queryFn: async () => {
      const res = await axios.get(`${baseUrl}/v1/dedicated/server/datacenter/availabilities`, {
        timeout: 30000,
      });
      return res.data;
    },
    staleTime: 60_000,
  });
}

type GateState = "checking" | "needs-account" | "ok";

interface AccountForm {
  name: string;
  appKey: string;
  appSecret: string;
  consumerKey: string;
  zone: string;
}

const DEFAULT_FORM: AccountForm = {
  name: "默认账户",
  appKey: "",
  appSecret: "",
  consumerKey: "",
  zone: "IE",
};

function endpointForZone(zone: string): string {
  const hit = OVH_SUBSIDIARIES.find((s) => s.code === zone);
  return hit?.endpoint || "ovh-eu";
}

/**
 * 多账户 gate:
 * - 启动时拉 /api/accounts;一个账户都没有 → 整屏拦截,必须先添加一个账户
 * - 已有任意账户 → 放行(进设置页可以加更多)
 */
export function OvhCredsGate({ children }: { children: ReactNode }) {
  const [state, setState] = useState<GateState>("checking");

  useEffect(() => {
    let cancelled = false;
    api
      .get("/accounts")
      .then((res) => {
        if (cancelled) return;
        const accs = res.data?.accounts || [];
        setState(accs.length > 0 ? "ok" : "needs-account");
      })
      .catch(() => {
        // 后端挂了或还没启动,先放行让单请求层报错
        if (!cancelled) setState("ok");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (state === "checking") {
    return (
      <div className="fixed inset-0 z-[90] bg-background flex items-center justify-center">
        <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (state === "needs-account") {
    return <AccountOverlay onSuccess={() => setState("ok")} />;
  }

  return <>{children}</>;
}

function AccountOverlay({ onSuccess }: { onSuccess: () => void }) {
  const qc = useQueryClient();
  const [form, setForm] = useState<AccountForm>(DEFAULT_FORM);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string>("");

  const set = (k: keyof AccountForm, v: string) =>
    setForm((prev) => ({ ...prev, [k]: v }));

  // token 申请页按站点分:EU / US / CA 三站的 token 互不通用,链接必须跟着所选子公司走
  const tokenSiteUrl = apiBaseUrlForEndpoint(endpointForZone(form.zone || "IE"));

  const canSubmit =
    form.name.trim() &&
    form.appKey.trim() &&
    form.appSecret.trim() &&
    form.consumerKey.trim() &&
    !submitting;

  const submit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    setError("");
    try {
      const zone = form.zone || "IE";
      const res = await api.post("/accounts", {
        name: form.name.trim(),
        appKey: form.appKey.trim(),
        appSecret: form.appSecret.trim(),
        consumerKey: form.consumerKey.trim(),
        zone,
        endpoint: endpointForZone(zone),
        setDefault: true, // 首次创建自动设默认
      });
      qc.invalidateQueries({ queryKey: ["accounts", "list"] });
      // 后端 /accounts 会带回 subsidiaryWarning:凭据能用,但这个账户在 OVH 那边属于另一个子公司。
      // 它不影响 valid,却决定目录/价格/库存/下单 region 打到哪个站点,所以必须当场说出来。
      const warning: string = res.data?.subsidiaryWarning || "";
      if (res.data?.valid === false) {
        setError(
          "账户已保存,但 OVH 验证失败。检查 APP KEY / APP SECRET / CONSUMER KEY 是否匹配所选子公司。可以先进入再到设置页修复。" +
            (warning ? " " + warning : "")
        );
        // 验证失败也放行,不强卡用户
        prefetchAfterCredsSaved(qc, zone);
        onSuccess();
        return;
      }
      if (warning) {
        // 凭据没问题就放行,但这层引导页马上会被 onSuccess 卸载,setError 用户根本看不到 ——
        // 用长时间 toast 把错配带到主界面上,让用户去设置页把 zone 改对再下单。
        toast.warning(warning, { duration: 20000 });
      }
      prefetchAfterCredsSaved(qc, zone);
      onSuccess();
    } catch (e: any) {
      setError(e?.response?.data?.error || e?.message || "保存失败");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="fixed inset-0 z-[90] bg-background/95 backdrop-blur-sm flex items-center justify-center px-4 py-8 overflow-y-auto">
      <div className="w-full max-w-lg border border-border rounded-2xl bg-background p-7 space-y-5">
        <div className="flex items-center gap-2.5">
          <div className="w-10 h-10 rounded-xl bg-secondary flex items-center justify-center">
            <Globe className="w-5 h-5" />
          </div>
          <div>
            <h2 className="text-lg font-semibold leading-tight">添加第一个 OVH 账户</h2>
            <p className="text-[12px] text-muted-foreground mt-0.5">
              系统支持多账户,先添加一个用起来,后续可以在"设置 → 账户"加更多
            </p>
          </div>
        </div>

        <div className="space-y-3.5">
          <Field label="账户名称 *" hint="用户起的别名,比如 主号 / 小号 A,只用于本地区分">
            <Input
              autoFocus
              value={form.name}
              onChange={(e) => set("name", e.target.value)}
              placeholder="主号"
            />
          </Field>
          <Field label="APP KEY *">
            <PasswordInput value={form.appKey} onChange={(v) => set("appKey", v)} placeholder="xxxxxxxxxxxxxxxx" />
          </Field>
          <Field label="APP SECRET *">
            <PasswordInput value={form.appSecret} onChange={(v) => set("appSecret", v)} placeholder="xxxxxxxxxxxxxxxx" />
          </Field>
          <Field label="CONSUMER KEY *">
            <PasswordInput value={form.consumerKey} onChange={(v) => set("consumerKey", v)} placeholder="xxxxxxxxxxxxxxxx" />
          </Field>

          <Field label="OVH 子公司 (Zone)">
            <Select value={form.zone} onValueChange={(v) => set("zone", v)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {OVH_SUBSIDIARIES.map((s) => (
                  <SelectItem key={s.code} value={s.code}>
                    {s.code} · {s.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-[11px] text-muted-foreground mt-1.5">
              Endpoint <code className="px-1 py-0.5 bg-muted rounded">{endpointForZone(form.zone)}</code>
              {" · "}IAM <code className="px-1 py-0.5 bg-muted rounded">go-ovh-{form.zone.toLowerCase()}</code>
              {" 由子公司自动派生"}
            </p>
          </Field>

          {error && <p className="text-[12px] text-destructive">{error}</p>}
        </div>

        <Button onClick={submit} disabled={!canSubmit} className="w-full">
          {submitting ? (
            <>
              <Loader2 className="w-4 h-4 animate-spin mr-1.5" />
              验证并创建…
            </>
          ) : (
            <>
              <SettingsIcon className="w-4 h-4 mr-1.5" />
              创建并进入
            </>
          )}
        </Button>

        <p className="text-[10px] text-muted-foreground leading-relaxed">
          {/* createToken 页面是按站点分开的:三站的 token 互不通用,
              美区账户拿 eu.api.ovh.com 申请的 token 永远登不进去(实测三站的
              /createToken/ 各自 200)。链接必须跟着上面选的子公司走。 */}
          凭据保存到本地 SQLite,不会上传。还没有?去
          <a
            href={`${tokenSiteUrl}/createToken/`}
            target="_blank"
            rel="noreferrer"
            className="underline mx-1"
          >
            {tokenSiteUrl.replace("https://", "")}/createToken
          </a>
          申请（{form.zone} 属于该站点，别在其它站点申请）。
        </p>
      </div>
    </div>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div>
      <label className="block text-[12px] font-medium mb-1.5">{label}</label>
      {children}
      {hint && <p className="text-[11px] text-muted-foreground mt-1">{hint}</p>}
    </div>
  );
}

function PasswordInput({
  value,
  onChange,
  placeholder,
  autoFocus,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  autoFocus?: boolean;
}) {
  return (
    <div className="relative">
      <KeyRound className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
      <Input
        type="password"
        autoComplete="off"
        spellCheck={false}
        autoFocus={autoFocus}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="pl-9 font-mono text-[13px]"
      />
    </div>
  );
}
