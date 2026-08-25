import { useState } from "react";
import { AlertCircle, Cpu, Mail } from "lucide-react";
import type { OwnedServer } from "@/hooks/use-server-control";
import { useServerInterventions } from "@/hooks/use-server-control";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/common/Skeleton";
import { Chip } from "@/components/common/Chip";
import { useActiveAccountEndpoint } from "@/components/common/active-endpoint";
import { HardwareReplaceDialog } from "./HardwareReplaceDialog";
import { ChangeContactDialog } from "./ChangeContactDialog";

/** 维护 Tab：维护记录列表 + 硬件更换工单 + 变更联系人 */
export function MaintenanceTab({ server }: { server: OwnedServer }) {
  const interventions = useServerInterventions(server.serviceName);
  const [hwOpen, setHwOpen] = useState(false);
  const [contactOpen, setContactOpen] = useState(false);
  // 美区没有 NIC 联系人系统，后端会 400；入口直接禁用并写明原因，别让用户白跑一趟
  const { isUS, ready } = useActiveAccountEndpoint();
  const contactUnsupported = ready && isUS;

  return (
    <>
      <div className="space-y-6">
        <div>
          <div className="flex items-center gap-2 mb-3">
            <AlertCircle className="w-4 h-4 text-muted-foreground" />
            <h3 className="text-sm font-semibold">维护记录</h3>
          </div>
          {interventions.isPending ? (
            <Skeleton className="h-32 rounded-2xl" />
          ) : interventions.isError ? (
            /* 详情全部拉取失败时后端返 500。以前这里和"确实没有记录"共用一句文案，
               用户会以为机器一直健康，而实际上是没读到 —— 两种状态必须分开 */
            <div className="border border-destructive/40 bg-destructive/5 rounded-2xl p-6 text-center text-sm space-y-2">
              <p className="text-destructive">维护记录读取失败</p>
              <p className="text-[12px] text-muted-foreground">
                {(interventions.error as any)?.response?.data?.error ||
                  (interventions.error as any)?.message ||
                  "请稍后重试"}
              </p>
              <Button size="sm" variant="outline" onClick={() => interventions.refetch()}>
                重试
              </Button>
            </div>
          ) : (interventions.data || []).length === 0 ? (
            <div className="border border-border rounded-2xl p-6 text-center text-sm text-muted-foreground">
              暂无维护记录
            </div>
          ) : (
            <div className="border border-border rounded-2xl divide-y divide-border">
              {(interventions.data || []).slice(0, 20).map((iv: any) => (
                <InterventionRow key={iv.id} intervention={iv} />
              ))}
            </div>
          )}
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <ActionCard
            icon={Cpu}
            title="硬件更换"
            description="提交硬件故障工单（硬盘 / 内存 / 散热）"
            onClick={() => setHwOpen(true)}
          />
          <ActionCard
            icon={Mail}
            title="变更联系人"
            description={
              contactUnsupported
                ? "美区账户不支持：OVHcloud US 没有 NIC 联系人系统，请在 US 控制台或联系客服办理"
                : "切换 admin / tech / billing NIC，含待审请求管理"
            }
            onClick={() => setContactOpen(true)}
            disabled={contactUnsupported}
          />
        </div>
      </div>

      <HardwareReplaceDialog serviceName={server.serviceName} open={hwOpen} onOpenChange={setHwOpen} />
      <ChangeContactDialog serviceName={server.serviceName} open={contactOpen} onOpenChange={setContactOpen} />
    </>
  );
}

/** 单条维护记录：对齐旧前端字段（id / interventionId / type / status / description / expectedEndDate） */
function InterventionRow({ intervention: iv }: { intervention: any }) {
  const status = String(iv.status || "").toLowerCase();
  const tone = status === "done" ? "success" : status === "doing" ? "warning" : "default";
  // 旧前端：active intervention 用 .interventionId 加 # 前缀，否则用 .id
  const displayId = iv.interventionId || iv.id;
  return (
    <div className="px-4 py-3 text-[13px] space-y-1">
      <div className="flex items-center gap-2 flex-wrap">
        <span className="font-mono font-semibold">#{displayId}</span>
        {iv.type && <Chip tone="default">{iv.type}</Chip>}
        {iv.status && <Chip tone={tone}>{iv.status}</Chip>}
      </div>
      {iv.description && <p className="text-[12px] text-foreground/80">{iv.description}</p>}
      {iv.expectedEndDate && (
        <p className="text-[11px] text-muted-foreground">
          预计结束：{new Date(iv.expectedEndDate).toLocaleString("zh-CN")}
        </p>
      )}
    </div>
  );
}

function ActionCard({
  icon: Icon,
  title,
  description,
  onClick,
  disabled,
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  description: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <div className={"border border-border rounded-2xl p-5 flex flex-col gap-3" + (disabled ? " opacity-60" : "")}>
      <div className="flex items-center gap-2">
        <Icon className="w-4 h-4 text-muted-foreground" />
        <h3 className="text-sm font-semibold">{title}</h3>
      </div>
      <p className="text-[12px] text-muted-foreground flex-1">{description}</p>
      <Button variant="outline" size="sm" className="self-start" onClick={onClick} disabled={disabled}>
        {disabled ? "不可用" : "打开"}
      </Button>
    </div>
  );
}
