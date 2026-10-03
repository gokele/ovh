import SwiftUI

/**
 * App 入口:五 Tab 信息架构(总览 / 机器 / 抢购 / 雷达 / 设置),机器仍是主体。
 * 配对闸在前;配对页支持 ovhconsole://pair 深链。
 */
@main
struct OvhConsoleApp: App {
    @StateObject private var conn = Connection()
    @StateObject private var theme = Theme()
    @StateObject private var toast = Toast()
    @StateObject private var nav = AppNav()
    @State private var pendingPairURL: URL? = nil

    var body: some Scene {
        WindowGroup {
            ThemeSync {
                Group {
                    if !conn.isPaired {
                        PairingScreen(deepLink: pendingPairURL, onPaired: { pendingPairURL = nil })
                    } else {
                        MainScreen()
                    }
                }
                .environmentObject(conn)
                .environmentObject(theme)
                .environmentObject(toast)
                .environmentObject(nav)
            }
            .preferredColorScheme(theme.mode == .system ? nil : (theme.dark ? .dark : .light))
            .onOpenURL { url in
                // 深链:ovhconsole://pair?host=..&code=..&auto=1
                if url.scheme == "ovhconsole", url.host == "pair" {
                    pendingPairURL = url
                }
            }
        }
    }
}

/// 把 SwiftUI 环境里的系统深浅同步进 Theme。
/// UITraitCollection.current 在 body 计算的部分时机不可靠(设置页白条 bug 的根因)。
private struct ThemeSync<Content: View>: View {
    @Environment(\.colorScheme) private var scheme
    @EnvironmentObject var theme: Theme
    @ViewBuilder var content: () -> Content

    var body: some View {
        content()
            .onAppear { theme.systemDark = scheme == .dark }
            .onChange(of: scheme) { theme.systemDark = ($0 == .dark) }
    }
}

/// 页面导航状态(跨 Tab 联动:总览 KPI 点击跳对应 Tab 等)
@MainActor
final class AppNav: ObservableObject {
    @Published var tab: Tab = .machines
    /// 抢购页内的分段:catalog / queue / history
    @Published var snipeSegment = 0
    /// 雷达页内的分段:0 独服 / 1 VPS
    @Published var radarSegment = 0

    enum Tab: Hashable { case overview, machines, snipe, radar, settings }
}

struct MainScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var nav: AppNav
    @EnvironmentObject var toast: Toast
    var t: Tokens { theme.t }

    init() {
        // iOS 16 的 UITabBar 外观:透明底 + 细线
        let app = UITabBarAppearance()
        app.configureWithTransparentBackground()
        UITabBar.appearance().standardAppearance = app
        UITabBar.appearance().scrollEdgeAppearance = app
    }

    var body: some View {
        TabView(selection: $nav.tab) {
            DashboardScreen()
                .tabItem { Label("总览", systemImage: "gauge.with.dots.needle.50percent") }
                .tag(AppNav.Tab.overview)

            MachinesScreen()
                .tabItem { Label("机器", systemImage: "server.rack") }
                .tag(AppNav.Tab.machines)

            SnipeScreen()
                .tabItem { Label("抢购", systemImage: "bolt.fill") }
                .tag(AppNav.Tab.snipe)

            RadarScreen()
                .tabItem { Label("雷达", systemImage: "dot.radiowaves.left.and.right") }
                .tag(AppNav.Tab.radar)

            SettingsScreen()
                .tabItem { Label("设置", systemImage: "gearshape.fill") }
                .tag(AppNav.Tab.settings)
        }
        .tint(t.color(t.accent))
        .overlay(toast.view, alignment: .bottom)
        .task {
            await conn.loadAccounts()
        }
    }
}

extension Toast {
    var view: some View { toastBody }

    @ViewBuilder private var toastBody: some View {
        if let m = msg {
            VStack {
                Spacer()
                HStack(spacing: 8) {
                    Image(systemName: isError ? "xmark.octagon.fill" : "checkmark.circle.fill")
                        .foregroundColor(isError ? Color(hex: 0xF87171) : Color(hex: 0x22C55E))
                    Text(m).font(.system(size: 12.5, weight: .semibold)).foregroundColor(.white)
                        .lineLimit(3)
                }
                .padding(.horizontal, 16).padding(.vertical, 11)
                .background(Capsule().fill(Color(hex: 0x0F172A).opacity(0.96)))
                .padding(.bottom, 84)
            }
            .transition(.move(edge: .bottom).combined(with: .opacity))
            .allowsHitTesting(false)
        }
    }
}
