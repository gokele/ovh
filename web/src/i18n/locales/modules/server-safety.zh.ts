/** Destructive server actions use stable, translated outcomes. */
export const serverSafetyApiZh = {
  "RESCUE_INVALID_REQUEST": "救援请求格式不正确,未执行操作。",
  "RESCUE_EMAIL_REQUIRED": "请填写完整有效的邮箱,或明确选择使用账户联系邮箱。",
  "RESCUE_BOOT_CHANGED": "无法确认启动配置仍与页面一致,未执行操作。请重新打开窗口核对。",
  "RETRACTION_ORDER_CHANGED": "请重新打开撤单窗口,确认订单号后再提交。",
  "RETRACTION_UNAVAILABLE": "无法确认此订单仍可撤回,未提交申请。请刷新并在 OVH 订单页核对。",
  "RETRACTION_SUBMITTED": "订单撤回申请已提交,影响整张订单的所有产品及数据（含备份）。退款可能扣除已使用天数,请在 OVH 订单页查看结果。"
};
