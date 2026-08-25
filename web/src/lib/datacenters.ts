/**
 * OVH 标准数据中心列表（前端固定的 12 个 + 别名映射）
 * - code：前端显示用的小写代码（如 mum）
 * - apiCode：OVH 后端 API 返回的代码（如 ynm，仅当与 code 不同时设置）
 * - name：中文城市
 * - region：所在国家 / 区域
 */
export interface DataCenter {
  code: string;
  apiCode?: string;
  name: string;
  region: string;
}

export const OVH_DATACENTERS: DataCenter[] = [
  // GRA 是 Gravelines(格拉沃利讷),不是"格拉夫尼茨";名称与后端 catalog.dcCityMap 保持一致
  { code: "gra", name: "格拉沃利讷", region: "法国" },
  { code: "sbg", name: "斯特拉斯堡", region: "法国" },
  { code: "rbx", name: "鲁贝", region: "法国" },
  // 巴黎有三个可用区,OVH 的可用性接口按 eu-west-par-a/b/c 分别返回库存。
  // 以前这张表没有它们,导致巴黎的货在前端既看不见也选不了(实测三区都有真实库存)。
  { code: "par-a", apiCode: "eu-west-par-a", name: "巴黎 A", region: "法国" },
  { code: "par-b", apiCode: "eu-west-par-b", name: "巴黎 B", region: "法国" },
  { code: "par-c", apiCode: "eu-west-par-c", name: "巴黎 C", region: "法国" },
  { code: "bhs", name: "博阿尔诺", region: "加拿大" },
  // 多伦多同理,可用性接口返回的是长码 ca-east-tor-a
  { code: "tor", apiCode: "ca-east-tor-a", name: "多伦多", region: "加拿大" },
  { code: "mum", apiCode: "ynm", name: "孟买", region: "印度" },
  { code: "waw", name: "华沙", region: "波兰" },
  { code: "fra", name: "法兰克福", region: "德国" },
  { code: "lon", name: "伦敦", region: "英国" },
  { code: "hil", name: "俄勒冈", region: "美国西部" },
  { code: "vin", name: "弗吉尼亚", region: "美国东部" },
  { code: "sgp", name: "新加坡", region: "新加坡" },
  { code: "syd", name: "悉尼", region: "澳大利亚" },
];

/** 从可用性 map（res.data.availability）里查某个 DC 的状态，自动处理 mum/ynm 别名 */
export function lookupDcStatus(
  availMap: Record<string, string> | undefined,
  dc: DataCenter
): string | undefined {
  if (!availMap) return undefined;
  return availMap[dc.code] || (dc.apiCode ? availMap[dc.apiCode] : undefined);
}

/**
 * 该机型在**当前账户所在站点**实际可选的机房。
 *
 * 以前所有地方都渲染写死的 16 个机房,于是欧区机型也会列出 HIL / VIN(美国机房)——
 * 那两个对它永远是红点,因为欧区目录里根本没有它们;反过来美区账户也会看到一堆
 * 自己买不到的欧洲机房。可用性接口是按账户站点查的,它返回哪些机房,就只显示哪些。
 *
 * 返回顺序沿用 OVH_DATACENTERS 的固定顺序(界面稳定),接口里出现而表里没有的
 * 机房码原样兜底展示,不吞掉。
 */
export function datacentersForPlan(
  availMap: Record<string, Record<string, string>> | undefined,
  planCode: string
): DataCenter[] {
  const codes = availMap?.[planCode];
  // 可用性还没到手 → 先按完整表渲染,避免闪一下空白
  if (!codes || Object.keys(codes).length === 0) return OVH_DATACENTERS;

  const has = (dc: DataCenter) => (dc.apiCode && codes[dc.apiCode] !== undefined) || codes[dc.code] !== undefined;
  const known = OVH_DATACENTERS.filter(has);

  // 接口给了、但本地表里没有的机房码:原样显示,别让用户看不到真实在售的机房
  const knownApiCodes = new Set(OVH_DATACENTERS.flatMap((d) => [d.code, d.apiCode].filter(Boolean) as string[]));
  const extras: DataCenter[] = Object.keys(codes)
    .filter((c) => !knownApiCodes.has(c))
    .map((c) => ({ code: c, name: c.toUpperCase(), region: "—" }));

  return [...known, ...extras];
}
