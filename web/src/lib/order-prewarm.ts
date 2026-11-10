import { apiDatacenterCode } from "@/lib/datacenters";

export interface PrewarmRequest {
  duration?: string;
  account_id: string;
  planCode: string;
  datacenters: string[];
  options: string[];
  quantity: number;
  coupon: string;
  preorder: boolean;
}

export interface PrewarmStatus {
  duration?: string;
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
  return JSON.stringify({ account_id: request.account_id, planCode: request.planCode.trim(),
    quantity: request.quantity, coupon: request.coupon.trim(), preorder: request.preorder,
    duration: request.duration || "P1M",
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
