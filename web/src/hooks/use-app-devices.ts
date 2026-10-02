/**
 * App 配对管理(网页端):生成配对码(倒计时)、设备列表、吊销。
 * 后端接口:POST /api/app/pairing-codes、GET/DELETE /api/app/devices。
 */
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { qk } from "@/lib/query";
import { toast } from "sonner";

export interface AppDeviceRow {
  id: number;
  name: string;
  createdAt: string;
  lastUsedAt?: string;
  revoked: boolean;
  revokedAt?: string;
}

export interface PairingCodeResp {
  code: string;
  expiresAt: string;
  pairUrl: string;
}

/** 设备列表(含已吊销;后端 10 分钟内不重复写 last_used,列表足够新鲜) */
export function useAppDevices() {
  return useQuery({
    queryKey: qk.app.devices(),
    queryFn: async () => (await api.get<{ devices: AppDeviceRow[] }>("/app/devices")).data.devices || [],
    staleTime: 60_000,
  });
}

/** 生成配对码:2 分钟一次性,组件拿着 expiresAt 做倒计时展示 */
export function useCreatePairingCode() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => (await api.post<PairingCodeResp>("/app/pairing-codes")).data,
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.app.devices() }),
    onError: () => toast.error("生成配对码失败"),
  });
}

/** 吊销设备:立即生效,该设备下一次请求就 401 */
export function useRevokeAppDevice() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: number) => (await api.delete(`/app/devices/${id}`)).data,
    onSuccess: () => {
      toast.success("设备已吊销,其令牌立即失效");
      qc.invalidateQueries({ queryKey: qk.app.devices() });
    },
    onError: () => toast.error("吊销失败"),
  });
}
