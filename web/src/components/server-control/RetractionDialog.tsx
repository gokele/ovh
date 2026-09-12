import { useEffect, useState } from "react";
import { AlertTriangle, Undo2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useRequestRetraction, type RetractionInfo } from "@/hooks/use-server-control";

/**
 * 14 天无理由撤单。
 *
 * 这是**退掉整张订单**，不是退服务器：OVH 会退款，服务器随之注销。
 * 所以走和重装同一套把关 —— 二次确认 + 必须先选理由（OVH 那边也是必填）。
 */
export function RetractionDialog({
  serviceName,
  displayName,
  info,
  open,
  onOpenChange,
}: {
  serviceName: string;
  displayName: string;
  info: RetractionInfo;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const request = useRequestRetraction(serviceName);
  const [reason, setReason] = useState("");
  const [comment, setComment] = useState("");
  const [confirming, setConfirming] = useState(false);

  // 每次打开都重置。不重置的话用户上次选到一半关掉，
  // 下次打开会看到一个已经选好理由、只差一步就提交的对话框。
  useEffect(() => {
    if (!open) return;
    setReason("");
    setComment("");
    setConfirming(false);
  }, [open]);

  const deadline = info.retractionDate
    ? new Date(info.retractionDate).toLocaleString("zh-CN")
    : "";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Undo2 className="w-4 h-4" />
            申请无理由撤单
          </DialogTitle>
          <DialogDescription className="mt-0.5">
            <span className="font-mono">{displayName}</span>
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {/* 后果写在最前面。这一步不是"退服务器"，是退整张订单 ——
              用户很容易以为只是取消续费之类的可逆操作 */}
          <div className="flex items-start gap-2.5 rounded-xl border border-destructive/40 bg-destructive/5 px-3.5 py-3">
            <AlertTriangle className="w-4 h-4 text-destructive mt-0.5 flex-shrink-0" />
            <div className="text-[12px] leading-relaxed">
              提交后 OVH 会退掉这张订单并<b>注销这台服务器</b>，上面的数据一并消失。
              这个操作不可撤销。
              {deadline && (
                <div className="mt-1 text-muted-foreground">
                  撤回期截止：{deadline}
                  {info.orderDate && (
                    // 起算点要写出来:用户会拿剩余天数去对「开通日 + 14 天」,
                    // 而撤回期是从下单起算的,机器常常下单后几天才交付
                    <span className="block mt-0.5">
                      从下单（{new Date(info.orderDate).toLocaleDateString("zh-CN")}）起算，
                      不是从服务器开通日起算
                    </span>
                  )}
                </div>
              )}
            </div>
          </div>

          <div>
            <label className="block text-[13px] font-medium mb-1.5">
              撤单理由 <span className="text-destructive">*</span>
            </label>
            <Select value={reason} onValueChange={(v) => { setReason(v); setConfirming(false); }}>
              <SelectTrigger>
                <SelectValue placeholder="选一个理由（OVH 必填）" />
              </SelectTrigger>
              <SelectContent>
                {(info.reasons || []).map((r) => (
                  <SelectItem key={r.value} value={r.value}>
                    {r.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div>
            <label className="block text-[13px] font-medium mb-1.5">补充说明（可选）</label>
            <textarea
              rows={3}
              value={comment}
              onChange={(e) => setComment(e.target.value)}
              placeholder="想跟 OVH 多说两句就写在这里"
              className="w-full px-3 py-2 border border-border rounded-xl text-base sm:text-[13px] bg-background focus:outline-none focus:ring-1 focus:ring-ring resize-none"
            />
          </div>

          {confirming && (
            <p className="text-[12px] text-destructive">
              再点一次「确认撤单」提交。提交后服务器会被注销。
            </p>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={request.isPending}>
            取消
          </Button>
          <Button
            variant="destructive"
            disabled={!reason || request.isPending}
            onClick={() => {
              if (!confirming) {
                setConfirming(true);
                return;
              }
              request.mutate({ reason, comment }, { onSuccess: () => onOpenChange(false) });
            }}
          >
            {request.isPending ? "提交中…" : confirming ? "确认撤单（不可逆）" : "申请撤单"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
