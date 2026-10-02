import SwiftUI

/**
 * VPS 控制台(SwiftUI 版):状态头 + 三段 Tab(概览/电源/快照)。
 */
struct VpsDetailView: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let vps: [String: Any]
    let onBack: () -> Void

    @State private var section = "overview"
    @State private var alias = ""
    @State private var info: [String: Any]?
    @State private var snapshot: [String: Any]?
    @State private var showReinstall = false

    var t: Tokens { theme.t }
    var name: String { vps["name"] as? String ?? "" }
    var state: String { (info?["state"] as? String) ?? (vps["state"] as? String ?? "—") }
    var running: Bool { ["running", "active"].contains(state.lowercased()) }

    var body: some View {
        VStack(spacing: 0) {
            header
            HStack(spacing: 2) {
                forTap("overview", "概览"); forTap("snapshot", "快照")
                forTap("ddos", "DDoS"); forTap("maintenance", "维护")
            }
            .padding(3)
            .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.border), lineWidth: 1)))
            .padding(.horizontal, 16)
            .padding(.top, 10)

            ScrollView {
                VStack(spacing: 10) {
                    switch section {
                    case "snapshot": SnapshotPane(vpsName: name)
                    case "ddos": VpsMitigationPane(vpsName: name)
                    case "maintenance": vpsMaintenanceTab
                    default: overviewTab
                    }
                }
                .padding(16)
            }
        }
        .background(t.color(t.bg))
        .task { await load() }
        .sheet(isPresented: $showReinstall) { VpsReinstallSheet(vpsName: name) }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 5) {
            Button(action: onBack) {
                HStack(spacing: 5) {
                    Image(systemName: "chevron.left").font(.system(size: 14))
                    Text("机器").font(.system(size: 12))
                }.foregroundColor(t.color(t.muted))
            }
            HStack(spacing: 9) {
                Circle().fill(t.color(running ? t.success : t.danger)).frame(width: 9, height: 9)
                Text((vps["displayName"] as? String) ?? name.components(separatedBy: ".").first!)
                    .font(.system(size: 21, weight: .bold)).foregroundColor(t.color(t.fg)).lineLimit(1)
            }
            Text(name).font(.system(size: 10.5, design: .monospaced)).foregroundColor(t.color(t.faint))
            if let ip = (vps["ips"] as? [String])?.first {
                Text(ip).font(.system(size: 12, design: .monospaced)).foregroundColor(t.color(t.muted))
            }
            HStack(spacing: 6) {
                Pill(text: state.uppercased(), color: running ? t.success : t.danger, t: t)
                Pill(text: vps["model"] as? String ?? "VPS", color: t.muted, t: t)
                if let z = vps["zone"] as? String, !z.isEmpty {
                    Pill(text: z.uppercased(), color: t.muted, t: t)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 16)
        .padding(.top, 6)
    }

    private func forTap(_ id: String, _ label: String) -> some View {
        Button { section = id } label: {
            Text(label)
                .font(.system(size: 11, weight: section == id ? .semibold : .regular))
                .foregroundColor(t.color(section == id ? t.fg : t.muted))
                .frame(maxWidth: .infinity).padding(.vertical, 5)
                .background(RoundedRectangle(cornerRadius: 8).fill(section == id ? t.color(t.surface) : Color.clear))
        }
        .buttonStyle(.plain)
    }

    private var overviewTab: some View {
        VStack(spacing: 10) {
            VStack(spacing: 4) {
                kv("型号", (info?["model"] as? [String: Any])?["name"] as? String ?? vps["model"] as? String ?? "—")
                kv("状态", state)
                kv("IP", ((vps["ips"] as? [String]) ?? []).joined(separator: ", "))
                kv("OS", (info?["os"] as? String) ?? vps["os"] as? String ?? "—")
            }
            .padding(13)
            .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(t.border), lineWidth: 1)))

            // 电源动作(原电源 Tab 并回概览,与 web 四 Tab 对齐)
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible()), GridItem(.flexible())], spacing: 8) {
                if !running {
                    ActTile(icon: "power", label: "启动", t: t) { Task { await act("start") } }
                }
                if running {
                    ActTile(icon: "power.dotted", label: "关机", t: t) { Task { await act("stop") } }
                }
                ActTile(icon: "arrow.clockwise", label: "重启", t: t) { Task { await act("reboot") } }
                ActTile(icon: "display", label: "控制台", t: t) { Task { await openConsole() } }
            }
        }
    }

    // MARK: VPS 维护 Tab(别名 / 终止)
    private var vpsMaintenanceTab: some View {
        VStack(spacing: 10) {
            // 别名(本地显示名,不下发 OVH)
            VStack(alignment: .leading, spacing: 8) {
                Text("服务器别名").font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                TextField(vps["displayName"] as? String ?? name, text: $alias)
                    .font(.system(size: 13))
                    .foregroundColor(t.color(t.fg))
                    .padding(.horizontal, 13)
                    .frame(height: 42)
                    .background(RoundedRectangle(cornerRadius: 11).fill(t.color(t.surfaceMuted)).overlay(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.border), lineWidth: 1)))
                Text("只在本控制台显示,不下发给 OVH").font(.system(size: 10)).foregroundColor(t.color(t.faint))
            }
            .padding(13)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(t.border), lineWidth: 1)))

            // 终止(红区:立即销毁,与 web 同警告)
            Button {
                confirmTerminate()
            } label: {
                HStack(spacing: 10) {
                    Image(systemName: "exclamationmark.octagon.fill").font(.system(size: 17)).foregroundColor(t.color(t.danger))
                    VStack(alignment: .leading, spacing: 1) {
                        Text("终止 VPS").font(.system(size: 12.5, weight: .bold)).foregroundColor(t.color(t.danger))
                        Text("确认后立即销毁,数据不可恢复 —— 建议用「到期终止」代替").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                    }
                    Spacer()
                }
                .padding(12)
                .background(RoundedRectangle(cornerRadius: 13).fill(t.color(t.danger).opacity(0.05)).overlay(RoundedRectangle(cornerRadius: 13).stroke(Color(red: 0.9, green: 0.65, blue: 0.65), style: StrokeStyle(lineWidth: 1, dash: [4, 3]))))
            }
            .buttonStyle(.plain)
        }
    }

    private func confirmTerminate() {
        let alert = UIAlertController(title: "终止 VPS?", message: "确认后立即销毁,数据不可恢复。这是不可逆操作。", preferredStyle: .alert)
        alert.addAction(UIAlertAction(title: "取消", style: .cancel))
        alert.addAction(UIAlertAction(title: "终止", style: .destructive) { _ in
            Task {
                guard await Biometric.require("终止 VPS") else { return }
                _ = await conn.client.actionPostData("/vps-control/\(name)/terminate", bodyData: Data("{}".utf8))
            }
        })
        UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.keyWindow?.rootViewController }.first?.present(alert, animated: true)
    }

    private var powerTab: some View {
        VStack(spacing: 10) {
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible()), GridItem(.flexible())], spacing: 8) {
                if !running {
                    ActTile(icon: "power", label: "启动", t: t) { Task { await act("start") } }
                }
                if running {
                    ActTile(icon: "power.dotted", label: "关机", t: t) { Task { await act("stop") } }
                }
                ActTile(icon: "arrow.clockwise", label: "重启", t: t) { Task { await act("reboot") } }
                ActTile(icon: "display", label: "控制台", t: t) { Task { await openConsole() } }
            }
            Button { showReinstall = true } label: {
                HStack(spacing: 10) {
                    Image(systemName: "opticaldiscdrive.fill").font(.system(size: 17)).foregroundColor(t.color(t.danger))
                    VStack(alignment: .leading, spacing: 1) {
                        Text("重装系统").font(.system(size: 12.5, weight: .bold)).foregroundColor(t.color(t.danger))
                        Text("Face ID + 输入名称确认 · 清空全部数据").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                    }
                    Spacer()
                }
                .padding(12)
                .background(RoundedRectangle(cornerRadius: 13).fill(t.color(t.danger).opacity(0.05)).overlay(RoundedRectangle(cornerRadius: 13).stroke(Color(red: 0.9, green: 0.65, blue: 0.65), style: StrokeStyle(lineWidth: 1, dash: [4, 3]))))
            }
            .buttonStyle(.plain)
        }
    }

    private func act(_ verb: String) async {
        guard await Biometric.require(verb) else { return }
        _ = try? await conn.client.post("/vps-control/\(name)/\(verb)", body: [:])
        await load()
    }

    private func openConsole() async {
        if let r = try? await conn.client.post("/vps-control/\(name)/console", body: [:]),
           let url = r["url"] as? String, let u = URL(string: url) {
            _ = await UIApplication.shared.open(u)
        }
    }

    private func load() async {
        info = try? await conn.client.getDict("/vps-control/\(name)/info")
    }

    private func kv(_ k: String, _ v: String) -> some View {
        HStack(alignment: .top) {
            Text(k).font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
            Spacer()
            Text(v).font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg)).multilineTextAlignment(.trailing)
        }
    }
}

// MARK: - 快照面板

struct SnapshotPane: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let vpsName: String
    @State private var snap: [String: Any]?
    @State private var err: String?

    var t: Tokens { theme.t }
    private var has: Bool { snap?["snapshot"] != nil }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("快照").font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
            if let e = err {
                Text(e).font(.system(size: 11.5)).foregroundColor(t.color(t.danger))
            } else if has {
                kv("创建于", ((snap?["snapshot"] as? [String: Any])?["creationDate"] as? String ?? "—").prefix(16).replacingOccurrences(of: "T", with: " "))
                kv("描述", (snap?["snapshot"] as? [String: Any])?["description"] as? String ?? "—")
                HStack(spacing: 8) {
                    SnapBtn(icon: "arrow.uturn.backward", label: "回滚", color: t.danger, t: t, action: { await revert() })
                    SnapBtn(icon: "trash", label: "删除", color: t.danger, t: t, action: { await remove() })
                }.padding(.top, 4)
            } else {
                Text("没有快照(每台 VPS 只能有一份)").font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
                SnapBtn(icon: "camera", label: "创建快照", color: t.muted, t: t, action: { await create() })
            }
        }
        .padding(13)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(t.border), lineWidth: 1)))
        .task { await load() }
    }

    private func load() async {
        do { snap = try await conn.client.getDict("/vps-control/\(vpsName)/snapshot") }
        catch { err = error.localizedDescription }
    }

    private func create() async {
        _ = try? await conn.client.post("/vps-control/\(vpsName)/snapshot", body: ["description": "App 创建"])
        await load()
    }
    private func revert() async {
        guard await Biometric.require("回滚快照") else { return }
        _ = try? await conn.client.post("/vps-control/\(vpsName)/snapshot/revert", body: [:])
        await load()
    }
    private func remove() async {
        _ = try? await conn.client.delete("/vps-control/\(vpsName)/snapshot")
        await load()
    }
    private func kv(_ k: String, _ v: String) -> some View {
        HStack {
            Text(k).font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
            Spacer()
            Text(v).font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
        }
    }
}

struct SnapBtn: View {
    let icon: String
    let label: String
    let color: String
    let t: Tokens
    let action: () async -> Void
    var body: some View {
        Button { Task { await action() } } label: {
            HStack(spacing: 5) {
                Image(systemName: icon).font(.system(size: 12))
                Text(label).font(.system(size: 11.5))
            }
            .foregroundColor(t.color(color))
            .padding(.horizontal, 12).padding(.vertical, 7)
            .background(Capsule().stroke(t.color(color), lineWidth: 1))
        }
        .buttonStyle(.plain)
    }
}

// MARK: - VPS DDoS 缓解面板(按 IP 开关,契约同独服)

struct VpsMitigationPane: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let vpsName: String

    @State private var blocks: [[String: Any]] = []
    @State private var err: String?

    var t: Tokens { theme.t }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("DDoS 永久缓解").font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
            if let e = err {
                Text(e).font(.system(size: 11.5)).foregroundColor(t.color(t.danger))
            } else if blocks.isEmpty {
                Text("无 IP 信息").font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
            } else {
                ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                    Button { toggle(row) } label: {
                        HStack {
                            Text(row.ip).font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
                            Spacer()
                            Text(row.permanent ? "开 · 点关" : "关 · 点开")
                                .font(.system(size: 11.5, weight: .semibold))
                                .foregroundColor(t.color(row.permanent ? t.success : t.faint))
                        }
                        .padding(.vertical, 4)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
        .padding(13)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(t.border), lineWidth: 1)))
        .task { await load() }
    }

    private struct Row { let ip: String; let block: String; let permanent: Bool }
    private var rows: [Row] {
        blocks.flatMap { b -> [Row] in
            let block = b["ipBlock"] as? String ?? ""
            return ((b["mitigations"] as? [[String: Any]]) ?? []).map {
                Row(ip: $0["ipOnMitigation"] as? String ?? "", block: block, permanent: $0["permanent"] as? Bool ?? false)
            }
        }
    }

    private func toggle(_ row: Row) {
        let a = UIAlertController(title: row.permanent ? "关闭永久缓解?" : "开启永久缓解?",
                                  message: row.permanent ? "关闭后不再常驻缓解(自动缓解仍在)。" : "开启后常驻 DDoS 缓解,攻击流量在 OVH 边缘清洗。",
                                  preferredStyle: .alert)
        a.addAction(UIAlertAction(title: "取消", style: .cancel))
        a.addAction(UIAlertAction(title: "确认", style: .destructive) { _ in
            Task {
                let base = "/vps-control/\(vpsName)/mitigation/\(row.ip)"
                let q = "?block=\(row.block.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? row.block)"
                if row.permanent { _ = await conn.client.actionDelete(base + q) }
                else { _ = await conn.client.actionPostData(base + q, bodyData: Data("{}".utf8)) }
                await load()
            }
        })
        UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.keyWindow?.rootViewController }.first?.present(a, animated: true)
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/vps-control/\(vpsName)/mitigation")
            blocks = (r["ips"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
    }
}
