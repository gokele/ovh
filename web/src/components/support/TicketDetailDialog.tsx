import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  CheckCircle2,
  ExternalLink,
  Loader2,
  RotateCcw,
  Send,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { LoadFailed, errorMessage } from "@/components/common/LoadFailed";
import { fmtDateTime } from "@/i18n/format";
import { useSupportAction, useSupportDetail } from "@/hooks/use-support";
import { TicketState } from "./TicketState";
import { TicketMessage } from "./TicketMessage";

/** 对话与工单资料分区;正文始终作为纯文本显示,操作保留显式确认。 */
export function TicketDetailDialog({
  accountId,
  id,
  onClose,
}: {
  accountId: string;
  id: number;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const detail = useSupportDetail(accountId, id);
  const mutation = useSupportAction(accountId, id);
  const [mode, setMode] = useState<"reply" | "close" | "reopen">("reply");
  const [body, setBody] = useState("");
  const history = useRef<HTMLDivElement>(null);
  // 首次读取及新增回复后显示最新消息,普通状态刷新不打断阅读位置。
  useEffect(() => {
    if (history.current)
      history.current.scrollTop = history.current.scrollHeight;
  }, [detail.data?.messages.length]);
  const ticket = detail.data?.ticket;
  const open = ticket?.state === "open";
  const closed = ticket?.state === "closed";
  const allowed =
    (mode === "reply" && open) ||
    (mode === "reopen" && closed) ||
    (mode === "close" && open && ticket?.canBeClosed);
  const busy = mutation.isPending || detail.isFetching;
  /** 状态冲突由服务端再次核对,失败后保留正文且不自动重发。 */
  const submit = async () => {
    if (!allowed || busy || (mode !== "close" && !body.trim())) return;
    try {
      await mutation.mutateAsync({
        action: mode,
        body: mode === "close" ? undefined : body.trim(),
      });
      setBody("");
      setMode("reply");
    } catch {
      /* Display inline. */
    }
  };
  const facts = [
    {
      label: t("support.account"),
      value: detail.data?.accountName || accountId,
    },
    {
      label: t("support.category"),
      value: ticket?.category
        ? t(`support.categories.${ticket.category}`, {
            defaultValue: ticket.category,
          })
        : "—",
    },
    {
      label: t("support.product"),
      value: ticket?.product
        ? t(`support.products.${ticket.product}`, {
            defaultValue: ticket.product,
          })
        : "—",
    },
    { label: t("support.relatedService"), value: ticket?.serviceName || "—" },
    {
      label: t("support.updated"),
      value: ticket?.updateDate ? fmtDateTime(ticket.updateDate) : "—",
    },
  ];
  const metadata = (
    <dl className="space-y-5">
      {facts.map((fact) => (
        <div key={fact.label}>
          <dt className="text-xs text-muted-foreground">{fact.label}</dt>
          <dd className="mt-1.5 break-words text-sm leading-6 [overflow-wrap:anywhere]">
            {fact.value}
          </dd>
        </div>
      ))}
    </dl>
  );
  return (
    <Dialog
      open
      onOpenChange={(value) => {
        if (!value && !mutation.isPending) onClose();
      }}
    >
      <DialogContent
        className="flex h-[88dvh] flex-col gap-0 overflow-hidden p-0 sm:h-[min(820px,85dvh)] sm:max-w-5xl sm:p-0"
        style={{ paddingBottom: 0 }}
      >
        <DialogHeader className="shrink-0 border-b border-border px-5 pb-5 pt-6 sm:px-7">
          <DialogTitle className="pr-9 text-lg leading-7 [overflow-wrap:anywhere] sm:text-xl">
            {ticket?.subject || t("support.detailHint")}
          </DialogTitle>
          <DialogDescription asChild>
            <div className="flex flex-wrap items-center gap-3 pt-1">
              <span className="text-xs tabular-nums">
                {t("support.ticketNumber", {
                  number: ticket?.ticketNumber ?? id,
                })}
              </span>
              <TicketState state={ticket?.state} />
            </div>
          </DialogDescription>
        </DialogHeader>
        {detail.isPending ? (
          <p role="status" className="p-7">
            {t("support.loading")}
          </p>
        ) : detail.isError ? (
          <div className="p-7">
            <LoadFailed
              error={detail.error}
              onRetry={() => detail.refetch()}
              compact
            />
          </div>
        ) : (
          detail.data && (
            <div className="grid min-h-0 flex-1 lg:grid-cols-[minmax(0,1fr)_240px]">
              <div className="flex min-h-0 min-w-0 flex-col">
                <details className="max-h-48 shrink-0 overflow-y-auto border-b border-border px-5 py-3 lg:hidden">
                  <summary className="cursor-pointer rounded text-sm font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                    {t("support.ticketInfo")}
                  </summary>
                  <div className="py-4">{metadata}</div>
                </details>
                <div
                  ref={history}
                  className="min-h-0 flex-1 space-y-6 overflow-y-auto overscroll-contain px-5 py-6 sm:px-7"
                  aria-label={t("support.messages")}
                  tabIndex={0}
                >
                  {detail.data.messages.length ? (
                    [...detail.data.messages]
                      .sort(
                        (a, b) =>
                          new Date(a.creationDate).getTime() -
                          new Date(b.creationDate).getTime(),
                      )
                      .map((message) => (
                        <TicketMessage
                          key={message.messageId}
                          message={message}
                        />
                      ))
                  ) : (
                    <p className="py-8 text-sm text-muted-foreground">
                      {t("support.noMessages")}
                    </p>
                  )}
                </div>
                <div className="max-h-[50dvh] shrink-0 space-y-3 overflow-y-auto border-t border-border bg-muted/20 px-5 py-4 pb-[max(env(safe-area-inset-bottom),1rem)] sm:px-7">
                  {allowed && mode !== "close" && (
                    <div className="space-y-2">
                      <label
                        htmlFor="support-reply-body"
                        className="text-sm font-medium"
                      >
                        {mode === "reopen"
                          ? t("support.reopenReason")
                          : t("support.replyBody")}
                      </label>
                      <Textarea
                        id="support-reply-body"
                        className="min-h-24 resize-y rounded-xl bg-background text-base leading-7"
                        value={body}
                        onChange={(event) => setBody(event.target.value)}
                        placeholder={t(
                          mode === "reopen"
                            ? "support.reopenPlaceholder"
                            : "support.replyPlaceholder",
                        )}
                        lang="en"
                        spellCheck
                        aria-describedby="support-reply-language"
                        disabled={busy}
                      />
                      <p
                        id="support-reply-language"
                        className="text-xs leading-5 text-muted-foreground"
                      >
                        {t("support.englishHint")}
                      </p>
                    </div>
                  )}
                  {mode === "close" && allowed && (
                    <div className="flex items-start gap-3 rounded-xl border border-border bg-background p-4">
                      <CheckCircle2
                        aria-hidden="true"
                        className="mt-0.5 h-5 w-5 shrink-0"
                      />
                      <p className="text-sm leading-6">
                        {t("support.closeConfirm")}
                      </p>
                    </div>
                  )}
                  {closed && mode !== "reopen" && (
                    <p className="text-sm leading-6 text-muted-foreground">
                      {t("support.closedHint")}
                    </p>
                  )}
                  {!open && !closed && (
                    <p className="text-sm text-warning">
                      {t("support.unknownState")}
                    </p>
                  )}
                  {mutation.isError && (
                    <p
                      role="alert"
                      className="break-words text-sm leading-6 text-destructive"
                    >
                      {errorMessage(mutation.error)}
                    </p>
                  )}
                  {mutation.isSuccess && (
                    <p role="status" className="text-sm text-success">
                      {t("support.actionSubmitted")}
                    </p>
                  )}
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <div>
                      {open && (
                        <Button
                          variant="ghost"
                          className="h-11 px-2 text-muted-foreground"
                          onClick={() => {
                            setMode(mode === "close" ? "reply" : "close");
                            mutation.reset();
                          }}
                          disabled={busy || !ticket?.canBeClosed}
                        >
                          {mode === "close"
                            ? t("common.cancel")
                            : t("support.closeTicket")}
                        </Button>
                      )}
                      {closed && mode === "reopen" && (
                        <Button
                          className="h-11"
                          variant="ghost"
                          onClick={() => {
                            setMode("reply");
                            mutation.reset();
                          }}
                          disabled={busy}
                        >
                          {t("common.cancel")}
                        </Button>
                      )}
                    </div>
                    {closed && mode !== "reopen" ? (
                      <Button
                        className="h-11"
                        variant="outline"
                        onClick={() => {
                          setMode("reopen");
                          mutation.reset();
                        }}
                        disabled={busy}
                      >
                        <RotateCcw aria-hidden="true" />
                        {t("support.reopen")}
                      </Button>
                    ) : (
                      allowed && (
                        <Button
                          className="h-11 px-5"
                          onClick={submit}
                          disabled={busy || (mode !== "close" && !body.trim())}
                        >
                          {mutation.isPending ? (
                            <Loader2
                              aria-hidden="true"
                              className="animate-spin motion-reduce:animate-none"
                            />
                          ) : mode === "close" ? (
                            <CheckCircle2 aria-hidden="true" />
                          ) : (
                            <Send aria-hidden="true" />
                          )}
                          {mutation.isPending
                            ? t("support.sending")
                            : t(`support.actions.${mode}`)}
                        </Button>
                      )
                    )}
                  </div>
                </div>
              </div>
              <aside className="hidden overflow-y-auto border-l border-border bg-muted/20 px-5 py-6 lg:block">
                <h3 className="mb-6 text-sm font-semibold">
                  {t("support.ticketInfo")}
                </h3>
                {metadata}
                <a
                  href="https://help.ovhcloud.com/"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-6 flex min-h-11 items-center gap-2 text-xs leading-5 text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
                >
                  <ExternalLink
                    aria-hidden="true"
                    className="h-3.5 w-3.5 shrink-0"
                  />
                  {t("support.helpCenter")}
                </a>
              </aside>
            </div>
          )
        )}
      </DialogContent>
    </Dialog>
  );
}
