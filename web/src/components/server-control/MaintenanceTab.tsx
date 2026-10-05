import { useState } from "react";
import { AlertCircle, CalendarRange, Cpu, Mail, Network } from "lucide-react";
import type { OwnedServer } from "@/hooks/use-server-control";
import { useServerInterventions } from "@/hooks/use-server-control";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/common/Skeleton";
import { Chip } from "@/components/common/Chip";
import { useActiveAccountEndpoint } from "@/components/common/active-endpoint";
import { NetworkSpecsDialog } from "@/components/server-control/NetworkSpecsDialog";
import { EngagementDialog } from "@/components/server-control/EngagementDialog";
import { HardwareReplaceDialog } from "./HardwareReplaceDialog";
import { ChangeContactDialog } from "./ChangeContactDialog";
import { useTranslation } from "react-i18next";
import { errorMessage } from "@/components/common/LoadFailed";
import { fmtDateTime } from "@/i18n/format";

/** 维护 Tab：维护记录列表 + 硬件更换工单 + 变更联系人 */
export function MaintenanceTab({ server }: { server: OwnedServer }) {
  const { t } = useTranslation();
  const [netSpecsOpen, setNetSpecsOpen] = useState(false);
  const [engagementOpen, setEngagementOpen] = useState(false);
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
            <h3 className="text-sm font-semibold">{t("maint.maintenance.recordsTitle")}</h3>
          </div>
          {interventions.isPending ? (
            <Skeleton className="h-32 rounded-2xl" />
          ) : interventions.isError ? (
            /* 详情全部拉取失败时后端返 500。以前这里和"确实没有记录"共用一句文案，
               用户会以为机器一直健康，而实际上是没读到 —— 两种状态必须分开 */
            <div className="border border-destructive/40 bg-destructive/5 rounded-2xl p-6 text-center text-sm space-y-2">
              <p className="text-destructive">{t("maint.maintenance.loadFailed")}</p>
              <p className="text-[12px] text-muted-foreground">{errorMessage(interventions.error)}</p>
              <Button size="sm" variant="outline" onClick={() => interventions.refetch()}>
                {t("common.retry")}
              </Button>
            </div>
          ) : (interventions.data || []).length === 0 ? (
            <div className="border border-border rounded-2xl p-6 text-center text-sm text-muted-foreground">
              {t("maint.maintenance.empty")}
            </div>
          ) : (
            <div className="border border-border rounded-2xl divide-y divide-border">
              {(interventions.data || []).slice(0, 20).map((iv: any) => (
                <InterventionRow key={iv.id} intervention={iv} />
              ))}
            </div>
          )}
        </div>

        {/* 四张同类卡片:都是"偶尔打开一次的对话框"。
            网络规格和合同期原来在页面顶部当胶囊按钮 —— 它们没有值可显示,
            只是个入口,却各占 100px 把标签行挤到换行。放这里才是它们的同类。 */}
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <ActionCard
            icon={Network}
            title={t("maint.maintenance.cards.netSpecsTitle")}
            description={t("maint.maintenance.cards.netSpecsDesc")}
            onClick={() => setNetSpecsOpen(true)}
          />
          <ActionCard
            icon={CalendarRange}
            title={t("maint.maintenance.cards.engagementTitle")}
            description={t("maint.maintenance.cards.engagementDesc")}
            onClick={() => setEngagementOpen(true)}
          />
          <ActionCard
            icon={Cpu}
            title={t("maint.maintenance.cards.hwTitle")}
            description={t("maint.maintenance.cards.hwDesc")}
            onClick={() => setHwOpen(true)}
          />
          <ActionCard
            icon={Mail}
            title={t("maint.maintenance.cards.contactTitle")}
            description={
              contactUnsupported
                ? t("maint.maintenance.cards.contactDescUs")
                : t("maint.maintenance.cards.contactDesc")
            }
            onClick={() => setContactOpen(true)}
            disabled={contactUnsupported}
          />
        </div>
      </div>

      <NetworkSpecsDialog
        serviceName={server.serviceName}
        open={netSpecsOpen}
        onOpenChange={setNetSpecsOpen}
      />
      <EngagementDialog
        serviceName={server.serviceName}
        open={engagementOpen}
        onOpenChange={setEngagementOpen}
      />
      <HardwareReplaceDialog serviceName={server.serviceName} open={hwOpen} onOpenChange={setHwOpen} />
      <ChangeContactDialog serviceName={server.serviceName} open={contactOpen} onOpenChange={setContactOpen} />
    </>
  );
}

/** 单条维护记录：对齐旧前端字段（id / interventionId / type / status / description / expectedEndDate） */
function InterventionRow({ intervention: iv }: { intervention: any }) {
  const { t } = useTranslation();
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
          {t("maint.maintenance.expectedEnd", { time: fmtDateTime(iv.expectedEndDate) })}
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
  const { t } = useTranslation();
  return (
    <div className={"border border-border rounded-2xl p-5 flex flex-col gap-3" + (disabled ? " opacity-60" : "")}>
      <div className="flex items-center gap-2">
        <Icon className="w-4 h-4 text-muted-foreground" />
        <h3 className="text-sm font-semibold">{title}</h3>
      </div>
      <p className="text-[12px] text-muted-foreground flex-1">{description}</p>
      <Button variant="outline" size="sm" className="self-start" onClick={onClick} disabled={disabled}>
        {disabled ? t("maint.maintenance.unavailable") : t("maint.maintenance.openBtn")}
      </Button>
    </div>
  );
}
