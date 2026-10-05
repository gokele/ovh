import { ShieldAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Chip } from "@/components/common/Chip";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed } from "@/components/common/LoadFailed";
import { DetailErrorTag } from "@/components/common/PartialNotice";
import {
  useVpsMitigation, useEnableVpsMitigation, useDisableVpsMitigation,
} from "@/hooks/use-vps-control";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";

/** VPS DDoS Mitigation 管理。逻辑跟 server-control 的 MitigationPane 相同 —— OVH
 *  自动缓解默认开,我们只暴露「永久缓解」手动开关。VPS 一般只 1 个 IP,UI 比 dedicated 简单。 */
export function VpsMitigationPane({ serviceName }: { serviceName: string }) {
  const { t } = useTranslation();
  const list = useVpsMitigation(serviceName);
  const enable = useEnableVpsMitigation(serviceName);
  const disable = useDisableVpsMitigation(serviceName);

  if (list.isPending) return <Skeleton className="h-40 rounded-2xl" />;
  // 读失败 ≠ 这台 VPS 没有 IP。后端拿不到 /vps/{svc}/ips 时直接返 500,这里 data 就是空数组,
  // 再走下面的「该 VPS 无 IP」等于在 DDoS 页面上告诉用户"没 IP,不用配防护" —— 防护就此被跳过。
  // 注:mitigation 这套端点 EU / US / CA 三区都注册,不存在"本区没这个能力"的情况,
  // 所以这里的 isError 一律是真失败,可以放心报错(参见 use-vps-control.ts 里 unsupported 的约定)。
  if (list.isError) {
    return (
      <LoadFailed
        icon={ShieldAlert}
        title={t("vps.mitigation.loadFailed")}
        error={list.error}
        onRetry={() => list.refetch()}
      />
    );
  }

  const blocks = list.data || [];
  if (blocks.length === 0) {
    return <EmptyState icon={ShieldAlert} title={t("vps.mitigation.noIp")} />;
  }

  const handleToggle = async (ip: string, block: string, currentlyActive: boolean) => {
    try {
      if (currentlyActive) {
        await disable.mutateAsync({ ip, block });
        toast.success(t("vps.mitigation.toast.disabled"));
      } else {
        await enable.mutateAsync({ ip, block });
        toast.success(t("vps.mitigation.toast.enabled"));
      }
    } catch (e: any) {
      const raw = String(e?.response?.data?.error || e?.message || "");
      // OVH 在 mitigation 处理中 / 攻击进行中时,state 必须是 "ok" 才允许操作
      if (/state need to be ok/i.test(raw)) {
        toast.error(t("vps.mitigation.toast.stateNotOk"), { duration: 6000 });
      } else if (/is not valid for type ipv4/i.test(raw)) {
        toast.error(t("vps.mitigation.toast.ipv4Only"), { duration: 6000 });
      } else {
        toast.error(raw || t("vps.mitigation.toast.failed"));
      }
    }
  };

  return (
    <div className="space-y-3">
      <p className="text-[11px] text-muted-foreground">
        {t("vps.mitigation.intro")}
        <br />
        <span className="text-warning">{t("vps.mitigation.ipv6Note")}</span>
      </p>
      {blocks.map((blk) => {
        const isV6 = blk.ipBlock.includes(":") && !blk.ipBlock.includes(".");
        // 后端同一行已经给了裸 IP，直接用它比从 ipBlock 上 split("/") 反推更准也更直白
        const bareIp = blk.ipAddress || blk.ipBlock.split("/")[0];
        return (
        <div key={blk.ipBlock} className="border border-border rounded-2xl overflow-hidden">
          <div className="px-3.5 py-2.5 border-b border-border bg-secondary/30 flex items-center gap-2">
            <ShieldAlert className="w-3.5 h-3.5 text-muted-foreground" />
            <code className="text-[12px] font-mono font-semibold">{blk.ipBlock}</code>
            {isV6 && <span className="text-[10px] text-muted-foreground ml-1">IPv6</span>}
            {blk.error && <span className="text-[11px] text-destructive ml-auto">{blk.error}</span>}
          </div>
          {isV6 ? (
            <div className="px-3.5 py-3 text-[12px] text-muted-foreground">
              {t("vps.mitigation.v6NotApplicable")}
            </div>
          ) : blk.mitigations.length === 0 ? (
            <div className="px-3.5 py-3 text-[12px] text-muted-foreground flex items-center gap-2 flex-wrap">
              <span>{t("vps.mitigation.noPermanent")}</span>
              <Button
                size="sm"
                variant="outline"
                className="ml-auto h-7"
                onClick={() => handleToggle(bareIp, blk.ipBlock, false)}
                disabled={enable.isPending}
              >
                {t("vps.mitigation.enableBtn")}
              </Button>
            </div>
          ) : (
            <div className="divide-y divide-border">
              {blk.mitigations.map((m) => {
                // OVH MitigationStateEnum = creationPending / ok / removalPending
                // 只有 ok 是稳定态可以操作;另外两个是过渡态,需等。
                // permanent 字段已被 OVH 标 DEPRECATED,这里只展示不再判定。
                const isOk = m.state === "ok";
                const isCreating = m.state === "creationPending";
                const isRemoving = m.state === "removalPending";
                // 详情没拉到的 IP 后端保留占位(只有 ipOnMitigation + error)，不标出来就是一行空状态。
                // 注：VpsMitigationIp 类型里还没有 error 字段（hooks 层，本次不改），先就地读。
                const rowErr = (m as unknown as { error?: string }).error;
                return (
                  <div key={m.ipOnMitigation} className="px-3.5 py-2.5 flex items-center gap-2 text-[12px] flex-wrap">
                    <code className="font-mono">{m.ipOnMitigation}</code>
                    {rowErr ? (
                      <DetailErrorTag message={rowErr} />
                    ) : (
                      <Chip tone={mitigationTone(m.state)}>{t(stateText(m.state))}</Chip>
                    )}
                    {m.auto && <span className="text-[11px] text-muted-foreground">{t("vps.mitigation.autoTag")}</span>}
                    {m.permanent && (
                      <span className="text-[11px] text-success">{t("vps.mitigation.permanentTag")}</span>
                    )}
                    <Button
                      size="sm"
                      variant="outline"
                      className="ml-auto h-7"
                      onClick={() => handleToggle(m.ipOnMitigation, blk.ipBlock, true)}
                      disabled={disable.isPending || !isOk || !!rowErr}
                      title={
                        isCreating
                          ? t("vps.mitigation.creatingTitle")
                          : isRemoving
                            ? t("vps.mitigation.removingTitle")
                            : ""
                      }
                    >
                      {isCreating
                        ? t("vps.mitigation.applying")
                        : isRemoving
                          ? t("vps.mitigation.removing")
                          : t("vps.mitigation.disableBtn")}
                    </Button>
                  </div>
                );
              })}
            </div>
          )}
        </div>
        );
      })}
    </div>
  );
}

function mitigationTone(state: string): "success" | "warning" | "default" {
  if (state === "ok") return "success";
  if (state === "creationPending" || state === "removalPending") return "warning";
  return "default";
}

/** OVH 三个状态值 → i18n key(vps.mitigation.state.*),渲染处统一 t();没收录的原样透传 */
function stateText(state: string): string {
  return {
    ok: "vps.mitigation.state.ok",
    creationPending: "vps.mitigation.state.creationPending",
    removalPending: "vps.mitigation.state.removalPending",
  }[state] || state;
}
