/**
 * 把用户手打的一串「用逗号分隔」的值切开。
 *
 * 为什么不能直接 `.split(",")`:这些输入框里的内容全是人打的，而中文输入法
 * 默认打出来的是**全角**标点。用户输入 `gra，bhs`，半角切法会得到一个词
 * `"gra，bhs"` —— 它匹配不上任何机房，而且不会有任何报错：订阅照样建，
 * 只是永远不触发。这类坏法比报错难查得多。
 *
 * 认这些分隔符：半角/全角逗号、顿号、半角/全角分号、换行。
 * 不按空格切 —— 空格更可能是用户在逗号后面顺手敲的，trim 掉即可。
 *
 * 后端有一份等价实现（server/internal/types/SplitList），两边必须保持一致：
 * 前端切好发过去，后端对直接打 API 的调用方也要做同样的宽容处理。
 */
export function splitList(input: string | null | undefined): string[] {
  if (!input) return [];
  return input
    .split(/[,，、;；\r\n\t]+/)
    // \s 在 JS 里涵盖全角空格 U+3000 和不换行空格 U+00A0（从网页复制时很常见）
    .map((s) => s.trim())
    .filter(Boolean);
}
