/**
 * 框架无关的 API 客户端(React Native fetch / 网页 fetch 通用)。
 *
 * 网页端的 lib/api.ts 绑死了 axios + localStorage + sonner,App 用不了 ——
 * 这里只做三件事:基址、鉴权头、错误解包。凭据由调用方注入(网页从
 * localStorage 读,App 从 SecureStore 读),客户端本身不持有存储。
 */

export interface ApiClientOptions {
  /** 后端基址,如 https://ovh.example.com(不带 /api 后缀) */
  baseUrl: string;
  /** 设备令牌(App 配对产物),走 Authorization: Bearer */
  deviceToken?: string;
  /** 访问密钥(手动填密钥的兜底路径),走 X-API-Key */
  apiKey?: string;
  /** 当前账户 id,自动追加 ?account=xxx —— 与网页同协议 */
  accountId?: string;
  /** 超时毫秒,默认 20s(OVH 侧接口另有自己的客户端超时,这里只管到自建后端) */
  timeoutMs?: number;
}

/** 后端统一的失败结构:HTTP 非 2xx 时 body 里必有 error(中文可读) */
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

/** 从响应体里取人话错误文案;取不到就退回状态码描述 */
function extractErrorMessage(status: number, body: unknown): string {
  if (body && typeof body === "object") {
    const b = body as Record<string, unknown>;
    for (const key of ["message", "error", "msg"]) {
      const v = b[key];
      if (typeof v === "string" && v.trim()) return v;
    }
  }
  if (typeof body === "string" && body.trim()) return body.slice(0, 200);
  return status === 401 ? "鉴权失败:令牌或密钥无效" : `请求失败(HTTP ${status})`;
}

/** 组装完整 URL:拼 /api 前缀与 account 查询参数 */
function buildUrl(opts: ApiClientOptions, path: string, extraQuery?: Record<string, string>): string {
  const base = opts.baseUrl.replace(/\/+$/, "");
  const url = new URL(base + (path.startsWith("/api") ? path : "/api" + path));
  if (opts.accountId) url.searchParams.set("account", opts.accountId);
  if (extraQuery) {
    for (const [k, v] of Object.entries(extraQuery)) url.searchParams.set(k, v);
  }
  return url.toString();
}

/** 发请求并解包;非 2xx 抛 ApiError(带后端中文文案) */
async function request<T>(opts: ApiClientOptions, method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (opts.deviceToken) headers["Authorization"] = `Bearer ${opts.deviceToken}`;
  else if (opts.apiKey) headers["X-API-Key"] = opts.apiKey;
  if (body !== undefined) headers["Content-Type"] = "application/json";

  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), opts.timeoutMs ?? 20_000);
  let res: Response;
  try {
    res = await fetch(buildUrl(opts, path), {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: ctrl.signal,
    });
  } catch (e) {
    // 中断/网络失败统一成一句话,别把 TypeError 甩到界面上
    clearTimeout(timer);
    throw new ApiError(0, e instanceof Error && e.name === "AbortError" ? "请求超时" : "连不上后端,检查地址与网络");
  }
  clearTimeout(timer);

  const text = await res.text();
  let parsed: unknown = undefined;
  try {
    parsed = text ? JSON.parse(text) : undefined;
  } catch {
    parsed = text;
  }
  if (!res.ok) throw new ApiError(res.status, extractErrorMessage(res.status, parsed));
  return parsed as T;
}

/** 创建一组动词方法;每次调用时重读 opts,便于配对完成后原地换凭据 */
export function createApiClient(getOpts: () => ApiClientOptions) {
  return {
    get: <T>(path: string, query?: Record<string, string>) => {
      // query 拼进 URL 由 buildUrl 处理;这里借 body 位传
      const o = getOpts();
      const merged: ApiClientOptions = query ? { ...o, accountId: o.accountId } : o;
      if (query) {
        const qs = new URLSearchParams(query).toString();
        return request<T>(merged, "GET", qs ? path + (path.includes("?") ? "&" : "?") + qs : path);
      }
      return request<T>(merged, "GET", path);
    },
    post: <T>(path: string, body?: unknown) => request<T>(getOpts(), "POST", path, body ?? {}),
    put: <T>(path: string, body?: unknown) => request<T>(getOpts(), "PUT", path, body),
    del: <T>(path: string) => request<T>(getOpts(), "DELETE", path),
  };
}

export type ApiClient = ReturnType<typeof createApiClient>;
