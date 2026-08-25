import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { toast } from "sonner";

export interface OVHAccount {
  id: string;
  name: string;
  endpoint: string;
  zone: string;
  appKey: string;
  appSecret: string;
  consumerKey: string;
  iam: string;
  isDefault: boolean;
  createdAt: string;
}

export interface AccountInput {
  name: string;
  zone: string;
  endpoint?: string; // 可空, 后端按 zone 推
  appKey: string;
  appSecret: string;
  consumerKey: string;
  iam?: string;
  setDefault?: boolean;
}

const ACCOUNTS_KEY = ["accounts", "list"] as const;

/**
 * 创建 / 更新 / 验证账户时后端带回来的子公司错配说明(空 = 没问题)。
 *
 * 后端(handlers.SubsidiaryMismatchNote)拿账户里存的 zone 跟 OVH /me 返回的 ovhSubsidiary 比:
 * zone 决定目录站点、价格币种和下单 region,ovhSubsidiary 才是 OVH 认的归属。
 * 两者不同区时凭据依然 valid=true,所有请求却都会打到错误的站点 —— 这段话是用户在
 * 真正下单失败之前唯一能看到的信号,必须原样显示,不能只吞成一句"验证通过"。
 */
export interface AccountVerifyResult {
  valid: boolean;
  subsidiaryWarning?: string;
}

/** 全部账户列表(默认账户排首位) */
export function useAccounts() {
  return useQuery({
    queryKey: ACCOUNTS_KEY,
    queryFn: async () => {
      const res = await api.get<{ accounts: OVHAccount[] }>("/accounts");
      return res.data.accounts || [];
    },
    staleTime: 5 * 60_000,
  });
}

/** 默认账户(取列表中 isDefault, 没有就第一个) */
export function useDefaultAccount(): OVHAccount | null {
  const q = useAccounts();
  const list = q.data || [];
  return list.find((a) => a.isDefault) || list[0] || null;
}

/** 创建账户。后端会自动调 /me 验证,返回 valid 字段 */
export function useCreateAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: AccountInput) => {
      const res = await api.post<{ account: OVHAccount } & AccountVerifyResult>("/accounts", input);
      return res.data;
    },
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: ACCOUNTS_KEY });
      if (data.valid) {
        toast.success(`账户 ${data.account.name} 创建成功`);
      } else {
        toast.warning(`账户已保存,但 OVH 验证失败,请检查凭据`);
      }
      // 子公司填错不会让 valid 变 false,但会让目录/价格/下单全部走错站点,单独长时间提示
      if (data.subsidiaryWarning) {
        toast.warning(data.subsidiaryWarning, { duration: 15000 });
      }
    },
    onError: (e: any) => toast.error(e?.response?.data?.error || "创建失败"),
  });
}

/** 更新账户 */
export function useUpdateAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, input }: { id: string; input: Partial<AccountInput> }) => {
      const res = await api.put<{ account: OVHAccount } & AccountVerifyResult>(`/accounts/${id}`, input);
      return res.data;
    },
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: ACCOUNTS_KEY });
      toast.success("账户已更新");
      if (!data.valid) {
        toast.warning("账户已保存,但 OVH 验证失败,请检查凭据");
      }
      if (data.subsidiaryWarning) {
        toast.warning(data.subsidiaryWarning, { duration: 15000 });
      }
    },
    onError: (e: any) => toast.error(e?.response?.data?.error || "更新失败"),
  });
}

/** 删除账户(级联删除关联的 queue/history/sniper 记录) */
export function useDeleteAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => (await api.delete(`/accounts/${id}`)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ACCOUNTS_KEY });
      // 关联数据也变了,顺手 invalidate
      qc.invalidateQueries({ queryKey: ["queue"] });
      qc.invalidateQueries({ queryKey: ["history"] });
      toast.success("账户已删除,关联数据一并清理");
    },
    onError: (e: any) => toast.error(e?.response?.data?.error || "删除失败"),
  });
}

/** 把指定账户标为默认 */
export function useSetDefaultAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => (await api.post(`/accounts/${id}/set-default`)).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ACCOUNTS_KEY });
      toast.success("已设为默认账户");
    },
    onError: (e: any) => toast.error(e?.response?.data?.error || "设默认失败"),
  });
}

/** 重新验证账户凭据(调 OVH /me) */
export function useVerifyAccount() {
  return useMutation({
    mutationFn: async (id: string) =>
      (await api.post<AccountVerifyResult>(`/accounts/${id}/verify`)).data,
    onSuccess: (data) => {
      if (data.valid) {
        toast.success("OVH 凭据验证通过");
      } else {
        toast.error("OVH 凭据验证失败,检查 AppKey / AppSecret / ConsumerKey");
      }
      // 凭据有效 ≠ 区配对了。这条警告比"验证通过"重要得多,单独弹且停久一点
      if (data.subsidiaryWarning) {
        toast.warning(data.subsidiaryWarning, { duration: 15000 });
      }
    },
  });
}

/** 按 ID 查账户(从 useAccounts 缓存里找,不发请求) */
export function findAccountByID(accounts: OVHAccount[] | undefined, id: string): OVHAccount | undefined {
  if (!accounts || !id) return undefined;
  return accounts.find((a) => a.id === id);
}

/** zone 颜色映射, 用于账户 chip 区分(EU 蓝 / US 红 / CA 绿 等) */
export function accountChipColor(zone: string): string {
  const z = zone.toUpperCase();
  if (z === "US") return "bg-red-100 text-red-700 dark:bg-red-950/40 dark:text-red-300";
  if (z === "CA" || z === "QC") return "bg-green-100 text-green-700 dark:bg-green-950/40 dark:text-green-300";
  if (z === "ASIA" || z === "SG" || z === "AU" || z === "IN") return "bg-orange-100 text-orange-700 dark:bg-orange-950/40 dark:text-orange-300";
  // EU 系
  return "bg-blue-100 text-blue-700 dark:bg-blue-950/40 dark:text-blue-300";
}
