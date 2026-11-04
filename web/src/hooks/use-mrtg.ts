import { useAccountScope } from "@/hooks/use-account";
import { useQueries } from "@tanstack/react-query";
import { useAccountApi } from "@/hooks/use-account-api";
import { qk, accountQueryKey } from "@/lib/query";

export type MrtgPeriod = "hourly" | "daily" | "weekly" | "monthly" | "yearly";

export interface MrtgPoint {
  timestamp: number;
  value: { value: number; unit: string };
}

export interface MrtgInterface {
  mac: string;
  data: MrtgPoint[];
}

export interface MrtgResponse {
  success: boolean;
  interfaces: MrtgInterface[];
}

/**
 * 同时拉 download 和 upload 两条流量曲线。
 * 旧前端协议：GET /server-control/:serviceName/mrtg?period=...&type=traffic:download|upload
 * 返回 { success, interfaces: [{ mac, data: [{ timestamp, value: {value, unit} }] }] }
 */
export function useMrtgTraffic(serviceName: string | null, period: MrtgPeriod) {
  const accountId = useAccountScope();
  const api = useAccountApi(accountId);
  const results = useQueries({
    queries: [
      {
        queryKey: accountQueryKey(qk.serverControl.mrtg(serviceName || "", period, "download"), accountId),
        queryFn: async () => {
          const res = await api.get<MrtgResponse>(
            `/server-control/${serviceName}/mrtg?period=${period}&type=traffic:download`
          );
          return res.data;
        },
        enabled: !!serviceName,
        staleTime: 60_000,
      },
      {
        queryKey: accountQueryKey(qk.serverControl.mrtg(serviceName || "", period, "upload"), accountId),
        queryFn: async () => {
          const res = await api.get<MrtgResponse>(
            `/server-control/${serviceName}/mrtg?period=${period}&type=traffic:upload`
          );
          return res.data;
        },
        enabled: !!serviceName,
        staleTime: 60_000,
      },
    ],
  });

  const [download, upload] = results;
  return {
    download: download.data,
    upload: upload.data,
    isPending: download.isPending || upload.isPending,
    isFetching: download.isFetching || upload.isFetching,
    isError: download.isError || upload.isError,
    // 把 error 也交出去:组件要拿它渲染"为什么没数据",不能只给一个 boolean
    error: download.error ?? upload.error,
    refetch: () => Promise.all([download.refetch(), upload.refetch()]),
  };
}
