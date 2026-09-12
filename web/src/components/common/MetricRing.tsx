import { cn } from "@/lib/utils";
import { useIsNarrow } from "@/hooks/use-narrow";

/** 环形百分比卡片。左侧标签 + 副信息,右侧环 + 中心 %。
 *  纯 SVG 实现,无 recharts 依赖,极轻量。
 *  颜色按阈值切:< 60% 绿(成功色) / 60-85% 黄(警告) / > 85% 红(危险)
 */
export function MetricRing({
  label,
  subLabel,
  percent,
  size,
}: {
  label: string;
  subLabel: string;
  percent: number;
  size?: number;
}) {
  // 手机端三个环并排,96px 的环会把格子撑满没有留白
  const narrow = useIsNarrow();
  size = size ?? (narrow ? 68 : 96);
  const safePct = Math.max(0, Math.min(100, percent));
  const tone = safePct >= 85 ? "danger" : safePct >= 60 ? "warning" : "success";

  // 圆环参数:留 8px 内边距,3/4 圆弧从上偏左开始顺时针
  const stroke = 8;
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  // 用 270° 弧（3/4 圆），缺口在底部正中
  const visibleArc = 0.75 * c;
  const filled = (safePct / 100) * visibleArc;
  const dashArray = `${filled} ${c}`;

  const toneStroke =
    tone === "danger"
      ? "stroke-destructive"
      : tone === "warning"
        ? "stroke-amber-500"
        : "stroke-emerald-500";
  const toneText =
    tone === "danger"
      ? "text-destructive"
      : tone === "warning"
        ? "text-amber-600 dark:text-amber-400"
        : "text-emerald-600 dark:text-emerald-400";

  return (
    // 手机端三个环并排,每格只有 ~118px —— 左文右环那套横排会把两边都压扁,
    // 所以窄屏改成竖排:环在上、文字在下,居中对齐。sm+ 回到原来的横排。
    <div className="flex flex-col-reverse sm:flex-row items-center sm:justify-between gap-2 sm:gap-4 px-2.5 py-3 sm:px-5 sm:py-4 h-full">
      <div className="min-w-0 text-center sm:text-left">
        <div className="text-[11px] sm:text-[12px] text-muted-foreground truncate">{label}</div>
        <div className={cn("mt-0.5 sm:mt-1 text-[12px] sm:text-[18px] font-semibold tabular-nums truncate", toneText)}>
          {subLabel}
        </div>
      </div>
      <div
        className="relative flex-shrink-0"
        style={{ width: size, height: size }}
      >
        {/* 旋转必须作用在内部的 <g> 上,不能加在 <svg> 元素本身。
            加在 svg 上的话,旋转 135° 会把它的**包围盒**撑成 size×√2
            (96px 的环量出来是 136px),超出父容器 20px ——
            画出来的圆没变形(圆旋转还是圆),但那 20px 会参与布局计算,
            在窄容器里能把兄弟元素挤走甚至撑出横向滚动条。
            放进 <g transform> 则是在 viewBox 坐标系里转,svg 本身仍是 size×size。 */}
        <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
          <g transform={`rotate(135 ${size / 2} ${size / 2})`}>
          {/* 背景轨道 */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            strokeWidth={stroke}
            strokeLinecap="round"
            className="stroke-border"
            strokeDasharray={`${visibleArc} ${c}`}
          />
          {/* 进度 */}
          <circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            strokeWidth={stroke}
            strokeLinecap="round"
            className={cn(toneStroke, "transition-[stroke-dasharray] duration-700")}
            strokeDasharray={dashArray}
          />
          </g>
        </svg>
        <div className="absolute inset-0 flex items-center justify-center">
          <span className={cn("text-[20px] font-semibold tabular-nums", toneText)}>
            {Math.round(safePct)}
            <span className="text-[11px] ml-0.5">%</span>
          </span>
        </div>
      </div>
    </div>
  );
}
