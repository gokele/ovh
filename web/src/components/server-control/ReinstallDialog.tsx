import { useEffect, useMemo, useState } from "react";
import { HardDrive, Search, AlertTriangle, Database, Plus, X as XIcon, Cog, Zap, RefreshCw, Loader2, Wand2, Terminal } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/common/Skeleton";
import { EmptyState } from "@/components/common/EmptyState";
import { LoadFailed, errorMessage } from "@/components/common/LoadFailed";
import {
  useServerTemplates,
  useReinstallServer,
  useServerDiskInfo,
  useServerRaidProfiles,
  useServerPartitionSchemes,
  type CustomPartition,
} from "@/hooks/use-server-control";
import { OsIcon, detectOsKind, osBrandColor, osBrandTextColor } from "@/components/server-control/OsIcon";
import { useTheme } from "@/hooks/use-theme";
import { toast } from "sonner";
import { buildSmartPlan } from "@/lib/smart-storage";
import { Trans, useTranslation } from "react-i18next";
import { fmtDateTime } from "@/i18n/format";

/** OS 分组(标签走 i18n key,渲染处 t())+ 显示顺序(按用户使用频率排) */
const OS_GROUPS: { kind: ReturnType<typeof detectOsKind>; label: string }[] = [
  { kind: "debian",   label: "ctrl.reinstall.os.debian" },
  { kind: "ubuntu",   label: "ctrl.reinstall.os.ubuntu" },
  { kind: "windows",  label: "ctrl.reinstall.os.windows" },
  { kind: "proxmox",  label: "ctrl.reinstall.os.proxmox" },
  { kind: "rocky",    label: "ctrl.reinstall.os.rocky" },
  { kind: "alma",     label: "ctrl.reinstall.os.alma" },
  { kind: "fedora",   label: "ctrl.reinstall.os.fedora" },
  { kind: "esxi",     label: "ctrl.reinstall.os.esxi" },
  { kind: "centos",   label: "ctrl.reinstall.os.centos" },
  { kind: "opensuse", label: "ctrl.reinstall.os.opensuse" },
  { kind: "freebsd",  label: "ctrl.reinstall.os.freebsd" },
  { kind: "byoi",     label: "ctrl.reinstall.os.byoi" },
  { kind: "byolinux", label: "ctrl.reinstall.os.byolinux" },
  { kind: "linux",    label: "ctrl.reinstall.os.linux" },
];

const HARDWARE_RAID_LEVELS = [
  { value: "", label: "ctrl.reinstall.hwRaid.none" },
  { value: "raid0", label: "ctrl.reinstall.hwRaid.raid0" },
  { value: "raid1", label: "ctrl.reinstall.hwRaid.raid1" },
  { value: "raid5", label: "ctrl.reinstall.hwRaid.raid5" },
  { value: "raid6", label: "ctrl.reinstall.hwRaid.raid6" },
  { value: "raid10", label: "ctrl.reinstall.hwRaid.raid10" },
];

// schema: dedicated.server.reinstall.storage.partitioning.layout.RaidLevelEnum
// = [0,1,5,6,7,10]，三区一致。以前少了 raid7（后端一直支持），用户在界面上够不到。
const SOFTWARE_RAID_LEVELS = [
  { value: "raid0", label: "ctrl.reinstall.swRaid.raid0" },
  { value: "raid1", label: "ctrl.reinstall.swRaid.raid1" },
  { value: "raid5", label: "ctrl.reinstall.swRaid.raid5" },
  { value: "raid6", label: "ctrl.reinstall.swRaid.raid6" },
  { value: "raid7", label: "ctrl.reinstall.swRaid.raid7" },
  { value: "raid10", label: "ctrl.reinstall.swRaid.raid10" },
];

// schema: dedicated.server.reinstall.storage.partitioning.layout.FileSystemEnum
// 共 14 个值，三区一致，后端 reinstallFileSystems 全部支持。
// 以前前端只列了 6 个，ZFS/NTFS/VMFS 等在自定义分区里完全选不到。
// 注意文件系统和 RAID 级别有兼容矩阵（后端 checkFSRaidCompat 按官方分区文档校验），
// 选了不兼容的组合会在提交时被挡下并说明原因。
const FILESYSTEMS = [
  "ext4",
  "ext3",
  "xfs",
  "btrfs",
  "zfs",
  "swap",
  "reiserfs",
  "ntfs",
  "fat16",
  "ufs",
  "vmfs5",
  "vmfs6",
  "vmfsl",
  "none",
];

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
  const { t } = useTranslation();
  // 品牌色当文字色在深色底上要提亮(见 osBrandTextColor 注释)
  const brandDark = useTheme().resolved === "dark";

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
  /** 官方 partitioning.disks:软 RAID 只用前 N 块盘。空串 = 全部盘(官方默认) */
  const [softwareRaidDisks, setSoftwareRaidDisks] = useState("");
  /** 官方 hardwareRaid.arrays:RAID10 的阵列数(12 盘 4 arrays = 4×RAID1 再组 RAID0) */
  const [hwRaidArrays, setHwRaidArrays] = useState("");
  /** 官方 hardwareRaid.spares:热备盘数 */
  const [hwRaidSpares, setHwRaidSpares] = useState("");
  /** 官方 Data erasure:勾选的盘组重装后保留数据(erase:false,只允许非安装组) */
  const [keepDiskGroups, setKeepDiskGroups] = useState<Set<number>>(new Set());
  /** OS 特定定制答案(官方 customizeQuestions 动态渲染) */
  const [customizationAnswers, setCustomizationAnswers] = useState<Record<string, string | boolean>>({});
  const [customPartitions, setCustomPartitions] = useState<CustomPartition[]>([]);
  const [showSmart, setShowSmart] = useState(false);

  // 内置分区方案
  const ps = useServerPartitionSchemes(serviceName, templateName || null);
  // 智能配置方案:随磁盘信息和所选系统变化。纯函数,见 lib/smart-storage.ts
  const smartPlan = useMemo(
    () => buildSmartPlan(disk.data || {}, detectOsKind(templateName)),
    [disk.data, templateName]
  );
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

  // 官方 installationTemplate 元数据(拉不到时退回全集,不阻塞普通流程)
  const selectedTpl = useMemo(
    () => (tpl.data || []).find((x) => x.templateName === templateName),
    [tpl.data, templateName]
  );
  const osFilesystems = selectedTpl?.filesystems;
  const lvmReady = selectedTpl?.lvmReady !== false; // 缺省按可用
  const noPartitioning = selectedTpl?.noPartitioning === true;
  const softRaidOnlyMirroring = selectedTpl?.softRaidOnlyMirroring === true;
  const customizeQuestions = selectedTpl?.customizeQuestions || [];
  // 官方兼容表:esxi 等不支持自定义分区的模板,存储配置整体不可用
  useEffect(() => {
    if (noPartitioning && storageMode !== "default") setStorageMode("default");
  }, [noPartitioning, storageMode]);

  /**
   * 挡住提交的读失败清单。
   *
   * 重装不可逆:数据全清、装完才发现装错就只能再装一次。这个对话框里每一块"看起来像事实"的
   * 文案背后都是一次请求 —— 请求挂了但界面照旧写「不支持硬件 RAID」「该模板没有内置分区方案」
   * 「未检测到磁盘组信息」,用户就会在信息缺失的情况下改方案、然后按下确认。
   * 所以只要当前存储模式真正依赖的那几项没读到,就锁住按钮,让他先重试。
   *
   * 只挑相关项,不搞"任何一处失败全锁死":
   *   - 模板列表:任何模式都要用(而且它带 localStorage 缓存,失败时屏幕上那份可能是旧的)
   *   - 磁盘组  :高级存储配置要按磁盘组配 RAID / 分区;ZFS 预设要拿它算 /var/lib/vz 上限
   *   - 硬件 RAID 支持情况:高级存储配置里决定"能不能选硬件 RAID"
   *   - 内置分区方案:scheme 模式下唯一的选项来源
   */
  const blockingErrors: { label: string; error: unknown; retry: () => void }[] = [];
  if (tpl.isError) {
    blockingErrors.push({ label: "ctrl.reinstall.dep.templates", error: tpl.error, retry: () => tpl.refetch() });
  }
  if ((useCustomStorage || useProxmox9Zfs) && disk.isError) {
    blockingErrors.push({ label: "ctrl.reinstall.dep.diskGroups", error: disk.error, retry: () => disk.refetch() });
  }
  if (useCustomStorage && raid.isError) {
    blockingErrors.push({ label: "ctrl.reinstall.dep.hwRaid", error: raid.error, retry: () => raid.refetch() });
  }
  if (storageMode === "scheme" && ps.isError) {
    blockingErrors.push({ label: "ctrl.reinstall.dep.schemes", error: ps.error, retry: () => ps.refetch() });
  }
  const blocked = blockingErrors.length > 0;
  /** 读失败清单的本地化拼接(重装按钮 title 和提交拦截 toast 共用) */
  const failedList = blocked
    ? blockingErrors.map((b) => t(b.label)).join(t("ctrl.reinstall.listSep"))
    : "";

  // 一旦进入 blocked,把"已点过下一步"的确认态收回。
  // 否则重试成功的那一刻按钮直接停在「确认重装（不可逆）」上,用户随手一点就提交了 ——
  // 而中间界面上的数据换过一轮,他并不知道自己确认的还是不是原来那份配置。
  useEffect(() => {
    if (blocked) setConfirming(false);
  }, [blocked]);

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
      toast.error(t("ctrl.reinstall.toast.selectTemplate"));
      return;
    }
    // 兜底:按钮已经 disabled,但状态可能在点击那一刻才翻成 error
    if (blocked) {
      setConfirming(false);
      toast.error(t("ctrl.reinstall.toast.blockedRetry", { list: failedList }));
      return;
    }
    // OVH 的 customizations.hostname 只接受合法主机名/FQDN，非法值会被 OVH 以英文错误码打回
    const hn = hostname.trim();
    if (hn && !/^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/.test(hn)) {
      toast.error(t("ctrl.reinstall.toast.hostnameInvalid"));
      return;
    }
    if (useProxmox9Zfs && zfsRaid1Impossible) {
      toast.error(t("ctrl.reinstall.toast.zfsRaid1Impossible"));
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
        // 官方 customizeQuestions 的答案原样回传(空值由 mutation 过滤)
        customizations: Object.keys(customizationAnswers).length > 0 ? customizationAnswers : undefined,
        useProxmox9Zfs,
        zfsRaidLevel: useProxmox9Zfs ? zfsRaidLevel : undefined,
        zfsVzSize: useProxmox9Zfs ? zfsVzSize : undefined,
        partitionSchemeName: storageMode === "scheme" && partitionSchemeName ? partitionSchemeName : undefined,
        hardwareRaid: useCustomStorage ? hardwareRaid : undefined,
        useSoftwareRaid: useCustomStorage && useSoftwareRaid,
        softwareRaidLevel: useCustomStorage && useSoftwareRaid ? softwareRaidLevel : undefined,
        softwareRaidDisks:
          useCustomStorage && useSoftwareRaid && softwareRaidDisks ? parseInt(softwareRaidDisks) : undefined,
        hwRaidArrays:
          useCustomStorage && hwRaidArrays ? parseInt(hwRaidArrays) : undefined,
        hwRaidSpares:
          useCustomStorage && hwRaidSpares ? parseInt(hwRaidSpares) : undefined,
        keepDiskGroups:
          useCustomStorage && keepDiskGroups.size > 0 ? Array.from(keepDiskGroups) : undefined,
        customPartitions: useCustomStorage ? customPartitions : undefined,
        diskGroups: useCustomStorage ? disk.data : undefined,
      });
      toast.success(t("ctrl.reinstall.toast.submitted"));
      // 后端忽略了哪份配置必须让用户看见：装是装得成，但他填的东西没生效
      (res?.warnings || []).forEach((w) => toast.warning(w, { duration: 6000 }));
      onOpenChange(false);
      reset();
    } catch (e: any) {
      toast.error(errorMessage(e));
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
    setSoftwareRaidDisks("");
    setHwRaidArrays("");
    setHwRaidSpares("");
    setKeepDiskGroups(new Set());
    setCustomizationAnswers({});
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
            {t("ctrl.reinstall.title")}
          </DialogTitle>
          <DialogDescription>{t("ctrl.reinstall.desc")}</DialogDescription>
        </DialogHeader>

        <div className="overflow-y-auto flex-1 -mx-6 px-6 space-y-5">
          {/* 读失败总览 —— 有任何一条就锁住提交,见 blockingErrors 注释 */}
          {blocked && (
            <div className="border border-destructive/40 bg-destructive/5 rounded-2xl p-3 space-y-2">
              <div className="flex items-start gap-2 text-[12px]">
                <AlertTriangle className="w-4 h-4 text-destructive mt-0.5 flex-shrink-0" />
                <div className="leading-relaxed">
                  <p className="font-semibold">{t("ctrl.reinstall.blockedTitle")}</p>
                  <p className="text-muted-foreground">{t("ctrl.reinstall.blockedDesc")}</p>
                </div>
              </div>
              <div className="space-y-1.5 pl-6">
                {blockingErrors.map((b) => (
                  <div key={b.label} className="flex items-start justify-between gap-2 text-[11px]">
                    <span className="min-w-0">
                      <span className="font-semibold">{t("ctrl.reinstall.depFailed", { what: t(b.label) })}</span>
                      <span className="text-muted-foreground"> · {errorMessage(b.error)}</span>
                    </span>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      className="h-6 px-2 text-[11px] flex-shrink-0"
                      onClick={b.retry}
                    >
                      {t("common.retry")}
                    </Button>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Windows 提示 */}
          <div className="border border-info/40 bg-info/5 rounded-2xl p-3 text-[12px] flex items-start gap-2">
            <Zap className="w-4 h-4 text-info mt-0.5 flex-shrink-0" />
            <div className="text-foreground/80 leading-relaxed space-y-1">
              <p>{t("ctrl.reinstall.winHintRefresh")}</p>
              <p>
                <Trans i18nKey="ctrl.reinstall.winHintStd" components={{ b: <span className="font-semibold" /> }} />
              </p>
            </div>
          </div>

          {/* 模板搜索 */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <label className="text-[12px] font-semibold">{t("ctrl.reinstall.tplLabel")}</label>
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-[11px]"
                onClick={() => tpl.refetch()}
                disabled={tpl.isFetching}
                title={t("ctrl.reinstall.refreshTitle")}
              >
                <RefreshCw className={`w-3 h-3 mr-1 ${tpl.isFetching ? "animate-spin" : ""}`} />
                {t("common.refresh")}
              </Button>
            </div>
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
              <Input
                placeholder={t("ctrl.reinstall.searchPlaceholder")}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-9"
              />
            </div>
            {/* 「共 N 个模板」是个断言。拉失败又没有本地缓存时它会显示「共 0 个」,
                等于告诉用户这台机器一个系统都装不了。 */}
            <p className="text-[11px] text-muted-foreground mt-1 mb-2">
              {tpl.isError && (tpl.data || []).length === 0
                ? t("ctrl.reinstall.countUnknown")
                : search
                  ? t("ctrl.reinstall.countMatched", { n: filtered.length })
                  : t("ctrl.reinstall.countTotal", { n: (tpl.data || []).length })}
              {tpl.dataUpdatedAt > 0 && (
                <> · {t("ctrl.reinstall.cachedAt", { time: fmtDateTime(tpl.dataUpdatedAt) })}</>
              )}
            </p>

            {/* 这个 query 带 localStorage initialData:刷新失败时屏幕上仍会留着上次的模板列表,
                连"共 N 个"都是旧的。不标出来的话,OVH 那边已经下架 / 新增过的模板用户完全看不出来,
                挑一个早已不存在的模板去重装,只会在提交时被 OVH 打回(甚至装成别的版本)。 */}
            {tpl.isError && (tpl.data || []).length > 0 && (
              <div className="flex items-start gap-2 rounded-xl border border-warning/40 bg-warning/5 px-3 py-2 text-[11px] mb-2">
                <AlertTriangle className="w-3.5 h-3.5 text-warning flex-shrink-0 mt-0.5" />
                <p className="leading-relaxed text-foreground/80">
                  <Trans
                    i18nKey="ctrl.reinstall.staleCacheWarn"
                    values={{ err: errorMessage(tpl.error) }}
                    components={{ b: <span className="font-semibold" /> }}
                  />
                </p>
              </div>
            )}

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
                    <p className="text-[13px] font-medium text-foreground">{t("ctrl.reinstall.loadingTitle")}</p>
                    <p className="text-[11px]">{t("ctrl.reinstall.loadingHint")}</p>
                  </div>
                </div>
              </div>
            ) : tpl.isError && (tpl.data || []).length === 0 ? (
              // 「未找到匹配模板」会被读成"搜索词不对",用户只会反复换关键词,永远等不到结果
              <LoadFailed
                icon={HardDrive}
                title={t("ctrl.reinstall.tplLoadFailed")}
                error={tpl.error}
                onRetry={() => tpl.refetch()}
              />
            ) : filtered.length === 0 ? (
              <EmptyState icon={HardDrive} title={t("ctrl.reinstall.noMatch")} />
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
                            {t(group.label)}
                          </span>
                        </div>
                        <span
                          className="text-[10px] font-semibold px-1.5 py-0.5 rounded-full flex-shrink-0"
                          style={{ backgroundColor: brandColor + "22", color: osBrandTextColor(group.kind, brandDark) }}
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
                          {t("ctrl.reinstall.pickGroup")}
                        </div>
                      );
                    }
                    if (items.length === 0) {
                      return (
                        <div className="h-full flex items-center justify-center text-[12px] text-muted-foreground">
                          {t("ctrl.reinstall.groupEmpty")}
                        </div>
                      );
                    }
                    return (
                      <div className="divide-y divide-border/60">
                        {items.map((item) => {
                          const selected = templateName === item.templateName;
                          const kind = detectOsKind(item.templateName, item.distribution, item.family);
                          const brandColor = osBrandColor(kind);
                          return (
                            <button
                              key={item.templateName}
                              type="button"
                              onClick={() => setTemplateName(item.templateName)}
                              className={`w-full text-left px-4 py-2.5 hover:bg-secondary/50 transition-colors flex items-center gap-3 ${
                                selected ? "bg-secondary" : ""
                              }`}
                            >
                              <OsIcon
                                templateName={item.templateName}
                                distribution={item.distribution}
                                family={item.family}
                                size={24}
                              />
                              <div className="flex-1 min-w-0">
                                <div className="text-[13px] font-mono font-semibold truncate">{item.templateName}</div>
                                <div className="text-[11px] text-muted-foreground truncate">
                                  {item.distribution} · {item.family} · {item.bitFormat}-bit
                                </div>
                              </div>
                              {selected && (
                                <span
                                  className="text-[10px] font-semibold px-2 py-0.5 rounded-full flex-shrink-0"
                                  style={{ backgroundColor: brandColor, color: "#fff" }}
                                >
                                  {t("ctrl.reinstall.selectedBadge")}
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

          {/* 官方 noPartitioning(如 ESXi):分区由软件发布方决定,自定义存储整体不可用 */}
          {noPartitioning && (
            <div className="border border-info/40 bg-info/5 rounded-2xl p-3 text-[12px] flex items-start gap-2">
              <Zap className="w-4 h-4 text-info mt-0.5 flex-shrink-0" />
              <p className="text-foreground/80 leading-relaxed">{t("ctrl.reinstall.noPartitioning")}</p>
            </div>
          )}

          {/* 存储配置模式（三者互斥，见 storageMode 注释） */}
          {!noPartitioning && (
          <div className="border border-border rounded-2xl p-4 space-y-2">
            <div className="flex items-center gap-2">
              <Cog className="w-4 h-4 text-muted-foreground" />
              <h4 className="text-[13px] font-semibold">{t("ctrl.reinstall.storage.title")}</h4>
            </div>
            <p className="text-[11px] text-muted-foreground">
              {t("ctrl.reinstall.storage.desc")}
            </p>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-1.5 text-[12px]">
              <StorageModeOption
                checked={storageMode === "default"}
                onSelect={() => setStorageMode("default")}
                label={t("ctrl.reinstall.mode.default")}
                hint={t("ctrl.reinstall.mode.defaultHint")}
              />
              {isProxmox9 && (
                <StorageModeOption
                  checked={storageMode === "zfs"}
                  onSelect={() => setStorageMode("zfs")}
                  label={t("ctrl.reinstall.mode.zfs")}
                  hint={t("ctrl.reinstall.mode.zfsHint")}
                />
              )}
              <StorageModeOption
                checked={storageMode === "scheme"}
                onSelect={() => setStorageMode("scheme")}
                label={t("ctrl.reinstall.mode.scheme")}
                hint={t(templateName ? "ctrl.reinstall.mode.schemeHint" : "ctrl.reinstall.mode.schemeHintDisabled")}
                disabled={!templateName}
              />
              <StorageModeOption
                checked={storageMode === "custom"}
                onSelect={() => setStorageMode("custom")}
                label={t("ctrl.reinstall.mode.custom")}
                hint={t("ctrl.reinstall.mode.customHint")}
              />
            </div>
          </div>)}
          

          {/* Proxmox 9 ZFS 配置 */}
          {isProxmox9 && storageMode === "zfs" && (
            <div className="border border-success/40 bg-success/5 rounded-2xl p-4 space-y-3">
              <div className="flex items-center gap-2">
                <Database className="w-4 h-4 text-success" />
                <h4 className="text-[13px] font-semibold">{t("ctrl.reinstall.zfs.title")}</h4>
              </div>
              <p className="text-[11px] text-muted-foreground">
                {t("ctrl.reinstall.zfs.desc")}
              </p>

              <div className="space-y-3">
                  <div>
                    <label className="block text-[12px] mb-1.5">{t("ctrl.reinstall.zfs.raidLevel")}</label>
                    <div className="flex gap-4 text-[13px]">
                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="radio"
                          checked={zfsRaidLevel === 1}
                          onChange={() => setZfsRaidLevel(1)}
                          className="w-4 h-4"
                        />
                        {t("ctrl.reinstall.zfs.raid1")}
                      </label>
                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="radio"
                          checked={zfsRaidLevel === 0}
                          onChange={() => setZfsRaidLevel(0)}
                          className="w-4 h-4"
                        />
                        {t("ctrl.reinstall.zfs.raid0")}
                      </label>
                    </div>
                  </div>
                  {zfsRaid1Impossible && (
                    <p className="text-[11px] text-destructive">
                      {t("ctrl.reinstall.zfs.singleDiskWarn")}
                    </p>
                  )}
                  <div>
                    <label className="block text-[12px] mb-1.5">{t("ctrl.reinstall.zfs.vzLabel")}</label>
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
                      {/* 磁盘信息读失败时不能继续写"读取中",那会让用户一直等一个不会来的数字 */}
                      {t("ctrl.reinstall.zfs.vzHint", {
                        max: vzMaxKnown
                          ? `${zfsCap.maxVzGB} GB`
                          : disk.isError
                            ? t("ctrl.reinstall.zfs.maxUnknownFailed")
                            : t("ctrl.reinstall.zfs.maxUnknownLoading"),
                      })}
                      {zfsCap.singleDiskGB > 0 && (
                        <>
                          {" "}
                          {t("ctrl.reinstall.zfs.vzDetail", {
                            disks: zfsCap.diskCount,
                            size: zfsCap.singleDiskGB,
                            level: zfsRaidLevel,
                            usable: Math.floor(zfsCap.usableMB / 1024),
                          })}
                        </>
                      )}
                    </p>
                  </div>
                </div>
            </div>
          )}

          {/* 自定义 Hostname */}
          <div>
            <label className="block text-[12px] font-semibold mb-1.5">{t("ctrl.reinstall.hostnameLabel")}</label>
            <Input
              placeholder={t("ctrl.reinstall.hostnamePlaceholder")}
              value={hostname}
              onChange={(e) => setHostname(e.target.value)}
            />
          </div>

          {/* 官方 customizeQuestions:OS 特定定制(sshKey / 安装后脚本 / 语言 / LACP…),
              键名与取值由 OVH 模板定义,这里动态渲染、原样回传 —— 与官方 Manager 同源 */}
          {customizeQuestions.length > 0 && (
            <div className="space-y-3">
              <h4 className="text-[12px] font-semibold flex items-center gap-1.5">
                <Terminal className="w-3.5 h-3.5 text-muted-foreground" />
                {t("ctrl.reinstall.customize.title")}
              </h4>
              {customizeQuestions.map((q) => {
                const isBool = q.type === "boolean";
                const val = customizationAnswers[q.name];
                return (
                  <div key={q.name}>
                    <label className="block text-[12px] font-medium mb-1">
                      {q.description || q.name}
                      {q.required && <span className="text-destructive ml-0.5">*</span>}
                    </label>
                    {isBool ? (
                      <label className="flex items-center gap-2 cursor-pointer text-[12px]">
                        <input
                          type="checkbox"
                          checked={val === true}
                          onChange={(e) =>
                            setCustomizationAnswers((a) => ({ ...a, [q.name]: e.target.checked }))
                          }
                          className="w-4 h-4"
                        />
                        {t("ctrl.reinstall.customize.enable")}
                      </label>
                    ) : q.name === "postInstallationScript" ? (
                      <Textarea
                        value={typeof val === "string" ? val : ""}
                        onChange={(e) => setCustomizationAnswers((a) => ({ ...a, [q.name]: e.target.value }))}
                        placeholder={t("ctrl.reinstall.customize.scriptHint")}
                        className="font-mono text-[11px] min-h-[72px]"
                      />
                    ) : (
                      <Input
                        value={typeof val === "string" ? val : ""}
                        onChange={(e) => setCustomizationAnswers((a) => ({ ...a, [q.name]: e.target.value }))}
                        placeholder={q.name === "sshKey" ? t("ctrl.reinstall.customize.sshKeyHint") : q.type || ""}
                        className={q.name === "sshKey" ? "font-mono text-[11px]" : ""}
                      />
                    )}
                  </div>
                );
              })}
              <p className="text-[11px] text-muted-foreground">{t("ctrl.reinstall.customize.note")}</p>
            </div>
          )}

          {/* 内置分区方案（仅 scheme 模式） */}
          {storageMode === "scheme" && templateName && (
            <div>
              <label className="block text-[12px] font-semibold mb-1.5">{t("ctrl.reinstall.mode.scheme")}</label>
              {ps.isPending ? (
                <Skeleton className="h-9 rounded-md" />
              ) : ps.isError ? (
                // 原来读失败也照样显示「该模板没有内置分区方案，请改用其它存储模式。」——
                // 这句话是在直接指挥用户改重装方案,而方案可能好端端地在那儿,只是这次没问到。
                <LoadFailed
                  compact
                  icon={Cog}
                  title={t("ctrl.reinstall.scheme.loadFailed")}
                  error={ps.error}
                  onRetry={() => ps.refetch()}
                />
              ) : (ps.data || []).length === 0 ? (
                <p className="text-[12px] text-muted-foreground">{t("ctrl.reinstall.scheme.empty")}</p>
              ) : (
                <Select value={partitionSchemeName} onValueChange={setPartitionSchemeName}>
                <SelectTrigger>
                  <SelectValue placeholder={t("ctrl.reinstall.mode.default")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value=" ">{t("ctrl.reinstall.mode.default")}</SelectItem>
                  {(ps.data || []).map((s) => (
                    <SelectItem key={s.name} value={s.name}>
                      {t("ctrl.reinstall.scheme.item", { name: s.name, priority: s.priority })}
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
                {t("ctrl.reinstall.advTitle")}
              </div>
            )}

            {useCustomStorage && (
              <div className="space-y-4 border border-border rounded-2xl p-4 bg-secondary/30">
                {/* 磁盘组 + 硬件 RAID */}
                {disk.isPending ? (
                  <Skeleton className="h-20 rounded-md" />
                ) : disk.isError ? (
                  // 「未检测到磁盘组信息」= 断言这机器没有可配置的磁盘组,用户会跳过 RAID 直接提交,
                  // 结果是拿一份没有任何磁盘约束的存储配置去重装。
                  <LoadFailed
                    compact
                    icon={HardDrive}
                    title={t("ctrl.reinstall.disk.loadFailed")}
                    error={disk.error}
                    onRetry={() => disk.refetch()}
                  />
                ) : Object.keys(disk.data || {}).length === 0 ? (
                  <p className="text-[12px] text-muted-foreground">{t("ctrl.reinstall.disk.empty")}</p>
                ) : (
                  <div className="space-y-3">
                    <h4 className="text-[12px] font-semibold">{t("ctrl.reinstall.disk.groupTitle")}</h4>
                    {Object.entries(disk.data || {}).map(([gidStr, group]) => {
                      const gid = parseInt(gidStr);
                      return (
                        <div key={gid} className="border border-border rounded-xl p-3 space-y-2 bg-background">
                          <div className="flex items-center gap-2 text-[12px]">
                            <HardDrive className="w-3.5 h-3.5 text-muted-foreground" />
                            <span className="font-semibold">{t("ctrl.reinstall.disk.group", { id: gid })}</span>
                            {group.raidController && (
                              <span className="text-[10px] px-1.5 py-0.5 rounded-full border border-border">
                                {group.raidController}
                              </span>
                            )}
                            {/* 官方 Data erasure:默认所有盘组都会被擦。
                                非安装盘组勾选后带 erase:false —— 混合盘机器保数据盘的唯一手段 */}
                            <label className="ml-auto flex items-center gap-1.5 cursor-pointer text-[11px] text-muted-foreground">
                              <input
                                type="checkbox"
                                checked={keepDiskGroups.has(gid)}
                                onChange={(e) => {
                                  const next = new Set(keepDiskGroups);
                                  if (e.target.checked) next.add(gid);
                                  else next.delete(gid);
                                  setKeepDiskGroups(next);
                                }}
                                className="w-3.5 h-3.5"
                              />
                              {t("ctrl.reinstall.disk.keepData")}
                            </label>
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
                            <label className="block text-[11px] text-muted-foreground mb-1">{t("ctrl.reinstall.hwRaid.label")}</label>
                            {/* hook 现在只把 404/501（OVH 明说没有 RAID 控制器）当成 supported:false,
                                其余错误会抛出来走 isError —— 否则这里会在读失败时言之凿凿地写
                                「此服务器不支持硬件 RAID」,用户照办改用软 RAID 装完才发现白折腾。 */}
                            {raid.isPending ? (
                              <Skeleton className="h-9 rounded-md" />
                            ) : raid.isError ? (
                              <p className="text-[11px] text-destructive">
                                {t("ctrl.reinstall.hwRaid.readFailed", { err: errorMessage(raid.error) })}
                              </p>
                            ) : !raid.data?.supported ? (
                              <p className="text-[11px] text-warning">
                                {t("ctrl.reinstall.hwRaid.unsupported")}
                              </p>
                            ) : (
                              <Select
                                value={hardwareRaid[gid] || ""}
                                onValueChange={(v) => setHardwareRaid({ ...hardwareRaid, [gid]: v === " " ? "" : v })}
                              >
                                <SelectTrigger className="h-9">
                                  <SelectValue placeholder={t("ctrl.reinstall.hwRaid.none")} />
                                </SelectTrigger>
                                <SelectContent>
                                  {HARDWARE_RAID_LEVELS.map((l) => (
                                    <SelectItem key={l.value || "none"} value={l.value || " "}>
                                      {t(l.label)}
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

                {/* 官方 hardwareRaid 高级参数:RAID10 阵列数 + 热备盘。
                    arrays=4 配 12 盘 = 4 组 3 盘 RAID1 再组 RAID0;spares 是热备 */}
                {raid.data?.supported && Object.values(hardwareRaid).some((v) => v && v !== "") && (
                  <div className="flex flex-wrap items-center gap-3 text-[11px]">
                    {Object.values(hardwareRaid).includes("raid10") && (
                      <label className="flex items-center gap-1.5">
                        <span className="text-muted-foreground">{t("ctrl.reinstall.hwRaid.arrays")}</span>
                        <Input
                          type="number"
                          min={1}
                          value={hwRaidArrays}
                          onChange={(e) => setHwRaidArrays(e.target.value.replace(/\D/g, ""))}
                          placeholder={t("ctrl.reinstall.hwRaid.auto")}
                          className="h-7 w-20"
                        />
                      </label>
                    )}
                    <label className="flex items-center gap-1.5">
                      <span className="text-muted-foreground">{t("ctrl.reinstall.hwRaid.spares")}</span>
                      <Input
                        type="number"
                        min={0}
                        value={hwRaidSpares}
                        onChange={(e) => setHwRaidSpares(e.target.value.replace(/\D/g, ""))}
                        placeholder={t("ctrl.reinstall.hwRaid.none")}
                        className="h-7 w-20"
                      />
                    </label>
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
                    {t("ctrl.reinstall.swRaid.label")}
                  </label>
                  {useSoftwareRaid && (
                    <div className="pl-6 space-y-2">
                      <Select value={softwareRaidLevel} onValueChange={setSoftwareRaidLevel}>
                        <SelectTrigger className="h-9">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {/* 官方 softRaidOnlyMirroring:部分 OS 只支持 RAID 0/1 且只能用前两块盘 */}
                          {SOFTWARE_RAID_LEVELS.filter((l) =>
                            softRaidOnlyMirroring ? l.value === "raid0" || l.value === "raid1" : true
                          ).map((l) => (
                            <SelectItem key={l.value} value={l.value}>
                              {t(l.label)}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <div className="flex items-center gap-2">
                        <Input
                          type="number"
                          min={1}
                          value={softwareRaidDisks}
                          onChange={(e) =>
                            setSoftwareRaidDisks(e.target.value.replace(/\D/g, ""))
                          }
                          placeholder={t("ctrl.reinstall.swRaid.disksPlaceholder")}
                          className="h-8 w-28"
                        />
                        <p className="text-[11px] text-muted-foreground">
                          {t("ctrl.reinstall.swRaid.disksHint")}
                        </p>
                      </div>
                      <p className="text-[11px] text-muted-foreground">
                        {t("ctrl.reinstall.swRaid.desc")}
                      </p>
                    </div>
                  )}
                </div>

                {/* 自定义分区 */}
                <div className="border-t border-border pt-3">
                  <div className="flex flex-wrap items-center justify-between gap-2 mb-2">
                    <h4 className="text-[12px] font-semibold">{t("ctrl.reinstall.part.title")}</h4>
                    <div className="flex flex-wrap gap-2">
                    {/* 智能配置:按实际磁盘生成一份方案,省掉手填。
                        混合盘(多磁盘组)也给方案 —— 挑最快的组装系统,
                        而不是笼统回落"用默认分区"。 */}
                    <Button
                      type="button"
                      size="sm"
                      variant="secondary"
                      onClick={() => setShowSmart(true)}
                      disabled={!disk.data || Object.keys(disk.data).length === 0}
                    >
                      <Wand2 className="w-3.5 h-3.5 mr-1" />
                      {t("ctrl.reinstall.smart.title")}
                    </Button>
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
                      {t("ctrl.reinstall.part.add")}
                    </Button>
                    </div>
                  </div>
                  <p className="text-[11px] text-muted-foreground mb-2">{t("ctrl.reinstall.part.desc")}</p>
                  {customPartitions.length > 0 && (
                    <div className="space-y-2">
                      {customPartitions.map((p, idx) => (
                        <PartitionRow
                          key={idx}
                          partition={p}
                          diskGroupIds={Object.keys(disk.data || {}).map((s) => parseInt(s))}
                          filesystems={osFilesystems}
                          lvmReady={lvmReady}
                          softRaidOnlyMirroring={softRaidOnlyMirroring}
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
                <Trans i18nKey="ctrl.reinstall.confirmWarn" components={{ b: <span className="font-semibold" /> }} />
              </div>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          {/* blocked 时按钮锁死:信息缺失下按不可逆的重装,是这个对话框里代价最大的一种误操作 */}
          <Button
            onClick={handleSubmit}
            disabled={!templateName || mut.isPending || blocked}
            title={blocked ? t("ctrl.reinstall.submitBlockedTitle", { list: failedList }) : undefined}
          >
            {mut.isPending
              ? t("ctrl.reinstall.submitting")
              : blocked
                ? t("ctrl.reinstall.submitBlocked")
                : confirming
                  ? t("ctrl.reinstall.submitConfirm")
                  : t("ctrl.reinstall.next")}
          </Button>
        </DialogFooter>
      </DialogContent>

      {/* 智能配置确认。不直接套用 —— 分区是不可逆操作的入口,
          必须先让用户看清"装在哪个组、为什么、另一个组会怎样"。 */}
      <Dialog open={showSmart} onOpenChange={setShowSmart}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Wand2 className="w-4 h-4" />
              {t("ctrl.reinstall.smart.title")}
            </DialogTitle>
            <DialogDescription>{t("ctrl.reinstall.smart.desc")}</DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            <div className="rounded-xl border border-border bg-muted/40 px-3.5 py-3">
              <p className="text-[12px] font-medium mb-1.5">{t("ctrl.reinstall.smart.detected")}</p>
              {smartPlan.groups.length === 0 ? (
                <p className="text-[11px] text-muted-foreground">{t("ctrl.reinstall.smart.noDisks")}</p>
              ) : (
                <ul className="text-[11px] text-muted-foreground space-y-0.5">
                  {smartPlan.groups.map((g) => (
                    <li key={g.id}>
                      {t("ctrl.reinstall.smart.groupLine", { id: g.id })}
                      {g.label}
                      {g.id === smartPlan.targetGroupId && !smartPlan.blocked && (
                        <span className="ml-1.5 text-foreground font-medium">{t("ctrl.reinstall.smart.target")}</span>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </div>

            {smartPlan.blocked ? (
              <div className="rounded-xl border border-warning/40 bg-warning/5 px-3.5 py-3">
                <p className="text-[12px] leading-relaxed">{smartPlan.blocked}</p>
              </div>
            ) : (
              <>
                <div className="rounded-xl border border-border px-3.5 py-3">
                  <p className="text-[12px] font-medium mb-1.5">{t("ctrl.reinstall.smart.partitions")}</p>
                  <div className="space-y-1">
                    {smartPlan.partitions.map((p, i) => (
                      <div key={i} className="text-[11px] font-mono flex flex-wrap gap-x-2">
                        <span className="text-foreground">{p.mountpoint}</span>
                        <span className="text-muted-foreground">{p.filesystem}</span>
                        <span className="text-muted-foreground">
                          {p.size === 0 ? t("ctrl.reinstall.smart.remaining") : `${p.size}MB`}
                        </span>
                        <span className="text-muted-foreground">{t("ctrl.reinstall.smart.partGroup", { id: p.diskGroupId })}</span>
                        {p.raid && <span className="text-muted-foreground">{p.raid.toUpperCase()}</span>}
                      </div>
                    ))}
                  </div>
                </div>
                <ul className="text-[11px] text-muted-foreground space-y-1 list-disc pl-4">
                  {smartPlan.notes.map((n, i) => (
                    <li key={i}>{n}</li>
                  ))}
                </ul>
              </>
            )}
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowSmart(false)}>
              {t("common.cancel")}
            </Button>
            <Button
              disabled={!!smartPlan.blocked || smartPlan.partitions.length === 0}
              onClick={() => {
                // 覆盖而不是追加:用户点"智能配置"要的是一份完整方案,
                // 追加会和他之前手填的撞车(比如两个 size=0)
                setCustomPartitions(smartPlan.partitions);
                setShowSmart(false);
                toast.success(t("ctrl.reinstall.smart.applied", { n: smartPlan.partitions.length }));
              }}
            >
              {t("ctrl.reinstall.smart.apply")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
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
  filesystems,
  lvmReady,
  softRaidOnlyMirroring,
}: {
  partition: CustomPartition;
  diskGroupIds: number[];
  onChange: (p: CustomPartition) => void;
  onRemove: () => void;
  /** 官方 installationTemplate.filesystems:该 OS 实际支持的文件系统;缺省退回全集 */
  filesystems?: string[];
  /** 官方 lvmReady:false 时该 OS 不支持 LVM,LV 名输入不可用 */
  lvmReady?: boolean;
  /** 官方 softRaidOnlyMirroring:true 时软 RAID 只允许 0/1 */
  softRaidOnlyMirroring?: boolean;
}) {
  const { t } = useTranslation();
  // 官方口径:文件系统下拉按所选模板的 filesystems 过滤(模板没给元数据时退回全集)
  const fsOptions = (filesystems && filesystems.length > 0 ? filesystems : FILESYSTEMS).filter(
    (fs) => FILESYSTEMS.includes(fs)
  );
  const raidOptions = softRaidOnlyMirroring
    ? SOFTWARE_RAID_LEVELS.filter((l) => l.value === "raid0" || l.value === "raid1")
    : SOFTWARE_RAID_LEVELS;
  return (
    <div className="border border-border rounded-xl p-2.5 flex items-center gap-2 text-[12px] bg-background flex-wrap">
      <Input
        value={partition.mountpoint}
        onChange={(e) => onChange({ ...partition, mountpoint: e.target.value })}
        placeholder={t("ctrl.reinstall.part.mountpoint")}
        className="h-8 w-32"
      />
      <Select value={partition.filesystem} onValueChange={(v) => onChange({ ...partition, filesystem: v })}>
        <SelectTrigger className="h-8 w-24">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {fsOptions.map((fs) => (
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
        title={t("ctrl.reinstall.part.sizeHint")}
      />
      <span className="text-[11px] text-muted-foreground">MB</span>
      {diskGroupIds.length > 1 && (
        <Select
          value={String(partition.diskGroupId ?? "")}
          onValueChange={(v) => onChange({ ...partition, diskGroupId: parseInt(v) })}
        >
          <SelectTrigger className="h-8 w-24">
            <SelectValue placeholder={t("ctrl.reinstall.part.diskGroup")} />
          </SelectTrigger>
          <SelectContent>
            {diskGroupIds.map((gid) => (
              <SelectItem key={gid} value={String(gid)}>
                {t("ctrl.reinstall.disk.group", { id: gid })}
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
          <SelectValue placeholder={t("ctrl.reinstall.part.noRaid")} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value=" ">{t("ctrl.reinstall.part.noRaid")}</SelectItem>
          {raidOptions.map((l) => (
            <SelectItem key={l.value} value={l.value}>
              {l.value.toUpperCase()}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button type="button" variant="outline" size="sm" onClick={onRemove} className="ml-auto h-8 w-8 p-0">
        <XIcon className="w-3.5 h-3.5" />
      </Button>
      {/* 官方 extras(第二行):ZFS 显示 zpool 名;非 ZFS 且 OS 支持 LVM 显示逻辑卷名。
          官方 auto-fix:同 RAID 的 LV 自动归同一 VG、同名 zpool 合并 —— 用户只需命名即可控制分组 */}
      {partition.filesystem === "zfs" && (
        <Input
          value={partition.zpoolName ?? ""}
          onChange={(e) => onChange({ ...partition, zpoolName: e.target.value })}
          placeholder={t("ctrl.reinstall.part.zpool")}
          className="h-8 w-36 font-mono text-[11px]"
          title={t("ctrl.reinstall.part.zpoolHint")}
        />
      )}
      {partition.filesystem !== "zfs" && lvmReady && (
        <Input
          value={partition.lvName ?? ""}
          onChange={(e) => onChange({ ...partition, lvName: e.target.value })}
          placeholder={t("ctrl.reinstall.part.lv")}
          className="h-8 w-36 font-mono text-[11px]"
          title={t("ctrl.reinstall.part.lvHint")}
        />
      )}
    </div>
  );
}
