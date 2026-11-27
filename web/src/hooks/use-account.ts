import { useDefaultAccount } from "@/hooks/use-accounts";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { readPartialFailures, toPartialList, type PartialList } from "./partial-list";
import { useActiveAccount } from "@/hooks/use-active-account";

/** 默认账户也解析为实际 ID,切换默认账户后不能复用上一账户的缓存或付款窗口。 */
export function useAccountScope(): string {
  const [selected] = useActiveAccount();
  const fallback = useDefaultAccount();
  return selected || fallback?.id || "";
}

export interface AccountInfo {
  customerCode: string;
  nichandle: string;
  email: string;
  firstname?: string;
  name?: string;
  city?: string;
  country?: string;
  kycValidated?: boolean;
  state?: string;
  currency?: { code: string; symbol: string };
  /** OVH 子公司：IE / FR / DE / US / CA / ASIA / SG / AU / IN 等。决定结算货币和价格档 */
  ovhSubsidiary?: string;
}

export interface RefundRecord {
  refundId: string;
  orderId: string;
  date: string;
  priceWithTax: { value: number; text: string; currencyCode: string };
  pdfUrl?: string;
}

/**
 * OVH 官方订单记录(/me/order → billing.Order)。
 * priceWithTax/priceWithoutTax/tax 是 {value, text, currencyCode} 对象(OVH order.Price),
 * 展示一律用 text(带货币符号,后端格式化好的);retractionDate 非空且未过期 = 还在撤回期内。
 */
export interface OrderRecord {
  accountId?: string;
  orderId: number;
  orderStatus?: string;
  paymentStatusError?: string;
  date: string;
  expirationDate?: string | null;
  retractionDate?: string | null;
  pdfUrl?: string;
  /** OVH 控制台深链;后端在 OVH 没回 url 时会用账户 endpoint 补上 */
  url?: string;
  priceWithTax?: { value: number; text: string; currencyCode: string };
  priceWithoutTax?: { value: number; text: string; currencyCode: string };
  tax?: { value: number; text: string; currencyCode: string };
}

export interface EmailHistoryEntry {
  id: number;
  date: string;
  subject: string;
  body: string;
}

/**
 * OVH 账户信息。
 *
 * body 是后端把 OVH /me 原样透传的 nichandle，不能往里塞自定义字段，
 * 所以「账户 zone 与 OVH 实际 ovhSubsidiary 不一致」只能走 X-Subsidiary-Mismatch 响应头
 * （见后端 handlers.GetAccountInfo）。不读这个头，这条错配对用户完全不可见 ——
 * 而 zone 决定目录站点、价格币种和下单 region，配错了每一次调用都打在错误的站点上。
 * 浏览器里 header key 一律小写；同源请求不需要 Access-Control-Expose-Headers。
 */
export interface AccountInfoResult {
  info: AccountInfo;
  /** true = 账户配置的子公司与 OVH 返回的 ovhSubsidiary 不一致，详细原因在后端日志里 */
  subsidiaryMismatch: boolean;
}

export function useAccountInfo() {
  const accountId = useAccountScope();
  return useQuery({
    queryKey: qk.account.info(accountId),
    queryFn: async (): Promise<AccountInfoResult> => {
      const res = await api.get<AccountInfo>("/ovh/account/info", { params: { account: accountId } });
      const raw = (res.headers as Record<string, unknown> | undefined)?.["x-subsidiary-mismatch"];
      return { info: res.data, subsidiaryMismatch: String(raw ?? "") === "1" };
    },
  });
}

/** 信用余额(GET /me/credit/balance 及详情),只读 */
export interface CreditBalance {
  balanceName: string;
  type: string;
  usable: boolean;
  amount?: { value?: number; currencyCode?: string; text?: string };
  expiring?: { amount?: { value?: number; currencyCode?: string; text?: string }; expirationDate?: string }[];
}

export function useCreditBalances() {
  const accountId = useAccountScope();
  return useQuery({
    queryKey: qk.account.creditBalance(accountId),
    queryFn: async () => {
      const res = await api.get<{ data?: CreditBalance[]; failed?: number }>("/ovh/account/credit-balance", { params: { account: accountId } });
      return { list: res.data?.data || [], failed: res.data?.failed || 0 };
    },
    staleTime: 60_000,
  });
}

/** 预付款账户(GET /me/ovhAccount,只有 EU/CA);supported=false 表示该站点没有充值接口 */
export interface TopupInfo {
  accountId: string;
  supported: boolean;
  accounts: { id: string; balance: number | null; currency: string; balanceText: string; canBeCredited: boolean; isActive: boolean }[];
}

export function useTopupInfo(accountId: string) {
  return useQuery({
    queryKey: qk.account.topup(accountId),
    queryFn: async () => (await api.get<TopupInfo>("/ovh/account/topup", { params: { account: accountId } })).data,
    enabled: !!accountId,
    staleTime: 60_000,
    retry: false,
  });
}

export interface TopupOrder {
  accountId: string;
  orderId: number;
  url?: string;
  amount: number | null;
  currency: string;
  priceText: string;
}

/** 生成充值订单(只生成待付款订单,不扣款);不自动重试,避免多出几张订单 */
export function useCreateTopup(accountId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: { ovhAccountId: string; amount: number }) =>
      (await api.post<TopupOrder>("/ovh/account/topup", { ...body, confirm: true }, { params: { account: accountId } })).data,
    retry: false,
    onSettled: () => {
      qc.invalidateQueries({ queryKey: qk.account.orders(accountId) });
    },
  });
}

export interface RedeemResult {
  balanceName: string;
  type: string;
  amount: number | null;
  currency: string;
  text: string;
  expirationDate?: string;
}

/** 兑换券码到余额(POST /me/credit/code);不自动重试 */
export function useRedeemCode(accountId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (code: string) =>
      (await api.post<RedeemResult>("/ovh/account/credit-code", { code }, { params: { account: accountId } })).data,
    retry: false,
    onSettled: () => {
      qc.invalidateQueries({ queryKey: qk.account.creditBalance(accountId) });
      qc.invalidateQueries({ queryKey: qk.account.topup(accountId) });
    },
  });
}

/**
 * 退款记录。
 * body 仍是裸数组，但后端在部分详情拉取失败时会带 X-Partial-Failures 头，
 * 不读这个头的话「列表少了几条」对用户完全不可见，所以这里统一包成 PartialList。
 */
export function useRefunds() {
  const accountId = useAccountScope();
  return useQuery({
    queryKey: qk.account.refunds(accountId),
    queryFn: async (): Promise<PartialList<RefundRecord>> => {
      const res = await api.get<RefundRecord[]>("/ovh/account/refunds", { params: { account: accountId } });
      return toPartialList(res.data, readPartialFailures(res.headers));
    },
  });
}

/** 邮件历史。同 useRefunds：裸数组 + X-Partial-Failures 头 */
export function useEmails() {
  const accountId = useAccountScope();
  return useQuery({
    queryKey: qk.account.emails(accountId),
    queryFn: async (): Promise<PartialList<EmailHistoryEntry>> => {
      const res = await api.get<EmailHistoryEntry[]>("/ovh/account/email-history", { params: { account: accountId } });
      return toPartialList(res.data, readPartialFailures(res.headers));
    },
  });
}

/** OVH 订单记录。同 useRefunds：裸数组 + X-Partial-Failures 头 */
export function useOrders() {
  const accountId = useAccountScope();
  return useQuery({
    queryKey: qk.account.orders(accountId),
    queryFn: async (): Promise<PartialList<OrderRecord>> => {
      const res = await api.get<OrderRecord[]>("/ovh/account/orders", { params: { account: accountId } });
      return toPartialList(res.data, readPartialFailures(res.headers));
    },
  });
}


export interface OrderPaymentInfo {
  accountId: string;
  accountName: string;
  orderId: number;
  status: string;
  amount: number | null;
  currency: string;
  priceText: string;
  attempted: boolean;
  methods: { id: number; type: string; label: string; default: boolean }[];
  /** 可用信用余额(预付款/代金券/赠送/押金);enough=同币种且足够付清整单 */
  balances?: { name: string; type: string; amount: number; currency: string; text: string; enough: boolean }[];
  balanceError?: string;
}

/** 付款请求体:绑定支付方式,或用余额付清整单(POST /me/order/{id}/balance) */
export type OrderPayBody =
  | { kind: "method"; accountId: string; paymentMethodId: number; paymentType: string; amount: number; currency: string; confirm: true }
  | { kind: "balance"; accountId: string; balanceName: string; amount: number; currency: string; confirm: true };

/** 订单交付跟踪(只读):状态 + followUp 交付步骤(BETA)+ 商品明细 */
export interface OrderTracking {
  accountId: string;
  orderId: number;
  status: string;
  steps: { step: string; status: string; history: { date: string; label: string; description: string }[] }[];
  details: { description: string; detailType: string; quantity: string; domain?: string; cancelled: boolean; total?: string }[];
  followUpError?: string;
  detailsError?: string;
}

/** 进行中的订单每分钟刷新一次;已交付/已取消的不轮询 */
export function useOrderTracking(accountId: string, orderId: number, enabled: boolean) {
  return useQuery({
    queryKey: qk.account.orderTracking(accountId, orderId),
    queryFn: async () =>
      (await api.get<OrderTracking>(`/ovh/account/orders/${orderId}/tracking`, { params: { account: accountId } })).data,
    enabled: enabled && !!accountId && orderId > 0,
    staleTime: 30_000,
    retry: false,
    refetchInterval: (q) => {
      const s = (q.state.data as OrderTracking | undefined)?.status;
      return s && ["delivered", "cancelled"].includes(s) ? false : 60_000;
    },
  });
}

/** 打开付款窗口时只读取可用方式,不产生付款请求。 */
export function useOrderPaymentInfo(accountId: string, orderId: number, open: boolean) {
  return useQuery({
    queryKey: qk.account.orderPayment(accountId, orderId),
    queryFn: async () => (await api.get<OrderPaymentInfo>(`/ovh/account/orders/${orderId}/payment`, { params: { account: accountId } })).data,
    enabled: open, staleTime: 0, retry: false, refetchOnWindowFocus: false,
  });
}

/** 手动付款不自动重试;任何结果都刷新订单和付款登记。 */
export function usePayAccountOrder(accountId: string, orderId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: OrderPayBody): Promise<{ status?: string; orderStatus?: string; message?: string }> => {
      const { accountId: pinnedAccount, kind, ...payload } = body;
      const path = kind === "balance" ? "pay-balance" : "pay";
      return (await api.post(`/ovh/account/orders/${orderId}/${path}`, payload, { params: { account: pinnedAccount } })).data;
    },
    retry: false,
    onSettled: () => {
      qc.invalidateQueries({ queryKey: qk.account.orders(accountId) });
      qc.invalidateQueries({ queryKey: qk.account.orderPayment(accountId, orderId) });
      qc.invalidateQueries({ queryKey: qk.history() });
      qc.invalidateQueries({ queryKey: qk.account.creditBalance(accountId) });
      qc.invalidateQueries({ queryKey: qk.account.topup(accountId) });
    },
  });
}

/** 账户的默认支付方式:自动付款只会用它扣款,无效时 OVH 不扣款、订单保持待付款。只读。 */
export interface DefaultPaymentInfo {
  accountId: string;
  usable: boolean;
  found: boolean;
  label?: string;
  paymentType?: string;
  status?: string;
  expirationDate?: string;
  reason?: string;
}

/** 打开自动付款开关时才查询;失败不阻止开启,由界面如实显示。 */
export function useDefaultPaymentMethod(accountId: string, enabled: boolean) {
  return useQuery({
    queryKey: qk.account.defaultPayment(accountId),
    queryFn: async () => (await api.get<DefaultPaymentInfo>("/ovh/account/default-payment-method", { params: { account: accountId } })).data,
    enabled: enabled && !!accountId, staleTime: 60_000, retry: false, refetchOnWindowFocus: false,
  });
}
