/**
 * 设置页共享小组件:Section 标题容器。
 * 原来是 settings.tsx 的私有组件,App 配对面板(独立文件)也要用,抽出来。
 */
import type { ReactNode } from "react";

export function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="space-y-5">
      <h2 className="text-base font-semibold">{title}</h2>
      <div className="space-y-4">{children}</div>
    </div>
  );
}
