import LocalAuthentication

/**
 * Face ID / Touch ID 闸门:危险动作的物理确认层。
 * 设备没录生物识别时回退系统密码(LAPolicy 默认行为),不静默放行。
 */
enum Biometric {
    /** 弹一次验证;通过返回 true,取消/失败返回 false */
    static func require(_ actionName: String) async -> Bool {
        let ctx = LAContext()
        var err: NSError?
        // 没硬件也没密码 → 没有可用的闸,直接放行会让"闸"名存实亡;
        // 但锁死用户更糟 —— 返回 true 并由调用方的确认对话兜底(它已经在前面拦了一道)
        guard ctx.canEvaluatePolicy(.deviceOwnerAuthentication, error: &err) else {
            return true
        }
        do {
            return try await ctx.evaluatePolicy(
                .deviceOwnerAuthentication,
                localizedReason: "确认\(actionName)"
            )
        } catch {
            return false
        }
    }
}
