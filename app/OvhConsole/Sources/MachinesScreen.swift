import SwiftUI

/**
 * 机器(App 主体):独服 / VPS 两段列表 → 详情全功能控制。
 * 右上角"眼睛"开关:IP 打码(与 web 隐私模式同语义,全局持久化)。
 */
struct MachinesScreen: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @AppStorage("ovh_mask_ip") private var mask = false
    var t: Tokens { theme.t }

    @State private var servers: [[String: Any]] = []
    @State private var vpsList: [[String: Any]] = []
    @State private var err: String?
    @State private var loading = true
    @State private var seg = 0
    @State private var path: [MachineRef] = []
    @State private var showAccountPicker = false

    var body: some View {
        NavigationStack(path: $path) {
            ScrollView {
                VStack(spacing: 12) {
                    Picker("", selection: $seg) {
                        Text("独立服务器 · \(servers.count)").tag(0)
                        Text("VPS · \(vpsList.count)").tag(1)
                    }
                    .pickerStyle(.segmented)

                    if let e = err {
                        Card { LoadFailed(message: e) { Task { await load() } } }
                    } else if loading && servers.isEmpty && vpsList.isEmpty {
                        ProgressView().padding(.top, 60)
                    } else {
                        // 只在"真的拿不到机器 + 账户全失效"时才警告;
                        // 列表有数据时 valid 标志可能是旧状态,再喊失效会和真实数据打架
                        if servers.isEmpty && vpsList.isEmpty && accountsInvalid {
                            Card(border: t.warning) {
                                HStack(spacing: 9) {
                                    Image(systemName: "exclamationmark.triangle.fill").font(.system(size: 13)).foregroundColor(t.color(t.warning))
                                    Text("OVH 账户凭据已失效,列表可能为空 —— 去网页端「设置 → OVH 账户 → 重新验证凭据」")
                                        .font(.system(size: 11)).foregroundColor(t.color(t.muted))
                                }
                            }
                        }
                        if seg == 0 {
                            if servers.isEmpty {
                                Card { EmptyHint(icon: "server.rack", text: "账户下没有独立服务器") }
                            } else {
                                ForEach(servers.indices, id: \.self) { i in
                                    ServerCard(item: servers[i], mask: mask) { push(servers[i]) }
                                }
                            }
                        } else {
                            if vpsList.isEmpty {
                                Card { EmptyHint(icon: "cube.box", text: "账户下没有 VPS") }
                            } else {
                                ForEach(vpsList.indices, id: \.self) { i in
                                    VpsCard(item: vpsList[i], mask: mask) { push(vpsList[i]) }
                                }
                            }
                        }
                    }
                }
                .padding(16)
            }
            .background(t.color(t.bg))
            .navigationTitle("机器")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarColorScheme(theme.dark ? .dark : .light, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    AccountButton { showAccountPicker = true }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        mask.toggle()
                    } label: {
                        Image(systemName: mask ? "eye.slash.fill" : "eye")
                            .font(.system(size: 14, weight: .semibold))
                            .foregroundColor(t.color(mask ? t.accent : t.muted))
                    }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        Task { await load() }
                    } label: {
                        Image(systemName: "arrow.clockwise").font(.system(size: 14, weight: .semibold)).foregroundColor(t.color(t.muted))
                    }
                }
            }
            .navigationDestination(for: MachineRef.self) { ref in
                if ref.isVps, let item = vpsList.first(where: { ($0["name"] as? String) == ref.id }) {
                    VpsDetailView(item: item)
                } else if let item = servers.first(where: { ($0["serviceName"] as? String) == ref.id }) {
                    ServerDetailView(item: item)
                }
            }
            .refreshable { await load() }
            .onChange(of: conn.accountId) { _ in
                loading = true
                Task { await load() }
            }
        }
        .sheet(isPresented: $showAccountPicker) {
            AccountPickerSheet()
                .environmentObject(theme).environmentObject(conn).environmentObject(toast)
        }
        .task {
            await conn.loadAccounts()
            await load()
        }
    }

    private func push(_ item: [String: Any]) {
        if seg == 1, let name = item["name"] as? String {
            path.append(MachineRef(id: name, isVps: true))
        } else if let sn = item["serviceName"] as? String {
            path.append(MachineRef(id: sn, isVps: false))
        }
    }

    /// 账户凭据是否全部失效(此时 OVH 列表为空不是"没机器")
    private var accountsInvalid: Bool {
        !conn.accounts.isEmpty && conn.accounts.allSatisfy { ($0["valid"] as? Bool ?? false) == false }
    }

    private func load() async {
        err = nil
        do {
            let c = conn.client
            async let s = c.getDict("/server-control/list")
            async let v = c.getDict("/vps-control/list")
            let (sr, vr) = try await (s, v)
            servers = (sr["servers"] as? [[String: Any]]) ?? []
            vpsList = (vr["vps"] as? [[String: Any]]) ?? []
        } catch {
            err = error.localizedDescription
        }
        loading = false
    }
}

/// 详情推入引用(NavigationStack 值类型)
struct MachineRef: Hashable {
    let id: String
    let isVps: Bool
}

// MARK: - 独服卡

struct ServerCard: View {
    @EnvironmentObject var theme: Theme
    let item: [String: Any]
    let mask: Bool
    let onTap: () -> Void
    var t: Tokens { theme.t }

    var body: some View {
        let state = item["state"] as? String ?? ""
        let rescue = (item["netbootMode"] as? String) == "rescue"
        let ok = ["ok", "active"].contains(state.lowercased())
        let tint = rescue ? t.warning : (ok ? t.success : t.danger)

        Button(action: onTap) {
            VStack(spacing: 10) {
                HStack(spacing: 10) {
                    RoundedRectangle(cornerRadius: 9)
                        .fill(t.color(tint).opacity(0.18))
                        .frame(width: 34, height: 34)
                        .overlay(Image(systemName: "server.rack").font(.system(size: 14, weight: .semibold)).foregroundColor(t.color(tint)))
                    VStack(alignment: .leading, spacing: 2) {
                        Text(displayName).font(.system(size: 15, weight: .bold)).foregroundColor(t.color(t.fg)).lineLimit(1)
                        // 副标题只在有别名时显示机器名;没别名时两行会重复
                        if hasAlias {
                            Text(item["serviceName"] as? String ?? "")
                                .font(.system(size: 10, design: .monospaced)).foregroundColor(t.color(t.faint)).lineLimit(1)
                        }
                    }
                    Spacer()
                    Chip(text: rescue ? "救援模式" : state.uppercased(), color: tint)
                    Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.faint))
                }
                HStack {
                    HStack(spacing: 6) {
                        Dot(color: tint)
                        Text(mask ? maskIP(item["ip"] as? String ?? "") : (item["ip"] as? String ?? ""))
                            .font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                    }
                    Spacer()
                    Text("\((item["datacenter"] as? String ?? "—").uppercased()) · \(renewalText)")
                        .font(.system(size: 11)).foregroundColor(t.color(t.muted))
                }
                .padding(.top, 10)
                .overlay(Rectangle().frame(height: 0.5).foregroundColor(t.color(t.border)).opacity(0.6), alignment: .top)
                if rescue {
                    HStack(spacing: 7) {
                        Image(systemName: "exclamationmark.triangle.fill").font(.system(size: 11)).foregroundColor(t.color(t.warning))
                        Text("下次重启进入救援镜像 · 修完在「电源」里退出救援")
                            .font(.system(size: 10.5)).foregroundColor(t.color(t.fg))
                    }
                    .padding(9)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(RoundedRectangle(cornerRadius: 10).fill(t.color(t.warning).opacity(0.08)))
                }
            }
            .padding(14)
            .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(tint), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }

    /// 有别名时 name(含 "别名 | 机器名" 结构)≠ serviceName
    private var hasAlias: Bool {
        let n = item["name"] as? String ?? ""
        return n != (item["serviceName"] as? String ?? "") && !n.isEmpty
    }
    private var displayName: String {
        let n = item["name"] as? String ?? ""
        let sn = item["serviceName"] as? String ?? ""
        if hasAlias { return n.components(separatedBy: " | ").first ?? n }
        return sn
    }
    private var renewalText: String {
        if let rt = item["renewalType"] as? Bool { return rt ? "自动续费" : "手动续费" }
        return "续费未知"
    }
}

// MARK: - VPS 卡

let VPS_STATE_CN: [String: String] = [
    "running": "运行中", "active": "运行中", "stopped": "已关机", "suspended": "已暂停",
    "error": "错误", "reinstalling": "重装中", "installing": "安装中",
    "deleted": "已删除", "to_delete": "待删除", "todelete": "待删除", "unknown": "未知",
]

struct VpsCard: View {
    @EnvironmentObject var theme: Theme
    let item: [String: Any]
    let mask: Bool
    let onTap: () -> Void
    var t: Tokens { theme.t }

    var body: some View {
        let state = item["state"] as? String ?? ""
        let running = ["running", "active"].contains(state.lowercased())
        let name = item["name"] as? String ?? ""
        let ips = item["ips"] as? [String] ?? []

        Button(action: onTap) {
            VStack(spacing: 10) {
                HStack(spacing: 10) {
                    RoundedRectangle(cornerRadius: 9)
                        .fill(t.color(running ? t.success : t.danger).opacity(0.18))
                        .frame(width: 34, height: 34)
                        .overlay(Image(systemName: "cube.fill").font(.system(size: 14, weight: .semibold)).foregroundColor(t.color(running ? t.success : t.danger)))
                    VStack(alignment: .leading, spacing: 2) {
                        Text((item["displayName"] as? String) ?? name.components(separatedBy: ".").first ?? name)
                            .font(.system(size: 15, weight: .bold)).foregroundColor(t.color(t.fg)).lineLimit(1)
                        Text(name).font(.system(size: 10, design: .monospaced)).foregroundColor(t.color(t.faint)).lineLimit(1)
                    }
                    Spacer()
                    Chip(text: VPS_STATE_CN[state.lowercased()] ?? state.uppercased(), color: running ? t.success : t.danger)
                    Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.faint))
                }
                HStack {
                    Text(mask ? maskIP(ips.first ?? "—") : (ips.first ?? "—"))
                        .font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
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
