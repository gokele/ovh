/**
 * 生物识别闸门(Face ID / Touch ID / Android 生物识别):
 * 盖在危险动作(硬重启/救援切换)之前 —— 网页端做不到的一层物理确认。
 * 设备没录入生物识别时自动退回系统的设备密码(PIN)验证,不静默放行。
 */
import * as LocalAuthentication from "expo-local-authentication";

export interface BiometricResult {
  ok: boolean;
  /** 失败/取消的 人话原因 */
  reason?: string;
}

/** 设备是否支持任何生物识别硬件 */
export async function biometricsAvailable(): Promise<boolean> {
  const has = await LocalAuthentication.hasHardwareAsync();
  const enrolled = await LocalAuthentication.isEnrolledAsync();
  return has && enrolled;
}

/**
 * 弹一次生物识别;成功返回 ok。
 * - 没有生物识别 → 走 authenticateAsync 的默认回退(设备密码)
 * - 用户取消 / 多次失败 → ok:false + 原因,调用方不执行动作
 */
export async function requireBiometric(actionName: string): Promise<BiometricResult> {
  try {
    const res = await LocalAuthentication.authenticateAsync({
      promptMessage: `确认${actionName}`,
      cancelLabel: "取消",
      disableDeviceFallback: false, // 设备密码兜底:生物识别没录入的老设备不能被锁死在门外
    });
    if (res.success) return { ok: true };
    return { ok: false, reason: res.error === "user_cancel" ? "已取消" : "验证未通过" };
  } catch {
    return { ok: false, reason: "生物识别不可用" };
  }
}
