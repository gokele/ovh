import { createFileRoute } from "@tanstack/react-router";
import { User, Mail, RefreshCw, FileText, Inbox, ShieldCheck, type LucideIcon } from "lucide-react";
import { useState } from "react";
import { PageHeader } from "@/components/common/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Chip } from "@/components/common/Chip";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { PartialNotice } from "@/components/common/PartialNotice";
import { useAccountInfo, useRefunds, useEmails, type EmailHistoryEntry } from "@/hooks/use-account";

/** 账户管理：顶部 3 张 KPI + Tabs (邮件 / 退款) */
export const Route = createFileRoute("/account")({
  component: AccountPage,
});

function AccountPage() {
  const q = useAccountInfo();
  // 后端把 OVH /me 原样透传在 body 里,错配标记只能放响应头,所以 hook 返回 { info, subsidiaryMismatch }
  const me = q.data?.info;
  const loading = q.isPending;
  return (
    <div className="space-y-6">
      <PageHeader icon={User} title="账户管理" description="查看和管理您的 OVH 账户信息" />

      {/* 后端在 /me 响应上打的 X-Subsidiary-Mismatch:账户里配的 zone 与 OVH 认定的
          ovhSubsidiary 不在一起。凭据是有效的,所以别的地方一切正常,只有目录/价格/下单
          会静默走错站点 —— 这是用户在下单失败之前唯一能看到的信号,放在最显眼处。 */}
      {q.data?.subsidiaryMismatch && (
        <div className="flex items-start gap-2 rounded-xl border border-warning/40 bg-warning/5 px-3 py-2 text-[12px]">
          <ShieldCheck className="w-4 h-4 text-warning flex-shrink-0 mt-0.5" />
          <div>
            <p className="font-semibold">账户子公司配置与 OVH 实际归属不一致</p>
            <p className="text-muted-foreground mt-0.5">
              OVH 返回的 ovhSubsidiary 是 <code className="font-mono">{q.data.info.ovhSubsidiary || "—"}</code>，
              与设置页里给这个账户填的子公司(zone)不同。EU / US / CA 是三套互不相通的系统，
              目录、价格、币种、库存、下单 region 全部由子公司决定 —— 请到「设置 → OVH 账户」把 zone 改成
              上面这个值(endpoint 会跟着自动切)。详细原因见日志页。
            </p>
          </div>
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <KpiCard
          icon={User}
          label="客户代码"
          value={me?.customerCode}
          sub={me?.nichandle}
          loading={loading}
          badge={
            me && (
              <Chip tone={me.kycValidated ? "success" : "warning"}>
                <ShieldCheck className="w-3 h-3" />
                {me.kycValidated ? "已验证" : "未验证"}
              </Chip>
            )
          }
        />
        <KpiCard icon={Mail} label="邮箱" value={me?.email} loading={loading} />
        <KpiCard
          icon={User}
          label="账户持有人"
          value={me ? `${me.firstname ?? ""} ${me.name ?? ""}`.trim() : undefined}
          sub={me?.city && me?.country ? `${me.city}, ${me.country}` : undefined}
          loading={loading}
        />
      </div>

      <Tabs defaultValue="emails">
        <TabsList>
          <TabsTrigger value="emails">邮件历史</TabsTrigger>
          <TabsTrigger value="refunds">退款记录</TabsTrigger>
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
  const emails = useEmails();
  const [selected, setSelected] = useState<EmailHistoryEntry | null>(null);

  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <Card className="overflow-hidden">
        <div className="px-4 py-3 border-b border-border flex items-center justify-between">
          <span className="text-sm font-semibold">邮件列表</span>
          <Button variant="outline" size="sm" onClick={() => emails.refetch()} disabled={emails.isFetching}>
            <RefreshCw className={`w-3.5 h-3.5 ${emails.isFetching ? "animate-spin" : ""}`} />
            刷新
          </Button>
        </div>
        {emails.isPending ? (
          <div className="p-4 space-y-2">
            {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-14 rounded-xl" />)}
          </div>
        ) : (emails.data?.items || []).length === 0 ? (
          /* 空列表 + failedCount>0 = 这次一条都没读到，不是"账户没有邮件" */
          (emails.data?.failedCount || 0) > 0 ? (
            <EmptyState
              icon={Inbox}
              title="邮件历史读取失败"
              description={`有 ${emails.data?.failedCount} 条邮件详情未能获取，请点刷新重试。`}
            />
          ) : (
            <EmptyState icon={Inbox} title="暂无邮件" />
          )
        ) : (
          <div className="max-h-[500px] overflow-y-auto">
            <PartialNotice failedCount={emails.data?.failedCount || 0} what="邮件历史" className="m-3" />
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
                <p className="text-[11px] text-muted-foreground">{new Date(e.date).toLocaleString("zh-CN")}</p>
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
            <h3 className="text-sm font-semibold">邮件详情</h3>
          </div>
          {selected ? (
            <>
              <p className="text-[15px] font-semibold mb-1">{selected.subject}</p>
              <p className="text-[12px] text-muted-foreground mb-4">{new Date(selected.date).toLocaleString("zh-CN")}</p>
              <pre className="text-[12px] font-mono whitespace-pre-wrap text-foreground bg-secondary rounded-lg p-3 max-h-[400px] overflow-y-auto">
                {selected.body}
              </pre>
            </>
          ) : (
            <EmptyState icon={Mail} title="请选择一封邮件" />
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function RefundsTab() {
  const refunds = useRefunds();
  return (
    <Card>
      <CardContent className="p-5">
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-sm font-semibold">退款记录</h3>
          <Button variant="outline" size="sm" onClick={() => refunds.refetch()} disabled={refunds.isFetching}>
            <RefreshCw className={`w-3.5 h-3.5 ${refunds.isFetching ? "animate-spin" : ""}`} />
            刷新
          </Button>
        </div>
        {refunds.isPending ? (
          <div className="space-y-2">
            {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-16 rounded-xl" />)}
          </div>
        ) : (refunds.data?.items || []).length === 0 ? (
          (refunds.data?.failedCount || 0) > 0 ? (
            <EmptyState
              icon={Inbox}
              title="退款记录读取失败"
              description={`有 ${refunds.data?.failedCount} 条退款详情未能获取，请点刷新重试。`}
            />
          ) : (
            <EmptyState icon={Inbox} title="暂无退款记录" />
          )
        ) : (
          <div>
            <PartialNotice failedCount={refunds.data?.failedCount || 0} what="退款记录" className="mb-3" />
            <div className="divide-y divide-border">
            {(refunds.data?.items || []).map((r) => (
              <div key={r.refundId} className="py-3 flex items-center justify-between gap-4">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="font-mono text-sm font-semibold">#{r.refundId}</span>
                    <Chip tone="default">订单 {r.orderId}</Chip>
                  </div>
                  <p className="text-[11px] text-muted-foreground">{new Date(r.date).toLocaleString("zh-CN")}</p>
                </div>
                <div className="text-right flex-shrink-0">
                  <p className="text-lg font-bold text-success">{r.priceWithTax.text}</p>
                  {r.pdfUrl && (
                    <a href={r.pdfUrl} target="_blank" rel="noopener noreferrer" className="text-[11px] text-foreground hover:underline">
                      下载 PDF
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
