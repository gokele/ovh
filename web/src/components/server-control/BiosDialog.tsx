import { Cog, RefreshCw } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { useServerBiosSettings } from "@/hooks/use-server-control";

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
  const settings = q.data?.settings || {};
  const sgx = q.data?.sgx;
  const keys = Object.keys(settings).filter((k) => k !== "success" && k !== "sgx");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl max-h-[85vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Cog className="w-5 h-5" />
            BIOS 设置
          </DialogTitle>
          <DialogDescription>当前 BIOS 配置（只读）。如需调整请通过 OVH 工单流程。</DialogDescription>
        </DialogHeader>

        <div className="overflow-y-auto -mx-6 px-6 space-y-4">
          {q.isPending ? (
            <Skeleton className="h-40 rounded-2xl" />
          ) : keys.length === 0 && !sgx ? (
            <EmptyState icon={Cog} title="未获取到 BIOS 设置" />
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
            刷新
          </Button>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            关闭
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
