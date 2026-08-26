import { useState } from "react";
import { CalendarRange, Check, AlertCircle, X, FileClock, ExternalLink } from "lucide-react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { Chip } from "@/components/common/Chip";
import {
  useEngagement, useEngagementAvailable, useEngagementRequest,
  useCreateEngagementRequest, useDeleteEngagementRequest, useUpdateEngagementEndRule,
  type EngagementPricing,
} from "@/hooks/use-server-control";
import { toast } from "sonner";
import { formatMoney } from "@/lib/money";

/** EngagementDialog 钩子绑定 —— dedicated / vps 各自传入。
 *  类型上接受 (svc, enabled?) → query / mutation 形状即可。 */
export type EngagementHooks = {
  // isError 必须进契约：后端不再把 401/403/5xx/超时吞成 engagement:null 了，
  // 前端如果只看 data==null，就会把"读取失败"渲染成"该服务未签合同期"——照样是误导。
  useEngagement: (svc: string | null) => { data: any; isPending: boolean; isError: boolean; error: unknown };
  useEngagementAvailable: (svc: string | null, enabled?: boolean) => { data: any; isPending: boolean; isError: boolean };
  useEngagementRequest: (svc: string | null) => { data: any; isPending: boolean; isError: boolean };
  useCreateEngagementRequest: (svc: string) => { mutateAsync: (vars: { pricingMode: string }) => Promise<any>; isPending: boolean };
  useDeleteEngagementRequest: (svc: string) => { mutateAsync: () => Promise<any>; isPending: boolean };
  /** CANCEL_SERVICE 不可撤销，后端要求带 confirm:true，组件必须先弹二次确认 */
  useUpdateEngagementEndRule: (svc: string) => { mutateAsync: (vars: { strategy: string; confirm?: boolean }) => Promise<any>; isPending: boolean };
};

const DEFAULT_HOOKS: EngagementHooks = {
  useEngagement,
  useEngagementAvailable,
  useEngagementRequest,
  useCreateEngagementRequest,
  useDeleteEngagementRequest,
  useUpdateEngagementEndRule,
};

/** 合同期管理:查看当前 engagement + 切换更长承诺期 + 改到期策略。
 *  默认绑 dedicated;VPS 传入 vps 版 hooks 即可复用整套 UI。 */
export function EngagementDialog({
  serviceName,
  open,
  onOpenChange,
  hooks = DEFAULT_HOOKS,
}: {
  serviceName: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  hooks?: EngagementHooks;
}) {
  const current = hooks.useEngagement(open ? serviceName : null);
  const available = hooks.useEngagementAvailable(open ? serviceName : null, open);
  const ongoing = hooks.useEngagementRequest(open ? serviceName : null);
  const createReq = hooks.useCreateEngagementRequest(serviceName);
  const deleteReq = hooks.useDeleteEngagementRequest(serviceName);
  const updateRule = hooks.useUpdateEngagementEndRule(serviceName);
  const [confirmMode, setConfirmMode] = useState<string | null>(null);
  /** 待二次确认的到期策略（目前只有 CANCEL_SERVICE 会走这里） */
  const [confirmStrategy, setConfirmStrategy] = useState<string | null>(null);

  const isLoading = current.isPending || available.isPending || ongoing.isPending;

  const handleSubscribe = async (pricingMode: string) => {
    try {
      const data = await createReq.mutateAsync({ pricingMode });
      const orderUrl: string | undefined = data?.request?.order?.url;
      setConfirmMode(null);
      if (orderUrl) {
        toast.success("订单已创建,正在打开 OVH 支付页面…");
        // 在新标签打开,不影响当前页面
        window.open(orderUrl, "_blank", "noopener,noreferrer");
      } else {
        toast.success("变更请求已提交,请前往 OVH manager 完成支付");
      }
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "提交失败");
    }
  };

  const handleCancelRequest = async () => {
    try {
      await deleteReq.mutateAsync();
      toast.success("已撤销变更请求");
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "撤销失败");
    }
  };

  /**
   * 改到期策略。
   * CANCEL_SERVICE = 承诺期一结束就销毁服务器，不可撤销，后端要求 confirm:true，
   * 所以这里先弹二次确认，用户点了"确认销毁"才带 confirm 发出去；其余策略照旧直发。
   */
  const handleEndRule = async (strategy: string, confirmed = false) => {
    if (strategy === "CANCEL_SERVICE" && !confirmed) {
      setConfirmStrategy(strategy);
      return;
    }
    try {
      await updateRule.mutateAsync(
        strategy === "CANCEL_SERVICE" ? { strategy, confirm: true } : { strategy },
      );
      toast.success("到期策略已更新");
      setConfirmStrategy(null);
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "更新失败");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <CalendarRange className="w-5 h-5" />
            合同期管理
          </DialogTitle>
          <DialogDescription>切换更长承诺期享受折扣 / 改到期策略</DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto -mx-6 px-6 space-y-4">
          {isLoading ? (
            <Skeleton className="h-40 rounded-2xl" />
          ) : (
            <>
              {/* 当前合同期 */}
              <section className="border border-border rounded-2xl p-3.5 space-y-2">
                <div className="flex items-center gap-2">
                  <FileClock className="w-4 h-4 text-muted-foreground" />
                  <h3 className="text-sm font-semibold">当前合同期</h3>
                </div>
                {current.data ? (
                  <div className="space-y-1.5 text-[12px]">
                    {current.data.currentPeriod && (
                      <div className="text-muted-foreground">
                        周期: {new Date(current.data.currentPeriod.startDate).toLocaleDateString("zh-CN")} —{" "}
                        {new Date(current.data.currentPeriod.endDate).toLocaleDateString("zh-CN")}
                      </div>
                    )}
                    {current.data.endRule && (
                      <div className="flex items-center gap-2 flex-wrap pt-1">
                        <span className="text-muted-foreground">到期策略:</span>
                        <Chip tone="default">{translateEndStrategy(current.data.endRule.strategy)}</Chip>
                        {current.data.endRule.possibleStrategies
                          .filter((s) => s !== current.data!.endRule!.strategy)
                          .map((s) => (
                            <Button
                              key={s}
                              size="sm"
                              variant="outline"
                              className="h-6 text-[11px] px-2"
                              onClick={() => handleEndRule(s)}
                              disabled={updateRule.isPending}
                            >
                              改为「{translateEndStrategy(s)}」
                            </Button>
                          ))}
                      </div>
                    )}
                  </div>
                ) : current.isError ? (
                  /* 读失败 ≠ 没签合同期。写成"未签合同期"会让用户以为可以随便改续费方式 */
                  <p className="text-[12px] text-destructive">
                    合同期信息读取失败：
                    {(current.error as any)?.response?.data?.error ||
                      (current.error as any)?.message ||
                      "请关闭弹窗后重试"}
                    。当前是否处于承诺期未知，请勿据此判断。
                  </p>
                ) : (
                  <p className="text-[12px] text-muted-foreground">
                    该服务未签合同期,按标准月付方式续费。可在下方订阅承诺期享受折扣。
                  </p>
                )}
              </section>

              {/* 进行中的变更请求 */}
              {ongoing.data && (
                <section className="border border-amber-500/40 bg-amber-500/5 rounded-2xl p-3.5 space-y-2">
                  <div className="flex items-center gap-2">
                    <AlertCircle className="w-4 h-4 text-amber-600 dark:text-amber-400" />
                    <h3 className="text-sm font-semibold">订单已创建,等待支付</h3>
                  </div>
                  <p className="text-[11px] text-muted-foreground">
                    OVH 已为此变更创建订单,**付款前合同期不会生效**,服务继续按原月付。30 天未付订单将自动取消。
                  </p>
                  <div className="text-[12px] space-y-1">
                    {ongoing.data.pricing && (
                      <div>目标: {ongoing.data.pricing.description}</div>
                    )}
                    {ongoing.data.requestDate && (
                      <div className="text-muted-foreground">
                        提交时间: {new Date(ongoing.data.requestDate).toLocaleString("zh-CN")}
                      </div>
                    )}
                    {ongoing.data.order?.orderId && (
                      <div className="text-muted-foreground">
                        订单号: #{ongoing.data.order.orderId}
                      </div>
                    )}
                  </div>
                  <div className="flex gap-2 flex-wrap pt-1">
                    {ongoing.data.order?.url && (
                      <Button
                        size="sm"
                        variant="default"
                        onClick={() => window.open(ongoing.data!.order!.url, "_blank", "noopener,noreferrer")}
                      >
                        <ExternalLink className="w-3.5 h-3.5 mr-1" />
                        前往 OVH 支付
                      </Button>
                    )}
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={handleCancelRequest}
                      disabled={deleteReq.isPending}
                    >
                      <X className="w-3.5 h-3.5 mr-1" />
                      撤销请求
                    </Button>
                  </div>
                </section>
              )}

              {/* 可订阅的 engagement 列表 */}
              <section className="space-y-2">
                <div className="flex items-center justify-between gap-2 flex-wrap">
                  <h3 className="text-sm font-semibold">可订阅的承诺期</h3>
                  <p className="text-[10px] text-muted-foreground">
                    长周期承诺通常有折扣(部分入门款无折扣)。月均价就是月度等效成本。
                  </p>
                </div>
                {(available.data || []).length === 0 ? (
                  <EmptyState icon={CalendarRange} title="暂无可订阅的承诺期" />
                ) : (
                  <div className="space-y-2">
                    {(available.data || []).map((p) => (
                      <PricingRow
                        key={p.pricingMode}
                        pricing={p}
                        onSubscribe={() => setConfirmMode(p.pricingMode)}
                        /* ongoing 读失败时也禁用：放开等于允许对着一个"可能已存在的请求"再提交一遍 */
                        disabled={createReq.isPending || !!ongoing.data || ongoing.isError}
                      />
                    ))}
                    {ongoing.data && (
                      <p className="text-[11px] text-muted-foreground text-center pt-1">
                        有变更请求处理中,需先撤销才能订阅新承诺期
                      </p>
                    )}
                    {!ongoing.data && ongoing.isError && (
                      <p className="text-[11px] text-warning text-center pt-1">
                        进行中的变更请求读取失败,暂时无法订阅 —— 无法确认是否已有未付订单,重复提交会多出一笔订单。
                      </p>
                    )}
                  </div>
                )}
              </section>
            </>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>

      {/* 二次确认子弹窗 */}
      <Dialog open={!!confirmMode} onOpenChange={(v) => !v && setConfirmMode(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>确认订阅承诺期?</DialogTitle>
            <DialogDescription>提交前请阅读流程</DialogDescription>
          </DialogHeader>
          <ol className="space-y-1.5 text-[12px] text-muted-foreground list-decimal pl-5">
            <li>
              OVH 创建一笔<span className="font-semibold text-foreground">未付订单</span>
              (一次性预付=承诺期总价;周期付费=首期价)
            </li>
            <li>
              <span className="font-semibold text-foreground">付款前合同期不会激活</span>
              ,服务继续按原月付收费
            </li>
            <li>
              若已设自动扣款 + 余额充足 → 几分钟内自动扣款激活
            </li>
            <li>
              否则需手动去 OVH manager 支付。30 天没付订单自动取消
            </li>
            <li>
              一旦付款 → 合同期锁死,中途解约按未消耗月数计违约金
            </li>
          </ol>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmMode(null)}>
              取消
            </Button>
            <Button
              onClick={() => confirmMode && handleSubscribe(confirmMode)}
              disabled={createReq.isPending}
            >
              <Check className="w-3.5 h-3.5 mr-1" />
              {createReq.isPending ? "提交中…" : "创建订单"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 到期策略二次确认:CANCEL_SERVICE 不可撤销 */}
      <Dialog open={!!confirmStrategy} onOpenChange={(v) => !v && setConfirmStrategy(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="text-destructive">确认改为「到期自动销毁服务」?</DialogTitle>
            <DialogDescription>承诺期结束时服务将被销毁</DialogDescription>
          </DialogHeader>
          <div className="text-[12px] text-muted-foreground space-y-1.5">
            <p>
              承诺期结束时,OVH 会
              <span className="font-semibold text-destructive">直接销毁这台服务器</span>
              ,数据不保留、IP 不保留。
            </p>
            <p>设置后若要反悔,需要在承诺期结束前改回其它策略。</p>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmStrategy(null)}>
              取消
            </Button>
            <Button
              variant="destructive"
              disabled={updateRule.isPending}
              onClick={() => confirmStrategy && handleEndRule(confirmStrategy, true)}
            >
              {updateRule.isPending ? "提交中…" : "确认销毁"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Dialog>
  );
}

function PricingRow({
  pricing,
  onSubscribe,
  disabled,
}: {
  pricing: EngagementPricing;
  onSubscribe: () => void;
  disabled: boolean;
}) {
  // 币种一律用 OVH 给的 currencyCode。以前兜底 "USD" —— 合同期这套端点三区都有,
  // 欧区账户的价格会被硬贴上 USD 标签。拿不到就只显示数字(formatMoney 不编货币符号)。
  const currency = pricing.price?.currencyCode || "";
  const priceValue = pricing.price?.value ?? 0;
  // 承诺期总长(月),只用于标题展示
  const months = parseDurationMonths(pricing.engagementConfiguration?.duration || "");
  // 价格对应的周期是 pricing 行**自己的** duration —— schema 里它的描述是
  // "Default renew interval"(一个续费区间)。以前拿承诺期月数去除:
  // upfront 模式碰巧对(区间=承诺期),periodic 模式区间只有 1 个月,
  // 月付价被再除以 12,「月均价」低了 12 倍 —— 用户会按假价签一年合同。
  const intervalMonths = parseDurationMonths(pricing.duration || "");
  const perMonth = intervalMonths > 0 ? priceValue / intervalMonths : 0;

  const isUpfront = pricing.pricingMode.toLowerCase().includes("upfront");
  // 总价只有区间=承诺期(upfront)时才等于 price;periodic 的 price 是每期价,
  // 总价 = 每期价 × 期数
  const totalValue =
    intervalMonths > 0 && months > 0 ? (priceValue / intervalMonths) * months : priceValue;
  const totalText =
    isUpfront && pricing.price?.text
      ? pricing.price.text
      : totalValue > 0
        ? formatMoney(totalValue, currency)
        : "—";
  const perMonthText = perMonth > 0 ? `${formatMoney(perMonth, currency)} / 月` : "";

  const friendlyTitle = humanizeDescription(pricing.description, months, isUpfront);

  return (
    <div className="border border-border rounded-xl p-3 flex items-center gap-3 flex-wrap">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <div className="text-[13px] font-semibold">{friendlyTitle}</div>
          <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-secondary text-muted-foreground">
            {isUpfront ? "一次性预付" : "周期付费"}
          </span>
        </div>
        {pricing.engagementConfiguration && (
          <div className="text-[11px] text-muted-foreground mt-1">
            到期: {translateEndStrategy(pricing.engagementConfiguration.defaultEndAction)}
          </div>
        )}
      </div>
      <div className="text-right">
        <div className="text-[13px] font-semibold">{totalText}</div>
        {perMonthText && (
          <div className="text-[10px] text-muted-foreground">{perMonthText}</div>
        )}
        <Button size="sm" variant="outline" onClick={onSubscribe} disabled={disabled} className="mt-1.5">
          订阅
        </Button>
      </div>
    </div>
  );
}

// parseDurationMonths ISO8601 (P1Y / P3M / P12M / P1Y6M) → 月数
function parseDurationMonths(iso: string): number {
  if (!iso) return 0;
  const m = iso.match(/^P(?:(\d+)Y)?(?:(\d+)M)?/);
  if (!m) return 0;
  const years = parseInt(m[1] || "0", 10);
  const monthsPart = parseInt(m[2] || "0", 10);
  return years * 12 + monthsPart;
}

// humanizeDescription "rental for 12 months" → "12 个月预付套餐"
function humanizeDescription(desc: string, months: number, isUpfront: boolean): string {
  if (months > 0) {
    const human = months % 12 === 0 ? `${months / 12} 年` : `${months} 个月`;
    return isUpfront ? `${human}预付` : `${human}周期`;
  }
  return desc || "—";
}

// translateEndStrategy OVH 到期策略枚举 → 中文
function translateEndStrategy(s: string): string {
  switch (s) {
    case "REACTIVATE_ENGAGEMENT":
      return "到期自动再签同样合同期";
    case "STOP_ENGAGEMENT_FALLBACK_DEFAULT_PRICE":
      return "到期转月付(回到标准价)";
    case "STOP_ENGAGEMENT_KEEP_PRICE":
      return "到期转月付(保持当前价,无合同期)";
    case "CANCEL_SERVICE":
      return "到期自动销毁服务";
    default:
      return s || "—";
  }
}
