import { useEffect, useRef, useState } from "react";
import { Sparkles, Loader2, Download, CheckCircle2, AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";
import { useSelfUpdate, useUpdateProgress, type UpdateCheck } from "@/hooks/use-system-metrics";
import { toast } from "sonner";

/**
 * 在线更新按钮。
 *
 * 全流程：点一下 → 后端下载 → 校验 SHA256 → 替换正在运行的二进制 → 自动重启。
 * 用户不需要下载文件、不需要手动替换、不需要自己重启。
 *
 * 前端这一半的关键在**重启那几秒**：服务在换进程映像，所有请求都会失败。
 * 这时不能报错吓人 —— 连不上恰恰是重启正在发生的信号。所以进入 restarting 之后
 * 改为轮询 /api/version（不带鉴权也能通），直到它回报新版本号，再整页刷新
 * （二进制换了，内嵌的前端资源也跟着换了，不刷新会继续跑旧页面）。
 */
export function UpdateButton({ check }: { check?: UpdateCheck }) {
  const mut = useSelfUpdate();
  const [started, setStarted] = useState(false);
  const [waitingRestart, setWaitingRestart] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);
  const pollRef = useRef<number | null>(null);

  const progress = useUpdateProgress(started && !waitingRestart);

  // 进度推进到 restarting 就切换到「等重启」模式
  useEffect(() => {
    const phase = progress.data?.phase;
    if (phase === "restarting") setWaitingRestart(true);
    if (phase === "failed") {
      setFailed(progress.data?.error || "更新失败");
      setStarted(false);
    }
  }, [progress.data?.phase, progress.data?.error]);

  // 等待重启：轮询 /api/version，等到版本号变了就刷新整页
  useEffect(() => {
    if (!waitingRestart) return;
    const target = check?.latest;
    let tries = 0;
    const timer = window.setInterval(async () => {
      tries += 1;
      try {
        const res = await api.get<{ version: string }>("/version");
        const now = res.data?.version;
        // 版本号变成目标值 = 新进程已经起来了
        if (now && (!target || now === target)) {
          window.clearInterval(timer);
          toast.success(`已更新到 v${now}，正在刷新页面`);
          setTimeout(() => window.location.reload(), 800);
        } else if (now && target && now !== target && tries > 3) {
          // 服务回来了但版本不是目标值 = 新版本没起来、回滚保护把旧版换回来了。
          // 这是确凿信号，不能当"没消息"吞掉——以前这里会一路等到超时，
          // 然后说"二进制可能已经替换成功"，两头都不对。
          // 头几秒放过：旧进程还没退完时也会回旧版本号。
          window.clearInterval(timer);
          setWaitingRestart(false);
          setStarted(false);
          setFailed(
            `更新到 v${target} 失败，已自动回滚到 v${now}。服务正常运行，详情见后端日志`
          );
        }
      } catch {
        // 连不上是预期内的：进程正在被替换。继续等。
      }
      // 60 秒还没回来就别转圈了，给一句能行动的话
      if (tries > 60) {
        window.clearInterval(timer);
        setWaitingRestart(false);
        setStarted(false);
        setFailed("服务重启超时。二进制可能已经替换成功，请手动确认进程是否在跑（若用 systemd/docker 托管，它通常会自行拉起）");
      }
    }, 1000);
    pollRef.current = timer;
    return () => window.clearInterval(timer);
  }, [waitingRestart, check?.latest]);

  const start = async () => {
    setFailed(null);
    try {
      const res = await mut.mutateAsync();
      if (!res.success) {
        setFailed(res.error || "无法开始更新");
        return;
      }
      setStarted(true);
      toast.info(res.message || "已开始更新");
    } catch (e: any) {
      setFailed(e?.response?.data?.error || "无法开始更新");
    }
  };

  // 没有新版本就不显示任何东西
  if (!check?.hasUpdate && !started && !waitingRestart && !failed) return null;

  if (waitingRestart) {
    return (
      <span className="inline-flex items-center gap-1.5 text-[11px] text-primary">
        <Loader2 className="w-3 h-3 animate-spin" />
        正在重启并加载新版本…
      </span>
    );
  }

  if (started) {
    const p = progress.data;
    const pct = p?.percent ?? 0;
    return (
      <span className="inline-flex items-center gap-2 text-[11px] text-muted-foreground" title={p?.message}>
        <Loader2 className="w-3 h-3 animate-spin" />
        {p?.phase === "installing" ? (
          <span>校验通过，正在替换…</span>
        ) : (
          <span className="inline-flex items-center gap-1.5">
            下载中
            <span className="inline-block w-16 h-1 rounded bg-secondary overflow-hidden align-middle">
              <span className="block h-full bg-primary transition-all" style={{ width: `${pct}%` }} />
            </span>
            {pct}%
          </span>
        )}
      </span>
    );
  }

  if (failed) {
    return (
      <span className="inline-flex items-center gap-1.5 text-[11px] text-destructive" title={failed}>
        <AlertTriangle className="w-3 h-3" />
        <span className="max-w-[220px] truncate">{failed}</span>
        <button className="underline hover:no-underline" onClick={start}>
          重试
        </button>
      </span>
    );
  }

  return (
    <span className="inline-flex items-center gap-1.5">
      <a
        href={check!.url}
        target="_blank"
        rel="noreferrer noopener"
        className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-medium bg-primary/10 text-primary hover:bg-primary/20 transition-colors"
        title={`有新版本 v${check!.latest}，点击查看更新说明`}
      >
        <Sparkles className="w-3 h-3" />
        v{check!.latest}
      </a>
      <Button size="sm" variant="outline" className="h-6 px-2 text-[11px]" onClick={start} disabled={mut.isPending}>
        {mut.isPending ? <Loader2 className="w-3 h-3 animate-spin" /> : <Download className="w-3 h-3 mr-1" />}
        立即更新
      </Button>
    </span>
  );
}

/** 更新完成后的提示（重启回来那一下用得上，目前留给将来扩展） */
export function UpdatedBadge({ version }: { version: string }) {
  return (
    <span className="inline-flex items-center gap-1 text-[11px] text-success">
      <CheckCircle2 className="w-3 h-3" />
      已是最新 v{version}
    </span>
  );
}
