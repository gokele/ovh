import SwiftUI

/**
 * 独服维护 Tab(真数据):硬件规格 / 撤单资格(可撤时剩余天数+截止) /
 * 变更联系人待确认数。工单制操作(硬件更换提交等)引导网页端。
 */
struct MaintenanceTab: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let serviceName: String

    @State private var hw: [String: Any]?
    @State private var hwErr: String?
    @State private var retr: [String: Any]?
    @State private var contactCount: Int?
    @State private var showRetraction = false
    @State private var showHardware = false

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 10) {
            hwGroup
            retractionGroup
            contactGroup
            // 写操作入口:硬件更换工单(撤单入口在 retractionGroup,可撤时才显示)
            Button { showHardware = true } label: {
                HStack(spacing: 10) {
                    Image(systemName: "wrench.and.screwdriver").font(.system(size: 16)).foregroundColor(t.color(t.fg))
                    VStack(alignment: .leading, spacing: 1) {
                        Text("硬件更换(工单)").font(.system(size: 12.5, weight: .bold)).foregroundColor(t.color(t.fg))
                        Text("硬盘 / 内存 / 散热 · Face ID 确认 · 提交 OVH 工单").font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
                    }
                    Spacer()
                    Image(systemName: "chevron.right").font(.system(size: 12)).foregroundColor(t.color(t.faint))
                }
                .padding(12)
                .background(RoundedRectangle(cornerRadius: 13).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 13).stroke(t.color(t.border), lineWidth: 1)))
            }
            .buttonStyle(.plain)
        }
        .task { await load() }
        .sheet(isPresented: $showHardware) { HardwareReplaceSheet(serviceName: serviceName) }
        .sheet(isPresented: $showRetraction) { RetractionSheet(serviceName: serviceName) }
    }

    // MARK: 硬件规格

    private var hwGroup: some View {
        GroupCard(t: t, icon: "cpu", title: "硬件规格") {
            if let e = hwErr {
                Text(e).font(.system(size: 11.5)).foregroundColor(t.color(t.danger))
            } else if let h = hw {
                kv("处理器", h["processorName"] as? String ?? "—")
                kv("核心/线程", "\(h["coresPerProcessor"] ?? "?")C / \(h["threadsPerProcessor"] ?? "?")T")
                if let mem = h["memorySize"] as? [String: Any] {
                    kv("内存", "\(mem["value"] ?? "?") \(mem["unit"] ?? "")")
                }
                if let groups = h["diskGroups"] as? [[String: Any]], !groups.isEmpty {
                    // 磁盘组:每组一行(2×480GB SSD 这种)
                    let text = groups.map { g -> String in
                        let disks = g["disks"] as? [[String: Any]] ?? []
                        let first = disks.first ?? [:]
                        return "\(disks.count)×\(first["size"] ?? "?")\(first["unit"] ?? "") \(first["type"] ?? "")"
                    }.joined(separator: " + ")
                    kv("磁盘", text)
                }
            } else {
                Text("读取中…").font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
            }
        }
    }

    // MARK: 撤单(可撤时是最要紧的信息)

    private var retractionGroup: some View {
        GroupCard(t: t, icon: "shield.slash", title: "撤回订单(14 天内)") {
            if let r = retr {
                if r["eligible"] as? Bool == true {
                    // 剩余天数 + 截止时间;口径写在界面上(从下单日起算,与开通日不同)
                    let hours = r["hoursLeft"] as? Double ?? 0
                    let days = Int(ceil(hours / 24))
                    Text("可撤单 · 剩 \(days) 天(从下单日起算,不是开通日)")
                        .font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.warning))
                    if let rd = r["retractionDate"] as? String {
                        kv("截止", String(rd.prefix(16)).replacingOccurrences(of: "T", with: " "))
                    }
                    Text("撤单 = 退款 + 服务器注销,不可逆。")
                        .font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
                    Button { showRetraction = true } label: {
                        Text("提交撤单申请")
                            .font(.system(size: 11.5, weight: .semibold)).foregroundColor(t.color(t.danger))
                            .frame(maxWidth: .infinity, minHeight: 40)
                            .background(RoundedRectangle(cornerRadius: 11).stroke(t.color(t.danger), lineWidth: 1))
                    }
                    .buttonStyle(.plain)
                } else {
                    Text(r["message"] as? String ?? "不在撤回期内")
                        .font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
                }
            } else {
                Text("读取中…").font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
            }
        }
    }

    // MARK: 变更联系人

    private var contactGroup: some View {
        GroupCard(t: t, icon: "person.2", title: "变更联系人") {
            if let n = contactCount {
                Text(n > 0 ? "\(n) 个待确认(网页端处理)" : "无待确认请求")
                    .font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
            } else {
                Text("读取中…").font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
            }
        }
    }

    // MARK: 工单制指路

    private var guideGroup: some View {
        GroupCard(t: t, icon: "wrench.and.screwdriver", title: "硬件更换 / 任务改期 / 合同期") {
            Text("工单制操作,请到网页端控制台完成(数据与确认流程更完整)")
                .font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
        }
    }

    private func load() async {
        let c = conn.client
        // 三个只读接口并发
        async let h = c.getDict("/server-control/\(serviceName)/hardware")
        async let r = c.getDict("/server-control/\(serviceName)/retraction")
        async let cc = c.getDict("/ovh/contact-change-requests")
        // hardware 可能失败(账户区域),单独记错误不连坐其它组
        do { hw = try await h; hwErr = nil } catch { hwErr = error.localizedDescription }
        retr = try? await r
        if let cr = try? await cc {
            contactCount = (cr["requests"] as? [[String: Any]])?.count ?? 0
        }
    }

    private func kv(_ k: String, _ v: String) -> some View {
        HStack(alignment: .top) {
            Text(k).font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
            Spacer()
            Text(v).font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg)).multilineTextAlignment(.trailing)
        }
    }
}

// MARK: - 高级 Tab

/**
 * 独服高级 Tab(真数据):Backup FTP / DDoS 永久缓解(可开关)/ 虚拟 MAC / vRack。
 */
struct AdvancedTab: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let serviceName: String

    @State private var ftp: [String: Any]?
    @State private var ftpErr: String?
    @State private var mitBlocks: [[String: Any]] = []
    @State private var mitErr: String?
    @State private var vmacCount: Int?
    @State private var vrackCount: Int?

    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 10) {
            ftpGroup
            mitigationGroup
            countsGroup
        }
        .task { await load() }
    }

    // MARK: Backup FTP(美区等不可用时如实显示原因)

    private var ftpGroup: some View {
        GroupCard(t: t, icon: "externaldrive", title: "Backup FTP") {
            if let e = ftpErr {
                Text(e).font(.system(size: 11.5)).foregroundColor(t.color(t.danger))
            } else if let f = ftp {
                if f["success"] as? Bool == false {
                    // 后端用 200+success:false 表达「区域不可用」等 —— 显示原因,不做假激活按钮
                    Text(f["error"] as? String ?? "此区域不可用")
                        .font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
                } else if let quota = f["quota"] as? [String: Any] {
                    kv("配额", "\(quota["value"] ?? "?") \(quota["unit"] ?? "")")
                    if let usage = f["usage"] as? [String: Any] {
                        kv("已用", "\(usage["value"] ?? "?") \(usage["unit"] ?? "")")
                    }
                    if let url = f["ftpUrl"] as? String {
                        kv("地址", String(url.dropFirst("ftp://".count)))
                    }
                } else {
                    Text(ftp?.isEmpty == false ? "未激活(网页端可激活)" : "读取中…")
                        .font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
                }
            } else {
                Text("读取中…").font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
            }
        }
    }

    // MARK: DDoS 永久缓解(按 IP 开关)

    private var mitigationGroup: some View {
        GroupCard(t: t, icon: "shield.lefthalf.filled", title: "DDoS 永久缓解") {
            if let e = mitErr {
                Text(e).font(.system(size: 11.5)).foregroundColor(t.color(t.danger))
            } else if mitBlocks.isEmpty {
                Text("无 IP 信息").font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
            } else {
                // 每个 IP 一行,点击开关(POST 启用 / DELETE 关闭,必带 ?block=)
                ForEach(Array(ipRows.enumerated()), id: \.offset) { _, row in
                    mitigationRow(row)
                }
            }
        }
    }

    private struct MitRow {
        let ip: String
        let block: String
        let permanent: Bool
    }

    private var ipRows: [MitRow] {
        mitBlocks.flatMap { b -> [MitRow] in
            let block = b["ipBlock"] as? String ?? ""
            let ms = b["mitigations"] as? [[String: Any]] ?? []
            return ms.map { MitRow(ip: $0["ipOnMitigation"] as? String ?? "", block: block, permanent: $0["permanent"] as? Bool ?? false) }
        }
    }

    private func mitigationRow(_ row: MitRow) -> some View {
        Button { toggleMitigation(row) } label: {
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

    /// 开关缓解:契约 POST(启用)/ DELETE(关闭)+ 必带 ?block=<所属网段>(与 RN 版核对同款)
    private func toggleMitigation(_ row: MitRow) {
        let alert = UIAlertController(
            title: row.permanent ? "关闭永久缓解?" : "开启永久缓解?",
            message: row.permanent
                ? "关闭后该 IP 不再常驻 DDoS 缓解(OVH 自动缓解仍在)。"
                : "开启后该 IP 常驻 DDoS 缓解,攻击流量在 OVH 边缘清洗。",
            preferredStyle: .alert)
        alert.addAction(UIAlertAction(title: "取消", style: .cancel))
        alert.addAction(UIAlertAction(title: "确认", style: .destructive) { _ in
            Task {
                let base = "/server-control/\(serviceName)/mitigation/\(row.ip)"
                let q = "?block=\(row.block.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? row.block)"
                if row.permanent {
                    _ = await conn.client.actionDelete(base + q)
                } else {
                    _ = await conn.client.actionPostData(base + q, bodyData: Data("{}".utf8))
                }
                await load()
            }
        })
        AlertHost.present(alert)
    }

    // MARK: 计数(虚拟 MAC / vRack)

    private var countsGroup: some View {
        GroupCard(t: t, icon: "network", title: "虚拟 MAC / vRack") {
            HStack {
                kv("虚拟 MAC", vmacCount.map { "\($0) 个" } ?? "…")
            }
            HStack {
                kv("vRack 成员", vrackCount.map { $0 > 0 ? "\($0) 个" : "未加入" } ?? "…")
            }
            Text("管理操作在网页端").font(.system(size: 10.5)).foregroundColor(t.color(t.faint))
        }
    }

    private func load() async {
        let c = conn.client
        do {
            let f = try await c.getDict("/server-control/\(serviceName)/backup-ftp")
            ftp = f; ftpErr = nil
        } catch { ftpErr = error.localizedDescription }
        do {
            let m = try await c.getDict("/server-control/\(serviceName)/mitigation")
            mitBlocks = (m["ips"] as? [[String: Any]]) ?? []
            mitErr = nil
        } catch { mitErr = error.localizedDescription }
        if let v = try? await c.getDict("/server-control/\(serviceName)/virtual-mac") {
            vmacCount = (v["virtualMacs"] as? [[String: Any]])?.count ?? 0
        }
        if let vr = try? await c.getDict("/server-control/\(serviceName)/vrack") {
            vrackCount = (vr["vracks"] as? [[String: Any]])?.count ?? 0
        }
    }

    private func kv(_ k: String, _ v: String) -> some View {
        HStack {
            Text(k).font(.system(size: 11.5)).foregroundColor(t.color(t.muted))
            Spacer()
            Text(v).font(.system(size: 11.5, design: .monospaced)).foregroundColor(t.color(t.fg))
        }
    }
}

// MARK: - 共享分组卡

struct GroupCard<Content: View>: View {
    let t: Tokens
    let icon: String
    let title: String
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                Image(systemName: icon).font(.system(size: 14)).foregroundColor(t.color(t.muted))
                Text(title).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
            }
            content
        }
        .padding(13)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 16).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 16).stroke(t.color(t.border), lineWidth: 1)))
    }
}
