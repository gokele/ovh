import type { DiskGroup, CustomPartition } from "@/hooks/use-server-control";

/**
 * 「智能配置」:按机器实际的磁盘情况生成一份分区方案。
 *
 * ── 规则出处(全部来自 OVH 官方分区文档,不是拍脑袋)──────────────────
 * https://docs.ovhcloud.com/en/guides/bare-metal-cloud/dedicated-servers/partitioning_ovh/
 *
 *  1. "Up to 1 partition can be configured to fill the remaining space (size 0)."
 *     → 整份方案里只有 `/` 留空占满,其余必须给定大小。
 *  2. "the API only supports OS installation and storage customisation on
 *     1 single disk group."
 *     → **多磁盘组(混合盘)不能同时自定义两个组**。这是接口的硬限制。
 *       所以混合盘时的正确做法不是放弃,而是挑一个组装系统、把方案给它,
 *       另一个组这次不动,装完进系统自己挂载。
 *  3. "/boot partition type cannot be XFS"(Debian 系)
 *     → /boot 一律 ext4。
 *  4. "If not specified, raidLevel will be set to 1."
 *  5. swap 不能 size 0,且 "The RAID level for swap can only be set to 1"
 *     → 这份方案**不生成 swap**:大小拿不准时给错比不给更糟,
 *       而 OVH 的模板本身会处理内存交换。需要的人可以手动加一条。
 */

/** 磁盘快慢排序:数字越小越快。用于混合盘挑系统盘。 */
const SPEED_RANK: Record<string, number> = { nvme: 0, ssd: 1, sas: 2, sata: 3, unknown: 9 };

function rankOf(t?: string): number {
  const k = (t || "").trim().toLowerCase();
  if (k in SPEED_RANK) return SPEED_RANK[k];
  // 有些机器把类型写成 "SSD NVMe" / "HDD SATA" 这种组合串
  if (k.includes("nvme")) return SPEED_RANK.nvme;
  if (k.includes("ssd")) return SPEED_RANK.ssd;
  if (k.includes("sas")) return SPEED_RANK.sas;
  if (k.includes("sata") || k.includes("hdd")) return SPEED_RANK.sata;
  return SPEED_RANK.unknown;
}

/** 人话化的盘型标签 */
export function diskTypeLabel(t?: string): string {
  const r = rankOf(t);
  if (r === SPEED_RANK.nvme) return "NVMe 固态";
  if (r === SPEED_RANK.ssd) return "SSD 固态";
  if (r === SPEED_RANK.sas) return "SAS 机械";
  if (r === SPEED_RANK.sata) return "SATA 机械";
  return t || "未知类型";
}

export interface GroupSummary {
  id: number;
  diskCount: number;
  /** 单盘容量(GB),取该组第一块盘 */
  diskCapacityGB: number;
  diskType?: string;
  label: string;
}

/**
 * 按盘数选软 RAID 级别。
 *
 * 刻意**不**默认 RAID 0:它没有任何冗余,4 块盘里坏一块就是全盘数据没了。
 * 抢到的机器多半要跑点东西,默认值不该是"任何一块盘坏掉就全丢"。
 * 想要满容量的人可以在生成之后手动改成 RAID 0 —— 那是一次显式选择。
 */
export function pickRaidLevel(diskCount: number): number | null {
  if (diskCount <= 1) return null; // 单盘无从 RAID
  if (diskCount === 2) return 1; // 镜像
  return 5; // 3 块及以上:容量利用率高,且容忍坏一块
}

export interface SmartPlan {
  /** 生成的分区(可能为空:Windows 等情况不生成) */
  partitions: CustomPartition[];
  /** 系统装在哪个磁盘组 */
  targetGroupId: number;
  groups: GroupSummary[];
  /** 给用户看的说明,逐条 */
  notes: string[];
  /** 为空表示可以应用;非空表示不建议自动生成,原因在这 */
  blocked?: string;
}

export function summarizeGroups(diskGroups: Record<string, DiskGroup>): GroupSummary[] {
  return Object.entries(diskGroups)
    .map(([key, g]) => {
      const id = Number(g.id ?? key) || Number(key) || 0;
      const disks = g.disks || [];
      const cap = disks[0]?.capacity ?? 0;
      const type = g.diskType || disks[0]?.diskType;
      return {
        id,
        diskCount: disks.length,
        diskCapacityGB: cap,
        diskType: type,
        label: `${disks.length}×${cap}${disks[0]?.unit || "GB"} ${diskTypeLabel(type)}`,
      };
    })
    .filter((g) => g.id > 0 && g.diskCount > 0)
    .sort((a, b) => a.id - b.id);
}

/**
 * 生成方案。osKind 来自 detectOsKind,用来避开 Windows
 * (NTFS 只支持 RAID 1,分区规则和 Linux 完全不同,自动生成弊大于利)。
 */
export function buildSmartPlan(
  diskGroups: Record<string, DiskGroup>,
  osKind?: string
): SmartPlan {
  const groups = summarizeGroups(diskGroups);
  if (groups.length === 0) {
    return { partitions: [], targetGroupId: 0, groups, notes: [], blocked: "没读到磁盘组信息,无法生成方案" };
  }
  if (osKind === "windows") {
    return {
      partitions: [],
      targetGroupId: groups[0].id,
      groups,
      notes: [],
      blocked: "Windows 的分区规则和 Linux 不同(NTFS 只支持 RAID 1),这里不自动生成,请用默认分区方案",
    };
  }

  // 系统装最快的那一组;同样快就用编号小的(OVH 默认装在 diskGroupId 1)
  const target = [...groups].sort(
    (a, b) => rankOf(a.diskType) - rankOf(b.diskType) || a.id - b.id
  )[0];
  const raid = pickRaidLevel(target.diskCount);
  const notes: string[] = [];

  notes.push(
    `系统装在磁盘组 ${target.id}(${target.label})` +
      (groups.length > 1 ? " —— 这组最快" : "")
  );
  notes.push(
    raid === null
      ? "只有一块盘,不做 RAID"
      : raid === 1
        ? "2 块盘 → RAID 1 镜像:坏一块数据还在,可用容量是一半"
        : `${target.diskCount} 块盘 → RAID 5:可用约 ${target.diskCount - 1} 块盘的容量,允许坏一块`
  );
  notes.push("/boot 用 ext4 —— OVH 文档明确 /boot 不能用 XFS");
  notes.push("根分区留空 = 占满剩余空间(整份方案只允许一个这样的分区)");

  if (groups.length > 1) {
    const others = groups.filter((g) => g.id !== target.id);
    notes.push(
      `另外 ${others.length} 个磁盘组(${others.map((g) => g.label).join("、")})这次不动 —— ` +
        "OVH 接口只支持对一个磁盘组做自定义分区。装完进系统自己分区挂载即可,上面的数据不受影响"
    );
  }
  if (raid === 5) {
    notes.push("想要满容量可以把根分区改成 RAID 0,但那样坏任意一块盘就全丢,请自行权衡");
  }

  const raidStr = raid === null ? undefined : `raid${raid}`;
  const partitions: CustomPartition[] = [
    {
      mountpoint: "/boot",
      filesystem: "ext4",
      size: 1024,
      order: 1,
      type: "primary",
      // /boot 固定用镜像:1GB 的代价换一个"坏盘还能开机"
      raid: raid === null ? undefined : "raid1",
      diskGroupId: target.id,
    },
    {
      mountpoint: "/",
      filesystem: "ext4",
      size: 0, // 0 = 占满剩余
      order: 2,
      type: "primary",
      raid: raidStr,
      diskGroupId: target.id,
    },
  ];

  return { partitions, targetGroupId: target.id, groups, notes };
}
