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
        <IconBadge icon={Icon} />
      ) : null}
      <p className={cn("font-medium text-foreground", illustration ? "text-[15px]" : "text-sm")}>{title}</p>
      {description && <p className="text-xs text-muted-foreground mt-1.5 max-w-sm leading-relaxed">{description}</p>}
      {action && <div className="mt-4 flex flex-wrap items-center justify-center gap-2">{action}</div>}
    </div>
  );
}

/**
 * 紧凑场景(弹窗、面板)的小插画:淡蓝光晕 + 虚线环 + 浮起的白色卡片托着图标 + 星光。
 * 与整页插画同一套 .illo-* 配色与动效,减少动态效果时静止。
 */
function IconBadge({ icon: Icon }: { icon: LucideIcon }) {
  return (
    <div className="relative w-24 h-20 mb-2" aria-hidden>
      <svg viewBox="0 0 96 80" className="illo absolute inset-0 w-full h-full">
        <ellipse cx="48" cy="72" rx="24" ry="4" className="illo-shadow" />
        <circle cx="48" cy="38" r="30" className="illo-halo" />
        <circle cx="48" cy="38" r="22" className="illo-ring" />
        <path className="illo-twinkle illo-accent" transform="translate(80 16) scale(.7)" d="M0 -6 C1 -1.6 1.6 -1 6 0 C1.6 1 1 1.6 0 6 C-1 1.6 -1.6 1 -6 0 C-1.6 -1 -1 -1.6 0 -6 Z" />
        <path className="illo-twinkle illo-warm" transform="translate(16 58) scale(.55)" d="M0 -6 C1 -1.6 1.6 -1 6 0 C1.6 1 1 1.6 0 6 C-1 1.6 -1.6 1 -6 0 C-1.6 -1 -1 -1.6 0 -6 Z" />
      </svg>
      <div className="absolute left-1/2 top-[38px] -translate-x-1/2 -translate-y-1/2">
        <div className="illo-float w-11 h-11 rounded-xl bg-card shadow-md ring-1 ring-border/70 flex items-center justify-center text-primary">
          <Icon className="w-5 h-5" strokeWidth={1.8} />
        </div>
      </div>
    </div>
  );
}
