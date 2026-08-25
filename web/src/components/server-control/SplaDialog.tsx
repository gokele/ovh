import { useState } from "react";
import { Zap, AlertCircle } from "lucide-react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api } from "@/lib/api";
import { toast } from "sonner";

/**
 * 登记 SPLA 许可证。
 *
 * 为什么必须让用户自己填序列号：`POST /dedicated/server/{sn}/spla` 的
 * `serialNumber` 是必填的「License serial number」，指的是**你自己买的**
 * SPLA 授权号。这里以前写死了一个常量，那个值是微软公开发布的
 * Windows 10 Pro KMS 客户端安装密钥 —— 不是任何人的授权号，而且所有人、
 * 所有机器点一下都提交同一个，用户全程没有输入的机会。
 *
 * `type` 的三个取值来自 schema 的 SplaTypeEnum，以前也被写死成 os，
 * SQL Server 的两种授权在界面上根本够不到。
 */
const SPLA_TYPES = [
  { value: "os", label: "操作系统 (Windows Server)" },
  { value: "sqlstd", label: "SQL Server 标准版" },
  { value: "sqlweb", label: "SQL Server 网页版" },
];

export function SplaDialog({
  serviceName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const [type, setType] = useState("os");
  const [serial, setSerial] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    const sn = serial.trim();
    if (!sn) {
      toast.error("请填写你的 SPLA 许可证序列号");
      return;
    }
    setBusy(true);
    try {
      await api.post(`/server-control/${serviceName}/spla`, { type, serialNumber: sn });
      toast.success("许可证已提交");
      setSerial("");
      onOpenChange(false);
    } catch (e: any) {
      toast.error(
        e?.response?.data?.error || e?.message || "提交失败",
        { duration: 8000 }
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Zap className="w-5 h-5" />
            登记 SPLA 许可证
          </DialogTitle>
          <DialogDescription>{serviceName}</DialogDescription>
        </DialogHeader>

        <div className="space-y-3 py-1">
          <div>
            <label className="text-[12px] font-semibold block mb-1.5">授权类型</label>
            <Select value={type} onValueChange={setType}>
              <SelectTrigger className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SPLA_TYPES.map((t) => (
                  <SelectItem key={t.value} value={t.value}>
                    {t.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div>
            <label className="text-[12px] font-semibold block mb-1.5">许可证序列号</label>
            <Input
              value={serial}
              onChange={(e) => setSerial(e.target.value)}
              placeholder="XXXXX-XXXXX-XXXXX-XXXXX-XXXXX"
              autoFocus
            />
          </div>

          <div className="border border-amber-500/40 bg-amber-500/10 rounded-xl p-2.5 flex gap-2">
            <AlertCircle className="w-3.5 h-3.5 text-amber-600 dark:text-amber-400 flex-shrink-0 mt-0.5" />
            <p className="text-[11px] text-muted-foreground">
              填你自己购买的 SPLA 授权序列号。这一步是把授权登记到 OVH 名下，
              不是申请或生成授权 —— 填别人的或网上找的密钥会被 OVH 拒绝。
            </p>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy ? "提交中…" : "提交"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
