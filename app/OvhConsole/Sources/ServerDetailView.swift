import SwiftUI

/**
 * 单机控制台(SwiftUI 版):状态头 + 四段 Tab(概览/电源/维护/高级)。
 * 电源动作带确认;危险动作走 Face ID(LocalAuthentication)。
 */
struct ServerDetailView: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let server: [String: Any]
    let onBack: () -> Void

    @State private var section = "overview"
    @State private var serviceInfo: [String: Any]?
    @State private var showReinstall = false
    @State private var showBootMode = false
    @State private var busy = false

    var t: Tokens { theme.t }
    var serviceName: String { server["serviceName"] as? String ?? "" }
    var rescue: Bool { (server["netbootMode"] as? String) == "rescue" }

    var body: some View {
        VStack(spacing: 0) {
            header
            segTabs
            ScrollView {
                VStack(spacing: 10) {
                    switch section {
                    case "power": powerTab
                    case "maintenance": MaintenancePlaceholder(t: t)
                    case "advanced": AdvancedPlaceholder(t: t)
                    default: overviewTab
                    }
                }
                .padding(16)
                .padding(.bottom, 20)
            }
        }
        .background(t.color(t.bg))
        .task { await loadInfo() }
        .sheet(isPresented: $showBootMode) { BootModeSheet(serviceName: serviceName) }
        .sheet(isPresented: $showReinstall) { ReinstallSheet(serviceName: serviceName, serverName: serviceName) }
    }

    // MARK: 状态头(别名大字 + serviceName 小字 + IP + 胶囊条)

    private var header: some View {
        VStack(alignment: .leading, spacing: 5) {
            Button(action: onBack) {
                HStack(spacing: 5) {
                    Image(systemName: "chevron.left").font(.system(size: 14))
                    Text("机器").font(.system(size: 12))
                }.foregroundColor(t.color(t.muted))
            }
            HStack(spacing: 9) {
                Circle().fill(t.color(rescue ? t.warning : t.success)).frame(width: 9, height: 9)
                Text(displayName).font(.system(size: 21, weight: .bold)).foregroundColor(t.color(t.fg)).lineLimit(1)
            }
            Text(serviceName).font(.system(size: 10.5, design: .monospaced)).foregroundColor(t.color(t.faint))
            HStack(spacing: 7) {
                Text(server["ip"] as? String ?? "").font(.system(size: 12, design: .monospaced)).foregroundColor(t.color(t.muted))
                Button {
                    UIPasteboard.general.string = server["ip"] as? String ?? ""
                } label: {
                    HStack(spacing: 3) {
                        Image(systemName: "doc.on.doc").font(.system(size: 10))
                        Text("复制").font(.system(size: 10))
                    }.foregroundColor(t.color(t.faint))
                }
            }
            HStack(spacing: 6) {
                if rescue { Pill(text: "救援模式", color: t.warning, t: t) }
                Pill(text: (server["state"] as? String ?? "—").uppercased(), color: rescue ? t.warning : t.success, t: t)
                Pill(text: "\((server["datacenter"] as? String ?? "—").uppercased()) 机房", color: t.muted, t: t)
                Pill(text: renewalPill, color: t.muted, t: t)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 16)
        .padding(.top, 6)
    }

    private var segTabs: some View {
        HStack(spacing: 2) {
            forTap("overview", "概览"); forTap("power", "电源"); forTap("maintenance", "维护"); forTap("advanced", "高级")
        }
        .padding(3)
        .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.border), lineWidth: 1)))
        .padding(.horizontal, 16)
        .padding(.top, 10)
    }

    private func forTap(_ id: String, _ label: String) -> some View {
        Button { section = id } label: {
            Text(label)
                .font(.system(size: 11, weight: section == id ? .semibold : .regular))
                .foregroundColor(t.color(section == id ? t.fg : t.muted))
                .frame(maxWidth: .infinity)
                .padding(.vertical, 5)
                .background(
                    RoundedRectangle(cornerRadius: 8)
                        .fill(section == id ? t.color(t.surface) : Color.clear)
                        .overlay(RoundedRectangle(cornerRadius: 8).stroke(section == id ? t.color(t.border) : Color.clear, lineWidth: 1))
                )
        }
        .buttonStyle(.plain)
    }

    // MARK: 概览

    private var overviewTab: some View {
        VStack(spacing: 10) {
            VStack(spacing: 4) {
                kv("处理器", processor)
                kv("机房", (server["datacenter"] as? String ?? "—").uppercased())
                kv("IP", server["ip"] as? String ?? "—")
                kv("OS", server["os"] as? String ?? "—")
                kv("到期", expiration)
                kv("续费", renewalText)
            }
            .padding(13)
            .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(t.border), lineWidth: 1)))
        }
    }

    // MARK: 电源

    private var powerTab: some View {
        VStack(spacing: 10) {
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible()), GridItem(.flexible())], spacing: 8) {
                ActTile(icon: "bolt.fill", label: "硬重启", t: t) {
                    confirm("硬重启?", "相当于按电源键强制重启,未落盘的数据会丢失;硬盘数据不受影响。", path: "/server-control/\(serviceName)/reboot")
                }
                ActTile(icon: "power", label: "启动模式", t: t) { showBootMode = true }
                ActTile(icon: "lifepreserver", label: rescue ? "退出救援" : "一键救援", t: t) {
                    if rescue {
                        confirm("退出救援模式?", "改回硬盘启动并重启,回到正常系统。", path: "/server-control/\(serviceName)/rescue/exit", body: ["confirm": true])
                    } else {
                        confirm("进入救援模式?", "确认后立刻重启进入救援镜像;原系统数据不动,root 密码发到救援邮箱。", path: "/server-control/\(serviceName)/rescue", body: ["confirm": true])
                    }
                }
                ActTile(icon: "display", label: "KVM 屏幕", t: t) { Task { await openKvm() } }
                ActTile(icon: "eye", label: "监控探测", t: t) {
                    Task { await put("/server-control/\(serviceName)/monitoring", ["enabled": !(server["monitoring"] as? Bool ?? false), "monitoring": !(server["monitoring"] as? Bool ?? false)]) }
                }
            }
            // 重装红区
            Button { showReinstall = true } label: {
                HStack(spacing: 10) {
                    Image(systemName: "opticaldiscdrive.fill").font(.system(size: 17)).foregroundColor(t.color(t.danger))
                    VStack(alignment: .leading, spacing: 1) {
                        Text("重装系统").font(.system(size: 12.5, weight: .bold)).foregroundColor(t.color(t.danger))
                        Text("智能分区方案 · Face ID + 输入机器名确认").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                    }
                    Spacer()
                    Image(systemName: "chevron.right").font(.system(size: 12)).foregroundColor(t.color(t.danger))
                }
                .padding(12)
                .background(RoundedRectangle(cornerRadius: 13).fill(t.color(t.danger).opacity(0.05)).overlay(RoundedRectangle(cornerRadius: 13).stroke(Color(red: 0.9, green: 0.65, blue: 0.65), style: StrokeStyle(lineWidth: 1, dash: [4, 3]))))
            }
            .buttonStyle(.plain)
        }
    }

    // MARK: 动作

    private func confirm(_ title: String, _ msg: String, path: String, body: [String: Any]? = nil) {
        let alert = UIAlertController(title: title, message: msg, preferredStyle: .alert)
        alert.addAction(UIAlertAction(title: "取消", style: .cancel))
        alert.addAction(UIAlertAction(title: "确认", style: .destructive) { _ in
            Task {
                guard await Biometric.require(title) else { return }
                await post(path, body)
            }
        })
        UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.keyWindow?.rootViewController }.first?.present(alert, animated: true)
    }

    private func post(_ path: String, _ body: [String: Any]? = nil) async {
        // Swift 6:[String:Any] 非 Sendable,先序列化成 Data 再跨并发域
        let data = body.flatMap { try? JSONSerialization.data(withJSONObject: $0) }
        let (ok, msg) = await conn.client.actionPostData(path, bodyData: data)
        if !ok { showError(msg) }
    }

    private func put(_ path: String, _ body: [String: Any]? = nil) async {
        let data = body.flatMap { try? JSONSerialization.data(withJSONObject: $0) }
        let (ok, msg) = await conn.client.actionPutData(path, bodyData: data)
        if !ok { showError(msg) }
    }

    private func openKvm() async {
        do {
            let r = try await conn.client.post("/server-control/\(serviceName)/console")
            if let url = r["url"] as? String, let u = URL(string: url) {
                _ = await UIApplication.shared.open(u)
            }
        } catch { showError(error.localizedDescription) }
    }

    private func loadInfo() async {
        if let r = try? await conn.client.getDict("/server-control/\(serviceName)/serviceinfo") {
            serviceInfo = r["serviceInfo"] as? [String: Any]
        }
    }

    private func showError(_ msg: String) {
        let alert = UIAlertController(title: "失败", message: msg, preferredStyle: .alert)
        alert.addAction(UIAlertAction(title: "好", style: .default))
        UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.keyWindow?.rootViewController }.first?.present(alert, animated: true)
    }

    // MARK: 取值 helpers

    private var displayName: String {
        let n = server["name"] as? String ?? ""
        let sn = server["serviceName"] as? String ?? ""
        if n != sn, !n.isEmpty { return n.components(separatedBy: " | ").first ?? n }
        return sn
    }
    private var processor: String {
        let n = server["name"] as? String ?? ""
        if n.contains(" | ") { return String(n.split(separator: " | ").dropFirst().joined(separator: " | ")) }
        return "—"
    }
    private var expiration: String {
        if let e = serviceInfo?["expiration"] as? String { return String(e.prefix(10)) }
        return "—"
    }
    private var renewalText: String {
        if let rt = server["renewalType"] as? Bool { return rt ? "自动续费" : "手动续费" }
        return "未知"
    }
    private var renewalPill: String { "续费 \(server["renewalType"] as? Bool == true ? "自动" : "手动")" }
    private func kv(_ k: String, _ v: String) -> some View {
        HStack(alignment: .top) {
            Text(k).font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
            Spacer()
            Text(v).font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg)).multilineTextAlignment(.trailing)
        }
    }
}

// MARK: - 复用件

struct ActTile: View {
    let icon: String
    let label: String
    let t: Tokens
    let onTap: () -> Void
    var body: some View {
        Button(action: onTap) {
            VStack(spacing: 6) {
                Image(systemName: icon).font(.system(size: 19)).foregroundColor(t.color(t.fg))
                Text(label).font(.system(size: 10.5)).foregroundColor(t.color(t.fg))
            }
            .frame(maxWidth: .infinity, minHeight: 86)
            .background(RoundedRectangle(cornerRadius: 13).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 13).stroke(t.color(t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }
}

struct MaintenancePlaceholder: View {
    let t: Tokens
    var body: some View {
        PlaceholderCard(t: t, text: "维护:硬件规格 / 撤单 / 联系人 —— 下一批接入")
    }
}
struct AdvancedPlaceholder: View {
    let t: Tokens
    var body: some View {
        PlaceholderCard(t: t, text: "高级:Backup FTP / 缓解 / vRack —— 下一批接入")
    }
}
struct PlaceholderCard: View {
    let t: Tokens
    let text: String
    var body: some View {
        Text(text).font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(13)
            .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surfaceMuted)))
    }
}
