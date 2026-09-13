import { useEffect, useState } from "react";
import { LifeBuoy, AlertTriangle, Loader2 } from "lucide-react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { LoadFailed } from "@/components/common/LoadFailed";
import { useRescueStatus, useEnterRescue, useExitRescue } from "@/hooks/use-server-control";

/**
 * 一键救援系统。
 *
 * 手动做这件事要在 OVH 后台点四步:进服务器 → 改 netboot 为救援 → 填收密码的邮箱
 * → 回去重启服务器。漏掉最后一步是最常见的错误 —— netboot 改了但没重启,
 * 机器还在正常系统里跑,用户对着 SSH 连不上发懵。
 * 这里把三个 API 调用合成一个按钮,并且明确告诉用户重启已经发出去了。
 */
export function RescueDialog({
  serviceName,
  displayName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  displayName?: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const status = useRescueStatus(serviceName, open);
  const enter = useEnterRescue(serviceName);
  const exit = useExitRescue(serviceName);
  const [email, setEmail] = useState("");
  const [confirming, setConfirming] = useState(false);

  // 每次打开都重置二次确认,并把上次用过的邮箱带出来省得重填
  useEffect(() => {
    if (!open) return;
    setConfirming(false);
    setEmail(status.data?.rescueMail || "");
  }, [open, status.data?.rescueMail]);

  const inRescue = status.data?.inRescue === true;
  const busy = enter.isPending || exit.isPending;
  // 邮箱可以留空(用账户默认联系邮箱),填了就得像个邮箱 —— 填错等于收不到密码
  const emailBad = email.trim() !== "" && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <LifeBuoy className="w-4 h-4" />
            救援系统
          </DialogTitle>
          <DialogDescription>
            {displayName ? <span className="font-mono">{displayName}</span> : serviceName}
          </DialogDescription>
        </DialogHeader>

        {status.isError ? (
          <LoadFailed
            compact
            icon={LifeBuoy}
            title="救援状态读取失败"
            error={status.error}
            onRetry={() => status.refetch()}
          />
        ) : (
          <div className="space-y-4">
            <div className="rounded-xl border border-border bg-muted/40 px-3.5 py-3">
              <p className="text-[12px]">
                当前状态：
                {status.isPending ? (
                  <span className="text-muted-foreground">读取中…</span>
                ) : inRescue ? (
                  <b className="text-amber-600 dark:text-amber-500">救援模式</b>
                ) : (
                  <b>正常系统</b>
                )}
              </p>
              <p className="text-[11px] text-muted-foreground mt-1 leading-relaxed">
                救援系统是一个独立的临时 Linux，从网络启动，<b>不会动你硬盘上的数据</b>。
                进去之后可以挂载硬盘修配置、改密码、拷数据。
              </p>
            </div>

            {inRescue ? (
              <div className="rounded-xl border border-border px-3.5 py-3 text-[12px] leading-relaxed">
                机器已经在救援模式里。修完之后点下面的「退出救援模式」切回正常系统 ——
                <b>不切回去的话，每次重启都会再进救援</b>。
              </div>
            ) : (
              <>
                <div>
                  <label className="block text-[13px] font-medium mb-1.5">
                    接收登录密码的邮箱<span className="text-muted-foreground font-normal">（可选）</span>
                  </label>
                  <Input
                    value={email}
                    onChange={(e) => { setEmail(e.target.value); setConfirming(false); }}
                    placeholder="留空 = 发到 OVH 账户的联系邮箱"
                    inputMode="email"
                  />
                  {emailBad ? (
                    <p className="text-[11px] text-destructive mt-1">
                      这个邮箱格式不对。救援系统的 root 密码会发到这里，填错就收不到。
                    </p>
                  ) : (
                    <p className="text-[11px] text-muted-foreground mt-1">
                      进入救援后 OVH 会把 root 密码发到这个邮箱，约 3~5 分钟后可以 SSH 登录。
                    </p>
                  )}
                </div>

                <div className="flex items-start gap-2.5 rounded-xl border border-amber-500/40 bg-amber-500/5 px-3.5 py-3">
                  <AlertTriangle className="w-4 h-4 text-amber-600 dark:text-amber-500 mt-0.5 flex-shrink-0" />
                  <div className="text-[12px] leading-relaxed">
                    点下去会<b>立刻重启服务器</b>，上面正在跑的服务会中断。
                    硬盘数据不受影响。
                  </div>
                </div>

                {confirming && (
                  <p className="text-[12px] text-amber-600 dark:text-amber-500">
                    再点一次「确认进入救援」就会重启。
                  </p>
                )}
              </>
            )}
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            关闭
          </Button>
          {inRescue ? (
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => exit.mutate(undefined, { onSuccess: () => onOpenChange(false) })}
            >
              {exit.isPending && <Loader2 className="w-4 h-4 animate-spin mr-1.5" />}
              退出救援模式（会重启）
            </Button>
          ) : (
            <Button
              disabled={busy || status.isPending || emailBad}
              onClick={() => {
                if (!confirming) { setConfirming(true); return; }
                enter.mutate(
                  { email: email.trim() || undefined },
                  { onSuccess: () => onOpenChange(false) }
                );
              }}
            >
              {enter.isPending && <Loader2 className="w-4 h-4 animate-spin mr-1.5" />}
              {confirming ? "确认进入救援（会重启）" : "进入救援模式"}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
