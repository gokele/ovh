import type { Config } from "tailwindcss";

/**
 * Tailwind 配置：
 * - darkMode: class（手动切换主题）
 * - 颜色、圆角、间距等通通走 CSS 变量（globals.css 定义），组件不硬编码 hex
 * - 全局禁用阴影（设计规范决定）
 */
export default {
  darkMode: ["class"],
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    container: {
      center: true,
      padding: "1.5rem",
      screens: { "2xl": "1400px" },
    },
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
          // 实底专用:深色下 --destructive 是文字优化的亮红(95 处 text-destructive
          // 要 4.5+),按钮白字跟着亮红会跌破 —— 实底一律用 strong
          strong: "hsl(var(--destructive-strong))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        success: "hsl(var(--success))",
        warning: "hsl(var(--warning))",
        "button-primary": {
          DEFAULT: "hsl(var(--button-primary))",
          foreground: "hsl(var(--button-primary-foreground))",
          hover: "hsl(var(--button-primary-hover))",
          active: "hsl(var(--button-primary-active))",
          disabled: "hsl(var(--button-primary-disabled))",
          "disabled-foreground": "hsl(var(--button-primary-disabled-foreground))",
        },
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
      boxShadow: {
        none: "none",
        DEFAULT: "none",
        sm: "none",
        md: "none",
        lg: "none",
        xl: "none",
      },
      fontFamily: {
        sans: ["Inter", "-apple-system", "BlinkMacSystemFont", "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", "sans-serif"],
        mono: ["SF Mono", "Monaco", "Cascadia Code", "Roboto Mono", "Consolas", "monospace"],
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
} satisfies Config;
