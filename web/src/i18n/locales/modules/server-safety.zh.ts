/** Destructive server actions use stable, translated outcomes. */
export const serverSafetyApiZh = {
  PREWARM_INVALID: "请选择账户、型号和机房，并填写有效的备车数量。",
  PREWARM_UNAVAILABLE: "暂时无法备车，可以照常创建抢购任务。",
  "INVALID_SERVICE_NAME": "OVH 服务名无效，请从列表重新选择。",
  "REQUEST_TOO_LARGE": "请求体过大，请减少提交内容后重试。",
  "RESCUE_INVALID_REQUEST": "救援请求格式不正确,未执行操作。",
  "RESCUE_EMAIL_REQUIRED": "请填写完整有效的邮箱,或明确选择使用账户联系邮箱。",
  "RESCUE_BOOT_CHANGED": "无法确认启动配置仍与页面一致,未执行操作。请重新打开窗口核对。",
  "RETRACTION_ORDER_CHANGED": "请重新打开撤单窗口,确认订单号后再提交。",
  "RETRACTION_UNAVAILABLE": "无法确认此订单仍可撤回,未提交申请。请刷新并在 OVH 订单页核对。",
  "RETRACTION_SUBMITTED": "订单撤回申请已提交,影响整张订单的所有产品及数据（含备份）。退款可能扣除已使用天数,请在 OVH 订单页查看结果。"
};
