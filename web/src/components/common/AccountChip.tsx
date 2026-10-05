import { useAccounts, accountChipColor, findAccountByID } from "@/hooks/use-accounts";
import { errorMessage } from "@/components/common/LoadFailed";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";

/** 显示某个账户的小 chip:`主号 · IE`。多账户场景所有任务行尾都用它。
 *
 *  查不到账户时**必须先分清是哪种查不到**,三种原因对用户的含义完全相反:
 *    还在加载   → 等一下就有,什么都别断言
 *    列表读失败 → 是我们没问到列表,用户的账户大概率好好的
 *    列表拿到了但没这条 → 账户是真的没了(历史记录里留着已删账户的 accountId)
 *  以前一律渲染成"未知账户 / 账户 xxx 不存在",等于拿一次网络抖动去宣判用户的账户
 *  被删了 —— 用户会跑去设置页重建账户,或者以为历史记录脏了,两种都是被我们骗的。
 */
export function AccountChip({ accountId, className }: { accountId: string; className?: string }) {
  const { data: accounts, isPending, isError, error } = useAccounts();
  const { t } = useTranslation();
  const acc = findAccountByID(accounts, accountId);

  if (!acc) {
    const state = !accountId
      ? "unset"
      : isError
        ? "failed"
        : isPending || !accounts
          ? "loading"
          : "missing";

    const label =
      state === "unset"
        ? t("commons.accountChip.unsetLabel")
        : state === "failed"
          ? t("commons.accountChip.failedLabel")
          : state === "loading"
            ? t("commons.accountChip.loadingLabel")
            : t("commons.accountChip.missingLabel");

    const title =
      state === "unset"
        ? t("commons.accountChip.unsetTitle")
        : state === "failed"
          ? t("commons.accountChip.failedTitle", { id: accountId, reason: errorMessage(error) })
          : state === "loading"
            ? t("commons.accountChip.loadingTitle")
            : t("commons.accountChip.missingTitle", { id: accountId });

    return (
      <span
        className={cn(
          "inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10.5px] font-medium",
          // 失败态用 destructive 底色,跟"账户确实没了"的灰色分开,一眼能看出是我们没读到
          state === "failed"
            ? "bg-destructive/10 text-destructive"
            : "bg-muted text-muted-foreground",
          className
        )}
        title={title}
      >
        {label}
      </span>
    );
  }

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10.5px] font-medium whitespace-nowrap",
        accountChipColor(acc.zone),
        className
      )}
      title={t("commons.accountChip.knownTitle", { name: acc.name, zone: acc.zone, endpoint: acc.endpoint })}
    >
      {acc.name} · {acc.zone}
    </span>
  );
}
