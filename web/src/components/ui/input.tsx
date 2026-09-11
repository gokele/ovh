import * as React from "react";
import { cn } from "@/lib/utils";

/** Input：12px 圆角，1px 边框，focus 时 ring 用 --ring */
export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {}

const Input = React.forwardRef<HTMLInputElement, InputProps>(({ className, type, ...props }, ref) => {
  return (
    <input
      type={type}
      ref={ref}
      className={cn(
        // 手机端必须 >= 16px:iOS Safari 在聚焦一个字号小于 16px 的输入框时
        // 会自动把整个页面放大,用户得手动双指缩回去,每个搜索框都中招。
        // h-11(44px) 同时满足可达性的最小点击区。
        "flex h-11 sm:h-10 w-full rounded-xl border border-input bg-background px-3.5 py-2 text-base sm:text-sm",
        "placeholder:text-muted-foreground",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-0",
        "disabled:cursor-not-allowed disabled:opacity-50",
        "file:border-0 file:bg-transparent file:text-sm file:font-medium",
        className
      )}
      {...props}
    />
  );
});
Input.displayName = "Input";

export { Input };
