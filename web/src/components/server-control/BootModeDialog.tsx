import { useState } from "react";
import { HardDrive, Shield, Power, Network, Database, Check } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { useServerBootModes, useSetServerBootMode, useRebootServer, type BootMode } from "@/hooks/use-server-control";
import { toast } from "sonner";

/** 启动模式对话框：列出所有可选启动模式，点击非当前项即切换 + 自动重启（对齐旧前端） */
export function BootModeDialog({
  serviceName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const q = useServerBootModes(serviceName, open);
  const setBoot = useSetServerBootMode();
  const reboot = useRebootServer();
  const [confirmingId, setConfirmingId] = useState<number | null>(null);

  const handlePick = (mode: BootMode) => {
    if (mode.active) return;
    setConfirmingId(mode.id);
  };

  const handleConfirm = async (mode: BootMode) => {
    try {
      await setBoot.mutateAsync({ serviceName, bootId: mode.id });
      toast.success("启动模式已切换");
      try {
        await reboot.mutateAsync(serviceName);
        toast.success("服务器已重启，启动模式生效");
      } catch {
        toast.warning("启动模式已切换，但重启失败，请手动重启");
      }
      onOpenChange(false);
      setConfirmingId(null);
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "切换启动模式失败");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>启动模式</DialogTitle>
          <DialogDescription>选择服务器的启动模式。切换后将自动重启服务器以生效。</DialogDescription>
        </DialogHeader>

        {q.isPending ? (
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-28 rounded-2xl" />
            ))}
          </div>
        ) : (q.data || []).length === 0 ? (
          <EmptyState icon={HardDrive} title="暂无可选启动模式" />
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 max-h-[70vh] sm:max-h-[60vh] overflow-y-auto pr-1">
            {(q.data || []).map((mode) => {
              const Icon = pickIcon(mode.bootType);
              const isConfirming = confirmingId === mode.id;
              return (
                <button
                  key={mode.id}
                  disabled={mode.active || setBoot.isPending || reboot.isPending}
                  onClick={() => handlePick(mode)}
                  className={`p-4 border rounded-2xl text-left transition-colors flex flex-col items-center text-center gap-2 ${
                    mode.active
                      ? "border-primary bg-primary/5 cursor-default"
                      : "border-border hover:border-foreground hover:bg-secondary/50"
                  } ${isConfirming ? "ring-2 ring-ring" : ""}`}
                >
                  <Icon className={`w-7 h-7 ${mode.active ? "text-foreground" : "text-muted-foreground"}`} />
                  <div className="w-full">
                    <div className="flex items-center justify-center gap-1.5 mb-1">
                      <h4 className="text-[13px] font-semibold">{mode.bootType}</h4>
                      {mode.active && <Check className="w-3.5 h-3.5" />}
                    </div>
                    <p className="text-[11px] text-muted-foreground line-clamp-2">{mode.description}</p>
                  </div>
                  {isConfirming && !mode.active && (
                    <div className="flex gap-2 w-full mt-1">
                      <Button
                        size="sm"
                        className="flex-1"
                        disabled={setBoot.isPending || reboot.isPending}
                        onClick={(e) => {
                          e.stopPropagation();
                          handleConfirm(mode);
                        }}
                      >
                        {setBoot.isPending || reboot.isPending ? "切换中…" : "确认切换并重启"}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={(e) => {
                          e.stopPropagation();
                          setConfirmingId(null);
                        }}
                      >
                        取消
                      </Button>
                    </div>
                  )}
                </button>
              );
            })}
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 按 bootType 关键字挑选图标（对齐旧前端的图标映射） */
function pickIcon(bootType: string) {
  const type = bootType.toLowerCase();
  if (type.includes("hard") || type.includes("disk")) return HardDrive;
  if (type.includes("rescue") || type.includes("ipxe")) return Shield;
  if (type.includes("power") || type.includes("off")) return Power;
  if (type.includes("network")) return Network;
  return Database;
}
