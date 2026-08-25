import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, getActiveAccount } from "@/lib/api";
import { toast } from "sonner";

/** 服务器本地别名 map: { service_name: alias }。
 *  - axios interceptor 会自动给 /server-control/* 加 ?account=<id>,所以不用手传
 *  - 切账户时 active-account 变 → axios 自动用新 id,需要前端再 invalidate 这个 query
 *  - alias 为空字符串等于"未设置",后端会去删除该行
 */
export function useServerAliases() {
  return useQuery<Record<string, string>>({
    queryKey: ["server-control", "aliases", getActiveAccount()],
    queryFn: async () => (await api.get<Record<string, string>>("/server-control/aliases")).data,
    staleTime: 30 * 60_000,
    gcTime: 60 * 60_000,
    refetchOnWindowFocus: false,
  });
}

/** 设置 / 删除一台机器的别名 */
export function useSetServerAlias() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ serviceName, alias }: { serviceName: string; alias: string }) => {
      const trimmed = alias.trim();
      if (trimmed === "") {
        await api.delete(`/server-control/${encodeURIComponent(serviceName)}/alias`);
      } else {
        await api.put(`/server-control/${encodeURIComponent(serviceName)}/alias`, { alias: trimmed });
      }
      return { serviceName, alias: trimmed };
    },
    onSuccess: ({ alias }) => {
      qc.invalidateQueries({ queryKey: ["server-control", "aliases"] });
      toast.success(alias === "" ? "已清除别名" : "别名已保存");
    },
    onError: (e: any) => {
      toast.error(e?.response?.data?.error || "保存失败");
    },
  });
}

/** 显示用:有别名取别名,没别名取原名(通常是 service_name 或 commercial display name) */
export function aliasOf(aliases: Record<string, string> | undefined, serviceName: string, fallback: string): string {
  const a = aliases?.[serviceName];
  return a && a !== "" ? a : fallback;
}
