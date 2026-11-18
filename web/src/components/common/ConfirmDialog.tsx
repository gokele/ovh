import { useCallback, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangle, HelpCircle } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export interface ConfirmOptions {
  title: string;
  description?: ReactNode;
  confirmText?: string;
  cancelText?: string;
  /** danger:删除、重启等不可撤回的操作,确认按钮用红色 */
  tone?: "danger" | "default";
}

/**
 * 统一的确认弹窗,替代浏览器原生 confirm()(样式与整站不一致、无法翻译按钮、手机上很突兀)。
 * 用法:const [confirm, confirmDialog] = useConfirm();  if (!(await confirm({...}))) return;
 * 并把 confirmDialog 渲染在组件里。
 */
export function useConfirm(): [(opts: ConfirmOptions) => Promise<boolean>, ReactNode] {
  const { t } = useTranslation();
  const [opts, setOpts] = useState<ConfirmOptions | null>(null);
  const resolver = useRef<((v: boolean) => void) | null>(null);

  const confirm = useCallback((o: ConfirmOptions) => {
    setOpts(o);
    return new Promise<boolean>((resolve) => {
      resolver.current = resolve;
    });
  }, []);

  const close = (v: boolean) => {
    resolver.current?.(v);
    resolver.current = null;
    setOpts(null);
  };

  const danger = opts?.tone === "danger";
  const element = (
    <Dialog open={!!opts} onOpenChange={(o) => !o && close(false)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <div className="flex items-start gap-3">
            <span
              className={cn(
                "w-10 h-10 rounded-full flex items-center justify-center shrink-0",
                danger ? "bg-destructive/10 text-destructive" : "bg-primary/10 text-primary",
              )}
            >
              {danger ? <AlertTriangle className="w-5 h-5" /> : <HelpCircle className="w-5 h-5" />}
            </span>
            <div className="min-w-0 pt-1 text-left">
              <DialogTitle>{opts?.title}</DialogTitle>
              {opts?.description && <DialogDescription className="mt-1.5">{opts.description}</DialogDescription>}
            </div>
          </div>
        </DialogHeader>
        <DialogFooter className="mt-2">
          <Button variant="outline" onClick={() => close(false)}>
            {opts?.cancelText || t("common.cancel")}
          </Button>
          <Button variant={danger ? "destructive" : "default"} onClick={() => close(true)} autoFocus>
            {opts?.confirmText || t("common.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
  return [confirm, element];
}
