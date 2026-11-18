import { useMemo, useState } from "react";

/**
 * 热力格(scaffold-master HeatGrid 的 React 移植):
 * 7×24 星期×小时网格,按数值深浅点亮;悬停十字高亮 + 读数;
 * 级联入场只动 opacity/scale(CSS),reduced-motion 静止。
 * cells: [{ ts(ISO), weight? }] —— 前端聚合到 星期×小时 桶。
 */
export interface HeatCellInput {
  ts: string;
  weight?: number;
}

export function HeatGrid({ cells, loading }: { cells: HeatCellInput[]; loading?: boolean }) {
  const [hover, setHover] = useState<{ d: number; h: number } | null>(null);
  const DAYS = ["一", "二", "三", "四", "五", "六", "日"];

  const grid = useMemo(() => {
    const g: number[][] = Array.from({ length: 7 }, () => Array(24).fill(0));
    for (const c of cells) {
      const d = new Date(c.ts);
      if (Number.isNaN(d.getTime())) continue;
      // getDay: 0=周日 → 表格周一开头
      const row = (d.getDay() + 6) % 7;
      g[row][d.getHours()] += c.weight ?? 1;
    }
    return g;
  }, [cells]);

  const max = useMemo(() => Math.max(1, ...grid.flat()), [grid]);
  const total = useMemo(() => grid.flat().reduce((a, b) => a + b, 0), [grid]);
  const hoverVal = hover ? grid[hover.d][hover.h] : null;

  const tone = (v: number) => {
    if (v === 0) return { background: "var(--hover-bg)" };
    const r = v / max;
    // 三档深浅:青柠主色不同透明度(浅→深)
    const a = 0.25 + r * 0.65;
    return { background: `rgba(130, 221, 45, ${a.toFixed(2)})` };
  };

  return (
    <div className="heat" style={{ opacity: loading ? 0.55 : 1 }}>
      <div className="heat-readout">
        <span className="eyebrow">HEAT / 7×24</span>
        {hover ? (
          <span className="tnum">周{DAYS[hover.d]} {String(hover.h).padStart(2, "0")}:00 · {hoverVal}</span>
        ) : (
          <span className="tnum">{total}</span>
        )}
      </div>
      <div className="heat-day" style={{ display: "grid", gridTemplateColumns: "26px minmax(0, 1fr)", gap: 6 }}>
        {grid.map((row, d) => (
          <div key={d} style={{ display: "contents" }}>
            <span style={{ fontSize: 11, color: "var(--ink-faint)", alignSelf: "center", textAlign: "right" }}>{DAYS[d]}</span>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(24, minmax(0, 1fr))", gap: 3 }}>
              {row.map((v, h) => (
                <div
                  key={h}
                  className="heat-cell"
                  style={{
                    ...tone(v),
                    aspectRatio: "1 / 1",
                    borderRadius: 3,
                    outline: hover && (hover.d === d || hover.h === h) ? "1.5px solid var(--tick-strong)" : "none",
                    outlineOffset: "-1.5px",
                    transition: "opacity .5s ease " + ((d * 24 + h) % 40) * 8 + "ms",
                  }}
                  onMouseEnter={() => setHover({ d, h })}
                  onMouseLeave={() => setHover(null)}
                />
              ))}
            </div>
          </div>
        ))}
      </div>
      <div className="heat-foot">
        <span className="heat-legend">
          <i style={{ ...tone(0), width: 10, height: 10, borderRadius: 2, display: "inline-block" }} /> 0
          <i style={{ ...tone(max * 0.5), width: 10, height: 10, borderRadius: 2, display: "inline-block", marginLeft: 8 }} /> ½
          <i style={{ ...tone(max), width: 10, height: 10, borderRadius: 2, display: "inline-block", marginLeft: 8 }} /> max
        </span>
      </div>
    </div>
  );
}
