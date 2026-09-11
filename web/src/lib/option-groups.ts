import type { ServerOption } from "@/hooks/use-servers";

/** 选项分组类别（对齐 OVH 官方 catalog 的 addonFamilies：memory / storage / system-storage / bandwidth + 派生） */
export type OptionGroupKey = "cpu" | "memory" | "systemStorage" | "storage" | "bandwidth" | "vrack" | "other";

export const OPTION_GROUP_LABELS: Record<OptionGroupKey, string> = {
  cpu: "CPU / 处理器",
  memory: "内存",
  systemStorage: "系统盘",
  storage: "存储 / 数据盘",
  bandwidth: "带宽 / 网络",
  vrack: "vRack 内网",
  other: "其他",
};

/**
 * 排除许可证 / 操作系统 / 控制面板等非硬件选项（旧前端 filteredOptions 逻辑）
 */
export function isHardwareOption(option: ServerOption): boolean {
  const v = option.value.toLowerCase();
  const l = option.label.toLowerCase();
  if (
    v.includes("windows-server") ||
    v.includes("sql-server") ||
    v.includes("cpanel-license") ||
    v.includes("plesk-") ||
    v.includes("-license-") ||
    v.startsWith("os-") ||
    v.includes("control-panel") ||
    v.includes("panel") ||
    v.includes("security") ||
    v.includes("antivirus") ||
    v.includes("firewall") ||
    l.includes("license") ||
    l.includes("许可证") ||
    l.includes("许可")
  ) {
    return false;
  }
  return true;
}

/**
 * 按 OVH 官方 catalog 的 addonFamilies 字段分组，family 缺失时退化到 value / desc 关键字。
 * OVH addonFamilies 取值：memory / storage / system-storage / bandwidth 等。
 */
export function classifyOption(option: ServerOption): OptionGroupKey {
  const family = option.family?.toLowerCase() || "";
  const desc = option.label.toLowerCase();
  const value = option.value.toLowerCase();

  // 优先看 family 字段（OVH 目录权威分类）
  if (family === "system-storage" || family.includes("system-storage")) return "systemStorage";
  if (family === "memory" || family.includes("memory") || family.includes("ram")) return "memory";
  if (family === "storage" || (family.includes("storage") && !family.includes("system"))) return "storage";
  if (family === "bandwidth" || family.includes("bandwidth") || family.includes("traffic")) return "bandwidth";
  if (family.includes("vrack")) return "vrack";

  // family 缺失或不识别时，用 value / desc 关键字兜底

  // CPU
  if (
    desc.includes("cpu") || desc.includes("processor") ||
    desc.includes("intel") || desc.includes("amd") ||
    desc.includes("xeon") || desc.includes("epyc") ||
    desc.includes("ryzen") || desc.includes("core")
  ) {
    return "cpu";
  }
  // vRack
  if (value.includes("vrack") || desc.includes("vrack") || desc.includes("内网")) {
    return "vrack";
  }
  // 内存
  if (
    desc.includes("ram") || desc.includes("memory") ||
    desc.includes("ddr") || value.startsWith("ram-")
  ) {
    return "memory";
  }
  // 系统盘：value 含 `-system-` 是 OVH 系统盘 addon 的命名约定
  if (value.includes("-system-")) {
    return "systemStorage";
  }
  // 普通存储
  if (
    desc.includes("ssd") || desc.includes("hdd") ||
    desc.includes("nvme") || desc.includes("storage") ||
    desc.includes("disk") || desc.includes("raid") ||
    value.includes("softraid") || value.includes("hybridsoftraid") ||
    value.includes("noraid")
  ) {
    return "storage";
  }
  // 带宽 / 流量
  if (
    desc.includes("bandwidth") || desc.includes("network") ||
    desc.includes("带宽") || desc.includes("mbps") || desc.includes("gbps") ||
    value.startsWith("bandwidth-") || value.startsWith("traffic-")
  ) {
    return "bandwidth";
  }
  return "other";
}

/** 把 availableOptions 按组分桶，并过滤掉非硬件选项 */
export function groupOptions(options: ServerOption[] | undefined): Record<OptionGroupKey, ServerOption[]> {
  const buckets: Record<OptionGroupKey, ServerOption[]> = {
    cpu: [],
    memory: [],
    systemStorage: [],
    storage: [],
    bandwidth: [],
    vrack: [],
    other: [],
  };
  (options || []).filter(isHardwareOption).forEach((opt) => {
    buckets[classifyOption(opt)].push(opt);
  });
  return buckets;
}

/**
 * 格式化选项显示名（对齐旧前端的友好展示）
 * 比如 `ram-64g-ecc-2400` → `64 GB`，`softraid-2x450nvme-24sk50` → `SOFTRAID 2x 450GB NVME`
 */
export function formatOptionDisplay(option: ServerOption, group: OptionGroupKey): string {
  const v = option.value;
  if (group === "memory") {
    const m = v.match(/ram-(\d+)g/i);
    if (m) return `${m[1]} GB`;
  }
  if (group === "storage" || group === "systemStorage") {
    const hybrid = v.match(/hybridsoftraid-(\d+)x(\d+)(sa|ssd|hdd)-(\d+)x(\d+)(nvme|ssd|hdd)/i);
    if (hybrid) {
      return `混合 ${hybrid[1]}× ${hybrid[2]}GB ${hybrid[3].toUpperCase()} + ${hybrid[4]}× ${hybrid[5]}GB ${hybrid[6].toUpperCase()}`;
    }
    const std = v.match(/(raid|softraid)-(\d+)x(\d+)(sa|ssd|hdd|nvme)/i);
    if (std) {
      return `${std[1].toUpperCase()} ${std[2]}× ${std[3]}GB ${std[4].toUpperCase()}`;
    }
    // noraid-0disk 之类 = "无盘 / 系统盘空"
    if (/noraid-0/i.test(v) || /0disk/i.test(v)) return "无盘";
  }
  if (group === "bandwidth") {
    if (v.toLowerCase().includes("unlimited")) return "无限流量";
    const combined = v.match(/traffic-(\d+)(tb|gb|mb)-(\d+)/i);
    if (combined) {
      return `${combined[3]} Mbps · ${combined[1]} ${combined[2].toUpperCase()} 流量`;
    }
    const traffic = v.match(/traffic-(\d+)(tb|gb)/i);
    if (traffic) return `${traffic[1]} ${traffic[2].toUpperCase()} 流量`;
    const bw = v.match(/bandwidth-(\d+)/i);
    if (bw) {
      const speed = parseInt(bw[1]);
      return speed >= 1000 ? `${speed / 1000} Gbps` : `${speed} Mbps`;
    }
  }
  if (group === "vrack") {
    const m = v.match(/vrack-bandwidth-(\d+)/i);
    if (m) {
      const speed = parseInt(m[1]);
      return speed >= 1000 ? `${speed / 1000} Gbps 内网` : `${speed} Mbps 内网`;
    }
  }
  return option.label;
}

/**
 * 把一个裸的 addon planCode 转成人话，例如：
 *   ram-64g-ecc-2133-24sk60      → 64 GB
 *   softraid-2x480ssd-24sk60     → SOFTRAID 2× 480GB SSD
 *
 * 为什么需要"只吃 code"的版本：队列项和监控订阅存下来的是 addon planCode，
 * 它们是**当时那一刻的快照** —— 机型可能已经下架、目录可能没拉到，
 * 这两种情况下都拿不到对应的 ServerOption。而 classifyOption / formatOptionDisplay
 * 本来就是靠 value 的正则在判，不依赖目录，所以直接喂一个只有 value 的壳就行。
 *
 * 认不出来就原样返回 code —— 显示一串看不懂的代码，也好过显示"3 个配置"
 * 这种既看不出是什么、又无法排查的说法。
 */
export function describeOptionCode(code: string): string {
  const v = code.trim();
  if (!v) return "";
  const shell: ServerOption = { value: v, label: v };
  const text = formatOptionDisplay(shell, classifyOption(shell));
  return text && text !== v ? text : v;
}

/** 一组 addon planCode 的可读摘要，用于列表卡片。认不出的原样带上 */
export function describeOptionCodes(codes: string[] | undefined): string {
  if (!codes || codes.length === 0) return "";
  return codes.map(describeOptionCode).join(" · ");
}
