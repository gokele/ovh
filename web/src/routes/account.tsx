import { createFileRoute } from "@tanstack/react-router";
import { User, Mail, RefreshCw, FileText, Inbox, ShieldCheck, type LucideIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { fmtDateTime } from "@/i18n/format";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Chip } from "@/components/common/Chip";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { PartialNotice } from "@/components/common/PartialNotice";
import { LoadFailed, LoadFailedBanner } from "@/components/common/LoadFailed";
import { useAccountInfo, useRefunds, useEmails, type EmailHistoryEntry } from "@/hooks/use-account";

/** 账户管理：顶部 3 张 KPI + Tabs (邮件 / 退款) */
export const Route = createFileRoute("/account")({
  component: AccountPage,
});

function AccountPage() {
  const { t } = useTranslation();
  const q = useAccountInfo();
  // 后端把 OVH /me 原样透传在 body 里,错配标记只能放响应头,所以 hook 返回 { info, subsidiaryMismatch }
  const me = q.data?.info;
  const loading = q.isPending;
  return (
    <div className="space-y-3 sm:space-y-6">
      <PageHeader icon={User} title={t("account.title")} description={t("account.description")} />

      {/* 后端在 /me 响应上打的 X-Subsidiary-Mismatch:账户里配的 zone 与 OVH 认定的
          ovhSubsidiary 不在一起。凭据是有效的,所以别的地方一切正常,只有目录/价格/下单
          会静默走错站点 —— 这是用户在下单失败之前唯一能看到的信号,放在最显眼处。 */}
      {q.data?.subsidiaryMismatch && (
        <div className="flex items-start gap-2 rounded-xl border border-warning/40 bg-warning/5 px-3 py-2 text-[12px]">
          <ShieldCheck className="w-4 h-4 text-warning flex-shrink-0 mt-0.5" />
          <div>
            <p className="font-semibold">{t("account.mismatchTitle")}</p>
            <p className="text-muted-foreground mt-0.5">
              {t("account.mismatchDetailPre")}<code className="font-mono">{q.data.info.ovhSubsidiary || "—"}</code>{t("account.mismatchDetailPost")}
            </p>
          </div>
        </div>
      )}

      {/* /me 请求挂了的话 me 为 undefined:三张 KPI 全变「—」、KYC 标签整个消失。
          那副样子跟"这个账户就是没填这些信息 / 没做过 KYC"完全一样,而 KYC 没过 OVH 是会拦下单的
          —— 用户照着这页判断自己的验证状态,两个方向都可能判错。所以整页必须先说一句"没读到"。 */}
      {q.isError && (
        <LoadFailedBanner
          title={t("account.loadFailedTitle")}
          error={q.error}
          onRetry={() => q.refetch()}
        />
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <KpiCard
          icon={User}
          label={t("account.customerCode")}
          value={me?.customerCode}
          sub={me?.nichandle}
          loading={loading}
          badge={
            me && (
              <Chip tone={me.kycValidated ? "success" : "warning"}>
                <ShieldCheck className="w-3 h-3" />
                {me.kycValidated ? t("account.kycValidated") : t("account.kycNotValidated")}
              </Chip>
            )
          }
        />
        <KpiCard icon={Mail} label={t("account.email")} value={me?.email} loading={loading} />
        <KpiCard
          icon={User}
          label={t("account.holder")}
          value={me ? `${me.firstname ?? ""} ${me.name ?? ""}`.trim() : undefined}
          sub={me?.city && me?.country ? `${me.city}, ${me.country}` : undefined}
          loading={loading}
        />
      </div>

      <Tabs defaultValue="emails">
        <TabsList>
          <TabsTrigger value="emails">{t("account.emailsTab")}</TabsTrigger>
          <TabsTrigger value="refunds">{t("account.refundsTab")}</TabsTrigger>
        </TabsList>
        <TabsContent value="emails">
          <EmailsTab />
        </TabsContent>
        <TabsContent value="refunds">
          <RefundsTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function KpiCard({
  icon: Icon,
  label,
  value,
  sub,
  loading,
  badge,
}: {
  icon: LucideIcon;
  label: string;
  value?: string;
  sub?: string;
  loading?: boolean;
  badge?: React.ReactNode;
}) {
  return (
    <Card>
      <CardContent className="p-5 flex items-start gap-3">
        <div className="w-10 h-10 rounded-full bg-secondary flex items-center justify-center flex-shrink-0">
          <Icon className="w-5 h-5" strokeWidth={1.75} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-2">
            <p className="text-[12px] text-muted-foreground">{label}</p>
            {badge}
          </div>
          {loading ? (
            <Skeleton className="h-6 w-32 mt-1" />
          ) : (
            <p className="text-lg font-bold truncate" title={value}>{value || "—"}</p>
          )}
          {sub && <p className="text-[11px] text-muted-foreground mt-0.5 truncate">{sub}</p>}
        </div>
      </CardContent>
    </Card>
  );
}

function EmailsTab() {
  const { t } = useTranslation();
  const emails = useEmails();
  const [selected, setSelected] = useState<EmailHistoryEntry | null>(null);

  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <Card className="overflow-hidden">
        <div className="px-4 py-3 border-b border-border flex items-center justify-between">
          <span className="text-sm font-semibold">{t("account.emailList")}</span>
          <Button variant="outline" size="sm" onClick={() => emails.refetch()} disabled={emails.isFetching}>
            <RefreshCw className={`w-3.5 h-3.5 ${emails.isFetching ? "animate-spin" : ""}`} />
            {t("common.refresh")}
          </Button>
        </div>
        {emails.isPending ? (
          <div className="p-4 space-y-2">
            {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-14 rounded-xl" />)}
          </div>
        ) : emails.isError ? (
          /* 整条请求挂掉时 data 是 undefined,下面 `failedCount || 0` 只会读出 0,
             于是一路掉进「暂无邮件」—— 那是"部分失败"的兜底,盖不住"整个请求失败"。
             OVH 的下单确认、付款提醒、账户告警全在这条历史里,说成"没有邮件"
             等于告诉用户 OVH 从没通知过他。 */
          <div className="p-4">
            <LoadFailed
              icon={Inbox}
              title={t("account.emailsFailedTitle")}
              error={emails.error}
              onRetry={() => emails.refetch()}
              compact
            />
          </div>
        ) : (emails.data?.items || []).length === 0 ? (
          /* 空列表 + failedCount>0 = 这次一条都没读到，不是"账户没有邮件" */
          (emails.data?.failedCount || 0) > 0 ? (
            <EmptyState
              icon={Inbox}
              title={t("account.emailsPartialTitle")}
              description={t("account.emailsPartialDesc", { count: emails.data?.failedCount || 0 })}
            />
          ) : (
            <EmptyState icon={Inbox} title={t("account.noEmails")} />
          )
        ) : (
          <div className="max-h-[500px] overflow-y-auto">
            <PartialNotice failedCount={emails.data?.failedCount || 0} what={t("account.emailsTab")} className="m-3" />
            <div className="divide-y divide-border">
            {(emails.data?.items || []).map((e) => (
              <button
                key={e.id}
                type="button"
                onClick={() => setSelected(e)}
                className={
                  "w-full text-left px-4 py-3 hover:bg-muted transition-colors " +
                  (selected?.id === e.id ? "bg-secondary" : "")
                }
              >
                <div className="flex items-center gap-2 mb-1">
                  <Mail className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
                  <p className="text-[13px] font-medium truncate">{e.subject}</p>
                </div>
                <p className="text-[11px] text-muted-foreground">{fmtDateTime(e.date)}</p>
              </button>
            ))}
            </div>
          </div>
        )}
      </Card>

      <Card>
        <CardContent className="p-5">
          <div className="flex items-center gap-2 mb-4">
            <FileText className="w-4 h-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">{t("account.emailDetail")}</h3>
          </div>
          {selected ? (
            <>
              <p className="text-[15px] font-semibold mb-1">{selected.subject}</p>
              <p className="text-[12px] text-muted-foreground mb-4">{fmtDateTime(selected.date)}</p>
              <pre className="text-[12px] font-mono whitespace-pre-wrap text-foreground bg-secondary rounded-lg p-3 max-h-[400px] overflow-y-auto">
                {selected.body}
              </pre>
            </>
          ) : (
            <EmptyState icon={Mail} title={t("account.selectEmail")} />
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function RefundsTab() {
  const { t } = useTranslation();
  const refunds = useRefunds();
  return (
    <Card>
      <CardContent className="p-5">
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-sm font-semibold">{t("account.refundsTab")}</h3>
          <Button variant="outline" size="sm" onClick={() => refunds.refetch()} disabled={refunds.isFetching}>
            <RefreshCw className={`w-3.5 h-3.5 ${refunds.isFetching ? "animate-spin" : ""}`} />
            {t("common.refresh")}
          </Button>
        </div>
        {refunds.isPending ? (
          <div className="space-y-2">
            {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-16 rounded-xl" />)}
          </div>
        ) : refunds.isError ? (
          /* 跟邮件同一个盲区:请求整条挂掉 → data 为 undefined → failedCount 算成 0 →
             落到「暂无退款记录」。这块是钱:用户会据此认定"OVH 没退给我",然后去开工单/发起争议,
             而实际上我们只是没问到。失败就老实说失败。 */
          <LoadFailed
            icon={Inbox}
            title={t("account.refundsFailedTitle")}
            error={refunds.error}
            onRetry={() => refunds.refetch()}
            compact
          />
        ) : (refunds.data?.items || []).length === 0 ? (
          (refunds.data?.failedCount || 0) > 0 ? (
            <EmptyState
              icon={Inbox}
              title={t("account.refundsPartialTitle")}
              description={t("account.refundsPartialDesc", { count: refunds.data?.failedCount || 0 })}
            />
          ) : (
            <EmptyState icon={Inbox} title={t("account.noRefunds")} />
          )
        ) : (
          <div>
            <PartialNotice failedCount={refunds.data?.failedCount || 0} what={t("account.refundsTab")} className="mb-3" />
            <div className="divide-y divide-border">
            {(refunds.data?.items || []).map((r) => (
              <div key={r.refundId} className="py-3 flex items-center justify-between gap-4">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="font-mono text-sm font-semibold">#{r.refundId}</span>
                    <Chip tone="default">{t("account.orderChip", { orderId: r.orderId })}</Chip>
                  </div>
                  <p className="text-[11px] text-muted-foreground">{fmtDateTime(r.date)}</p>
                </div>
                <div className="text-right flex-shrink-0">
                  <p className="text-lg font-bold text-success">{r.priceWithTax.text}</p>
                  {r.pdfUrl && (
                    <a href={r.pdfUrl} target="_blank" rel="noopener noreferrer" className="text-[11px] text-foreground hover:underline">
                      {t("account.downloadPdf")}
                    </a>
                  )}
                </div>
              </div>
            ))}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
