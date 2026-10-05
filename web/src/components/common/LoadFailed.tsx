import type { LucideIcon } from "lucide-react";
import { AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { apiMessage } from "@/lib/api-error";
import i18n from "@/i18n";

/**
 * 「读取失败」占位 —— 必须跟空态分开渲染。
 *
 * 抢购工具里这两种状态的含义完全相反：
 *   空态   = OVH 说没有 / 确实没配 → 用户应该换目标
 *   失败态 = 我们没问到 → 用户应该重试，绝不能当成"没货"放弃
 * 以前一律走 EmptyState（"暂时缺货" / "暂无数据"），等于在给用户假消息。
 */
export function LoadFailed({
  icon: Icon = AlertTriangle,
  title,
  error,
  onRetry,
  compact,
}: {
  icon?: LucideIcon;
  title?: string;
  error?: unknown;
  onRetry?: () => void;
  /** 内嵌在卡片/表格里时用紧凑排版 */
  compact?: boolean;
}) {
  const msg = errorMessage(error);
  return (
    <div
      className={`border border-destructive/40 bg-destructive/5 rounded-2xl flex flex-col items-center gap-2 text-center ${
        compact ? "p-4" : "py-12 px-6"
      }`}
    >
      <Icon className={compact ? "w-6 h-6 text-destructive" : "w-9 h-9 text-destructive"} strokeWidth={1.5} />
      <p className="text-[13px] font-semibold text-foreground">{title ?? i18n.t("common.readFailed")}</p>
      <p className="text-[11px] text-muted-foreground max-w-sm">{msg}</p>
      {onRetry && (
        <Button size="sm" variant="outline" className="mt-1" onClick={onRetry}>
          {i18n.t("common.retry")}
        </Button>
      )}
    </div>
  );
}

/** 从 axios / fetch / Error 里挖出一句能给用户看的话。
 *  走 apiMessage 翻译层:后端带 code 的消息按当前语言出译文 */
export function errorMessage(error: unknown): string {
  return apiMessage(error) || i18n.t("common.checkNetwork");
}

/**
 * 顶部条幅版：整页还有别的内容可看时，用它提示"这一块的数据是旧的/缺的"，
 * 而不是把整页替换掉。
 */
export function LoadFailedBanner({
  title,
  error,
  onRetry,
}: {
  title: string;
  error?: unknown;
  onRetry?: () => void;
}) {
  return (
    <div className="flex items-start gap-2 rounded-xl border border-destructive/40 bg-destructive/5 px-3 py-2 text-[12px]">
      <AlertTriangle className="w-4 h-4 text-destructive flex-shrink-0 mt-0.5" />
      <div className="flex-1 min-w-0">
        <p className="font-semibold">{title ?? i18n.t("common.readFailed")}</p>
        <p className="text-muted-foreground mt-0.5">{errorMessage(error)}</p>
      </div>
      {onRetry && (
        <Button size="sm" variant="outline" className="flex-shrink-0" onClick={onRetry}>
          {i18n.t("common.retry")}
        </Button>
      )}
    </div>
  );
}
