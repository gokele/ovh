import { useState } from "react";
import { Cpu, HardDrive, Activity, RotateCcw, AlertTriangle } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useCreateIntervention, type FaultyDisk } from "@/hooks/use-server-control";
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
  // 故障盘序列号（每行一个，可写成 "序列号" 或 "序列号 槽位号"）。
  // OVH 按 disk_serial 定位要换的盘，拿不到就只能整机换盘，所以这里必填。
  const [diskInput, setDiskInput] = useState("");
  // 故障内存槽位（可选，逗号或换行分隔，如 DIMM_A1）
  const [slotInput, setSlotInput] = useState("");

  const reset = () => {
    setType("");
    setDetails("");
    setComment("");
    setDiskInput("");
    setSlotInput("");
  };

  /** 把每行 "序列号 [槽位号]" 解析成 OVH 要的 {disk_serial, slot_id?} */
  const parseDisks = (raw: string): FaultyDisk[] =>
    raw
      .split(/[\n,]/)
      .map((line) => line.trim())
      .filter(Boolean)
      .map((line) => {
        const [serial, slot] = line.split(/\s+/);
        const slotId = slot !== undefined ? Number(slot) : NaN;
        return Number.isFinite(slotId)
          ? { disk_serial: serial, slot_id: slotId }
          : { disk_serial: serial };
      });

  const parseSlots = (raw: string): string[] =>
    raw
      .split(/[\n,]/)
      .map((v) => v.trim())
      .filter(Boolean);

  const handleSubmit = async () => {
    if (!type) {
      toast.error("请选择硬件类型");
      return;
    }
    if ((type === "memory" || type === "cooling") && !details.trim()) {
      toast.error("此类型需要填写故障详情");
      return;
    }
    const disks = type === "hardDiskDrive" ? parseDisks(diskInput) : [];
    if (type === "hardDiskDrive" && disks.length === 0) {
      toast.error("请填写至少一块故障盘的序列号");
      return;
    }
    try {
      await mut.mutateAsync({
        serviceName,
        type,
        details: details || undefined,
        comment: comment || undefined,
        disks: disks.length ? disks : undefined,
        slots: type === "memory" ? parseSlots(slotInput) : undefined,
      });
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

            {type === "hardDiskDrive" && (
              <div className="space-y-2">
                <label className="text-[12px] font-semibold block">
                  故障盘序列号（必填，每行一块）
                </label>
                <textarea
                  rows={3}
                  value={diskInput}
                  onChange={(e) => setDiskInput(e.target.value)}
                  placeholder={"S3Z2NB0K123456\nS3Z2NB0K654321 2   ← 序列号后可跟槽位号"}
                  className="w-full px-3 py-2 border border-border rounded-md text-[13px] font-mono bg-background focus:outline-none focus:ring-1 focus:ring-ring resize-none"
                />
                <div className="border border-info/40 bg-info/5 rounded-2xl p-3 text-[12px] leading-relaxed">
                  OVH 按 <code className="font-mono">disk_serial</code> 定位要换的盘。留空会被判成「更换整机所有硬盘」，
                  后端会直接拒绝。序列号可在系统里执行 <code className="font-mono">smartctl -i /dev/sdX</code> 查看，
                  或在 OVH 控制台的硬件信息里找到。
                </div>
              </div>
            )}

            {type === "memory" && (
              <div>
                <label className="text-[12px] font-semibold block mb-1.5">
                  故障内存槽位（可选，逗号或换行分隔，如 DIMM_A1）
                </label>
                <Input
                  value={slotInput}
                  onChange={(e) => setSlotInput(e.target.value)}
                  placeholder="DIMM_A1, DIMM_B2"
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
