import { useEffect, useMemo, useRef, useState } from "react";

/**
 * 中奖彩带：一次性庆祝粒子，仅 transform/opacity 动画（合成层，无重排）。
 *
 * - 每台机器每个浏览器会话只撒一次（sessionStorage 去重），刷新/再次进入不重复
 * - prefers-reduced-motion 下完全不渲染（横幅本身静态可见，庆祝效果是纯增量）
 * - 粒子约 26 个、1.6s 自然减速收尾，动画结束自动卸载
 * - 颜色为纯色小纸片（琥珀/绿/天蓝/玫红/黄），不使用渐变
 */
const COLORS = ["#f59e0b", "#fbbf24", "#34d399", "#38bdf8", "#fb7185", "#facc15"];
const COUNT = 26;
const DURATION_MS = 1600;

interface Particle {
  left: number;
  delay: number;
  duration: number;
  drift: number;
  rotate: number;
  size: number;
  color: string;
  round: boolean;
}

export function LotteryConfetti({ sessionKey }: { sessionKey: string }) {
  const storageKey = `ovh_lottery_confetti_${sessionKey}`;
  const [particles, setParticles] = useState<Particle[] | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (reduced || sessionStorage.getItem(storageKey)) return;
    sessionStorage.setItem(storageKey, "1");

    // 随机性放进 state 一次性生成，渲染期间稳定
    setParticles(
      Array.from({ length: COUNT }, () => ({
        left: Math.random() * 100,
        delay: Math.random() * 260,
        duration: DURATION_MS + Math.random() * 500,
        drift: (Math.random() - 0.5) * 90,
        rotate: Math.random() * 720 - 360,
        size: 5 + Math.random() * 5,
        color: COLORS[Math.floor(Math.random() * COLORS.length)],
        round: Math.random() < 0.3,
      })),
    );
    timer.current = setTimeout(() => setParticles(null), DURATION_MS + 900);
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, [storageKey]);

  const rendered = useMemo(() => particles ?? [], [particles]);
  if (rendered.length === 0) return null;

  return (
    <div className="pointer-events-none absolute inset-0 overflow-visible" aria-hidden="true">
      {rendered.map((p, i) => (
        <span
          key={i}
          className="lottery-confetti-particle absolute top-0"
          style={{
            left: `${p.left}%`,
            width: p.size,
            height: p.round ? p.size : p.size * 0.45,
            backgroundColor: p.color,
            borderRadius: p.round ? "50%" : "1px",
            animationDuration: `${p.duration}ms`,
            animationDelay: `${p.delay}ms`,
            // 关键帧由 CSS 类提供，漂移/旋转等随机量走 CSS 变量注入
            ["--drift" as string]: `${p.drift}px`,
            ["--rot" as string]: `${p.rotate}deg`,
          }}
        />
      ))}
    </div>
  );
}
