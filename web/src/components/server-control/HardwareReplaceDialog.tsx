import { useState } from "react";
import { Cpu, HardDrive, Activity, RotateCcw, AlertTriangle } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useCreateIntervention } from "@/hooks/use-server-control";
import { toast } from "sonner";

type HardwareType = "hardDiskDrive" | "memory" | "cooling" | "";

/** 硬件更换工单：硬盘 / 内存（必填详情）/ 散热（必填详情）+ 可选英文备注 */
export function HardwareReplaceDialog({
  serviceName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const mut = useCreateIntervention();
  const [type, setType] = useState<HardwareType>("");
  const [details, setDetails] = useState("");
  const [comment, setComment] = useState("");

  const reset = () => {
    setType("");
    setDetails("");
    setComment("");
  };

  const handleSubmit = async () => {
    if (!type) {
      toast.error("请选择硬件类型");
      return;
    }
    if ((type === "memory" || type === "cooling") && !details.trim()) {
      toast.error("此类型需要填写故障详情");
      return;
    }
    try {
      await mut.mutateAsync({ serviceName, type, details: details || undefined, comment: comment || undefined });
      toast.success("硬件更换工单已提交");
      onOpenChange(false);
      reset();
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "提交失败");
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v);
        if (!v) reset();
      }}
    >
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Cpu className="w-5 h-5" />
            硬件更换申请
          </DialogTitle>
          <DialogDescription>提交工单后 OVH 客服会安排现场更换硬件，期间服务器可能离线。</DialogDescription>
        </DialogHeader>

        {!type ? (
          <div className="space-y-3">
            <p className="text-[13px] font-medium">请选择要更换的硬件类型：</p>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <TypeCard
                icon={HardDrive}
                title="硬盘"
                description="故障或损坏的硬盘"
                onClick={() => setType("hardDiskDrive")}
              />
              <TypeCard
                icon={Cpu}
                title="内存 (RAM)"
                description="故障的内存模块"
                onClick={() => setType("memory")}
              />
              <TypeCard
                icon={Activity}
                title="散热系统"
                description="风扇或散热器"
                onClick={() => setType("cooling")}
              />
            </div>
          </div>
        ) : (
          <div className="space-y-3">
            <div>
              <label className="text-[12px] font-semibold block mb-1.5">组件类型</label>
              <div className="flex gap-2">
                <div className="flex-1 px-3 py-2 border border-border rounded-md text-[13px] bg-secondary/30">
                  {type === "hardDiskDrive" && "硬盘驱动器"}
                  {type === "memory" && "内存 (RAM)"}
                  {type === "cooling" && "散热系统"}
                </div>
                <Button variant="outline" size="icon" onClick={() => setType("")} title="重新选择">
                  <RotateCcw className="w-4 h-4" />
                </Button>
              </div>
            </div>

            <div>
              <label className="text-[12px] font-semibold block mb-1.5">备注说明（可选，建议英文）</label>
              <textarea
                rows={3}
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                placeholder="Describe the issue in English (optional)…"
                className="w-full px-3 py-2 border border-border rounded-md text-[13px] bg-background focus:outline-none focus:ring-1 focus:ring-ring resize-none"
              />
            </div>

            {(type === "memory" || type === "cooling") && (
              <div>
                <label className="text-[12px] font-semibold block mb-1.5">
                  故障详情（{type === "memory" ? "内存必填" : "散热必填"}，建议英文）
                </label>
                <Input
                  value={details}
                  onChange={(e) => setDetails(e.target.value)}
                  placeholder={
                    type === "memory" ? "e.g., Memory module failure, slot 1" : "e.g., Fan noise, overheating issue"
                  }
                />
              </div>
            )}

            <div className="border border-warning/40 bg-warning/5 rounded-2xl p-3 text-[12px] flex items-start gap-2">
              <AlertTriangle className="w-4 h-4 text-warning mt-0.5 flex-shrink-0" />
              <ul className="list-disc list-inside leading-relaxed space-y-0.5">
                <li>系统将创建工单提交给 OVH 客服</li>
                <li>OVH 将安排硬件更换时间</li>
                <li>更换期间服务器可能离线</li>
                <li>进度通过邮件通知</li>
              </ul>
            </div>
          </div>
        )}

        <DialogFooter>
          {type && (
            <Button variant="outline" onClick={() => setType("")}>
              返回
            </Button>
          )}
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          {type && (
            <Button onClick={handleSubmit} disabled={mut.isPending}>
              {mut.isPending ? "提交中…" : "提交申请"}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TypeCard({
  icon: Icon,
  title,
  description,
  onClick,
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  description: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="p-4 border border-border rounded-2xl hover:border-foreground hover:bg-secondary/50 transition-colors text-center flex flex-col items-center gap-2"
    >
      <Icon className="w-7 h-7 text-muted-foreground" />
      <div>
        <h4 className="text-[13px] font-semibold mb-0.5">{title}</h4>
        <p className="text-[11px] text-muted-foreground">{description}</p>
      </div>
    </button>
  );
}
