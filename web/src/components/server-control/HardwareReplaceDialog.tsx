import { useState } from "react";
import { Cpu, HardDrive, Activity, RotateCcw, AlertTriangle } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useCreateIntervention, type FaultyDisk } from "@/hooks/use-server-control";
import { toast } from "sonner";
import { useTranslation, Trans } from "react-i18next";
import { errorMessage } from "@/components/common/LoadFailed";

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
  const { t } = useTranslation();
  const mut = useCreateIntervention();
  const [type, setType] = useState<HardwareType>("");
  const [details, setDetails] = useState("");
  const [comment, setComment] = useState("");
  // 故障盘序列号（每行一个，可写成 "序列号" 或 "序列号 槽位号"）。
  // OVH 按 disk_serial 定位要换的盘，拿不到就只能整机换盘，所以这里必填。
  const [diskInput, setDiskInput] = useState("");
  // 故障盘坏到读不出序列号时,改列【健康盘】让 OVH 换其余的(接口的 inverse 语义,
  // OVH 硬盘更换指南明确写了这条路)。默认关:正常模式列故障盘。
  const [inverse, setInverse] = useState(false);
  // 故障内存槽位（可选，逗号或换行分隔，如 DIMM_A1）
  const [slotInput, setSlotInput] = useState("");

  const reset = () => {
    setType("");
    setDetails("");
    setComment("");
    setDiskInput("");
    setInverse(false);
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
      toast.error(t("maint.hwReplace.toast.needType"));
      return;
    }
    if ((type === "memory" || type === "cooling") && !details.trim()) {
      toast.error(t("maint.hwReplace.toast.needDetails"));
      return;
    }
    const disks = type === "hardDiskDrive" ? parseDisks(diskInput) : [];
    if (type === "hardDiskDrive" && disks.length === 0) {
      toast.error(
        inverse ? t("maint.hwReplace.toast.needInverseDisks") : t("maint.hwReplace.toast.needDisks")
      );
      return;
    }
    try {
      const res = await mut.mutateAsync({
        serviceName,
        type,
        details: details || undefined,
        comment: comment || undefined,
        disks: disks.length ? disks : undefined,
        inverse: type === "hardDiskDrive" ? inverse : undefined,
        slots: type === "memory" ? parseSlots(slotInput) : undefined,
      });
      // 工单号是后续跟进的唯一凭据,必须让用户看到并留得住(时长拉长)
      const tn = res?.ticketNumber && res.ticketNumber !== "0" ? res.ticketNumber : "";
      toast.success(
        tn
          ? t("maint.hwReplace.toast.submittedWithTicket", { ticket: tn })
          : res?.message || t("maint.hwReplace.toast.submitted"),
        { duration: 12000 }
      );
      if (res?.notice) toast.info(res.notice, { duration: 12000 });
      onOpenChange(false);
      reset();
    } catch (e: any) {
      toast.error(errorMessage(e));
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
            {t("maint.hwReplace.title")}
          </DialogTitle>
          <DialogDescription>{t("maint.hwReplace.desc")}</DialogDescription>
        </DialogHeader>

        {!type ? (
          <div className="space-y-3">
            <p className="text-[13px] font-medium">{t("maint.hwReplace.pickTitle")}</p>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <TypeCard
                icon={HardDrive}
                title={t("maint.hwReplace.hw.diskTitle")}
                description={t("maint.hwReplace.hw.diskDesc")}
                onClick={() => setType("hardDiskDrive")}
              />
              <TypeCard
                icon={Cpu}
                title={t("maint.hwReplace.hw.memoryTitle")}
                description={t("maint.hwReplace.hw.memoryDesc")}
                onClick={() => setType("memory")}
              />
              <TypeCard
                icon={Activity}
                title={t("maint.hwReplace.hw.coolingTitle")}
                description={t("maint.hwReplace.hw.coolingDesc")}
                onClick={() => setType("cooling")}
              />
            </div>
          </div>
        ) : (
          <div className="space-y-3">
            <div>
              <label className="text-[12px] font-semibold block mb-1.5">{t("maint.hwReplace.typeLabel")}</label>
              <div className="flex gap-2">
                <div className="flex-1 px-3 py-2 border border-border rounded-md text-[13px] bg-secondary/30">
                  {type === "hardDiskDrive" && t("maint.hwReplace.typeValue.hardDiskDrive")}
                  {type === "memory" && t("maint.hwReplace.typeValue.memory")}
                  {type === "cooling" && t("maint.hwReplace.typeValue.cooling")}
                </div>
                <Button
                  variant="outline"
                  size="icon"
                  onClick={() => setType("")}
                  title={t("maint.hwReplace.reselectTitle")}
                >
                  <RotateCcw className="w-4 h-4" />
                </Button>
              </div>
            </div>

            <div>
              <label className="text-[12px] font-semibold block mb-1.5">
                {t("maint.hwReplace.commentLabel")}
              </label>
              <textarea
                rows={3}
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                placeholder={t("maint.hwReplace.commentPlaceholder")}
                className="w-full px-3 py-2 border border-border rounded-md text-base sm:text-[13px] bg-background focus:outline-none focus:ring-1 focus:ring-ring resize-none"
              />
            </div>

            {(type === "memory" || type === "cooling") && (
              <div>
                <label className="text-[12px] font-semibold block mb-1.5">
                  {t("maint.hwReplace.detailsLabel", {
                    what:
                      type === "memory"
                        ? t("maint.hwReplace.detailReqMemory")
                        : t("maint.hwReplace.detailReqCooling"),
                  })}
                </label>
                <Input
                  value={details}
                  onChange={(e) => setDetails(e.target.value)}
                  placeholder={
                    type === "memory"
                      ? t("maint.hwReplace.detailsPlaceholderMemory")
                      : t("maint.hwReplace.detailsPlaceholderFan")
                  }
                />
              </div>
            )}

            {type === "hardDiskDrive" && (
              <div className="space-y-2">
                <label className="text-[12px] font-semibold block">
                  {inverse ? t("maint.hwReplace.diskLabelInverse") : t("maint.hwReplace.diskLabelNormal")}
                </label>
                <textarea
                  rows={3}
                  value={diskInput}
                  onChange={(e) => setDiskInput(e.target.value)}
                  placeholder={t("maint.hwReplace.diskPlaceholder")}
                  className="w-full px-3 py-2 border border-border rounded-md text-base sm:text-[13px] font-mono bg-background focus:outline-none focus:ring-1 focus:ring-ring resize-none"
                />
                <label className="flex items-start gap-2 cursor-pointer text-[12px]">
                  <input
                    type="checkbox"
                    className="mt-0.5"
                    checked={inverse}
                    onChange={(e) => setInverse(e.target.checked)}
                  />
                  <span>
                    <Trans i18nKey="maint.hwReplace.inverseLabel" components={{ b: <b /> }} />
                    <span className="block text-muted-foreground">{t("maint.hwReplace.inverseNote")}</span>
                  </span>
                </label>
                <div className="border border-info/40 bg-info/5 rounded-2xl p-3 text-[12px] leading-relaxed">
                  <Trans
                    i18nKey="maint.hwReplace.diskGuide"
                    components={{ code: <code className="font-mono" />, b: <b /> }}
                  />
                </div>
              </div>
            )}

            {type === "memory" && (
              <div>
                <label className="text-[12px] font-semibold block mb-1.5">
                  {t("maint.hwReplace.slotLabel")}
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
                <li>{t("maint.hwReplace.flow.ticket")}</li>
                <li>{t("maint.hwReplace.flow.schedule")}</li>
                <li>{t("maint.hwReplace.flow.offline")}</li>
                <li>{t("maint.hwReplace.flow.mail")}</li>
              </ul>
            </div>
          </div>
        )}

        <DialogFooter>
          {type && (
            <Button variant="outline" onClick={() => setType("")}>
              {t("maint.hwReplace.backBtn")}
            </Button>
          )}
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          {type && (
            <Button onClick={handleSubmit} disabled={mut.isPending}>
              {mut.isPending ? t("maint.hwReplace.submitting") : t("maint.hwReplace.submitBtn")}
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
