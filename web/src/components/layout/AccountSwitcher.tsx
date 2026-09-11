import { useEffect, useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Check, ChevronDown, ChevronsUpDown, Plus } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useAccounts } from "@/hooks/use-accounts";
import { LoadFailed } from "@/components/common/LoadFailed";
import { useActiveAccount } from "@/hooks/use-active-account";
import { zoneStyle, zoneName, regionOf, type ZoneRegion } from "@/lib/zone-color";
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

  // 按 API endpoint 区域分组,组内保持后端给的顺序(默认账户通常在前)。
  // 认不出子公司的账户单独归到「未知区」,不并进欧区 —— 猜错区 = 下单打错站点。
  const grouped = useMemo(() => {
    const order: (ZoneRegion | "unknown")[] = ["eu", "us", "ca", "unknown"];
    const buckets = new Map<string, typeof list>();
    for (const a of list) {
      const key = regionOf(a.zone) ?? "unknown";
      const arr = buckets.get(key) || [];
      arr.push(a);
      buckets.set(key, arr);
    }
    return order
      .filter((k) => buckets.has(k))
      .map((k) => ({
        region: k,
        label: zoneStyle(buckets.get(k)![0].zone).label,
        accounts: buckets.get(k)!,
      }));
  }, [list]);

  // 没选过、或选中的账户被删了 → 落到默认账户（没有默认就取第一个）
  useEffect(() => {
    if (!list.length) return;
    if (active) return;
    const fallback = list.find((a) => a.isDefault) || list[0];
    if (fallback) setActive(fallback.id);
  }, [list, active, setActive]);


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
              "w-full flex items-center gap-2 text-left transition-colors",
              compact
                // 顶栏版:填充式胶囊,不用描边 —— 描边会让它看起来像个输入框,
                // 而它是个控件。32px 高在 48px 顶栏里上下各留 8px。
                ? "h-8 pl-2 pr-1.5 rounded-lg bg-secondary hover:bg-muted active:bg-muted"
                : "px-2.5 py-2 rounded-lg border border-border hover:bg-muted"
            )}
            title="切换账户：机型列表、价格、库存、控制台全部跟着当前账户走"
          >
            {/* 账户名在前、区域徽章在后。
                名字才是身份 —— 同一个区里可以有好几个账户,它们的区域完全相同,
                能把它们区分开的只有名字,所以名字优先占据宽度。
                徽章回答的是另一个问题:「这个账户属于哪套目录」——
                三区 planCode 互不相通,拿欧区机型配美区账户必然被拒。 */}
            {compact ? (
              <span className="min-w-0 flex-1 flex items-center gap-1.5">
                <span className="text-[13px] font-medium truncate">{active?.name || "选择账户"}</span>
                {active && (
                  <span
                    className={cn(
                      "flex-shrink-0 px-1.5 py-px rounded text-[10px] font-semibold tracking-wide",
                      zoneStyle(active.zone).badge
                    )}
                  >
                    {active.zone}
                  </span>
                )}
              </span>
            ) : (
              <span className="min-w-0 flex-1">
                <span className="block text-[13px] font-medium truncate">{active?.name || "选择账户"}</span>
                <span className="block text-[11px] text-muted-foreground truncate">
                  {active ? `${active.zone} · ${zoneName(active.zone)}` : "未选择"}
                </span>
              </span>
            )}
            {compact ? (
              <ChevronDown className="w-3 h-3 text-muted-foreground flex-shrink-0" strokeWidth={2.5} />
            ) : (
              <ChevronsUpDown className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />
            )}
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-[248px] p-1">
          {/* 切换账户的后果写在面板顶部 —— 用户正要做选择的这一刻才是该说的时候。
              侧栏版在外面也有一份常驻说明,顶栏版(手机)只靠这里。 */}
          <p className="px-2 pt-1.5 pb-2 text-[10.5px] text-muted-foreground leading-snug border-b border-border mb-1">
            机型、价格、库存、控制台都按这个账户所在站点显示
          </p>
          {/* 按区域分组。同一个区里可以有好几个账户,平铺成一条长列表的话
              用户得逐行去读子公司码才知道哪几个是一伙的;分组之后
              「这三个共用欧区目录、那一个是美区」一眼就分得清。 */}
          {/* 手机上屏幕高度富余,280px 会把第三个区挤到要滚才看得到;
              桌面侧栏空间紧,维持原值 */}
          <div className="max-h-[45vh] sm:max-h-[280px] overflow-y-auto">
            {grouped.map(({ region, label, accounts }) => (
              <div key={region} className="mb-0.5 last:mb-0">
                {/* 只有一个区时不必再打分组标题 —— 那是句废话 */}
                {grouped.length > 1 && (
                  <div className="px-2 pt-1.5 pb-1 text-[10px] font-semibold text-muted-foreground tracking-wide">
                    {label}
                  </div>
                )}
                {accounts.map((a) => (
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
                    <Check
                      className={cn(
                        "w-3.5 h-3.5 flex-shrink-0",
                        a.id === activeId ? "opacity-100" : "opacity-0"
                      )}
                    />
                    {/* 名字占满可用宽度:同区账户之间只有名字不一样 */}
                    <span className="min-w-0 flex-1">
                      <span className="block font-medium truncate">
                        {a.name}
                        {a.isDefault && (
                          <span className="ml-1 text-[10px] font-normal text-muted-foreground">默认</span>
                        )}
                      </span>
                      <span className="block text-[11px] text-muted-foreground truncate">
                        {zoneName(a.zone)}
                      </span>
                    </span>
                    <span
                      className={cn(
                        "flex-shrink-0 px-1.5 py-px rounded text-[10px] font-semibold tracking-wide",
                        zoneStyle(a.zone).badge
                      )}
                    >
                      {a.zone}
                    </span>
                  </button>
                ))}
              </div>
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
      {/* 这句说明只在侧栏版出现。顶栏只有 48px 高,多这一行会把它撑破、
          压到下面的页面标题上。同样的意思已放进下拉面板顶部,
          手机用户点开切换器时照样看得到。 */}
      {!compact && (
        <p className="px-0.5 mt-1.5 text-[10px] text-muted-foreground leading-snug">
          机型、价格、库存、控制台都按这个账户所在站点显示
        </p>
      )}
    </div>
  );
}
