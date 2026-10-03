import SwiftUI

/**
 * 设计系统(深色 OLED 控制台风格,可切浅色):
 * - bg 近黑深蓝、卡片深蓝灰、绿色 accent —— 运维控制台的"夜班仪表"气质
 * - token 全语义化,组件不写裸色
 * - 图标一律 SF Symbols;间距走 4/8 节奏
 */
final class Tokens {
    let dark: Bool
    init(dark: Bool) { self.dark = dark }

    // 表面
    let bg = "bg", surface = "surface", surfaceMuted = "surfaceMuted", border = "border"
    // 文字
    let fg = "fg", muted = "muted", faint = "faint"
    // 语义色(accent = 品牌主操作绿)
    let accent = "accent", success = "success", danger = "danger", warning = "warning", info = "info"

    func color(_ name: String) -> Color {
        if dark {
            switch name {
            case bg: return Color(hex: 0x020617)
            case surface: return Color(hex: 0x0F172A)
            case surfaceMuted: return Color(hex: 0x1E293B)
            case border: return Color(hex: 0x263349)
            case fg: return Color(hex: 0xF8FAFC)
            case muted: return Color(hex: 0x94A3B8)
            case faint: return Color(hex: 0x5B6B84)
            case accent, success: return Color(hex: 0x22C55E)
            case danger: return Color(hex: 0xF87171)
            case warning: return Color(hex: 0xFBBF24)
            case info: return Color(hex: 0x38BDF8)
            default: return Color(hex: 0xF8FAFC)
            }
        } else {
            switch name {
            case bg: return Color(hex: 0xF4F6FA)
            case surface: return Color(hex: 0xFFFFFF)
            case surfaceMuted: return Color(hex: 0xEBEFF5)
            case border: return Color(hex: 0xDCE3EC)
            case fg: return Color(hex: 0x0F172A)
            case muted: return Color(hex: 0x55647C)
            case faint: return Color(hex: 0x8A99AE)
            case accent, success: return Color(hex: 0x15803D)
            case danger: return Color(hex: 0xDC2626)
            case warning: return Color(hex: 0xB45309)
            case info: return Color(hex: 0x0369A1)
            default: return Color(hex: 0x0F172A)
            }
        }
    }
}

extension Color {
    init(hex: UInt32) {
        self.init(.sRGB,
                  red: Double((hex >> 16) & 0xFF) / 255,
                  green: Double((hex >> 8) & 0xFF) / 255,
                  blue: Double(hex & 0xFF) / 255,
                  opacity: 1)
    }
}

/// 外观模式:跟随系统 / 深色 / 浅色(与 web 设置同语义,UserDefaults 持久化)
@MainActor
final class Theme: ObservableObject {
    enum Mode: String { case system, dark, light }
    static let KEY = "ovh_appearance"
    @Published var mode: Mode {
        didSet { UserDefaults.standard.set(mode.rawValue, forKey: Self.KEY) }
    }
    /// 系统当前是否深色 —— 由根视图从 SwiftUI 环境同步(onAppear/onChange)。
    /// 不能用 UITraitCollection.current:它在 body 计算的部分时机
    /// (启动、sheet 弹出等)拿到 unspecified,导致深色下随机渲染出浅色块。
    @Published var systemDark: Bool = false

    var dark: Bool {
        if mode == .dark { return true }
        if mode == .light { return false }
        return systemDark
    }
    var t: Tokens { Tokens(dark: dark) }

    init() {
        // 首次安装(没存过偏好)默认深色:运维控制台的主视觉;
        // 用户手动选过就跟用户走
        if UserDefaults.standard.string(forKey: Self.KEY) == nil {
            UserDefaults.standard.set(Mode.dark.rawValue, forKey: Self.KEY)
        }
        let raw = UserDefaults.standard.string(forKey: Self.KEY) ?? Mode.dark.rawValue
        mode = Mode(rawValue: raw) ?? .dark
    }
}

// MARK: - 页面骨架件

/// 大标题页头(每个 Tab 页共用):标题 + 副题 + 右侧动作
struct PageHeader: View {
    @EnvironmentObject var theme: Theme
    let title: String
    var subtitle: String = ""
    var trailing: AnyView? = nil
    var t: Tokens { theme.t }

    var body: some View {
        HStack(alignment: .bottom) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.system(size: 25, weight: .bold)).foregroundColor(t.color(t.fg))
                if !subtitle.isEmpty {
                    Text(subtitle).font(.system(size: 11)).foregroundColor(t.color(t.muted))
                }
            }
            Spacer()
            trailing
        }
        .padding(.horizontal, 16).padding(.top, 8).padding(.bottom, 10)
    }
}

/// 覆盖页(二级页)头:返回 + 标题
struct SubpageHeader: View {
    @EnvironmentObject var theme: Theme
    let title: String
    var subtitle: String = ""
    let onClose: () -> Void
    var t: Tokens { theme.t }

    var body: some View {
        HStack(spacing: 10) {
            Button(action: onClose) {
                Image(systemName: "chevron.left").font(.system(size: 16, weight: .semibold))
                    .foregroundColor(t.color(t.fg))
                    .frame(width: 34, height: 34)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            VStack(alignment: .leading, spacing: 1) {
                Text(title).font(.system(size: 18, weight: .bold)).foregroundColor(t.color(t.fg))
                if !subtitle.isEmpty {
                    Text(subtitle).font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                }
            }
            Spacer()
        }
        .padding(.horizontal, 10).padding(.vertical, 8)
    }
}

/// 卡片(默认表面色 + 细边;可传色调描边)
struct Card<Content: View>: View {
    @EnvironmentObject var theme: Theme
    var border: String? = nil
    var padded: Bool = true
    @ViewBuilder var content: () -> Content
    var t: Tokens { theme.t }

    var body: some View {
        content()
            .padding(padded ? 14 : 0)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)))
            .overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(border ?? t.border), lineWidth: border != nil ? 1.5 : 1))
    }
}

/// 小徽章 chip
struct Chip: View {
    @EnvironmentObject var theme: Theme
    let text: String
    var color: String? = nil
    var mono: Bool = false
    var t: Tokens { theme.t }

    var body: some View {
        let c = color ?? t.muted
        Text(text)
            .font(.system(size: 10, weight: .semibold, design: mono ? .monospaced : .default))
            .foregroundColor(t.color(c))
            .padding(.horizontal, 7).padding(.vertical, 3)
            .background(RoundedRectangle(cornerRadius: 7).fill(t.color(c).opacity(0.12)))
    }
}

/// 状态点(红/绿/黄)
struct Dot: View {
    @EnvironmentObject var theme: Theme
    let color: String
    var t: Tokens { theme.t }
    var body: some View {
        Circle().fill(t.color(color)).frame(width: 7, height: 7)
    }
}

/// KPI 数字块
struct StatTile: View {
    @EnvironmentObject var theme: Theme
    let icon: String
    let label: String
    let value: String
    var tint: String? = nil
    var t: Tokens { theme.t }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 5) {
                Image(systemName: icon).font(.system(size: 11)).foregroundColor(t.color(tint ?? t.muted))
                Text(label).font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
            }
            Text(value).font(.system(size: 21, weight: .bold, design: .rounded))
                .foregroundColor(t.color(t.fg)).lineLimit(1).minimumScaleFactor(0.5)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(13)
        .background(RoundedRectangle(cornerRadius: 15).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 15).stroke(t.color(t.border), lineWidth: 1)))
    }
}

/// 分节标题
struct SectionTitle: View {
    @EnvironmentObject var theme: Theme
    let text: String
    var t: Tokens { theme.t }
    var body: some View {
        Text(text.uppercased())
            .font(.system(size: 10.5, weight: .bold))
            .foregroundColor(t.color(t.faint))
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// 读取失败(带重试)
struct LoadFailed: View {
    @EnvironmentObject var theme: Theme
    let message: String
    var retry: (() -> Void)? = nil
    var t: Tokens { theme.t }
    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: "wifi.exclamationmark").font(.system(size: 22)).foregroundColor(t.color(t.faint))
            Text(message).font(.system(size: 12)).foregroundColor(t.color(t.muted)).multilineTextAlignment(.center)
            if let r = retry {
                Button { r() } label: {
                    Text("重试").font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.accent))
                        .padding(.horizontal, 18).padding(.vertical, 7)
                        .background(Capsule().stroke(t.color(t.accent), lineWidth: 1))
                }.buttonStyle(.plain)
            }
        }
        .frame(maxWidth: .infinity).padding(.vertical, 28)
    }
}

/// 空态
struct EmptyHint: View {
    @EnvironmentObject var theme: Theme
    let icon: String
    let text: String
    var t: Tokens { theme.t }
    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: icon).font(.system(size: 24)).foregroundColor(t.color(t.faint))
            Text(text).font(.system(size: 12)).foregroundColor(t.color(t.muted)).multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity).padding(.vertical, 30)
    }
}

/// 键值行(详情信息用)
struct KV: View {
    @EnvironmentObject var theme: Theme
    let k: String
    let v: String
    var mono: Bool = false
    var t: Tokens { theme.t }
    var body: some View {
        HStack(alignment: .top) {
            Text(k).font(.system(size: 12)).foregroundColor(t.color(t.muted))
            Spacer(minLength: 12)
            Text(v).font(.system(size: 12, weight: .medium, design: mono ? .monospaced : .default))
                .foregroundColor(t.color(t.fg)).multilineTextAlignment(.trailing)
        }
    }
}

/// 操作按钮:primary(绿底)/ ghost(描边)/ danger(红描边);action 允许 async
struct ActBtn: View {
    @EnvironmentObject var theme: Theme
    enum Kind { case primary, ghost, danger }
    let kind: Kind
    let icon: String?
    let label: String
    var busy: Bool = false
    let action: () async -> Void
    var t: Tokens { theme.t }

    var body: some View {
        Button { if !busy { Task { await action() } } } label: {
            HStack(spacing: 6) {
                if busy {
                    ProgressView().scaleEffect(0.7).tint(btnFg)
                } else if let i = icon {
                    Image(systemName: i).font(.system(size: 12, weight: .semibold))
                }
                Text(label).font(.system(size: 12.5, weight: .semibold))
            }
            .foregroundColor(btnFg)
            .frame(maxWidth: .infinity, minHeight: 42)
            .background(bg)
        }
        .buttonStyle(.plain)
    }

    private var btnFg: Color {
        switch kind {
        case .primary: return theme.t.dark ? Color.white : t.color(t.accent)
        case .ghost: return t.color(t.fg)
        case .danger: return t.color(t.danger)
        }
    }
    @ViewBuilder private var bg: some View {
        switch kind {
        case .primary: RoundedRectangle(cornerRadius: 12).fill(t.color(t.accent))
        case .ghost: RoundedRectangle(cornerRadius: 12).stroke(t.color(t.border), lineWidth: 1)
        case .danger: RoundedRectangle(cornerRadius: 12).stroke(t.color(t.danger), lineWidth: 1)
        }
    }
}

/// 列表行按钮(设置/入口用)
struct NavRow: View {
    @EnvironmentObject var theme: Theme
    let icon: String
    let title: String
    var desc: String = ""
    var tint: String? = nil
    let action: () -> Void
    var t: Tokens { theme.t }

    var body: some View {
        Button(action: action) {
            HStack(spacing: 11) {
                Image(systemName: icon)
                    .font(.system(size: 15))
                    .foregroundColor(t.color(tint ?? t.fg))
                    .frame(width: 30, height: 30)
                    .background(RoundedRectangle(cornerRadius: 8).fill(t.color(t.surfaceMuted)))
                VStack(alignment: .leading, spacing: 1.5) {
                    Text(title).font(.system(size: 13.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                    if !desc.isEmpty {
                        Text(desc).font(.system(size: 10.5)).foregroundColor(t.color(t.muted)).lineLimit(2)
                    }
                }
                Spacer()
                Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.faint))
            }
            .padding(11)
            .background(RoundedRectangle(cornerRadius: 14).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 14).stroke(t.color(t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }
}

/// 二次确认 sheet(危险操作共用)
struct ConfirmSheet: View {
    @EnvironmentObject var theme: Theme
    @Environment(\.dismiss) private var dismiss
    let title: String
    let message: String
    var confirmText: String = "确认执行"
    var danger: Bool = true
    let onConfirm: () async -> Void
    @State private var busy = false
    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 16) {
            Capsule().fill(t.color(t.border)).frame(width: 36, height: 4).padding(.top, 10)
            VStack(spacing: 8) {
                Image(systemName: danger ? "exclamationmark.triangle.fill" : "checkmark.shield.fill")
                    .font(.system(size: 30)).foregroundColor(t.color(danger ? t.warning : t.accent))
                Text(title).font(.system(size: 16, weight: .bold)).foregroundColor(t.color(t.fg))
                Text(message).font(.system(size: 12)).foregroundColor(t.color(t.muted))
                    .multilineTextAlignment(.center).fixedSize(horizontal: false, vertical: true)
            }.padding(.horizontal, 20)
            HStack(spacing: 10) {
                ActBtn(kind: .ghost, icon: nil, label: "取消") { dismiss() }
                ActBtn(kind: danger ? .danger : .primary, icon: nil, label: busy ? "执行中…" : confirmText, busy: busy) {
                    busy = true
                    await onConfirm()
                    busy = false
                    dismiss()
                }
            }.padding(.horizontal, 16)
            Spacer(minLength: 12)
        }
        .background(t.color(t.bg))
    }
}

/// 通用结果提示(toast)
@MainActor
final class Toast: ObservableObject {
    @Published var msg: String?
    @Published var isError: Bool = false
    private var generation = 0

    func show(_ m: String, error: Bool = false) {
        generation += 1
        let gen = generation
        withAnimation { msg = m; isError = error }
        Task {
            try? await Task.sleep(nanoseconds: 2_800_000_000)
            // 连续 toast:只有最新一条的隐藏任务生效
            guard gen == generation else { return }
            withAnimation { msg = nil }
        }
    }
}

/// 进度圆环(系统监控)
struct Ring: View {
    @EnvironmentObject var theme: Theme
    let label: String
    let sub: String
    var pct: Double
    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 7) {
            ZStack {
                Circle().stroke(t.color(t.border), lineWidth: 6)
                Circle().trim(from: 0, to: max(0.01, min(1, pct / 100)))
                    .stroke(ringColor, style: StrokeStyle(lineWidth: 6, lineCap: .round))
                    .rotationEffect(.degrees(-90))
                Text("\(Int(pct))%").font(.system(size: 15, weight: .bold, design: .rounded)).foregroundColor(t.color(t.fg))
            }
            .frame(width: 74, height: 74)
            Text(label).font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.fg))
            Text(sub).font(.system(size: 9.5)).foregroundColor(t.color(t.muted))
        }
        .frame(maxWidth: .infinity)
    }

    private var ringColor: Color {
        pct >= 90 ? t.color(t.danger) : pct >= 75 ? t.color(t.warning) : t.color(t.accent)
    }
}

/// 简易流式布局(机房 chip 群)
struct FlowLayout: Layout {
    var spacing: CGFloat = 8
    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? 320
        var x: CGFloat = 0, y: CGFloat = 0, rowH: CGFloat = 0
        for sub in subviews {
            let size = sub.sizeThatFits(.unspecified)
            if x + size.width > width, x > 0 { x = 0; y += rowH + spacing; rowH = 0 }
            x += size.width + spacing
            rowH = max(rowH, size.height)
        }
        return CGSize(width: width, height: y + rowH)
    }
    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, rowH: CGFloat = 0
        for sub in subviews {
            let size = sub.sizeThatFits(.unspecified)
            if x + size.width > bounds.maxX, x > bounds.minX {
                x = bounds.minX; y += rowH + spacing; rowH = 0
            }
            sub.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
            x += size.width + spacing
            rowH = max(rowH, size.height)
        }
    }
}

// MARK: - 通用工具

/// 隐私打码:IPv4 后两段掩掉;域名留前两段
func maskIP(_ s: String) -> String {
    guard !s.isEmpty else { return s }
    if let r = s.range(of: #"\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}"#, options: .regularExpression) {
        let parts = String(s[r]).components(separatedBy: ".")
        if parts.count == 4 {
            return s.replacingCharacters(in: r, with: parts[0] + "." + parts[1] + ".***.***")
        }
    }
    // IPv6:冒号地址保留首组,其余打码
    if s.contains(":") && !s.contains(".") {
        let groups = s.components(separatedBy: ":")
        if groups.count >= 3 { return groups.prefix(2).joined(separator: ":") + ":…:" + groups.suffix(1).joined() }
        return s
    }
    let parts = s.components(separatedBy: ".")
    if parts.count >= 3 { return parts.prefix(2).joined(separator: ".") + ".***" }
    return s
}

func fmtBytes(_ v: Double) -> String {
    let units = ["B", "KB", "MB", "GB", "TB", "PB"]
    var val = max(0, v), i = 0
    while val >= 1024, i < units.count - 1 { val /= 1024; i += 1 }
    return i == 0 ? "\(Int(val)) \(units[i])" : String(format: "%.1f %@", val, units[i])
}

/// OVH ISO 时间 → "yyyy-MM-dd HH:mm"(失败原样返回)
func fmtDate(_ raw: String?) -> String {
    guard let s = raw, !s.isEmpty else { return "—" }
    let iso = s.hasSuffix("Z") ? String(s.dropLast()) + "+0000" : s
    let f = DateFormatter()
    for fmt in ["yyyy-MM-dd'T'HH:mm:ssZZZZZ", "yyyy-MM-dd'T'HH:mm:ss.SSSZZZZZ", "yyyy-MM-dd HH:mm:ss", "yyyy-MM-dd"] {
        f.dateFormat = fmt
        if let d = f.date(from: iso) {
            let out = DateFormatter()
            out.dateFormat = fmt == "yyyy-MM-dd" ? "yyyy-MM-dd" : "yyyy-MM-dd HH:mm"
            return out.string(from: d)
        }
    }
    return s
}

/// 可用性白名单(对齐后端 IsAvailableForOrder):仅 \d+H(-high|-low) 可下单
func isOrderable(_ s: String) -> Bool {
    s.range(of: #"^\d+H(-high|-low)?$"#, options: .regularExpression) != nil
}

/// bps → Mbps/Gbps 文本
func fmtMbps(_ bps: Double) -> String {
    bps >= 1_000_000 ? String(format: "%.1f Gbps", bps / 1_000_000) : String(format: "%.0f Mbps", bps / 1_000)
}
