import type { LucideIcon } from "lucide-react";

interface PageHeaderProps {
  icon: LucideIcon;
  title: string;
  description?: string;
  action?: React.ReactNode;
}

/**
 * 统一的页面顶部 header：左侧 icon 方块 + 标题 + 描述，右侧操作按钮。
 *
 * 手机端（< sm）刻意瘦一圈：
 * - 不画 icon 方块 —— 它在 390px 宽里吃掉 40px 却不带信息，标题本身就是标识
 * - 标题降到 17px，描述压成一行省略（描述多是「这页的数据是怎么来的」这类背景，
 *   要看的人会看，但不该在首屏占两行）
 * - 操作按钮跟标题挤同一行（以前它们各占一行，白吃 52px）
 * 这几条加起来在手机上省掉约 90px —— 原来光页头就占首屏的 1/10。
 */
export function PageHeader({ icon: Icon, title, description, action }: PageHeaderProps) {
  return (
    <div className="flex items-start justify-between gap-2 sm:gap-4">
      <div className="flex items-center gap-2.5 sm:gap-3 min-w-0 flex-1">
        {/* 图标方块只在 sm+ 出现 */}
        <div className="hidden sm:flex w-11 h-11 rounded-xl bg-secondary items-center justify-center flex-shrink-0">
          <Icon className="w-6 h-6 text-foreground" strokeWidth={1.75} />
        </div>
        <div className="min-w-0">
          {/* 标题在手机端由顶栏承担,这里再写一遍就是同一句话占两行。
              sm+ 顶栏是完整面包屑,标题才回到页面里。 */}
          <h1 className="hidden sm:block text-[28px] font-bold text-foreground leading-tight tracking-tight truncate">
            {title}
          </h1>
          {description && (
            <p className="text-[11px] sm:text-[14px] text-muted-foreground mt-0.5 line-clamp-1">
              {description}
            </p>
          )}
        </div>
      </div>
      {/* 按钮容器不能用 flex-shrink-0:它让容器保持 max-content 宽度,
          于是 flex-wrap 永远不触发 —— VPS 补货页四个按钮在手机上
          422px 塞进 366px,把整页撑出横向滚动条。
          min-w-0 + 允许收缩,按钮才会真的换行。 */}
      {action && (
        <div className="flex items-center gap-1.5 sm:gap-2 flex-wrap justify-end min-w-0">
          {action}
        </div>
      )}
    </div>
  );
}
