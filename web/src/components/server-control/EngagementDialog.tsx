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
import { Trans, useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { errorMessage } from "@/components/common/LoadFailed";
import { fmtDate, fmtDateTime } from "@/i18n/format";

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

/** OVH 到期策略枚举 → 语言包 key;没匹配上的透传原始枚举值 */
function endStrategyKey(s: string): string | null {
  switch (s) {
    case "REACTIVATE_ENGAGEMENT":
      return "maint.engagement.strategy.reactivate";
    case "STOP_ENGAGEMENT_FALLBACK_DEFAULT_PRICE":
      return "maint.engagement.strategy.fallbackDefault";
    case "STOP_ENGAGEMENT_KEEP_PRICE":
      return "maint.engagement.strategy.keepPrice";
    case "CANCEL_SERVICE":
      return "maint.engagement.strategy.cancelService";
    default:
      return null;
  }
}

function endStrategyText(t: TFunction, s: string): string {
  const key = endStrategyKey(s);
  return (key ? t(key) : s) || "—";
}

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
  const { t } = useTranslation();
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
        toast.success(t("maint.engagement.toast.orderCreated"));
        // 在新标签打开,不影响当前页面
        window.open(orderUrl, "_blank", "noopener,noreferrer");
      } else {
        toast.success(t("maint.engagement.toast.requestSubmitted"));
      }
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };

  const handleCancelRequest = async () => {
    try {
      await deleteReq.mutateAsync();
      toast.success(t("maint.engagement.toast.requestCancelled"));
    } catch (e: any) {
      toast.error(errorMessage(e));
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
      toast.success(t("maint.engagement.toast.endRuleUpdated"));
      setConfirmStrategy(null);
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <CalendarRange className="w-5 h-5" />
            {t("maint.engagement.title")}
          </DialogTitle>
          <DialogDescription>{t("maint.engagement.desc")}</DialogDescription>
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
                  <h3 className="text-sm font-semibold">{t("maint.engagement.currentTitle")}</h3>
                </div>
                {current.data ? (
                  <div className="space-y-1.5 text-[12px]">
                    {current.data.currentPeriod && (
                      <div className="text-muted-foreground">
                        {t("maint.engagement.periodRange", {
                          start: fmtDate(current.data.currentPeriod.startDate),
                          end: fmtDate(current.data.currentPeriod.endDate),
                        })}
                      </div>
                    )}
                    {current.data.endRule && (
                      <div className="flex items-center gap-2 flex-wrap pt-1">
                        <span className="text-muted-foreground">{t("maint.engagement.endRuleLabel")}</span>
                        <Chip tone="default">
                          {endStrategyText(t, current.data.endRule.strategy)}
                        </Chip>
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
                              {t("maint.engagement.changeTo", { strategy: endStrategyText(t, s) })}
                            </Button>
                          ))}
                      </div>
                    )}
                  </div>
                ) : current.isError ? (
                  /* 读失败 ≠ 没签合同期。写成"未签合同期"会让用户以为可以随便改续费方式 */
                  <p className="text-[12px] text-destructive">
                    {t("maint.engagement.loadFailed", { err: errorMessage(current.error) })}
                  </p>
                ) : (
                  <p className="text-[12px] text-muted-foreground">{t("maint.engagement.noEngagement")}</p>
                )}
              </section>

              {/* 进行中的变更请求 */}
              {ongoing.data && (
                <section className="border border-warning/40 bg-warning/5 rounded-2xl p-3.5 space-y-2">
                  <div className="flex items-center gap-2">
                    <AlertCircle className="w-4 h-4 text-warning" />
                    <h3 className="text-sm font-semibold">{t("maint.engagement.ongoingTitle")}</h3>
                  </div>
                  <p className="text-[11px] text-muted-foreground">{t("maint.engagement.ongoingDesc")}</p>
                  <div className="text-[12px] space-y-1">
                    {ongoing.data.pricing && (
                      <div>{t("maint.engagement.targetLabel", { what: ongoing.data.pricing.description })}</div>
                    )}
                    {ongoing.data.requestDate && (
                      <div className="text-muted-foreground">
                        {t("maint.engagement.requestTimeLabel", { time: fmtDateTime(ongoing.data.requestDate) })}
                      </div>
                    )}
                    {ongoing.data.order?.orderId && (
                      <div className="text-muted-foreground">
                        {t("maint.engagement.orderNoLabel", { id: ongoing.data.order.orderId })}
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
                        {t("maint.engagement.payBtn")}
                      </Button>
                    )}
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={handleCancelRequest}
                      disabled={deleteReq.isPending}
                    >
                      <X className="w-3.5 h-3.5 mr-1" />
                      {t("maint.engagement.cancelReqBtn")}
                    </Button>
                  </div>
                </section>
              )}

              {/* 可订阅的 engagement 列表 */}
              <section className="space-y-2">
                <div className="flex items-center justify-between gap-2 flex-wrap">
                  <h3 className="text-sm font-semibold">{t("maint.engagement.availableTitle")}</h3>
                  <p className="text-[10px] text-muted-foreground">{t("maint.engagement.availableHint")}</p>
                </div>
                {(available.data || []).length === 0 ? (
                  <EmptyState icon={CalendarRange} title={t("maint.engagement.emptyAvailable")} />
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
                        {t("maint.engagement.ongoingBlock")}
                      </p>
                    )}
                    {!ongoing.data && ongoing.isError && (
                      <p className="text-[11px] text-warning text-center pt-1">
                        {t("maint.engagement.ongoingError")}
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
            {t("common.close")}
          </Button>
        </DialogFooter>
      </DialogContent>

      {/* 二次确认子弹窗 */}
      <Dialog open={!!confirmMode} onOpenChange={(v) => !v && setConfirmMode(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("maint.engagement.confirmTitle")}</DialogTitle>
            <DialogDescription>{t("maint.engagement.confirmDesc")}</DialogDescription>
          </DialogHeader>
          <ol className="space-y-1.5 text-[12px] text-muted-foreground list-decimal pl-5">
            <li>
              <Trans
                i18nKey="maint.engagement.step1"
                components={{ b: <span className="font-semibold text-foreground" /> }}
              />
            </li>
            <li>
              <Trans
                i18nKey="maint.engagement.step2"
                components={{ b: <span className="font-semibold text-foreground" /> }}
              />
            </li>
            <li>{t("maint.engagement.step3")}</li>
            <li>{t("maint.engagement.step4")}</li>
            <li>{t("maint.engagement.step5")}</li>
          </ol>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmMode(null)}>
              {t("common.cancel")}
            </Button>
            <Button
              onClick={() => confirmMode && handleSubscribe(confirmMode)}
              disabled={createReq.isPending}
            >
              <Check className="w-3.5 h-3.5 mr-1" />
              {createReq.isPending ? t("maint.engagement.submitting") : t("maint.engagement.createOrderBtn")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 到期策略二次确认:CANCEL_SERVICE 不可撤销 */}
      <Dialog open={!!confirmStrategy} onOpenChange={(v) => !v && setConfirmStrategy(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="text-destructive">{t("maint.engagement.destroyTitle")}</DialogTitle>
            <DialogDescription>{t("maint.engagement.destroyDesc")}</DialogDescription>
          </DialogHeader>
          <div className="text-[12px] text-muted-foreground space-y-1.5">
            <p>
              <Trans
                i18nKey="maint.engagement.destroyBody1"
                components={{ b: <span className="font-semibold text-destructive" /> }}
              />
            </p>
            <p>{t("maint.engagement.destroyBody2")}</p>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmStrategy(null)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={updateRule.isPending}
              onClick={() => confirmStrategy && handleEndRule(confirmStrategy, true)}
            >
              {updateRule.isPending ? t("maint.engagement.submitting") : t("maint.engagement.confirmDestroyBtn")}
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
  const { t } = useTranslation();
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

  // 用 schema 的权威字段,不靠 pricingMode 字符串猜。
  // services.billing.engagement.TypeEnum = ['periodic','upfront'],描述原文:
  // "either fully pre-paid (upfront) or periodically paid up to engagement duration (periodic)"。
  // pricingMode 的描述只是 "Pricing model identifier" —— 自由字符串,没有枚举约束,
  // 猜错会让「总价」槽位显示成每期价(12 个月 periodic 显示一个月的钱)。
  // engagementConfiguration 缺失时才退回子串判断。
  const engType = pricing.engagementConfiguration?.type;
  const isUpfront =
    engType === "upfront" ||
    engType === "periodic" ? engType === "upfront" : pricing.pricingMode.toLowerCase().includes("upfront");
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
  const perMonthText = perMonth > 0 ? t("maint.engagement.perMonth", { price: formatMoney(perMonth, currency) }) : "";

  const friendlyTitle = humanizeDescription(t, pricing.description, months, isUpfront);

  return (
    <div className="border border-border rounded-xl p-3 flex items-center gap-3 flex-wrap">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <div className="text-[13px] font-semibold">{friendlyTitle}</div>
          <span className="text-[10px] font-medium px-1.5 py-0.5 rounded bg-secondary text-muted-foreground">
            {isUpfront ? t("maint.engagement.badgeUpfront") : t("maint.engagement.badgePeriodic")}
          </span>
        </div>
        {pricing.engagementConfiguration && (
          <div className="text-[11px] text-muted-foreground mt-1">
            {t("maint.engagement.endActionLabel", {
              strategy: endStrategyText(t, pricing.engagementConfiguration.defaultEndAction),
            })}
          </div>
        )}
      </div>
      <div className="text-right">
        <div className="text-[13px] font-semibold">{totalText}</div>
        {perMonthText && (
          <div className="text-[10px] text-muted-foreground">{perMonthText}</div>
        )}
        <Button size="sm" variant="outline" onClick={onSubscribe} disabled={disabled} className="mt-1.5">
          {t("maint.engagement.subscribeBtn")}
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

// humanizeDescription "rental for 12 months" → 本地化的「N 年/N 个月 + 预付/周期」标题
function humanizeDescription(t: TFunction, desc: string, months: number, isUpfront: boolean): string {
  if (months > 0) {
    const human =
      months % 12 === 0
        ? t("maint.engagement.durationYears", { n: months / 12 })
        : t("maint.engagement.durationMonths", { n: months });
    return t(isUpfront ? "maint.engagement.upfrontTitle" : "maint.engagement.periodicTitle", {
      duration: human,
    });
  }
  return desc || "—";
}
