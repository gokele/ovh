import { ShieldAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Chip } from "@/components/common/Chip";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { DetailErrorTag } from "@/components/common/PartialNotice";
import {
  useVpsMitigation, useEnableVpsMitigation, useDisableVpsMitigation,
} from "@/hooks/use-vps-control";
import { toast } from "sonner";

/** VPS DDoS Mitigation 管理。逻辑跟 server-control 的 MitigationPane 相同 —— OVH
 *  自动缓解默认开,我们只暴露「永久缓解」手动开关。VPS 一般只 1 个 IP,UI 比 dedicated 简单。 */
export function VpsMitigationPane({ serviceName }: { serviceName: string }) {
  const list = useVpsMitigation(serviceName);
  const enable = useEnableVpsMitigation(serviceName);
  const disable = useDisableVpsMitigation(serviceName);

  if (list.isPending) return <Skeleton className="h-40 rounded-2xl" />;

  const blocks = list.data || [];
  if (blocks.length === 0) {
    return <EmptyState icon={ShieldAlert} title="该 VPS 无 IP" />;
  }

  const handleToggle = async (ip: string, block: string, currentlyActive: boolean) => {
    try {
      if (currentlyActive) {
        await disable.mutateAsync({ ip, block });
        toast.success("已关闭永久 DDoS 缓解");
      } else {
        await enable.mutateAsync({ ip, block });
        toast.success("已启用永久 DDoS 缓解");
      }
    } catch (e: any) {
      const raw = String(e?.response?.data?.error || e?.message || "");
      // OVH 在 mitigation 处理中 / 攻击进行中时,state 必须是 "ok" 才允许操作
      if (/state need to be ok/i.test(raw)) {
        toast.error("当前 mitigation 状态不允许关闭(可能正在被自动启用或攻击中)。等状态变 ok 再试", { duration: 6000 });
      } else if (/is not valid for type ipv4/i.test(raw)) {
        toast.error("OVH anti-DDoS 只支持 IPv4。IPv6 默认有网络层防护,无需手动配置", { duration: 6000 });
      } else {
        toast.error(raw || "操作失败");
      }
    }
  };

  return (
    <div className="space-y-3">
      <p className="text-[11px] text-muted-foreground">
        OVH 自动缓解(auto)默认开启,检测到攻击时自动启用。下面是手动启用「永久缓解」的开关 —
        开启后 VPS 所有流量长期过 Anti-DDoS 设备(延迟略增,持续防护)。
        <br />
        <span className="text-amber-600 dark:text-amber-400">仅支持 IPv4。IPv6 走 OVH 网络层默认免疫,无需手动配置。</span>
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
              IPv6 不适用 anti-DDoS Mitigation(OVH 网络层免疫)
            </div>
          ) : blk.mitigations.length === 0 ? (
            <div className="px-3.5 py-3 text-[12px] text-muted-foreground flex items-center gap-2 flex-wrap">
              <span>无永久缓解,自动缓解备用中</span>
              <Button
                size="sm"
                variant="outline"
                className="ml-auto h-7"
                onClick={() => handleToggle(bareIp, blk.ipBlock, false)}
                disabled={enable.isPending}
              >
                启用永久缓解
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
                      <Chip tone={mitigationTone(m.state)}>{stateText(m.state)}</Chip>
                    )}
                    {m.auto && <span className="text-[11px] text-muted-foreground">自动</span>}
                    {m.permanent && (
                      <span className="text-[11px] text-emerald-600 dark:text-emerald-400">永久</span>
                    )}
                    <Button
                      size="sm"
                      variant="outline"
                      className="ml-auto h-7"
                      onClick={() => handleToggle(m.ipOnMitigation, blk.ipBlock, true)}
                      disabled={disable.isPending || !isOk || !!rowErr}
                      title={
                        isCreating
                          ? "正在启用中,通常 30 秒-2 分钟,等状态变 ok 再点关闭"
                          : isRemoving
                            ? "正在移除中,稍后会自动从列表消失"
                            : ""
                      }
                    >
                      {isCreating ? "应用中…" : isRemoving ? "移除中…" : "关闭永久"}
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

/** OVH 三个状态值翻译 */
function stateText(state: string): string {
  return {
    ok: "已生效",
    creationPending: "应用中",
    removalPending: "移除中",
  }[state] || state;
}
