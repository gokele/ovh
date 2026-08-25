import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { readPartialFailures, toPartialList, type PartialList } from "./partial-list";

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
  return useQuery({
    queryKey: qk.account.info(),
    queryFn: async (): Promise<AccountInfoResult> => {
      const res = await api.get<AccountInfo>("/ovh/account/info");
      const raw = (res.headers as Record<string, unknown> | undefined)?.["x-subsidiary-mismatch"];
      return { info: res.data, subsidiaryMismatch: String(raw ?? "") === "1" };
    },
  });
}

/**
 * 退款记录。
 * body 仍是裸数组，但后端在部分详情拉取失败时会带 X-Partial-Failures 头，
 * 不读这个头的话「列表少了几条」对用户完全不可见，所以这里统一包成 PartialList。
 */
export function useRefunds() {
  return useQuery({
    queryKey: qk.account.refunds(),
    queryFn: async (): Promise<PartialList<RefundRecord>> => {
      const res = await api.get<RefundRecord[]>("/ovh/account/refunds");
      return toPartialList(res.data, readPartialFailures(res.headers));
    },
  });
}

/** 邮件历史。同 useRefunds：裸数组 + X-Partial-Failures 头 */
export function useEmails() {
  return useQuery({
    queryKey: qk.account.emails(),
    queryFn: async (): Promise<PartialList<EmailHistoryEntry>> => {
      const res = await api.get<EmailHistoryEntry[]>("/ovh/account/email-history");
      return toPartialList(res.data, readPartialFailures(res.headers));
    },
  });
}
