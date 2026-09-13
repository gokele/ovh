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

/** 电源与系统 Tab：重启 / 重装 / IPMI / 启动模式 / 解锁 Windows / 任务 / BIOS / 安装进度 */
export function PowerTab({ server }: { server: OwnedServer }) {
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
      toast.success("重启已发起");
      setRebootOpen(false);
    } catch (e: any) {
      toast.error(e.response?.data?.error || "重启失败");
    } finally {
      setRebooting(false);
    }
  };

  return (
    <>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
        <ActionCard
          icon={Power}
          title="重启服务器"
          description="硬重启(相当于按电源键,未落盘的数据会丢)"
          onClick={() => setRebootOpen(true)}
          tone="warning"
        />
        {/* 救援排在重装前面:遇到"进不去系统"时,该先进救援看看,
            而不是直接重装把数据抹掉。顺序本身就是一种引导。 */}
        <ActionCard
          icon={LifeBuoy}
          title="一键救援系统"
          description="进救援模式修系统(不动硬盘数据),自动改启动项+重启"
          onClick={() => setRescueOpen(true)}
          tone="warning"
        />
        <ActionCard
          icon={HardDrive}
          title="重装系统"
          description="选择 OS 模板 + ZFS / RAID / 自定义分区"
          onClick={() => setReinstallOpen(true)}
          tone="danger"
        />
        <ActionCard
          icon={Monitor}
          title="IPMI 控制台"
          description="远程 KVM 控制台（20s 倒计时获取链接）"
          onClick={() => setIpmiOpen(true)}
        />
        <ActionCard
          icon={Server}
          title="启动模式"
          description="切换硬盘 / 救援 / 网络启动"
          onClick={() => setBootOpen(true)}
          tone="warning"
        />
        <ActionCard
          icon={Zap}
          title="SPLA 许可证"
          description="登记你的 Windows / SQL Server 授权"
          onClick={() => setSplaOpen(true)}
        />
        <ActionCard
          icon={RotateCw}
          title="查看任务"
          description="近期所有运维任务（含可用时间段查询）"
          onClick={() => setTasksOpen(true)}
        />
        <ActionCard
          icon={Cog}
          title="BIOS 设置"
          description="查看 BIOS / SGX 当前配置"
          onClick={() => setBiosOpen(true)}
          tone="warning"
        />
        <ActionCard
          icon={Activity}
          title="安装进度"
          description="实时查看当前重装任务进度"
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
            <DialogTitle>确认硬重启 {server.serviceName}？</DialogTitle>
            <DialogDescription>
              这相当于按下电源键，不是操作系统里的正常重启：内存和磁盘缓存里还没落盘的数据会丢，
              正在跑的服务会被直接切断。确认前请先在系统里停好业务。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRebootOpen(false)} disabled={rebooting}>
              取消
            </Button>
            <Button variant="destructive" onClick={doReboot} disabled={rebooting}>
              {rebooting ? "正在发起…" : "确认重启"}
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
  const iconColor = tone === "danger" ? "text-destructive" : tone === "warning" ? "text-warning" : "text-foreground";
  return (
    <div className="border border-border rounded-2xl p-5 flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <Icon className={`w-4 h-4 ${iconColor}`} />
        <h3 className="text-sm font-semibold">{title}</h3>
      </div>
      <p className="text-[12px] text-muted-foreground flex-1">{description}</p>
      <Button variant="outline" size="sm" onClick={onClick} className="self-start">
        执行
      </Button>
    </div>
  );
}
