import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { toast } from "sonner";
import i18n from "@/i18n";
import { apiMessage } from "@/lib/api-error";

export interface LogEntry {
  id: string;
  timestamp: string;
  level: "INFO" | "WARNING" | "ERROR" | "DEBUG";
  message: string;
  source: string;
}

/** 日志列表 */
export function useLogs(autoRefresh: boolean = true) {
  return useQuery({
    queryKey: qk.logs(),
    // Array.isArray:调用方直接 .filter/.map,truthy 非数组会整页白屏
    queryFn: async () => {
      const d = (await api.get<LogEntry[]>("/logs")).data;
      return Array.isArray(d) ? d : [];
    },
    refetchInterval: autoRefresh ? 5000 : false,
  });
}

/** 清空日志 */
export function useClearLogs() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => (await api.delete("/logs")).data,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.logs() });
      toast.success(i18n.t("hooksMsg.logs.cleared"));
    },
    onError: (e: any) => toast.error(apiMessage(e)),
  });
}
