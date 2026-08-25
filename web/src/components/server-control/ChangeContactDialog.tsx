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
      toast.error("请至少填写一个联系人字段");
      return;
    }
    try {
      await submit.mutateAsync({ serviceName, admin, tech, billing });
      toast.success("变更请求已提交，等待邮件确认");
      setAdmin("");
      setTech("");
      setBilling("");
      setTab("requests");
      list.refetch();
    } catch (e: any) {
      const msg = String(e?.response?.data?.error || e?.message || "");
      // OVH 业务约束:一个 service 同时只能有一个待审 contact change task
      if (/contact change task is already running/i.test(msg)) {
        toast.error("该服务器已有待审的变更请求,请先在「待审请求」处理后再提交新的", { duration: 5000 });
        setTab("requests");
        list.refetch();
        return;
      }
      toast.error(msg || "提交失败");
    }
  };

  const handleAction = async (id: number | string, mode: "accept" | "refuse" | "resend", tokenVal?: string) => {
    try {
      await action.mutateAsync({ id, action: mode, token: tokenVal });
      toast.success(mode === "resend" ? "确认邮件已重发" : mode === "accept" ? "已接受" : "已拒绝");
      setTokenTarget(null);
      setToken("");
    } catch (e: any) {
      // 后端有时用 message 字段(refuse/accept/resend),有时用 error(change-contact)。
      // 都读一遍,优先 message(更具体),最后兜底 OVH 原始 err.message
      const msg =
        e?.response?.data?.message ||
        e?.response?.data?.error ||
        e?.message ||
        "操作失败";
      toast.error(msg, { duration: 6000 });
    }
  };

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl max-h-[90vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Mail className="w-5 h-5" />
              变更联系人
            </DialogTitle>
            <DialogDescription>切换 admin / tech / billing 联系人。OVH 将向当前联系人邮箱发送确认链接。</DialogDescription>
          </DialogHeader>

          <Tabs value={tab} onValueChange={setTab} className="flex-1 overflow-hidden flex flex-col">
            <TabsList>
              <TabsTrigger value="submit">提交变更</TabsTrigger>
              <TabsTrigger value="requests">待审请求</TabsTrigger>
            </TabsList>

            <TabsContent value="submit" className="overflow-y-auto -mx-6 px-6">
              <div className="space-y-3 py-2">
                {/* OVHcloud US 没有 NIC 联系人体系，后端会直接 400。
                    与其让用户填完再吃一个错误，不如在入口就说清楚并禁掉提交。 */}
                {usUnsupported && (
                  <div className="flex items-start gap-2 rounded-xl border border-warning/40 bg-warning/5 px-3 py-2 text-[12px]">
                    <AlertCircle className="w-4 h-4 text-warning flex-shrink-0 mt-0.5" />
                    <span>
                      美区账户不支持通过 API 变更服务器联系人 —— OVHcloud US 没有 NIC 联系人系统，
                      请在 OVHcloud US 控制台或联系客服办理。
                    </span>
                  </div>
                )}
                <ContactField label="Admin 联系人" placeholder="ab12345-ovh 或 someone@example.com" value={admin} onChange={setAdmin} disabled={usUnsupported} />
                <ContactField label="Tech 联系人" placeholder="ab12345-ovh 或 someone@example.com" value={tech} onChange={setTech} disabled={usUnsupported} />
                <ContactField label="Billing 联系人" placeholder="ab12345-ovh 或 someone@example.com" value={billing} onChange={setBilling} disabled={usUnsupported} />
                <p className="text-[11px] text-muted-foreground">
                  填 OVH NIC handle(如 ab12345-ovh)或目标 OVH 账户邮箱皆可。留空则保持原联系人。
                </p>
                <div className="pt-2">
                  <Button onClick={handleSubmit} disabled={submit.isPending || usUnsupported}>
                    {submit.isPending ? "提交中…" : "提交变更请求"}
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
                  title="当前区域不支持"
                  description={list.data.message || "当前账户所在区域不支持联系人变更请求"}
                />
              ) : list.isError ? (
                <EmptyState
                  icon={Mail}
                  title="待审请求读取失败"
                  description={
                    (list.error as any)?.response?.data?.error ||
                    (list.error as any)?.message ||
                    "请稍后刷新重试"
                  }
                />
              ) : (list.data?.requests || []).length === 0 ? (
                <EmptyState icon={Mail} title="暂无待审请求" />
              ) : (
                <div className="space-y-2 py-2">
                  <PartialNotice failedCount={list.data?.failedCount || 0} what="待审请求" />
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
              关闭
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
              输入邮件 Token
            </DialogTitle>
            <DialogDescription>
              将"{tokenTarget?.mode === "accept" ? "接受" : "拒绝"}"该变更请求。请粘贴 OVH 邮件中的确认 token。
            </DialogDescription>
          </DialogHeader>
          <Input value={token} onChange={(e) => setToken(e.target.value)} placeholder="邮件中的 token 字符串" />
          <DialogFooter>
            <Button variant="outline" onClick={() => setTokenTarget(null)}>
              取消
            </Button>
            <Button
              disabled={!token || action.isPending}
              onClick={() => tokenTarget && handleAction(tokenTarget.id, tokenTarget.mode, token)}
            >
              {action.isPending ? "提交中…" : tokenTarget?.mode === "accept" ? "确认接受" : "确认拒绝"}
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
          {req.askingAccount && <span className="ml-2">· 发起人 {req.askingAccount}</span>}
        </p>
      )}
      <p className="text-[11px] text-muted-foreground">
        {req.dateRequest ? `请求 ${new Date(req.dateRequest).toLocaleString("zh-CN")}` : ""}
        {req.dateDone && <span className="ml-2">· 完成 {new Date(req.dateDone).toLocaleString("zh-CN")}</span>}
      </p>
      {canAct && (
        <div className="flex gap-1.5 pt-1">
          <Button size="sm" variant="outline" onClick={onAccept} disabled={busy}>
            <Check className="w-3.5 h-3.5 mr-1" />
            接受
          </Button>
          <Button size="sm" variant="outline" onClick={onRefuse} disabled={busy}>
            <XIcon className="w-3.5 h-3.5 mr-1" />
            拒绝
          </Button>
          <Button size="sm" variant="outline" onClick={onResend} disabled={busy}>
            <RefreshCw className="w-3.5 h-3.5 mr-1" />
            重发邮件
          </Button>
        </div>
      )}
    </div>
  );
}
