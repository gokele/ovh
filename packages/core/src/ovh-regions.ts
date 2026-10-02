/**
 * OVH 大区（EU / US / CA）在前端的唯一权威判定。
 *
 * 为什么要集中：EU / US / CA 是三套互不相通的系统（目录、价格、库存、购物车、账户全独立），
 * 前端有三处地方需要知道"当前账户在哪个区"——可用性接口打哪个站点、哪些功能整块不存在、
 * 提示文案怎么写。以前这三处各写一份 `endpoint === "ovh-us"`：
 *   1. endpoint 是用户在设置页可填的自由字符串，还有 kimsufi-* / soyoustart-* 品牌别名
 *      （kimsufi-ca / soyoustart-ca 都是 CA 站点），散装比较漏一种写法门控就失效；
 *   2. 加区（ovh-ca）在散装比较里一律被当成"非美区"=欧洲，于是加区账户会去查欧洲站点。
 *
 * 判定规则逐条对齐后端 internal/ovh/helpers.go 的 EndpointRegion / APIBaseURLForRegion，
 * 两边必须同时改，否则前端绿灯、后端 404。
 */

export type OvhRegion = "EU" | "US" | "CA";

/** endpoint（go-ovh 名）→ 大区。空值按 EU（OVH 默认站点，覆盖面最广），与后端 EndpointRegion 一致。 */
export function endpointRegion(endpoint?: string | null): OvhRegion {
  const e = (endpoint || "").trim().toLowerCase();
  if (e === "ovh-us") return "US";
  if (e.endsWith("-ca")) return "CA"; // ovh-ca / kimsufi-ca / soyoustart-ca
  return "EU";
}

/** 是否美区账户。US OVHcloud 是独立公司，一整批端点在它的 schema 里根本不存在。 */
export function isUsEndpoint(endpoint?: string | null): boolean {
  return endpointRegion(endpoint) === "US";
}

/**
 * 大区 → OVH 公开 REST API 站点（不带 /v1、/1.0）。
 * 对齐后端 APIBaseURLForRegion。前端直查公开接口（可用性）时必须用账户所在站点：
 * 实测 https://eu.api.ovh.com 与 https://ca.api.ovh.com 的
 * /v1/dedicated/server/datacenter/availabilities 返回完全相同（244 个 planCode），
 * 而 https://api.us.ovhcloud.com 是另一份数据（423 个 planCode，只有 134 个与 EU 重合，
 * 且多出 vin / hil 两个机房）——拿 EU 的库存去渲染美区账户，289 个机型直接查无此机。
 */
export function apiBaseUrlForRegion(region: OvhRegion): string {
  switch (region) {
    case "US":
      return "https://api.us.ovhcloud.com";
    case "CA":
      return "https://ca.api.ovh.com";
    default:
      return "https://eu.api.ovh.com";
  }
}

/** endpoint → 公开 API 站点，等价于 apiBaseUrlForRegion(endpointRegion(endpoint)) */
export function apiBaseUrlForEndpoint(endpoint?: string | null): string {
  return apiBaseUrlForRegion(endpointRegion(endpoint));
}

/** 后端返回的 region 字段（"US" / "CA" / "EU" 这类原始码）→ 中文名。
 *  认不出来时返回空串，让调用方退回自己那份文案，而不是硬印一个可能错的大区名。 */
export function regionLabelOf(code?: string | null): string {
  const c = (code || "").trim().toUpperCase();
  if (c === "US" || c === "CA" || c === "EU") return regionLabel(c as OvhRegion);
  return "";
}

/** 大区的中文名，用于提示文案 */
export function regionLabel(region: OvhRegion): string {
  switch (region) {
    case "US":
      return "美区";
    case "CA":
      return "加区";
    default:
      return "欧区";
  }
}
