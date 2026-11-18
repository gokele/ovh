import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * 胶囊状态徽章。tone 决定语义色：
 * - default: 灰底深字
 * - success / warning / danger / info: 各自的语义浅底深字
 * - solid: 全黑底白字（强调）
 */
type Tone = "default" | "success" | "warning" | "danger" | "info" | "solid";

const toneClasses: Record<Tone, string> = {
  default: "bg-muted text-muted-foreground",
  success: "bg-success/10 text-success",
  warning: "bg-warning/[.12] text-warning",
  danger: "bg-destructive/10 text-destructive",
  info: "bg-primary/10 text-primary",
  solid: "bg-button-primary text-button-primary-foreground",
};

interface ChipProps extends React.HTMLAttributes<HTMLSpanElement> {
  tone?: Tone;
  size?: "sm" | "md";
}

/** 通用胶囊 chip，状态徽章统一用这个 */
export function Chip({ tone = "default", size = "sm", className, children, ...rest }: ChipProps) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full font-medium whitespace-nowrap leading-none tabular-nums",
        size === "sm" ? "h-[22px] px-2 text-[11.5px]" : "h-6 px-2.5 text-xs",
        toneClasses[tone],
        className
      )}
      {...rest}
    >
      {children}
    </span>
  );
}
