import { useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2, LogOut, UserRound } from "lucide-react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Section } from "@/routes/settings-sections";
import { logout } from "@/components/common/AuthGate";

/** 设置 → 登录账号:当前用户、修改密码、退出登录 */
export function AccountSecuritySection() {
  const { t } = useTranslation();
  const me = useQuery({
    queryKey: ["auth", "me"],
    queryFn: async () => (await api.get<{ username?: string; authEnabled?: boolean }>("/auth/me")).data,
    staleTime: 60_000,
  });
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [loggingOut, setLoggingOut] = useState(false);

  const mismatch = confirm !== "" && confirm !== next;
  const tooShort = next !== "" && next.length < 8;
  const canSave = !saving && current !== "" && next.length >= 8 && confirm === next;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!canSave) return;
    setSaving(true);
    setError("");
    try {
      await api.post("/auth/password", { currentPassword: current, newPassword: next });
      setCurrent("");
      setNext("");
      setConfirm("");
      toast.success(t("settings.account.changed"));
    } catch (err: any) {
      const code = err?.response?.data?.code;
      if (code === "AUTH_BAD_CURRENT_PASSWORD") setError(t("settings.account.badCurrent"));
      else if (code === "AUTH_PASSWORD_TOO_LONG") setError(t("commons.auth.passwordTooLong"));
      else if (err?.response?.status === 429) setError(t("commons.auth.rateLimited", { secs: err.response.data?.retryAfter || 60 }));
      else setError(err?.response?.data?.error || err?.message || t("settings.account.failed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Section title={t("settings.account.title")}>
      <div className="flex items-center justify-between gap-3 rounded-xl border border-border px-4 py-3">
        <div className="flex items-center gap-3 min-w-0">
          <div className="w-9 h-9 rounded-full bg-primary/10 text-primary flex items-center justify-center shrink-0">
            <UserRound className="w-4 h-4" />
          </div>
          <div className="min-w-0">
            <p className="text-[12px] text-muted-foreground">{t("settings.account.signedInAs")}</p>
            <p className="text-[14px] font-medium truncate">{me.data?.username || "—"}</p>
          </div>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={loggingOut}
          onClick={() => {
            setLoggingOut(true);
            void logout();
          }}
        >
          {loggingOut ? <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" /> : <LogOut className="w-3.5 h-3.5 mr-1.5" />}
          {t("settings.account.logout")}
        </Button>
      </div>

      <form onSubmit={submit} className="space-y-4 max-w-sm" noValidate>
        <h3 className="text-[13px] font-semibold pt-1">{t("settings.account.changeTitle")}</h3>
        {/* 隐藏的用户名字段:让密码管理器知道这是哪个账号的密码 */}
        <input type="text" name="username" autoComplete="username" value={me.data?.username || ""} readOnly hidden />
        <div className="space-y-1.5">
          <label htmlFor="pw-current" className="block text-[13px] font-medium">
            {t("settings.account.current")}
          </label>
          <Input id="pw-current" type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} />
        </div>
        <div className="space-y-1.5">
          <label htmlFor="pw-new" className="block text-[13px] font-medium">
            {t("settings.account.new")}
          </label>
          <Input id="pw-new" type="password" autoComplete="new-password" value={next} aria-invalid={tooShort || undefined} onChange={(e) => setNext(e.target.value)} />
          <p className={tooShort ? "text-[11px] text-destructive" : "text-[11px] text-muted-foreground"}>
            {tooShort ? t("commons.auth.passwordTooShort") : t("commons.auth.passwordRule")}
          </p>
        </div>
        <div className="space-y-1.5">
          <label htmlFor="pw-confirm" className="block text-[13px] font-medium">
            {t("commons.auth.confirmPassword")}
          </label>
          <Input id="pw-confirm" type="password" autoComplete="new-password" value={confirm} aria-invalid={mismatch || undefined} onChange={(e) => setConfirm(e.target.value)} />
          {mismatch && <p className="text-[11px] text-destructive">{t("commons.auth.mismatch")}</p>}
        </div>
        {error && (
          <p role="alert" className="text-[12px] text-destructive">
            {error}
          </p>
        )}
        <div className="flex items-center gap-3">
          <Button type="submit" disabled={!canSave}>
            {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
            {t("settings.account.save")}
          </Button>
          <p className="text-[11px] text-muted-foreground">{t("settings.account.revokeNote")}</p>
        </div>
      </form>
    </Section>
  );
}
