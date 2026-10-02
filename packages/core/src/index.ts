/**
 * @ovh-console/core 出口。
 * 纯逻辑(无 React / DOM / axios):可用性白名单、机房表、子公司与大区、
 * 分词、下单上限、金额渲染、区域配色、fetch 客户端、读侧共享类型。
 */
export * from "./availability";
export * from "./split-list";
export * from "./datacenters";
export * from "./order-limits";
export * from "./money";
export * from "./ovh-regions";
export * from "./ovh-subsidiaries";
export * from "./zone";
export * from "./api-client";
export * from "./types";
