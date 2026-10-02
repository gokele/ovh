import SwiftUI

/**
 * App 入口:机器控制台为主体,配对闸在前。
 * 菜单(雷达/队列/设置)收在右上角 —— 次要入口。
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

/// 页面导航(overlay 模型:主屏之上覆盖次要页)
@MainActor
final class AppNav: ObservableObject {
    @Published var overlay: String? = nil   // "radar" / "queue" / "profile"
    @Published var menuOpen = false
}

struct MainScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var nav: AppNav
    @State private var accounts: [[String: Any]] = []

    var t: Tokens { theme.t }

    var body: some View {
        ZStack {
            VStack(spacing: 0) {
                topBar
                MachinesScreen()
            }

            if nav.menuOpen { menu }

            // 覆盖层:菜单三页(雷达/队列/设置),「完成」返回
            switch nav.overlay {
            case "radar":
                RadarOverlay(onClose: { nav.overlay = nil })
                    .transition(.move(edge: .trailing))
            case "queue":
                QueueOverlay(onClose: { nav.overlay = nil })
                    .transition(.move(edge: .trailing))
            case "history":
                HistoryOverlay(onClose: { nav.overlay = nil })
                    .transition(.move(edge: .trailing))
            case "logs":
                LogsOverlay(onClose: { nav.overlay = nil })
                    .transition(.move(edge: .trailing))
            case "profile":
                ProfileOverlay(onClose: { nav.overlay = nil }, onDisconnected: { nav.overlay = nil })
                    .transition(.move(edge: .trailing))
            default:
                EmptyView()
            }
        }
    }

    /// 顶栏:当前账户徽章 + 菜单按钮
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
            Button { nav.menuOpen = true } label: {
                Image(systemName: "line.3.horizontal").font(.system(size: 20)).foregroundColor(t.color(t.fg))
            }
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

    /// 弹出菜单:三个次要入口
    private var menu: some View {
        ZStack(alignment: .topTrailing) {
            Color.black.opacity(0.35).ignoresSafeArea().onTapGesture { nav.menuOpen = false }
            VStack(alignment: .leading, spacing: 0) {
                HStack {
                    Text("更多").font(.system(size: 14, weight: .bold)).foregroundColor(t.color(t.fg))
                    Spacer()
                    Button { nav.menuOpen = false } label: { Image(systemName: "xmark").font(.system(size: 15)).foregroundColor(t.color(t.muted)) }
                }
                .padding(14)
                .overlay(Rectangle().frame(height: 0.5).foregroundColor(t.color(t.border)), alignment: .bottom)

                menuItem("radar", icon: "dot.radiowaves.left.and.right", title: "补货雷达", desc: "机型 × 机房可用性")
                menuItem("queue", icon: "list.bullet.rectangle", title: "抢购队列", desc: "任务状态与耗时")
                menuItem("history", icon: "clock.arrow.circlepath", title: "抢购历史", desc: "订单状态与付款倒计时")
                menuItem("logs", icon: "doc.text.magnifyingglass", title: "运行日志", desc: "最近 200 条,自动刷新")
                menuItem("profile", icon: "person.crop.circle", title: "设置与账户", desc: "配对 / 账户 / 外观")
            }
            .frame(width: 250)
            .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(t.border), lineWidth: 1)))
            .padding(.trailing, 14)
            .padding(.top, 56)
        }
    }

    private func menuItem(_ id: String, icon: String, title: String, desc: String) -> some View {
        Button {
            nav.menuOpen = false
            nav.overlay = id
        } label: {
            HStack(spacing: 11) {
                Image(systemName: icon).font(.system(size: 17)).foregroundColor(t.color(t.fg))
                VStack(alignment: .leading, spacing: 1) {
                    Text(title).font(.system(size: 13.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                    Text(desc).font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                }
                Spacer()
            }
            .padding(.horizontal, 14).padding(.vertical, 12)
        }
        .buttonStyle(.plain)
    }
}
