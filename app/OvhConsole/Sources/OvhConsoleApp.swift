import SwiftUI

/**
 * App 入口:机器控制台为主体,配对闸在前。
 * 底部菜单栏四 Tab:机器 / 雷达 / 队列 / 更多 —— 机器仍是首 Tab(核心),
 * 其余入口不再藏在右上角弹出菜单里。
 */
@main
struct OvhConsoleApp: App {
    @StateObject private var conn = Connection()
    @StateObject private var theme = Theme()
    @StateObject private var nav = AppNav()

    var body: some Scene {
        WindowGroup {
            Group {
                if !conn.isPaired {
                    PairingScreen()
                } else {
                    MainScreen()
                }
            }
            .environmentObject(conn)
            .environmentObject(theme)
            .environmentObject(nav)
            .preferredColorScheme(theme.dark ? .dark : .light)
            // 跟随系统:theme.dark 由这里初始化
            .onAppear {
                if !theme.dark {
                    theme.dark = UITraitCollection.current.userInterfaceStyle == .dark
                }
            }
        }
    }
}

/// 页面导航:底部四 Tab + 「更多」内的二级入口(覆盖页)
@MainActor
final class AppNav: ObservableObject {
    /// 底部 Tab: machines / radar / queue / more
    @Published var tab: String = "machines"
    /// 二级覆盖页(more 里的项): monitor / history / logs / profile
    @Published var overlay: String? = nil
}

struct MainScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var nav: AppNav
    @State private var accounts: [[String: Any]] = []

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 0) {
            topBar

            // Tab 内容
            switch nav.tab {
            case "radar":
                RadarOverlay(onClose: { nav.tab = "machines" })
            case "queue":
                QueueOverlay(onClose: { nav.tab = "machines" })
            case "more":
                MorePage()
            default:
                MachinesScreen()
            }

            tabBar
        }
        .overlay(
            // 二级覆盖页(更多里的项):「完成」回到更多
            Group {
                switch nav.overlay {
                case "monitor":
                    MonitorOverlay(onClose: { nav.overlay = nil }).transition(.move(edge: .trailing))
                case "history":
                    HistoryOverlay(onClose: { nav.overlay = nil }).transition(.move(edge: .trailing))
                case "logs":
                    LogsOverlay(onClose: { nav.overlay = nil }).transition(.move(edge: .trailing))
                case "profile":
                    ProfileOverlay(onClose: { nav.overlay = nil }, onDisconnected: { nav.overlay = nil })
                        .transition(.move(edge: .trailing))
                default:
                    EmptyView()
                }
            }
        )
    }

    // MARK: 顶栏(账户徽章;菜单按钮移除 —— 导航在底部)

    private var topBar: some View {
        HStack(spacing: 8) {
            if let acc = activeAccount {
                let zone = acc["zone"] as? String ?? ""
                Text(zone)
                    .font(.system(size: 9.5, weight: .bold))
                    .foregroundColor(t.color(t.fg))
                    .padding(.horizontal, 6).padding(.vertical, 2)
                    .background(RoundedRectangle(cornerRadius: 6).fill(t.color(t.surfaceMuted)))
                Text(acc["name"] as? String ?? "").font(.system(size: 13, weight: .semibold)).foregroundColor(t.color(t.fg)).lineLimit(1)
            } else {
                Text("服务器控制台").font(.system(size: 16, weight: .bold)).foregroundColor(t.color(t.fg))
            }
            Spacer()
        }
        .padding(.horizontal, 16).padding(.vertical, 10)
        .overlay(Rectangle().frame(height: 0.5).foregroundColor(t.color(t.border)), alignment: .bottom)
        .task { await loadAccounts() }
    }

    private var activeAccount: [String: Any]? {
        let id = conn.accountId
        return accounts.first { ($0["id"] as? String) == id } ?? accounts.first { ($0["isDefault"] as? Bool) == true } ?? accounts.first
    }

    private func loadAccounts() async {
        if let r = try? await conn.client.getDict("/accounts") {
            accounts = (r["accounts"] as? [[String: Any]]) ?? []
        }
    }

    // MARK: 底部菜单栏

    private var tabBar: some View {
        HStack(spacing: 0) {
            tabItem("machines", icon: "server.rack", label: "机器")
            tabItem("radar", icon: "dot.radiowaves.left.and.right", label: "雷达")
            tabItem("queue", icon: "list.bullet.rectangle", label: "队列")
            tabItem("more", icon: "ellipsis.circle", label: "更多")
        }
        .padding(.top, 8)
        .padding(.bottom, 24)   // iPhone home 指示条安全区
        .background(t.color(t.surface).overlay(Rectangle().frame(height: 0.5).foregroundColor(t.color(t.border)), alignment: .top))
    }

    private func tabItem(_ id: String, icon: String, label: String) -> some View {
        let on = nav.tab == id
        return Button {
            nav.tab = id
            nav.overlay = nil   // 切 Tab 时关掉二级页
        } label: {
            VStack(spacing: 3) {
                Image(systemName: icon)
                    .font(.system(size: 21, weight: on ? .semibold : .regular))
                    .foregroundColor(t.color(on ? t.fg : t.faint))
                Text(label)
                    .font(.system(size: 10, weight: on ? .semibold : .regular))
                    .foregroundColor(t.color(on ? t.fg : t.faint))
            }
            .frame(maxWidth: .infinity)
            .contentShape(Rectangle())   // 整格可点,不只图标文字
        }
        .buttonStyle(.plain)
    }
}

/// 「更多」页:监控 / 历史 / 日志 / 设置(列表入口 → 二级覆盖页)
struct MorePage: View {
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var nav: AppNav
    @EnvironmentObject var conn: Connection
    var t: Tokens { theme.t }

    var body: some View {
        ScrollView {
            VStack(spacing: 10) {
                Text("更多")
                    .font(.system(size: 24, weight: .bold)).foregroundColor(t.color(t.fg))
                    .frame(maxWidth: .infinity, alignment: .leading)

                row("monitor", icon: "eye", title: "服务器监控", desc: "补货订阅与自动下单")
                row("history", icon: "clock.arrow.circlepath", title: "抢购历史", desc: "订单状态与付款倒计时")
                row("logs", icon: "doc.text.magnifyingglass", title: "运行日志", desc: "最近 200 条,自动刷新")
                row("profile", icon: "person.crop.circle", title: "设置与账户", desc: "配对 / 账户 / 外观")

                Text("后端:\(conn.serverUrl)")
                    .font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.top, 6)
            }
            .padding(16)
        }
        .background(t.color(t.bg))
    }

    private func row(_ id: String, icon: String, title: String, desc: String) -> some View {
        Button { nav.overlay = id } label: {
            HStack(spacing: 11) {
                Image(systemName: icon).font(.system(size: 17)).foregroundColor(t.color(t.fg))
                VStack(alignment: .leading, spacing: 1) {
                    Text(title).font(.system(size: 13.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                    Text(desc).font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                }
                Spacer()
                Image(systemName: "chevron.right").font(.system(size: 12)).foregroundColor(t.color(t.faint))
            }
            .padding(13)
            .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }
}
