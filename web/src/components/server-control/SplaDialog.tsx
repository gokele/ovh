import { useState } from "react";
import { Zap, AlertCircle, ShieldCheck, Loader2 } from "lucide-react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api } from "@/lib/api";
import { useQueryClient } from "@tanstack/react-query";
import { qk } from "@/lib/query";
import { useSplaList, hasActiveSpla } from "@/hooks/use-server-control";
import { toast } from "sonner";
import { Trans, useTranslation } from "react-i18next";
import { errorMessage } from "@/components/common/LoadFailed";

/**
 * 登记 SPLA 许可证 + 一键解锁 Windows 安装。
 *
 * OVH 把 Windows 模板锁在「这台机器名下要有一条 type=os 的 SPLA 记录」后面。
 * 没有这条记录时，重装的模板列表里根本不会出现 Windows。
 * `POST /dedicated/server/{sn}/spla` 就是登记那条记录，
 * `serialNumber` 是必填的「License serial number」。
 *
 * 上面那个一键按钮提交的是 WINDOWS_GVLK —— 微软**公开发布**的 Windows 10 Pro
 * KMS 客户端安装密钥(GVLK)，见 learn.microsoft.com 的 KMS client activation keys。
 * 它是公开值，不是谁的授权号：作用只是让 OVH 那道检查通过，
 * 并不代表你真的持有 Windows Server 授权(真激活还需要能连上 KMS 服务器)。
 * 按钮上的说明把这一点写明了，选择权交给用户。
 *
 * 下面的手填表单保留给「我有自己的 SPLA 授权号」的人，
 * `type` 的三个取值来自 schema 的 SplaTypeEnum —— SQL Server 那两种也在里面。
 */

/**
 * 微软公开的 Windows 10 Pro KMS 客户端安装密钥(GVLK)。
 * https://learn.microsoft.com/en-us/windows-server/get-started/kms-client-activation-keys
 * 这是一个公开常量，不是任何人的私有授权号。
 */
const WINDOWS_GVLK = "W269N-WFGWX-YVC9B-4J6C9-T83GX";

/** schema 的 SplaTypeEnum 全集,手填表单用(文案 key,渲染处 t()) */
const SPLA_TYPES = [
  { value: "os", labelKey: "maint.spla.type.os" },
  { value: "sqlstd", labelKey: "maint.spla.type.sqlstd" },
  { value: "sqlweb", labelKey: "maint.spla.type.sqlweb" },
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
  const { t } = useTranslation();
  const [type, setType] = useState("os");
  const [serial, setSerial] = useState("");
  const [busy, setBusy] = useState(false);
  const [unlocking, setUnlocking] = useState(false);
  const qc = useQueryClient();
  const spla = useSplaList(serviceName, open);
  // 只看 os:SQL Server 那两类是另一回事(要真买了 SQL 授权才谈得上登记),
  // 一键按钮不碰它们,交给下面的手填表单。
  const unlocked = hasActiveSpla(spla.data, "os");
  // 详情部分拉失败时不能断言"还没解锁" —— 宁可让按钮可点(重复提交 OVH 会自己拒),
  // 也不要因为一次限流就把已解锁的机器显示成未解锁
  const unknown = spla.isPending || spla.isError || spla.data?.partial === true;

  /** 只登记操作系统(os)这一类 —— 它是 Windows 模板出现与否的那道闸 */
  const unlockWindows = async () => {
    setUnlocking(true);
    try {
      await api.post(`/server-control/${serviceName}/spla`, {
        type: "os",
        serialNumber: WINDOWS_GVLK,
      });
      toast.success(t("maint.spla.toast.unlocked"), { duration: 7000 });
      qc.invalidateQueries({ queryKey: qk.serverControl.spla(serviceName) });
    } catch (e: any) {
      toast.error(errorMessage(e), { duration: 8000 });
    } finally {
      setUnlocking(false);
    }
  };

  /** 手填表单:登记你自己买的 SPLA 授权(三种类型都能选) */
  const submit = async () => {
    const sn = serial.trim();
    if (!sn) {
      toast.error(t("maint.spla.toast.needSerial"));
      return;
    }
    setBusy(true);
    try {
      await api.post(`/server-control/${serviceName}/spla`, { type, serialNumber: sn });
      toast.success(t("maint.spla.toast.submitted"));
      setSerial("");
      qc.invalidateQueries({ queryKey: qk.serverControl.spla(serviceName) });
      onOpenChange(false);
    } catch (e: any) {
      toast.error(errorMessage(e), { duration: 8000 });
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
            {t("maint.spla.title")}
          </DialogTitle>
          <DialogDescription>{serviceName}</DialogDescription>
        </DialogHeader>

        <div className="space-y-3 py-1">
          {/* 一键解锁。已经有 type=os 的有效记录就置灰 —— 再点一次没有任何意义,
              只会多一条重复记录或换来 OVH 的报错。 */}
          <div className="rounded-xl border border-border bg-muted/40 px-3.5 py-3 space-y-2">
            <div className="flex items-center justify-between gap-2">
              <p className="text-[12px] font-semibold">{t("maint.spla.unlockTitle")}</p>
              {unlocked && !unknown && (
                <span className="inline-flex items-center gap-1 text-[11px] text-success">
                  <ShieldCheck className="w-3.5 h-3.5" />
                  {t("maint.spla.unlocked")}
                </span>
              )}
            </div>
            <p className="text-[11px] text-muted-foreground leading-relaxed">
              <Trans i18nKey="maint.spla.unlockDesc" components={{ b: <b /> }} />
            </p>
            <p className="text-[11px] text-muted-foreground">{t("maint.spla.sqlNote")}</p>
            <Button
              className="w-full"
              variant={unlocked && !unknown ? "outline" : "default"}
              disabled={unlocking || busy || (unlocked && !unknown)}
              onClick={unlockWindows}
            >
              {unlocking && <Loader2 className="w-4 h-4 animate-spin mr-1.5" />}
              {spla.isPending
                ? t("maint.spla.checking")
                : unlocked && !unknown
                  ? t("maint.spla.alreadyUnlocked")
                  : t("maint.spla.unlockBtn")}
            </Button>
            {spla.isError && (
              <p className="text-[11px] text-warning">{t("maint.spla.unknownWarn")}</p>
            )}
          </div>

          <div className="border-t border-border pt-3">
            <label className="text-[12px] font-semibold block mb-1.5">{t("maint.spla.typeLabel")}</label>
            <Select value={type} onValueChange={setType}>
              <SelectTrigger className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SPLA_TYPES.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {t(opt.labelKey)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div>
            <label className="text-[12px] font-semibold block mb-1.5">{t("maint.spla.serialLabel")}</label>
            <Input
              value={serial}
              onChange={(e) => setSerial(e.target.value)}
              placeholder={t("maint.spla.serialPlaceholder")}
              autoFocus
            />
          </div>

          <div className="border border-warning/40 bg-warning/10 rounded-xl p-2.5 flex gap-2">
            <AlertCircle className="w-3.5 h-3.5 text-warning flex-shrink-0 mt-0.5" />
            <p className="text-[11px] text-muted-foreground">
              <Trans i18nKey="maint.spla.manualNote" components={{ b: <b /> }} />
            </p>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={submit} disabled={busy}>
            {busy ? t("maint.spla.submitting") : t("maint.spla.submit")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
