import { useState } from "react";
import { Camera, Trash2, RotateCcw, Plus, AlertTriangle, Pencil } from "lucide-react";
import {
  useVpsSnapshot, useCreateVpsSnapshot, useUpdateVpsSnapshot, useRevertVpsSnapshot, useDeleteVpsSnapshot,
} from "@/hooks/use-vps-control";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed, errorMessage } from "@/components/common/LoadFailed";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { toast } from "sonner";
import { Trans, useTranslation } from "react-i18next";
import { fmtDateTime } from "@/i18n/format";

/** VPS 快照管理:OVH 免费档同时只允许 1 个,所以本 pane 只展示单快照 + 操作 */
export function VpsSnapshotPane({ serviceName }: { serviceName: string }) {
  const { t } = useTranslation();
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
        title={t("vps.snapshot.loadFailed")}
        error={snap.error}
        onRetry={() => snap.refetch()}
      />
    );
  }

  const handleCreate = async () => {
    try {
      await create.mutateAsync({ description: createDesc });
      toast.success(t("vps.snapshot.toast.created"));
      setCreateOpen(false);
      setCreateDesc("");
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };

  const handleRevert = async () => {
    if (revertConfirm !== serviceName) {
      toast.error(t("vps.snapshot.toast.nameMismatch"));
      return;
    }
    try {
      await revert.mutateAsync();
      toast.success(t("vps.snapshot.toast.reverted"));
      setRevertOpen(false);
      setRevertConfirm("");
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };

  const handleDelete = async () => {
    if (!confirm(t("vps.snapshot.deleteConfirm"))) return;
    try {
      await remove.mutateAsync();
      toast.success(t("vps.snapshot.toast.deleted"));
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };

  const handleEditDesc = async () => {
    try {
      await update.mutateAsync({ description: editDesc });
      toast.success(t("vps.snapshot.toast.descUpdated"));
      setEditOpen(false);
    } catch (e: any) {
      toast.error(errorMessage(e));
    }
  };

  if (!snap.data) {
    return (
      <div className="space-y-3">
        <div className="border border-border rounded-2xl p-6">
          <EmptyState
            icon={Camera}
            title={t("vps.snapshot.empty")}
            description={t("vps.snapshot.emptyDesc")}
          />
          <div className="flex justify-center mt-2">
            <Button onClick={() => setCreateOpen(true)} disabled={create.isPending}>
              <Plus className="w-4 h-4 mr-1" />
              {t("vps.snapshot.createBtn")}
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
          <h3 className="text-sm font-semibold">{t("vps.snapshot.currentTitle")}</h3>
          <code className="ml-auto text-[10px] font-mono text-muted-foreground">#{s.id}</code>
        </div>
        <div className="text-[12px] space-y-1">
          {s.description && <div className="font-medium">{s.description}</div>}
          <div className="text-muted-foreground">
            {t("vps.snapshot.creationDate", {
              time: s.creationDate ? fmtDateTime(s.creationDate) : "—",
            })}
          </div>
          {s.region && (
            <div className="text-muted-foreground">{t("vps.snapshot.region", { region: s.region })}</div>
          )}
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
            {t("vps.snapshot.editDescBtn")}
          </Button>
          <Button
            size="sm"
            variant="destructive"
            onClick={() => setRevertOpen(true)}
            disabled={revert.isPending}
          >
            <RotateCcw className="w-3.5 h-3.5 mr-1" />
            {t("vps.snapshot.revertBtn")}
          </Button>
          <Button size="sm" variant="outline" onClick={handleDelete} disabled={remove.isPending}>
            <Trash2 className="w-3.5 h-3.5 mr-1" />
            {t("vps.snapshot.deleteBtn")}
          </Button>
        </div>
      </div>

      <p className="text-[11px] text-muted-foreground px-1">
        {t("vps.snapshot.singleWarn")}
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
            <DialogTitle>{t("vps.snapshot.editTitle")}</DialogTitle>
          </DialogHeader>
          <Input
            value={editDesc}
            onChange={(e) => setEditDesc(e.target.value)}
            placeholder={t("vps.snapshot.descPlaceholder")}
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button onClick={handleEditDesc} disabled={update.isPending}>
              {update.isPending ? t("vps.snapshot.saving") : t("common.save")}
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
              {t("vps.snapshot.revertTitle")}
            </DialogTitle>
            <DialogDescription>{t("vps.snapshot.revertDesc")}</DialogDescription>
          </DialogHeader>
          <div className="border border-destructive/40 bg-destructive/5 rounded-xl p-3 space-y-1.5 text-[12px]">
            <p className="font-semibold text-destructive">{t("vps.snapshot.revertWarnTitle")}</p>
            <ul className="list-disc pl-5 text-muted-foreground space-y-0.5">
              <li>{t("vps.snapshot.revertItemFs", { time: fmtDateTime(s.creationDate) })}</li>
              <li>{t("vps.snapshot.revertItemReboot")}</li>
              <li>{t("vps.snapshot.revertItemMeta")}</li>
            </ul>
          </div>
          <div>
            <label className="text-[12px] block mb-1.5">
              <Trans
                i18nKey="vps.snapshot.confirmNameLabel"
                values={{ name: serviceName }}
                components={{ code: <code className="font-mono" /> }}
              />
            </label>
            <Input
              value={revertConfirm}
              onChange={(e) => setRevertConfirm(e.target.value)}
              placeholder={serviceName}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRevertOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={handleRevert}
              disabled={revert.isPending || revertConfirm !== serviceName}
            >
              {revert.isPending ? t("vps.snapshot.reverting") : t("vps.snapshot.revertConfirmBtn")}
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
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("vps.snapshot.createTitle")}</DialogTitle>
          <DialogDescription>{t("vps.snapshot.createDesc")}</DialogDescription>
        </DialogHeader>
        <Input
          value={desc}
          onChange={(e) => setDesc(e.target.value)}
          placeholder={t("vps.snapshot.createPlaceholder")}
        />
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={onConfirm} disabled={pending}>
            {pending ? t("vps.snapshot.creating") : t("vps.snapshot.createBtn")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
