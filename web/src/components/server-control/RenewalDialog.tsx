import { useEffect, useState } from "react";
import { Repeat, AlertCircle, Lock } from "lucide-react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  useUpdateRenewal,
  useTerminateService,
  useConfirmTermination,
  type ServiceInfo,
} from "@/hooks/use-server-control";
import { Input } from "@/components/ui/input";
import { toast } from "sonner";

type RenewMode = "auto" | "manual" | "delete";

const MODE_OPTIONS: Array<{ value: RenewMode; label: string; desc: string }> = [
  { value: "auto", label: "自动续费", desc: "到期前 OVH 自动扣款续费" },
  { value: "manual", label: "手动续费", desc: "到期前需手动付款,不付则服务终止" },
  { value: "delete", label: "到期注销", desc: "取消续费,到期后销毁(需邮件确认)" },
];

/** 用 VPS / dedicated 各自的 update hook 都行,Dialog 只关心 mutation 接口形状 */
export type RenewalMutation = {
  mutateAsync: (vars: { mode: RenewMode; period?: number }) => Promise<any>;
  isPending: boolean;
};

/** 终止流程的两步。VPS 和独服的端点不同(/vps-control 与 /server-control),
 *  所以必须由调用方注入 —— 写死一边会让另一边打到错误的端点上。 */
export type TerminationMutations = {
  terminate: { mutateAsync: () => Promise<any>; isPending: boolean };
  confirm: {
    mutateAsync: (vars: { token: string }) => Promise<any>;
    isPending: boolean;
  };
};

/** 续费策略修改对话框:三选一 + 周期选择;forced 套餐禁用全部操作。
 *  默认用 dedicated 的 useUpdateRenewal,VPS 调用方传入自己的 mutation 即可复用。 */
export function RenewalDialog({
  serviceName,
  info,
  open,
  onOpenChange,
  mutation,
  termination,
}: {
  serviceName: string;
  info: ServiceInfo | (Omit<ServiceInfo, "possibleRenewPeriod"> & { possibleRenewPeriod?: number[] });
  open: boolean;
  onOpenChange: (v: boolean) => void;
  /** 可选:不传则用 dedicated 的 useUpdateRenewal(serviceName) */
  mutation?: RenewalMutation;
  /** 可选:不传则用 dedicated 的终止端点。VPS 必须传自己的 */
  termination?: TerminationMutations;
}) {
  const currentMode: RenewMode = info.renewalDeleteAtExpiration
    ? "delete"
    : info.renewalType
      ? "auto"
      : "manual";
  const [mode, setMode] = useState<RenewMode>(currentMode);
  const [period, setPeriod] = useState<number>(info.renewalPeriod || 1);
  const defaultUpdate = useUpdateRenewal(serviceName);
  const update = mutation ?? defaultUpdate;

  // 到期注销走的是 OVH 的服务终止流程,不是 serviceInfos 里的 renew 标志位。
  // 那条路会被 OVH 回 400 "Arguments conflicting",而且它自己的 issue 里记着
  // 这组标志位行为不可预测。终止有专用端点,也是 OVH 控制台走的那条。
  // 代价是必须邮件确认 —— 这是 OVH 的规定,绕不开,所以对话框要有第二步。
  const defaultTerminate = useTerminateService(serviceName);
  const defaultConfirm = useConfirmTermination(serviceName);
  const terminate = termination?.terminate ?? defaultTerminate;
  const confirmTerm = termination?.confirm ?? defaultConfirm;
  // 已经发起终止、正在等用户填邮件里的 token
  const [awaitingToken, setAwaitingToken] = useState(false);
  const [token, setToken] = useState("");

  // 弹窗每次打开同步当前状态
  useEffect(() => {
    if (open) {
      setMode(currentMode);
      setPeriod(info.renewalPeriod || 1);
      setAwaitingToken(false);
      setToken("");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const periods = info.possibleRenewPeriod && info.possibleRenewPeriod.length > 0
    ? info.possibleRenewPeriod
    : [1, 3, 6, 12];

  const errText = (e: any, fallback: string) =>
    e?.response?.data?.error || e?.response?.data?.message || e?.message || fallback;

  const handleSubmit = async () => {
    if (mode === "delete") {
      try {
        const res = await terminate.mutateAsync();
        setAwaitingToken(true);
        toast.info(res?.message || "终止请求已提交,确认码已发到管理员邮箱", { duration: 8000 });
      } catch (e: any) {
        toast.error(errText(e, "发起终止失败"), { duration: 6000 });
      }
      return;
    }
    try {
      await update.mutateAsync({ mode, period });
      toast.success("续费策略已更新");
      onOpenChange(false);
    } catch (e: any) {
      toast.error(errText(e, "更新失败"), { duration: 6000 });
    }
  };

  const handleConfirmTerm = async () => {
    if (!token.trim()) {
      toast.error("请填写邮件里的确认码");
      return;
    }
    try {
      await confirmTerm.mutateAsync({ token: token.trim() });
      toast.success("已确认终止。到期日之前服务器照常运行,到期后才销毁");
      onOpenChange(false);
    } catch (e: any) {
      toast.error(errText(e, "确认失败"), { duration: 8000 });
    }
  };

  const busy = update.isPending || terminate.isPending || confirmTerm.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Repeat className="w-5 h-5" />
            修改续费策略
          </DialogTitle>
          <DialogDescription>{serviceName}</DialogDescription>
        </DialogHeader>

        {info.renewalForced ? (
          <div className="border border-amber-500/40 bg-amber-500/10 rounded-xl p-3 flex gap-2.5">
            <Lock className="w-4 h-4 text-amber-600 dark:text-amber-400 flex-shrink-0 mt-0.5" />
            <div className="text-[12px]">
              <p className="font-semibold text-amber-700 dark:text-amber-300 mb-1">
                合同期内,无法修改
              </p>
              <p className="text-muted-foreground">
                该服务器处于 OVH 套餐合同期(engaged),续费策略由 OVH 锁定。需要联系 OVH 客服或等合同期结束。
              </p>
            </div>
          </div>
        ) : (
          <div className="space-y-3 py-1">
            {/* 三选一 */}
            <div className="space-y-1.5">
              {MODE_OPTIONS.map((opt) => {
                const selected = mode === opt.value;
                return (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => setMode(opt.value)}
                    className={[
                      "w-full text-left rounded-xl border px-3.5 py-2.5 transition-colors",
                      selected
                        ? "border-primary bg-primary/5"
                        : "border-border bg-secondary/30 hover:bg-secondary/50",
                    ].join(" ")}
                  >
                    <div className="flex items-center gap-2">
                      <div
                        className={[
                          "w-4 h-4 rounded-full border-2 flex items-center justify-center flex-shrink-0",
                          selected ? "border-primary" : "border-muted-foreground/40",
                        ].join(" ")}
                      >
                        {selected && <div className="w-2 h-2 rounded-full bg-primary" />}
                      </div>
                      <span className="text-[13px] font-semibold">{opt.label}</span>
                      {currentMode === opt.value && (
                        <span className="ml-auto text-[10px] text-muted-foreground">当前</span>
                      )}
                    </div>
                    <p className="text-[11px] text-muted-foreground mt-1 ml-6">{opt.desc}</p>
                  </button>
                );
              })}
            </div>

            {/* 续费周期(到期注销时隐藏) */}
            {mode !== "delete" && (
              <div className="pt-1">
                <label className="text-[12px] font-semibold block mb-1.5">续费周期</label>
                <Select value={String(period)} onValueChange={(v) => setPeriod(Number(v))}>
                  <SelectTrigger className="h-9">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {periods.map((p) => (
                      <SelectItem key={p} value={String(p)}>
                        {p} 个月
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            {mode === "delete" && !awaitingToken && (
              <div className="border border-destructive/40 bg-destructive/5 rounded-xl p-2.5 flex gap-2">
                <AlertCircle className="w-3.5 h-3.5 text-destructive flex-shrink-0 mt-0.5" />
                <div className="text-[11px] text-muted-foreground space-y-1">
                  <p>
                    服务器将在到期日 (
                    {info.expiration ? new Date(info.expiration).toLocaleDateString("zh-CN") : "—"}
                    ) 销毁,数据无法恢复。
                  </p>
                  <p>
                    OVH 要求邮件确认:点下面的按钮后会往账户管理员邮箱发一封确认邮件,
                    把里面的确认码填回来才真正生效。
                  </p>
                  <p>
                    到期之前服务器<b>照常运行</b>,不会立即停机 —— OVH 的终止是
                    「当期订阅结束时才生效」。立即释放资源是另一个接口,本控制台不调用。
                  </p>
                  <p>
                    <b>反悔要去 OVH 控制台</b>(API 没有取消终止的端点):
                    My offers and services → 该服务右侧 <code>...</code> →
                    Stop cancellation of service。撤销是立即生效的。
                  </p>
                </div>
              </div>
            )}

            {mode === "delete" && awaitingToken && (
              <div className="space-y-2 pt-1">
                <div className="border border-amber-500/40 bg-amber-500/10 rounded-xl p-2.5 text-[11px] text-muted-foreground">
                  确认邮件已发到账户管理员邮箱。把邮件里的确认码填在下面 ——
                  <b>没填之前终止不会生效</b>,服务器仍会照常续费。
                </div>
                <label className="text-[12px] font-semibold block">邮件里的确认码</label>
                <Input
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder="粘贴邮件中的 token"
                  autoFocus
                />
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          {!info.renewalForced &&
            (awaitingToken ? (
              <Button onClick={handleConfirmTerm} disabled={busy} variant="destructive">
                {confirmTerm.isPending ? "确认中…" : "确认终止"}
              </Button>
            ) : (
              <Button
                onClick={handleSubmit}
                disabled={
                  busy ||
                  (mode !== "delete" && mode === currentMode && period === info.renewalPeriod)
                }
                variant={mode === "delete" ? "destructive" : "default"}
              >
                {busy ? "提交中…" : mode === "delete" ? "申请终止" : "保存"}
              </Button>
            ))}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
