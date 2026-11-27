import { PackageCheck, RefreshCw } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { OrderTimeline } from "@/components/account/OrderTimeline";
import { AccountChip } from "@/components/common/AccountChip";
import { LoadFailed } from "@/components/common/LoadFailed";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { trackingOrderId, useOrderTracking } from "@/hooks/use-account";

export function OrderTrackingButton({ accountId, orderId, children }: { accountId?: string; orderId?: string | number; children?: ReactNode }) {
  const { t } = useTranslation();
  const id = trackingOrderId(accountId, orderId);
  if (!accountId || !id) return children ?? null;
  const label = t("orderTrack.trackNumber", { id });
  return (
    <Dialog key={JSON.stringify([accountId, id])}>
      <DialogTrigger asChild>
        <button type="button" className="inline-flex items-center gap-1 text-xs font-medium text-primary underline-offset-4 hover:underline"
          title={label} aria-label={children ? undefined : label}>
          {children ? <>{children}<span className="sr-only"> · {label}</span></> : <><PackageCheck className="h-3.5 w-3.5" aria-hidden />{t("orderTrack.trackOrder")}</>}
        </button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("orderTrack.trackNumber", { id })}</DialogTitle>
          <DialogDescription>{t("orderTrack.originalAccount")}</DialogDescription>
        </DialogHeader>
        {/* Mounted only while open: rendering a page of rows must not fetch their orders. */}
        <OrderTrackingDetails accountId={accountId} orderId={id} />
      </DialogContent>
    </Dialog>
  );
}

function OrderTrackingDetails({ accountId, orderId }: { accountId: string; orderId: string }) {
  const { t } = useTranslation();
  const track = useOrderTracking(accountId, orderId, true);
  const { data } = track;
  if (track.isError) return <LoadFailed compact error={track.error} onRetry={() => void track.refetch()} />;
  if (!data) return <p role="status" className="text-sm text-muted-foreground">{t("orderTrack.loadingTrack")}</p>;
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <AccountChip accountId={accountId} />
        <span className="text-sm font-medium">{t(`orderTrack.status.${data.status}`, { defaultValue: data.status })}</span>
        <Button type="button" size="sm" variant="ghost" className="ml-auto" disabled={track.isFetching} onClick={() => void track.refetch()}>
          <RefreshCw className="h-3.5 w-3.5" aria-hidden />{t("common.refresh")}
        </Button>
      </div>
      {!data.followUpError && data.steps.length > 0 && (
        <ul className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
          {data.steps.map((s, i) => <li key={i}>{t(`orderTrack.step.${s.step}`, { defaultValue: s.step })} · {t(`orderTrack.stepStatus.${s.status}`, { defaultValue: s.status })}</li>)}
        </ul>
      )}
      <OrderTimeline data={data} />
    </div>
  );
}
