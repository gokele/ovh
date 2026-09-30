import { Cpu, MemoryStick, HardDrive, HardDriveDownload, Wifi, Network, Server } from "lucide-react";
import {
  OPTION_GROUP_LABELS,
  formatOptionDisplay,
  type OptionGroupKey,
} from "@/lib/option-groups";
import type { ServerOption } from "@/hooks/use-servers";

/** option 组 → 图标映射 */
const ICON_MAP: Record<OptionGroupKey, React.ComponentType<{ className?: string }>> = {
  cpu: Cpu,
  memory: MemoryStick,
  systemStorage: HardDriveDownload,
  storage: HardDrive,
  bandwidth: Wifi,
  vrack: Network,
  other: Server,
};

/** 单组配置选择器:组内单选,胶囊形式。
 *  - hasStock 返回 undefined 时不渲染绿/红点(availability 数据还没来)
 *  - defaultValueSet 里的项右侧加"默认"小徽章 */
export function OptionGroupSection({
  groupKey,
  options,
  picked,
  defaultValueSet,
  hasStock,
  onPick,
}: {
  groupKey: OptionGroupKey;
  options: ServerOption[];
  picked: string;
  defaultValueSet: Set<string>;
  /** 给定 option value,跟用户其它选配组合后能否凑出至少一个 DC 有货。
   *  undefined 表示 OVH availability 数据没回,不渲染绿/红点。 */
  hasStock?: (value: string) => boolean;
  onPick: (value: string) => void;
}) {
  const Icon = ICON_MAP[groupKey];
  return (
    <div>
      <h3 className="text-[13px] font-semibold mb-2.5 flex items-center gap-1.5">
        <Icon className="w-3.5 h-3.5 text-muted-foreground" />
        {OPTION_GROUP_LABELS[groupKey]}
      </h3>
      <div className="flex flex-wrap gap-2">
        {options.map((opt) => {
          const active = picked === opt.value;
          const isDefault = defaultValueSet.has(opt.value);
          const inStock = hasStock ? hasStock(opt.value) : undefined;
          return (
            <button
              key={opt.value}
              type="button"
              onClick={() => onPick(opt.value)}
              className={
                "group relative inline-flex items-center gap-2 px-3 h-9 rounded-full border text-[12px] transition-colors " +
                (active
                  ? "border-foreground bg-foreground text-background"
                  : "border-border bg-secondary/40 hover:bg-secondary text-foreground")
              }
              title={inStock === false ? `${opt.value} (当前组合在所有 DC 缺货)` : opt.value}
            >
              {inStock !== undefined && (
                <span
                  className={
                    "inline-block w-1.5 h-1.5 rounded-full flex-shrink-0 " +
                    (inStock ? "bg-success" : "bg-destructive")
                  }
                  aria-label={inStock ? "有货" : "缺货"}
                />
              )}
              <span className="font-semibold">{formatOptionDisplay(opt, groupKey)}</span>
              {isDefault && (
                <span className={"text-[9px] px-1.5 py-0.5 rounded-full " + (active ? "bg-background/20" : "bg-foreground/10")}>
                  默认
                </span>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
