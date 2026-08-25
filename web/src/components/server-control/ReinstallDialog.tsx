import { useEffect, useMemo, useState } from "react";
import { HardDrive, Search, AlertTriangle, Database, Plus, X as XIcon, Cog, Zap, RefreshCw, Loader2 } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import {
  useServerTemplates,
  useReinstallServer,
  useServerDiskInfo,
  useServerRaidProfiles,
  useServerPartitionSchemes,
  type CustomPartition,
} from "@/hooks/use-server-control";
import { OsIcon, detectOsKind, osBrandColor } from "@/components/server-control/OsIcon";
import { toast } from "sonner";

/** OS 分组的中文标签 + 显示顺序(按用户使用频率排) */
const OS_GROUPS: { kind: ReturnType<typeof detectOsKind>; label: string }[] = [
  { kind: "debian",   label: "Debian" },
  { kind: "ubuntu",   label: "Ubuntu" },
  { kind: "windows",  label: "Windows" },
  { kind: "proxmox",  label: "Proxmox VE" },
  { kind: "rocky",    label: "Rocky Linux" },
  { kind: "alma",     label: "AlmaLinux" },
  { kind: "fedora",   label: "Fedora" },
  { kind: "esxi",     label: "VMware ESXi" },
  { kind: "centos",   label: "CentOS" },
  { kind: "opensuse", label: "openSUSE" },
  { kind: "freebsd",  label: "FreeBSD" },
  { kind: "byoi",     label: "BYOI(镜像导入)" },
  { kind: "byolinux", label: "BYO Linux" },
  { kind: "linux",    label: "其他 Linux" },
];

const HARDWARE_RAID_LEVELS = [
  { value: "", label: "默认（无 RAID）" },
  { value: "raid0", label: "RAID 0 · 条带（最大容量，无冗余）" },
  { value: "raid1", label: "RAID 1 · 镜像（数据冗余）" },
  { value: "raid5", label: "RAID 5 · 分布式奇偶（平衡）" },
  { value: "raid6", label: "RAID 6 · 双重奇偶（高冗余）" },
  { value: "raid10", label: "RAID 10 · 镜像+条带（高性能+冗余）" },
];

const SOFTWARE_RAID_LEVELS = [
  { value: "raid0", label: "RAID 0 · 2+ 盘" },
  { value: "raid1", label: "RAID 1 · 2+ 盘（推荐）" },
  { value: "raid5", label: "RAID 5 · 3+ 盘" },
  { value: "raid6", label: "RAID 6 · 4+ 盘" },
  { value: "raid10", label: "RAID 10 · 4+ 盘" },
];

const FILESYSTEMS = ["ext4", "ext3", "xfs", "btrfs", "swap", "reiserfs"];

/**
 * 重装系统对话框（1:1 对齐旧前端）：
 * - OS 模板搜索 + 选择
 * - 自定义 Hostname
 * - Proxmox 9 + ZFS 高级配置（仅 proxmox9_64 显示）
 * - 高级存储配置：硬件 RAID（每磁盘组） + 软 RAID + 自定义分区
 * - 内置分区方案（templates 接口附带）
 * - Windows / 危险操作提示
 */
export function ReinstallDialog({
  serviceName,
  open,
  onOpenChange,
}: {
  serviceName: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const tpl = useServerTemplates(serviceName, open);
  const disk = useServerDiskInfo(serviceName, open);
  const raid = useServerRaidProfiles(serviceName, open);
  const mut = useReinstallServer();

  // 基本
  const [search, setSearch] = useState("");
  const [templateName, setTemplateName] = useState("");
  /** 当前展开的 OS 分组(左栏选中)。null = 没选,搜索时强制 null 让右栏展示扁平结果。 */
  const [activeGroup, setActiveGroup] = useState<ReturnType<typeof detectOsKind> | null>(null);
  const [hostname, setHostname] = useState("");

  /**
   * 存储配置模式。ZFS 预设 / 内置分区方案 / 高级存储配置三者写的是 reinstall 的同一个 storage 字段，
   * 后端同时收到两份"用户亲手填的存储配置"会直接 400，ZFS 与另两者撞车时也只会保留 ZFS 并回警告。
   * 与其让用户自己去猜哪个生效，不如在 UI 上做成单选：任何时候只有一种模式在起作用。
   *   default = 用模板默认分区 / zfs = Proxmox 9 + ZFS 预设 / scheme = 内置分区方案 / custom = 高级存储配置
   */
  const [storageMode, setStorageMode] = useState<"default" | "zfs" | "scheme" | "custom">("default");
  const [zfsRaidLevel, setZfsRaidLevel] = useState<0 | 1>(1);
  const [zfsVzSize, setZfsVzSize] = useState(100 * 1024); // MB

  // 高级存储
  const [hardwareRaid, setHardwareRaid] = useState<Record<number, string>>({});
  const [useSoftwareRaid, setUseSoftwareRaid] = useState(false);
  const [softwareRaidLevel, setSoftwareRaidLevel] = useState("raid1");
  const [customPartitions, setCustomPartitions] = useState<CustomPartition[]>([]);

  // 内置分区方案
  const ps = useServerPartitionSchemes(serviceName, templateName || null);
  const [partitionSchemeName, setPartitionSchemeName] = useState("");

  // 确认
  const [confirming, setConfirming] = useState(false);

  // 重置 selectedScheme 当模板变化时
  useEffect(() => {
    setPartitionSchemeName("");
  }, [templateName]);

  // 换模板时把存储模式收敛到合法值：ZFS 预设只对 proxmox9_64 有意义，
  // 换到别的模板还留在 zfs 模式会发出一份 OVH 不认的配置
  useEffect(() => {
    if (templateName !== "proxmox9_64") {
      setStorageMode((m) => (m === "zfs" ? "default" : m));
    } else {
      setStorageMode((m) => (m === "default" ? "zfs" : m));
    }
  }, [templateName]);

  // 已选模板对应的发行版自动展开到左栏(刷新 / 预设模板的场景)
  useEffect(() => {
    if (!templateName || activeGroup) return;
    const t = (tpl.data || []).find((x) => x.templateName === templateName);
    if (t) setActiveGroup(detectOsKind(t.templateName, t.distribution, t.family));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [templateName, tpl.data]);

  const filtered = useMemo(() => {
    const all = tpl.data || [];
    if (!search) return all;
    const s = search.toLowerCase();
    return all.filter(
      (t) => t.templateName.toLowerCase().includes(s) || t.distribution.toLowerCase().includes(s) || t.family.toLowerCase().includes(s)
    );
  }, [tpl.data, search]);

  // 按 OS kind 分组,组内按 templateName 排序;空组不显示
  const groupedTemplates = useMemo(() => {
    const buckets = new Map<string, typeof filtered>();
    for (const t of filtered) {
      const k = detectOsKind(t.templateName, t.distribution, t.family);
      const arr = buckets.get(k);
      if (arr) arr.push(t);
      else buckets.set(k, [t]);
    }
    for (const arr of buckets.values()) {
      arr.sort((a, b) => a.templateName.localeCompare(b.templateName));
    }
    return OS_GROUPS
      .filter((g) => buckets.has(g.kind))
      .map((g) => ({ ...g, items: buckets.get(g.kind)! }));
  }, [filtered]);

  const isProxmox9 = templateName === "proxmox9_64";
  const useProxmox9Zfs = isProxmox9 && storageMode === "zfs";
  const useCustomStorage = storageMode === "custom";

  /**
   * /var/lib/vz 的上限，口径必须跟后端一致，否则用户填了个前端放行、后端 400 的值。
   * 后端（server_control_basic.go 的 ZFS 分支）算法：
   *   RAID0 → 总容量 = 单盘容量 × 盘数；RAID1 等 → 总容量 = 单盘容量（镜像，可用只有一块）
   *   可用   = 总容量 × 1024 × 0.92（文件系统开销）
   *   root   = 可用 − /boot 1024MB − swap 8192MB − vz，必须 > 0
   * 这里再额外给根目录留 20GB，避免刚好卡在 root=1MB 这种能过校验但没法用的值。
   */
  const zfsCap = useMemo(() => {
    const groups = disk.data || {};
    const first = Object.values(groups)[0];
    if (!first?.disks?.length) return { singleDiskGB: 0, diskCount: 0, usableMB: 0, maxVzGB: 0 };
    const d = first.disks[0];
    const singleDiskGB = d.unit?.toLowerCase().startsWith("t") ? d.capacity * 1024 : d.capacity;
    const diskCount = first.disks.length;
    const totalGB = zfsRaidLevel === 0 ? singleDiskGB * diskCount : singleDiskGB;
    const usableMB = Math.floor(totalGB * 1024 * 0.92);
    const BOOT_MB = 1024;
    const SWAP_MB = 8192;
    const ROOT_RESERVE_MB = 20 * 1024;
    const maxVzGB = Math.max(10, Math.floor((usableMB - BOOT_MB - SWAP_MB - ROOT_RESERVE_MB) / 1024));
    return { singleDiskGB, diskCount, usableMB, maxVzGB };
  }, [disk.data, zfsRaidLevel]);
  // 单盘机器做不了镜像，后端会 400；这里提前提示，别等提交才发现
  const zfsRaid1Impossible = zfsRaidLevel !== 0 && zfsCap.diskCount > 0 && zfsCap.diskCount < 2;
  // 磁盘信息还没拉到时上限未知，此时不做钳制（宁可让后端兜底，也别把用户填的值改成 10GB）
  const vzMaxKnown = zfsCap.maxVzGB > 0 && zfsCap.diskCount > 0;
  const vzMaxGB = vzMaxKnown ? zfsCap.maxVzGB : Number.MAX_SAFE_INTEGER;

  // RAID0 → RAID1 时可用容量直接砍半，原来的 vz 值可能已经越界，跟着收一下
  useEffect(() => {
    if (!vzMaxKnown) return;
    setZfsVzSize((cur) => Math.min(cur, zfsCap.maxVzGB * 1024));
  }, [vzMaxKnown, zfsCap.maxVzGB]);

  const handleSubmit = async () => {
    if (!templateName) {
      toast.error("请选择系统模板");
      return;
    }
    // OVH 的 customizations.hostname 只接受合法主机名/FQDN，非法值会被 OVH 以英文错误码打回
    const hn = hostname.trim();
    if (hn && !/^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/.test(hn)) {
      toast.error("Hostname 只能包含字母、数字、连字符和点，且不能以连字符开头或结尾");
      return;
    }
    if (useProxmox9Zfs && zfsRaid1Impossible) {
      toast.error("该服务器只有 1 块磁盘，无法使用 ZFS RAID1，请改用 RAID0");
      return;
    }
    if (!confirming) {
      setConfirming(true);
      return;
    }
    try {
      // storageMode 是单选，所以这里每次最多只往下发一份存储配置，
      // 不会再出现"自定义存储配置与内置分区方案只能选择一种"那种 400
      const res = await mut.mutateAsync({
        serviceName,
        templateName,
        customHostname: hostname.trim() || undefined,
        useProxmox9Zfs,
        zfsRaidLevel: useProxmox9Zfs ? zfsRaidLevel : undefined,
        zfsVzSize: useProxmox9Zfs ? zfsVzSize : undefined,
        partitionSchemeName: storageMode === "scheme" && partitionSchemeName ? partitionSchemeName : undefined,
        hardwareRaid: useCustomStorage ? hardwareRaid : undefined,
        useSoftwareRaid: useCustomStorage && useSoftwareRaid,
        softwareRaidLevel: useCustomStorage && useSoftwareRaid ? softwareRaidLevel : undefined,
        customPartitions: useCustomStorage ? customPartitions : undefined,
        diskGroups: useCustomStorage ? disk.data : undefined,
      });
      toast.success("系统重装请求已发送");
      // 后端忽略了哪份配置必须让用户看见：装是装得成，但他填的东西没生效
      (res?.warnings || []).forEach((w) => toast.warning(w, { duration: 6000 }));
      onOpenChange(false);
      reset();
    } catch (e: any) {
      toast.error(e?.response?.data?.error || "重装失败");
    }
  };

  const reset = () => {
    setSearch("");
    setTemplateName("");
    setHostname("");
    setStorageMode("default");
    setZfsRaidLevel(1);
    setZfsVzSize(100 * 1024);
    setHardwareRaid({});
    setUseSoftwareRaid(false);
    setSoftwareRaidLevel("raid1");
    setCustomPartitions([]);
    setPartitionSchemeName("");
    setConfirming(false);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v);
        if (!v) reset();
      }}
    >
      <DialogContent className="w-[95vw] sm:w-full sm:max-w-4xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <HardDrive className="w-5 h-5 text-destructive" />
            重装系统
          </DialogTitle>
          <DialogDescription>选择要安装的操作系统模板。此操作将清空服务器所有数据。</DialogDescription>
        </DialogHeader>

        <div className="overflow-y-auto flex-1 -mx-6 px-6 space-y-5">
          {/* Windows 提示 */}
          <div className="border border-info/40 bg-info/5 rounded-2xl p-3 text-[12px] flex items-start gap-2">
            <Zap className="w-4 h-4 text-info mt-0.5 flex-shrink-0" />
            <div className="text-foreground/80 leading-relaxed space-y-1">
              <p>已解锁 Windows 后请刷新页面以获取最新模板列表。</p>
              <p>不熟悉 Windows 系统时，建议直接选 <span className="font-semibold">Windows Std</span> 系列。</p>
            </div>
          </div>

          {/* 模板搜索 */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <label className="text-[12px] font-semibold">操作系统模板</label>
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-[11px]"
                onClick={() => tpl.refetch()}
                disabled={tpl.isFetching}
                title="模板列表本地长期缓存，点击重新拉取最新版"
              >
                <RefreshCw className={`w-3 h-3 mr-1 ${tpl.isFetching ? "animate-spin" : ""}`} />
                刷新
              </Button>
            </div>
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
              <Input
                placeholder="搜索模板…如 ubuntu / debian / proxmox / windows"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-9"
              />
            </div>
            <p className="text-[11px] text-muted-foreground mt-1 mb-2">
              {search ? `找到 ${filtered.length} 个匹配模板` : `共 ${(tpl.data || []).length} 个模板`}
              {tpl.dataUpdatedAt > 0 && (
                <> · 缓存于 {new Date(tpl.dataUpdatedAt).toLocaleString("zh-CN")}</>
              )}
            </p>

            {tpl.isPending ? (
              // 跟正式列表同尺寸 + 同左右栏布局的骨架,中间转圈 + 文案,避免"白屏 5 秒不知道在干啥"
              <div className="border border-border rounded-2xl overflow-hidden grid grid-cols-[140px_1fr] sm:grid-cols-[180px_1fr] lg:grid-cols-[200px_1fr] h-[360px]">
                <div className="border-r border-border bg-muted/30 p-2 space-y-1.5">
                  {[0, 1, 2, 3, 4, 5].map((i) => (
                    <div key={i} className="flex items-center gap-2 px-2 py-1.5">
                      <Skeleton className="w-5 h-5 rounded-md flex-shrink-0" />
                      <Skeleton className="h-3 flex-1 rounded" />
                    </div>
                  ))}
                </div>
                <div className="flex flex-col items-center justify-center gap-3 text-muted-foreground px-4 text-center">
                  <Loader2 className="w-7 h-7 animate-spin text-foreground/60" />
                  <div className="space-y-1">
                    <p className="text-[13px] font-medium text-foreground">正在加载操作系统模板…</p>
                    <p className="text-[11px]">首次拉 OVH 需要 3-8 秒,之后会缓存</p>
                  </div>
                </div>
              </div>
            ) : filtered.length === 0 ? (
              <EmptyState icon={HardDrive} title="未找到匹配模板" />
            ) : (
              // 左右两栏:左 OS 分组列表,右 当前分组的模板。搜索时右栏自动平铺所有命中。
              <div className="border border-border rounded-2xl overflow-hidden grid grid-cols-[140px_1fr] sm:grid-cols-[180px_1fr] lg:grid-cols-[200px_1fr] h-[360px]">
                {/* 左栏:OS 分组 */}
                <div className="border-r border-border overflow-y-auto bg-muted/30">
                  {groupedTemplates.map((group) => {
                    const brandColor = osBrandColor(group.kind);
                    const active = activeGroup === group.kind;
                    return (
                      <button
                        key={group.kind}
                        type="button"
                        onClick={() => setActiveGroup(group.kind)}
                        className={`w-full flex items-center justify-between gap-2 pl-3 pr-3 py-2 text-left transition-colors border-b border-border/60 last:border-b-0 ${
                          active ? "bg-background" : "hover:bg-background/60"
                        }`}
                        style={active ? { boxShadow: `inset 3px 0 0 0 ${brandColor}` } : undefined}
                      >
                        <div className="flex items-center gap-2.5 min-w-0">
                          <OsIcon
                            templateName={group.items[0].templateName}
                            distribution={group.items[0].distribution}
                            family={group.items[0].family}
                            size={20}
                          />
                          <span className={`text-[13px] truncate ${active ? "font-semibold text-foreground" : "text-foreground/80"}`}>
                            {group.label}
                          </span>
                        </div>
                        <span
                          className="text-[10px] font-semibold px-1.5 py-0.5 rounded-full flex-shrink-0"
                          style={{ backgroundColor: brandColor + "22", color: brandColor }}
                        >
                          {group.items.length}
                        </span>
                      </button>
                    );
                  })}
                </div>

                {/* 右栏:当前分组的模板列表 */}
                <div className="overflow-y-auto">
                  {(() => {
                    // 搜索时优先平铺命中结果;否则只显示选中分组的模板
                    const showFlat = !!search;
                    const items = showFlat
                      ? filtered
                      : (groupedTemplates.find((g) => g.kind === activeGroup)?.items || []);
                    if (!showFlat && !activeGroup) {
                      return (
                        <div className="h-full flex items-center justify-center text-[12px] text-muted-foreground px-6 text-center">
                          ← 左侧选择一个发行版
                        </div>
                      );
                    }
                    if (items.length === 0) {
                      return (
                        <div className="h-full flex items-center justify-center text-[12px] text-muted-foreground">
                          该分组下没有模板
                        </div>
                      );
                    }
                    return (
                      <div className="divide-y divide-border/60">
                        {items.map((t) => {
                          const selected = templateName === t.templateName;
                          const kind = detectOsKind(t.templateName, t.distribution, t.family);
                          const brandColor = osBrandColor(kind);
                          return (
                            <button
                              key={t.templateName}
                              type="button"
                              onClick={() => setTemplateName(t.templateName)}
                              className={`w-full text-left px-4 py-2.5 hover:bg-secondary/50 transition-colors flex items-center gap-3 ${
                                selected ? "bg-secondary" : ""
                              }`}
                            >
                              <OsIcon
                                templateName={t.templateName}
                                distribution={t.distribution}
                                family={t.family}
                                size={24}
                              />
                              <div className="flex-1 min-w-0">
                                <div className="text-[13px] font-mono font-semibold truncate">{t.templateName}</div>
                                <div className="text-[11px] text-muted-foreground truncate">
                                  {t.distribution} · {t.family} · {t.bitFormat}-bit
                                </div>
                              </div>
                              {selected && (
                                <span
                                  className="text-[10px] font-semibold px-2 py-0.5 rounded-full flex-shrink-0"
                                  style={{ backgroundColor: brandColor, color: "#fff" }}
                                >
                                  已选
                                </span>
                              )}
                            </button>
                          );
                        })}
                      </div>
                    );
                  })()}
                </div>
              </div>
            )}
          </div>

          {/* 存储配置模式（三者互斥，见 storageMode 注释） */}
          <div className="border border-border rounded-2xl p-4 space-y-2">
            <div className="flex items-center gap-2">
              <Cog className="w-4 h-4 text-muted-foreground" />
              <h4 className="text-[13px] font-semibold">存储配置</h4>
            </div>
            <p className="text-[11px] text-muted-foreground">
              以下四选一。ZFS 预设、内置分区方案、高级存储配置写的是同一份配置，OVH 只接受其中一种。
            </p>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-1.5 text-[12px]">
              <StorageModeOption
                checked={storageMode === "default"}
                onSelect={() => setStorageMode("default")}
                label="使用模板默认分区"
                hint="最省事，OVH 按模板推荐布局装"
              />
              {isProxmox9 && (
                <StorageModeOption
                  checked={storageMode === "zfs"}
                  onSelect={() => setStorageMode("zfs")}
                  label="Proxmox 9 + ZFS 预设（推荐）"
                  hint="ZFS 根文件系统 + 独立 /var/lib/vz"
                />
              )}
              <StorageModeOption
                checked={storageMode === "scheme"}
                onSelect={() => setStorageMode("scheme")}
                label="内置分区方案"
                hint={templateName ? "选用该模板自带的分区方案" : "先选一个系统模板"}
                disabled={!templateName}
              />
              <StorageModeOption
                checked={storageMode === "custom"}
                onSelect={() => setStorageMode("custom")}
                label="高级存储配置"
                hint="硬件 / 软 RAID + 自定义分区"
              />
            </div>
          </div>

          {/* Proxmox 9 ZFS 配置 */}
          {isProxmox9 && storageMode === "zfs" && (
            <div className="border border-success/40 bg-success/5 rounded-2xl p-4 space-y-3">
              <div className="flex items-center gap-2">
                <Database className="w-4 h-4 text-success" />
                <h4 className="text-[13px] font-semibold">Proxmox VE 9 + ZFS 根文件系统</h4>
              </div>
              <p className="text-[11px] text-muted-foreground">
                使用 ZFS 作为根文件系统，提供快照、压缩、数据完整性检查等高级功能。
              </p>

              <div className="space-y-3">
                  <div>
                    <label className="block text-[12px] mb-1.5">RAID 级别</label>
                    <div className="flex gap-4 text-[13px]">
                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="radio"
                          checked={zfsRaidLevel === 1}
                          onChange={() => setZfsRaidLevel(1)}
                          className="w-4 h-4"
                        />
                        RAID1（镜像，冗余）
                      </label>
                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="radio"
                          checked={zfsRaidLevel === 0}
                          onChange={() => setZfsRaidLevel(0)}
                          className="w-4 h-4"
                        />
                        RAID0（条带，最大容量）
                      </label>
                    </div>
                  </div>
                  {zfsRaid1Impossible && (
                    <p className="text-[11px] text-destructive">
                      该服务器只检测到 1 块磁盘，无法做镜像，请改用 RAID0。
                    </p>
                  )}
                  <div>
                    <label className="block text-[12px] mb-1.5">/var/lib/vz 大小（GB）· VM/容器存储</label>
                    <Input
                      type="number"
                      min={10}
                      max={vzMaxKnown ? zfsCap.maxVzGB : undefined}
                      value={Math.floor(zfsVzSize / 1024)}
                      onChange={(e) => {
                        const v = Math.max(10, Math.min(vzMaxGB, parseInt(e.target.value) || 100));
                        setZfsVzSize(v * 1024);
                      }}
                      className="w-40"
                    />
                    {/* 上限跟后端同口径：RAID1 只算单盘容量，且要扣 /boot + swap + 根目录预留 */}
                    <p className="text-[11px] text-muted-foreground mt-1">
                      剩余分给根目录（/），最大 {vzMaxKnown ? `${zfsCap.maxVzGB} GB` : "未知（磁盘信息读取中）"}
                      {zfsCap.singleDiskGB > 0 && (
                        <>
                          {" "}
                          （{zfsCap.diskCount} × {zfsCap.singleDiskGB}GB，RAID{zfsRaidLevel} 下实际可用约{" "}
                          {Math.floor(zfsCap.usableMB / 1024)}GB，已扣除 /boot 1GB 与 swap 8GB）
                        </>
                      )}
                    </p>
                  </div>
                </div>
            </div>
          )}

          {/* 自定义 Hostname */}
          <div>
            <label className="block text-[12px] font-semibold mb-1.5">自定义 Hostname（可选）</label>
            <Input
              placeholder="如 server1.example.com"
              value={hostname}
              onChange={(e) => setHostname(e.target.value)}
            />
          </div>

          {/* 内置分区方案（仅 scheme 模式） */}
          {storageMode === "scheme" && templateName && (
            <div>
              <label className="block text-[12px] font-semibold mb-1.5">内置分区方案</label>
              {ps.isPending ? (
                <Skeleton className="h-9 rounded-md" />
              ) : (ps.data || []).length === 0 ? (
                <p className="text-[12px] text-muted-foreground">该模板没有内置分区方案，请改用其它存储模式。</p>
              ) : (
              <Select value={partitionSchemeName} onValueChange={setPartitionSchemeName}>
                <SelectTrigger>
                  <SelectValue placeholder="使用模板默认分区" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value=" ">使用模板默认分区</SelectItem>
                  {(ps.data || []).map((s) => (
                    <SelectItem key={s.name} value={s.name}>
                      {s.name} · 优先级 {s.priority}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              )}
            </div>
          )}

          {/* 高级存储配置 */}
          <div className="border-t border-border pt-4">
            {useCustomStorage && (
              <div className="flex items-center gap-2 text-[13px] font-semibold mb-3">
                <Cog className="w-4 h-4" />
                高级存储配置（RAID & 自定义分区）
              </div>
            )}

            {useCustomStorage && (
              <div className="space-y-4 border border-border rounded-2xl p-4 bg-secondary/30">
                {/* 磁盘组 + 硬件 RAID */}
                {disk.isPending ? (
                  <Skeleton className="h-20 rounded-md" />
                ) : Object.keys(disk.data || {}).length === 0 ? (
                  <p className="text-[12px] text-muted-foreground">未检测到磁盘组信息</p>
                ) : (
                  <div className="space-y-3">
                    <h4 className="text-[12px] font-semibold">磁盘组配置</h4>
                    {Object.entries(disk.data || {}).map(([gidStr, group]) => {
                      const gid = parseInt(gidStr);
                      return (
                        <div key={gid} className="border border-border rounded-xl p-3 space-y-2 bg-background">
                          <div className="flex items-center gap-2 text-[12px]">
                            <HardDrive className="w-3.5 h-3.5 text-muted-foreground" />
                            <span className="font-semibold">磁盘组 {gid}</span>
                            {group.raidController && (
                              <span className="text-[10px] px-1.5 py-0.5 rounded-full border border-border">
                                {group.raidController}
                              </span>
                            )}
                          </div>
                          <div className="grid grid-cols-2 gap-1.5 text-[11px] text-muted-foreground">
                            {group.disks.map((d, idx) => (
                              <div key={idx} className="flex items-center gap-1.5">
                                <span className="w-1.5 h-1.5 rounded-full bg-foreground/40" />
                                {d.capacity}
                                {d.unit} {d.technology || ""} {d.interface || ""}
                              </div>
                            ))}
                          </div>
                          <div>
                            <label className="block text-[11px] text-muted-foreground mb-1">硬件 RAID 模式</label>
                            {!raid.data?.supported ? (
                              <p className="text-[11px] text-warning">
                                此服务器不支持硬件 RAID，可改用下方"软 RAID"。
                              </p>
                            ) : (
                              <Select
                                value={hardwareRaid[gid] || ""}
                                onValueChange={(v) => setHardwareRaid({ ...hardwareRaid, [gid]: v === " " ? "" : v })}
                              >
                                <SelectTrigger className="h-9">
                                  <SelectValue placeholder="默认（无 RAID）" />
                                </SelectTrigger>
                                <SelectContent>
                                  {HARDWARE_RAID_LEVELS.map((l) => (
                                    <SelectItem key={l.value || "none"} value={l.value || " "}>
                                      {l.label}
                                    </SelectItem>
                                  ))}
                                </SelectContent>
                              </Select>
                            )}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                )}

                {/* 软 RAID */}
                <div className="border-t border-border pt-3">
                  <label className="flex items-center gap-2 cursor-pointer text-[12px] font-semibold mb-2">
                    <input
                      type="checkbox"
                      checked={useSoftwareRaid}
                      onChange={(e) => setUseSoftwareRaid(e.target.checked)}
                      className="w-4 h-4"
                    />
                    <HardDrive className="w-3.5 h-3.5" />
                    使用软 RAID（Software RAID）
                  </label>
                  {useSoftwareRaid && (
                    <div className="pl-6 space-y-2">
                      <Select value={softwareRaidLevel} onValueChange={setSoftwareRaidLevel}>
                        <SelectTrigger className="h-9">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {SOFTWARE_RAID_LEVELS.map((l) => (
                            <SelectItem key={l.value} value={l.value}>
                              {l.label}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <p className="text-[11px] text-muted-foreground">
                        软 RAID 由 Linux mdadm 管理，不需要硬件 RAID 控制器。所有磁盘将自动加入软 RAID 阵列。
                      </p>
                    </div>
                  )}
                </div>

                {/* 自定义分区 */}
                <div className="border-t border-border pt-3">
                  <div className="flex items-center justify-between mb-2">
                    <h4 className="text-[12px] font-semibold">自定义分区方案（可选）</h4>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={() =>
                        setCustomPartitions([
                          ...customPartitions,
                          {
                            mountpoint: "/",
                            filesystem: "ext4",
                            size: 0,
                            order: customPartitions.length + 1,
                            type: "primary",
                            raid: useSoftwareRaid ? softwareRaidLevel : undefined,
                            // 不预设磁盘组：编号从 1 起（官方分区文档），0 不是合法组。
                            // 只有一个组时也不用选，留 undefined 让 OVH 用默认组。
                            diskGroupId: undefined,
                          },
                        ])
                      }
                    >
                      <Plus className="w-3.5 h-3.5 mr-1" />
                      添加分区
                    </Button>
                  </div>
                  <p className="text-[11px] text-muted-foreground mb-2">留空则使用默认分区。size=0 表示剩余空间。</p>
                  {customPartitions.length > 0 && (
                    <div className="space-y-2">
                      {customPartitions.map((p, idx) => (
                        <PartitionRow
                          key={idx}
                          partition={p}
                          diskGroupIds={Object.keys(disk.data || {}).map((s) => parseInt(s))}
                          onChange={(np) => {
                            const next = [...customPartitions];
                            next[idx] = np;
                            setCustomPartitions(next);
                          }}
                          onRemove={() => setCustomPartitions(customPartitions.filter((_, i) => i !== idx))}
                        />
                      ))}
                    </div>
                  )}
                </div>
              </div>
            )}
          </div>

          {/* 危险提示 */}
          {confirming && (
            <div className="border border-destructive/40 bg-destructive/5 rounded-2xl p-3 text-[12px] flex items-start gap-2">
              <AlertTriangle className="w-4 h-4 text-destructive mt-0.5 flex-shrink-0" />
              <div className="leading-relaxed">
                确认后服务器将立即开始重装，<span className="font-semibold">所有数据将被清空</span>。再次点击"确认重装"提交。
              </div>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={handleSubmit} disabled={!templateName || mut.isPending}>
            {mut.isPending ? "提交中…" : confirming ? "确认重装（不可逆）" : "下一步"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 存储模式单选项。用 radio 而不是多个 checkbox，UI 上就杜绝了"同时选两种"这条会被后端 400 的路径 */
function StorageModeOption({
  checked,
  onSelect,
  label,
  hint,
  disabled,
}: {
  checked: boolean;
  onSelect: () => void;
  label: string;
  hint?: string;
  disabled?: boolean;
}) {
  return (
    <label
      className={
        "flex items-start gap-2 rounded-xl border px-3 py-2 cursor-pointer transition-colors " +
        (checked ? "border-foreground bg-secondary/50 " : "border-border hover:bg-secondary/30 ") +
        (disabled ? "opacity-50 cursor-not-allowed" : "")
      }
    >
      <input
        type="radio"
        checked={checked}
        onChange={() => !disabled && onSelect()}
        disabled={disabled}
        className="w-4 h-4 mt-0.5"
      />
      <span className="min-w-0">
        <span className="block font-semibold">{label}</span>
        {hint && <span className="block text-[11px] text-muted-foreground">{hint}</span>}
      </span>
    </label>
  );
}

/** 单行自定义分区编辑（mountpoint / filesystem / size / 磁盘组 / RAID） */
function PartitionRow({
  partition,
  diskGroupIds,
  onChange,
  onRemove,
}: {
  partition: CustomPartition;
  diskGroupIds: number[];
  onChange: (p: CustomPartition) => void;
  onRemove: () => void;
}) {
  return (
    <div className="border border-border rounded-xl p-2.5 flex items-center gap-2 text-[12px] bg-background flex-wrap">
      <Input
        value={partition.mountpoint}
        onChange={(e) => onChange({ ...partition, mountpoint: e.target.value })}
        placeholder="挂载点"
        className="h-8 w-32"
      />
      <Select value={partition.filesystem} onValueChange={(v) => onChange({ ...partition, filesystem: v })}>
        <SelectTrigger className="h-8 w-24">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {FILESYSTEMS.map((fs) => (
            <SelectItem key={fs} value={fs}>
              {fs}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Input
        type="number"
        min={0}
        value={partition.size}
        onChange={(e) => onChange({ ...partition, size: parseInt(e.target.value) || 0 })}
        placeholder="MB"
        className="h-8 w-24"
        title="size=0 表示剩余空间"
      />
      <span className="text-[11px] text-muted-foreground">MB</span>
      {diskGroupIds.length > 1 && (
        <Select
          value={String(partition.diskGroupId ?? "")}
          onValueChange={(v) => onChange({ ...partition, diskGroupId: parseInt(v) })}
        >
          <SelectTrigger className="h-8 w-24">
            <SelectValue placeholder="磁盘组" />
          </SelectTrigger>
          <SelectContent>
            {diskGroupIds.map((gid) => (
              <SelectItem key={gid} value={String(gid)}>
                磁盘组 {gid}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      <Select
        value={partition.raid || ""}
        onValueChange={(v) => onChange({ ...partition, raid: v === " " ? undefined : v })}
      >
        <SelectTrigger className="h-8 w-28">
          <SelectValue placeholder="无 RAID" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value=" ">无 RAID</SelectItem>
          {SOFTWARE_RAID_LEVELS.map((l) => (
            <SelectItem key={l.value} value={l.value}>
              {l.value.toUpperCase()}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button type="button" variant="outline" size="sm" onClick={onRemove} className="ml-auto h-8 w-8 p-0">
        <XIcon className="w-3.5 h-3.5" />
      </Button>
    </div>
  );
}
