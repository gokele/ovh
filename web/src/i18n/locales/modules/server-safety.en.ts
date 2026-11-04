/** Destructive server actions use stable, translated outcomes. */
export const serverSafetyApiEn = {
  PREWARM_INVALID: "Select an account, model, datacenters and a valid quantity to prepare carts.",
  PREWARM_UNAVAILABLE: "Carts could not be prepared yet. You can still create a sniping task.",
  "INVALID_SERVICE_NAME": "Invalid OVH service name. Select a service from the list.",
  "REQUEST_TOO_LARGE": "The request is too large. Reduce its content and try again.",
  "RESCUE_INVALID_REQUEST": "Invalid rescue request. No operation was performed.",
  "RESCUE_EMAIL_REQUIRED": "Enter a complete, valid email or explicitly select the account contact email.",
  "RESCUE_BOOT_CHANGED": "The boot configuration could not be confirmed against this window. No operation was performed. Reopen the window to check it.",
  "RETRACTION_ORDER_CHANGED": "Reopen the withdrawal window and confirm the order number before submitting.",
  "RETRACTION_UNAVAILABLE": "This order could not be confirmed as eligible for withdrawal. Nothing was submitted. Refresh and verify it in OVH.",
  "RETRACTION_SUBMITTED": "Order withdrawal was requested for every product and its data, including backups. Used service days may be deducted from the refund. Check the result in OVH."
};
