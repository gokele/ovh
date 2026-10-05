import { useState } from "react";
import { Mail, RefreshCw, Check, X as XIcon, KeyRound, AlertCircle } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { Chip } from "@/components/common/Chip";
import { PartialNotice } from "@/components/common/PartialNotice";
import { useActiveAccountEndpoint } from "@/components/common/active-endpoint";
import { useChangeContact, useContactChangeRequests, useContactRequestAction } from "@/hooks/use-server-control";
import { toast } from "sonner";
import { useTranslation, Trans } from "react-i18next";
import { errorMessage } from "@/components/common/LoadFailed";
import { fmtDateTime } from "@/i18n/format";

/** 变更联系人对话框：提交新 NIC + 查看 / 接受 / 拒绝 / 重发邮件 待审请求 + token 子对话框 */
export function ChangeContactDialog({
  serviceName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const { t } = useTranslation();
  const submit = useChangeContact();
  const list = useContactChangeRequests(open);
  const action = useContactRequestAction();
  // 美区账户没有 NIC 联系人体系，提交必然 400，入口就禁掉
  const { isUS, ready } = useActiveAccountEndpoint();
  const usUnsupported = ready && isUS;

  const [admin, setAdmin] = useState("");
  const [tech, setTech] = useState("");
  const [billing, setBilling] = useState("");
  const [tab, setTab] = useState("submit");
  const [tokenTarget, setTokenTarget] = useState<{ id: number | string; mode: "accept" | "refuse" } | null>(null);
  const [token, setToken] = useState("");

  const handleSubmit = async () => {
    if (!admin && !tech && !billing) {
      toast.error(t("maint.contact.toast.needOneField"));
      return;
    }
    try {
      await submit.mutateAsync({ serviceName, admin, tech, billing });
      toast.success(t("maint.contact.toast.submitted"));
      setAdmin("");
      setTech("");
      setBilling("");
      setTab("requests");
      list.refetch();
    } catch (e: any) {
      // OVH 业务约束:一个 service 同时只能有一个待审 contact change task
      if (/contact change task is already running/i.test(String(e?.response?.data?.error || e?.message || ""))) {
        toast.error(t("maint.contact.toast.alreadyRunning"), { duration: 5000 });
        setTab("requests");
        list.refetch();
        return;
      }
      toast.error(errorMessage(e));
    }
  };

  const handleAction = async (id: number | string, mode: "accept" | "refuse" | "resend", tokenVal?: string) => {
    try {
      await action.mutateAsync({ id, action: mode, token: tokenVal });
      toast.success(
        mode === "resend"
          ? t("maint.contact.toast.resent")
          : mode === "accept"
            ? t("maint.contact.toast.accepted")
            : t("maint.contact.toast.refused")
      );
      setTokenTarget(null);
      setToken("");
    } catch (e: any) {
      // 后端有时用 message 字段(refuse/accept/resend),有时用 error(change-contact),
      // errorMessage 会按 error→message→err.message 顺序挖,最后兜底通用文案
      toast.error(errorMessage(e), { duration: 6000 });
    }
  };

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl max-h-[90vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Mail className="w-5 h-5" />
              {t("maint.contact.title")}
            </DialogTitle>
            <DialogDescription>{t("maint.contact.desc")}</DialogDescription>
          </DialogHeader>

          <Tabs value={tab} onValueChange={setTab} className="flex-1 overflow-hidden flex flex-col">
            <TabsList>
              <TabsTrigger value="submit">{t("maint.contact.tabSubmit")}</TabsTrigger>
              <TabsTrigger value="requests">{t("maint.contact.tabRequests")}</TabsTrigger>
            </TabsList>

            <TabsContent value="submit" className="overflow-y-auto -mx-6 px-6">
              <div className="space-y-3 py-2">
                {/* OVHcloud US 没有 NIC 联系人体系，后端会直接 400。
                    与其让用户填完再吃一个错误，不如在入口就说清楚并禁掉提交。 */}
                {usUnsupported && (
                  <div className="flex items-start gap-2 rounded-xl border border-warning/40 bg-warning/5 px-3 py-2 text-[12px]">
                    <AlertCircle className="w-4 h-4 text-warning flex-shrink-0 mt-0.5" />
                    <span>{t("maint.contact.usUnsupported")}</span>
                  </div>
                )}
                <ContactField label={t("maint.contact.fields.admin")} placeholder={t("maint.contact.fieldPlaceholder")} value={admin} onChange={setAdmin} disabled={usUnsupported} />
                <ContactField label={t("maint.contact.fields.tech")} placeholder={t("maint.contact.fieldPlaceholder")} value={tech} onChange={setTech} disabled={usUnsupported} />
                <ContactField label={t("maint.contact.fields.billing")} placeholder={t("maint.contact.fieldPlaceholder")} value={billing} onChange={setBilling} disabled={usUnsupported} />
                <p className="text-[11px] text-muted-foreground">{t("maint.contact.fieldHint")}</p>
                <div className="pt-2">
                  <Button onClick={handleSubmit} disabled={submit.isPending || usUnsupported}>
                    {submit.isPending ? t("maint.contact.submitting") : t("maint.contact.submitBtn")}
                  </Button>
                </div>
              </div>
            </TabsContent>

            <TabsContent value="requests" className="overflow-y-auto -mx-6 px-6">
              {list.isPending ? (
                <Skeleton className="h-40 rounded-2xl" />
              ) : list.data?.unsupported ? (
                // 501 = 该区（US）根本没有 /me/task/contactChange 系列端点。
                // 这是「没有这个能力」而不是「请求失败」，别渲染成可重试的错误。
                <EmptyState
                  icon={Mail}
                  title={t("maint.contact.unsupportedTitle")}
                  description={list.data.message || t("maint.contact.unsupportedDesc")}
                />
              ) : list.isError ? (
                <EmptyState
                  icon={Mail}
                  title={t("maint.contact.loadFailedTitle")}
                  description={errorMessage(list.error)}
                />
              ) : (list.data?.requests || []).length === 0 ? (
                <EmptyState icon={Mail} title={t("maint.contact.emptyRequests")} />
              ) : (
                <div className="space-y-2 py-2">
                  <PartialNotice failedCount={list.data?.failedCount || 0} what={t("maint.contact.partialWhat")} />
                  {(list.data?.requests || []).map((req: any) => (
                    <RequestRow
                      key={req.id}
                      req={req}
                      onAccept={() => {
                        setTokenTarget({ id: req.id, mode: "accept" });
                      }}
                      onRefuse={() => {
                        setTokenTarget({ id: req.id, mode: "refuse" });
                      }}
                      onResend={() => handleAction(req.id, "resend")}
                      busy={action.isPending}
                    />
                  ))}
                </div>
              )}
            </TabsContent>
          </Tabs>

          <DialogFooter>
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              {t("common.close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Token 子对话框：接受/拒绝时输入邮件里的 token */}
      <Dialog open={!!tokenTarget} onOpenChange={(v) => !v && setTokenTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <KeyRound className="w-5 h-5" />
              {t("maint.contact.tokenTitle")}
            </DialogTitle>
            <DialogDescription>
              {t("maint.contact.tokenDesc", {
                action:
                  tokenTarget?.mode === "accept"
                    ? t("maint.contact.actionAccept")
                    : t("maint.contact.actionRefuse"),
              })}
            </DialogDescription>
          </DialogHeader>
          <Input
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder={t("maint.contact.tokenPlaceholder")}
            autoComplete="off"
            spellCheck={false}
          />
          {/* 这个 token 和 API 密钥长得像但完全是两回事,写清楚省得用户去翻 .env。
              过期/用过都会失败,而列表里正好有「重发邮件」,直接指过去。 */}
          <p className="text-[11px] text-muted-foreground">
            <Trans
              i18nKey="maint.contact.tokenHint"
              components={{ b: <b />, code: <code className="px-1 bg-muted rounded" /> }}
            />
          </p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setTokenTarget(null)}>
              {t("common.cancel")}
            </Button>
            <Button
              disabled={!token || action.isPending}
              onClick={() => tokenTarget && handleAction(tokenTarget.id, tokenTarget.mode, token)}
            >
              {action.isPending
                ? t("maint.contact.submitting")
                : tokenTarget?.mode === "accept"
                  ? t("maint.contact.acceptBtn")
                  : t("maint.contact.refuseBtn")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

function ContactField({
  label,
  placeholder,
  value,
  onChange,
  disabled,
}: {
  label: string;
  placeholder: string;
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
}) {
  return (
    <div>
      <label className="text-[12px] font-semibold block mb-1.5">{label}</label>
      <Input placeholder={placeholder} value={value} onChange={(e) => onChange(e.target.value)} disabled={disabled} />
    </div>
  );
}

function RequestRow({
  req,
  onAccept,
  onRefuse,
  onResend,
  busy,
}: {
  req: any;
  onAccept: () => void;
  onRefuse: () => void;
  onResend: () => void;
  busy: boolean;
}) {
  const { t } = useTranslation();
  // 旧前端字段：state / serviceDomain / fromAccount / toAccount / askingAccount / contactTypes(数组) / dateRequest / dateDone
  const state = String(req.state || "").toLowerCase();
  const tone =
    state === "done"
      ? "success"
      : state === "refused" || state === "cancelled"
        ? "default"
        : "warning";
  const types = Array.isArray(req.contactTypes) ? req.contactTypes.join(" / ") : "";
  const canAct = state === "todo" || state === "doing" || state === "validatingbycustomers";
  return (
    <div className="border border-border rounded-2xl p-3 space-y-1.5">
      <div className="flex items-center gap-2 flex-wrap">
        <span className="font-mono font-semibold text-[12px]">#{req.id}</span>
        <Chip tone={tone}>{req.state || "—"}</Chip>
        {types && <span className="text-[11px] text-muted-foreground">{types}</span>}
      </div>
      {req.serviceDomain && (
        <p className="text-[12px] font-mono break-all">{req.serviceDomain}</p>
      )}
      {(req.fromAccount || req.toAccount) && (
        <p className="text-[11px] text-muted-foreground font-mono">
          {req.fromAccount || "—"} → {req.toAccount || "—"}
          {req.askingAccount && (
            <span className="ml-2">{t("maint.contact.askingAccount", { who: req.askingAccount })}</span>
          )}
        </p>
      )}
      <p className="text-[11px] text-muted-foreground">
        {req.dateRequest ? t("maint.contact.requestTime", { time: fmtDateTime(req.dateRequest) }) : ""}
        {req.dateDone && (
          <span className="ml-2">{t("maint.contact.doneTime", { time: fmtDateTime(req.dateDone) })}</span>
        )}
      </p>
      {canAct && (
        <div className="flex gap-1.5 pt-1">
          <Button size="sm" variant="outline" onClick={onAccept} disabled={busy}>
            <Check className="w-3.5 h-3.5 mr-1" />
            {t("maint.contact.accept")}
          </Button>
          <Button size="sm" variant="outline" onClick={onRefuse} disabled={busy}>
            <XIcon className="w-3.5 h-3.5 mr-1" />
            {t("maint.contact.refuse")}
          </Button>
          <Button size="sm" variant="outline" onClick={onResend} disabled={busy}>
            <RefreshCw className="w-3.5 h-3.5 mr-1" />
            {t("maint.contact.resend")}
          </Button>
        </div>
      )}
    </div>
  );
}
