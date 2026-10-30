import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";

export interface SupportTicket {
  ticketId: number;
  ticketNumber?: number;
  subject?: string;
  state?: string;
  category?: string;
  product?: string;
  serviceName?: string;
  creationDate?: string;
  updateDate?: string;
  canBeClosed?: boolean;
  error?: string;
  code?: string;
  params?: Record<string, unknown>;
}
export interface SupportOptions {
  accountId: string;
  accountName: string;
  categories: string[];
  products: string[];
  subcategories: string[];
}
export interface SupportTicketList {
  accountId: string;
  accountName: string;
  items: SupportTicket[];
  page: number;
  hasMore: boolean;
  failedCount: number;
}
export interface SupportDetail {
  accountId: string;
  accountName: string;
  ticket: SupportTicket;
  messages: {
    messageId: number;
    body: string;
    from: string;
    creationDate: string;
  }[];
}
export interface CreateSupportInput {
  subject: string;
  body: string;
  category: string;
  product: string;
  subcategory?: string;
  serviceName?: string;
}
export interface SupportCreated {
  accountId: string;
  result: {
    ticketId?: number;
    ticketNumber?: number;
    messageId?: number;
    additionalNotice?: string;
  };
}

/** 默认账户查询也显式传空串,避免请求拦截器跟随随后切换的账户。 */
export function useSupportTickets(
  accountId: string,
  page: number,
  status: string,
  subject = "",
) {
  return useQuery({
    queryKey: qk.support.list(accountId, page, status, subject),
    queryFn: async () =>
      (
        await api.get<SupportTicketList>("/ovh/support/tickets", {
          params: { account: accountId, page, status, subject },
        })
      ).data,
    retry: false,
  });
}

/** 创建窗口打开后才读取官方分类,不会产生工单。 */
export function useSupportOptions(accountId: string) {
  return useQuery({
    queryKey: qk.support.options(accountId),
    queryFn: async () =>
      (
        await api.get<SupportOptions>("/ovh/support/options", {
          params: { account: accountId },
        })
      ).data,
    retry: false,
  });
}

/** 关联服务随账户及所选产品读取,失败时允许用户手动填写。 */
export function useSupportServices(accountId: string, product: string) {
  return useQuery({
    queryKey: qk.support.services(accountId, product),
    queryFn: async () =>
      (
        await api.get<string[]>("/ovh/support/services", {
          params: { account: accountId, product },
        })
      ).data,
    enabled: !!product,
    retry: false,
  });
}

/** 工单详情始终固定到打开它时的实际账户。 */
export function useSupportDetail(accountId: string, id: number) {
  return useQuery({
    queryKey: qk.support.detail(accountId, id),
    queryFn: async () =>
      (
        await api.get<SupportDetail>(`/ovh/support/tickets/${id}`, {
          params: { account: accountId },
        })
      ).data,
    retry: false,
  });
}

/** 创建仅由用户提交触发,不自动重试或切换账户。 */
export function useCreateSupportTicket(accountId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: CreateSupportInput) =>
      (
        await api.post<SupportCreated>("/ovh/support/tickets", body, {
          params: { account: accountId },
        })
      ).data,
    retry: false,
    onSettled: () => qc.invalidateQueries({ queryKey: qk.support.all() }),
  });
}

/** 回复/关闭/重开不会自动重试;任何结果都重新读取真实状态。 */
export function useSupportAction(accountId: string, id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: {
      action: "reply" | "close" | "reopen";
      body?: string;
    }) =>
      (
        await api.post(`/ovh/support/tickets/${id}`, body, {
          params: { account: accountId },
        })
      ).data,
    retry: false,
    onSettled: () => {
      qc.invalidateQueries({ queryKey: qk.support.all() });
    },
  });
}
