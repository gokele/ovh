/**
 * 库存可用性判据 —— 必须和后端 ovh.IsAvailableForOrder 用同一套规则。
 *
 * OVH 的 dedicated.AvailabilityEnum（三区一致）:
 *   ['120H','1440H','1H-high','1H-low','2160H','240H','24H','480H','720H','72H',
 *    'comingSoon','unavailable','unknown']
 *
 * 只有 \d+H(-high|-low)? 这一族是「多久能交付」的承诺，也就是真能下单。
 * comingSoon 是「即将上线但还没开卖」，unknown 是「OVH 没给状态」，两个都下不了单。
 *
 * 以前前端写的是黑名单 `s !== "unavailable" && s !== "unknown"` ——
 * 那会把 comingSoon 判成有货：列表上亮绿点、抢购对话框允许下单，
 * 而后端按白名单判定不可订，任务进队列后空转。前端说有、后端说没有，
 * 用户完全无从判断。后端 helpers.go 的注释里就明确禁止这种写法。
 */
const ORDERABLE = /^\d+H(-high|-low)?$/;

/** 这个可用性取值是否真的可以下单 */
export function isOrderable(availability?: string | null): boolean {
  return !!availability && ORDERABLE.test(availability);
}

/** 一组机房状态里有没有任意一个可下单 */
export function anyOrderable(values: Array<string | undefined | null>): boolean {
  return values.some(isOrderable);
}
