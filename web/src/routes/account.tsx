import { OfficialChat } from "@/components/support/OfficialChat";
import { SupportTickets } from "@/components/support/SupportTickets";
import { OrderPaymentButton } from "@/components/account/OrderPaymentButton";
import { createFileRoute } from "@tanstack/react-router";
import { User, Mail, RefreshCw, FileText, Inbox, ShieldCheck, ShoppingBag, ExternalLink, Wallet, MapPin, Globe, type LucideIcon } from "lucide-react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { fmtDate, fmtDateTime, fmtMoney } from "@/i18n/format";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Chip } from "@/components/common/Chip";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { PartialNotice } from "@/components/common/PartialNotice";
import { LoadFailed, LoadFailedBanner } from "@/components/common/LoadFailed";
import { useAccountInfo, useAccountScope, useCreditBalances, useRefunds, useEmails, useOrders, type AccountInfo, type CreditBalance, type EmailHistoryEntry, type OrderRecord } from "@/hooks/use-account";

/** 账户管理：顶部账户资料条 + Tabs (邮件 / 订单 / 退款 / 工单 / 在线客服) */
export const Route = createFileRoute("/account")({
  component: AccountPage,
});

function AccountPage() {
  const accountId = useAccountScope();
  // 只重置账户相关内容,保留用户正在查看的标签页。
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
        <div className="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/5 px-3 py-2 text-[12px]">
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

      {/* 读取失败时横幅已经说明;再摆三张「—」卡片只会占掉手机一整屏 */}
      {/* 账户总览:左侧身份卡(持有人 / 邮箱 / KYC + 客户代码、所在地、子公司),右侧蓝色钱包卡(可用余额) */}
      {!q.isError && (
        <div className="grid gap-3 sm:gap-4 xl:grid-cols-[minmax(0,1fr)_360px] items-stretch">
          <IdentityCard me={me} loading={loading} />
          <WalletCard currency={me?.currency?.code} />
        </div>
      )}

      <Tabs defaultValue="emails">
        <TabsList className="h-auto">
          <TabsTrigger value="emails">{t("account.emailsTab")}</TabsTrigger>
          <TabsTrigger value="orders">{t("account.ordersTab")}</TabsTrigger>
          <TabsTrigger value="refunds">{t("account.refundsTab")}</TabsTrigger>
          <TabsTrigger value="support">{t("support.title")}</TabsTrigger>
          <TabsTrigger value="chat">{t("liveChat.title")}</TabsTrigger>
        </TabsList>
        <TabsContent value="emails">
          <EmailsTab key={accountId} />
        </TabsContent>
        <TabsContent value="orders">
          <OrdersTab key={accountId} />
        </TabsContent>
        <TabsContent value="refunds">
          <RefundsTab key={accountId} />
        </TabsContent>
      <TabsContent value="support"><SupportTickets key={accountId} /></TabsContent>
        <TabsContent value="chat"><OfficialChat key={accountId} /></TabsContent>
      </Tabs>
    </div>
  );
}

/** 账户资料单元格:标签 + 值 + 附注;未读到显示 —,不把空当事实 */
/** 身份卡:头像首字母 + 持有人 + 邮箱 + KYC,下方三格客户代码 / 所在地 / 子公司 */
function IdentityCard({ me, loading }: { me?: AccountInfo; loading: boolean }) {
  const { t } = useTranslation();
  const holder = me ? `${me.firstname ?? ""} ${me.name ?? ""}`.trim() : "";
  const initials = (holder || me?.email || "?")
    .split(/\s+/)
    .map((w) => w[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
  return (
    <Card className="overflow-hidden flex flex-col">
      <div className="relative flex-1 flex items-center px-4 sm:px-6 pt-5 pb-4">
        <div className="absolute inset-0 bg-gradient-to-r from-primary/[0.06] via-transparent to-transparent pointer-events-none" aria-hidden />
        <div className="relative flex items-center gap-3.5 min-w-0 w-full">
          {loading ? (
            <Skeleton className="w-12 h-12 rounded-full shrink-0" />
          ) : (
            <span className="w-12 h-12 rounded-full bg-primary/10 text-primary ring-1 ring-primary/20 flex items-center justify-center text-[16px] font-semibold shrink-0" aria-hidden>
              {initials}
            </span>
          )}
          <div className="min-w-0 flex-1">
            {loading ? (
              <>
                <Skeleton className="h-5 w-40" />
                <Skeleton className="h-4 w-52 mt-1.5" />
              </>
            ) : (
              <>
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1 min-w-0">
                  <span className="text-[18px] font-semibold tracking-tight truncate">{holder || me?.email || "—"}</span>
                  {me && (
                    <Chip tone={me.kycValidated ? "success" : "warning"}>
                      <ShieldCheck className="w-3 h-3" aria-hidden />
                      {me.kycValidated ? t("account.kycValidated") : t("account.kycNotValidated")}
                    </Chip>
                  )}
                </div>
                <div className="mt-0.5 flex items-center gap-1.5 text-[13px] text-muted-foreground min-w-0">
                  <Mail className="w-3.5 h-3.5 shrink-0" aria-hidden />
                  <span className="truncate" title={me?.email}>{me?.email || "—"}</span>
                </div>
              </>
            )}
          </div>
        </div>
      </div>
      <dl className="grid grid-cols-2 sm:grid-cols-3 border-t border-border divide-x divide-border [&>*:nth-child(3)]:max-sm:col-span-2 [&>*:nth-child(3)]:max-sm:border-l-0 [&>*:nth-child(3)]:max-sm:border-t">
        <InfoCell
          icon={User}
          label={t("account.customerCode")}
          value={me?.customerCode}
          // 美区账户的 NIC 就是邮箱,与邮箱重复时不再显示
          sub={me?.nichandle && me.nichandle !== me?.email ? me.nichandle : undefined}
          loading={loading}
          mono
        />
        <InfoCell icon={MapPin} label={t("account.location")} value={me?.city && me?.country ? `${me.city}, ${me.country}` : me?.country} loading={loading} />
        <InfoCell icon={Globe} label={t("account.subsidiary")} value={me?.ovhSubsidiary} sub={me?.currency?.code} loading={loading} mono />
      </dl>
    </Card>
  );
}

function InfoCell({
  icon: Icon,
  label,
  value,
  sub,
  loading,
  mono,
}: {
  icon: LucideIcon;
  label: string;
  value?: React.ReactNode;
  sub?: string;
  loading?: boolean;
  mono?: boolean;
}) {
  return (
    <div className="min-w-0 px-4 sm:px-6 py-3">
      <dt className="flex items-center gap-1.5 text-[12px] text-muted-foreground">
        <Icon className="w-3.5 h-3.5" aria-hidden />
        {label}
      </dt>
      <dd className="mt-1 min-w-0">
        {loading ? (
          <Skeleton className="h-5 w-24" />
        ) : (
          <div className={`text-[14px] font-semibold truncate ${mono ? "font-mono" : ""}`} title={typeof value === "string" ? value : undefined}>
            {value || "—"}
          </div>
        )}
        {sub && <div className="text-[11.5px] text-muted-foreground truncate">{sub}</div>}
      </dd>
    </div>
  );
}

/** 钱包卡(只读):可用余额合计 + 各项余额(预付款 / 代金券 / 赠送额度 / 押金)+ 即将过期提醒。
 *  付款弹窗里可直接选足额余额付清订单。 */
function WalletCard({ currency }: { currency?: string }) {
  const { t } = useTranslation();
  const q = useCreditBalances();
  const money = (a?: CreditBalance["amount"]) =>
    a?.value != null && Number.isFinite(a.value) && a.currencyCode ? fmtMoney(a.value, a.currencyCode) : a?.text || "—";
  const list = q.data?.list || [];
  // 合计只算可用余额,按币种分组;最多的币种做主数字,其它币种列在下面
  const totals = new Map<string, number>();
  for (const b of list) {
    const v = b.amount?.value;
    if (b.usable && v != null && Number.isFinite(v) && b.amount?.currencyCode) totals.set(b.amount.currencyCode, (totals.get(b.amount.currencyCode) || 0) + v);
  }
  const groups = [...totals.entries()].sort((a, b) => b[1] - a[1]);
  return (
    // 不用 Card:全局卡片样式会把背景盖成白色;这里自己画蓝色渐变
    <section
      aria-label={t("account.balance.title")}
      className="relative overflow-hidden rounded-xl text-white flex flex-col xl:min-h-[180px] shadow-[0_10px_24px_-10px_rgba(31,107,255,.6)]"
      style={{ background: "linear-gradient(135deg, #4c8dff 0%, #1f5fe6 100%)" }}
    >
      {/* 装饰:右上两个半透明圆,呼应钱包/卡片的质感 */}
      <span className="absolute -top-10 -right-8 w-36 h-36 rounded-full bg-white/10 pointer-events-none" aria-hidden />
      <span className="absolute top-10 -right-14 w-28 h-28 rounded-full bg-white/[.07] pointer-events-none" aria-hidden />
      <div className="relative px-5 pt-5 pb-4">
        <div className="flex items-center justify-between gap-2">
          <span className="inline-flex items-center gap-1.5 text-[13px] text-white/85">
            <Wallet className="w-4 h-4" aria-hidden />
            {t("account.balance.title")}
          </span>
          <button
            type="button"
            onClick={() => q.refetch()}
            disabled={q.isFetching}
            className="w-7 h-7 -mr-1.5 rounded-full inline-flex items-center justify-center text-white/80 hover:bg-white/15 hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/70"
            aria-label={t("account.balance.reload")}
            title={t("account.balance.reload")}
          >
            <RefreshCw className={`w-3.5 h-3.5 ${q.isFetching ? "animate-spin motion-reduce:animate-none" : ""}`} aria-hidden />
          </button>
        </div>
        {q.isPending ? (
          <div className="mt-2 h-9 w-40 rounded-lg bg-white/20 animate-pulse motion-reduce:animate-none" />
        ) : q.isError ? (
          <div className="mt-2 text-[13px] text-white/90">
            {t("account.balance.loadFailed")}
            <button type="button" onClick={() => q.refetch()} className="ml-2 underline underline-offset-2 hover:text-white">{t("common.retry")}</button>
          </div>
        ) : (
          <>
            <div className="mt-1 text-[32px] leading-10 font-semibold tracking-tight tabular-nums">
              {groups.length ? fmtMoney(groups[0][1], groups[0][0]) : currency ? fmtMoney(0, currency) : "0"}
            </div>
            {groups.slice(1).map(([cur, v]) => (
              <div key={cur} className="text-[13px] text-white/85 tabular-nums">+ {fmtMoney(v, cur)}</div>
            ))}
            <div className="mt-0.5 text-[12px] text-white/75">{list.length ? t("account.balance.hint") : t("account.balance.empty")}</div>
          </>
        )}
      </div>
      {list.length > 0 && (
        <ul className="relative mt-auto mx-3 mb-3 rounded-xl bg-white/[.12] backdrop-blur-sm divide-y divide-white/10">
          {list.map((b) => {
            const soon = (b.expiring || [])
              .filter((e) => e.expirationDate)
              .sort((x, y) => String(x.expirationDate).localeCompare(String(y.expirationDate)))[0];
            return (
              <li key={b.balanceName} className="px-3.5 py-2.5">
                <div className="flex items-center justify-between gap-3 text-[13px]">
                  <span className="truncate text-white/90">
                    {t(`account.payment.balanceType.${b.type}`, { defaultValue: b.type })}
                    {!b.usable && <span className="ml-1.5 text-[11px] rounded-full bg-white/20 px-1.5 py-0.5">{t("account.balance.unusable")}</span>}
                  </span>
                  <span className={`font-semibold tabular-nums ${b.usable ? "" : "text-white/60"}`}>{money(b.amount)}</span>
                </div>
                {soon && (
                  <div className="mt-0.5 text-[11.5px] text-amber-200">
                    {t("account.balance.expiring", { amount: money(soon.amount), date: fmtDate(soon.expirationDate as string) })}
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
      {q.data && q.data.failed > 0 && <p className="relative px-5 pb-3 text-[12px] text-amber-200">{t("account.balance.partial", { n: q.data.failed })}</p>}
    </section>
  );
}

function EmailsTab() {
  const { t } = useTranslation();
  const emails = useEmails();
  const [selected, setSelected] = useState<EmailHistoryEntry | null>(null);
  // 窄屏下列表和详情上下排列:点邮件后自动滚到详情,不用自己往下翻
  const detailRef = useRef<HTMLDivElement>(null);
  const pick = (e: EmailHistoryEntry) => {
    setSelected(e);
    if (window.matchMedia("(max-width: 1023px)").matches) {
      requestAnimationFrame(() => detailRef.current?.scrollIntoView({ behavior: "smooth", block: "start" }));
    }
  };

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
            <EmptyState illustration="mail" title={t("account.noEmails")} />
          )
        ) : (
          <div className="max-h-[500px] overflow-y-auto">
            <PartialNotice failedCount={emails.data?.failedCount || 0} what={t("account.emailsTab")} className="m-3" />
            <div className="divide-y divide-border">
            {(emails.data?.items || []).map((e) => (
              <button
                key={e.id}
                type="button"
                onClick={() => pick(e)}
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

      <Card ref={detailRef} className="scroll-mt-4">
        <CardContent className="p-4">
          <div className="flex items-center gap-2 mb-4">
            <FileText className="w-4 h-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">{t("account.emailDetail")}</h3>
          </div>
          {selected ? (
            <>
              <p className="mb-1 text-base font-semibold [overflow-wrap:anywhere]">{selected.subject}</p>
              <p className="text-[12px] text-muted-foreground mb-4">{fmtDateTime(selected.date)}</p>
              <pre className="max-h-[400px] max-w-full overflow-y-auto whitespace-pre-wrap rounded-xl bg-muted/50 p-4 font-sans text-base leading-7 text-foreground [overflow-wrap:anywhere]">
                {selected.body}
              </pre>
            </>
          ) : (
            <EmptyState illustration="mail" title={t("account.selectEmail")} />
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
      <CardContent className="p-4">
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
            <EmptyState illustration="refunds" title={t("account.noRefunds")} />
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
                  <p className="text-sm font-semibold tabular-nums text-success">{r.priceWithTax.text}</p>
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

function OrdersTab() {
  const accountId = useAccountScope();
  const { t } = useTranslation();
  const orders = useOrders();
  return (
    <Card>
      <CardContent className="p-4">
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-sm font-semibold">{t("account.ordersTab")}</h3>
          <Button variant="outline" size="sm" onClick={() => orders.refetch()} disabled={orders.isFetching}>
            <RefreshCw className={`w-3.5 h-3.5 ${orders.isFetching ? "animate-spin" : ""}`} />
            {t("common.refresh")}
          </Button>
        </div>
        {orders.isPending ? (
          <div className="space-y-2">
            {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-16 rounded-xl" />)}
          </div>
        ) : orders.isError ? (
          /* 跟退款同一个盲区:请求整条挂掉 → data 为 undefined → 落到「暂无订单」。
             用户会据此认定"这笔单没下成"再去重复下单,所以失败必须明说。 */
          <LoadFailed
            icon={ShoppingBag}
            title={t("account.ordersFailedTitle")}
            error={orders.error}
            onRetry={() => orders.refetch()}
            compact
          />
        ) : (orders.data?.items || []).length === 0 ? (
          (orders.data?.failedCount || 0) > 0 ? (
            <EmptyState
              icon={ShoppingBag}
              title={t("account.ordersPartialTitle")}
              description={t("account.ordersPartialDesc", { count: orders.data?.failedCount || 0 })}
            />
          ) : (
            <EmptyState illustration="orders" title={t("account.noOrders")} />
          )
        ) : (
          <div>
            <PartialNotice failedCount={orders.data?.failedCount || 0} what={t("account.ordersTab")} className="mb-3" />
            <div className="divide-y divide-border">
            {(orders.data?.items || []).map((o) => (
              <OrderRow key={`${accountId}:${o.orderId}`} order={o} accountId={accountId} />
            ))}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function OrderRow({ order, accountId }: { order: OrderRecord; accountId: string }) {
  const { t } = useTranslation();
  // retractionDate 非空且还在未来 = 订单处于法定撤回期内,过期了就不显示
  const retractUntil =
    order.retractionDate && !Number.isNaN(new Date(order.retractionDate).getTime()) && new Date(order.retractionDate) > new Date()
      ? fmtDateTime(order.retractionDate)
      : null;
  return (
    <div className="py-3 flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <div className="min-w-0">
        <div className="flex items-center gap-2 mb-1 flex-wrap">
          <span className="font-mono text-sm font-semibold">#{order.orderId}</span>
          {retractUntil && (
            <Chip tone="warning">{t("account.orderRetractable", { date: retractUntil })}</Chip>
          )}
        </div>
        <p className="text-[11px] text-muted-foreground">{fmtDateTime(order.date)}</p>
      </div>
      <div className="sm:text-right flex-shrink-0">
        <p className="text-sm font-semibold tabular-nums">{order.priceWithTax?.text || "—"}</p>
        {order.orderStatus === "notPaid" && <div className="my-2"><OrderPaymentButton accountId={order.accountId || accountId} orderId={order.orderId} managerUrl={order.url} /></div>}
        {order.paymentStatusError && <p className="text-xs text-warning" title={order.paymentStatusError}>{t("account.payment.statusUnknown")}</p>}
        <div className="flex flex-wrap items-center sm:justify-end gap-3">
          {order.url && (
            <a href={order.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-0.5 text-[11px] text-foreground hover:underline">
              <ExternalLink className="w-3 h-3" />
              {t("account.orderOpenManager")}
            </a>
          )}
          {order.pdfUrl && (
            <a href={order.pdfUrl} target="_blank" rel="noopener noreferrer" className="text-[11px] text-foreground hover:underline">
              {t("account.downloadPdf")}
            </a>
          )}
        </div>
      </div>
    </div>
  );
}
