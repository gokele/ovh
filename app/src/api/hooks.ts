/**
 * 数据 hooks:轻量自写(不引 react-query,v1 的轮询面很小,不值得多一层依赖)。
 * 统一语义:loading / error / data 三态,error 是人话中文。
 * 轮询只在组件挂载时跑(后台不轮询 —— 省电,且 TG 已是推送层)。
 */
import { useCallback, useEffect, useRef, useState } from "react";
import type { ApiClient } from "@core/api-client";

interface QueryState<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
}

/** 轮询式 GET hook:intervalMs=0 表示只拉一次 */
export function usePoll<T>(client: ApiClient | null, path: string, intervalMs = 0): QueryState<T> & { refresh: () => void } {
  const [state, setState] = useState<QueryState<T>>({ data: null, error: null, loading: true });
  const alive = useRef(true);

  const run = useCallback(async () => {
    if (!client) return;
    try {
      const d = await client.get<T>(path);
      if (alive.current) setState({ data: d, error: null, loading: false });
    } catch (e) {
      if (alive.current) {
        setState((s) => ({ data: s.data, error: e instanceof Error ? e.message : "请求失败", loading: false }));
      }
    }
  }, [client, path]);

  useEffect(() => {
    alive.current = true;
    run();
    let timer: ReturnType<typeof setInterval> | null = null;
    if (intervalMs > 0) timer = setInterval(run, intervalMs);
    return () => {
      alive.current = false;
      if (timer) clearInterval(timer);
    };
  }, [run, intervalMs]);

  return { ...state, refresh: run };
}

/** 一次性 POST 包装(动作按钮用):pending / run */
export function useAction(client: ApiClient | null) {
  const [pending, setPending] = useState(false);
  const run = useCallback(
    async (path: string, body?: unknown): Promise<{ ok: boolean; message?: string }> => {
      if (!client) return { ok: false, message: "未连接" };
      setPending(true);
      try {
        const res = await client.post<{ message?: string }>(path, body);
        return { ok: true, message: res?.message };
      } catch (e) {
        return { ok: false, message: e instanceof Error ? e.message : "操作失败" };
      } finally {
        setPending(false);
      }
    },
    [client],
  );
  return { pending, run };
}
