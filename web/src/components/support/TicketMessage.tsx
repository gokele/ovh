import { useTranslation } from "react-i18next";
import { Headphones, UserRound } from "lucide-react";
import { fmtDateTime } from "@/i18n/format";
import type { SupportDetail } from "@/hooks/use-support";

/** 客服居左、我方居右,纯文本正文保留换行且不会执行 HTML。 */
export function TicketMessage({
  message,
}: {
  message: SupportDetail["messages"][number];
}) {
  const { t } = useTranslation();
  const customer = message.from === "customer";
  const Icon = customer ? UserRound : Headphones;
  return (
    <article
      className={`flex items-start gap-2 sm:gap-3 ${customer ? "flex-row-reverse" : ""}`}
    >
      <div
        className={`mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full ${customer ? "bg-muted text-muted-foreground" : "bg-foreground text-background"}`}
      >
        <Icon aria-hidden="true" className="h-4 w-4" />
      </div>
      <div
        className={`flex min-w-0 max-w-[85%] flex-col ${customer ? "items-end" : "items-start"}`}
      >
        <div
          className={`mb-2 flex flex-wrap items-center gap-x-3 gap-y-1 ${customer ? "flex-row-reverse justify-start" : ""}`}
        >
          <span className="text-sm font-medium">
            {t(`support.senders.${message.from}`, {
              defaultValue: message.from,
            })}
          </span>
          <time
            dateTime={message.creationDate}
            className="text-xs tabular-nums text-muted-foreground"
          >
            {message.creationDate ? fmtDateTime(message.creationDate) : "—"}
          </time>
        </div>
        <p
          className={`max-w-prose whitespace-pre-wrap rounded-2xl px-4 py-3 text-base leading-7 [overflow-wrap:anywhere] ${customer ? "rounded-tr-sm bg-foreground text-background" : "rounded-tl-sm border border-border bg-background"}`}
        >
          {message.body}
        </p>
      </div>
    </article>
  );
}
