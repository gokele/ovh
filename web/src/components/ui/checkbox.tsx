import * as React from "react";
import * as CheckboxPrimitive from "@radix-ui/react-checkbox";
import { Check } from "lucide-react";
import { cn } from "@/lib/utils";

/** shadcn-ui Checkbox：checked 时 button-primary 黑底白勾 */
const Checkbox = React.forwardRef<
  React.ElementRef<typeof CheckboxPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof CheckboxPrimitive.Root>
>(({ className, ...props }, ref) => (
  <CheckboxPrimitive.Root
    ref={ref}
    className={cn(
      // 视觉还是 16px(桌面上放大会很丑),但用伪元素把触摸热区撑到 44px ——
      // 手机上 16px 的点击目标基本点不中,而这个勾选框控制的是
      // 「自动付款」这类真要花钱的开关,点偏一下代价很实在。
      "peer h-4 w-4 shrink-0 rounded border border-input bg-background",
      "relative before:absolute before:left-1/2 before:top-1/2 before:-translate-x-1/2 before:-translate-y-1/2 before:h-11 before:w-11 before:content-[''] sm:before:hidden",
      "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1",
      "disabled:cursor-not-allowed disabled:opacity-50",
      "data-[state=checked]:bg-button-primary data-[state=checked]:text-button-primary-foreground data-[state=checked]:border-button-primary",
      className
    )}
    {...props}
  >
    <CheckboxPrimitive.Indicator className={cn("flex items-center justify-center text-current")}>
      <Check className="h-3 w-3" strokeWidth={3} />
    </CheckboxPrimitive.Indicator>
  </CheckboxPrimitive.Root>
));
Checkbox.displayName = CheckboxPrimitive.Root.displayName;

export { Checkbox };
