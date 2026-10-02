/**
 * 金额显示的唯一入口。
 *
 * 为什么不能兜底成 EUR：币种是按 **子公司** 定的，不是按站点定的。
 * 实测 /v1/order/catalog/public/eco?ovhSubsidiary=X 的 locale.currencyCode：
 *   IE/FR/DE/NL/PT/FI/CZ/LT = EUR   GB = GBP   PL = PLN   MA = MAD   TN = TND   SN = XOF
 *   CA/QC = CAD   US/WE/WS = USD   SG = SGD   AU = AUD   IN = INR
 * 后端这一轮已经把"拿不到 currencyCode 就留空"落实了（internal/price/price.go、
 * internal/purchase/purchase.go 都明确写了"不再默认 EUR"），前端若再 `|| "EUR"`
 * 等于把后端的诚实留空重新伪造成欧元——美区/加区的订单会被显示成 €。
 * 所以：币种缺失时只显示数字，符号和币种码一个都不编。
 */

/** 币种符号表。表里没有的币种不猜符号，改成"数字 + 币种码"后缀显示。 */
const CURRENCY_SYMBOLS: Record<string, string> = {
  EUR: "€",
  USD: "$",
  GBP: "£",
  CAD: "CA$",
  SGD: "S$",
  AUD: "A$",
  INR: "₹",
};

/** 币种码 → 符号；未知或空一律返回空串（绝不回落成 €） */
export function currencySymbol(code?: string | null): string {
  const c = (code || "").trim().toUpperCase();
  if (!c) return "";
  return CURRENCY_SYMBOLS[c] || "";
}

/**
 * 金额 + 币种的统一渲染：
 *   有符号   → `€42.99`
 *   无符号   → `42.99 PLN`
 *   没币种   → `42.99`（不带任何货币标记，配合 CURRENCY_UNKNOWN_HINT 说明原因）
 */
export function formatMoney(value: number, code?: string | null, digits = 2): string {
  const n = value.toFixed(digits);
  const c = (code || "").trim().toUpperCase();
  if (!c) return n;
  const sym = CURRENCY_SYMBOLS[c];
  return sym ? `${sym}${n}` : `${n} ${c}`;
}

/** 币种缺失时给 title / 说明用的文案，别让用户以为是页面少渲染了一截 */
export const CURRENCY_UNKNOWN_HINT =
  "OVH 未返回币种（currencyCode 为空）。币种按账户子公司定：US/WE/WS=USD、CA/QC=CAD、SG=SGD、AU=AUD、GB=GBP，不能默认按欧元读。";
