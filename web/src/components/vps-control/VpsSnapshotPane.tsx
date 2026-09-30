import { useState } from "react";
import { Camera, Trash2, RotateCcw, Plus, AlertTriangle, Pencil } from "lucide-react";
import {
  useVpsSnapshot, useCreateVpsSnapshot, useUpdateVpsSnapshot, useRevertVpsSnapshot, useDeleteVpsSnapshot,
} from "@/hooks/use-vps-control";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed } from "@/components/common/LoadFailed";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { toast } from "sonner";

/** VPS 快照管理:OVH 免费档同时只允许 1 个,所以本 pane 只展示单快照 + 操作 */
export function VpsSnapshotPane({ serviceName }: { serviceName: string }) {
  const snap = useVpsSnapshot(serviceName);
  const create = useCreateVpsSnapshot(serviceName);
  const update = useUpdateVpsSnapshot(serviceName);
  const revert = useRevertVpsSnapshot(serviceName);
  const remove = useDeleteVpsSnapshot(serviceName);

  const [createOpen, setCreateOpen] = useState(false);
  const [createDesc, setCreateDesc] = useState("");
  const [revertOpen, setRevertOpen] = useState(false);
  const [revertConfirm, setRevertConfirm] = useState("");
  const [editOpen, setEditOpen] = useState(false);
  const [editDesc, setEditDesc] = useState("");

  if (snap.isPending) return <Skeleton className="h-40 rounded-2xl" />;
  // 快照读失败 ≠ 没有快照。
  // 后端在 OVH 返 404(确实没做过快照)时会转成 200 + snapshot:null,所以走到 isError 一定是没问到。
  // 这时如果还按下面的「暂无快照」渲染,有两个实打实的后果:
  //   1. 用户以为回滚点没了,白白重做一遍准备工作,或者干脆放弃回滚;
  //   2. 顺手点「创建快照」,撞上 OVH 的"已存在快照"(免费档每台只允许 1 个),白等一次报错。
  if (snap.isError) {
    return (
      <LoadFailed
        icon={Camera}
        title="快照信息读取失败"
        error={snap.error}
        onRetry={() => snap.refetch()}
      />
    );
  }

  const handleCreate = async () => {
    try {
      await create.mutateAsync({ description: createDesc });
      toast.success("快照创建任务已提交,通常 1-3 分钟完成");
      setCreateOpen(false);
      setCreateDesc("");
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "创建失败");
    }
  };

  const handleRevert = async () => {
    if (revertConfirm !== serviceName) {
      toast.error("VPS 名称不匹配");
      return;
    }
    try {
      await revert.mutateAsync();
      toast.success("回滚任务已提交,VPS 即将进入维护态");
      setRevertOpen(false);
      setRevertConfirm("");
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "回滚失败");
    }
  };

  const handleDelete = async () => {
    if (!confirm("确认删除当前快照?快照本身会删除,VPS 当前状态不受影响。")) return;
    try {
      await remove.mutateAsync();
      toast.success("快照已删除");
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "删除失败");
    }
  };

  const handleEditDesc = async () => {
    try {
      await update.mutateAsync({ description: editDesc });
      toast.success("快照描述已更新");
      setEditOpen(false);
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "更新失败");
    }
  };

  if (!snap.data) {
    return (
      <div className="space-y-3">
        <div className="border border-border rounded-2xl p-6">
          <EmptyState
            icon={Camera}
            title="暂无快照"
            description="OVH 免费档每台 VPS 同时只能存 1 个快照。改大动作前先做一个,出问题能 1 分钟回滚"
          />
          <div className="flex justify-center mt-2">
            <Button onClick={() => setCreateOpen(true)} disabled={create.isPending}>
              <Plus className="w-4 h-4 mr-1" />
              创建快照
            </Button>
          </div>
        </div>
        <CreateDialog
          open={createOpen}
          onOpenChange={setCreateOpen}
          desc={createDesc}
          setDesc={setCreateDesc}
          onConfirm={handleCreate}
          pending={create.isPending}
        />
      </div>
    );
  }

  const s = snap.data;
  return (
    <div className="space-y-3">
      <div className="border border-success/40 bg-success/5 rounded-2xl p-4 space-y-2.5">
        <div className="flex items-center gap-2 flex-wrap">
          <Camera className="w-4 h-4 text-success" />
          <h3 className="text-sm font-semibold">当前快照</h3>
          <code className="ml-auto text-[10px] font-mono text-muted-foreground">#{s.id}</code>
        </div>
        <div className="text-[12px] space-y-1">
          {s.description && <div className="font-medium">{s.description}</div>}
          <div className="text-muted-foreground">
            创建时间: {s.creationDate ? new Date(s.creationDate).toLocaleString("zh-CN") : "—"}
          </div>
          {s.region && <div className="text-muted-foreground">区域: {s.region}</div>}
        </div>
        <div className="flex flex-wrap gap-2 pt-1">
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setEditDesc(s.description || "");
              setEditOpen(true);
            }}
          >
            <Pencil className="w-3.5 h-3.5 mr-1" />
            改描述
          </Button>
          <Button
            size="sm"
            variant="destructive"
            onClick={() => setRevertOpen(true)}
            disabled={revert.isPending}
          >
            <RotateCcw className="w-3.5 h-3.5 mr-1" />
            回滚到此快照
          </Button>
          <Button size="sm" variant="outline" onClick={handleDelete} disabled={remove.isPending}>
            <Trash2 className="w-3.5 h-3.5 mr-1" />
            删除快照
          </Button>
        </div>
      </div>

      <p className="text-[11px] text-muted-foreground px-1">
        免费档单 VPS 只能存 1 个快照。要做新快照得先删旧的。
      </p>

      {/* 创建快照对话框(仅用于「改描述」时复用?其实不需要这里渲染,数据存在时不会触发 createOpen) */}
      <CreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        desc={createDesc}
        setDesc={setCreateDesc}
        onConfirm={handleCreate}
        pending={create.isPending}
      />

      {/* 修改描述 */}
      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>修改快照描述</DialogTitle>
          </DialogHeader>
          <Input
            value={editDesc}
            onChange={(e) => setEditDesc(e.target.value)}
            placeholder="给快照一段描述,方便日后辨识"
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditOpen(false)}>
              取消
            </Button>
            <Button onClick={handleEditDesc} disabled={update.isPending}>
              {update.isPending ? "保存中…" : "保存"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 回滚确认 — 需要输入 serviceName 才能确认 */}
      <Dialog open={revertOpen} onOpenChange={setRevertOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <AlertTriangle className="w-5 h-5 text-destructive" />
              回滚到此快照?
            </DialogTitle>
            <DialogDescription>这个操作不可逆</DialogDescription>
          </DialogHeader>
          <div className="border border-destructive/40 bg-destructive/5 rounded-xl p-3 space-y-1.5 text-[12px]">
            <p className="font-semibold text-destructive">快照之后所有改动会丢失:</p>
            <ul className="list-disc pl-5 text-muted-foreground space-y-0.5">
              <li>文件系统回到 {new Date(s.creationDate).toLocaleString("zh-CN")} 那一刻</li>
              <li>VPS 会自动重启,期间几分钟无法访问</li>
              <li>IP / 密码 等元数据不变</li>
            </ul>
          </div>
          <div>
            <label className="text-[12px] block mb-1.5">
              请输入 VPS 名称 <code className="font-mono">{serviceName}</code> 确认:
            </label>
            <Input
              value={revertConfirm}
              onChange={(e) => setRevertConfirm(e.target.value)}
              placeholder={serviceName}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRevertOpen(false)}>
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={handleRevert}
              disabled={revert.isPending || revertConfirm !== serviceName}
            >
              {revert.isPending ? "回滚中…" : "确认回滚"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function CreateDialog({
  open,
  onOpenChange,
  desc,
  setDesc,
  onConfirm,
  pending,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  desc: string;
  setDesc: (v: string) => void;
  onConfirm: () => void;
  pending: boolean;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>创建快照</DialogTitle>
          <DialogDescription>OVH 会暂停 VPS 30 秒-3 分钟做快照,期间网络中断</DialogDescription>
        </DialogHeader>
        <Input
          value={desc}
          onChange={(e) => setDesc(e.target.value)}
          placeholder="描述(可选),如 装 nginx 前"
        />
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={onConfirm} disabled={pending}>
            {pending ? "创建中…" : "创建快照"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
