import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react";
import { AlertCircle, ArrowRight, Check, Eye, EyeOff, KeyRound, Loader2, ShieldCheck, Sparkles, UserRound } from "lucide-react";
import axios from "axios";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { BrandLogo } from "@/components/common/BrandLogo";
import { LoginArt } from "@/components/illustrations/LoginArt";
import {
  api,
  clearLegacyApiKey,
  clearSessionToken,
  getLegacyApiKey,
  getSessionToken,
  onAuthFailure,
  setSessionToken,
  SESSION_HEADER,
} from "@/lib/api";
import { useTranslation } from "react-i18next";
import i18n from "@/i18n";
import { LanguageToggle } from "@/components/layout/LanguageToggle";
import { ThemeToggle } from "@/components/layout/ThemeToggle";

type AuthState = "checking" | "setup" | "login" | "authed";

/**
 * 全局鉴权拦截(账户密码登录):
 * - 先问 /api/auth/status:还没有管理员 → 初始化页;否则看本地会话令牌是否还有效
 * - 初始化要提供「初始化密钥」= 旧版 API 密钥。老用户浏览器里存着旧密钥,自动带上,
 *   只需要填用户名和密码;新装的从 .env 的 API_SECRET_KEY 或启动日志里拿
 * - 验证用裸 axios,不走带拦截器的 `api`:避免循环触发会话失效
 * - 进入应用后仍订阅 401:会话在别处被登出 / 改密后,重新弹登录
 */
export function AuthGate({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>("checking");
  const [errMsg, setErrMsg] = useState("");

  const resolve = useCallback(async (expiredMessage?: string) => {
    let status: { authEnabled?: boolean; setupRequired?: boolean };
    try {
      const res = await axios.get("/api/auth/status", { timeout: 10000 });
      status = res.data || {};
    } catch {
      // 后端没起来:放行进入应用,让各请求自己报错 —— 卡在这一层用户什么都看不到
      setState("authed");
      return;
    }
    if (status.authEnabled === false) {
      setState("authed");
      return;
    }
    if (status.setupRequired) {
      clearSessionToken();
      setErrMsg("");
      setState("setup");
      return;
    }
    const token = getSessionToken();
    if (!token) {
      setErrMsg(expiredMessage || "");
      setState("login");
      return;
    }
    try {
      const res = await axios.get("/api/auth/me", {
        headers: { [SESSION_HEADER]: token },
        timeout: 10000,
        validateStatus: () => true,
      });
      if (res.status === 200) {
        setState("authed");
        return;
      }
      if (res.status === 429) {
        setErrMsg(res.data?.message || i18n.t("commons.auth.rateLimited", { secs: res.data?.retryAfter || 60 }));
        setState("login");
        return;
      }
      clearSessionToken();
      setErrMsg(expiredMessage || i18n.t("commons.auth.sessionExpired"));
      setState("login");
    } catch {
      setState("authed");
    }
  }, []);

  useEffect(() => {
    void resolve();
  }, [resolve]);

  useEffect(() => {
    if (state !== "authed") return;
    return onAuthFailure(() => {
      setState("checking");
      void resolve(i18n.t("commons.auth.sessionExpired"));
    });
  }, [state, resolve]);

  if (state === "checking") {
    return (
      <div className="fixed inset-0 z-[100] bg-background flex items-center justify-center">
        <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (state === "setup" || state === "login") {
    return (
      <AuthScreen
        mode={state}
        initialError={errMsg}
        onModeChange={(m) => {
          setErrMsg("");
          setState(m);
        }}
        onSuccess={(token) => {
          setSessionToken(token);
          clearLegacyApiKey();
          setErrMsg("");
          setState("authed");
        }}
      />
    );
  }

  return <>{children}</>;
}

/** 退出登录:作废服务端会话,回到登录页 */
export async function logout(): Promise<void> {
  try {
    await api.post("/auth/logout");
  } catch {
    // 服务端没作废也要清本地:用户要的是「这台设备退出」
  }
  clearSessionToken();
  window.location.reload();
}

function PasswordInput({
  id,
  value,
  onChange,
  autoComplete,
  autoFocus,
  invalid,
}: {
  id: string;
  value: string;
  onChange: (v: string) => void;
  autoComplete: string;
  autoFocus?: boolean;
  invalid?: boolean;
}) {
  const { t } = useTranslation();
  const [show, setShow] = useState(false);
  return (
    <div className="relative">
      <Input
        id={id}
        type={show ? "text" : "password"}
        autoComplete={autoComplete}
        autoFocus={autoFocus}
        spellCheck={false}
        value={value}
        aria-invalid={invalid || undefined}
        onChange={(e) => onChange(e.target.value)}
        className="h-12 pr-12 rounded-xl text-[15px]"
      />
      <button
        type="button"
        onClick={() => setShow((v) => !v)}
        aria-label={show ? t("commons.auth.hidePassword") : t("commons.auth.showPassword")}
        aria-pressed={show}
        className="absolute right-1.5 top-1/2 -translate-y-1/2 h-9 w-9 inline-flex items-center justify-center rounded-lg text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {show ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
      </button>
    </div>
  );
}

function Field({ id, label, hint, children }: { id: string; label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <label htmlFor={id} className="text-[13px] font-medium block">
        {label}
      </label>
      {children}
      {hint && <p className="text-[12px] text-muted-foreground leading-relaxed">{hint}</p>}
    </div>
  );
}

function AuthScreen({
  mode,
  initialError,
  onSuccess,
  onModeChange,
}: {
  mode: "setup" | "login";
  initialError?: string;
  onSuccess: (token: string) => void;
  onModeChange: (m: "setup" | "login") => void;
}) {
  const { t } = useTranslation();
  const legacyKey = getLegacyApiKey() || "";
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [setupKey, setSetupKey] = useState("");
  // 浏览器里有旧密钥就先用它,失败了才让用户手动填
  const [askSetupKey, setAskSetupKey] = useState(!legacyKey);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(initialError || "");
  const [cooldown, setCooldown] = useState(0);
  useEffect(() => {
    if (cooldown <= 0) return;
    const timer = setInterval(() => setCooldown((n) => (n <= 1 ? 0 : n - 1)), 1000);
    return () => clearInterval(timer);
  }, [cooldown]);

  const isSetup = mode === "setup";
  const mismatch = isSetup && confirm !== "" && confirm !== password;
  const tooShort = isSetup && password !== "" && password.length < 8;
  const canSubmit =
    !submitting &&
    cooldown === 0 &&
    username.trim() !== "" &&
    password !== "" &&
    (!isSetup || (confirm === password && password.length >= 8 && (!askSetupKey || setupKey.trim() !== "")));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    setSubmitting(true);
    setError("");
    try {
      const res = await axios.post(
        isSetup ? "/api/auth/setup" : "/api/auth/login",
        isSetup
          ? { username: username.trim(), password, setupKey: askSetupKey ? setupKey.trim() : legacyKey }
          : { username: username.trim(), password },
        { timeout: 20000, validateStatus: () => true },
      );
      const code: string | undefined = res.data?.code;
      if (res.status === 200 && res.data?.token) {
        onSuccess(res.data.token);
        return;
      }
      if (res.status === 429) {
        const secs = Number(res.data?.retryAfter) || Number(res.headers?.["retry-after"]) || 60;
        setCooldown(secs);
        setError(t("commons.auth.rateLimited", { secs }));
        return;
      }
      if (code === "SETUP_REQUIRED") {
        onModeChange("setup");
        return;
      }
      if (code === "AUTH_SETUP_DONE") {
        onModeChange("login");
        return;
      }
      if (code === "AUTH_BAD_SETUP_KEY") {
        // 浏览器里存的旧密钥不对(服务端换过),改成让用户手填
        if (!askSetupKey) setAskSetupKey(true);
        setError(t("commons.auth.badSetupKey"));
        return;
      }
      const known: Record<string, string> = {
        AUTH_BAD_CREDENTIALS: "commons.auth.badCredentials",
        AUTH_INVALID_USERNAME: "commons.auth.invalidUsername",
        AUTH_PASSWORD_TOO_SHORT: "commons.auth.passwordTooShort",
        AUTH_PASSWORD_TOO_LONG: "commons.auth.passwordTooLong",
      };
      if (code && known[code]) {
        setError(t(known[code]));
        return;
      }
      setError(t("commons.auth.unexpectedStatus", { status: res.status }));
    } catch (err: any) {
      setError(t("commons.auth.unreachable", { reason: err?.message || t("commons.auth.networkError") }));
    } finally {
      setSubmitting(false);
    }
  };

  const title = isSetup ? t("commons.auth.setupTitle") : t("commons.auth.loginTitle");
  const subtitle = isSetup
    ? legacyKey
      ? t("commons.auth.setupSubtitleMigrate")
      : t("commons.auth.setupSubtitle")
    : t("commons.auth.loginSubtitle");
  const features = [t("commons.auth.feature1"), t("commons.auth.feature2"), t("commons.auth.feature3")];

  return (
    <div className="fixed inset-0 z-[100] overflow-y-auto bg-background">
      <div className="absolute top-3 right-3 z-10 flex items-center gap-1">
        <LanguageToggle />
        <ThemeToggle />
      </div>
      <div className="min-h-full flex items-center justify-center px-4 py-14 md:px-8">
        <div className="w-full max-w-[400px] md:max-w-[940px] md:grid md:grid-cols-[1.05fr_1fr] md:rounded-[28px] md:border md:border-border md:bg-card md:overflow-hidden md:shadow-[0_1px_2px_rgba(16,40,100,.04),0_24px_60px_-28px_rgba(26,80,216,.28)]">
          {/* 左:品牌面板(仅桌面) */}
          <aside className="hidden md:flex flex-col relative p-10 bg-gradient-to-br from-primary/[0.08] via-primary/[0.03] to-transparent border-r border-border">
            <div className="flex items-center gap-2.5">
              <BrandLogo className="w-9 h-9" />
              <span className="text-[15px] font-semibold tracking-tight">{t("commons.auth.brandName")}</span>
            </div>
            <LoginArt className="w-full max-w-[320px] mx-auto mt-8 mb-6" />
            <h2 className="text-[22px] leading-snug font-semibold tracking-tight">{t("commons.auth.tagline")}</h2>
            <ul className="mt-4 space-y-2.5">
              {features.map((f) => (
                <li key={f} className="flex items-center gap-2.5 text-[13.5px] text-muted-foreground">
                  <span className="w-5 h-5 rounded-full bg-primary/10 text-primary inline-flex items-center justify-center shrink-0">
                    <Check className="w-3 h-3" strokeWidth={3} />
                  </span>
                  {f}
                </li>
              ))}
            </ul>
            <p className="mt-auto pt-8 flex items-center gap-1.5 text-[12px] text-muted-foreground">
              <ShieldCheck className="w-3.5 h-3.5 text-primary" />
              {t("commons.auth.selfHosted")}
            </p>
          </aside>

          {/* 右:表单 */}
          <section className="flex flex-col justify-center md:px-12 md:py-12">
            <div className="flex flex-col items-center text-center md:items-start md:text-left mb-7">
              <BrandLogo className="w-14 h-14 mb-5 md:hidden" />
              {isSetup && (
                <span className="mb-3 inline-flex items-center gap-1.5 rounded-full bg-primary/10 text-primary px-2.5 py-1 text-[12px] font-medium">
                  <Sparkles className="w-3 h-3" />
                  {legacyKey ? t("commons.auth.badgeMigrate") : t("commons.auth.badgeFirstRun")}
                </span>
              )}
              <h1 className="text-[26px] leading-tight font-semibold tracking-tight">{title}</h1>
              <p className="text-[14px] text-muted-foreground mt-2 leading-relaxed">{subtitle}</p>
            </div>

            <form onSubmit={submit} className="space-y-4" noValidate>
              <Field id="auth-username" label={t("commons.auth.username")}>
                <div className="relative">
                  <UserRound className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
                  <Input
                    id="auth-username"
                    autoComplete="username"
                    autoFocus
                    autoCapitalize="none"
                    spellCheck={false}
                    value={username}
                    placeholder={t("commons.auth.usernamePlaceholder")}
                    onChange={(e) => setUsername(e.target.value)}
                    className="h-12 pl-10 rounded-xl text-[15px]"
                  />
                </div>
              </Field>

              <Field
                id="auth-password"
                label={t("commons.auth.password")}
                hint={isSetup ? (tooShort ? <span className="text-destructive">{t("commons.auth.passwordTooShort")}</span> : t("commons.auth.passwordRule")) : undefined}
              >
                <PasswordInput
                  id="auth-password"
                  value={password}
                  onChange={setPassword}
                  autoComplete={isSetup ? "new-password" : "current-password"}
                  invalid={tooShort}
                />
              </Field>

              {isSetup && (
                <Field
                  id="auth-confirm"
                  label={t("commons.auth.confirmPassword")}
                  hint={mismatch ? <span className="text-destructive">{t("commons.auth.mismatch")}</span> : undefined}
                >
                  <PasswordInput id="auth-confirm" value={confirm} onChange={setConfirm} autoComplete="new-password" invalid={mismatch} />
                </Field>
              )}

              {isSetup && askSetupKey && (
                <Field id="auth-setup-key" label={t("commons.auth.setupKey")} hint={t("commons.auth.setupKeyHint")}>
                  <div className="relative">
                    <KeyRound className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground pointer-events-none" />
                    <Input
                      id="auth-setup-key"
                      type="password"
                      autoComplete="off"
                      spellCheck={false}
                      value={setupKey}
                      onChange={(e) => setSetupKey(e.target.value)}
                      className="h-12 pl-10 rounded-xl font-mono text-[13px]"
                    />
                  </div>
                </Field>
              )}

              {error && (
                <div role="alert" className="flex items-start gap-2 rounded-xl bg-destructive/10 px-3.5 py-2.5 text-[13px] text-destructive leading-relaxed">
                  <AlertCircle className="w-4 h-4 mt-0.5 shrink-0" />
                  <span>{error}</span>
                </div>
              )}

              <Button type="submit" disabled={!canSubmit} className="w-full h-12 rounded-xl text-[15px] font-medium mt-2">
                {submitting ? (
                  <>
                    <Loader2 className="w-4 h-4 animate-spin mr-1.5" />
                    {isSetup ? t("commons.auth.creating") : t("commons.auth.signingIn")}
                  </>
                ) : cooldown > 0 ? (
                  t("commons.auth.waitSeconds", { count: cooldown })
                ) : (
                  <>
                    {isSetup ? t("commons.auth.createAccount") : t("commons.auth.signIn")}
                    <ArrowRight className="w-4 h-4 ml-1.5" />
                  </>
                )}
              </Button>
            </form>

            {isSetup && (
              <p className="text-[12px] text-muted-foreground mt-6 leading-relaxed text-center md:text-left">
                {t("commons.auth.setupNote")}
              </p>
            )}
          </section>
        </div>
      </div>
    </div>
  );
}
