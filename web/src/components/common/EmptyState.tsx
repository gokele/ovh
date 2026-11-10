import type { LucideIcon } from "lucide-react";
import { Illustration, type IllustrationName } from "@/components/illustrations/Illustration";
import { cn } from "@/lib/utils";

interface EmptyStateProps {
  /** 弹窗等紧凑场景用图标 */
  icon?: LucideIcon;
  /** 整页空态用自绘插画;同时给出时插画优先 */
  illustration?: IllustrationName;
  title: string;
  description?: string;
  action?: React.ReactNode;
  className?: string;
}

/** 空态展示:插画或图标 + 标题 + 说明 + 下一步操作。 */
export function EmptyState({ icon: Icon, illustration, title, description, action, className }: EmptyStateProps) {
  return (
    <div className={cn("flex flex-col items-center justify-center text-center", illustration ? "py-10 px-6" : "py-12 px-6", className)}>
      {illustration ? (
        <Illustration name={illustration} className="w-40 h-[120px] mb-2" />
      ) : Icon ? (
        <Icon className="w-10 h-10 text-muted-foreground/50 mb-3" strokeWidth={1.5} aria-hidden />
      ) : null}
      <p className={cn("font-medium text-foreground", illustration ? "text-[15px]" : "text-sm")}>{title}</p>
      {description && <p className="text-xs text-muted-foreground mt-1.5 max-w-sm leading-relaxed">{description}</p>}
      {action && <div className="mt-4 flex flex-wrap items-center justify-center gap-2">{action}</div>}
    </div>
  );
}
