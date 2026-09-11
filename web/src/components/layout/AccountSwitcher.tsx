import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Check, ChevronsUpDown, Plus, User } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useAccounts } from "@/hooks/use-accounts";
import { LoadFailed } from "@/components/common/LoadFailed";
import { useActiveAccount } from "@/hooks/use-active-account";
import { OVH_SUBSIDIARIES } from "@/lib/ovh-subsidiaries";
import { cn } from "@/lib/utils";

/**
 * 账户切换器(桌面在左侧栏顶部,手机在顶栏) —— **全站唯一**的账户入口。
 *
 * 为什么只留这一个：OVH 的 EU / US / CA 三个站点目录互不相通，同一台机器
 * 在欧区叫 24sk602、美区叫 24sk602-v1-us。以前列表页、下单对话框、服务器控制页
 * 各有一个账户选择器且互不同步，"拿欧区机型配美区账户"一键就能做出来，
 * 结果是任务被后端拒绝（400），而用户只看到控制台里一个红色报错。
 *
 * 现在切一次，机型列表、可用性、价格、控制台、下单账户全部跟着走。
 */
export function AccountSwitcher({
  onNavigate,
  compact,
}: {
  onNavigate?: () => void;
  /** 顶栏用的紧凑版:去掉「当前账户」标题和外边距,高度压到 36px 塞进 48px 的顶栏 */
  compact?: boolean;
}) {
  const accounts = useAccounts();
  const [activeId, setActive] = useActiveAccount();
  // 受控:选完要自己关掉。Radix Popover 默认不会因为点了内容里的按钮就收起,
  // 不管的话选完账户面板还杵在那儿挡着导航。
  const [open, setOpen] = useState(false);

  const list = accounts.data || [];
  const active = list.find((a) => a.id === activeId);

  // 没选过、或选中的账户被删了 → 落到默认账户（没有默认就取第一个）
  useEffect(() => {
    if (!list.length) return;
    if (active) return;
    const fallback = list.find((a) => a.isDefault) || list[0];
    if (fallback) setActive(fallback.id);
  }, [list, active, setActive]);

  const zoneLabel = (zone: string) =>
    OVH_SUBSIDIARIES.find((s) => s.code === zone)?.label?.split(" · ")[0] || zone;

  if (accounts.isPending) {
    return <div className="mx-3 mt-3 h-[52px] rounded-lg bg-muted animate-pulse" />;
  }

  // 读取失败 ≠ 一个账户都没有。这里是全站唯一的账户入口,一旦塌成虚线的
  // "添加 OVH 账户",下游每个页面都会跟着按"没有账户"降级:机型列表空、
  // 下单按钮灰、控制台说没绑定 —— 用户看到的是"我的账户没了",于是去重新
  // 填一遍 AppKey/AppSecret,而真相只是这一次 /accounts 请求没成功。
  // 失败必须说成失败,并且给一个重试口。
  if (accounts.isError) {
    return (
      <div className={compact ? "" : "mx-3 mt-3"}>
        <LoadFailed
          title="账户列表读取失败"
          error={accounts.error}
          onRetry={() => accounts.refetch()}
          compact
        />
      </div>
    );
  }

  if (!list.length) {
    return (
      <Link
        to="/settings"
        onClick={onNavigate}
        className={cn(
          "flex items-center gap-2 px-2.5 rounded-lg border border-dashed border-border text-[13px] text-muted-foreground hover:bg-muted transition-colors",
          compact ? "h-9" : "mx-3 mt-3 py-2"
        )}
      >
        <Plus className="w-4 h-4" />
        添加 OVH 账户
      </Link>
    );
  }

  return (
    <div className={compact ? "min-w-0" : "px-3 mt-3"}>
      {!compact && (
        <div className="px-0.5 mb-1 text-[11px] font-semibold text-muted-foreground uppercase tracking-wider">
          当前账户
        </div>
      )}
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <button
            className={cn(
              "w-full flex items-center gap-2 rounded-lg border border-border hover:bg-muted transition-colors text-left",
              // 顶栏版:单行,账户名和站点码并排;侧栏版:两行
              compact ? "h-9 px-2" : "px-2.5 py-2"
            )}
            title="切换账户：机型列表、价格、库存、控制台全部跟着当前账户走"
          >
            <span
              className={cn(
                "flex items-center justify-center rounded-md bg-secondary flex-shrink-0",
                compact ? "w-6 h-6" : "w-7 h-7"
              )}
            >
              <User className="w-3.5 h-3.5" />
            </span>
            {compact ? (
              <span className="min-w-0 flex-1 flex items-baseline gap-1.5">
                <span className="text-[13px] font-medium truncate">{active?.name || "选择账户"}</span>
                <span className="text-[11px] text-muted-foreground flex-shrink-0">
                  {active?.zone || ""}
                </span>
              </span>
            ) : (
              <span className="min-w-0 flex-1">
                <span className="block text-[13px] font-medium truncate">{active?.name || "选择账户"}</span>
                <span className="block text-[11px] text-muted-foreground truncate">
                  {active ? `${active.zone} · ${zoneLabel(active.zone)}` : "未选择"}
                </span>
              </span>
            )}
            <ChevronsUpDown className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-[248px] p-1">
          <div className="max-h-[280px] overflow-y-auto">
            {list.map((a) => (
              <button
                key={a.id}
                onClick={() => {
                  setActive(a.id);
                  setOpen(false);
                }}
                className={cn(
                  "w-full flex items-center gap-2 px-2 py-2.5 sm:py-1.5 rounded-md text-left text-[13px] transition-colors",
                  a.id === activeId ? "bg-secondary" : "hover:bg-muted"
                )}
              >
                <Check className={cn("w-3.5 h-3.5 flex-shrink-0", a.id === activeId ? "opacity-100" : "opacity-0")} />
                <span className="min-w-0 flex-1">
                  <span className="block font-medium truncate">
                    {a.name}
                    {a.isDefault && <span className="ml-1 text-[10px] text-muted-foreground">默认</span>}
                  </span>
                  <span className="block text-[11px] text-muted-foreground truncate">
                    {a.zone} · {zoneLabel(a.zone)}
                  </span>
                </span>
              </button>
            ))}
          </div>
          <Link
            to="/settings"
            onClick={() => {
              setOpen(false);
              onNavigate?.();
            }}
            className="flex items-center gap-2 px-2 py-1.5 mt-1 rounded-md text-[13px] text-muted-foreground hover:bg-muted border-t border-border pt-2 transition-colors"
          >
            <Plus className="w-3.5 h-3.5" />
            管理账户
          </Link>
        </PopoverContent>
      </Popover>
      <p className="px-0.5 mt-1.5 text-[10px] text-muted-foreground leading-snug">
        机型、价格、库存、控制台都按这个账户所在站点显示
      </p>
    </div>
  );
}
