import { useEffect, useRef, useState } from "react";
import { Monitor, Loader2, ExternalLink, Download } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";
import { toast } from "sonner";

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
        setError(e?.response?.data?.error || "查询控制台类型失败");
      } finally {
        setTypesLoading(false);
      }
    })();
  }, [open, serviceName]);

  // 申请控制台会话(要轮询 OVH 任务,约 20 秒)
  const openConsole = () => {
    if (!chosen) {
      toast.error("请先选择接入方式");
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
          setError("控制台返回为空");
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
          toast.success("JNLP 文件已下载");
          setResult({ accessType });
        } else {
          setResult({ url: value, accessType });
        }
      } catch (e: any) {
        clearInterval(interval);
        setLoading(false);
        setError(e?.response?.data?.error || "打开控制台失败");
      }
    })();
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Monitor className="w-5 h-5" />
            IPMI / KVM 控制台
          </DialogTitle>
          <DialogDescription>
            选择接入方式后申请远程控制台。申请要向 OVH 提交任务并轮询，约 20 秒。
          </DialogDescription>
        </DialogHeader>

        {/* 接入方式选择：查得快，先让用户挑，别等 20 秒才发现没有 Java KVM */}
        {!loading && !result && (
          <div className="space-y-2">
            {typesLoading ? (
              <p className="text-[13px] text-muted-foreground flex items-center gap-2">
                <Loader2 className="w-4 h-4 animate-spin" />
                正在查询该服务器支持的接入方式…
              </p>
            ) : types && types.supportedTypes.length > 0 ? (
              <>
                <label className="text-[12px] font-semibold block">接入方式</label>
                <div className="space-y-1.5">
                  {types.supportedTypes.map((t) => (
                    <label
                      key={t}
                      className={`flex items-start gap-2 px-3 py-2 border rounded-xl cursor-pointer text-[13px] transition-colors ${
                        chosen === t ? "border-primary bg-primary/5" : "border-border hover:bg-secondary/40"
                      }`}
                    >
                      <input
                        type="radio"
                        name="ipmi-type"
                        className="mt-0.5"
                        checked={chosen === t}
                        onChange={() => setChosen(t)}
                      />
                      <span>
                        {types.typeLabels[t] || t}
                        {t === "kvmipJnlp" && (
                          <span className="block text-[11px] text-muted-foreground mt-0.5">
                            需要本机装 Java Web Start。新版 JDK 已移除它，可用 OpenWebStart 打开 .jnlp
                          </span>
                        )}
                        {t === "serialOverLanSshKey" && (
                          <span className="block text-[11px] text-muted-foreground mt-0.5">
                            用 OVH 账户里已登记的 SSH 公钥连接
                          </span>
                        )}
                      </span>
                    </label>
                  ))}
                </div>
                {!types.activated && (
                  <p className="text-[11px] text-warning">
                    该服务器 IPMI 显示未激活，申请可能失败；如失败请先到 OVH 后台启用 IPMI。
                  </p>
                )}
              </>
            ) : (
              <p className="text-[13px] text-muted-foreground">该服务器不支持 KVM / SOL 控制台。</p>
            )}
          </div>
        )}

        <div className="flex flex-col items-center justify-center py-6 gap-3">
          {loading ? (
            <>
              <Loader2 className="w-12 h-12 animate-spin text-muted-foreground" />
              <p className="text-[13px] text-muted-foreground">
                正在获取控制台访问… {countdown > 0 && `约 ${countdown}s`}
              </p>
            </>
          ) : error ? (
            <p className="text-[13px] text-destructive">{error}</p>
          ) : result ? (
            <div className="w-full space-y-3">
              <p className="text-[13px] text-success font-semibold text-center">控制台访问已就绪</p>
              {result.url ? (
                <a
                  href={result.url}
                  target="_blank"
                  rel="noreferrer"
                  className="flex items-center justify-center gap-2 w-full px-4 py-3 border border-border rounded-2xl hover:bg-secondary/50 transition-colors text-[13px] font-semibold"
                >
                  <ExternalLink className="w-4 h-4" />
                  在新标签页打开控制台
                </a>
              ) : (
                <div className="flex items-center justify-center gap-2 w-full px-4 py-3 border border-border rounded-2xl text-[13px] text-muted-foreground">
                  <Download className="w-4 h-4" />
                  JNLP 文件已下载，请用 Java 打开
                </div>
              )}
              <p className="text-[11px] text-muted-foreground text-center">
                链接仅当次有效。访问类型：{result.accessType}
              </p>
            </div>
          ) : null}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            关闭
          </Button>
          {result ? (
            <Button onClick={openConsole} disabled={loading}>
              换一种方式重开
            </Button>
          ) : (
            <Button onClick={openConsole} disabled={loading || typesLoading || !chosen}>
              {loading ? "申请中…" : "打开控制台"}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
