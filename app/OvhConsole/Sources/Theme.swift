import SwiftUI

/**
 * 主题 token:浅色默认(白色优先),深色跟随系统 —— 与 web 端两套校准值一致。
 * 色值来自 web/src/styles/globals.css 的 :root 与 .dark(WCAG AA 已校准)。
 */
enum Scheme {
    static func tokens(_ dark: Bool) -> Tokens {
        dark ? Tokens.dark : Tokens.light
    }
}

struct Tokens {
    let bg: String
    let surface: String
    let surfaceMuted: String
    let border: String
    let fg: String
    let muted: String
    let faint: String
    let success: String
    let warning: String
    let danger: String
    let info: String

    /// hex → Color(RN 端没有UIColor(hex:),这里做一次解析)
    func color(_ hex: String) -> Color {
        var v: UInt64 = 0
        Scanner(string: String(hex.dropFirst())).scanHexInt64(&v)
        return Color(
            red: Double((v >> 16) & 0xFF) / 255,
            green: Double((v >> 8) & 0xFF) / 255,
            blue: Double(v & 0xFF) / 255
        )
    }

    static let light = Tokens(
        bg: "#FFFFFF", surface: "#FFFFFF", surfaceMuted: "#F5F5F5",
        border: "#E5E5E5", fg: "#0A0A0A", muted: "#666666", faint: "#8F8F8F",
        success: "#157F3A", warning: "#AC4705", danger: "#BE1B1B", info: "#1E6EE8"
    )
    static let dark = Tokens(
        bg: "#0F0F0F", surface: "#161616", surfaceMuted: "#1D1D1D",
        border: "#2E2E2E", fg: "#F5F5F5", muted: "#9A9A9A", faint: "#6B6B6B",
        success: "#4CC97E", warning: "#E8A33D", danger: "#F26D6D", info: "#5B9CF8"
    )
}

/// 环境注入,子视图 @EnvironmentObject 式取用
final class Theme: ObservableObject {
    @Published var dark: Bool
    var t: Tokens { Scheme.tokens(dark) }
    init() { dark = false }
}
