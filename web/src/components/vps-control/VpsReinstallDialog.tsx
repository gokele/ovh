import { useState, useMemo } from "react";
import { HardDrive, Loader2, AlertCircle } from "lucide-react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { PartialNotice } from "@/components/common/PartialNotice";
import { errorMessage } from "@/components/common/LoadFailed";
import { useVpsTemplates, useReinstallVps, useVpsCurrentOS, type VpsTemplate } from "@/hooks/use-vps-control";
import { toast } from "sonner";
import { splitList } from "@/lib/split-list";
import { Trans, useTranslation } from "react-i18next";

/** VPS 重装系统:模板列表 + 语言 + SSH key 选项 + 二次确认 */
export function VpsReinstallDialog({
  serviceName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const templates = useVpsTemplates(open ? serviceName : null);
  const currentOS = useVpsCurrentOS(open ? serviceName : null);
  const reinstall = useReinstallVps(serviceName);
  const { t } = useTranslation();

  const [templateId, setTemplateId] = useState<number | string | null>(null);
  const [doNotSendPassword, setDoNotSendPassword] = useState(false);
  const [sshKeyNames, setSshKeyNames] = useState<string>(""); // 逗号分隔
  const [confirmName, setConfirmName] = useState("");

  const tplItems: VpsTemplate[] = templates.data?.items || [];
  const selected: VpsTemplate | null = useMemo(
    () => tplItems.find((tpl) => String(tpl.id) === String(templateId)) || null,
    [tplItems, templateId],
  );

  // 切模板时同步语言到该模板默认语言。templateId 可能是 number(EU) 或 string(US imageId),
  // Select 的 value 只能是 string,这里按需 cast
  const handleTemplateChange = (v: string) => {
    // 尝试转 number,纯数字是旧模板缓存,否则当 imageId 字符串 —— 后端统一转 imageId
    const asNum = Number(v);
    const id: number | string = !Number.isNaN(asNum) && String(asNum) === v ? asNum : v;
    setTemplateId(id);
  };

  const handleSubmit = async () => {
    if (!templateId) {
      toast.error(t("vps.reinstall.toast.selectTemplate"));
      return;
    }
    if (confirmName !== serviceName) {
      toast.error(t("vps.reinstall.toast.nameMismatch"));
      return;
    }
    const sshKey = splitList(sshKeyNames);
    try {
      await reinstall.mutateAsync({
        templateId,
        sshKey: sshKey.length > 0 ? sshKey : undefined,
        doNotSendPassword,
      });
      toast.success(t("vps.reinstall.toast.submitted"));
      onOpenChange(false);
      setTemplateId(null);
      setConfirmName("");
      setSshKeyNames("");
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <HardDrive className="w-5 h-5" />
            {t("vps.reinstall.title")}
          </DialogTitle>
          <DialogDescription>{serviceName}</DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto -mx-6 px-6 space-y-4">
          {/* 当前系统 */}
          {currentOS.data && (
            <div className="border border-border rounded-xl p-3 bg-secondary/30 flex items-baseline gap-2 flex-wrap">
              <span className="text-[11px] text-muted-foreground">{t("vps.reinstall.currentOs")}</span>
              <span className="text-[13px] font-semibold">{currentOS.data.name}</span>
              {currentOS.data.distribution && (
                <span className="text-[11px] text-muted-foreground">({currentOS.data.distribution})</span>
              )}
            </div>
          )}

          <div className="border border-destructive/40 bg-destructive/5 rounded-xl p-3 flex gap-2.5">
            <AlertCircle className="w-4 h-4 text-destructive flex-shrink-0 mt-0.5" />
            <div className="text-[12px]">
              <p className="font-semibold text-destructive mb-0.5">{t("vps.reinstall.wipeTitle")}</p>
              <p className="text-muted-foreground">{t("vps.reinstall.wipeDesc")}</p>
            </div>
          </div>

          {/* 模板选择 */}
          <div>
            <label className="text-[12px] font-semibold block mb-1.5">{t("vps.reinstall.tplLabel")}</label>
            {templates.isPending ? (
              <div className="flex items-center gap-2 text-[12px] text-muted-foreground">
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
                {t("vps.reinstall.loadingTemplates")}
              </div>
            ) : templates.isError ? (
              // 后端现在区分「详情全挂(500)」和「账户真没模板(200 空列表)」。
              // 全挂时再显示"暂无可用模板"会让用户以为账户没模板从而放弃重试。
              <div className="flex items-center gap-2 text-[12px] text-destructive">
                <AlertCircle className="w-3.5 h-3.5" />
                {t("vps.reinstall.tplLoadFailed")}
                {errorMessage(templates.error)}
                <Button size="sm" variant="outline" className="h-7" onClick={() => templates.refetch()}>
                  {t("common.retry")}
                </Button>
              </div>
            ) : (
              <Select
                value={templateId != null ? String(templateId) : ""}
                onValueChange={handleTemplateChange}
              >
                <SelectTrigger className="h-9">
                  <SelectValue placeholder={
                    tplItems.length === 0
                      ? t("vps.reinstall.tplEmpty")
                      : t("vps.reinstall.tplPlaceholder")
                  } />
                </SelectTrigger>
                <SelectContent className="max-h-72">
                  {tplItems.map((tpl) => (
                    <SelectItem key={String(tpl.id)} value={String(tpl.id)}>
                      {tpl.distribution ? `${tpl.distribution} — ` : ""}{tpl.name} ({tpl.bitFormat}-bit)
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
            <PartialNotice
              failedCount={templates.data?.failedCount || 0}
              what={t("vps.reinstall.partialWhat")}
              className="mt-1.5"
            />
            {selected && (selected.locale || (selected.availableLanguage || []).length > 0) && (
              <p className="text-[11px] text-muted-foreground mt-1.5">
                {t("vps.reinstall.tplId", { id: selected.id })}
                {selected.locale ? ` · ${t("vps.reinstall.tplLocale", { locale: selected.locale })}` : ""}
                {(selected.availableLanguage || []).length > 0
                  ? ` · ${t("vps.reinstall.tplLangs", { n: selected.availableLanguage.length })}`
                  // images 制镜像没有 language 概念,rebuild 也不收 language 字段 ——
                  // 全量切 rebuild 后这里恒为空,不显示误导性的"支持 0 种语言"
                  : ""}
              </p>
            )}
          </div>

          {/* 语言下拉已删:全量走 rebuild 后 OVH 不收 language 字段,
              images 制镜像也没有语言清单 —— 留着就是"选了不生效"的假控件 */}


          {/* SSH key */}
          <div>
            <label className="text-[12px] font-semibold block mb-1.5">{t("vps.reinstall.sshLabel")}</label>
            <Input
              value={sshKeyNames}
              onChange={(e) => setSshKeyNames(e.target.value)}
              placeholder={t("vps.reinstall.sshPlaceholder")}
            />
            <p className="text-[11px] text-muted-foreground mt-1">
              {t("vps.reinstall.sshHint")}
            </p>
          </div>

          {/* 不发送密码 */}
          <div className="flex items-center gap-2">
            <Checkbox
              id="vps-noPwd"
              checked={doNotSendPassword}
              onCheckedChange={(v) => setDoNotSendPassword(v === true)}
            />
            <label htmlFor="vps-noPwd" className="text-[12px] cursor-pointer">
              {t("vps.reinstall.noPwdMail")}
            </label>
          </div>

          {/* 二次确认 */}
          <div className="border-t pt-3">
            <label className="text-[12px] font-semibold block mb-1.5">
              <Trans
                i18nKey="vps.reinstall.confirmNameLabel"
                values={{ name: serviceName }}
                components={{ code: <code className="font-mono" /> }}
              />
            </label>
            <Input
              value={confirmName}
              onChange={(e) => setConfirmName(e.target.value)}
              placeholder={serviceName}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button
            variant="destructive"
            onClick={handleSubmit}
            disabled={reinstall.isPending || !templateId || confirmName !== serviceName}
          >
            {reinstall.isPending ? t("vps.reinstall.submitting") : t("vps.reinstall.submitConfirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
