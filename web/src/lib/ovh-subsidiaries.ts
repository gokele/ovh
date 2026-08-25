import { endpointRegion } from "@/lib/ovh-regions";

/**
 * OVH 子公司列表（每个 subsidiary 对应独立目录 + 独立币种 + 独立税率）。
 * - endpoint 决定 API host（eu / us / ca）
 * - subsidiary 决定使用哪份目录、用什么币、加多少税
 */
export interface OvhSubsidiary {
  code: string;
  endpoint: "ovh-eu" | "ovh-us" | "ovh-ca";
  /** 中文标签（地区名 + 币种 + VAT/GST 提示） */
  label: string;
  currency: string;
}

/**
 * 常见的 OVH subsidiary。
 * label 只写国家 + 币种，**税率不在这里写死**，因为 catalog 接口本身返回 locale.taxRate，
 * 用 API 实际值更准（IE 实测是 23% 不是 0%，CA 实测是 0% 不是 GST/PST 另计等）。
 */
export const OVH_SUBSIDIARIES: OvhSubsidiary[] = [
  // 欧洲（eu.api.ovh.com）
  { code: "IE", endpoint: "ovh-eu", label: "爱尔兰 · EUR", currency: "EUR" },
  { code: "FR", endpoint: "ovh-eu", label: "法国 · EUR", currency: "EUR" },
  { code: "DE", endpoint: "ovh-eu", label: "德国 · EUR", currency: "EUR" },
  { code: "GB", endpoint: "ovh-eu", label: "英国 · GBP", currency: "GBP" },
  { code: "IT", endpoint: "ovh-eu", label: "意大利 · EUR", currency: "EUR" },
  { code: "ES", endpoint: "ovh-eu", label: "西班牙 · EUR", currency: "EUR" },
  { code: "PL", endpoint: "ovh-eu", label: "波兰 · PLN", currency: "PLN" },
  { code: "NL", endpoint: "ovh-eu", label: "荷兰 · EUR", currency: "EUR" },
  { code: "PT", endpoint: "ovh-eu", label: "葡萄牙 · EUR", currency: "EUR" },
  { code: "FI", endpoint: "ovh-eu", label: "芬兰 · EUR", currency: "EUR" },
  { code: "CZ", endpoint: "ovh-eu", label: "捷克 · EUR", currency: "EUR" },

  // 北美（api.us.ovhcloud.com）。US 独立产品线（plans 137 / addons 768，比 EU 多 40%）
  { code: "US", endpoint: "ovh-us", label: "美国 · USD（独立产品线）", currency: "USD" },

  // 加拿大 / 亚太（ca.api.ovh.com）
  { code: "CA", endpoint: "ovh-ca", label: "加拿大 · CAD", currency: "CAD" },
  { code: "QC", endpoint: "ovh-ca", label: "魁北克 · CAD", currency: "CAD" },
  { code: "ASIA", endpoint: "ovh-ca", label: "亚太 · USD", currency: "USD" },
  { code: "SG", endpoint: "ovh-ca", label: "新加坡 · SGD", currency: "SGD" },
  { code: "AU", endpoint: "ovh-ca", label: "澳大利亚 · AUD", currency: "AUD" },
  { code: "IN", endpoint: "ovh-ca", label: "印度 · INR", currency: "INR" },
];

/** 根据当前 endpoint 推断默认 subsidiary。
 *  对齐后端 ovh.DefaultSubsidiaryForEndpoint;大区判定走 lib/ovh-regions,
 *  免得这里又漏掉 kimsufi-ca / soyoustart-ca 这类同区别名(它们也属于 CA 站点)。 */
export function defaultSubsidiaryForEndpoint(endpoint: string | undefined): string {
  switch (endpointRegion(endpoint)) {
    case "US":
      return "US";
    case "CA":
      return "CA";
    default:
      return "IE";
  }
}
