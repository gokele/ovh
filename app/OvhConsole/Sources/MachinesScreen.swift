import SwiftUI

/**
 * 机器列表(App 主体):独服 + VPS 合并卡片流。
 * 打开即此页 —— 服务器控制是 App 的核心,抢购等入口在菜单里。
 */
struct MachinesScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @State private var servers: [[String: Any]] = []
    @State private var vpsList: [[String: Any]] = []
    @State private var error: String?
    @State private var loading = true
    @State private var openServer: [String: Any]?
    @State private var openVps: [String: Any]?

    var t: Tokens { theme.t }

    var body: some View {
        Group {
            if let srv = openServer {
                ServerDetailView(server: srv, onBack: { openServer = nil; Task { await load() } })
            } else if let v = openVps {
                VpsDetailView(vps: v, onBack: { openVps = nil; Task { await load() } })
            } else {
                list
            }
        }
        .task { await load() }
        .refreshable { await load() }
    }

    private var list: some View {
        ScrollView {
            LazyVStack(spacing: 12) {
                // 统计行
                Text("\(servers.count + vpsList.count) 台(独服 \(servers.count) · VPS \(vpsList.count))\(!loading ? " · 上次同步 5 秒内" : " · 同步中…")")
                    .font(.caption)
                    .foregroundColor(t.color(t.muted))
                    .frame(maxWidth: .infinity, alignment: .leading)

                if let error {
                    ErrorCard(message: error, t: t)
                } else if loading && servers.isEmpty && vpsList.isEmpty {
                    ProgressView().padding(.top, 40)
                } else if servers.isEmpty && vpsList.isEmpty {
                    EmptyCard(t: t, text: "没有已购服务器\n在设置与账户页确认账户,或去网页端查看")
                } else {
                    ForEach(servers.indices, id: \.self) { i in
                        ServerCard(item: servers[i], t: t) { openServer = servers[i] }
                    }
                    ForEach(vpsList.indices, id: \.self) { i in
                        VpsCard(item: vpsList[i], t: t) { openVps = vpsList[i] }
                    }
                }
            }
            .padding(.horizontal, 16)
            .padding(.bottom, 24)
        }
        .background(t.color(t.bg))
    }

    private func load() async {
        error = nil
        do {
            let c = conn.client
            // 双列表并发拉
            async let s = c.getDict("/server-control/list")
            async let v = c.getDict("/vps-control/list")
            let (sr, vr) = try await (s, v)
            servers = (sr["servers"] as? [[String: Any]]) ?? []
            vpsList = (vr["vps"] as? [[String: Any]]) ?? []
        } catch {
            self.error = error.localizedDescription
        }
        loading = false
    }
}

// MARK: - 独服卡

struct ServerCard: View {
    let item: [String: Any]
    let t: Tokens
    let onTap: () -> Void

    var body: some View {
        let state = item["state"] as? String ?? ""
        let rescue = (item["netbootMode"] as? String) == "rescue"
        let ok = ["ok", "active"].contains(state.lowercased())
        let dot = rescue ? t.warning : (ok ? t.success : t.danger)

        Button(action: onTap) {
            VStack(spacing: 10) {
                HStack(spacing: 9) {
                    // 状态色图标块
                    RoundedRectangle(cornerRadius: 9)
                        .fill(t.color(rescue ? t.warning : ok ? t.success : t.danger))
                        .frame(width: 28, height: 28)
                        .overlay(Image(systemName: "servericon").font(.system(size: 12, weight: .bold)).foregroundColor(.white))
                    VStack(alignment: .leading, spacing: 2) {
                        Text(displayName).font(.system(size: 15, weight: .bold)).foregroundColor(t.color(t.fg)).lineLimit(1)
                        Text(item["serviceName"] as? String ?? "").font(.system(size: 10, design: .monospaced)).foregroundColor(t.color(t.faint)).lineLimit(1)
                    }
                    Spacer()
                    Pill(text: rescue ? "救援模式" : state.uppercased(), color: rescue ? t.warning : (ok ? t.success : t.danger), t: t)
                    Image(systemName: "chevron.right").font(.system(size: 12)).foregroundColor(t.color(t.faint))
                }
                HStack {
                    HStack(spacing: 6) {
                        Circle().fill(t.color(dot)).frame(width: 6, height: 6)
                        Text(item["ip"] as? String ?? "").font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                    }
                    Spacer()
                    Text("\((item["datacenter"] as? String ?? "—").uppercased()) · \(renewalText)")
                        .font(.system(size: 11)).foregroundColor(t.color(t.muted))
                }
                if rescue {
                    HStack(spacing: 7) {
                        Image(systemName: "exclamationmark.triangle.fill").font(.system(size: 12)).foregroundColor(t.color(t.warning))
                        Text("下次重启进入救援镜像 · 原系统数据未动 · 修完点「退出救援」")
                            .font(.system(size: 10.5)).foregroundColor(t.color(t.fg))
                    }
                    .padding(8)
                    .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.warning).opacity(0.08)))
                }
            }
            .padding(14)
            .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(rescue ? t.warning : t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }

    private var displayName: String {
        let n = item["name"] as? String ?? ""
        let sn = item["serviceName"] as? String ?? ""
        if n != sn, !n.isEmpty { return n.components(separatedBy: " | ").first ?? n }
        return sn
    }
    private var renewalText: String {
        if let rt = item["renewalType"] as? Bool { return rt ? "自动续费" : "手动续费" }
        return "续费未知"
    }
}

// MARK: - VPS 卡

struct VpsCard: View {
    let item: [String: Any]
    let t: Tokens
    let onTap: () -> Void

    var body: some View {
        let state = item["state"] as? String ?? ""
        let running = ["running", "active"].contains(state.lowercased())
        let name = item["name"] as? String ?? ""
        let ips = item["ips"] as? [String] ?? []

        Button(action: onTap) {
            VStack(spacing: 10) {
                HStack(spacing: 9) {
                    RoundedRectangle(cornerRadius: 9)
                        .fill(t.color(running ? t.success : t.danger))
                        .frame(width: 28, height: 28)
                        .overlay(Image(systemName: "cube.fill").font(.system(size: 12, weight: .bold)).foregroundColor(.white))
                    VStack(alignment: .leading, spacing: 2) {
                        Text((item["displayName"] as? String) ?? name.components(separatedBy: ".").first!)
                            .font(.system(size: 15, weight: .bold)).foregroundColor(t.color(t.fg)).lineLimit(1)
                        Text(name).font(.system(size: 10, design: .monospaced)).foregroundColor(t.color(t.faint)).lineLimit(1)
                    }
                    Spacer()
                    Pill(text: state.uppercased(), color: running ? t.success : t.danger, t: t)
                    Image(systemName: "chevron.right").font(.system(size: 12)).foregroundColor(t.color(t.faint))
                }
                HStack {
                    Text(ips.first ?? "—").font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                    Spacer()
                    Text([(item["model"] as? String ?? "VPS"), ((item["zone"] as? String ?? "").uppercased())].filter { !$0.isEmpty }.joined(separator: " · "))
                        .font(.system(size: 11)).foregroundColor(t.color(t.muted))
                }
            }
            .padding(14)
            .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }
}

// MARK: - 小组件

struct Pill: View {
    let text: String
    let color: String
    let t: Tokens
    var body: some View {
        Text(text)
            .font(.system(size: 9.5, weight: .bold))
            .foregroundColor(t.color(color))
            .padding(.horizontal, 8).padding(.vertical, 3)
            .background(Capsule().stroke(t.color(color), lineWidth: 1))
            .background(Capsule().fill(t.color(color).opacity(0.08)))
    }
}

struct EmptyCard: View {
    let t: Tokens
    let text: String
    var body: some View {
        VStack(spacing: 8) {
            Image(systemName: "server.rack").font(.system(size: 18)).foregroundColor(t.color(t.faint))
            Text(text).font(.system(size: 12)).foregroundColor(t.color(t.muted)).multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .padding(20)
        .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surfaceMuted)))
    }
}

struct ErrorCard: View {
    let message: String
    let t: Tokens
    var body: some View {
        VStack(spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill").font(.system(size: 18)).foregroundColor(t.color(t.danger))
            Text(message).font(.system(size: 12)).foregroundColor(t.color(t.danger)).multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .padding(20)
        .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surfaceMuted)))
    }
}
