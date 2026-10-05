import { useEffect, useRef, useState } from "react";
import { Monitor, Loader2, ExternalLink, Download } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { errorMessage } from "@/components/common/LoadFailed";

/**
 * IPMI / KVM 控制台对话框：
 * - 打开时先查这台机器支持哪几种接入方式(轻量,秒回)
 * - 用户选一种(默认 HTML5),再点"打开控制台"申请会话(要轮询 OVH 任务,约 20s)
 * - kvmipHtml5URL / serialOverLanURL → 显示打开链接按钮
 * - kvmipJnlp(Java KVM) → 下载 .jnlp 文件
 *
 * 为什么要让用户选:HTML5 KVM 在部分机型上键盘映射/鼠标不同步,
 * 老运维就是要 Java KVM。以前后端按固定优先级自动挑、HTML5 排第一,
 * 同时支持两种的机器永远拿不到 JNLP。
 */
export function IpmiDialog({
  serviceName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const [countdown, setCountdown] = useState(20);
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState<{ url?: string; accessType?: string } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const startedRef = useRef(false);
  // 支持的接入方式(打开对话框时查一次)
  const [types, setTypes] = useState<{ supportedTypes: string[]; typeLabels: Record<string, string>; activated: boolean } | null>(null);
  const [chosen, setChosen] = useState<string>("");
  const [typesLoading, setTypesLoading] = useState(false);
  const { t } = useTranslation();

  // 打开对话框:先查支持哪几种(秒回),不自动申请会话
  useEffect(() => {
    if (!open) {
      setCountdown(20);
      setLoading(false);
      setResult(null);
      setError(null);
      setTypes(null);
      setChosen("");
      startedRef.current = false;
      return;
    }
    setTypesLoading(true);
    (async () => {
      try {
        const res = await api.get(`/server-control/${serviceName}/ipmi-types`);
        setTypes({
          supportedTypes: res.data?.supportedTypes || [],
          typeLabels: res.data?.typeLabels || {},
          activated: res.data?.activated !== false,
        });
        setChosen(res.data?.defaultType || "");
      } catch (e: any) {
        setError(errorMessage(e));
      } finally {
        setTypesLoading(false);
      }
    })();
  }, [open, serviceName]);

  // 申请控制台会话(要轮询 OVH 任务,约 20 秒)
  const openConsole = () => {
    if (!chosen) {
      toast.error(t("ctrl.ipmi.toast.pickType"));
      return;
    }
    startedRef.current = true;
    setError(null);
    setResult(null);
    setLoading(true);
    setCountdown(20);
    const interval = setInterval(() => {
      setCountdown((p) => (p <= 1 ? 0 : p - 1));
    }, 1000);

    (async () => {
      try {
        const res = await api.get(`/server-control/${serviceName}/console`, { params: { type: chosen } });
        clearInterval(interval);
        setLoading(false);
        const value = res.data?.console?.value;
        const accessType = res.data?.accessType;
        if (!value) {
          setError(t("ctrl.ipmi.emptyResult"));
          return;
        }
        if (accessType === "kvmipJnlp") {
          // Java KVM:OVH 返回的是 .jnlp 文件内容,存成文件交给 Java Web Start 打开
          const blob = new Blob([value], { type: "application/x-java-jnlp-file" });
          const url = window.URL.createObjectURL(blob);
          const a = document.createElement("a");
          a.href = url;
          a.download = `ipmi-${serviceName}.jnlp`;
          document.body.appendChild(a);
          a.click();
          document.body.removeChild(a);
          window.URL.revokeObjectURL(url);
          toast.success(t("ctrl.ipmi.toast.jnlpDownloaded"));
          setResult({ accessType });
        } else {
          setResult({ url: value, accessType });
        }
      } catch (e: any) {
        clearInterval(interval);
        setLoading(false);
        setError(errorMessage(e));
      }
    })();
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Monitor className="w-5 h-5" />
            {t("ctrl.ipmi.title")}
          </DialogTitle>
          <DialogDescription>
            {t("ctrl.ipmi.desc")}
          </DialogDescription>
        </DialogHeader>

        {/* 接入方式选择：查得快，先让用户挑，别等 20 秒才发现没有 Java KVM */}
        {!loading && !result && (
          <div className="space-y-2">
            {typesLoading ? (
              <p className="text-[13px] text-muted-foreground flex items-center gap-2">
                <Loader2 className="w-4 h-4 animate-spin" />
                {t("ctrl.ipmi.typesLoading")}
              </p>
            ) : types && types.supportedTypes.length > 0 ? (
              <>
                <label className="text-[12px] font-semibold block">{t("ctrl.ipmi.typeLabel")}</label>
                <div className="space-y-1.5">
                  {types.supportedTypes.map((type) => (
                    <label
                      key={type}
                      className={`flex items-start gap-2 px-3 py-2 border rounded-xl cursor-pointer text-[13px] transition-colors ${
                        chosen === type ? "border-primary bg-primary/5" : "border-border hover:bg-secondary/40"
                      }`}
                    >
                      <input
                        type="radio"
                        name="ipmi-type"
                        className="mt-0.5"
                        checked={chosen === type}
                        onChange={() => setChosen(type)}
                      />
                      <span>
                        {types.typeLabels[type] || type}
                        {type === "kvmipJnlp" && (
                          <span className="block text-[11px] text-muted-foreground mt-0.5">
                            {t("ctrl.ipmi.jnlpHint")}
                          </span>
                        )}
                        {type === "serialOverLanSshKey" && (
                          <span className="block text-[11px] text-muted-foreground mt-0.5">
                            {t("ctrl.ipmi.solHint")}
                          </span>
                        )}
                      </span>
                    </label>
                  ))}
                </div>
                {!types.activated && (
                  <p className="text-[11px] text-warning">
                    {t("ctrl.ipmi.notActivated")}
                  </p>
                )}
              </>
            ) : (
              <p className="text-[13px] text-muted-foreground">{t("ctrl.ipmi.noConsole")}</p>
            )}
          </div>
        )}

        <div className="flex flex-col items-center justify-center py-6 gap-3">
          {loading ? (
            <>
              <Loader2 className="w-12 h-12 animate-spin text-muted-foreground" />
              <p className="text-[13px] text-muted-foreground">
                {t("ctrl.ipmi.fetching")}
                {countdown > 0 && ` ${t("ctrl.ipmi.countdown", { n: countdown })}`}
              </p>
            </>
          ) : error ? (
            <p className="text-[13px] text-destructive">{error}</p>
          ) : result ? (
            <div className="w-full space-y-3">
              <p className="text-[13px] text-success font-semibold text-center">{t("ctrl.ipmi.ready")}</p>
              {result.url ? (
                <a
                  href={result.url}
                  target="_blank"
                  rel="noreferrer"
                  className="flex items-center justify-center gap-2 w-full px-4 py-3 border border-border rounded-2xl hover:bg-secondary/50 transition-colors text-[13px] font-semibold"
                >
                  <ExternalLink className="w-4 h-4" />
                  {t("ctrl.ipmi.openInNewTab")}
                </a>
              ) : (
                <div className="flex items-center justify-center gap-2 w-full px-4 py-3 border border-border rounded-2xl text-[13px] text-muted-foreground">
                  <Download className="w-4 h-4" />
                  {t("ctrl.ipmi.jnlpOpenHint")}
                </div>
              )}
              <p className="text-[11px] text-muted-foreground text-center">
                {t("ctrl.ipmi.linkHint", { type: result.accessType })}
              </p>
            </div>
          ) : null}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.close")}
          </Button>
          {result ? (
            <Button onClick={openConsole} disabled={loading}>
              {t("ctrl.ipmi.retryOther")}
            </Button>
          ) : (
            <Button onClick={openConsole} disabled={loading || typesLoading || !chosen}>
              {loading ? t("ctrl.ipmi.requesting") : t("ctrl.ipmi.openConsole")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
