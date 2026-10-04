import SwiftUI

/**
 * 独服详情(全功能):概览 / 电源 / 维护 / 高级 四段。
 * 顶部服务胶囊条(到期 · 续费策略),对齐 web 独服控制台。
 */
struct ServerDetailView: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    @EnvironmentObject var toast: Toast
    @AppStorage("ovh_mask_ip") private var mask = false
    let item: [String: Any]
    var t: Tokens { theme.t }

    var sn: String { item["serviceName"] as? String ?? "" }
    @State private var seg = 0
    @State private var serviceinfo: [String: Any]?
    @State private var sheet: ServerSheet?

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                headerCard
                ScrollView(.horizontal, showsIndicators: false) {
                    DetailTabs(tabs: ["概览", "电源", "维护", "高级"], selection: $seg)
                }

                switch seg {
                case 1: PowerSection(sn: sn, serviceinfo: serviceinfo, sheet: $sheet)
                case 2: MaintenanceSection(sn: sn, sheet: $sheet)
                case 3: AdvancedSection(sn: sn, sheet: $sheet)
                default: OverviewSection(sn: sn, item: item, mask: mask, serviceinfo: serviceinfo, sheet: $sheet)
                }
            }
            .padding(16)
        }
        .background(t.color(t.bg))
        .navigationTitle(displayName)
        .navigationBarTitleDisplayMode(.inline)
        .toolbarColorScheme(theme.dark ? .dark : .light, for: .navigationBar)
        .task { await load() }
        .refreshable { await load() }
        .sheet(item: $sheet) { s in serverSheet(s) }
    }

    private var displayName: String {
        let n = item["name"] as? String ?? ""
        if n != sn, !n.isEmpty { return n.components(separatedBy: " | ").first ?? n }
        return sn
    }

    // MARK: 顶部卡

    private var headerCard: some View {
        let state = item["state"] as? String ?? ""
        let ok = ["ok", "active"].contains(state.lowercased())
        let tint = ok ? t.success : t.danger
        let alias = displayName != sn ? displayName : nil
        return Card {
            VStack(spacing: 12) {
                // Hero 行:状态光环图标 + 名称区 + 状态 chip
                HStack(alignment: .center, spacing: 13) {
                    StatusBadgeIcon(systemImage: "server.rack", tint: tint)
                        .onLongPressGesture { sheet = .init(kind: .alias) }   // S-008:长按=设别名
                    VStack(alignment: .leading, spacing: 3) {
                        Text(alias ?? sn).font(.system(size: 17, weight: .bold)).foregroundColor(t.color(t.fg)).lineLimit(1)
                        Text(alias == nil ? "" : sn)
                            .font(.system(size: 10.5, design: .monospaced)).foregroundColor(t.color(t.faint)).lineLimit(1)
                        HStack(spacing: 8) {
                            Label("\((item["datacenter"] as? String ?? "—").uppercased())", systemImage: "mappin.and.ellipse")
                            Label(mask ? maskIP(item["ip"] as? String ?? "") : (item["ip"] as? String ?? "—"), systemImage: "network")
                        }
                        .font(.system(size: 10, design: .monospaced)).foregroundColor(t.color(t.muted))
                    }
                    Spacer()
                    VStack(spacing: 5) {
                        Chip(text: state.uppercased(), color: tint)
                        Text(renewalText.isEmpty ? "" : renewalText)
                            .font(.system(size: 9.5, weight: .medium)).foregroundColor(t.color(t.faint))
                    }
                }

                // 胶囊行:撤单倒计时 / 监控 / OS(功能锚点不变)
                if serviceinfo == nil {
                    HStack(spacing: 6) {
                        ForEach(0..<3, id: \.self) { _ in
                            RoundedRectangle(cornerRadius: 8).fill(t.color(t.surfaceMuted)).frame(width: 64, height: 22)
                        }
                        Spacer()
                    }
                }
                // 全部胶囊一排(FlowLayout:放得下单行,不够自动换行)
                FlowLayout(spacing: 6) {
                    if (retraction?["eligible"] as? Bool) == true {
                        Button { sheet = .init(kind: .retraction) } label: {
                            InfoPill(icon: "clock.badge.exclamationmark", text: "可撤单 · \(retractionLeftText)", tint: t.warning)
                        }.buttonStyle(.plain)
                    }
                    if let os = item["os"] as? String, !os.isEmpty {
                        Button { sheet = .init(kind: .reinstall) } label: {
                            InfoPill(icon: "terminal", text: os, tint: t.info)
                        }.buttonStyle(.plain)
                    }
                    Button { sheet = .init(kind: .renewal) } label: {
                        InfoPill(icon: "arrow.triangle.2.circlepath", text: renewalText.isEmpty ? "续费" : renewalText, tint: t.accent)
                    }.buttonStyle(.plain)
                    if let si = serviceinfo, let exp = si["expiration"] as? String, !exp.isEmpty {
                        InfoPill(icon: "calendar", text: "到期 \(fmtDate(exp))", tint: daysLeft(exp) < 7 ? t.danger : nil)
                    }
                    Button {
                        if monitoringOn == nil {
                            Task {
                                monitoringLoading = true
                                if let r = try? await conn.client.getDict("/server-control/\(sn)/monitoring") {
                                    monitoringOn = r["monitoring"] as? Bool
                                }
                                monitoringLoading = false
                            }
                        } else { sheet = .init(kind: .monitoring) }
                    } label: {
                        InfoPill(icon: "bell.badge", text: monitoringLabel, tint: monitoringColor)
                    }.buttonStyle(.plain)
                }
                if let si = serviceinfo {
                    let terminating = (si["terminationScheduled"] as? Bool ?? false) || (si["renewalDeleteAtExpiration"] as? Bool ?? false)
                    if terminating {
                        InfoPill(icon: "exclamationmark.triangle.fill", text: "到期将终止服务", tint: t.danger)
                    }
                }
            }
        }
    }

    private var monitoringLabel: String {
        if monitoringLoading { return "监控 读取中…" }
        if monitoringOn == nil { return "监控 状态未知 · 重试" }
        return monitoringOn! ? "监控 已开" : "监控 已关"
    }
    private var monitoringColor: String {
        if monitoringLoading { return t.muted }
        if monitoringOn == nil { return t.danger }
        return monitoringOn! ? t.success : t.faint
    }

    /// 撤单剩余窗口(retractionDate 与当前差)
    private var retractionLeftText: String {
        guard let deadline = retraction?["retractionDate"] as? String ?? retraction?["deadline"] as? String else {
            return "窗口内"
        }
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        for fmt in ["yyyy-MM-dd'T'HH:mm:ssZZZZZ", "yyyy-MM-dd"] {
            f.dateFormat = fmt
            if let d = f.date(from: deadline) {
                let hours = Int(d.timeIntervalSinceNow / 3600)
                return hours >= 24 ? "\(hours / 24) 天" : "\(max(0, hours)) 小时"
            }
        }
        return "窗口内"
    }

    private var renewalText: String {
        if let si = serviceinfo {
            // 终止态优先(S-015 五种细分)
            let term = terminationLabel(si)
            if !term.isEmpty { return term }
            if si["renewalForced"] as? Bool == true {
                let p = si["renewalPeriod"] as? Int ?? 1
                return "强制自动 · \(p)月"
            }
            if si["renewalType"] as? Bool == true {
                let p = si["renewalPeriod"] as? Int ?? 1
                return "自动 · \(p)月"
            }
            if si["renewalType"] == nil { return "续费未知" }   // 没读到 ≠ 手动
            return "手动"
        }
        return ""
    }

    private func terminationLabel(_ si: [String: Any]) -> String {
        // 后端实际字段:terminationScheduled/terminationAction(terminate|terminateAtExpirationDate|
        // terminateAtEngagementDate|deleteAtExpiration)/terminationStateUnknown
        if si["terminationStateUnknown"] as? Bool == true { return "终止状态未知" }
        let scheduled = si["terminationScheduled"] as? Bool ?? false
        let deleteAtExp = si["renewalDeleteAtExpiration"] as? Bool ?? false
        guard scheduled || deleteAtExp else { return "" }
        switch (si["terminationAction"] as? String ?? "").lowercased() {
        case "terminate": return "终止处理中(立即)"
        case "terminateatengagementdate": return "合同期结束终止"
        case "terminateatexpirationdate", "deleteatexpiration": return "到期终止"
        default: break
        }
        return deleteAtExp ? "到期终止" : "已安排终止"
    }

    private func daysLeft(_ iso: String) -> Int {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd'T'HH:mm:ssZZZZZ"
        var d = f.date(from: iso.hasSuffix("Z") ? String(iso.dropLast()) + "+0000" : iso)
        if d == nil {
            let f2 = DateFormatter()
            f2.dateFormat = "yyyy-MM-dd"   // 到期日常是纯日期
            d = f2.date(from: iso)
        }
        guard let date = d else { return 999 }
        return Calendar.current.dateComponents([.day], from: Date(), to: date).day ?? 999
    }

    @State private var retraction: [String: Any]? = nil
    @State private var monitoringOn: Bool? = nil
    @State private var monitoringLoading = true

    private func load() async {
        // 不缓存:refreshable / 保存续费后回来都要拿到最新状态
        async let si = conn.client.getDict("/server-control/\(sn)/serviceinfo")
        async let rt = conn.client.getDict("/server-control/\(sn)/retraction", timeoutSec: 20)
        async let mo = conn.client.getDict("/server-control/\(sn)/monitoring")
        if let r = try? await si {
            serviceinfo = (r["serviceInfo"] as? [String: Any]) ?? r
        }
        if let r = try? await rt { retraction = r }
        if let r = try? await mo { monitoringOn = r["monitoring"] as? Bool }
        monitoringLoading = false
    }

    // MARK: sheet 调度

    @ViewBuilder
    private func serverSheet(_ s: ServerSheet) -> some View {
        switch s.kind {
        case .mrtg: MrtgSheet(sn: sn)
        case .reboot: ConfirmSheet(title: "确认硬重启 \(sn)?", message: "这相当于按下电源键,不是操作系统里的正常重启:内存和磁盘缓存里还没落盘的数据会丢,正在跑的服务会被直接切断。确认前请先在系统里停好业务。", confirmText: "确认重启") {
            let (ok, msg) = await conn.client.actionPostData("/server-control/\(sn)/reboot", bodyData: nil)
            toast.show(ok ? "重启已发起" : (msg.isEmpty ? "重启失败" : msg), error: !ok)
        }
        case .rescue: RescueSheet(sn: sn)
        case .reinstall: ServerReinstallSheet(sn: sn)
        case .ipmi: IpmiSheet(sn: sn)
        case .bootMode: BootModeSheet(sn: sn)
        case .spla: SplaSheet(sn: sn)
        case .tasks: TasksSheet(sn: sn)
        case .bios: BiosSheet(sn: sn)
        case .installStatus: InstallStatusSheet(sn: sn)
        case .retraction: RetractionSheet(sn: sn)
        case .renewal: RenewalSheet(sn: sn, isVps: false, info: serviceinfo ?? [:])
        case .networkSpecs: NetworkSpecsSheet(sn: sn)
        case .engagement: EngagementSheet(sn: sn, isVps: false)
        case .hwReplace: HardwareReplaceSheet(sn: sn)
        case .changeContact: ChangeContactSheet(sn: sn, isVps: false)
        case .monitoring: MonitoringSheet(sn: sn)
        case .burst: ToggleSheet(title: "Burst 流量", icon: "bolt.horizontal", getPath: "/server-control/\(sn)/burst", putPath: "/server-control/\(sn)/burst")
        case .firewall: ToggleSheet(title: "网络防火墙", icon: "flame", getPath: "/server-control/\(sn)/firewall", putPath: "/server-control/\(sn)/firewall")
        case .backupFtp: BackupFtpSheet(sn: sn)
        case .secondaryDns: JsonSheet(title: "二级 DNS", icon: "globe", path: "/server-control/\(sn)/secondary-dns")
        case .virtualMac: JsonSheet(title: "虚拟 MAC", icon: "macpro.gen3", path: "/server-control/\(sn)/virtual-mac")
        case .vrack: JsonSheet(title: "vRack 成员", icon: "link", path: "/server-control/\(sn)/vrack")
        case .orderableBandwidth: JsonSheet(title: "可购带宽", icon: "speedometer", path: "/server-control/\(sn)/orderable/bandwidth")
        case .orderableTraffic: JsonSheet(title: "可购流量", icon: "arrow.up.arrow.down", path: "/server-control/\(sn)/orderable/traffic")
        case .orderableIp: JsonSheet(title: "可购 IP 块", icon: "number.square", path: "/server-control/\(sn)/orderable/ip")
        case .options: OptionsSheet(sn: sn)
        case .ipSpecs: JsonSheet(title: "IP 规格", icon: "number", path: "/server-control/\(sn)/ip-specs")
        case .mitigation: MitigationSheet(sn: sn, isVps: false)
        case .alias: ServerAliasSheet(sn: sn, current: item["name"] as? String ?? "")
        }
    }
}

/// 独服弹窗枚举
struct ServerSheet: Identifiable {
    enum Kind {
        case mrtg, reboot, rescue, reinstall, ipmi, bootMode, spla, tasks, bios, installStatus
        case retraction, renewal, networkSpecs, engagement, hwReplace, changeContact, monitoring
        case burst, firewall, backupFtp, secondaryDns, virtualMac, vrack
        case orderableBandwidth, orderableTraffic, orderableIp, options, ipSpecs, mitigation
        case alias
    }
    let kind: Kind
    var id: Kind { kind }
}

// MARK: - 概览段

struct OverviewSection: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let sn: String
    let item: [String: Any]
    let mask: Bool
    let serviceinfo: [String: Any]?
    @Binding var sheet: ServerSheet?
    var t: Tokens { theme.t }

    @State private var hardware: [String: Any]?
    @State private var ips: [[String: Any]] = []
    @State private var nics: [[String: Any]] = []
    @State private var err: String?

    var body: some View {
        VStack(spacing: 12) {
            if let e = err { Card { LoadFailed(message: e) { Task { await load() } } } }

            Card {
                VStack(spacing: 9) {
                    HStack {
                        SectionTitle(text: "硬件")
                        Spacer()
                        if let cr = item["commercialRange"] as? String, !cr.isEmpty {
                            Chip(text: cr.components(separatedBy: " | ").first ?? cr, color: t.info)
                        }
                    }
                    if let hw = hardware {
                        LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 8) {
                            SpecTile(icon: "cpu", label: "处理器", value: "\(hw["processorName"] ?? "—")")
                            SpecTile(icon: "cpu.fill", label: "核心", value: "\(hw["numberOfProcessors"] ?? 0) 颗 × \(hw["coresPerProcessor"] ?? 0) 核 / \(hw["threadsPerProcessor"] ?? 0) 线程")
                            SpecTile(icon: "memorychip", label: "内存", value: memText(hw["memorySize"]))
                            SpecTile(icon: "internaldrive", label: "磁盘", value: diskAllText(hw))
                        }
                        if let mb = hw["motherboard"] as? String, mb != "N/A", !mb.isEmpty {
                            KV(k: "主板", v: mb)
                        }
                    } else {
                        ProgressView().padding(10).frame(maxWidth: .infinity)
                    }
                }
            }
            Card {
                VStack(alignment: .leading, spacing: 10) {
                    HStack {
                        SectionTitle(text: "流量(网卡)")
                        Spacer()
                        Button { sheet = .init(kind: .mrtg) } label: {
                            Image(systemName: "arrow.up.left.and.arrow.down.right")
                                .font(.system(size: 12, weight: .semibold))
                                .foregroundColor(t.color(t.muted))
                        }.buttonStyle(.plain)
                    }
                    MrtgChartView(sn: sn, compact: true)
                }
            }

            Card {
                VStack(spacing: 8) {
                    SectionTitle(text: "IP 地址")
                    if ips.isEmpty {
                        // S-021:接口失败/空时回退列表自带主 IP
                        if let fallback = item["ip"] as? String, !fallback.isEmpty {
                            ipRow(["ip": fallback, "type": "IPv4"])
                        } else {
                            Text(err == nil ? "没有 IP 数据" : "—").font(.system(size: 11)).foregroundColor(t.color(t.faint))
                        }
                    }
                    ForEach(ips.indices, id: \.self) { i in ipRow(ips[i]) }
                }
            }

            Card {
                VStack(spacing: 8) {
                    SectionTitle(text: "网络接口")
                    if nics.isEmpty {
                        Text("无网卡数据").font(.system(size: 11)).foregroundColor(t.color(t.faint))
                    }
                    ForEach(nics.indices, id: \.self) { i in
                        let nic = nics[i]
                        KV(k: mask ? maskIP(nic["mac"] as? String ?? "") : (nic["mac"] as? String ?? "—"),
                           v: nic["virtualNetworkInterface"] as? String ?? (nic["linkType"] as? String ?? "public"), mono: true)
                    }
                }
            }
        }
        .task { await load() }
    }

    private func ipRow(_ ip: [String: Any]) -> some View {
        let raw = ip["ip"] as? String ?? (ip["address"] as? String ?? "—")
        return VStack(spacing: 4) {
            HStack {
                Text(mask ? maskIP(raw) : raw).font(.system(size: 12, design: .monospaced)).foregroundColor(t.color(t.fg))
                Spacer()
                if let t2 = ip["type"] as? String, !t2.isEmpty { Chip(text: t2) }
            }
            if let routed = ip["routedTo"] as? String, !routed.isEmpty {
                Text("→ " + (mask ? maskIP(routed) : routed)).font(.system(size: 10.5)).foregroundColor(t.color(t.muted))
            }
            if let desc = ip["description"] as? String, !desc.isEmpty {
                Text(desc).font(.system(size: 10)).foregroundColor(t.color(t.faint))
            }
        }
    }

    private func memText(_ mem: Any?) -> String {
        if let m = mem as? [String: Any],
           let unit = (m["unit"] as? String)?.lowercased(),
           let val = numToDoubleAny(m["value"]) {
            // OVH 常给 MB(32768),换算成人话
            if unit == "mb" && val >= 1024 { return String(format: "%.0f GB", val / 1024) }
            if unit == "gi" { return "\(Int(val)) GiB" }
            return "\(Int(val)) \(unit.uppercased())"
        }
        return mem.flatMap { "\($0)" } ?? "—"
    }

    /// 全部磁盘组合并为短文本(2×960GB NVMe / 2×2TB HDD)
    private func diskAllText(_ hw: [String: Any]) -> String {
        let groups = (hw["diskGroups"] as? [[String: Any]]) ?? []
        return groups.map { diskText($0) }.joined(separator: " / ")
    }

    private func diskText(_ g: [String: Any]) -> String {
        let n = (g["numberOfDisks"] as? Int) ?? (g["diskCount"] as? Int) ?? 0
        var capText = ""
        if let ds = g["diskSize"] as? [String: Any],
           let v = numToDoubleAny(ds["value"]) {
            let unit = (ds["unit"] as? String ?? "GB").uppercased()
            if unit == "MB" && v >= 1024 { capText = String(format: "%.0f GB", v / 1024) }
            else if unit == "GB" && v >= 1024 { capText = String(format: "%.1f TB", v / 1024) }
            else { capText = "\(Int(v)) \(unit)" }
        }
        let t2 = g["diskType"] as? String ?? (g["type"] as? String ?? "")
        let parts = [n > 0 ? "\(n) ×" : nil, capText.isEmpty ? nil : capText, !t2.isEmpty ? t2 : nil].compactMap { $0 }
        return parts.isEmpty ? "—" : parts.joined(separator: " ")
    }

    private func load() async {
        do {
            async let h = conn.client.getDict("/server-control/\(sn)/hardware")
            async let i = conn.client.getDict("/server-control/\(sn)/ips")
            async let n = conn.client.getDict("/server-control/\(sn)/network-interfaces")
            let (hr, ir, nr) = try await (h, i, n)
            hardware = hr["hardware"] as? [String: Any]
            ips = (ir["ips"] as? [[String: Any]]) ?? []
            nics = (nr["interfaces"] as? [[String: Any]]) ?? (nr["networkInterfaces"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
    }
}

// MARK: - 电源段(操作卡网格)

struct PowerSection: View {
    @EnvironmentObject var theme: Theme
    let sn: String
    let serviceinfo: [String: Any]?
    @Binding var sheet: ServerSheet?
    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 10) {
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 10) {
                opCard(.reboot, icon: "power", title: "硬重启", desc: "断电级重启", tint: t.danger)
                opCard(.rescue, icon: "lifepreserver", title: "救援系统", desc: "进入/退出救援", tint: t.warning)
                opCard(.reinstall, icon: "opticaldiscdrive.fill", title: "重装系统", desc: "全功能安装器", tint: t.danger)
                opCard(.ipmi, icon: "keyboard.onehanded.left", title: "IPMI/KVM", desc: "远程控制台", tint: t.info)
                opCard(.bootMode, icon: "memorychip", title: "启动模式", desc: "硬盘/救援/网络", tint: t.muted)
                opCard(.spla, icon: "pc", title: "Windows 授权", desc: "SPLA / GVLK", tint: t.info)
                opCard(.tasks, icon: "checklist", title: "运维任务", desc: "列表与预约", tint: t.muted)
                opCard(.bios, icon: "cpu", title: "BIOS 设置", desc: "含 SGX", tint: t.muted)
                opCard(.installStatus, icon: "progress.indicator", title: "安装进度", desc: "重装实时进度", tint: t.info)
                opCard(.monitoring, icon: "bell.badge", title: "OVH 监控", desc: "异常邮件通知", tint: t.muted)
            }
        }
    }

    private func opCard(_ kind: ServerSheet.Kind, icon: String, title: String, desc: String, tint: String) -> some View {
        Button { sheet = .init(kind: kind) } label: {
            VStack(alignment: .leading, spacing: 8) {
                RoundedRectangle(cornerRadius: 9)
                    .fill(t.color(tint).opacity(0.14))
                    .frame(width: 34, height: 34)
                    .overlay(Image(systemName: icon).font(.system(size: 14, weight: .semibold)).foregroundColor(t.color(tint)))
                Text(title).font(.system(size: 13, weight: .semibold)).foregroundColor(t.color(t.fg))
                Text(desc).font(.system(size: 10)).foregroundColor(t.color(t.muted)).lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(12)
            .background(RoundedRectangle(cornerRadius: 14).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 14).stroke(t.color(t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }
}

// MARK: - 维护段

struct MaintenanceSection: View {
    @EnvironmentObject var conn: Connection
    @EnvironmentObject var theme: Theme
    let sn: String
    @Binding var sheet: ServerSheet?
    var t: Tokens { theme.t }

    @State private var interventions: [[String: Any]] = []
    @State private var err: String?

    var body: some View {
        VStack(spacing: 10) {
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 10) {
                mRow(.networkSpecs, icon: "network", title: "网络规格", desc: "带宽与 IP 路由")
                mRow(.renewal, icon: "arrow.triangle.2.circlepath", title: "续费策略", desc: "自动/手动/终止")
                mRow(.engagement, icon: "doc.plaintext", title: "合同期", desc: "承诺期管理")
                mRow(.hwReplace, icon: "wrench.and.screwdriver", title: "硬件更换", desc: "硬盘/内存/散热")
                mRow(.changeContact, icon: "person.2", title: "变更联系人", desc: "admin/tech/billing")
                mRow(.retraction, icon: "arrow.uturn.left.circle", title: "撤单", desc: "14 天无理由")
            }

            Card {
                VStack(spacing: 9) {
                    SectionTitle(text: "维护记录(近 20 条)")
                    if let e = err {
                        LoadFailed(message: e) { Task { await load() } }
                    } else if interventions.isEmpty {
                        Text("没有维护记录").font(.system(size: 11)).foregroundColor(t.color(t.faint)).padding(.vertical, 8)
                    } else {
                        ForEach(interventions.prefix(20).indices, id: \.self) { i in
                            let it = interventions[i]
                            VStack(alignment: .leading, spacing: 3) {
                                HStack {
                                    Text(fmtDate(it["date"] as? String ?? it["startDate"] as? String))
                                        .font(.system(size: 11, weight: .semibold)).foregroundColor(t.color(t.fg))
                                    Spacer()
                                }
                                if let ty = it["type"] as? String, !ty.isEmpty {
                                    Text(ty).font(.system(size: 10.5, weight: .medium)).foregroundColor(t.color(t.muted)).lineLimit(2)
                                }
                            }
                            .padding(.vertical, 4)
                        }
                    }
                }
            }
        }
        .task { await load() }
    }

    private func mRow(_ kind: ServerSheet.Kind, icon: String, title: String, desc: String) -> some View {
        Button { sheet = .init(kind: kind) } label: {
            HStack(spacing: 10) {
                Image(systemName: icon).font(.system(size: 15)).foregroundColor(t.color(t.info))
                    .frame(width: 30, height: 30)
                    .background(RoundedRectangle(cornerRadius: 8).fill(t.color(t.info).opacity(0.12)))
                VStack(alignment: .leading, spacing: 1.5) {
                    Text(title).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                    Text(desc).font(.system(size: 9.5)).foregroundColor(t.color(t.muted))
                }
                Spacer()
            }
            .padding(11)
            .background(RoundedRectangle(cornerRadius: 13).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 13).stroke(t.color(t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }

    private func load() async {
        do {
            let r = try await conn.client.getDict("/server-control/\(sn)/interventions")
            interventions = (r["interventions"] as? [[String: Any]]) ?? []
            err = nil
        } catch { err = error.localizedDescription }
    }
}

// MARK: - 高级段

struct AdvancedSection: View {
    @EnvironmentObject var theme: Theme
    let sn: String
    @Binding var sheet: ServerSheet?
    var t: Tokens { theme.t }

    var body: some View {
        VStack(spacing: 10) {
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 10) {
                aRow(.burst, icon: "bolt.horizontal", title: "Burst 流量", desc: "突发带宽")
                aRow(.firewall, icon: "flame", title: "防火墙", desc: "网络层开关")
                aRow(.backupFtp, icon: "externaldrive.badge.icloud", title: "Backup FTP", desc: "备份与 ACL")
                aRow(.secondaryDns, icon: "globe", title: "二级 DNS", desc: "从 DNS 列表")
                aRow(.virtualMac, icon: "macpro.gen3", title: "虚拟 MAC", desc: "IP ↔ MAC 表")
                aRow(.vrack, icon: "link", title: "vRack", desc: "私网成员")
                aRow(.mitigation, icon: "shield.lefthalf.filled", title: "DDoS 缓解", desc: "按 IP 块开关")
                aRow(.ipSpecs, icon: "number.square", title: "IP 规格", desc: "v4/v6 块详情")
                aRow(.orderableBandwidth, icon: "speedometer", title: "可购带宽", desc: "套餐查询")
                aRow(.orderableTraffic, icon: "arrow.up.arrow.down", title: "可购流量", desc: "流量包")
                aRow(.orderableIp, icon: "number", title: "可购 IP 块", desc: "v4/v6")
                aRow(.options, icon: "shippingbox", title: "附加选项", desc: "已订阅清单")
            }
        }
    }

    private func aRow(_ kind: ServerSheet.Kind, icon: String, title: String, desc: String) -> some View {
        Button { sheet = .init(kind: kind) } label: {
            VStack(alignment: .leading, spacing: 8) {
                RoundedRectangle(cornerRadius: 9)
                    .fill(t.color(t.muted).opacity(0.12))
                    .frame(width: 34, height: 34)
                    .overlay(Image(systemName: icon).font(.system(size: 14, weight: .semibold)).foregroundColor(t.color(t.muted)))
                Text(title).font(.system(size: 12.5, weight: .semibold)).foregroundColor(t.color(t.fg))
                Text(desc).font(.system(size: 9.5)).foregroundColor(t.color(t.muted)).lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(12)
            .background(RoundedRectangle(cornerRadius: 13).fill(t.color(t.surface)).overlay(RoundedRectangle(cornerRadius: 13).stroke(t.color(t.border), lineWidth: 1)))
        }
        .buttonStyle(.plain)
    }
}

/// NSNumber/Int/Double → Double(ServerDetailView 磁盘容量用)
func numToDoubleAny(_ v: Any?) -> Double? {
    if let d = v as? Double { return d }
    if let i = v as? Int { return Double(i) }
    if let n = v as? NSNumber { return n.doubleValue }
    if let s2 = v as? String { return Double(s2) }
    return nil
}
