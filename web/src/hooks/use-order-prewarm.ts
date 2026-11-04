import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { apiMessage } from "@/lib/api-error";
import { prewarmID, prewarmIdentity, type PrewarmRequest, type PrewarmStatus } from "@/lib/order-prewarm";

export function useOrderPrewarm(enabled: boolean, request: PrewarmRequest) {
  const qc = useQueryClient();
  const identity = enabled ? prewarmIdentity(request) : "";
  const current = useRef<{ id: string; identity: string; preserve: boolean; pinned: boolean; controller: AbortController }>();
  const [view, setView] = useState<{ status?: PrewarmStatus; error?: string; pinning?: boolean }>({});

  useEffect(() => {
    setView({});
    if (!identity) return;
    const body = JSON.parse(identity) as PrewarmRequest;
    const session = { id: prewarmID(), identity, preserve: false, pinned: false, controller: new AbortController() };
    current.current = session;
    let stopped = false;
    let poll: ReturnType<typeof setTimeout>;
    let lastRefresh = 0;
    const update = async () => {
      try {
        let items: PrewarmStatus[];
        if (!lastRefresh || Date.now() - lastRefresh > 40_000) {
          items = (await api.post("/purchase/prewarm", { ...body, id: session.id, standby: session.pinned }, { signal: session.controller.signal })).data.items;
          lastRefresh = Date.now();
        } else {
          items = (await api.get("/purchase/prewarm", { params: { id: session.id }, signal: session.controller.signal })).data.items;
        }
        if (!stopped) setView({ status: items[0] });
      } catch (error) {
        if (!stopped) setView({ error: apiMessage(error) });
      } finally {
        if (!stopped) poll = setTimeout(update, 2000);
      }
    };
    const debounce = setTimeout(update, 600);
    return () => {
      stopped = true;
      clearTimeout(debounce);
      clearTimeout(poll);
      session.controller.abort();
      if (current.current === session) current.current = undefined;
      // DELETE also tombstones this ID if a cancelled POST arrives late.
      if (!session.preserve) void api.delete(`/purchase/prewarm/${session.id}`).catch(() => {});
    };
  }, [identity]);

  const pin = async () => {
    const session = current.current;
    if (!session || session.pinned) return;
    setView((v) => ({ ...v, pinning: true }));
    try {
      const { data } = await api.post<{ items: PrewarmStatus[] }>("/purchase/prewarm", {
        ...JSON.parse(identity), id: session.id, standby: true,
      }, { signal: session.controller.signal });
      if (current.current !== session) return;
      session.pinned = session.preserve = true;
      setView({ status: data.items[0] });
      void qc.invalidateQueries({ queryKey: ["purchase", "prewarm"] });
    } catch (error) {
      if (current.current === session) setView((v) => ({ ...v, pinning: false, error: apiMessage(error) }));
    }
  };

  return { ...view, enabled, pin, submitted: () => {
    if (current.current?.identity === identity) current.current.preserve = true;
  } };
}
