import { OrderDurationLabel } from "./OrderDurationField";
import { CheckCircle2, Loader2, ShoppingCart, Timer, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { PearlPanel, PearlPanelHead } from "@/components/pearl/ListKit";
import { useAccounts } from "@/hooks/use-accounts";
import type { useOrderPrewarm } from "@/hooks/use-order-prewarm";
import { api } from "@/lib/api";
import { apiMessage } from "@/lib/api-error";
import type { PrewarmStatus } from "@/lib/order-prewarm";
import { fmtDateTime } from "@/i18n/format";

export function OrderPrewarm({ preparation }: { preparation: ReturnType<typeof useOrderPrewarm> }) {
  const { t } = useTranslation();
  if (!preparation.enabled) return null;
  const status = preparation.status;
  const ready = status?.state === "ready";
  const waiting = !!preparation.error || status?.state === "waiting";
  const Icon = ready ? CheckCircle2 : waiting ? ShoppingCart : Loader2;
  return <div className="rounded-2xl border border-border bg-secondary/40 p-3.5 space-y-2.5">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div role="status" aria-live="polite" className="flex items-center gap-2 text-sm font-medium">
        <Icon aria-hidden="true" className={`h-4 w-4 shrink-0 ${!ready && !waiting ? "animate-spin motion-reduce:animate-none" : ""}`} />
        <span>{t(`prewarm.${ready ? "ready" : waiting ? "waiting" : "preparing"}`, { ready: status?.ready ?? 0, total: status?.total ?? 0 })}</span>
      </div>
      {!status?.standby && <Button type="button" variant="outline" size="sm" className="min-h-11" disabled={preparation.pinning} onClick={() => void preparation.pin()}>
        <Timer className="h-4 w-4" aria-hidden="true" />{t("prewarm.keep")}
      </Button>}
    </div>
    <p className="text-xs leading-relaxed text-muted-foreground">{t("prewarm.description")}</p>
    {status?.estimated && <p className="text-xs leading-relaxed text-muted-foreground">{t("prewarm.estimated")}</p>}
    {waiting && <p className="text-xs leading-relaxed text-muted-foreground">{t("prewarm.fallback")}</p>}
    {status?.standby && status.expiresAt && <p className="text-xs text-foreground">{t("prewarm.retained", { time: fmtDateTime(status.expiresAt) })}</p>}
  </div>;
}

export function StandbyCarts() {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const accounts = useAccounts();
  const query = useQuery({ queryKey: ["purchase", "prewarm"],
    queryFn: async () => (await api.get<{ items: PrewarmStatus[] }>("/purchase/prewarm")).data.items,
    refetchInterval: 5000 });
  const cancel = useMutation({ mutationFn: (id: string) => api.delete(`/purchase/prewarm/${encodeURIComponent(id.slice(4))}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["purchase", "prewarm"] }),
    onError: (error) => toast.error(apiMessage(error) || t("prewarm.cancelFailed")) });
  const items = query.data?.filter((s) => s.standby && s.id.startsWith("web:")) ?? [];
  if (items.length === 0) return null;
  return <PearlPanel>
    <PearlPanelHead eyebrow={t("prewarm.standbyTitle")} meta={`${items.length}`} />
    <div className="px-4 pb-4 sm:px-5">
      <p className="text-xs text-muted-foreground mb-3">{t("prewarm.description")}</p>
      <ul className="divide-y divide-border">
        {items.map((s) => <li key={s.id} className="flex items-center justify-between gap-3 py-3">
          <div className="min-w-0 space-y-1">
            <p className="text-sm font-medium break-all">{s.planCode} <span className="font-normal text-muted-foreground">· {accounts.data?.find((a) => a.id === s.accountId)?.name || s.accountId}</span></p>
            <p className="text-xs text-muted-foreground break-all">{s.datacenters.join(" / ").toUpperCase()} · <OrderDurationLabel value={s.duration} /> · {t(`prewarm.${s.state}`, { ready: s.ready, total: s.total })}</p>
            {s.expiresAt && <p className="text-xs text-muted-foreground">{t("prewarm.expires", { time: fmtDateTime(s.expiresAt) })}</p>}
          </div>
          <Button variant="ghost" className="min-h-11 min-w-11 shrink-0" disabled={cancel.isPending} aria-label={t("prewarm.cancel", { plan: s.planCode })} onClick={() => cancel.mutate(s.id)}><X className="h-4 w-4" /></Button>
        </li>)}
      </ul>
    </div>
  </PearlPanel>;
}
