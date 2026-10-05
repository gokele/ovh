import { Cog, RefreshCw } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed } from "@/components/common/LoadFailed";
import { useServerBiosSettings } from "@/hooks/use-server-control";
import { useTranslation } from "react-i18next";

/** BIOS 设置查看（含 SGX 子项；只读，旧前端也是只读展示） */
export function BiosDialog({
  serviceName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const q = useServerBiosSettings(serviceName, open);
  const { t } = useTranslation();
  const settings = q.data?.settings || {};
  const sgx = q.data?.sgx;
  const keys = Object.keys(settings).filter((k) => k !== "success" && k !== "sgx");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl max-h-[85vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Cog className="w-5 h-5" />
            {t("ctrl.bios.title")}
          </DialogTitle>
          <DialogDescription>{t("ctrl.bios.desc")}</DialogDescription>
        </DialogHeader>

        <div className="overflow-y-auto -mx-6 px-6 space-y-4">
          {q.isPending ? (
            <Skeleton className="h-40 rounded-2xl" />
          ) : q.isError ? (
            // hook 里以前把所有异常吞成 {},这里就永远只显示「未获取到 BIOS 设置」——
            // 和"这个机型确实不暴露 BIOS"完全无法区分。现在 hook 只对 404/501 返回空对象
            // (那才是业务事实),其余错误抛出来走这条分支。
            <LoadFailed icon={Cog} title={t("ctrl.bios.loadFailed")} error={q.error} onRetry={() => q.refetch()} />
          ) : keys.length === 0 && !sgx ? (
            <EmptyState icon={Cog} title={t("ctrl.bios.empty")} />
          ) : (
            <>
              {keys.length > 0 && (
                <div className="border border-border rounded-2xl overflow-hidden">
                  <table className="w-full text-[13px]">
                    <tbody>
                      {keys.map((k) => (
                        <tr key={k} className="border-b border-border last:border-b-0">
                          <td className="py-2 px-4 font-mono text-[12px] text-muted-foreground w-1/3">{k}</td>
                          <td className="py-2 px-4 font-mono break-all">{formatValue((settings as any)[k])}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              {sgx && (
                <div className="border border-border rounded-2xl overflow-hidden">
                  <div className="px-4 py-2 bg-secondary/50 text-[12px] font-semibold">SGX (Intel Software Guard Extensions)</div>
                  <table className="w-full text-[13px]">
                    <tbody>
                      {Object.entries(sgx).map(([k, v]) => (
                        <tr key={k} className="border-t border-border">
                          <td className="py-2 px-4 font-mono text-[12px] text-muted-foreground w-1/3">{k}</td>
                          <td className="py-2 px-4 font-mono break-all">{formatValue(v)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => q.refetch()} disabled={q.isFetching}>
            <RefreshCw className={`w-3.5 h-3.5 mr-1 ${q.isFetching ? "animate-spin" : ""}`} />
            {t("common.refresh")}
          </Button>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.close")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function formatValue(v: unknown): string {
  if (v === null || v === undefined) return "—";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}
