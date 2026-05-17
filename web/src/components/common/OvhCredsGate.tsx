import { useEffect, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import axios from "axios";
import { KeyRound, Loader2, Globe, Settings as SettingsIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api } from "@/lib/api";
import { OVH_SUBSIDIARIES, defaultSubsidiaryForEndpoint } from "@/lib/ovh-subsidiaries";

const PREFETCH_STALE = 2 * 60 * 60_000; // 与 useServers / useOvhCatalog / useAvailability staleTime 一致

/** 凭据存好后立刻预热三件套：服务器目录 / catalog(价格) / 可用性。
 *  fire-and-forget，写到 React Query 缓存里，
 *  用户切到 servers 页时直接命中，不会再看到"加载中"。
 */
function prefetchAfterCredsSaved(qc: ReturnType<typeof useQueryClient>, zone: string) {
  void qc.prefetchQuery({
    queryKey: ["servers", "list", { showApiServers: true }] as const,
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
  // 可用性走 OVH 公开接口，按 zone 选 base URL
  const meta = OVH_SUBSIDIARIES.find((s) => s.code === zone);
  const baseUrl =
    meta?.endpoint === "ovh-us"
      ? "https://api.us.ovhcloud.com"
      : meta?.endpoint === "ovh-ca"
        ? "https://ca.api.ovh.com"
        : "https://eu.api.ovh.com";
  void qc.prefetchQuery({
    queryKey: ["availability", "all", "auto"] as const,
    queryFn: async () => {
      const res = await axios.get(`${baseUrl}/v1/dedicated/server/datacenter/availabilities`, {
        timeout: 30000,
      });
      return res.data;
    },
    staleTime: 60_000,
  });
}

type GateState = "checking" | "needs-creds" | "ok";

interface CredsForm {
  appKey: string;
  appSecret: string;
  consumerKey: string;
  zone: string; // OVH 子公司，endpoint 由它自动派生
}

const DEFAULT_FORM: CredsForm = {
  appKey: "",
  appSecret: "",
  consumerKey: "",
  zone: "IE",
};

/** 根据 zone 推 endpoint；未匹配走 ovh-eu */
function endpointForZone(zone: string): string {
  const hit = OVH_SUBSIDIARIES.find((s) => s.code === zone);
  return hit?.endpoint || "ovh-eu";
}

/**
 * OVH 凭据强制配置：
 * - 启动时拉 /api/settings；三个凭据字段任一为空 → 整屏拦截，必须填完才能进入应用。
 * - 提交时调 /api/verify-auth 真实验证 OVH 那边能不能用；失败留在表单。
 * - 嵌在 AuthGate 之后：先验证后端访问密码，再验证 OVH 凭据；都通过才放行。
 */
export function OvhCredsGate({ children }: { children: ReactNode }) {
  const [state, setState] = useState<GateState>("checking");
  const [form, setForm] = useState<CredsForm>(DEFAULT_FORM);

  useEffect(() => {
    let cancelled = false;
    api
      .get("/settings")
      .then((res) => {
        if (cancelled) return;
        const cfg = res.data || {};
        const has = !!(cfg.appKey && cfg.appSecret && cfg.consumerKey);
        if (has) {
          setState("ok");
        } else {
          setForm({
            appKey: cfg.appKey || "",
            appSecret: cfg.appSecret || "",
            consumerKey: cfg.consumerKey || "",
            zone: cfg.zone || defaultSubsidiaryForEndpoint(cfg.endpoint),
          });
          setState("needs-creds");
        }
      })
      .catch(() => {
        // 拿不到 settings：可能网络错或后端挂，先放行让单请求层报错。
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

  if (state === "needs-creds") {
    return <CredsOverlay initialForm={form} onSuccess={() => setState("ok")} />;
  }

  return <>{children}</>;
}

function CredsOverlay({
  initialForm,
  onSuccess,
}: {
  initialForm: CredsForm;
  onSuccess: () => void;
}) {
  const qc = useQueryClient();
  const [form, setForm] = useState<CredsForm>(initialForm);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string>("");

  const set = (k: keyof CredsForm, v: string) =>
    setForm((prev) => ({ ...prev, [k]: v }));

  const canSubmit =
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
      await api.post("/settings", {
        appKey: form.appKey.trim(),
        appSecret: form.appSecret.trim(),
        consumerKey: form.consumerKey.trim(),
        zone,
        endpoint: endpointForZone(zone),
      });
      const verify = await api.post("/verify-auth", {});
      if (verify.data?.valid) {
        // 首次保存凭据后立即预热三件套（servers / catalog / availability），
        // 用户切到服务器列表页就能直接显示，不会再走"加载中"
        prefetchAfterCredsSaved(qc, zone);
        onSuccess();
      } else {
        setError("OVH 验证失败：检查 APP KEY / APP SECRET / CONSUMER KEY 是否匹配所选子公司");
      }
    } catch (e: any) {
      setError(e?.response?.data?.message || e?.message || "保存失败");
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
            <h2 className="text-lg font-semibold leading-tight">配置 OVH API 凭据</h2>
            <p className="text-[12px] text-muted-foreground mt-0.5">
              首次使用需要填写 OVH 的三个密钥，否则无法访问任何功能
            </p>
          </div>
        </div>

        <div className="space-y-3.5">
          <Field label="APP KEY *">
            <PasswordInput value={form.appKey} onChange={(v) => set("appKey", v)} placeholder="xxxxxxxxxxxxxxxx" autoFocus />
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
              验证并保存…
            </>
          ) : (
            <>
              <SettingsIcon className="w-4 h-4 mr-1.5" />
              保存并进入
            </>
          )}
        </Button>

        <p className="text-[10px] text-muted-foreground leading-relaxed">
          凭据保存到后端 SQLite。还没有？去
          <a
            href="https://eu.api.ovh.com/createToken/"
            target="_blank"
            rel="noreferrer"
            className="underline mx-1"
          >
            eu.api.ovh.com/createToken
          </a>
          申请。
        </p>
      </div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <label className="block text-[12px] font-medium mb-1.5">{label}</label>
      {children}
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
