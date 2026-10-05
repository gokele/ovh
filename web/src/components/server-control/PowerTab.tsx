import { useState } from "react";
import { Power, RotateCw, HardDrive, Monitor, Zap, Server, Cog, Activity, LifeBuoy } from "lucide-react";
import type { OwnedServer } from "@/hooks/use-server-control";
import { Button } from "@/components/ui/button";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { BootModeDialog } from "./BootModeDialog";
import { TasksDialog } from "./TasksDialog";
import { ReinstallDialog } from "./ReinstallDialog";
import { BiosDialog } from "./BiosDialog";
import { InstallProgressDialog } from "./InstallProgressDialog";
import { IpmiDialog } from "./IpmiDialog";
import { SplaDialog } from "./SplaDialog";
import { RescueDialog } from "./RescueDialog";
import { useTranslation } from "react-i18next";
import { errorMessage } from "@/components/common/LoadFailed";

/** 电源与系统 Tab：重启 / 重装 / IPMI / 启动模式 / 解锁 Windows / 任务 / BIOS / 安装进度 */
export function PowerTab({ server }: { server: OwnedServer }) {
  const { t } = useTranslation();
  const [bootOpen, setBootOpen] = useState(false);
  const [tasksOpen, setTasksOpen] = useState(false);
  const [reinstallOpen, setReinstallOpen] = useState(false);
  const [biosOpen, setBiosOpen] = useState(false);
  const [progressOpen, setProgressOpen] = useState(false);
  const [ipmiOpen, setIpmiOpen] = useState(false);
  const [splaOpen, setSplaOpen] = useState(false);
  const [rebootOpen, setRebootOpen] = useState(false);
  const [rescueOpen, setRescueOpen] = useState(false);
  const [rebooting, setRebooting] = useState(false);

  const doReboot = async () => {
    // 按下就发,不给第二次机会 —— 所以必须先确认、发的过程中必须禁用。
    setRebooting(true);
    try {
      await api.post(`/server-control/${server.serviceName}/reboot`);
      toast.success(t("maint.power.toast.rebootSent"));
      setRebootOpen(false);
    } catch (e: any) {
      toast.error(errorMessage(e));
    } finally {
      setRebooting(false);
    }
  };

  return (
    <>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
        <ActionCard
          icon={Power}
          title={t("maint.power.cards.rebootTitle")}
          description={t("maint.power.cards.rebootDesc")}
          onClick={() => setRebootOpen(true)}
          tone="warning"
        />
        {/* 救援排在重装前面:遇到"进不去系统"时,该先进救援看看,
            而不是直接重装把数据抹掉。顺序本身就是一种引导。 */}
        <ActionCard
          icon={LifeBuoy}
          title={t("maint.power.cards.rescueTitle")}
          description={t("maint.power.cards.rescueDesc")}
          onClick={() => setRescueOpen(true)}
          tone="warning"
        />
        <ActionCard
          icon={HardDrive}
          title={t("maint.power.cards.reinstallTitle")}
          description={t("maint.power.cards.reinstallDesc")}
          onClick={() => setReinstallOpen(true)}
          tone="danger"
        />
        <ActionCard
          icon={Monitor}
          title={t("maint.power.cards.ipmiTitle")}
          description={t("maint.power.cards.ipmiDesc")}
          onClick={() => setIpmiOpen(true)}
        />
        <ActionCard
          icon={Server}
          title={t("maint.power.cards.bootTitle")}
          description={t("maint.power.cards.bootDesc")}
          onClick={() => setBootOpen(true)}
          tone="warning"
        />
        <ActionCard
          icon={Zap}
          title={t("maint.power.cards.splaTitle")}
          description={t("maint.power.cards.splaDesc")}
          onClick={() => setSplaOpen(true)}
        />
        <ActionCard
          icon={RotateCw}
          title={t("maint.power.cards.tasksTitle")}
          description={t("maint.power.cards.tasksDesc")}
          onClick={() => setTasksOpen(true)}
        />
        <ActionCard
          icon={Cog}
          title={t("maint.power.cards.biosTitle")}
          description={t("maint.power.cards.biosDesc")}
          onClick={() => setBiosOpen(true)}
          tone="warning"
        />
        <ActionCard
          icon={Activity}
          title={t("maint.power.cards.progressTitle")}
          description={t("maint.power.cards.progressDesc")}
          onClick={() => setProgressOpen(true)}
        />
      </div>

      <SplaDialog serviceName={server.serviceName} open={splaOpen} onOpenChange={setSplaOpen} />
      <BootModeDialog serviceName={server.serviceName} open={bootOpen} onOpenChange={setBootOpen} />
      <RescueDialog
        serviceName={server.serviceName}
        displayName={server.name}
        open={rescueOpen}
        onOpenChange={setRescueOpen}
      />
      <TasksDialog serviceName={server.serviceName} open={tasksOpen} onOpenChange={setTasksOpen} />
      <ReinstallDialog serviceName={server.serviceName} open={reinstallOpen} onOpenChange={setReinstallOpen} />
      <BiosDialog serviceName={server.serviceName} open={biosOpen} onOpenChange={setBiosOpen} />
      <InstallProgressDialog serviceName={server.serviceName} open={progressOpen} onOpenChange={setProgressOpen} />
      <IpmiDialog serviceName={server.serviceName} open={ipmiOpen} onOpenChange={setIpmiOpen} />

      {/* 重启确认。
          以前这张卡片是"点一下就直接硬重启" —— 没有确认、没有禁用、没有进行中反馈。
          卡片自己的描述写着"相当于按电源键,未落盘的数据会丢",而误点一次就发生了,
          连点两下就是两次重启。跑着业务的机器经不起这个。 */}
      <Dialog open={rebootOpen} onOpenChange={(v) => !rebooting && setRebootOpen(v)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("maint.power.rebootTitle", { name: server.serviceName })}</DialogTitle>
            <DialogDescription>{t("maint.power.rebootDesc")}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRebootOpen(false)} disabled={rebooting}>
              {t("common.cancel")}
            </Button>
            <Button variant="destructive" onClick={doReboot} disabled={rebooting}>
              {rebooting ? t("maint.power.rebooting") : t("maint.power.confirmReboot")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

function ActionCard({
  icon: Icon,
  title,
  description,
  onClick,
  tone = "default",
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  description: string;
  onClick: () => void;
  tone?: "default" | "danger" | "warning";
}) {
  const { t } = useTranslation();
  const iconColor = tone === "danger" ? "text-destructive" : tone === "warning" ? "text-warning" : "text-foreground";
  return (
    <div className="border border-border rounded-2xl p-5 flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <Icon className={`w-4 h-4 ${iconColor}`} />
        <h3 className="text-sm font-semibold">{title}</h3>
      </div>
      <p className="text-[12px] text-muted-foreground flex-1">{description}</p>
      <Button variant="outline" size="sm" onClick={onClick} className="self-start">
        {t("maint.power.runBtn")}
      </Button>
    </div>
  );
}
