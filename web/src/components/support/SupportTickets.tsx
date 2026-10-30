import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ArrowRight,
  ExternalLink,
  LifeBuoy,
  Plus,
  RefreshCw,
  Search,
  UserRound,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { LoadFailed } from "@/components/common/LoadFailed";
import { EmptyState } from "@/components/common/EmptyState";
import { PartialNotice } from "@/components/common/PartialNotice";
import { Skeleton } from "@/components/common/Skeleton";
import { useAccountScope } from "@/hooks/use-account";
import { useSupportTickets, type SupportTicket } from "@/hooks/use-support";
import { fmtDateTime } from "@/i18n/format";
import { bodyMessage } from "@/lib/api-error";
import { NewTicketDialog } from "./NewTicketDialog";
import { TicketDetailDialog } from "./TicketDetailDialog";
import { TicketState } from "./TicketState";

/** 切换账户时清除工单和草稿,后续操作固定实际账户。 */
export function SupportTickets() {
  const accountId = useAccountScope();
  return <SupportTicketsBody key={accountId} accountId={accountId} />;
}

/** 工单收件箱采用清晰列层级;搜索在服务端分页之前进行。 */
function SupportTicketsBody({ accountId }: { accountId: string }) {
  const { t } = useTranslation();
  const [page, setPage] = useState(0);
  const [status, setStatus] = useState("");
  const [search, setSearch] = useState("");
  const [subject, setSubject] = useState("");
  const [creating, setCreating] = useState(false);
  const [selected, setSelected] = useState<{
    id: number;
    account: string;
  } | null>(null);
  const list = useSupportTickets(accountId, page, status, subject);
  const actualAccount = list.data?.accountId;
  useEffect(() => {
    const timer = setTimeout(() => {
      setSubject(search.trim());
      setPage(0);
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);
  /** 捕获实际账户和内部 ID,显示号码不会用于调用接口。 */
  const openTicket = (ticket: SupportTicket) => {
    if (actualAccount)
      setSelected({ id: ticket.ticketId, account: actualAccount });
  };
  return (
    <>
      <section className="overflow-hidden rounded-2xl border border-border bg-card">
        <header className="flex flex-col gap-4 px-5 py-6 sm:flex-row sm:items-start sm:justify-between sm:px-6">
          <div className="space-y-2">
            <div className="flex flex-wrap items-center gap-3">
              <h2 className="text-xl font-semibold tracking-tight">
                {t("support.title")}
              </h2>
              {list.data && (
                <span className="inline-flex items-center gap-1.5 rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground">
                  <UserRound aria-hidden="true" className="h-3.5 w-3.5" />
                  {list.data.accountName || actualAccount}
                </span>
              )}
            </div>
            <p className="text-sm leading-6 text-muted-foreground">
              {t("support.listHint")}
            </p>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <Button
              variant="ghost"
              size="icon"
              className="h-11 w-11"
              aria-label={t("common.refresh")}
              onClick={() => list.refetch()}
              disabled={list.isFetching}
            >
              <RefreshCw
                aria-hidden="true"
                className={
                  list.isFetching
                    ? "animate-spin motion-reduce:animate-none"
                    : ""
                }
              />
            </Button>
            <Button
              className="h-11"
              onClick={() => setCreating(true)}
              disabled={!actualAccount || list.isFetching}
            >
              <Plus aria-hidden="true" />
              {t("support.newTicket")}
            </Button>
          </div>
        </header>
        <div className="flex flex-col gap-3 border-y border-border bg-muted/20 px-5 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-6">
          <div
            className="inline-flex w-fit rounded-xl border border-border bg-background p-1"
            aria-label={t("support.status")}
          >
            {["", "open", "closed"].map((value) => (
              <button
                key={value}
                type="button"
                aria-pressed={status === value}
                onClick={() => {
                  setStatus(value);
                  setPage(0);
                }}
                className={`min-h-10 rounded-lg px-4 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${status === value ? "bg-foreground text-background" : "text-muted-foreground hover:bg-muted hover:text-foreground"}`}
              >
                {value
                  ? t(`support.statuses.${value}`)
                  : t("support.allTickets")}
              </button>
            ))}
          </div>
          <div className="relative sm:w-72">
            <Search
              aria-hidden="true"
              className="pointer-events-none absolute left-3 top-3.5 h-4 w-4 text-muted-foreground"
            />
            <Input
              className="h-11 rounded-xl pl-9"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              maxLength={255}
              aria-label={t("support.searchTickets")}
              placeholder={t("support.searchTickets")}
            />
          </div>
        </div>
        {list.isPending ? (
          <div
            className="space-y-5 p-6"
            role="status"
            aria-label={t("support.loading")}
          >
            {[0, 1, 2].map((n) => (
              <div key={n} className="flex items-center justify-between gap-4">
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-2/3" />
                  <Skeleton className="h-3 w-1/3" />
                </div>
                <Skeleton className="h-6 w-20 rounded-full" />
              </div>
            ))}
          </div>
        ) : list.isError ? (
          <div className="p-6">
            <LoadFailed
              error={list.error}
              onRetry={() => list.refetch()}
              compact
            />
          </div>
        ) : (
          <>
            <div className="px-5 sm:px-6">
              <PartialNotice
                failedCount={list.data?.failedCount || 0}
                what={t("support.title")}
                className="mt-4"
              />
            </div>
            {!list.data?.items.length ? (
              <EmptyState
                icon={LifeBuoy}
                title={t("support.empty")}
                description={t("support.emptyHint")}
              />
            ) : (
              <div>
                <div className="hidden grid-cols-[minmax(0,1fr)_150px_150px_100px_20px] gap-4 border-b px-6 py-3 text-xs font-medium text-muted-foreground lg:grid">
                  <span>{t("support.ticketColumn")}</span>
                  <span>{t("support.relatedService")}</span>
                  <span>{t("support.updated")}</span>
                  <span>{t("support.status")}</span>
                  <span />
                </div>
                {list.data.items.map((ticket) => (
                  <button
                    key={ticket.ticketId}
                    type="button"
                    className="group grid w-full gap-3 border-b border-border px-5 py-5 text-left transition-colors last:border-b-0 hover:bg-muted/35 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring sm:px-6 lg:grid-cols-[minmax(0,1fr)_150px_150px_100px_20px] lg:items-center lg:gap-4"
                    onClick={() => openTicket(ticket)}
                  >
                    <div className="min-w-0 space-y-1.5">
                      <div className="flex items-start justify-between gap-3">
                        <p className="text-base font-medium leading-6 [overflow-wrap:anywhere]">
                          {ticket.subject || t("support.detailUnavailable")}
                        </p>
                        <span className="lg:hidden">
                          <TicketState state={ticket.state} />
                        </span>
                      </div>
                      <p className="flex flex-wrap items-center gap-x-2 text-xs leading-5 text-muted-foreground">
                        <span className="tabular-nums">
                          #{ticket.ticketNumber ?? ticket.ticketId}
                        </span>
                        {ticket.category && (
                          <>
                            <span aria-hidden="true">·</span>
                            <span>
                              {t(`support.categories.${ticket.category}`, {
                                defaultValue: ticket.category,
                              })}
                            </span>
                          </>
                        )}
                      </p>
                      {ticket.error && (
                        <p className="break-words text-sm text-destructive">
                          {bodyMessage(ticket)}
                        </p>
                      )}
                    </div>
                    <p
                      className="min-w-0 truncate text-sm text-muted-foreground"
                      title={ticket.serviceName}
                    >
                      {ticket.serviceName ||
                        (ticket.product
                          ? t(`support.products.${ticket.product}`, {
                              defaultValue: ticket.product,
                            })
                          : "—")}
                    </p>
                    <p className="text-xs tabular-nums text-muted-foreground">
                      {ticket.updateDate ? fmtDateTime(ticket.updateDate) : "—"}
                    </p>
                    <span className="hidden lg:block">
                      <TicketState state={ticket.state} />
                    </span>
                    <ArrowRight
                      aria-hidden="true"
                      className="hidden h-4 w-4 text-muted-foreground transition-transform group-hover:translate-x-0.5 motion-reduce:transform-none lg:block"
                    />
                  </button>
                ))}
              </div>
            )}
            <footer className="flex flex-wrap items-center justify-between gap-3 border-t border-border px-5 py-4 sm:px-6">
              <a
                href="https://help.ovhcloud.com/"
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex min-h-11 items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground hover:underline"
              >
                <ExternalLink aria-hidden="true" className="h-3.5 w-3.5" />
                {t("support.helpCenter")}
              </a>
              <div className="flex items-center gap-2">
                <Button
                  className="h-11"
                  size="sm"
                  variant="outline"
                  disabled={page === 0 || list.isFetching}
                  onClick={() => setPage(page - 1)}
                >
                  {t("support.previous")}
                </Button>
                <span className="px-1 text-xs text-muted-foreground">
                  {t("support.page", { page: page + 1 })}
                </span>
                <Button
                  className="h-11"
                  size="sm"
                  variant="outline"
                  disabled={!list.data?.hasMore || list.isFetching}
                  onClick={() => setPage(page + 1)}
                >
                  {t("support.next")}
                </Button>
              </div>
            </footer>
          </>
        )}
      </section>
      {creating && actualAccount && (
        <NewTicketDialog
          accountId={actualAccount}
          onClose={() => setCreating(false)}
          onView={(id) => {
            setCreating(false);
            setSelected({ id, account: actualAccount });
          }}
        />
      )}
      {selected && (
        <TicketDetailDialog
          key={`${selected.account}:${selected.id}`}
          id={selected.id}
          accountId={selected.account}
          onClose={() => setSelected(null)}
        />
      )}
    </>
  );
}
