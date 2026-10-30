import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ExternalLink, Loader2, MessageCircle, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { errorMessage } from "@/components/common/LoadFailed";
import { useAccountScope } from "@/hooks/use-account";
import { useAccounts } from "@/hooks/use-accounts";
import { authorizeOfficialChat, type ChatIdentity, type ChatProduct } from "@/lib/official-chat";

/** 聊天跟随全站账户选择,切换实际账户时卸载旧会话。 */
export function OfficialChat() {
  const accountId = useAccountScope();
  return <OfficialChatBody key={accountId} accountId={accountId} />;
}

/** 使用独立官方窗口遵守 OVH 禁止第三方内嵌的策略;不收集聊天内容。 */
function OfficialChatBody({ accountId }: { accountId: string }) {
  const { t } = useTranslation();
  const accounts = useAccounts();
  const account = accounts.data?.find((a) => a.id === accountId);
  const [product, setProduct] = useState<ChatProduct>("dedicated");
  const [busy, setBusy] = useState(false);
  const [identity, setIdentity] = useState<ChatIdentity | null>(null);
  const [error, setError] = useState<unknown>(null);
  const popup = useRef<Window | null>(null);
  const pending = useRef<AbortController | null>(null);

  /** 取消迟到响应并关闭旧窗口,不允许新账户沿用其认证链接。 */
  const release = () => {
    pending.current?.abort();
    pending.current = null;
    popup.current?.close();
    popup.current = null;
  };
  useEffect(() => release, []);

  /** 同步打开空窗口避免异步认证被拦截;明确点击后才生成一次性令牌。 */
  const open = async () => {
    if (!accountId || busy) return;
    release();
    setError(null);
    setIdentity(null);
    const target = window.open("about:blank", "_blank", "popup,width=480,height=760");
    if (!target) { setError(new Error("CHAT_POPUP_BLOCKED")); return; }
    target.opener = null;
    target.document.title = t("liveChat.preparing");
    target.document.body.textContent = t("liveChat.preparingAccount", { name: account?.name || accountId });
    popup.current = target;
    const controller = new AbortController();
    pending.current = controller;
    setBusy(true);
    try {
      const result = await authorizeOfficialChat(accountId, product, target, controller.signal);
      if (!controller.signal.aborted) setIdentity(result);
    } catch (cause) {
      if (!controller.signal.aborted) {
        target.close();
        popup.current = null;
        setError(cause);
      }
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  };
  /** 用户主动关闭窗口时同时取消尚未完成的认证。 */
  const close = () => { release(); setBusy(false); setIdentity(null); };
  const localError = error instanceof Error && ["CHAT_POPUP_BLOCKED", "CHAT_INVALID_RESPONSE"].includes(error.message)
    ? t(`liveChat.errors.${error.message}`) : error ? errorMessage(error) : "";
  const helpUrl = account?.endpoint === "ovh-us" ? "https://support.us.ovhcloud.com/" : "https://help.ovhcloud.com/csm?id=csm_get_help";
  return (
    <section className="rounded-2xl border border-border bg-card p-5 sm:p-6">
      <header className="flex items-start gap-3">
        <MessageCircle aria-hidden="true" className="mt-1 size-5 shrink-0 text-muted-foreground" />
        <div><h2 className="text-lg font-semibold">{t("liveChat.title")}</h2><p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">{t("liveChat.description")}</p></div>
      </header>
      <div className="mt-6 space-y-5">
        <div className="rounded-xl bg-muted/50 p-4">
          <p className="text-xs text-muted-foreground">{t("liveChat.account")}</p>
          <p className="mt-1 break-words font-medium">{account?.name || "—"}{account && <span className="ml-2 text-sm text-muted-foreground">{account.zone} · {account.endpoint}</span>}</p>
          <p className="mt-2 text-xs leading-5 text-muted-foreground">{t("liveChat.switchHint")}</p>
        </div>
        <div className="max-w-sm space-y-2">
          <label htmlFor="official-chat-product" className="text-sm font-medium">{t("liveChat.product")}</label>
          <Select value={product} onValueChange={(v) => setProduct(v as ChatProduct)} disabled={busy}>
            <SelectTrigger id="official-chat-product" className="h-11"><SelectValue /></SelectTrigger>
            <SelectContent><SelectItem value="dedicated">{t("liveChat.dedicated")}</SelectItem><SelectItem value="vps">VPS</SelectItem></SelectContent>
          </Select>
        </div>
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground">{t("liveChat.expectation")}</p>
        {identity && <p role="status" className="text-sm leading-6">{t("liveChat.opened", { name: identity.accountName, code: identity.customerCode })}</p>}
        {!!localError && <p role="alert" className="break-words text-sm leading-6 text-destructive">{localError}</p>}
        <div className="flex flex-wrap gap-3">
          <Button className="min-h-11" onClick={open} disabled={busy || !accountId || !account}>
            {busy ? <Loader2 aria-hidden="true" className="size-4 animate-spin motion-reduce:animate-none" /> : <ExternalLink aria-hidden="true" className="size-4" />}
            {t(busy ? "liveChat.preparing" : identity ? "liveChat.reopen" : "liveChat.open")}
          </Button>
          {(busy || identity) && <Button className="min-h-11" variant="outline" onClick={close}><X aria-hidden="true" className="size-4" />{t("liveChat.close")}</Button>}
          <Button className="min-h-11" variant="ghost" asChild><a href={helpUrl} target="_blank" rel="noopener noreferrer">{t("liveChat.help")}<ExternalLink aria-hidden="true" className="size-4" /></a></Button>
        </div>
        <p className="text-xs leading-5 text-muted-foreground">{t("liveChat.permissions")}</p>
      </div>
    </section>
  );
}
