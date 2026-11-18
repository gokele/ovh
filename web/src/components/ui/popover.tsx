import * as React from "react";
import * as PopoverPrimitive from "@radix-ui/react-popover";
import { cn } from "@/lib/utils";

/** 与 pearl/layout.css 的 .m-tabbar 断点一致 */
const TABBAR_MQ = "(max-width: 820px)";
function useHasTabbar() {
  const [on, setOn] = React.useState(() => typeof window !== "undefined" && window.matchMedia?.(TABBAR_MQ).matches);
  React.useEffect(() => {
    const mq = window.matchMedia?.(TABBAR_MQ);
    if (!mq) return;
    const f = () => setOn(mq.matches);
    f();
    mq.addEventListener("change", f);
    return () => mq.removeEventListener("change", f);
  }, []);
  return !!on;
}

const Popover = PopoverPrimitive.Root;
const PopoverTrigger = PopoverPrimitive.Trigger;
const PopoverAnchor = PopoverPrimitive.Anchor;

const PopoverContent = React.forwardRef<
  React.ElementRef<typeof PopoverPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof PopoverPrimitive.Content>
>(({ className, align = "start", sideOffset = 4, collisionPadding, ...props }, ref) => {
  const hasTabbar = useHasTabbar();
  return (
  <PopoverPrimitive.Portal>
    <PopoverPrimitive.Content
      ref={ref}
      align={align}
      sideOffset={sideOffset}
      // 手机底部标签栏(≤820px)会挡住弹层下半截:把它算进碰撞边距,
      // 再用 Radix 给出的可用高度限高,弹层永远完整落在屏幕里、内部滚动。
      collisionPadding={collisionPadding ?? { top: 8, left: 8, right: 8, bottom: hasTabbar ? 84 : 8 }}
      className={cn(
        "max-h-[var(--radix-popover-content-available-height)] overflow-y-auto overscroll-contain",
        "z-[110] rounded-lg border border-border bg-popover p-0 text-popover-foreground shadow-md outline-none",
        "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
        className
      )}
      {...props}
    />
  </PopoverPrimitive.Portal>
  );
});
PopoverContent.displayName = PopoverPrimitive.Content.displayName;

export { Popover, PopoverTrigger, PopoverAnchor, PopoverContent };
