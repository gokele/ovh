/**
 * 下单规模上限。必须和后端 server/internal/types 里的同名常量保持一致 ——
 * 后端是权威(EnqueueItems 会拒),前端这份只是为了**提前**告诉用户,
 * 而不是让他发出几百个请求之后才撞墙。
 */
export const MAX_ORDER_QUANTITY = 20; // 单个机房最多几台
export const MAX_ORDER_FANOUT = 60; // 一次操作最多创建多少个任务

/**
 * 把「机房数 × 每机房数量」收进上限内。
 *
 * 要防的是多打一个数字:以前这里只有 `Math.max(1, qty)`,没有上界 ——
 * 填 9999、选 5 个机房就是近 5 万次串行 POST,浏览器直接卡死,
 * 而每条任务都是一次真实的下单尝试。
 */
export function clampOrderPlan(dcCount: number, quantity: number) {
  const qty = Math.min(MAX_ORDER_QUANTITY, Math.max(1, Math.floor(quantity) || 1));
  const maxQtyByFanout = dcCount > 0 ? Math.floor(MAX_ORDER_FANOUT / dcCount) : qty;
  // 机房本身就超过扇出上限时至少保住 1 台/机房,由后端的总量闸门兜最后一道
  const finalQty = Math.max(1, Math.min(qty, maxQtyByFanout));
  return {
    quantity: finalQty,
    total: dcCount * finalQty,
    clamped: finalQty !== Math.max(1, Math.floor(quantity) || 1),
  };
}
