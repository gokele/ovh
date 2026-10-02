/**
 * 连接状态:后端地址 + 设备令牌(SecureStore 持久化)+ 当前账户。
 * 令牌经 /api/app/pair 兑换获得;手机丢了在网页端吊销。
 */
import * as SecureStore from "expo-secure-store";
import { createApiClient, type ApiClient } from "@core/api-client";
import type { PairResult } from "@core/types";

const KEY_SERVER = "ovh_server_url";
const KEY_TOKEN = "ovh_device_token";
const KEY_ACCOUNT = "ovh_active_account";
const KEY_DEVICE_ID = "ovh_device_id";

/** 读连接配置(启动时一次) */
export async function loadConnection(): Promise<{ serverUrl: string; token: string; accountId: string }> {
  const [serverUrl, token, accountId] = await Promise.all([
    SecureStore.getItemAsync(KEY_SERVER),
    SecureStore.getItemAsync(KEY_TOKEN),
    SecureStore.getItemAsync(KEY_ACCOUNT),
  ]);
  return { serverUrl: serverUrl || "", token: token || "", accountId: accountId || "" };
}

/** 兑换配对码并把令牌落 SecureStore(成功后返回服务器版本号) */
export async function pairWithCode(serverUrl: string, code: string, deviceName: string): Promise<string> {
  const client = createApiClient(() => ({ baseUrl: serverUrl }));
  const res = await client.post<{ token: string; deviceId: number; serverVersion?: string }>("/app/pair", {
    code: code.trim().toUpperCase(),
    deviceName: deviceName.trim() || "iPhone",
  });
  await Promise.all([
    SecureStore.setItemAsync(KEY_SERVER, serverUrl.replace(/\/+$/, "")),
    SecureStore.setItemAsync(KEY_TOKEN, res.token),
    SecureStore.setItemAsync(KEY_DEVICE_ID, String(res.deviceId)),
  ]);
  return res.serverVersion || "";
}

/** 手动填密钥的兜底路径(扫码之外的另一种配对) */
export async function saveManualKey(serverUrl: string, apiKey: string): Promise<void> {
  await SecureStore.setItemAsync(KEY_SERVER, serverUrl.replace(/\/+$/, ""));
  await SecureStore.setItemAsync(KEY_TOKEN, "__key__" + apiKey);
}

/** 令牌可能是设备令牌,也可能是 __key__ 前缀的手动密钥 —— 拆开给客户端 */
function splitToken(raw: string): { deviceToken?: string; apiKey?: string } {
  if (raw.startsWith("__key__")) return { apiKey: raw.slice(7) };
  return { deviceToken: raw };
}

/** 创建 API 客户端(每次调用重读,配对完成后原地生效) */
export function makeClient(conn: { serverUrl: string; token: string; accountId: string }): ApiClient {
  const cred = splitToken(conn.token);
  return createApiClient(() => ({
    baseUrl: conn.serverUrl,
    deviceToken: cred.deviceToken,
    apiKey: cred.apiKey,
    accountId: conn.accountId,
  }));
}

/** 切换全局账户(与 web 同语义:所有请求自动带 ?account=) */
export async function setAccountId(id: string): Promise<void> {
  if (id) await SecureStore.setItemAsync(KEY_ACCOUNT, id);
  else await SecureStore.deleteItemAsync(KEY_ACCOUNT);
}

/** 断开连接(吊销由网页端做,这里只清本地) */
export async function forgetConnection(): Promise<void> {
  await Promise.all([
    SecureStore.deleteItemAsync(KEY_SERVER),
    SecureStore.deleteItemAsync(KEY_TOKEN),
    SecureStore.deleteItemAsync(KEY_DEVICE_ID),
  ]);
}
