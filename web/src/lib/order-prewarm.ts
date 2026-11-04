import { apiDatacenterCode } from "@/lib/datacenters";

export interface PrewarmRequest {
  account_id: string;
  planCode: string;
  datacenters: string[];
  options: string[];
  quantity: number;
  autoPay: boolean;
  coupon: string;
  preorder: boolean;
}

export interface PrewarmStatus {
  id: string;
  accountId: string;
  planCode: string;
  datacenters: string[];
  ready: number;
  total: number;
  state: "preparing" | "ready" | "waiting";
  expiresAt?: string;
  standby?: boolean;
  estimated?: boolean;
  error?: string;
}

// A stable identity prevents rerenders or checkbox order from rebuilding carts.
export function prewarmIdentity(request: PrewarmRequest): string {
  return JSON.stringify({ ...request, planCode: request.planCode.trim(), coupon: request.coupon.trim(),
    datacenters: [...new Set(request.datacenters.map(apiDatacenterCode))].sort(),
    options: [...new Set(request.options)].sort() });
}

export function prewarmID(): string {
  // crypto.randomUUID requires HTTPS; the console can also be served over HTTP.
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
  const bytes = new Uint8Array(16);
  globalThis.crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}
