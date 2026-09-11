import * as React from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";

/** shadcn-ui Dialog：禁止使用浏览器 alert/confirm，统一用这个 */
const Dialog = DialogPrimitive.Root;
const DialogTrigger = DialogPrimitive.Trigger;
const DialogPortal = DialogPrimitive.Portal;
const DialogClose = DialogPrimitive.Close;

const DialogOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn(
      "fixed inset-0 z-50 bg-black/40",
      "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
      className
    )}
    {...props}
  />
));
DialogOverlay.displayName = DialogPrimitive.Overlay.displayName;

const DialogContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content>
>(({ className, children, ...props }, ref) => (
  <DialogPortal>
    <DialogOverlay />
    <DialogPrimitive.Content
      ref={ref}
      className={cn(
        "fixed z-50 grid gap-4 border-border bg-background",
        // ── 手机端:从底部升起的 sheet ──
        // 居中弹窗在手机上有两个实际问题:高一点的对话框(重装、硬件更换)会顶破
        // 视口且没有滚动;底部的确认按钮落在拇指够不到的屏幕中间偏上。
        // 从底部升起则天然贴着拇指,而且 max-h + overflow 保证再长也能滚。
        "inset-x-0 bottom-0 w-full rounded-t-2xl border-t p-4 pt-3 max-h-[88vh] overflow-y-auto",
        "data-[state=open]:slide-in-from-bottom data-[state=closed]:slide-out-to-bottom",
        // ── sm 以上:回到原来的居中弹窗 ──
        "sm:inset-x-auto sm:bottom-auto sm:left-[50%] sm:top-[50%] sm:max-w-lg sm:translate-x-[-50%] sm:translate-y-[-50%]",
        "sm:rounded-2xl sm:border sm:p-6 sm:max-h-[85vh]",
        "sm:data-[state=closed]:zoom-out-95 sm:data-[state=open]:zoom-in-95",
        "sm:data-[state=open]:slide-in-from-bottom-0 sm:data-[state=closed]:slide-out-to-bottom-0",
        "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
        className
      )}
      style={{ paddingBottom: "max(env(safe-area-inset-bottom), 1rem)" }}
      {...props}
    >
      {/* 抓手条:手机端用来提示「这是个从底部拉上来的面板」,桌面端不需要 */}
      <div className="sm:hidden mx-auto -mt-1 mb-1 w-9 h-1 rounded-full bg-border flex-shrink-0" />
      {children}
      {/* 关闭按钮在手机端要 44px 点击区,桌面端维持原来的紧凑尺寸 */}
      <DialogPrimitive.Close className="absolute right-2 top-2 sm:right-4 sm:top-4 inline-flex items-center justify-center w-11 h-11 sm:w-auto sm:h-auto rounded-full sm:p-1.5 opacity-70 hover:opacity-100 hover:bg-muted transition-opacity focus:outline-none focus:ring-2 focus:ring-ring disabled:pointer-events-none">
        <X className="h-4 w-4" />
        <span className="sr-only">Close</span>
      </DialogPrimitive.Close>
    </DialogPrimitive.Content>
  </DialogPortal>
));
DialogContent.displayName = DialogPrimitive.Content.displayName;

const DialogHeader = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn("flex flex-col space-y-1.5 text-left", className)} {...props} />
);

/**
 * 对话框底部操作区。
 *
 * 手机端竖排且反向:子元素通常是「取消 → 确认」的顺序,flex-col-reverse
 * 让确认落到上面、取消在下面 —— 跟 iOS 系统弹窗一致,而且主操作离拇指更近。
 * 按钮全宽是为了给足点击面积,手机上并排两个按钮各自都太窄。
 */
const DialogFooter = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      "flex flex-col-reverse gap-2 [&>button]:w-full",
      "sm:flex-row sm:justify-end sm:gap-0 sm:space-x-2 sm:[&>button]:w-auto",
      className
    )}
    {...props}
  />
);

const DialogTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title ref={ref} className={cn("text-lg font-semibold leading-none tracking-tight", className)} {...props} />
));
DialogTitle.displayName = DialogPrimitive.Title.displayName;

const DialogDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description ref={ref} className={cn("text-sm text-muted-foreground", className)} {...props} />
));
DialogDescription.displayName = DialogPrimitive.Description.displayName;

export {
  Dialog,
  DialogPortal,
  DialogOverlay,
  DialogTrigger,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogFooter,
  DialogTitle,
  DialogDescription,
};
