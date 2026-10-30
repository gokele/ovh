import { api } from "@/lib/api";

export type ChatProduct = "dedicated" | "vps";
export interface ChatIdentity { accountName: string; customerCode: string }
export interface ChatTarget { closed: boolean; location: { replace: (url: string) => void } }

/** 一次性认证链接仅用于立即导航,不会进入查询缓存、组件状态或本地存储。 */
export async function authorizeOfficialChat(
  accountId: string,
  product: ChatProduct,
  target: ChatTarget,
  signal: AbortSignal,
): Promise<ChatIdentity | null> {
  const { data } = await api.post<ChatIdentity & { accountId: string; launchUrl: string }>(
    "/ovh/support/chat/session", { product }, { params: { account: accountId }, signal },
  );
  if (signal.aborted || target.closed) return null;
  let destination: URL;
  try { destination = new URL(data.launchUrl); }
  catch { throw new Error("CHAT_INVALID_RESPONSE"); }
  if (data.accountId !== accountId || !data.customerCode ||
    destination.origin !== "https://agent-chat.ovhcloud.com" ||
    destination.pathname !== "/sn_va_web_client_app_embed.do" ||
    destination.username || destination.password ||
    destination.searchParams.get("sysparm_queue") !== product ||
    !destination.searchParams.get("sysparm_session_id")) {
    throw new Error("CHAT_INVALID_RESPONSE");
  }
  target.location.replace(destination.href);
  return { accountName: data.accountName, customerCode: data.customerCode };
}
