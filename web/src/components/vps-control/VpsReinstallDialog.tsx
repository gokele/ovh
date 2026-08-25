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
import { useVpsTemplates, useReinstallVps, useVpsCurrentOS, type VpsTemplate } from "@/hooks/use-vps-control";
import { toast } from "sonner";

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

  const [templateId, setTemplateId] = useState<number | string | null>(null);
  const [language, setLanguage] = useState<string>("en");
  const [doNotSendPassword, setDoNotSendPassword] = useState(false);
  const [sshKeyNames, setSshKeyNames] = useState<string>(""); // 逗号分隔
  const [confirmName, setConfirmName] = useState("");

  const tplItems: VpsTemplate[] = templates.data?.items || [];
  const selected: VpsTemplate | null = useMemo(
    () => tplItems.find((t) => String(t.id) === String(templateId)) || null,
    [tplItems, templateId],
  );

  // 切模板时同步语言到该模板默认语言。templateId 可能是 number(EU) 或 string(US imageId),
  // Select 的 value 只能是 string,这里按需 cast
  const handleTemplateChange = (v: string) => {
    // 尝试转 number,纯数字则按 EU long 处理,否则当 US imageId 字符串
    const asNum = Number(v);
    const id: number | string = !Number.isNaN(asNum) && String(asNum) === v ? asNum : v;
    setTemplateId(id);
    const tpl = tplItems.find((t) => String(t.id) === v);
    if (tpl) {
      setLanguage(tpl.locale || tpl.availableLanguage?.[0] || "en");
    }
  };

  const handleSubmit = async () => {
    if (!templateId) {
      toast.error("请选择系统模板");
      return;
    }
    if (confirmName !== serviceName) {
      toast.error("VPS 名称不匹配,无法确认");
      return;
    }
    const sshKey = sshKeyNames
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    try {
      await reinstall.mutateAsync({
        templateId,
        language,
        sshKey: sshKey.length > 0 ? sshKey : undefined,
        doNotSendPassword,
      });
      toast.success("重装任务已提交,通常 5-10 分钟完成");
      onOpenChange(false);
      setTemplateId(null);
      setConfirmName("");
      setSshKeyNames("");
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "重装失败");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <HardDrive className="w-5 h-5" />
            重装系统
          </DialogTitle>
          <DialogDescription>{serviceName}</DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto -mx-6 px-6 space-y-4">
          {/* 当前系统 */}
          {currentOS.data && (
            <div className="border border-border rounded-xl p-3 bg-secondary/30 flex items-baseline gap-2 flex-wrap">
              <span className="text-[11px] text-muted-foreground">当前系统</span>
              <span className="text-[13px] font-semibold">{currentOS.data.name}</span>
              {currentOS.data.distribution && (
                <span className="text-[11px] text-muted-foreground">({currentOS.data.distribution})</span>
              )}
            </div>
          )}

          <div className="border border-destructive/40 bg-destructive/5 rounded-xl p-3 flex gap-2.5">
            <AlertCircle className="w-4 h-4 text-destructive flex-shrink-0 mt-0.5" />
            <div className="text-[12px]">
              <p className="font-semibold text-destructive mb-0.5">数据将被完全清除</p>
              <p className="text-muted-foreground">VPS 当前磁盘数据无法恢复(除非有快照)。建议先创建快照。</p>
            </div>
          </div>

          {/* 模板选择 */}
          <div>
            <label className="text-[12px] font-semibold block mb-1.5">系统模板</label>
            {templates.isPending ? (
              <div className="flex items-center gap-2 text-[12px] text-muted-foreground">
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
                加载模板中…
              </div>
            ) : templates.isError ? (
              // 后端现在区分「详情全挂(500)」和「账户真没模板(200 空列表)」。
              // 全挂时再显示"暂无可用模板"会让用户以为账户没模板从而放弃重试。
              <div className="flex items-center gap-2 text-[12px] text-destructive">
                <AlertCircle className="w-3.5 h-3.5" />
                模板列表读取失败：
                {(templates.error as any)?.response?.data?.error || (templates.error as any)?.message || "请稍后重试"}
                <Button size="sm" variant="outline" className="h-7" onClick={() => templates.refetch()}>
                  重试
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
                      ? "暂无可用模板(账户/区域可能无系统模板)"
                      : "选择 OS 模板"
                  } />
                </SelectTrigger>
                <SelectContent className="max-h-72">
                  {tplItems.map((t) => (
                    <SelectItem key={String(t.id)} value={String(t.id)}>
                      {t.distribution ? `${t.distribution} — ` : ""}{t.name} ({t.bitFormat}-bit)
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
            <PartialNotice
              failedCount={templates.data?.failedCount || 0}
              what="系统模板"
              className="mt-1.5"
            />
            {selected && (
              <p className="text-[11px] text-muted-foreground mt-1.5">
                模板 ID: {selected.id} · 默认语言: {selected.locale} · 支持{" "}
                {(selected.availableLanguage || []).length} 种语言
              </p>
            )}
          </div>

          {/* 语言 */}
          {selected && selected.availableLanguage && selected.availableLanguage.length > 0 && (
            <div>
              <label className="text-[12px] font-semibold block mb-1.5">语言</label>
              <Select value={language} onValueChange={setLanguage}>
                <SelectTrigger className="h-9">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {selected.availableLanguage.map((l) => (
                    <SelectItem key={l} value={l}>
                      {l}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {/* SSH key */}
          <div>
            <label className="text-[12px] font-semibold block mb-1.5">SSH 公钥(可选)</label>
            <Input
              value={sshKeyNames}
              onChange={(e) => setSshKeyNames(e.target.value)}
              placeholder="OVH SSH key 名称,多个用逗号分隔(从 /me/sshKey 拿)"
            />
            <p className="text-[11px] text-muted-foreground mt-1">
              填了 SSH key 可勾选下面「不发送密码邮件」,装机后直接用 key 登录
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
              不发送初始密码邮件(用 SSH key 登录)
            </label>
          </div>

          {/* 二次确认 */}
          <div className="border-t pt-3">
            <label className="text-[12px] font-semibold block mb-1.5">
              输入 VPS 名 <code className="font-mono">{serviceName}</code> 确认:
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
            取消
          </Button>
          <Button
            variant="destructive"
            onClick={handleSubmit}
            disabled={reinstall.isPending || !templateId || confirmName !== serviceName}
          >
            {reinstall.isPending ? "提交中…" : "确认重装"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
